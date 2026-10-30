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
	for _, kind := range []string{"exact", "arbitrary_initial_developer", "changed_model_text", "host_body_suffix", "wrong_permission_hash", "changed_tool", "missing_tool", "duplicate_fragment", "unknown_plugin", "renamed_plugin", "catalog_added_instruction", "catalog_label_only", "self_hash_only", "section_alias", "field_alias"} {
		t.Run(kind, func(t *testing.T) {
			model, permission, collaboration, host := "runtime model instructions", "runtime permissions", "runtime collaboration", "\n## Skills\nknown catalog\n"
			tool := object{"type": "namespace", "name": "functions", "description": "known namespace", "tools": []any{object{"type": "function", "name": "exec", "description": "known function <&>", "parameters": object{"type": "object", "properties": object{}}}}}
			member := tool["tools"].([]any)[0]
			header := object{"type": "namespace", "name": "functions", "description": "known namespace"}
			hh, _ := skillsRuntimeJSONHash(header)
			mh, _ := skillsRuntimeJSONHash(member)
			state := object{"model": object{"instructions": skillsRuntimeFragmentHash(model)}, "permissions": object{"instructions": skillsRuntimeFragmentHash(permission), "approved_command_prefixes": []any{}}, "collaboration_mode": object{"instructions": skillsRuntimeFragmentHash(collaboration)}, "host_skills": object{"body": host, "includeInstructions": true}, "top_level_tools": object{"functions": hh, "functions.exec": mh}}
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
			}
			blocks := []object{}
			for _, text := range texts {
				blocks = append(blocks, object{"type": "input_text", "text": text})
			}
			payloads := []object{{"type": "additional_tools", "role": "developer", "tools": []any{tool}}, {"type": "message", "role": "developer", "content": blocks}}
			records := [][]byte{}
			for _, payload := range payloads {
				b, _ := json.Marshal(object{"payload": payload})
				records = append(records, b)
			}
			raw, _ := json.Marshal(state)
			if ok := skillsRuntimeEnvelopeMatches(records, raw); ok != (kind == "exact") {
				t.Fatalf("independent envelope %s accepted=%v", kind, ok)
			}
		})
	}
}
