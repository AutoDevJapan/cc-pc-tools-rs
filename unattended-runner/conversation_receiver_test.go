package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestConversationReceiverRejectsArgumentsBeforeLoading(t *testing.T) {
	for _, args := range [][]string{{"--config", "elsewhere"}, {"--notify"}, {"--once"}, {"ask"}} {
		loaded := false
		var out strings.Builder
		code := conversationReceiverCLIWith(args, &out, func() (*Runner, func(), error) {
			loaded = true
			return nil, nil, errors.New("must not load")
		})
		if code != 2 || loaded {
			t.Fatal("receiver accepted another scope or loaded before argument validation", args, code)
		}
	}
}

func TestConversationReceiverHonorsExistingBusyLock(t *testing.T) {
	var out strings.Builder
	code := conversationReceiverCLIWith(nil, &out, func() (*Runner, func(), error) {
		return nil, nil, errConversationBusy
	})
	if code != 2 || out.String() != "busy\n" {
		t.Fatal("receiver did not retain the shared runner exclusion", code, out.String())
	}
}

type receiverReadOnlyTransport struct {
	base http.RoundTripper
	t    *testing.T
}

func (r receiverReadOnlyTransport) RoundTrip(q *http.Request) (*http.Response, error) {
	if q.Method != http.MethodGet {
		r.t.Errorf("receiver attempted an HTTP mutation: %s", q.Method)
		return nil, errors.New("receiver must only read Discord")
	}
	return r.base.RoundTrip(q)
}

func TestConversationReceiverDeliversFullReplyWithoutOtherPeriodicEffects(t *testing.T) {
	f := conversationTest(t, true)
	q := f.ask(t)
	text := "回答の全文\r\n" + strings.Repeat("省略しない🌿\n", 200) + "末尾"
	reply := f.reply(t, 8, text)
	index, err := f.r.loadConversations()
	if err != nil {
		t.Fatal(err)
	}
	reserved := q
	reserved.RequestID, reserved.SenderID, reserved.Phase = "other-reserved", "other-sender", "reserved"
	dir := filepath.Join(f.r.Config.Home, "conversations", reserved.RequestID)
	reserved.BodyFile, reserved.DecisionFile = filepath.Join(dir, "question.txt"), filepath.Join(dir, "decision.json")
	index.Questions[reserved.RequestID] = reserved
	if err := publish(f.r.conversationPath(), index); err != nil {
		t.Fatal(err)
	}
	backend := f.r.Run
	edits, answers := 0, 0
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if len(args) == 0 {
			t.Fatal("receiver invoked an unbounded/general program", path)
		}
		switch {
		case path == conversationSender && args[0] == "recovery-replies":
		case strings.HasSuffix(path, "scrub391.exe") && args[0] == "--guard-path":
		case path == conversationTodo && args[0] == "edit":
			edits++
		case path == conversationTodo && args[0] == "question-answer":
			answers++
		default:
			t.Fatalf("receiver escaped polling/canonical answer delivery: %s %v", path, args)
		}
		return backend(ctx, path, args...)
	}
	f.r.Client.Transport = receiverReadOnlyTransport{http.DefaultTransport, t}
	h := f.r.conversationReceiverTick(context.Background())
	row, err := f.r.todo(42)
	if err != nil || len(h.Errors) != 0 || h.ConversationReplies != 1 || edits != 1 || answers != 1 || !strings.Contains(row.Note, "<discord-owner-answer>\n"+text+"\n</discord-owner-answer>") {
		t.Fatal("receiver failed exact owner-answer delivery", h, edits, answers, err)
	}
	before := row.Note
	h = f.r.conversationReceiverTick(context.Background())
	row, _ = f.r.todo(42)
	if len(h.Errors) != 0 || edits != 1 || answers != 1 || row.Note != before || f.r.State.Requests[reply.Message].Text != text {
		t.Fatal("receiver redelivered an already accepted version", h, edits, answers)
	}
	if h.Registered != 0 || h.Scheduler || h.Retried != 0 || h.Civ6Queue != nil {
		t.Fatal("receiver ran unrelated periodic work", h)
	}
	index, _ = f.r.loadConversations()
	if index.Questions[reserved.RequestID].Phase != "reserved" {
		t.Fatal("receiver retried an old ask reservation")
	}
}

func TestConversationReceiverWritesOnlyDedicatedHealthAndUnlocks(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	path := filepath.Join(f.r.Config.Home, "health.json")
	if err := os.WriteFile(path, []byte("old general health"), 0600); err != nil {
		t.Fatal(err)
	}
	unlocked := false
	var out strings.Builder
	code := conversationReceiverCLIWith(nil, &out, func() (*Runner, func(), error) {
		return f.r, func() { unlocked = true }, nil
	})
	var h Health
	if code != 0 || !unlocked || json.Unmarshal([]byte(out.String()), &h) != nil || h.Mode != "discord-conversation-receiver-only" {
		t.Fatal("receiver CLI completion contract differs", code, out.String())
	}
	b, _ := os.ReadFile(path)
	if string(b) != "old general health" {
		t.Fatal("receiver overwrote general/scheduler health")
	}
	if readJSON(filepath.Join(f.r.Config.Home, "conversation-receiver-health.json"), maxState, &h) != nil || h.ConversationChecked != 1 {
		t.Fatal("receiver did not persist actual poll health")
	}
}

