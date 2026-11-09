package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Reuse the ordinary sender/API/ToDo fixture and the independent native proof.
// Every HTTP request is served locally; no real owner question is posted.
func taskQuestionConversationTest(t *testing.T) (*conversationFixture, *taskQuestionFixture) {
	t.Helper()
	f := taskQuestionTestAt(t, time.Now().UTC().Truncate(time.Second))
	f.row["note"] = "Preserve original work"
	f.saveTask(t)
	if err := f.r.register(context.Background(), f.path); err != nil {
		t.Fatal(err)
	}
	mediaJSON(t, filepath.Join(f.home, "conversation-mode.json"), map[string]any{"domain": "discord-conversation-mode-v1", "enabled": true})
	c := conversationTest(t, true)
	c.r.Config.Home = filepath.Join(f.home, "unattended")
	c.r.TodoPath = f.todos
	c.r.TaskQuestion = &f.r
	c.r.Now, f.r.Now = time.Now, time.Now
	c.input = f.input
	c.created = time.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339Nano)
	c.question = snowflake(time.Now().Add(-30 * time.Second))
	c.decision, _ = os.ReadFile(f.e.Decision)
	c.originalDecision = c.decision
	body, _ := os.ReadFile(f.e.Body)
	backend := c.r.Run
	c.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if strings.Contains(path, "scrub391") {
			return taskQuestionUnattributedFixture, nil
		}
		if len(args) > 0 && args[0] == "recovery-ask" {
			c.posts++
			b, _ := json.Marshal(map[string]any{"domain": "new-owner-recovery-messages", "delivery": "verified", "request_id": "conversation-v1-" + bytesHash([]byte(c.input.RequestID))[:32], "thread_id": c.question, "message_id": c.question, "progress_thread_id": "610", "original_fingerprint": strings.Repeat("a", 64), "original_question": string(body), "decision": c.decision, "image_sha256": []string{f.media.Images[0].SHA256}})
			return string(b), nil
		}
		if len(args) > 0 && args[0] == "recovery-replies" {
			versions := map[string]conversationReplyVersion{}
			for _, reply := range c.replies {
				v := conversationReplyVersion{}
				if edited, ok := c.edited[reply.Message]; ok {
					v.Edited = &edited
				}
				versions[reply.Message] = v
			}
			b, _ := json.Marshal(conversationProof{Domain: "new-owner-recovery-messages", ReadOnly: true, Source: "discord-api-v10", RequestID: argValue(args, "--request-id"), Thread: c.question, Owner: c.r.State.Destination.Owner, Fingerprint: strings.Repeat("a", 64), Question: c.question, Created: c.created, Progress: "610", Body: string(body), Decision: c.decision, OriginalDecision: c.originalDecision, Replies: c.replies, ReplyVersions: versions})
			return string(b), nil
		}
		out, err := backend(ctx, path, args...)
		if err == nil && len(args) > 0 && args[0] == "question-wait" {
			c.mutateTodo(t, func(row map[string]any) {
				wait := row["owner_question_wait"].(map[string]any)
				wait["domain"] = "todo-owner-question-wait-v1"
				wait["origin"] = map[string]any{"resume_generation": row["resume_generation"], "worker_receipt": row["worker_receipt"], "job": row["scheduler"].(map[string]any)["job"], "key": row["scheduler"].(map[string]any)["key"]}
				wait["blocked_at"] = time.Now().UTC().Format(time.RFC3339Nano)
			})
		}
		if err == nil && len(args) > 0 && args[0] == "question-answer" && !argPresent(args, "--delivery-only") {
			c.mutateTodo(t, func(row map[string]any) {
				for _, raw := range row["owner_question_answer_receipts"].([]any) {
					record := raw.(map[string]any)
					record["domain"] = "todo-owner-question-answer-receipt-v1"
					record["applied_at"] = time.Now().UTC().Format(time.RFC3339Nano)
				}
			})
		}
		return out, err
	}
	return c, f
}

