package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func generatedDelegationFixture(t *testing.T) (generatedQuestionRegistry, generatedQuestionBundle, string, map[string]any, *generatedQuestionOriginJob) {
	t.Helper()
	s, _ := generatedFixture(t)
	b := generatedBundle(s)
	parent := "11234567-0123-0123-0123-012345678901"
	parentTurn := "21234567-0123-0123-0123-012345678901"
	genTurn := "31234567-0123-0123-0123-012345678901"
	assignment := "Android image generation within the parent's original product assignment."
	raw, err := os.ReadFile(s.Rollout)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(raw, []byte{'\n'})
	var message map[string]any
	if err := json.Unmarshal(lines[1], &message); err != nil {
		t.Fatal(err)
	}
	message["payload"].(map[string]any)["content"] = []any{map[string]any{"type": "input_text", "text": assignment}}
	lines[1], err = json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	b.Assignment.SHA = bytesHash(lines[1])
	worktree := filepath.Dir(s.Proof)
	ctx, err := json.Marshal(map[string]any{"type": "turn_context", "timestamp": s.Created, "payload": map[string]any{"turn_id": genTurn, "cwd": worktree}})
	if err != nil {
		t.Fatal(err)
	}
	lines = append(append(append([][]byte{}, lines[:2]...), ctx), lines[2:]...)
	for i := range b.Events {
		b.Events[i].Line++
	}
	b.Delegation = &generatedQuestionDelegation{Job: "genuine-job", Parent: parent, ParentTurn: parentTurn, SpawnItem: "spawn-original", GenerationTurn: genTurn, GenerationItem: "generated-original", Context: generatedEventBound{Line: 3, SHA: bytesHash(ctx)}}
	if err := os.WriteFile(s.Rollout, bytes.Join(lines, []byte{'\n'}), 0600); err != nil {
		t.Fatal(err)
	}
	job := &generatedQuestionOriginJob{ID: "genuine-job", Todo: s.Scope.Todo, Thread: parent, Cwd: worktree, Status: "running"}
	responses := map[string]any{
		"child":  map[string]any{"thread": map[string]any{"id": s.Session, "cwd": worktree, "createdAt": s.Created.Unix() - 1, "parentThreadId": parent, "source": map[string]any{"subAgent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": parent, "depth": 1}}}}},
		"parent": map[string]any{"thread": map[string]any{"id": parent, "cwd": worktree}},
		"spawn":  map[string]any{"data": []any{map[string]any{"turnId": parentTurn, "item": map[string]any{"type": "collabAgentToolCall", "id": "spawn-original", "tool": "spawnAgent", "status": "completed", "senderThreadId": parent, "receiverThreadIds": []string{s.Session}, "prompt": assignment}}}},
		"turn":   map[string]any{"data": []any{map[string]any{"turnId": genTurn, "item": map[string]any{"id": "generated-original", "type": "imageGeneration", "status": "completed", "savedPath": s.Images[0].Path, "inputImages": []any{}}}}},
	}
	l := generatedQuestionLineage{Job: func(context.Context, string, uint64) (generatedQuestionOriginJob, error) { return *job, nil }, Call: func(_ context.Context, method string, args map[string]any) (json.RawMessage, error) {
		var v any
		if method == "thread/read" && args["threadId"] == s.Session {
			v = responses["child"]
		} else if method == "thread/read" && args["threadId"] == parent {
			v = responses["parent"]
		} else if method == "thread/items/read" && args["threadId"] == parent && args["turnId"] == parentTurn {
			v = responses["spawn"]
		} else if method == "thread/items/read" && args["threadId"] == s.Session && args["turnId"] == genTurn {
			v = responses["turn"]
		} else {
			return nil, errors.New("unexpected readonly API request")
		}
		return json.Marshal(v)
	}}
	r := generatedRegistryFixture(t, s)
	r.Lineage = l.verify
	manifest := filepath.Join(filepath.Dir(s.Proof), "delegated-evidence.json")
	writeGeneratedBundle(t, manifest, b)
	return r, b, manifest, responses, job
}