func TestConversationReceiverCapturesButDoesNotChangeStoppedTask(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	f.mutateTodo(t, func(row map[string]any) { row["owner_stopped"] = true })
	before, _ := os.ReadFile(f.r.TodoPath)
	reply := f.reply(t, 8, "停止後の古い元仕事への回答全文")
	base := f.r.Run
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path == conversationTodo {
			t.Fatal("receiver attempted to change a stopped original task", args)
		}
		return base(ctx, path, args...)
	}
	h := f.r.conversationReceiverTick(context.Background())
	after, _ := os.ReadFile(f.r.TodoPath)
	if string(before) != string(after) || h.ReceiverDeliveryHeld != 1 || f.r.State.Requests[reply.Message] == nil || f.r.State.Requests[reply.Message].Text != reply.Body {
		t.Fatal("receiver lost the source or released the owner stop", h)
	}
	if h.ConversationReplies != 0 || f.posts != 1 {
		t.Fatal("receiver counted storage as delivery or sent another question", h)
	}
}

func TestConversationReceiverRejectsWrongOwnerProof(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	reply := f.reply(t, 8, "別人には適用しない")
	base := f.r.Run
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		raw, err := base(ctx, path, args...)
		if err == nil && args[0] == "recovery-replies" {
			var p conversationProof
			if json.Unmarshal([]byte(raw), &p) != nil {
				t.Fatal("invalid fixture proof")
			}
			p.Owner = "999"
			b, _ := json.Marshal(p)
			return string(b), nil
		}
		return raw, err
	}
	before, _ := os.ReadFile(f.r.TodoPath)
	h := f.r.conversationReceiverTick(context.Background())
	after, _ := os.ReadFile(f.r.TodoPath)
	if string(before) != string(after) || len(h.Errors) == 0 || f.r.State.Requests[reply.Message] != nil {
		t.Fatal("receiver accepted a mismatched owner", h)
	}
}

func TestConversationReceiverObservesManualAnswerWithoutForgingDesktopApplication(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			f := conversationTest(t, true)
			f.ask(t)
			reply := f.reply(t, 8, "既に手動で受領して同じ元作業へ反映した全文")
			f.r.processConversations(context.Background(), &Health{})
			req := f.r.State.Requests[reply.Message]
			req.Phase, req.Todo = "conversation_source_verified", 0
			if f.r.save() != nil {
				t.Fatal("fixture capture save failed")
			}
			f.mutateTodo(t, func(row map[string]any) {
				row["status"] = "done"
				record := row["owner_question_answer_receipts"].([]any)[0].(map[string]any)
				record["domain"] = "todo-owner-question-answer-receipt-v1"
				record["applied_at"] = f.r.Now().UTC().Format(time.RFC3339Nano)
				if changed {
					record["answer"].(map[string]any)["reply_sha256"] = strings.Repeat("f", 64)
				}
			})
			before, _ := os.ReadFile(f.r.TodoPath)
			base := f.r.Run
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if path == conversationTodo {
					t.Fatal("receiver changed an already answered desktop origin", args)
				}
				return base(ctx, path, args...)
			}
			h := f.r.conversationReceiverTick(context.Background())
			after, _ := os.ReadFile(f.r.TodoPath)
			if string(before) != string(after) || req.Phase != "conversation_source_verified" || req.Todo != 0 {
				t.Fatal("receiver manufactured delivery/application or changed the original", h)
			}
			if !changed && (h.ReceiverAlreadyAnswered != 1 || len(h.Errors) != 0) {
				t.Fatal("exact manual canonical answer was not recognized", h)
			}
			if changed && (h.ReceiverAlreadyAnswered != 0 || h.ReceiverDeliveryHeld != 1) {
				t.Fatal("a different reply version inherited old acceptance", h)
			}
			if _, err := os.Stat(filepath.Join(f.r.Config.Home, "applied", reply.Message+".json")); !os.IsNotExist(err) {
				t.Fatal("receiver wrote a raw applied record")
			}
		})
	}
}

func TestConversationReceiverActualCanonicalAnswerRoundTrip(t *testing.T) {
	f := conversationNativeTest(t)
	f.ask(t)
	reply := f.reply(t, 8, "a\n既存の元依頼と途中成果を保って続行する全文")
	h := f.r.conversationReceiverTick(context.Background())
	row, err := f.r.todo(42)
	if err != nil || len(h.Errors) != 0 || row.Status != "todo" || len(row.QuestionAnswers) != 1 || !strings.Contains(row.Note, "<discord-owner-answer>\n"+reply.Body+"\n</discord-owner-answer>") {
		t.Fatal("real canonical CLI did not receive exact original answer", h, err)
	}
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("canonical readback unavailable")
	}
	saved := db["items"].([]any)[0].(map[string]any)
	instruction, _ := saved["scheduler"].(map[string]any)["owner_instruction"].(string)
	if !strings.Contains(instruction, reply.Body) || string(row.Spec) != `"unchanged-original-spec"` || len(row.Jobs) != 1 {
		t.Fatal("full pending prompt or original work history was lost")
	}
	before, _ := os.ReadFile(f.r.TodoPath)
	h = f.r.conversationReceiverTick(context.Background())
	after, _ := os.ReadFile(f.r.TodoPath)
	if len(h.Errors) != 0 || string(before) != string(after) {
		t.Fatal("real canonical answer was applied twice", h)
	}
}
