package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReceiverCommandFailureHelper(t *testing.T) {
	if len(os.Args) == 0 || os.Args[len(os.Args)-1] != "--receiver-command-failure" {
		return
	}
	fmt.Fprintln(os.Stdout, "private-command-stdout")
	fmt.Fprintln(os.Stderr, "private-command-stderr")
	os.Exit(7)
}

func TestReceiverGuardFailureDistinguishesExitFromCallerDeadline(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	_, exitError := runCommandBounded(context.Background(), time.Second, executable,
		"-test.run=^TestReceiverCommandFailureHelper$", "--", "--receiver-command-failure")
	if exitError == nil {
		t.Fatal("helper did not fail")
	}
	parent, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	_, deadlineError := runCommandBounded(parent, time.Second, executable,
		"-test.run=^TestReceiverCommandFailureHelper$", "--", "--receiver-command-failure")
	for _, sample := range []struct {
		name, cause string
		err         error
	}{
		{"exit", "command_exit", exitError},
		{"deadline", "caller_deadline", deadlineError},
	} {
		t.Run(sample.name, func(t *testing.T) {
			f := conversationTest(t, true)
			f.ask(t)
			f.reply(t, 8, "private-owner-text")
			base := f.r.Run
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if strings.HasSuffix(path, "scrub391.exe") {
					return "private-guard-output", sample.err
				}
				return base(ctx, path, args...)
			}
			h := receiverHealth(f.r.conversationReceiverTick(context.Background()))
			b, _ := json.Marshal(h)
			var public struct {
				Failures []struct {
					Code     string `json:"code"`
					Cause    string `json:"command_cause"`
					ExitCode *int   `json:"command_exit_code"`
				} `json:"delivery_failures"`
			}
			if json.Unmarshal(b, &public) != nil || len(public.Failures) != 1 || public.Failures[0].Code != "task_guard_command" || public.Failures[0].Cause != sample.cause {
				t.Fatalf("lost safe command cause: %s", b)
			}
			if sample.name == "exit" && (public.Failures[0].ExitCode == nil || *public.Failures[0].ExitCode != 7) {
				t.Fatal("lost actual subprocess exit code")
			}
			for _, secret := range []string{"private-command-stdout", "private-command-stderr", "private-guard-output", "private-owner-text", executable} {
				if strings.Contains(string(b), secret) {
					t.Fatal("private command material reached receiver health")
				}
			}
		})
	}
}

func TestReceiverSlowGuardLeavesBudgetForNextDelivery(t *testing.T) {
	f := conversationTest(t, true)
	q := f.ask(t)
	reply := f.reply(t, 8, "answer once")
	proof, err := f.r.conversationProof(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	f.r.ConversationReceiverOnly = true
	initial := f.r.conversationReceiverTick(context.Background())
	if initial.ConversationReplies != 1 {
		t.Fatal("fixture answer was not delivered before the audit")
	}
	originalNote, _ := os.ReadFile(f.r.TodoPath)
	base := f.r.Run
	guardStarted := false
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if strings.HasSuffix(path, "scrub391.exe") && !guardStarted {
			guardStarted = true
			<-ctx.Done()
			return "", ctx.Err()
		}
		return base(ctx, path, args...)
	}
	index, err := f.r.loadConversations()
	if err != nil {
		t.Fatal(err)
	}
	parent, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	health := Health{receiver: &conversationReceiverCounts{}}
	f.r.processConversationReplies(parent, index, q.RequestID, q, proof, &health)
	if !guardStarted || parent.Err() != nil {
		t.Fatal("one guard exhausted the whole receiver deadline")
	}
	if len(health.receiver.Failures) != 1 || health.receiver.Failures[0].CommandCause != "guard_deadline" {
		t.Fatal("the bounded guard deadline lost its distinct cause")
	}
	before, _ := os.ReadFile(f.r.TodoPath)
	if string(before) != string(originalNote) {
		t.Fatal("timed-out audit changed the existing answer")
	}
	// The same outer receiver remains usable after the bounded operation.
	f.r.Run = base
	f.r.processConversationReplies(parent, index, q.RequestID, q, proof, &health)
	if req := f.r.conversationRequest(reply.Message); req == nil || req.Phase != "conversation_reply_delivered" {
		t.Fatal("a later healthy delivery lost the remaining receiver budget")
	}
	after, _ := os.ReadFile(f.r.TodoPath)
	if string(after) != string(originalNote) || health.receiver.Audited != 1 {
		t.Fatal("recovered audit repeated the delivered answer")
	}
	if _, err := os.Stat(filepath.Join(f.r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".json")); err != nil {
		t.Fatal("the exact saved source was lost")
	}
}

