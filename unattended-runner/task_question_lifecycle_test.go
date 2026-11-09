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

func TestTaskQuestionWaitRequiredBeforeRegistrationAndSend(t *testing.T) {
	c, f := taskQuestionConversationTest(t)
	f.r.Keys.Home = filepath.Join(f.home, "nonwaiting-registry")
	f.e.Wait = false
	f.saveEvidence(t)
	c.input.Wait = false
	if err := f.r.register(context.Background(), f.path); err == nil {
		t.Fatal("non-waiting purpose was registered")
	}
	if _, err := c.r.conversationAsk(context.Background(), c.input); err == nil || c.posts != 0 {
		t.Fatal("non-waiting purpose reached sender")
	}
}

func TestTaskQuestionWaitRequiredForAnExistingRegistration(t *testing.T) {
	c, _ := taskQuestionConversationTest(t)
	c.input.Wait = false
	if _, err := c.r.conversationAsk(context.Background(), c.input); err == nil || c.posts != 0 {
		t.Fatal("existing purpose sent a non-waiting ask")
	}
}

// Actual canonical retirement must remain intact. The same retained question
// must still deliver edits/follow-ups and recover a lost release response.
func TestTaskQuestionFinishedOwnerCanonicalLifecycle(t *testing.T) {
	candidate := os.Getenv("CC_CONVERSATION_TODO_BINARY")
	if candidate == "" {
		t.Skip("canonical CLI required")
	}
	for _, kind := range []string{"followup", "edit", "lost_response", "prior_result", "repeated_edits"} {
		t.Run(kind, func(t *testing.T) {
			c, f := taskQuestionConversationTest(t)
			taskQuestionUseCanonicalTodo(t, c, f, candidate, true)
			c.ask(t)
			mediaJSON(t, filepath.Join(f.home, "isolated-jobs", f.e.Session, "job.json"), map[string]any{"id": f.e.Session, "status": "succeeded", "todo_id": 42})
			if kind == "prior_result" {
				c.mutateTodo(t, func(row map[string]any) {
					row["processed_results"] = map[string]any{f.e.Session: map[string]any{"generation": 1, "reason": "block"}}
				})
			}
			first := c.reply(t, 8, "a: original canonical answer")
			if kind == "lost_response" {
				backend := c.r.Run
				lost := false
				c.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
					out, err := backend(ctx, path, args...)
					if err == nil && !lost && path == conversationTodo && args[0] == "question-answer" && !argPresent(args, "--delivery-only") {
						lost = true
						return "", errTaskQuestion
					}
					return out, err
				}
			}
			c.r.ConversationReceiverOnly = true
			h := Health{}
			c.r.processConversations(context.Background(), &h)
			row, err := c.r.todo(42)
			if err != nil || string(row.ResumeGeneration) != "2" || row.Worker.Job != "" || row.Scheduler.Job != "" || len(row.QuestionAnswers) != 1 {
				t.Fatal("normal finished-owner retirement was changed")
			}
			if kind != "lost_response" && len(h.Errors) != 0 {
				t.Fatal(h.Errors)
			}
			if kind == "lost_response" {
				if len(h.Errors) == 0 {
					t.Fatal("lost response not exercised")
				}
			} else if kind == "edit" || kind == "repeated_edits" {
				c.replies[0].Body = "b: revised direction after canonical retirement"
				c.edited = map[string]string{first.Message: c.r.Now().UTC().Format("2006-01-02T15:04:05.999999999Z07:00")}
			} else {
				c.reply(t, 9, "a: same-question follow-up after retirement")
			}
			h = Health{}
			c.r.processConversations(context.Background(), &h)
			row, err = c.r.todo(42)
			want := c.replies[len(c.replies)-1].Body
			if err != nil || len(h.Errors) != 0 || !strings.Contains(row.conversationInstruction(), want) || len(row.QuestionAnswers) != 1 {
				t.Fatalf("canonical successor could not consume the same question: %v", h.Errors)
			}
			if _, err := c.r.conversationTask(context.Background(), 42); err == nil {
				t.Fatal("retirement granted general root access")
			}
			if kind == "repeated_edits" {
				c.replies[0].Body = "a: second edit retains the first canonical transition"
				c.edited[first.Message] = c.r.Now().UTC().Format(time.RFC3339Nano)
				h = Health{}
				c.r.processConversations(context.Background(), &h)
				row, _ = c.r.todo(42)
				if len(h.Errors) != 0 || !strings.Contains(row.conversationInstruction(), c.replies[0].Body) {
					t.Fatal("immutable transition did not survive edits", h.Errors)
				}
			}
		})
	}
}