func TestTaskQuestionOwnerReplyReachesOriginalTaskAndPendingInstructions(t *testing.T) {
	c, f := taskQuestionConversationTest(t)
	q := c.ask(t)
	if q.Phase != "waiting" {
		t.Fatal("qualified question did not reach original wait")
	}
	reply := c.reply(t, 8, "a: use this exact direction; preserve the original scope")
	h := Health{}
	c.r.ConversationReceiverOnly = true
	c.r.processConversations(context.Background(), &h)
	if len(h.Errors) != 0 {
		t.Fatalf("qualified owner reply did not reach original task: %v", h.Errors)
	}
	row, err := c.r.todo(42)
	if err != nil || row.Status != "todo" || len(row.QuestionWait) != 0 || len(row.QuestionAnswers) != 1 || !strings.Contains(row.Note, reply.Body) || !strings.Contains(row.conversationInstruction(), reply.Body) {
		t.Fatal("full reply was not durably delivered and the same wait consumed")
	}
	state, err := taskQuestionReadState(f.todos, 42)
	if err != nil || state.Worker.Job != f.e.Session || state.Worker.Generation != 1 || string(state.Generation) != "1" {
		t.Fatal("answer took over the original recipient or generation")
	}
	if req := c.r.conversationRequest(reply.Message); req == nil || req.Todo != 42 || req.Phase != "conversation_reply_delivered" {
		t.Fatal("captured owner source was not linked to the original task")
	}
	if _, err := c.r.conversationTask(context.Background(), 42); err == nil {
		t.Fatal("question metadata granted general root access")
	}
	for _, cmd := range c.commands {
		if cmd == "start" || cmd == "done" || cmd == "run" {
			t.Fatal("delivery launched or completed unrelated work")
		}
	}
}

func TestTaskQuestionReplyRejectsUnboundOrStaleEvidenceWithoutTaskMutation(t *testing.T) {
	for _, kind := range []string{"unregistered", "different_question", "different_task", "different_recipient", "actor", "body", "decision", "image", "declared_image", "question_receipt", "source_receipt", "source_context", "owner_changed", "live_false_owner", "live_bot", "live_edit", "generation", "wait_generation", "wait_recipient", "wait_question_owner", "scheduler_key", "stopped", "paused", "done", "registration", "expired", "destination"} {
		t.Run(kind, func(t *testing.T) {
			c, f := taskQuestionConversationTest(t)
			q := c.ask(t)
			reply := c.reply(t, 8, "a: original owner answer")
			proof, err := c.r.conversationProof(context.Background(), q)
			if err != nil {
				t.Fatal(err)
			}
			req, err := c.r.captureConversationReply(context.Background(), q, proof, reply)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "unregistered":
				f.r.Keys.Home = filepath.Join(f.home, "no-registration")
			case "different_question":
				q.RequestID = "normal-ask-" + strings.Repeat("b", 64)
			case "different_task":
				q.Todo = 43
			case "different_recipient":
				req.Todo = 43
			case "actor":
				q.Actor = &conversationActor{Todo: 43}
			case "body":
				_ = os.WriteFile(q.BodyFile, []byte("unrequested body"), 0600)
			case "decision":
				_ = os.WriteFile(q.DecisionFile, []byte(`{"todo_id":42}`), 0600)
			case "image":
				_ = os.WriteFile(f.media.Images[0].Path, []byte("changed image"), 0600)
			case "declared_image":
				q.AttachmentHashes = []string{strings.Repeat("c", 64)}
				q.InputSHA = canonicalHash([]any{q.Todo, q.OriginThread, q.QuestionSHA, canonicalHash(c.originalDecision), q.Title, q.Wait, q.AttachmentHashes})
				index, _ := c.r.loadConversations()
				index.Questions[q.RequestID] = q
				if publish(c.r.conversationPath(), index) != nil {
					t.Fatal("index fixture")
				}
			case "question_receipt":
				_ = os.WriteFile(q.ReceiptFile, []byte("changed receipt"), 0600)
			case "source_receipt":
				_ = os.WriteFile(filepath.Join(c.r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".json"), []byte(`{}`), 0600)
			case "source_context":
				req.Context = `{"source":"discord-api-v10"}`
			case "owner_changed":
				c.r.State.Destination.Owner = "301"
			case "live_false_owner", "live_bot":
				server(t, c.r, func(w http.ResponseWriter, _ *http.Request) {
					m := Message{}
					m.ID = reply.Message
					m.Channel = q.Thread
					m.Content = reply.Body
					m.Timestamp = reply.Created
					m.Author.ID = reply.Author
					if kind == "live_false_owner" {
						m.Author.ID = "301"
					} else {
						m.Author.Bot = true
					}
					respond(w, m)
				})
			case "live_edit":
				c.replies[0].Body = "edited after source acquisition"
			case "generation":
				c.mutateTodo(t, func(row map[string]any) {
					row["resume_generation"] = 2
					row["worker_receipt"].(map[string]any)["generation"] = 2
				})
			case "wait_generation":
				c.mutateTodo(t, func(row map[string]any) {
					row["owner_question_wait"].(map[string]any)["origin"].(map[string]any)["resume_generation"] = 2
				})
			case "wait_recipient":
				c.mutateTodo(t, func(row map[string]any) {
					origin := row["owner_question_wait"].(map[string]any)["origin"].(map[string]any)
					origin["job"] = "other"
				})
			case "wait_question_owner":
				c.mutateTodo(t, func(row map[string]any) {
					row["owner_question_wait"].(map[string]any)["question"].(map[string]any)["owner_id"] = "301"
				})
			case "scheduler_key":
				c.mutateTodo(t, func(row map[string]any) {
					row["scheduler"].(map[string]any)["key"] = "other"
					row["owner_question_wait"].(map[string]any)["origin"].(map[string]any)["key"] = "other"
				})
			case "stopped":
				c.mutateTodo(t, func(row map[string]any) { row["owner_stopped"] = true })
			case "paused":
				c.mutateTodo(t, func(row map[string]any) { row["paused"] = true })
			case "done":
				c.mutateTodo(t, func(row map[string]any) { row["status"] = "done" })
			case "registration":
				_ = os.WriteFile(filepath.Join(f.r.Keys.Home, taskQuestionName(f.e.Request)), []byte(`{"payload":{},"mac":"00"}`), 0600)
			case "expired":
				f.r.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
				c.r.Now = f.r.Now
			case "destination":
				f.r.Dst = func(generatedQuestionScope) bool { return false }
			}
			before, _ := os.ReadFile(c.r.TodoPath)
			commands := len(c.commands)
			if err := c.r.deliverConversationReply(context.Background(), q, reply, req); err == nil {
				t.Fatal("unbound or stale answer accepted")
			}
			after, _ := os.ReadFile(c.r.TodoPath)
			if !bytes.Equal(before, after) {
				t.Fatal("rejected answer mutated original task")
			}
			for _, cmd := range c.commands[commands:] {
				if cmd == "edit" || cmd == "question-answer" || cmd == "start" || cmd == "done" {
					t.Fatal("rejected answer issued a task mutation")
				}
			}
			if _, err := c.r.conversationTask(context.Background(), 42); err == nil {
				t.Fatal("general root access changed")
			}
		})
	}
}

