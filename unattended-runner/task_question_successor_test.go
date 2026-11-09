package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func taskQuestionReleasedFixture(t *testing.T, lost bool, setup ...func(*taskQuestionFixture)) (*conversationFixture, *taskQuestionFixture, conversationReply) {
	t.Helper()
	candidate := os.Getenv("CC_CONVERSATION_TODO_BINARY")
	if candidate == "" {
		t.Skip("canonical CLI required")
	}
	var c *conversationFixture
	var f *taskQuestionFixture
	if len(setup) == 1 {
		c, f = taskQuestionConversationSetup(t, setup[0])
	} else {
		c, f = taskQuestionConversationTest(t)
	}
	taskQuestionUseCanonicalTodo(t, c, f, candidate, true)
	c.ask(t)
	mediaJSON(t, filepath.Join(f.r.Jobs, f.e.Session, "job.json"), map[string]any{"id": f.e.Session, "status": "succeeded", "todo_id": 42})
	first := c.reply(t, 8, "a: original direction before the normal successor")
	if lost {
		backend, injected := c.r.Run, false
		c.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
			out, err := backend(ctx, path, args...)
			if err == nil && !injected && path == conversationTodo && args[0] == "question-answer" && !argPresent(args, "--delivery-only") {
				injected = true
				return "", errTaskQuestion
			}
			return out, err
		}
	}
	c.r.ConversationReceiverOnly = true
	h := Health{}
	c.r.processConversations(context.Background(), &h)
	row, err := c.r.todo(42)
	if err != nil || string(row.ResumeGeneration) != "2" || len(row.QuestionAnswers) != 1 || len(row.QuestionWait) != 0 || !strings.Contains(row.conversationInstruction(), first.Body) || lost != (len(h.Errors) != 0) {
		t.Fatalf("canonical release setup failed: %v %v", err, h.Errors)
	}
	return c, f, first
}

func taskQuestionStartSuccessor(t *testing.T, c *conversationFixture, f *taskQuestionFixture) string {
	t.Helper()
	id := "regular-successor"
	if err := os.MkdirAll(filepath.Join(f.r.Jobs, id), 0700); err != nil {
		t.Fatal(err)
	}
	mediaJSON(t, filepath.Join(f.r.Jobs, id, "job.json"), map[string]any{"id": id, "status": "running", "todo_id": 42, "created_at": time.Now().UTC().Format(time.RFC3339Nano)})
	if _, err := c.r.Run(context.Background(), conversationTodo, "start", "42", "--job", id); err != nil {
		t.Fatal(err)
	}
	row, err := c.r.todo(42)
	if err != nil || row.Status != "doing" || row.Worker.Job != id || row.Scheduler.Job != id || string(row.ResumeGeneration) != "3" {
		t.Fatal("normal canonical start did not assign the successor")
	}
	return id
}

func TestTaskQuestionCanonicalStartSuccessor(t *testing.T) {
	for _, kind := range []string{"followup", "edit", "lost_response"} {
		t.Run(kind, func(t *testing.T) {
			c, f, first := taskQuestionReleasedFixture(t, kind == "lost_response")
			id := taskQuestionStartSuccessor(t, c, f)
			message, want := first.Message, first.Body
			if kind == "followup" {
				reply := c.reply(t, 9, "b: full follow-up for the legitimate successor")
				message, want = reply.Message, reply.Body
			} else if kind == "edit" {
				want = "b: edited direction for the legitimate successor"
				c.replies[0].Body = want
				c.edited = map[string]string{first.Message: time.Now().UTC().Format(time.RFC3339Nano)}
			}
			h := Health{}
			c.r.processConversations(context.Background(), &h)
			row, err := c.r.todo(42)
			req := c.r.conversationRequest(message)
			if err != nil || len(h.Errors) != 0 || req == nil || req.Phase != "conversation_reply_delivered" || len(row.QuestionAnswers) != 1 || strings.Count(row.Note, want) != 1 || strings.Count(row.conversationInstruction(), want) != 1 {
				t.Fatalf("normal successor did not receive the same question: %v %v", err, h.Errors)
			}
			if row.Worker.Job != id || row.Scheduler.Job != id || string(row.ResumeGeneration) != "3" {
				t.Fatal("delivery wrote the old owner back")
			}
			if kind == "edit" && strings.Contains(row.conversationInstruction(), first.Body) {
				t.Fatal("pending input kept the superseded version")
			}
			before, _ := os.ReadFile(c.r.TodoPath)
			h = Health{}
			c.r.processConversations(context.Background(), &h)
			after, _ := os.ReadFile(c.r.TodoPath)
			if len(h.Errors) != 0 || !bytes.Equal(before, after) {
				t.Fatal("same message delivered twice", h.Errors)
			}
			if _, err := c.r.conversationTask(context.Background(), 42); err == nil {
				t.Fatal("successor gained general asset access")
			}
		})
	}
}

