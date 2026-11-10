package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Capture the actual durable pre-send state, then leave only the independently
// completed receipt/admission intact to model interruption before index publish.
func taskQuestionInterruptedIndex(t *testing.T, setup func(*taskQuestionFixture)) (*conversationFixture, *taskQuestionFixture) {
	t.Helper()
	candidate := os.Getenv("CC_CONVERSATION_TODO_BINARY")
	if candidate == "" {
		t.Skip("canonical CLI required")
	}
	c, f := taskQuestionConversationSetup(t, setup)
	taskQuestionUseCanonicalTodo(t, c, f, candidate, true)
	backend := c.r.Run
	var reserved, task []byte
	c.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "recovery-ask" && reserved == nil {
			var err error
			reserved, err = os.ReadFile(c.r.conversationPath())
			if err != nil {
				t.Fatal(err)
			}
			task, err = os.ReadFile(f.todos)
			if err != nil {
				t.Fatal(err)
			}
		}
		return backend(ctx, path, args...)
	}
	q := c.ask(t)
	var admission taskQuestionAdmission
	if len(reserved) == 0 || f.r.readSigned(taskQuestionAdmissionName(q.RequestID, true), &admission) != nil || admission.Question != taskQuestionAdmittedIdentity(q) {
		t.Fatal("signed posted boundary not reached")
	}
	if os.WriteFile(c.r.conversationPath(), reserved, 0600) != nil || os.WriteFile(f.todos, task, 0600) != nil {
		t.Fatal("restore interrupted boundary")
	}
	if c.r.save() != nil {
		t.Fatal("save restart state")
	}
	restarted := *c.r
	restarted.Config.Since = c.r.State.Since
	var err error
	restarted.State, err = loadIndependentState(restarted.Config, c.r.State.Destination)
	if err != nil {
		t.Fatal(err)
	}
	c.r = &restarted
	return c, f
}

func TestTaskQuestionUnpublishedAdmissionRejectsChangedBindings(t *testing.T) {
	for _, kind := range []string{"stage_only", "tampered", "receipt", "body", "image", "index_identity", "sender", "owner", "registration", "generation", "stop", "hold", "repo", "root", "api_question"} {
		t.Run(kind, func(t *testing.T) {
			c, f := taskQuestionInterruptedIndex(t, nil)
			index, _ := c.r.loadConversations()
			q := index.Questions[c.input.RequestID]
			switch kind {
			case "stage_only":
				if os.Remove(filepath.Join(f.r.Keys.Home, taskQuestionAdmissionName(q.RequestID, true))) != nil {
					t.Fatal("remove fixture admission")
				}
			case "tampered":
				if os.WriteFile(filepath.Join(f.r.Keys.Home, taskQuestionAdmissionName(q.RequestID, true)), []byte(`{"payload":{},"mac":"00"}`), 0600) != nil {
					t.Fatal("fixture admission")
				}
			case "receipt":
				if os.WriteFile(filepath.Join(c.r.Config.Home, "conversations", q.RequestID, "question.json"), []byte(`{}`), 0600) != nil {
					t.Fatal("fixture receipt")
				}
			case "body":
				if os.WriteFile(q.BodyFile, []byte("different question"), 0600) != nil {
					t.Fatal("fixture body")
				}
			case "image":
				q.Attachments = []string{filepath.Join(f.home, "unrequested.png")}
			case "index_identity":
				q.Title = "another question"
			case "sender":
				q.SenderID = "conversation-v1-" + strings.Repeat("b", 32)
			case "owner":
				c.r.State.Destination.Owner = "301"
			case "registration":
				if os.WriteFile(filepath.Join(f.r.Keys.Home, taskQuestionName(q.RequestID)), []byte(`{}`), 0600) != nil {
					t.Fatal("fixture registration")
				}
			case "generation":
				c.mutateTodo(t, func(row map[string]any) {
					row["resume_generation"] = 2
					row["worker_receipt"].(map[string]any)["generation"] = 2
				})
			case "stop":
				c.mutateTodo(t, func(row map[string]any) { row["owner_stopped"] = true })
			case "hold":
				c.mutateTodo(t, func(row map[string]any) { row["decision_wait"] = map[string]any{"another": "hold"} })
			case "repo":
				f.r.Repo = func(context.Context, taskQuestionRepository) bool { return false }
			case "root":
				f.r.Root = func(context.Context, string) bool { return false }
			case "api_question":
				c.question = snowflake(time.Now().Add(time.Second))
			}
			index.Questions[q.RequestID] = q
			if publish(c.r.conversationPath(), index) != nil {
				t.Fatal("fixture index")
			}
			f.r.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
			c.r.Now = f.r.Now
			before, _ := os.ReadFile(f.todos)
			h := Health{}
			c.r.processConversations(context.Background(), &h)
			after, _ := os.ReadFile(f.todos)
			if len(h.Errors) == 0 || !bytes.Equal(before, after) || c.posts != 1 {
				t.Fatal("unbound admission recovered or sender called", h.Errors)
			}
		})
	}
}