func TestTaskQuestionReplyRecoversPartialAndLostAnswerWithoutDuplicateDelivery(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "partial", true: "lost_response"}[lost], func(t *testing.T) {
			c, _ := taskQuestionConversationTest(t)
			q := c.ask(t)
			reply := c.reply(t, 8, "a: exactly once")
			backend := c.r.Run
			releases, appends := 0, 0
			c.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if path == conversationTodo && args[0] == "question-answer" {
					if argPresent(args, "--delivery-only") {
						appends++
					} else {
						releases++
						if releases == 1 {
							if lost {
								if _, err := backend(ctx, path, args...); err != nil {
									return "", err
								}
							}
							return "", errors.New("synthetic uncertain answer result")
						}
					}
				}
				return backend(ctx, path, args...)
			}
			h := Health{}
			c.r.ConversationReceiverOnly = true
			c.r.processConversations(context.Background(), &h)
			if len(h.Errors) == 0 || c.r.conversationRequest(reply.Message).Phase == "conversation_reply_delivered" {
				t.Fatal("partial result fabricated complete delivery")
			}
			row, _ := c.r.todo(42)
			if !strings.Contains(row.conversationInstruction(), reply.Body) {
				t.Fatal("full reply not parked before release")
			}
			h = Health{}
			c.r.processConversations(context.Background(), &h)
			row, _ = c.r.todo(42)
			if len(h.Errors) != 0 || row.Status != "todo" || len(row.QuestionAnswers) != 1 || appends != 1 || strings.Count(row.conversationInstruction(), reply.Body) != 1 {
				t.Fatalf("same-source recovery failed: %v appends=%d", h.Errors, appends)
			}
			want := 2
			if lost {
				want = 1
			}
			if releases != want {
				t.Fatal("answer replay released or mutated another wait")
			}
			if _, err := c.r.conversationTask(context.Background(), 42); err == nil {
				t.Fatal("general access was granted")
			}
			_ = q
		})
	}
}