// Scheduler controls are an offline contract fixture. The Rust acceptance test
// exercises the actual prepare/link writers; the start tests above execute the
// actual CLI. No scheduler process, owner service or real worker is launched.
func taskQuestionSchedulerProgress(t *testing.T, c *conversationFixture, f *taskQuestionFixture, stage string) {
	t.Helper()
	advance := func(operation string, change func(map[string]any)) {
		c.mutateTodo(t, func(row map[string]any) {
			beforeBytes, _ := json.Marshal(row)
			var before taskQuestionControl
			_ = json.Unmarshal(beforeBytes, &before)
			change(row)
			afterBytes, _ := json.Marshal(row)
			var after taskQuestionControl
			_ = json.Unmarshal(afterBytes, &after)
			answer := row["owner_question_answer_receipts"].([]any)[0].(map[string]any)
			state, _ := taskQuestionReadState(f.todos, 42)
			edge := map[string]any{"domain": "todo-owner-question-continuation-v1", "request_id": answer["answer"].(map[string]any)["request_id"], "question_receipt_sha256": answer["answer"].(map[string]any)["question_receipt_sha256"], "answer_file_sha256": answer["answer_file_sha256"], "metadata": taskQuestionMetadataOf(state), "operation": operation, "before": before, "after": after, "recorded_at": time.Now().UTC().Format(time.RFC3339Nano)}
			edges, _ := row["owner_question_continuation_receipts"].([]any)
			row["owner_question_continuation_receipts"] = append(edges, edge)
		})
	}
	key := strings.Repeat("a", 32)
	advance("scheduler_prepare", func(row map[string]any) {
		s := row["scheduler"].(map[string]any)
		s["key"], s["phase"] = key, "preparing"
	})
	if stage == "preparing" {
		return
	}
	advance("scheduler_prepared", func(row map[string]any) { row["scheduler"].(map[string]any)["phase"] = "prepared" })
	if stage == "prepared" {
		return
	}
	id := "regular-scheduler-successor"
	if err := os.MkdirAll(filepath.Join(f.r.Jobs, id), 0700); err != nil {
		t.Fatal(err)
	}
	mediaJSON(t, filepath.Join(f.r.Jobs, id, "job.json"), map[string]any{"id": id, "status": "running", "todo_id": 42, "launch_key": key, "created_at": time.Now().UTC().Format(time.RFC3339Nano)})
	advance("scheduler_link", func(row map[string]any) {
		row["status"] = "doing"
		row["jobs"] = append(row["jobs"].([]any), id)
		s := row["scheduler"].(map[string]any)
		s["job"], s["phase"] = id, "running"
	})
}