func TestTaskQuestionFinishedOwnerRejectsUnprovenSuccessors(t *testing.T) {
	candidate := os.Getenv("CC_CONVERSATION_TODO_BINARY")
	if candidate == "" {
		t.Skip("canonical CLI required")
	}
	for _, kind := range []string{"generation", "new_worker", "old_worker", "new_hold", "dependency", "scheduler_phase", "last_job", "key", "result", "answer_owner", "answer_wait", "answer_hash", "applied_at", "delivery", "intent_mac", "intent_question", "intent_registration", "intent_before", "job_running", "job_stop", "job_other_task", "expired", "other_scope"} {
		t.Run(kind, func(t *testing.T) {
			c, f := taskQuestionConversationTest(t)
			taskQuestionUseCanonicalTodo(t, c, f, candidate, true)
			q := c.ask(t)
			jobPath := filepath.Join(f.home, "isolated-jobs", f.e.Session, "job.json")
			mediaJSON(t, jobPath, map[string]any{"id": f.e.Session, "status": "succeeded", "todo_id": 42})
			c.reply(t, 8, "a: canonical original")
			c.r.ConversationReceiverOnly = true
			h := Health{}
			c.r.processConversations(context.Background(), &h)
			if len(h.Errors) != 0 {
				t.Fatal(h.Errors)
			}
			row, _ := c.r.todo(42)
			var answer struct {
				SHA string `json:"answer_file_sha256"`
			}
			_ = json.Unmarshal(row.QuestionAnswers[0], &answer)
			intentName := taskQuestionReleaseName(q.RequestID, answer.SHA)
			intentPath := filepath.Join(f.r.Keys.Home, intentName)
			c.reply(t, 9, "follow-up must not cross an unproven successor")
			switch kind {
			case "generation":
				c.mutateTodo(t, func(row map[string]any) {
					row["resume_generation"] = 3
					row["worker_receipt"].(map[string]any)["generation"] = 3
				})
			case "new_worker", "old_worker":
				c.mutateTodo(t, func(row map[string]any) {
					id := "different"
					if kind == "old_worker" {
						id = f.e.Session
					}
					row["worker_receipt"].(map[string]any)["job"] = id
					row["scheduler"].(map[string]any)["job"] = id
				})
			case "new_hold":
				c.mutateTodo(t, func(row map[string]any) { row["owner_stopped"] = true })
			case "dependency":
				c.mutateTodo(t, func(row map[string]any) { row["wait_on"] = []uint64{99} })
			case "scheduler_phase":
				c.mutateTodo(t, func(row map[string]any) { row["scheduler"].(map[string]any)["phase"] = "running" })
			case "last_job":
				c.mutateTodo(t, func(row map[string]any) { row["scheduler"].(map[string]any)["last_job"] = "different" })
			case "key":
				c.mutateTodo(t, func(row map[string]any) { row["scheduler"].(map[string]any)["key"] = "different" })
			case "result":
				c.mutateTodo(t, func(row map[string]any) {
					row["processed_results"].(map[string]any)[f.e.Session].(map[string]any)["reason"] = "superseded"
				})
			case "answer_owner", "answer_wait", "answer_hash", "applied_at":
				c.mutateTodo(t, func(row map[string]any) {
					a := row["owner_question_answer_receipts"].([]any)[0].(map[string]any)
					switch kind {
					case "answer_owner":
						a["answer"].(map[string]any)["author_id"] = "301"
					case "answer_wait":
						a["wait"].(map[string]any)["origin"].(map[string]any)["resume_generation"] = 3
					case "answer_hash":
						a["answer_file_sha256"] = strings.Repeat("b", 64)
					case "applied_at":
						a["applied_at"] = time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
					}
				})
			case "delivery":
				c.mutateTodo(t, func(row map[string]any) {
					row["owner_question_delivery_receipts"].([]any)[0].(map[string]any)["note_sha256"] = strings.Repeat("b", 64)
				})
			case "intent_mac":
				_ = os.WriteFile(intentPath, []byte(`{"payload":{},"mac":"00"}`), 0600)
			case "intent_question", "intent_registration", "intent_before":
				var intent taskQuestionRelease
				if err := f.r.readSigned(intentName, &intent); err != nil {
					t.Fatal(err)
				}
				if kind == "intent_question" {
					intent.QuestionSHA = strings.Repeat("b", 64)
				} else if kind == "intent_registration" {
					intent.Registration = strings.Repeat("b", 64)
				} else {
					intent.Before.ID = 43
				}
				payload, _ := json.Marshal(intent)
				key, err := f.r.Keys.key(false)
				if err != nil {
					t.Fatal(err)
				}
				envelope, _ := json.Marshal(generatedQuestionEnvelope{Payload: payload, MAC: taskQuestionMAC(key, payload)})
				eraseGeneratedKey(key)
				if os.WriteFile(intentPath, envelope, 0600) != nil {
					t.Fatal("intent fixture")
				}
			case "job_running":
				mediaJSON(t, jobPath, map[string]any{"id": f.e.Session, "status": "running", "todo_id": 42})
			case "job_stop":
				mediaJSON(t, jobPath, map[string]any{"id": f.e.Session, "status": "succeeded", "todo_id": 42, "owner_stopped": true})
			case "job_other_task":
				mediaJSON(t, jobPath, map[string]any{"id": f.e.Session, "status": "succeeded", "todo_id": 43})
			case "expired":
				f.r.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
				c.r.Now = f.r.Now
			case "other_scope":
				f.e.Scope.Owner = "301"
				f.saveEvidence(t)
			}
			before, _ := os.ReadFile(c.r.TodoPath)
			h = Health{}
			c.r.processConversations(context.Background(), &h)
			after, _ := os.ReadFile(c.r.TodoPath)
			if len(h.Errors) == 0 || !bytes.Equal(before, after) {
				t.Fatalf("unproven successor accepted: %s %v", kind, h.Errors)
			}
			if _, err := c.r.conversationTask(context.Background(), 42); err == nil {
				t.Fatal("successor granted root access")
			}
		})
	}
}