func TestGeneratedQuestionGenuineDelegationAcceptsOriginalNonPrefixAssignment(t *testing.T) {
	r, _, manifest, _, _ := generatedDelegationFixture(t)
	if _, err := r.check(context.Background(), manifest); err != nil {
		t.Fatalf("normal API bound delegation rejected: %v", err)
	}
	if _, err := os.Stat(r.Home); !os.IsNotExist(err) {
		t.Fatal("check created signed store", err)
	}
}

func TestGeneratedQuestionDelegationRejectsForgedOrOtherScope(t *testing.T) {
	for _, kind := range []string{"job-id", "job-task", "job-parent", "stopped-job", "child-parent", "source-parent", "spawn-child", "spawn-prompt", "spawn-turn", "spawn-tool", "generation-turn", "generation-image", "missing-api", "duplicate-api", "context-hash", "later-assignment", "changed-scope", "expired"} {
		t.Run(kind, func(t *testing.T) {
			r, b, manifest, responses, job := generatedDelegationFixture(t)
			if _, err := r.check(context.Background(), manifest); err != nil {
				t.Fatal("healthy delegated control", err)
			}
			child := responses["child"].(map[string]any)["thread"].(map[string]any)
			spawn := responses["spawn"].(map[string]any)["data"].([]any)[0].(map[string]any)
			generated := responses["turn"].(map[string]any)["data"].([]any)[0].(map[string]any)
			switch kind {
			case "job-id":
				job.ID = "another-job"
			case "job-task":
				job.Todo++
			case "job-parent":
				job.Thread = b.Session
			case "stopped-job":
				job.Stopped = true
			case "child-parent":
				child["parentThreadId"] = b.Session
			case "source-parent":
				child["source"] = map[string]any{"subAgent": map[string]any{"thread_spawn": map[string]any{"parent_thread_id": b.Session, "depth": 1}}}
			case "spawn-child":
				spawn["item"].(map[string]any)["receiverThreadIds"] = []string{b.Delegation.Parent}
			case "spawn-prompt":
				spawn["item"].(map[string]any)["prompt"] = "quoted later assignment"
			case "spawn-turn":
				spawn["turnId"] = b.Delegation.GenerationTurn
			case "spawn-tool":
				spawn["item"].(map[string]any)["tool"] = "sendInput"
			case "generation-turn":
				generated["turnId"] = b.Delegation.ParentTurn
			case "generation-image":
				generated["item"].(map[string]any)["savedPath"] = filepath.Join(filepath.Dir(b.Images[0].Path), "copy.png")
			case "missing-api":
				r.Lineage = (generatedQuestionLineage{Job: func(context.Context, string, uint64) (generatedQuestionOriginJob, error) { return *job, nil }}).verify
			case "duplicate-api":
				r.Lineage = (generatedQuestionLineage{Job: func(context.Context, string, uint64) (generatedQuestionOriginJob, error) { return *job, nil }, Call: func(context.Context, string, map[string]any) (json.RawMessage, error) {
					return json.RawMessage(`{"thread":{},"thread":{}}`), nil
				}}).verify
			case "context-hash":
				b.Delegation.Context.SHA = string(bytes.Repeat([]byte{'0'}, 64))
			case "later-assignment":
				raw, err := os.ReadFile(b.Rollout)
				if err != nil {
					t.Fatal(err)
				}
				lines := bytes.Split(raw, []byte{'\n'})
				lines = append(append(append([][]byte{}, lines[:1]...), lines[1]), lines[1:]...)
				b.Assignment.Line++
				b.Delegation.Context.Line++
				for i := range b.Events {
					b.Events[i].Line++
				}
				if err := os.WriteFile(b.Rollout, bytes.Join(lines, []byte{'\n'}), 0600); err != nil {
					t.Fatal(err)
				}
			case "changed-scope":
				b.Scope.Decision = string(bytes.Repeat([]byte{'c'}, 64))
				r.Destination = func(scope generatedQuestionScope) bool { return scope.Decision == generatedQuestion3084.Decision }
			case "expired":
				r.Now = func() time.Time { return b.Expires }
			}
			writeGeneratedBundle(t, manifest, b)
			if _, err := r.check(context.Background(), manifest); err == nil {
				t.Fatal("forged delegation accepted", kind)
			}
			if _, err := os.Stat(r.Home); !os.IsNotExist(err) {
				t.Fatal("rejected check mutated registry", err)
			}
		})
	}
}