func TestTaskQuestionSchedulerSuccessor(t *testing.T) {
	for _, stage := range []string{"preparing", "prepared", "running"} {
		for _, kind := range []string{"followup", "edit", "lost_response"} {
			t.Run(stage+"_"+kind, func(t *testing.T) {
				c, f, first := taskQuestionReleasedFixture(t, kind == "lost_response")
				taskQuestionSchedulerProgress(t, c, f, stage)
				message, want := first.Message, first.Body
				if kind == "followup" {
					reply := c.reply(t, 9, "b: full follow-up during normal scheduler progress")
					message, want = reply.Message, reply.Body
				} else if kind == "edit" {
					want = "b: edited direction during normal scheduler progress"
					c.replies[0].Body = want
					c.edited = map[string]string{first.Message: time.Now().UTC().Format(time.RFC3339Nano)}
				}
				h := Health{}
				c.r.processConversations(context.Background(), &h)
				row, err := c.r.todo(42)
				req := c.r.conversationRequest(message)
				if err != nil || len(h.Errors) != 0 || req == nil || req.Phase != "conversation_reply_delivered" || len(row.QuestionAnswers) != 1 || strings.Count(row.Note, want) != 1 || strings.Count(row.conversationInstruction(), want) != 1 || string(row.ResumeGeneration) != "2" {
					t.Fatalf("scheduler progress held the same question: %v %v", err, h.Errors)
				}
				before, _ := os.ReadFile(c.r.TodoPath)
				h = Health{}
				c.r.processConversations(context.Background(), &h)
				after, _ := os.ReadFile(c.r.TodoPath)
				if len(h.Errors) != 0 || !bytes.Equal(before, after) {
					t.Fatal("scheduler reply replay was not idempotent", h.Errors)
				}
				if _, err := c.r.conversationTask(context.Background(), 42); err == nil {
					t.Fatal("scheduler progress granted general root access")
				}
			})
		}
	}
}

