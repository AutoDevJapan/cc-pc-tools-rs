package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTaskQuestionAdmittedReplySurvivesGrantExpiry(t *testing.T) {
	c, f := taskQuestionConversationTest(t)
	c.ask(t)
	reply := c.reply(t, 8, "full authenticated reply after the original ask grant expired")
	f.r.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	c.r.Now = f.r.Now
	read := f.r.Read
	f.r.Read = func(path string, limit int64) ([]byte, error) {
		for _, original := range []string{f.e.Body, f.e.Decision, f.e.Media, f.media.Images[0].Path} {
			if conversationMediaSamePath(path, original) {
				t.Fatal("admitted answer reopened original material")
			}
		}
		return read(path, limit)
	}
	f.r.Media = func(context.Context, string) (generatedQuestionBundle, error) {
		return generatedQuestionBundle{}, errors.New("expired original material grant")
	}
	c.r.ConversationReceiverOnly = true
	h := Health{}
	c.r.processConversations(context.Background(), &h)
	row, _ := c.r.todo(42)
	if len(h.Errors) != 0 || len(row.QuestionWait) != 0 || strings.Count(row.conversationInstruction(), reply.Body) != 1 || c.r.conversationRequest(reply.Message).Phase != "conversation_reply_delivered" {
		t.Fatal("admitted answer stranded by ask expiry", h.Errors)
	}
}

func TestTaskQuestionExpiredFreshAskCannotUseStagedGrant(t *testing.T) {
	c, f := taskQuestionConversationTest(t)
	if err := c.r.recheckTaskQuestion(context.Background(), c.input); err != nil {
		t.Fatal(err)
	}
	f.r.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
	c.r.Now = f.r.Now
	if _, err := c.r.conversationAsk(context.Background(), c.input); err == nil || c.posts != 0 {
		t.Fatal("expired fresh send accepted")
	}
}

func TestTaskQuestionAdmittedReplyNeedsPostedSignatureAfterExpiry(t *testing.T) {
	for _, kind := range []string{"missing", "tampered", "new_hold", "stopped", "original_changed"} {
		t.Run(kind, func(t *testing.T) {
			c, f := taskQuestionConversationTest(t)
			q := c.ask(t)
			c.reply(t, 8, "untrusted continuation must remain held")
			switch kind {
			case "missing":
				if os.Remove(filepath.Join(f.r.Keys.Home, taskQuestionAdmissionName(q.RequestID, true))) != nil {
					t.Fatal("remove fixture admission")
				}
			case "tampered":
				if os.WriteFile(filepath.Join(f.r.Keys.Home, taskQuestionAdmissionName(q.RequestID, true)), []byte(`{"payload":{},"mac":"00"}`), 0600) != nil {
					t.Fatal("fixture admission")
				}
			case "new_hold":
				c.mutateTodo(t, func(row map[string]any) { row["decision_wait"] = map[string]any{"another": "hold"} })
			case "stopped":
				c.mutateTodo(t, func(row map[string]any) { row["owner_stopped"] = true })
			case "original_changed":
				f.e.Repository.ID++
				f.saveEvidence(t)
			}
			f.r.Now = func() time.Time { return time.Now().Add(2 * time.Hour) }
			c.r.Now = f.r.Now
			before, _ := os.ReadFile(f.todos)
			h := Health{}
			c.r.ConversationReceiverOnly = true
			c.r.processConversations(context.Background(), &h)
			after, _ := os.ReadFile(f.todos)
			if len(h.Errors) == 0 || string(before) != string(after) {
				t.Fatal("expired continuation bypassed independent binding", h.Errors)
			}
		})
	}
}

func TestTaskQuestionActualCanonicalLateReply(t *testing.T) {
	candidate := os.Getenv("CC_CONVERSATION_TODO_BINARY")
	if candidate == "" {
		t.Skip("canonical CLI required")
	}
	for _, kind := range []string{"native_owner", "active_owner", "finished_owner_lost_response"} {
		t.Run(kind, func(t *testing.T) {
			c, f := taskQuestionConversationSetup(t, func(f *taskQuestionFixture) { f.e.Expires = time.Now().Add(5 * time.Second); f.saveEvidence(t) })
			taskQuestionUseCanonicalTodo(t, c, f, candidate, kind != "native_owner")
			c.ask(t)
			if kind == "finished_owner_lost_response" {
				mediaJSON(t, filepath.Join(f.r.Jobs, f.e.Session, "job.json"), map[string]any{"id": f.e.Session, "status": "succeeded", "todo_id": 42})
			}
			// Wait for the actual short test grant; leave both real clocks untouched.
			if delay := time.Until(f.e.Expires.Add(20 * time.Millisecond)); delay > 0 {
				time.Sleep(delay)
			}
			reply := c.reply(t, 8, "full late canonical answer")
			backend := c.r.Run
			lost := false
			c.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				out, err := backend(ctx, path, args...)
				if err == nil && kind == "finished_owner_lost_response" && !lost && path == conversationTodo && args[0] == "question-answer" && !argPresent(args, "--delivery-only") {
					lost = true
					return "", errors.New("lost canonical release response")
				}
				return out, err
			}
			c.r.ConversationReceiverOnly = true
			h := Health{}
			c.r.processConversations(context.Background(), &h)
			if kind == "finished_owner_lost_response" {
				if !lost || len(h.Errors) == 0 {
					t.Fatal("response-loss fixture did not run")
				}
				h = Health{}
				c.r.processConversations(context.Background(), &h)
			}
			row, _ := c.r.todo(42)
			if len(h.Errors) != 0 || len(row.QuestionWait) != 0 || len(row.QuestionAnswers) != 1 || strings.Count(row.conversationInstruction(), reply.Body) != 1 {
				t.Fatal("late canonical answer did not reach original task", h.Errors)
			}
			if _, err := c.r.conversationTask(context.Background(), 42); err == nil {
				t.Fatal("late reply granted root access")
			}
			if kind == "finished_owner_lost_response" {
				taskQuestionStartSuccessor(t, c, f)
				next := c.reply(t, 9, "late follow-up for the regular successor")
				h = Health{}
				c.r.processConversations(context.Background(), &h)
				if len(h.Errors) != 0 {
					t.Fatal("late successor follow-up held", h.Errors)
				}
				c.replies[1].Body = "corrected late direction for the successor"
				c.edited = map[string]string{next.Message: time.Now().UTC().Format(time.RFC3339Nano)}
				for i := 0; i < 2; i++ {
					h = Health{}
					c.r.processConversations(context.Background(), &h)
					if len(h.Errors) != 0 {
						t.Fatal("late edit or duplicate held", h.Errors)
					}
				}
				row, _ = c.r.todo(42)
				if strings.Count(row.conversationInstruction(), c.replies[1].Body) != 1 || strings.Contains(row.conversationInstruction(), next.Body) {
					t.Fatal("late edit not preserved exactly once")
				}
			}
		})
	}
}