func TestGeneratedQuestionDelegationGuardStopsBeforeApiOrEvidenceRead(t *testing.T) {
	r, _, manifest, _, _ := generatedDelegationFixture(t)
	reads, calls := 0, 0
	r.Guard = func(context.Context, string) (bool, error) { return false, nil }
	r.Read = func(string, int64) ([]byte, error) { reads++; return nil, errors.New("must not read") }
	r.Lineage = func(context.Context, generatedQuestionSpec, string) error { calls++; return nil }
	_, err := r.check(context.Background(), manifest)
	var failure *generatedQuestionFailure
	if !errors.As(err, &failure) || failure.stage != "evidence_guard" || reads != 0 || calls != 0 {
		t.Fatalf("guard boundary changed: reads%d api%d err%v", reads, calls, err)
	}
}

func TestGeneratedQuestionDelegationRejectsDuplicateBundleFields(t *testing.T) {
	r, _, manifest, _, _ := generatedDelegationFixture(t)
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	raw = append([]byte(`{"domain":"forged",`), raw[1:]...)
	if err := os.WriteFile(manifest, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.check(context.Background(), manifest); err == nil {
		t.Fatal("duplicate evidence keys accepted")
	}
}

func TestGeneratedQuestionDelegationRechecksResourceAndParentState(t *testing.T) {
	for _, kind := range []string{"valid", "job-changed", "task-changed", "parent-changed", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			r, b, manifest, responses, job := generatedDelegationFixture(t)
			jobReads, childReads := 0, 0
			l := generatedQuestionLineage{
				Job: func(context.Context, string, uint64) (generatedQuestionOriginJob, error) {
					jobReads++
					copy := *job
					if jobReads > 1 {
						if kind == "job-changed" {
							copy.Status = "succeeded"
						}
						if kind == "task-changed" {
							copy.ResourceSHA = "changed"
						}
					}
					return copy, nil
				},
				Call: func(ctx context.Context, method string, args map[string]any) (json.RawMessage, error) {
					var v any
					if method == "thread/read" && args["threadId"] == b.Session {
						childReads++
						v = responses["child"]
						if childReads > 1 && kind == "parent-changed" {
							return json.RawMessage(`{"thread":{"id":"changed"}}`), nil
						}
					} else if method == "thread/read" && args["threadId"] == b.Delegation.Parent {
						v = responses["parent"]
					} else if method == "thread/items/read" && args["threadId"] == b.Delegation.Parent {
						v = responses["spawn"]
					} else if method == "thread/items/read" && args["threadId"] == b.Session {
						v = responses["turn"]
					} else {
						return nil, errors.New("unexpected method")
					}
					return json.Marshal(v)
				},
			}
			r.Lineage = l.verify
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if kind == "cancelled" {
				cancel()
			}
			_, err := r.check(ctx, manifest)
			if (err == nil) != (kind == "valid") {
				t.Fatal("state change admission differs", kind, err)
			}
			if kind == "valid" && (jobReads != 2 || childReads != 2) {
				t.Fatal("state was not independently rechecked", jobReads, childReads)
			}
		})
	}
}

func TestGeneratedQuestionDelegationNoCrossTurnEventSplice(t *testing.T) {
	r, b, manifest, _, _ := generatedDelegationFixture(t)
	if _, err := r.check(context.Background(), manifest); err != nil {
		t.Fatal("healthy control", err)
	}
	raw, err := os.ReadFile(b.Rollout)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(raw, []byte{'\n'})
	other := []byte(`{"type":"turn_context","payload":{"turn_id":"another-turn","cwd":"another-cwd"}}`)
	// Insert an actual different turn between exec return and wait, retaining
	// every selected native event/hash rather than merely corrupting an image.
	index := b.Events[2].Line - 1
	lines = append(append(append([][]byte{}, lines[:index]...), other), lines[index:]...)
	b.Events[2].Line++
	b.Events[3].Line++
	if err := os.WriteFile(b.Rollout, bytes.Join(lines, []byte{'\n'}), 0600); err != nil {
		t.Fatal(err)
	}
	writeGeneratedBundle(t, manifest, b)
	if _, err := r.check(context.Background(), manifest); err == nil {
		t.Fatal("generation output crossed a turn boundary")
	}
}