func TestTaskQuestionAssignedSuccessorRejectsUnprovenChanges(t *testing.T) {
	for _, kind := range []string{"generation", "worker", "hold", "question_wait", "dependency", "missing_ledger", "edge_before", "edge_after", "edge_metadata", "edge_answer", "edge_question", "edge_operation", "edge_time", "job_missing", "job_id", "job_task", "job_stop", "job_created", "scope", "expired"} {
		t.Run(kind, func(t *testing.T) {
			c, f, _ := taskQuestionReleasedFixture(t, false)
			id := taskQuestionStartSuccessor(t, c, f)
			jobFile := filepath.Join(f.r.Jobs, id, "job.json")
			job := map[string]any{"id": id, "status": "running", "todo_id": 42, "created_at": time.Now().UTC().Format(time.RFC3339Nano)}
			switch kind {
			case "job_missing":
				if err := os.Remove(jobFile); err != nil {
					t.Fatal(err)
				}
			case "job_id", "job_task", "job_stop", "job_created":
				if kind == "job_id" {
					job["id"] = "unrelated"
				}
				if kind == "job_task" {
					job["todo_id"] = 43
				}
				if kind == "job_stop" {
					job["owner_stopped"] = true
				}
				if kind == "job_created" {
					job["created_at"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
				}
				mediaJSON(t, jobFile, job)
			case "expired":
				f.r.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
				c.r.Now = f.r.Now
			default:
				c.mutateTodo(t, func(row map[string]any) {
					switch kind {
					case "generation":
						row["resume_generation"] = 4
						row["worker_receipt"].(map[string]any)["generation"] = 4
					case "worker":
						row["worker_receipt"].(map[string]any)["job"] = "forged"
					case "hold":
						row["owner_stopped"] = true
					case "question_wait":
						row["owner_question_wait"] = map[string]any{"domain": "other-wait"}
					case "dependency":
						row["wait_on"] = []uint64{99}
					case "scope":
						row["spec"] = "another-spec"
					case "missing_ledger":
						delete(row, "owner_question_continuation_receipts")
					default:
						edge := row["owner_question_continuation_receipts"].([]any)[0].(map[string]any)
						switch kind {
						case "edge_before":
							edge["before"].(map[string]any)["resume_generation"] = 0
						case "edge_after":
							edge["after"].(map[string]any)["worker_receipt"].(map[string]any)["job"] = "forged"
						case "edge_metadata":
							edge["metadata"].(map[string]any)["root"] = "D:/unrequested"
						case "edge_answer":
							edge["answer_file_sha256"] = strings.Repeat("b", 64)
						case "edge_question":
							edge["question_receipt_sha256"] = strings.Repeat("b", 64)
						case "edge_operation":
							edge["operation"] = "arbitrary_generation"
						case "edge_time":
							edge["recorded_at"] = time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
						}
					}
				})
			}
			c.reply(t, 9, "follow-up must not cross an unproven assignment")
			before, _ := os.ReadFile(c.r.TodoPath)
			h := Health{}
			c.r.processConversations(context.Background(), &h)
			after, _ := os.ReadFile(c.r.TodoPath)
			if len(h.Errors) == 0 || !bytes.Equal(before, after) {
				t.Fatal("unproven successor mutated Task", h.Errors)
			}
		})
	}
}

func TestTaskQuestionSchedulerRejectsForgedLink(t *testing.T) {
	for _, kind := range []string{"key", "generation", "gap", "scope", "job_key", "job_task", "hold"} {
		t.Run(kind, func(t *testing.T) {
			c, f, _ := taskQuestionReleasedFixture(t, false)
			taskQuestionSchedulerProgress(t, c, f, "running")
			if kind == "job_key" || kind == "job_task" {
				path := filepath.Join(f.r.Jobs, "regular-scheduler-successor", "job.json")
				b, _ := os.ReadFile(path)
				var job map[string]any
				_ = json.Unmarshal(b, &job)
				if kind == "job_key" {
					job["launch_key"] = strings.Repeat("b", 32)
				} else {
					job["todo_id"] = 43
				}
				mediaJSON(t, path, job)
			} else {
				c.mutateTodo(t, func(row map[string]any) {
					switch kind {
					case "key":
						row["scheduler"].(map[string]any)["key"] = strings.Repeat("b", 32)
					case "generation":
						row["resume_generation"] = 3
						row["worker_receipt"].(map[string]any)["generation"] = 3
					case "gap":
						row["owner_question_continuation_receipts"] = row["owner_question_continuation_receipts"].([]any)[1:]
					case "scope":
						row["owner_question_continuation_receipts"].([]any)[0].(map[string]any)["metadata"].(map[string]any)["id"] = 43
					case "hold":
						row["paused"] = true
					}
				})
			}
			c.reply(t, 9, "new reply must not cross a forged scheduler link")
			before, _ := os.ReadFile(c.r.TodoPath)
			h := Health{}
			c.r.processConversations(context.Background(), &h)
			after, _ := os.ReadFile(c.r.TodoPath)
			if len(h.Errors) == 0 || !bytes.Equal(before, after) {
				t.Fatal("forged scheduler link accepted", h.Errors)
			}
		})
	}
}

func TestTaskQuestionSuccessorStopAndGenerationWinAppendRace(t *testing.T) {
	for _, change := range []string{"stop", "generation", "other_wait"} {
		t.Run(change, func(t *testing.T) {
			c, f, _ := taskQuestionReleasedFixture(t, false)
			taskQuestionStartSuccessor(t, c, f)
			reply := c.reply(t, 9, "new reply must not overwrite a concurrent hold")
			backend, injected := c.r.Run, false
			c.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if !injected && path == conversationTodo && args[0] == "question-answer" && argPresent(args, "--delivery-only") {
					injected = true
					c.mutateTodo(t, func(row map[string]any) {
						if change == "stop" {
							row["owner_stopped"] = true
						} else if change == "other_wait" {
							row["owner_question_wait"] = map[string]any{"domain": "other-wait"}
						} else {
							row["resume_generation"] = 4
							row["worker_receipt"].(map[string]any)["generation"] = 4
						}
					})
				}
				return backend(ctx, path, args...)
			}
			h := Health{}
			c.r.processConversations(context.Background(), &h)
			row, _ := c.r.todo(42)
			if !injected || len(h.Errors) == 0 || strings.Contains(row.Note, reply.Body) || strings.Contains(row.conversationInstruction(), reply.Body) || c.r.conversationRequest(reply.Message).Phase == "conversation_reply_delivered" {
				t.Fatal("concurrent hold/generation was overwritten", h.Errors)
			}
		})
	}
}
