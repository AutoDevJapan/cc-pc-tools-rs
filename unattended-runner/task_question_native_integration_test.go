package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func taskQuestionNativeTransition(t *testing.T, c *conversationFixture, f *taskQuestionFixture, mode string, edit func()) {
	t.Helper()
	helper := os.Getenv("CC_CONVERSATION_SCHEDULER_FIXTURE")
	if helper == "" {
		t.Skip("native scheduler fixture required")
	}
	config := filepath.Join(f.home, "native-fixture.json")
	ready := filepath.Join(f.home, "native-ready")
	release := filepath.Join(f.home, "native-release")
	out := filepath.Join(f.home, "native-result.json")
	_ = os.Remove(ready)
	_ = os.Remove(release)
	mediaJSON(t, config, map[string]any{"domain": "task-question-integration-fixture-v1", "mode": mode, "store": f.todos, "jobs": f.r.Jobs, "ready": ready, "release": release, "output": out})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, helper, "--ignored", "--exact", "question_native_tests::native_question_fixture", "--nocapture")
	hide(cmd)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "CC_TASK_QUESTION_FIXTURE="+config)
	var output strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		deadline := time.Now().Add(15 * time.Second)
		for {
			if _, err := os.Stat(ready); err == nil {
				break
			}
			if time.Now().After(deadline) {
				cancel()
				_ = cmd.Wait()
				t.Fatal("native prepare barrier missing", output.String())
			}
			time.Sleep(10 * time.Millisecond)
		}
		edit()
		if err := os.WriteFile(release, []byte("continue"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal("native scheduler fixture failed", err, output.String())
	}
}

func taskQuestionNativeSetup(t *testing.T) func(*taskQuestionFixture) {
	return func(f *taskQuestionFixture) {
		spec := filepath.Join(f.home, "spec&scope.md")
		if os.WriteFile(spec, []byte("Maintain the requested product."), 0600) != nil {
			t.Fatal("fixture spec")
		}
		f.row["spec"], f.row["dispatchable"] = spec, true
		f.row["scheduler"].(map[string]any)["worktree"] = filepath.Join(f.home, "worktree")
		f.row["scheduler"].(map[string]any)["branch"] = "scheduler/owned-fixture"
	}
}

func TestTaskQuestionNativeSchedulerAndCanonicalStart(t *testing.T) {
	for _, mode := range []string{"reprepare", "linked_start"} {
		for _, kind := range []string{"followup", "edit", "lost_response"} {
			t.Run(mode+"_"+kind, func(t *testing.T) {
				c, f, first := taskQuestionReleasedFixture(t, kind == "lost_response", taskQuestionNativeSetup(t))
				if mode == "reprepare" {
					taskQuestionNativeTransition(t, c, f, "capacity_defer", nil)
					taskQuestionNativeTransition(t, c, f, "prepare", nil)
				} else {
					taskQuestionNativeTransition(t, c, f, "linked_start", nil)
				}
				want := first.Body
				if kind == "followup" {
					want = c.reply(t, 9, "native scheduler follow-up in full").Body
				} else if kind == "edit" {
					want = "native scheduler corrected direction in full"
					c.replies[0].Body = want
					c.edited = map[string]string{first.Message: time.Now().UTC().Format(time.RFC3339Nano)}
				}
				h := Health{}
				c.r.processConversations(context.Background(), &h)
				row, _ := c.r.todo(42)
				if len(h.Errors) != 0 || strings.Count(row.conversationInstruction(), want) != 1 || strings.Count(row.Note, want) != 1 {
					t.Fatal("real writer/consumer roundtrip failed", h.Errors)
				}
				if _, err := c.r.conversationTask(context.Background(), 42); err == nil {
					t.Fatal("native roundtrip granted root access")
				}
			})
		}
	}
}

