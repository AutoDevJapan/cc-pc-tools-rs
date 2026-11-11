package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// A completed exec has no yielded cell. Bind it to its actual call/result,
// rather than inventing a cell identifier that never appeared in the result.
func generatedSyncNoYieldFixture(t *testing.T) (generatedQuestionRegistry, generatedQuestionBundle, string) {
	t.Helper()
	s, _ := generatedFixture(t)
	r := generatedRegistryFixture(t, s)
	b := generatedBundle(s)
	raw, err := os.ReadFile(s.Rollout)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(raw, []byte{'\n'})
	var result map[string]any
	if err := json.Unmarshal(lines[b.Events[3].Line-1], &result); err != nil {
		t.Fatal(err)
	}
	payload := result["payload"].(map[string]any)
	payload["type"] = "custom_tool_call_output"
	payload["call_id"] = b.Events[0].Call
	final, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.Rollout, bytes.Join([][]byte{lines[0], lines[1], lines[b.Events[0].Line-1], final}, []byte{'\n'}), 0600); err != nil {
		t.Fatal(err)
	}
	b.Events = []generatedEventBound{b.Events[0], {Line: 4, SHA: bytesHash(final), Kind: "custom_tool_call_output", Call: b.Events[0].Call}}
	b.Cell = ""
	var generation map[string]any
	raw, err = os.ReadFile(b.Images[0].Record)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &generation); err != nil {
		t.Fatal(err)
	}
	generation["execCellId"] = ""
	raw, err = json.Marshal(generation)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b.Images[0].Record, raw, 0600); err != nil {
		t.Fatal(err)
	}
	b.Images[0].RecordSHA = bytesHash(raw)
	manifest := filepath.Join(filepath.Dir(s.Proof), "sync-no-yield.json")
	writeGeneratedBundle(t, manifest, b)
	return r, b, manifest
}

func TestGeneratedQuestionSynchronousNoYieldHasNoCell(t *testing.T) {
	r, _, manifest := generatedSyncNoYieldFixture(t)
	if _, err := r.check(context.Background(), manifest); err != nil {
		t.Fatalf("genuine synchronous call/result without yielded cell rejected: %v", err)
	}
	if _, err := os.Stat(r.Home); !os.IsNotExist(err) {
		t.Fatal("read-only check registered evidence", err)
	}
}

func TestGeneratedQuestionNoYieldStillBindsActualCallResult(t *testing.T) {
	for _, kind := range []string{"result-call", "pending-output", "other-tool", "other-scope", "changed-original"} {
		t.Run(kind, func(t *testing.T) {
			r, b, manifest := generatedSyncNoYieldFixture(t)
			if _, err := r.check(context.Background(), manifest); err != nil {
				t.Fatal("healthy no-yield control failed", err)
			}
			if kind == "other-scope" {
				b.Scope.Todo++
			} else if kind == "changed-original" {
				if err := os.WriteFile(b.Images[0].Path, []byte("changed image"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				raw, err := os.ReadFile(b.Rollout)
				if err != nil {
					t.Fatal(err)
				}
				lines := bytes.Split(raw, []byte{'\n'})
				index := 1
				if kind == "other-tool" {
					index = 0
				}
				line := b.Events[index].Line - 1
				var event map[string]any
				if err := json.Unmarshal(lines[line], &event); err != nil {
					t.Fatal(err)
				}
				payload := event["payload"].(map[string]any)
				switch kind {
				case "result-call":
					payload["call_id"] = "another-call"
					b.Events[index].Call = "another-call"
				case "pending-output":
					payload["output"] = "Script running with cell ID genuine-cell\n"
				case "other-tool":
					payload["name"] = "exec_command"
					b.Events[index].Name = "exec_command"
				}
				lines[line], err = json.Marshal(event)
				if err != nil {
					t.Fatal(err)
				}
				b.Events[index].SHA = bytesHash(lines[line])
				if err := os.WriteFile(b.Rollout, bytes.Join(lines, []byte{'\n'}), 0600); err != nil {
					t.Fatal(err)
				}
			}
			writeGeneratedBundle(t, manifest, b)
			if _, err := r.check(context.Background(), manifest); err == nil {
				t.Fatal("no-yield accepted unrelated source", kind)
			}
			if _, err := os.Stat(r.Home); !os.IsNotExist(err) {
				t.Fatal("rejected check created registry", err)
			}
		})
	}
}

func TestGeneratedQuestionAsyncStillRequiresActualCellAndWait(t *testing.T) {
	for _, kind := range []string{"empty-cell", "different-cell", "different-wait-call"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := generatedFixture(t)
			r := generatedRegistryFixture(t, s)
			b := generatedBundle(s)
			manifest := filepath.Join(filepath.Dir(s.Proof), "async-cell-negative.json")
			writeGeneratedBundle(t, manifest, b)
			if _, err := r.check(context.Background(), manifest); err != nil {
				t.Fatal("healthy async control failed", err)
			}
			switch kind {
			case "empty-cell":
				b.Cell = ""
			case "different-cell":
				b.Cell = "another-cell"
			case "different-wait-call":
				raw, err := os.ReadFile(b.Rollout)
				if err != nil {
					t.Fatal(err)
				}
				lines := bytes.Split(raw, []byte{'\n'})
				line := b.Events[3].Line - 1
				var event map[string]any
				if err := json.Unmarshal(lines[line], &event); err != nil {
					t.Fatal(err)
				}
				event["payload"].(map[string]any)["call_id"] = "another-wait"
				lines[line], err = json.Marshal(event)
				if err != nil {
					t.Fatal(err)
				}
				b.Events[3].SHA, b.Events[3].Call = bytesHash(lines[line]), "another-wait"
				if err := os.WriteFile(b.Rollout, bytes.Join(lines, []byte{'\n'}), 0600); err != nil {
					t.Fatal(err)
				}
			}
			writeGeneratedBundle(t, manifest, b)
			if _, err := r.check(context.Background(), manifest); err == nil {
				t.Fatal("async accepted invalid cell/wait", kind)
			}
		})
	}
}
