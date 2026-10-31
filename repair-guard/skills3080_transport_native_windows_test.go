package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSkills3080IndependentRequestRejectsJointNativeSourceChanges(t *testing.T) {
	for _, kind := range []string{"normal", "regenerated_id", "bootstrap_and_checkpoint", "developer_and_checkpoint", "tool_and_checkpoint", "fragment_order"} {
		t.Run(kind, func(t *testing.T) {
			raw, job, project, start, end := skillsSourcePrefixFixture()
			request, prompt := skillsTransportRequestFixture()
			body, _ := json.Marshal(request)
			expected, ok := skillsTransportInputProjection(body, prompt, "gpt-6-luna", "max")
			if !ok {
				t.Fatal("independent synthetic transport record")
			}
			records := []object{}
			var state object
			for _, line := range bytes.Split(raw, []byte{'\n'}) {
				if len(line) == 0 {
					continue
				}
				var r object
				json.Unmarshal(line, &r)
				if r["type"] == "world_state" {
					state = obj(obj(r["payload"])["state"])
				}
				records = append(records, r)
			}
			for _, r := range records {
				p := obj(r["payload"])
				if r["type"] != "response_item" {
					continue
				}
				if kind == "regenerated_id" {
					p["id"] = "msg_regenerated_" + hashBytes([]byte(str(p["type"]) + str(p["role"])))[:16]
				}
				if kind == "bootstrap_and_checkpoint" && p["role"] == "user" {
					for _, block := range p["content"].([]any) {
						b := obj(block)
						b["text"] = strings.ReplaceAll(str(b["text"]), "registered policy at launch", "different policy at launch")
					}
					obj(state["agents_md"])["text"] = strings.ReplaceAll(str(obj(state["agents_md"])["text"]), "registered policy at launch", "different policy at launch")
				}
				if p["type"] == "message" && p["role"] == "developer" {
					blocks := p["content"].([]any)
					if kind == "developer_and_checkpoint" {
						obj(blocks[0])["text"] = "different runtime model instructions"
						obj(state["model"])["instructions"] = skillsRuntimeFragmentHash(str(obj(blocks[0])["text"]))
					}
					if kind == "fragment_order" {
						blocks[0], blocks[1] = blocks[1], blocks[0]
					}
				}
				if kind == "tool_and_checkpoint" && p["type"] == "additional_tools" {
					tool := obj(p["tools"].([]any)[0])
					tool["description"] = "different tool schema instruction"
					hash, _ := skillsRuntimeJSONHash(tool)
					state["top_level_tools"] = object{"exec": hash}
				}
			}
			records = append(records,
				object{"type": "response_item", "timestamp": start.Add(time.Second).Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": string(prompt)}}}},
				object{"type": "response_item", "timestamp": start.Add(2 * time.Second).Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "assistant", "phase": "final_answer", "content": []object{{"type": "output_text", "text": "normal final"}}}},
			)
			var stream bytes.Buffer
			for _, r := range records {
				json.NewEncoder(&stream).Encode(r)
			}
			home := skillsFixtureRoot(t)
			t.Setenv("USERPROFILE", home)
			dir := filepath.Join(home, ".codex", "sessions", "2030", "01", "01")
			if os.MkdirAll(dir, 0700) != nil || os.WriteFile(filepath.Join(dir, "rollout-fixture-"+str(job["thread_id"])+".jsonl"), stream.Bytes(), 0600) != nil {
				t.Fatal("controlled native source")
			}
			job["status"] = "succeeded"
			proof := func(source *skillsRecordedTransport) *skillsNativeResultProof {
				return &skillsNativeResultProof{prompt: prompt, finalSHA: hashBytes([]byte("normal final")), exactInput: true, bootstrapSHA: hashBytes([]byte("old author policy")), runtimeProject: project, requireRuntimeSource: true, cwd: str(job["cwd"]), frozen: true, runtimeTransport: source}
			}
			// This is the actual former weakness, exercised through the same
			// Windows snapshot/projection engine: paired state still agrees.
			old := skillsFS{blocked: func(string) bool { return false }}
			_, selfConsistent := skillsNativeWindowWithSourceOrigin(&old, job, proof(nil), end, nil)
			old.close()
			if !selfConsistent {
				t.Fatal("joint-change control did not reproduce the original same-log acceptance")
			}
			current := skillsFS{blocked: func(string) bool { return false }}
			defer current.close()
			_, accepted := skillsNativeWindowWithSourceOrigin(&current, job, proof(&skillsRecordedTransport{Input: expected}), end, nil)
			if accepted != (kind == "normal" || kind == "regenerated_id") {
				t.Fatalf("independent source %s accepted=%v", kind, accepted)
			}
		})
	}
}
