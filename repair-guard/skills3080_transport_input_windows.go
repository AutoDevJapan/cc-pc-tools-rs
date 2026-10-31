package main

import (
	"bytes"
	"encoding/json"
	"strings"
)

// The ordered instruction/tool/input-content projection of one independently
// recorded model-visible request object, plus its model and effort. Other
// provider settings are not compared to native or authenticated by this view.
// It does not assert the first network attempt, wire bytes or provider receipt.
type skillsTransportInput struct {
	Model   string
	Effort  string
	Items   []string
	TaskSHA string
}

func skillsSourceKeys(m map[string]json.RawMessage, allowed ...string) bool {
	set := map[string]bool{}
	for _, name := range allowed {
		set[name] = true
	}
	for name := range m {
		if !set[name] {
			return false
		}
	}
	return true
}

func skillsSourceCanonicalSHA(raw []byte) (string, bool) {
	if !skillsNativeUniqueJSON(raw) {
		return "", false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return "", false
	}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if e.Encode(v) != nil {
		return "", false
	}
	return hashBytes(bytes.TrimSuffix(b.Bytes(), []byte{'\n'})), true
}

// Only transport identity fields that the documented request preparation can
// regenerate are omitted. Content, roles, metadata and tool order remain exact.
func skillsSourceInputItem(raw []byte) (sha, role, kind, text string, ok bool) {
	var m map[string]json.RawMessage
	if !skillsNativeUniqueJSON(raw) || json.Unmarshal(raw, &m) != nil || !skillsSourceKeys(m, "type", "role", "id", "phase", "content", "tools", "internal_chat_message_metadata_passthrough") {
		return
	}
	if json.Unmarshal(m["type"], &kind) != nil || json.Unmarshal(m["role"], &role) != nil || (role != "developer" && role != "user") {
		return
	}
	if id, present := m["id"]; present {
		var value *string
		if json.Unmarshal(id, &value) != nil || (value != nil && (len(*value) > 256 || strings.ContainsAny(*value, "\r\n\x00"))) {
			return
		}
		delete(m, "id")
	}
	if phase, present := m["phase"]; present {
		var value string
		if json.Unmarshal(phase, &value) != nil || value != "" {
			return
		}
		delete(m, "phase")
	}
	if metadata, present := m["internal_chat_message_metadata_passthrough"]; present {
		var value map[string]json.RawMessage
		if json.Unmarshal(metadata, &value) != nil || value == nil {
			return
		}
	}
	switch kind {
	case "message":
		if m["tools"] != nil {
			return
		}
		var blocks []json.RawMessage
		if json.Unmarshal(m["content"], &blocks) != nil || len(blocks) < 1 || len(blocks) > 64 {
			return
		}
		var joined strings.Builder
		for _, rawBlock := range blocks {
			var block struct {
				Type string  `json:"type"`
				Text *string `json:"text"`
			}
			if !skillsStrict(rawBlock, &block) || block.Type != "input_text" || block.Text == nil {
				return
			}
			joined.WriteString(*block.Text)
		}
		text = joined.String()
	case "additional_tools":
		if role != "developer" || m["content"] != nil || m["internal_chat_message_metadata_passthrough"] != nil {
			return
		}
		var tools []json.RawMessage
		if json.Unmarshal(m["tools"], &tools) != nil || len(tools) < 1 || len(tools) > 256 {
			return
		}
		for _, rawTool := range tools {
			var tool map[string]json.RawMessage
			var toolType string
			if json.Unmarshal(rawTool, &tool) != nil || tool == nil || json.Unmarshal(tool["type"], &toolType) != nil || toolType == "" {
				return
			}
		}
	default:
		return
	}
	normal, err := json.Marshal(m)
	if err != nil {
		return
	}
	sha, ok = skillsSourceCanonicalSHA(normal)
	return
}

func skillsTransportInputProjection(raw, prompt []byte, model, effort string) (skillsTransportInput, bool) {
	var empty skillsTransportInput
	var m map[string]json.RawMessage
	if len(raw) > 8<<20 || !skillsNativeUniqueJSON(raw) || json.Unmarshal(raw, &m) != nil ||
		!skillsSourceKeys(m, "type", "model", "input", "tools", "instructions", "tool_choice", "parallel_tool_calls", "reasoning", "store", "stream", "stream_options", "include", "service_tier", "prompt_cache_key", "text", "client_metadata", "access_programs", "generate") {
		return empty, false
	}
	if kind, present := m["type"]; present {
		var value string
		if json.Unmarshal(kind, &value) != nil || value != "response.create" {
			return empty, false
		}
	}
	if generate, present := m["generate"]; present && !bytes.Equal(bytes.TrimSpace(generate), []byte("true")) {
		return empty, false
	}
	// A first recorded full request may be the documented logical WebSocket
	// request. Never claim wire delivery; a recorded delta is not a full source.
	var out skillsTransportInput
	var reasoning struct {
		Effort  string `json:"effort"`
		Summary string `json:"summary,omitempty"`
		Context string `json:"context,omitempty"`
	}
	if json.Unmarshal(m["model"], &out.Model) != nil || skillsModel(out.Model) == "" || skillsModel(out.Model) != skillsModel(model) || !skillsStrict(m["reasoning"], &reasoning) || reasoning.Effort != effort {
		return empty, false
	}
	if reasoning.Context != "" && reasoning.Context != "auto" && reasoning.Context != "current_turn" && reasoning.Context != "all_turns" {
		return empty, false
	}
	out.Effort = reasoning.Effort
	// This contract supports the known Responses Lite initial rendering. A
	// second instruction channel or separately supplied tools is unsupported.
	if value, present := m["instructions"]; present {
		var text string
		if json.Unmarshal(value, &text) != nil || text != "" {
			return empty, false
		}
	}
	if value, present := m["tools"]; present {
		var tools []json.RawMessage
		if json.Unmarshal(value, &tools) != nil || len(tools) != 0 {
			return empty, false
		}
	}
	var input []json.RawMessage
	if json.Unmarshal(m["input"], &input) != nil || len(input) < 4 || len(input) > 64 {
		return empty, false
	}
	users, tools, developers := 0, 0, 0
	for i, item := range input {
		sha, role, kind, text, ok := skillsSourceInputItem(item)
		if !ok {
			return empty, false
		}
		if kind == "additional_tools" {
			if i != 0 {
				return empty, false
			}
			tools++
		} else if role == "developer" {
			if users != 0 {
				return empty, false
			}
			developers++
		} else {
			users++
			if i == len(input)-1 {
				if text != string(prompt) {
					return empty, false
				}
				out.TaskSHA = hashBytes([]byte(text))
			}
		}
		out.Items = append(out.Items, sha)
	}
	return out, tools == 1 && developers > 0 && users == 2 && skillsDigest(out.TaskSHA)
}