func TestTaskQuestionRecoveredIndexReachesCanonicalLateReplyExactlyOnce(t *testing.T) {
	for _, kind := range []string{"normal", "wait_response_lost", "release_response_lost", "finished_successor"} {
		t.Run(kind, func(t *testing.T) {
			c, f := taskQuestionInterruptedIndex(t, func(f *taskQuestionFixture) { f.e.Expires = time.Now().Add(5 * time.Second); f.saveEvidence(t) })
			if delay := time.Until(f.e.Expires.Add(20 * time.Millisecond)); delay > 0 {
				time.Sleep(delay)
			}
			backend := c.r.Run
			waitLost, releaseLost := false, false
			c.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				out, err := backend(ctx, path, args...)
				if err == nil && path == conversationTodo {
					if kind == "wait_response_lost" && args[0] == "question-wait" && !waitLost {
						waitLost = true
						return "", errors.New("wait response lost")
					}
					if kind == "release_response_lost" && args[0] == "question-answer" && !argPresent(args, "--delivery-only") && !releaseLost {
						releaseLost = true
						return "", errors.New("release response lost")
					}
				}
				return out, err
			}
			h := Health{}
			c.r.processConversations(context.Background(), &h)
			if kind == "wait_response_lost" {
				if !waitLost || len(h.Errors) == 0 {
					t.Fatal("wait loss injection missing")
				}
				h = Health{}
				c.r.processConversations(context.Background(), &h)
			}
			index, err := c.r.loadConversations()
			q := index.Questions[c.input.RequestID]
			if err != nil || len(h.Errors) != 0 || q.Phase != "waiting" || c.posts != 1 {
				t.Fatal("late same-question wait not recovered", h.Errors)
			}
			c.q = q
			if kind == "finished_successor" {
				mediaJSON(t, filepath.Join(f.r.Jobs, f.e.Session, "job.json"), map[string]any{"id": f.e.Session, "status": "succeeded", "todo_id": 42})
			}
			c.reply(t, 8, "full owner answer to the recovered question")
			late := time.Now().UTC()
			c.replies[0].Created = late.Format(time.RFC3339Nano)
			c.replies[0].Message = snowflake(late)
			if !late.After(f.e.Expires) {
				t.Fatal("answer not actually after expiry")
			}
			c.r.ConversationReceiverOnly = true
			h = Health{}
			c.r.processConversations(context.Background(), &h)
			if kind == "release_response_lost" {
				if !releaseLost || len(h.Errors) == 0 {
					t.Fatal("release loss injection missing")
				}
				h = Health{}
				c.r.processConversations(context.Background(), &h)
			}
			row, _ := c.r.todo(42)
			if len(h.Errors) != 0 || len(row.QuestionWait) != 0 || len(row.QuestionAnswers) != 1 || strings.Count(row.conversationInstruction(), c.replies[0].Body) != 1 {
				t.Fatal("recovered answer not delivered", h.Errors)
			}
			if kind == "finished_successor" {
				taskQuestionStartSuccessor(t, c, f)
				c.replies[0].Body = "corrected full reply for the regular successor"
				c.edited = map[string]string{c.replies[0].Message: time.Now().UTC().Format(time.RFC3339Nano)}
			}
			for i := 0; i < 2; i++ {
				h = Health{}
				c.r.processConversations(context.Background(), &h)
				if len(h.Errors) != 0 {
					t.Fatal("replay/edit rejected", h.Errors)
				}
			}
			row, _ = c.r.todo(42)
			if c.posts != 1 || len(row.QuestionAnswers) != 1 || strings.Count(row.conversationInstruction(), c.replies[0].Body) != 1 {
				t.Fatal("reply duplicated")
			}
			if _, err := c.r.conversationTask(context.Background(), 42); err == nil {
				t.Fatal("recovery granted root access")
			}
		})
	}
}

