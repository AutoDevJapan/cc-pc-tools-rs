package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkills3080RuntimeFingerprintMatchesIndependentProtocolVectors(t *testing.T) {
	// UTF-8, CRLF normalization, role and big-endian lengths independently
	// encoded with .NET; these are public synthetic bytes, not native digests.
	if skillsRuntimeFragmentHash("日本語\r\nx") != "194921f4fe79c87caa7d4191dca9bd6afc79656d" {
		t.Fatal("fragment encoding")
	}
	h, ok := skillsRuntimeJSONHash(object{"type": "function", "name": "exec", "description": "known <&>"})
	if !ok || h != "a9064be086046da9a94a5134fb9d648e5a59af21" {
		t.Fatal("canonical tool encoding")
	}
}

func TestSkills3080RuntimeEnvelopeNeedsIndependentCheckpointContent(t *testing.T) {
	for _, kind := range []string{"exact", "arbitrary_initial_developer", "changed_model_text", "host_body_suffix", "wrong_permission_hash", "changed_tool", "missing_tool", "duplicate_fragment", "unknown_plugin", "renamed_plugin", "catalog_added_instruction", "catalog_label_only", "self_hash_only", "section_alias", "field_alias", "drop_model", "drop_permissions", "drop_collaboration_mode", "drop_host_skills", "drop_top_level_tools", "drop_all", "empty_model", "empty_permissions", "empty_collaboration_mode", "empty_host_skills", "empty_top_level_tools", "missing_approved_prefixes", "null_approved_prefixes", "object_approved_prefixes", "string_approved_prefixes", "missing_model_id", "wrong_model_id", "missing_collaboration_model", "wrong_collaboration_model", "missing_collaboration_mode", "unknown_collaboration_mode"} {
		t.Run(kind, func(t *testing.T) {
			model, permission, collaboration, host := "runtime model instructions", "runtime permissions", "runtime collaboration", "\n## Skills\nknown catalog\n"
			tool := object{"type": "namespace", "name": "functions", "description": "known namespace", "tools": []any{object{"type": "function", "name": "exec", "description": "known function <&>", "parameters": object{"type": "object", "properties": object{}}}}}
			member := tool["tools"].([]any)[0]
			header := object{"type": "namespace", "name": "functions", "description": "known namespace"}
			hh, _ := skillsRuntimeJSONHash(header)
			mh, _ := skillsRuntimeJSONHash(member)
			state := object{"model": object{"model": "gpt-6-luna", "instructions": skillsRuntimeFragmentHash(model)}, "permissions": object{"instructions": skillsRuntimeFragmentHash(permission), "approved_command_prefixes": []any{}}, "collaboration_mode": object{"mode": "default", "model": "gpt-6-luna", "instructions": skillsRuntimeFragmentHash(collaboration)}, "host_skills": object{"body": host, "includeInstructions": true}, "top_level_tools": object{"functions": hh, "functions.exec": mh}}
			texts := []string{model, "<skills_instructions>" + host + "</skills_instructions>", permission, collaboration, skillsRuntimeRecommendationCatalog()}
			switch kind {
			case "arbitrary_initial_developer":
				texts = append([]string{"arbitrary added instruction"}, texts...)
			case "changed_model_text":
				texts[0] += " changed"
			case "host_body_suffix":
				texts[1] += " additional instruction"
			case "wrong_permission_hash":
				state["permissions"] = object{"instructions": strings.Repeat("a", 40)}
			case "changed_tool":
				tool["description"] = "changed declaration"
			case "missing_tool":
				state["top_level_tools"] = object{"functions": hh}
			case "duplicate_fragment":
				texts = append(texts, permission)
			case "unknown_plugin":
				texts[4] = strings.ReplaceAll(texts[4], "box@openai-curated-remote", "unknown@openai-curated-remote")
			case "renamed_plugin":
				texts[4] = strings.ReplaceAll(texts[4], "- Box (", "- Other name (")
			case "catalog_added_instruction":
				texts[4] += "\nadditional task instruction"
			case "catalog_label_only":
				texts[4] = "<recommended_plugins>arbitrary instruction</recommended_plugins>"
			case "self_hash_only":
				texts = append(texts, "unregistered instruction "+hashBytes([]byte("unregistered instruction")))
			case "section_alias":
				state["Model"] = state["model"]
				delete(state, "model")
			case "field_alias":
				state["permissions"] = object{"Instructions": skillsRuntimeFragmentHash(permission)}
			case "missing_approved_prefixes":
				delete(obj(state["permissions"]), "approved_command_prefixes")
			case "null_approved_prefixes":
				obj(state["permissions"])["approved_command_prefixes"] = nil
			case "object_approved_prefixes":
				obj(state["permissions"])["approved_command_prefixes"] = object{}
			case "string_approved_prefixes":
				obj(state["permissions"])["approved_command_prefixes"] = "[]"
			case "missing_model_id":
				delete(obj(state["model"]), "model")
			case "wrong_model_id":
				obj(state["model"])["model"] = "gpt-6.1-sol"
			case "missing_collaboration_model":
				delete(obj(state["collaboration_mode"]), "model")
			case "wrong_collaboration_model":
				obj(state["collaboration_mode"])["model"] = "gpt-6.1-sol"
			case "missing_collaboration_mode":
				delete(obj(state["collaboration_mode"]), "mode")
			case "unknown_collaboration_mode":
				obj(state["collaboration_mode"])["mode"] = "unknown"
			}
			toolPayload := true
			if strings.HasPrefix(kind, "drop_") || strings.HasPrefix(kind, "empty_") {
				key := strings.TrimPrefix(strings.TrimPrefix(kind, "drop_"), "empty_")
				for section, text := range map[string]string{"model": model, "permissions": permission, "collaboration_mode": collaboration, "host_skills": "<skills_instructions>" + host + "</skills_instructions>", "top_level_tools": ""} {
					if key != section && key != "all" {
						continue
					}
					if strings.HasPrefix(kind, "drop_") {
						delete(state, section)
					} else {
						state[section] = object{}
					}
					if section == "top_level_tools" {
						toolPayload = false
						continue
					}
					filtered := []string{}
					for _, v := range texts {
						if v != text {
							filtered = append(filtered, v)
						}
					}
					texts = filtered
				}
			}
			blocks := []object{}
			for _, text := range texts {
				blocks = append(blocks, object{"type": "input_text", "text": text})
			}
			payloads := []object{{"type": "additional_tools", "role": "developer", "tools": []any{tool}}, {"type": "message", "role": "developer", "content": blocks}}
			if !toolPayload {
				payloads = payloads[1:]
			}
			records := [][]byte{}
			for _, payload := range payloads {
				b, _ := json.Marshal(object{"payload": payload})
				records = append(records, b)
			}
			raw, _ := json.Marshal(state)
			if ok := skillsRuntimeEnvelopeMatches(records, raw, "gpt-6-luna"); ok != (kind == "exact") {
				t.Fatalf("independent envelope %s accepted=%v", kind, ok)
			}
		})
	}
}