func argPresent(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestTaskQuestionAskRetryRetainsTheSameWait(t *testing.T) {
	c, _ := taskQuestionConversationTest(t)
	q := c.ask(t)
	before, _ := os.ReadFile(c.r.TodoPath)
	again, err := c.r.conversationAsk(context.Background(), c.input)
	after, _ := os.ReadFile(c.r.TodoPath)
	if err != nil || again.RequestID != q.RequestID || again.ReceiptSHA != q.ReceiptSHA || c.posts != 1 || !bytes.Equal(before, after) {
		t.Fatal("idempotent ask changed the question or its original wait", err)
	}
}

func TestTaskQuestionReplyBindsOriginalAndNormalizedDecision(t *testing.T) {
	c, f := taskQuestionConversationTest(t)
	var decision map[string]any
	if json.Unmarshal(c.originalDecision, &decision) != nil {
		t.Fatal("decision fixture")
	}
	resources := decision["resources"].([]any)
	resources[0], resources[1] = resources[1], resources[0]
	mediaJSON(t, f.e.Decision, decision)
	c.originalDecision, _ = os.ReadFile(f.e.Decision)
	f.e.Scope.Decision = canonicalHash(c.originalDecision)
	f.media.Scope = f.e.Scope
	mediaJSON(t, f.e.Media, f.media)
	b, _ := os.ReadFile(f.e.Media)
	f.e.MediaSHA = bytesHash(b)
	f.e.Request = "normal-ask-" + canonicalHash([]any{"normal-ask-v1", f.e.Scope.Body, f.e.Scope.Decision, f.e.Scope.Todo, f.e.Title, f.e.Origin, [][]string{{f.media.Images[0].SHA256, "png"}}})
	f.input.RequestID = f.e.Request
	c.input = f.input
	f.saveEvidence(t)
	if err := f.r.register(context.Background(), f.path); err != nil {
		t.Fatal(err)
	}
	q := c.ask(t)
	if q.DecisionSHA == f.e.Scope.Decision {
		t.Fatal("did not exercise sender-normalized decision")
	}
	reply := c.reply(t, 8, "a: same original decision despite normalized identity")
	h := Health{}
	c.r.ConversationReceiverOnly = true
	c.r.processConversations(context.Background(), &h)
	if len(h.Errors) != 0 || c.r.conversationRequest(reply.Message).Phase != "conversation_reply_delivered" {
		t.Fatal("original decision was confused with sender identity", h.Errors)
	}
}

func TestTaskQuestionReplyAfterAnswerKeepsScopeAndRejectsNewGeneration(t *testing.T) {
	for _, kind := range []string{"followup", "edited", "stale_generation", "corrupt_answer_author"} {
		t.Run(kind, func(t *testing.T) {
			c, _ := taskQuestionConversationTest(t)
			c.ask(t)
			first := c.reply(t, 8, "a: first version")
			c.r.ConversationReceiverOnly = true
			h := Health{}
			c.r.processConversations(context.Background(), &h)
			if len(h.Errors) != 0 {
				t.Fatal(h.Errors)
			}
			if kind == "edited" {
				c.replies[0].Body = "b: revised current answer"
				c.edited = map[string]string{first.Message: time.Now().UTC().Format(time.RFC3339Nano)}
			} else {
				c.reply(t, 9, "a: current follow-up")
			}
			if kind == "stale_generation" {
				c.mutateTodo(t, func(row map[string]any) {
					row["resume_generation"] = 2
					row["worker_receipt"].(map[string]any)["generation"] = 2
				})
			}
			if kind == "corrupt_answer_author" {
				c.mutateTodo(t, func(row map[string]any) {
					row["owner_question_answer_receipts"].([]any)[0].(map[string]any)["answer"].(map[string]any)["author_id"] = "301"
				})
			}
			before, _ := os.ReadFile(c.r.TodoPath)
			h = Health{}
			c.r.processConversations(context.Background(), &h)
			if kind == "stale_generation" || kind == "corrupt_answer_author" {
				after, _ := os.ReadFile(c.r.TodoPath)
				if len(h.Errors) == 0 || !bytes.Equal(before, after) {
					t.Fatal("old question mutated another generation or accepted a false owner")
				}
				return
			}
			row, _ := c.r.todo(42)
			want := c.replies[len(c.replies)-1].Body
			if len(h.Errors) != 0 || !strings.Contains(row.conversationInstruction(), want) || len(row.QuestionAnswers) != 1 {
				t.Fatal("same-generation follow-up was not delivered", h.Errors)
			}
		})
	}
}

func taskQuestionUseCanonicalTodo(t *testing.T, c *conversationFixture, f *taskQuestionFixture, candidate string, active bool) {
	t.Helper()
	notify := filepath.Join(f.home, "isolated-notify.json")
	mediaJSON(t, notify, map[string]any{"guild_id": "100", "owner_user_id": "300", "application_id": "400", "task_channel_id": "200"})
	jobs := filepath.Join(f.home, "isolated-jobs")
	f.r.Jobs = jobs
	if active {
		if os.MkdirAll(filepath.Join(jobs, f.e.Session), 0700) != nil {
			t.Fatal("job fixture")
		}
		mediaJSON(t, filepath.Join(jobs, f.e.Session, "job.json"), map[string]any{"id": f.e.Session, "status": "running", "todo_id": 42})
	}
	backend := c.r.Run
	c.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path != conversationTodo {
			return backend(ctx, path, args...)
		}
		cmd := exec.CommandContext(ctx, candidate, args...)
		hide(cmd)
		cmd.WaitDelay = time.Second
		cmd.Env = append(os.Environ(), "CC_TODO_HOME="+f.home, "CC_TODO_JOBS="+jobs, "CC_ASK_CONFIG="+notify)
		b, err := cmd.Output()
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				t.Logf("isolated canonical %s rejected: %s", args[0], strings.TrimSpace(string(exit.Stderr)))
			}
			return "", errors.New("isolated canonical command rejected")
		}
		return string(b), nil
	}
}