func TestTaskQuestionNativeReplyEditBeforeDispatch(t *testing.T) {
	for _, stage := range []string{"edit_measure", "edit_launch"} {
		t.Run(stage, func(t *testing.T) {
			c, f, first := taskQuestionReleasedFixture(t, false, taskQuestionNativeSetup(t))
			want := "LATEST_CANONICAL_OWNER_REPLY"
			taskQuestionNativeTransition(t, c, f, stage, func() {
				c.replies[0].Body = want
				c.edited = map[string]string{first.Message: time.Now().UTC().Format(time.RFC3339Nano)}
				h := Health{}
				c.r.processConversations(context.Background(), &h)
				if len(h.Errors) != 0 {
					t.Fatal("canonical edit did not reach pending input", h.Errors)
				}
			})
			b, _ := os.ReadFile(filepath.Join(f.home, "native-result.json"))
			var result struct{ Launched []struct{ Text string } }
			if json.Unmarshal(b, &result) != nil || len(result.Launched) != 0 {
				t.Fatal("stale prompt launched")
			}
			taskQuestionNativeTransition(t, c, f, "dispatch", nil)
			b, _ = os.ReadFile(filepath.Join(f.home, "native-result.json"))
			if json.Unmarshal(b, &result) != nil || len(result.Launched) != 1 || !strings.Contains(result.Launched[0].Text, want) {
				t.Fatal("normal reprepare did not consume latest input")
			}
		})
	}
}

func TestTaskQuestionNativeUnstartedInputRequeue(t *testing.T) {
	c, f, first := taskQuestionReleasedFixture(t, false, taskQuestionNativeSetup(t))
	taskQuestionNativeTransition(t, c, f, "linked_start", nil)
	c.replies[0].Body = "new input after queued preparation"
	c.edited = map[string]string{first.Message: time.Now().UTC().Format(time.RFC3339Nano)}
	h := Health{}
	c.r.processConversations(context.Background(), &h)
	if len(h.Errors) != 0 {
		t.Fatal(h.Errors)
	}
	taskQuestionNativeTransition(t, c, f, "input_requeue", nil)
	row, _ := c.r.todo(42)
	if row.Status != "todo" || row.Worker.Job != "" || string(row.ResumeGeneration) != "4" {
		t.Fatal("normal retirement did not preserve the pending input")
	}
	state, _ := taskQuestionReadState(f.todos, 42)
	jb, _ := os.ReadFile(filepath.Join(f.r.Jobs, "regular-scheduler-successor", "job.json"))
	var proof struct {
		Input struct {
			Scope string `json:"scope_sha256"`
		} `json:"prepared_input"`
	}
	_ = json.Unmarshal(jb, &proof)
	if proof.Input.Scope != canonicalHash(taskQuestionMetadataOf(state)) {
		t.Fatal("native/Go canonical scope encoding differs")
	}
	reply := c.reply(t, 10, "follow-up after normal unstarted reprepare")
	h = Health{}
	c.r.processConversations(context.Background(), &h)
	row, _ = c.r.todo(42)
	if len(h.Errors) != 0 || strings.Count(row.conversationInstruction(), reply.Body) != 1 {
		t.Fatal("unstarted retry broke the same question lineage", h.Errors)
	}
}

func TestTaskQuestionNativeCyclesRejectBrokenProof(t *testing.T) {
	for _, mode := range []string{"reprepare", "linked_start"} {
		for _, kind := range []string{"gap", "key", "generation", "order", "stop", "other_hold"} {
			t.Run(mode+"_"+kind, func(t *testing.T) {
				c, f, _ := taskQuestionReleasedFixture(t, false, taskQuestionNativeSetup(t))
				if mode == "reprepare" {
					taskQuestionNativeTransition(t, c, f, "prepare", nil)
					taskQuestionNativeTransition(t, c, f, "prepare", nil)
				} else {
					taskQuestionNativeTransition(t, c, f, mode, nil)
				}
				c.mutateTodo(t, func(row map[string]any) {
					edges := row["owner_question_continuation_receipts"].([]any)
					switch kind {
					case "gap":
						row["owner_question_continuation_receipts"] = edges[1:]
					case "key":
						row["scheduler"].(map[string]any)["key"] = strings.Repeat("b", 32)
					case "generation":
						row["resume_generation"] = 9
						row["worker_receipt"].(map[string]any)["generation"] = 9
					case "order":
						edges[0], edges[1] = edges[1], edges[0]
					case "stop":
						row["owner_stopped"] = true
					case "other_hold":
						row["owner_question_wait"] = map[string]any{"domain": "another-question"}
					}
				})
				c.reply(t, 10, "unproven next actor cannot consume this reply")
				before, _ := os.ReadFile(f.todos)
				h := Health{}
				c.r.processConversations(context.Background(), &h)
				after, _ := os.ReadFile(f.todos)
				if len(h.Errors) == 0 || !bytes.Equal(before, after) {
					t.Fatal("broken native cycle accepted", h.Errors)
				}
			})
		}
	}
}