func TestReceiverNewAnswerRetainsTransactionBudget(t *testing.T) {
	f := conversationTest(t, true)
	q := f.ask(t)
	f.reply(t, 8, "new answer")
	proof, err := f.r.conversationProof(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	f.r.ConversationReceiverOnly = true
	base := f.r.Run
	checked := false
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path == conversationTodo && args[0] == "question-answer" {
			deadline, exists := ctx.Deadline()
			if !exists || time.Until(deadline) <= 10*time.Second {
				t.Fatal("new answer inherited an unchanged-audit deadline")
			}
			checked = true
		}
		return base(ctx, path, args...)
	}
	index, _ := f.r.loadConversations()
	ctx, cancel := context.WithTimeout(context.Background(), conversationReceiverBudget)
	defer cancel()
	health := Health{receiver: &conversationReceiverCounts{}}
	f.r.processConversationReplies(ctx, index, q.RequestID, q, proof, &health)
	if !checked || health.ConversationReplies != 1 || len(health.Errors) != 0 {
		t.Fatal("new answer did not complete through the canonical transaction", health.Errors)
	}
}

func TestReceiverPendingHoldAlsoBoundsItsGuard(t *testing.T) {
	f := conversationTest(t, true)
	q := f.ask(t)
	f.reply(t, 8, "pending held answer")
	f.mutateTodo(t, func(row map[string]any) { row["owner_stopped"] = true })
	f.r.ConversationReceiverOnly = true
	base := f.r.Run
	checked := false
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if strings.HasSuffix(path, "scrub391.exe") {
			deadline, exists := ctx.Deadline()
			if !exists || time.Until(deadline) > conversationReceiverGuardBudget {
				t.Fatal("pending held reply kept the twenty-second guard allowance")
			}
			checked = true
		}
		return base(ctx, path, args...)
	}
	before, _ := os.ReadFile(f.r.TodoPath)
	ctx, cancel := context.WithTimeout(context.Background(), conversationReceiverBudget)
	defer cancel()
	h := receiverHealth(f.r.conversationReceiverTick(ctx))
	after, _ := os.ReadFile(f.r.TodoPath)
	if !checked || h.KnownHold != 1 || string(before) != string(after) || len(h.Errors) != 0 || h.Holds[0].Request != q.RequestID {
		t.Fatal("held source lost its guard/stop boundary", h)
	}
}

func TestReceiverAuditMediaWriteRetainsTransactionBudget(t *testing.T) {
	f, reply, _, _ := ownerReplyMediaFixture(t, true)
	f.r.ConversationReceiverOnly = true
	base := f.r.Run
	checked := false
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path == conversationTodo && args[0] == "edit" {
			deadline, exists := ctx.Deadline()
			if !exists || time.Until(deadline) < 10*time.Second {
				t.Fatal("delivered reply's media write inherited a short guard budget")
			}
			checked = true
		}
		return base(ctx, path, args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), conversationReceiverBudget)
	defer cancel()
	h := receiverHealth(f.r.conversationReceiverTick(ctx))
	row, err := f.r.todo(42)
	if err != nil || !checked || h.Audited != 1 || h.Processed != 0 || len(h.Errors) != 0 || !strings.Contains(row.Note, "conversation-owner-media:") {
		t.Fatal("same-source media retry failed or repeated the answer", h, err)
	}
	if req := f.r.conversationRequest(reply.Message); req.Phase != "conversation_reply_delivered" {
		t.Fatal("media write changed answer delivery identity")
	}
}
