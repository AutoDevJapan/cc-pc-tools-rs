package main

import (
	"bytes"
	"encoding/json"
)

// A complete synthetic 0.165 launch, including each required independent
// checkpoint section and its separately rendered developer content. Legacy
// fixtures below keep their original digest-only format instead.
func skillsCompleteRuntimeFixture(records []object) []object {
	texts := []string{"runtime launch instructions", "runtime permission instructions", "runtime collaboration instructions", "<skills_instructions>synthetic host catalog</skills_instructions>"}
	tool := object{"type": "function", "name": "exec", "description": "synthetic definition", "parameters": object{"type": "object", "properties": object{}}}
	toolHash, _ := skillsRuntimeJSONHash(tool)
	stateFound := false
	for _, r := range records {
		if r["type"] != "world_state" {
			continue
		}
		st := obj(obj(r["payload"])["state"])
		if st == nil || st["agents_md"] == nil {
			continue
		}
		for i, key := range []string{"model", "permissions", "collaboration_mode"} {
			if st[key] == nil {
				st[key] = object{"instructions": skillsRuntimeFragmentHash(texts[i])}
			}
		}
		if st["host_skills"] == nil {
			st["host_skills"] = object{"body": "synthetic host catalog", "includeInstructions": true}
		}
		if st["top_level_tools"] == nil {
			st["top_level_tools"] = object{"exec": toolHash}
		}
		stateFound = true
	}
	if !stateFound {
		return records
	}
	seen := map[string]bool{}
	for _, r := range records {
		p := obj(r["payload"])
		if r["type"] != "response_item" || p["role"] != "developer" {
			continue
		}
		b, _ := json.Marshal(p["content"])
		var blocks []struct{ Text string }
		json.Unmarshal(b, &blocks)
		for _, block := range blocks {
			seen[block.Text] = true
		}
	}
	out := []object{}
	inserted := false
	for _, r := range records {
		if !inserted && r["type"] == "response_item" && obj(r["payload"])["role"] == "user" {
			blocks := []object{}
			for _, text := range texts {
				if !seen[text] {
					blocks = append(blocks, object{"type": "input_text", "text": text})
				}
			}
			out = append(out, object{"type": "response_item", "timestamp": r["timestamp"], "payload": object{"type": "additional_tools", "role": "developer", "tools": []object{tool}}})
			if len(blocks) > 0 {
				out = append(out, object{"type": "response_item", "timestamp": r["timestamp"], "payload": object{"type": "message", "role": "developer", "content": blocks}})
			}
			inserted = true
		}
		out = append(out, r)
	}
	return out
}

func skillsLegacyRuntimeFixture(raw []byte) []byte {
	var out bytes.Buffer
	for _, line := range bytes.Split(raw, []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var e struct {
			Type    string
			Payload struct{ Role string }
		}
		json.Unmarshal(line, &e)
		if e.Type == "response_item" && e.Payload.Role == "developer" {
			continue
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes()
}
