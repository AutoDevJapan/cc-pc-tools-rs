package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// These encodings are the documented 0.165.0 WorldStateHash encodings, not
// hashes learned from the response being qualified. The checkpoint is written
// by the runtime independently of its rendered developer messages.
// https://github.com/openai/codex/blob/360319d8dee62d87e2d59fc0c21805d79519cfed/codex-rs/core/src/context/world_state/mod.rs
func skillsRuntimeFragmentHash(text string) string {
	h := sha1.New()
	h.Write([]byte("codex-world-state-fragment-v1\x00"))
	for _, value := range []string{"developer", text} {
		value = strings.ReplaceAll(value, "\r\n", "\n")
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(value)))
		h.Write(n[:])
		h.Write([]byte(value))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func skillsRuntimeJSONHash(value any) (string, bool) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if e.Encode(value) != nil {
		return "", false
	}
	// Go and serde both sort object keys. Tool schemas contain integral JSON
	// numbers; UseNumber below retains their original spelling.
	sum := sha1.Sum(bytes.TrimSuffix(b.Bytes(), []byte{'\n'}))
	return hex.EncodeToString(sum[:]), true
}

// A pure catalog is a separate known runtime fragment. Its complete static
// wording and public name/ID pairs are fixed here, rather than accepting an
// arbitrary developer message carrying a plausible XML label. Listing a
// plugin conveys no permission to use it or to expand a typed task's scope.
// The rendering contract is RecommendedPluginsInstructions in the same tag.
func skillsRuntimeRecommendationCatalog() string {
	return "<recommended_plugins>\nHere is a list of plugins that are available but not installed.\n\n" +
		"- Box (box@openai-curated-remote)\n" +
		"- Databricks Genie (databricks@openai-curated-remote)\n" +
		"- Figma (figma@openai-curated-remote)\n" +
		"- Gmail (gmail@openai-curated-remote)\n" +
		"- Google Calendar (google-calendar@openai-curated-remote)\n" +
		"- Google Drive (google-drive@openai-curated-remote)\n" +
		"- Notion (notion@openai-curated-remote)\n" +
		"- Outlook Calendar (outlook-calendar@openai-curated-remote)\n" +
		"- Outlook Email (outlook-email@openai-curated-remote)\n" +
		"- SharePoint (sharepoint@openai-curated-remote)\n" +
		"- Slack (slack@openai-curated-remote)\n" +
		"- Snowflake (snowflake@openai-curated-remote)\n" +
		"- Teams (teams@openai-curated-remote)\n</recommended_plugins>"
}

func skillsRuntimeEnvelopeMatches(records [][]byte, state json.RawMessage, model string) bool {
	var st map[string]json.RawMessage
	if !skillsCreationHeaderKeys(state, "model", "permissions", "collaboration_mode", "host_skills", "top_level_tools") || json.Unmarshal(state, &st) != nil {
		return false
	}
	expected := map[string]bool{}
	for _, key := range []string{"model", "permissions", "collaboration_mode"} {
		if len(st[key]) == 0 {
			return false
		}
		var v struct {
			Instructions string          `json:"instructions"`
			Prefixes     json.RawMessage `json:"approved_command_prefixes"`
			Model        string          `json:"model"`
			Mode         string          `json:"mode"`
		}
		if !skillsCreationHeaderKeys(st[key], "instructions", "approved_command_prefixes", "model", "mode") || json.Unmarshal(st[key], &v) != nil {
			return false
		}
		if key == "permissions" {
			var prefixes []json.RawMessage
			if len(v.Prefixes) == 0 || json.Unmarshal(v.Prefixes, &prefixes) != nil || prefixes == nil || len(prefixes) != 0 {
				return false
			}
		} else if len(v.Prefixes) != 0 {
			return false
		}
		if key != "permissions" && (skillsModel(v.Model) == "" || skillsModel(v.Model) != skillsModel(model)) {
			return false
		}
		if key == "collaboration_mode" && v.Mode != "default" && v.Mode != "plan" {
			return false
		}
		if !skillsGitObjectID(v.Instructions) || expected[v.Instructions] {
			return false
		}
		expected[v.Instructions] = true
	}
	var host struct {
		Body    *string `json:"body"`
		Include bool    `json:"includeInstructions"`
	}
	if len(st["host_skills"]) == 0 || !skillsCreationHeaderKeys(st["host_skills"], "body", "includeInstructions") || json.Unmarshal(st["host_skills"], &host) != nil || host.Body == nil || *host.Body == "" || !host.Include {
		return false
	}
	expected[skillsRuntimeFragmentHash("<skills_instructions>"+*host.Body+"</skills_instructions>")] = true
	tools := map[string]string{}
	if len(st["top_level_tools"]) == 0 || json.Unmarshal(st["top_level_tools"], &tools) != nil || len(tools) == 0 {
		return false
	}
	for _, hash := range tools {
		if !civ6Commit.MatchString(hash) {
			return false
		}
	}
	seen := map[string]bool{}
	recommendation, toolSeen := false, false
	for _, raw := range records {
		var e struct {
			Payload json.RawMessage `json:"payload"`
		}
		var v struct {
			Type, Role string
			Content    []json.RawMessage
			Tools      []json.RawMessage
		}
		if json.Unmarshal(raw, &e) != nil || !skillsCreationHeaderKeys(e.Payload, "type", "role", "content", "tools") || json.Unmarshal(e.Payload, &v) != nil || v.Role != "developer" {
			return false
		}
		switch v.Type {
		case "message":
			if len(v.Content) == 0 || len(v.Tools) != 0 {
				return false
			}
			for _, rawBlock := range v.Content {
				var block struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}
				if !skillsStrict(rawBlock, &block) || block.Type != "input_text" {
					return false
				}
				if block.Text == skillsRuntimeRecommendationCatalog() {
					if recommendation {
						return false
					}
					recommendation = true
					continue
				}
				h := skillsRuntimeFragmentHash(block.Text)
				if !expected[h] || seen[h] {
					return false
				}
				seen[h] = true
			}
		case "additional_tools":
			if toolSeen || len(v.Tools) == 0 || len(v.Content) != 0 {
				return false
			}
			toolSeen = true
			declared := map[string]string{}
			for _, rawTool := range v.Tools {
				var tool map[string]any
				d := json.NewDecoder(bytes.NewReader(rawTool))
				d.UseNumber()
				if d.Decode(&tool) != nil {
					return false
				}
				name := str(tool["name"])
				if name == "" {
					name = str(tool["type"])
				}
				if name == "" {
					return false
				}
				if members, exists := tool["tools"]; exists {
					list, ok := members.([]any)
					if !ok || len(list) == 0 {
						return false
					}
					for _, member := range list {
						m, ok := member.(map[string]any)
						if !ok || m["tools"] != nil {
							return false
						}
						n := str(m["name"])
						if n == "" || strings.Contains(n, ".") {
							return false
						}
						key := name + "." + n
						h, ok := skillsRuntimeJSONHash(m)
						if !ok || declared[key] != "" {
							return false
						}
						declared[key] = h
					}
					delete(tool, "tools")
				}
				h, ok := skillsRuntimeJSONHash(tool)
				if !ok || declared[name] != "" {
					return false
				}
				declared[name] = h
			}
			if len(declared) != len(tools) {
				return false
			}
			for name, hash := range declared {
				if tools[name] != hash {
					return false
				}
			}
		default:
			return false
		}
	}
	return len(seen) == len(expected) && (len(tools) == 0 || toolSeen)
}