func TestTaskQuestionSignedAdmissionRecoversUnpublishedIndex(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "before_expiry", true: "after_expiry"}[expired], func(t *testing.T) {
			c, f := taskQuestionInterruptedIndex(t, nil)
			if expired {
				f.r.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
				c.r.Now = f.r.Now
			}
			h := Health{}
			c.r.processConversations(context.Background(), &h)
			index, err := c.r.loadConversations()
			row, rowErr := c.r.todo(42)
			if err != nil || rowErr != nil || len(h.Errors) != 0 || index.Questions[c.input.RequestID].Phase != "waiting" || !conversationWaitRecorded(row, index.Questions[c.input.RequestID]) || c.posts != 1 {
				t.Fatal("signed posted question did not recover without another sender call", h.Errors, c.posts)
			}
		})
	}
}

func TestTaskQuestionExpiryDuringFinalValidationNeverCallsSender(t *testing.T) {
	c, f := taskQuestionConversationTest(t)
	media := f.r.Media
	calls := 0
	expired := false
	f.r.Now = func() time.Time {
		if expired {
			return f.e.Expires.Add(time.Second)
		}
		return time.Now()
	}
	c.r.Now = f.r.Now
	f.r.Media = func(ctx context.Context, path string) (generatedQuestionBundle, error) {
		calls++
		value, err := media(ctx, path)
		if calls == 3 {
			expired = true
		}
		return value, err
	}
	_, err := c.r.conversationAsk(context.Background(), c.input)
	if !expired || calls < 3 || err == nil || c.posts != 0 {
		t.Fatal("expired validation reached sender", calls, c.posts, err)
	}
	row, _ := c.r.todo(42)
	if len(row.QuestionWait) != 0 || strings.Contains(row.Note, "discord-conversation-question-v1") {
		t.Fatal("expired fresh ask recorded a wait")
	}
}

func TestTaskQuestionSenderDeadlineAndLateOutcome(t *testing.T) {
	for _, lateCreation := range []bool{false, true} {
		t.Run(map[bool]string{false: "in_flight_before_deadline", true: "creation_after_deadline"}[lateCreation], func(t *testing.T) {
			c, f := taskQuestionConversationTest(t)
			backend := c.r.Run
			invoked := false
			c.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if len(args) > 0 && args[0] == "recovery-ask" {
					invoked = true
					if argValue(args, "--send-before") != f.e.Expires.UTC().Format(time.RFC3339Nano) || !f.r.Now().Before(f.e.Expires) {
						t.Fatal("fresh deadline not carried to sender")
					}
					f.r.Now = func() time.Time { return f.e.Expires.Add(time.Second) }
					c.r.Now = f.r.Now
					if lateCreation {
						c.created = f.r.Now().UTC().Format(time.RFC3339Nano)
						c.question = snowflake(f.r.Now())
					}
				}
				return backend(ctx, path, args...)
			}
			q, err := c.r.conversationAsk(context.Background(), c.input)
			if !invoked || c.posts != 1 {
				t.Fatal("in-flight fixture did not run")
			}
			if lateCreation {
				if err == nil {
					t.Fatal("post-expiry creation admitted")
				}
				if _, retryErr := c.r.conversationAsk(context.Background(), c.input); retryErr == nil || c.posts != 1 {
					t.Fatal("ambiguous late outcome resubmitted")
				}
			} else if err != nil || q.Phase != "waiting" {
				t.Fatal("valid existing creation stranded by late response", err)
			}
		})
	}
}
