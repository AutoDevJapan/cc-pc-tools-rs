package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func budgetWaitQuestions(t *testing.T, f *conversationFixture, base ConversationQuestion) map[string]ConversationQuestion {
	t.Helper()
	index, err := f.r.loadConversations()
	if err != nil {
		t.Fatal(err)
	}
	delete(index.Questions, base.RequestID)
	thread, _ := strconv.ParseUint(base.Thread, 10, 64)
	questions := map[string]ConversationQuestion{}
	for i := 0; i < 8; i++ {
		q := base
		q.RequestID, q.SenderID = fmt.Sprintf("wait-%02d", i), fmt.Sprintf("sender-%02d", i)
		q.Thread = strconv.FormatUint(thread+uint64(i+1), 10)
		q.Message = q.Thread
		root := filepath.Join(f.r.Config.Home, "conversations", q.RequestID)
		q.BodyFile, q.DecisionFile, q.ReceiptFile = filepath.Join(root, "question.txt"), filepath.Join(root, "decision.json"), filepath.Join(root, "question.json")
		index.Questions[q.RequestID] = q
		questions[q.Thread] = q
	}
	if err := publish(f.r.conversationPath(), index); err != nil {
		t.Fatal(err)
	}
	return questions
}

func TestTickStartsFreshIntakeBudgetAfterReplyPolling(t *testing.T) {
	f := conversationTest(t, true)
	budgetWaitQuestions(t, f, f.ask(t))
	// Observe the caller's stage deadline rather than the HTTP client's shorter
	// per-request timer. Production still retains its six-second HTTP timeout.
	f.r.Client.Timeout = 0
	var polls atomic.Int32
	scheduled, apiRequests := 0, 0
	var available time.Duration
	f.r.Run = func(ctx context.Context, _ string, args ...string) (string, error) {
		if len(args) != 0 && args[0] == "recovery-replies" {
			polls.Add(1)
			// Real per-question deadlines expire in two batches. No fixed sleep
			// or simulated success hides time consumed before the actual HTTP read.
			<-ctx.Done()
			return "", ctx.Err()
		}
		if len(args) != 0 && args[0] == "repair-step" {
			scheduled++
			return `{"repair_mode":true,"error":null}`, nil
		}
		return "", errors.New("unexpected command")
	}
	f.r.Client.Transport = tickBudgetTransport{onRequest: func(ctx context.Context) {
		apiRequests++
		deadline, _ := ctx.Deadline()
		available = time.Until(deadline)
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	h := f.r.tick(ctx)
	if polls.Load() != 8 || h.ConversationChecked != 8 || apiRequests != 1 {
		t.Fatal("the real tick did not reach all reply reads and then discovery", polls.Load(), apiRequests, h)
	}
	if available < 15*time.Second || available > 20*time.Second {
		t.Fatal("reply polling consumed the new-request HTTP budget", available)
	}
	if scheduled != 1 || !h.Scheduler {
		t.Fatal("the independent dispatcher lost its turn", scheduled, h)
	}
}

func TestTickReplyCancellationDoesNotDetachDiscoveryFromCaller(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	scheduled, detachedReads := 0, 0
	f.r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		if len(args) != 0 && args[0] == "recovery-replies" {
			cancel()
			return "", context.Canceled
		}
		if len(args) != 0 && args[0] == "repair-step" {
			scheduled++
		}
		return "", context.Canceled
	}
	f.r.Client.Transport = tickBudgetTransport{onRequest: func(requestCtx context.Context) {
		if requestCtx.Err() == nil {
			detachedReads++
		}
	}}
	h := f.r.tick(ctx)
	if ctx.Err() == nil || detachedReads != 0 || scheduled != 0 || h.Scheduler {
		t.Fatal("a fresh intake deadline escaped cancellation after reply polling", detachedReads, scheduled, h)
	}
}
