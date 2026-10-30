package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSkills3080CompletedPublicAndConsumerNativeDenyWrite(t *testing.T) {
	for _, kind := range []string{"public", "consumer"} {
		for _, priorWriter := range []bool{false, true} {
			name := kind + "/held_snapshot"
			if priorWriter {
				name = kind + "/existing_writer"
			}
			t.Run(name, func(t *testing.T) {
				root := skillsFixtureRoot(t)
				t.Setenv("USERPROFILE", root)
				var spec []byte
				if kind == "public" {
					spec, _ = json.Marshal(skillsPublicScopeFixture())
					if _, ok := skillsParsePublicReview(spec); !ok {
						t.Fatal("typed public fixture")
					}
				} else {
					c, _, _, _ := skillsConsumerFixture()
					spec, _ = json.Marshal(c)
					if _, ok := skillsParseConsumer(spec); !ok {
						t.Fatal("typed consumer fixture")
					}
				}
				// Source/launch binding has separate typed-input tests. This is the
				// common Windows native-proof stage after that exact input is bound.
				start := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
				thread := "11111111-1111-4111-8111-111111111111"
				cwd, owner, final := "D:/CC/Work/synthetic-completed-review", "fixed owner instructions", "fixed typed review final"
				dir := filepath.Join(root, ".codex", "sessions", "2026", "10", "25")
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(dir, "rollout-synthetic-"+thread+".jsonl")
				f, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}
				enc := json.NewEncoder(f)
				for _, row := range []object{
					{"type": "session_meta", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"id": thread, "cwd": cwd}},
					{"type": "response_item", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": skillsBootstrapFixture(cwd, owner, false)}}}},
					{"type": "turn_context", "timestamp": start.Add(time.Second).Format(time.RFC3339Nano), "payload": object{"cwd": cwd, "model": "gpt-6-luna", "effort": "max"}},
					{"type": "response_item", "timestamp": start.Add(time.Second).Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": string(spec)}}}},
					{"type": "response_item", "timestamp": start.Add(2 * time.Second).Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "assistant", "phase": "final_answer", "content": []object{{"type": "output_text", "text": final}}}},
				} {
					if err := enc.Encode(row); err != nil {
						f.Close()
						t.Fatal(err)
					}
				}
				if err := f.Close(); err != nil {
					t.Fatal(err)
				}
				if priorWriter {
					writer, err := os.OpenFile(path, os.O_WRONLY, 0600)
					if err != nil {
						t.Fatal(err)
					}
					defer writer.Close()
				}
				job := object{"thread_id": thread, "cwd": cwd, "status": "succeeded", "started_at": start.Format(time.RFC3339Nano), "finished_at": start.Add(time.Minute).Format(time.RFC3339Nano), "model": "gpt-6-luna", "effort": "max"}
				fs := skillsFS{blocked: func(string) bool { return false }}
				defer fs.close()
				n, ok := skillsCompletedReviewNative(&fs, job, spec, hashBytes([]byte(final)), true, hashBytes([]byte(owner)))
				if ok == priorWriter || (ok && len(n) != 1) {
					t.Fatal("typed completed native proof did not enforce write exclusion")
				}
				if !priorWriter {
					writer, err := os.OpenFile(path, os.O_WRONLY, 0600)
					if err == nil {
						writer.Close()
						t.Fatal("held typed completed proof allowed rewriting")
					}
				}
			})
		}
	}
}