func TestTaskQuestionActualCanonicalReplyRoundTrip(t *testing.T) {
	candidate := os.Getenv("CC_CONVERSATION_TODO_BINARY")
	if candidate == "" {
		t.Skip("fresh canonical CLI required")
	}
	if !filepath.IsAbs(candidate) {
		t.Fatal("candidate must be absolute")
	}
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "native_owner", true: "active_owner"}[active], func(t *testing.T) {
			c, f := taskQuestionConversationTest(t)
			taskQuestionUseCanonicalTodo(t, c, f, candidate, active)
			q := c.ask(t)
			reply := c.reply(t, 8, "a: actual canonical delivery")
			h := Health{}
			c.r.ConversationReceiverOnly = true
			c.r.processConversations(context.Background(), &h)
			row, err := c.r.todo(42)
			if err != nil || len(h.Errors) != 0 || row.Status != "todo" || len(row.QuestionWait) != 0 || len(row.QuestionAnswers) != 1 || len(row.QuestionDeliveries) != 1 || !strings.Contains(row.conversationInstruction(), reply.Body) {
				t.Fatalf("actual canonical reply delivery incomplete: %v", h.Errors)
			}
			state, err := taskQuestionReadState(f.todos, 42)
			if err != nil || state.Worker.Job != f.e.Session || state.Worker.Generation != 1 {
				t.Fatal("canonical delivery took over the original owner")
			}
			if req := c.r.conversationRequest(reply.Message); req == nil || req.Todo != 42 || req.Phase != "conversation_reply_delivered" {
				t.Fatal("actual canonical receipt did not bind original task")
			}
			if _, err := c.r.conversationTask(context.Background(), 42); err == nil {
				t.Fatal("canonical acceptance granted root access")
			}
			_ = q
		})
	}
}

func TestTaskQuestionActualCanonicalStopsAndGenerationWinRaces(t *testing.T) {
	candidate := os.Getenv("CC_CONVERSATION_TODO_BINARY")
	if candidate == "" {
		t.Skip("fresh canonical CLI required")
	}
	for _, stage := range []string{"append", "release"} {
		for _, change := range []string{"stop", "generation"} {
			t.Run(stage+"_"+change, func(t *testing.T) {
				c, f := taskQuestionConversationTest(t)
				taskQuestionUseCanonicalTodo(t, c, f, candidate, true)
				c.ask(t)
				reply := c.reply(t, 8, "a: race must preserve new stop or generation")
				backend := c.r.Run
				injected := false
				c.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
					if path == conversationTodo && args[0] == "question-answer" && argPresent(args, "--delivery-only") == (stage == "append") && !injected {
						injected = true
						c.mutateTodo(t, func(row map[string]any) {
							if change == "stop" {
								row["owner_stopped"] = true
							} else {
								row["resume_generation"] = 2
								row["worker_receipt"].(map[string]any)["generation"] = 2
							}
						})
					}
					return backend(ctx, path, args...)
				}
				h := Health{}
				c.r.ConversationReceiverOnly = true
				c.r.processConversations(context.Background(), &h)
				row, _ := c.r.todo(42)
				if !injected || len(h.Errors) == 0 || row.Status != "block" || len(row.QuestionWait) == 0 || len(row.QuestionAnswers) != 0 || c.r.conversationRequest(reply.Message).Phase == "conversation_reply_delivered" {
					t.Fatal("stale delivery released a stop or newer generation")
				}
				if stage == "append" && len(row.QuestionDeliveries) != 0 {
					t.Fatal("CAS allowed a stale append")
				}
			})
		}
	}
}
