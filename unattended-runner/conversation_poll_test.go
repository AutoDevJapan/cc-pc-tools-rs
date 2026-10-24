package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestConversationEveryActiveWaitIsPolledOnEachMinuteTick(t *testing.T) {
	f := conversationTest(t, true)
	base := f.ask(t)
	index, err := f.r.loadConversations()
	if err != nil {
		t.Fatal(err)
	}
	delete(index.Questions, base.RequestID)
	questions := map[string]ConversationQuestion{}
	thread, _ := strconv.ParseUint(base.Thread, 10, 64)
	for i := 0; i < 23; i++ {
		q := base
		q.RequestID = fmt.Sprintf("question-%02d", i)
		q.SenderID = "sender-" + q.RequestID
		q.Thread = strconv.FormatUint(thread+uint64(i+1), 10)
		q.Message = q.Thread
		root := filepath.Join(f.r.Config.Home, "conversations", q.RequestID)
		q.BodyFile, q.DecisionFile, q.ReceiptFile = filepath.Join(root, "question.txt"), filepath.Join(root, "decision.json"), filepath.Join(root, "question.json")
		if i >= 8 {
			q.Phase = "closed"
		} else if i == 7 {
			q.Phase = "answered"
		}
		index.Questions[q.RequestID] = q
		questions[q.Thread] = q
	}
	index.Cursor = "question-07"
	if err := publish(f.r.conversationPath(), index); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	read := map[string]int{}
	f.r.Run = func(ctx context.Context, _ string, args ...string) (string, error) {
		if len(args) == 0 || args[0] != "recovery-replies" {
			return "", errors.New("poll must not POST, notify or change a task")
		}
		q := questions[argValue(args, "--thread")]
		mu.Lock()
		read[q.RequestID]++
		mu.Unlock()
		if q.RequestID == "question-00" {
			return "", errors.New("one unavailable question")
		}
		body, _ := json.Marshal(conversationProof{Domain: "new-owner-recovery-messages", ReadOnly: true, Source: "discord-api-v10", RequestID: q.SenderID, Thread: q.Thread, Owner: f.r.State.Destination.Owner, Question: q.Message, Progress: q.Progress, Created: f.created, Body: "aかbを選んでください。", Decision: f.decision})
		return string(body), nil
	}
	now := f.r.Now()
	for minute := 0; minute < 2; minute++ {
		stamp := now.Add(time.Duration(minute) * time.Minute)
		f.r.Now = func() time.Time { return stamp }
		h := Health{}
		f.r.processConversations(context.Background(), &h)
		if h.ConversationWaiting != 8 || h.ConversationChecked != 8 || h.ConversationPollDeferred != 1 || h.ConversationPollSeconds != 60 {
			t.Fatalf("minute tick missed an active wait: %+v", h)
		}
		snapshot, err := f.r.conversationPollSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 8; i++ {
			id := fmt.Sprintf("question-%02d", i)
			if read[id] != minute+1 || snapshot.Checks[id].CheckedAt != stamp.UTC().Format(time.RFC3339Nano) || snapshot.Checks[id].OK != (i != 0) {
				t.Fatal("history or another failed question delayed an active wait", id)
			}
		}
	}
}

func TestConversationCapturedReplyIncludesExactFullTextAndDoesNotRedeliver(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	text := "  回答の先頭\r\n" + strings.Repeat("全文を保持する🌿\r\n", 2000) + "末尾も保持  "
	reply := f.reply(t, 8, text)
	h := Health{}
	f.r.processConversations(context.Background(), &h)
	row, err := f.r.todo(42)
	if err != nil || len(h.Errors) != 0 || h.ConversationReplies != 1 || !strings.Contains(row.Note, "<discord-owner-answer>\n"+text+"\n</discord-owner-answer>") {
		t.Fatal("received text was omitted, escaped, trimmed or shortened", h.Errors)
	}
	before := row.Note
	f.r.processConversations(context.Background(), &Health{})
	row, _ = f.r.todo(42)
	if row.Note != before || f.r.State.Requests[reply.Message].Text != text {
		t.Fatal("a received version was duplicated or changed")
	}
}

func TestConversationAllAlreadyObservedRepliesAreDeliveredInOneTick(t *testing.T) {
	f := conversationTest(t, false)
	f.ask(t)
	a := f.reply(t, 8, "a\n最初の回答")
	b := f.reply(t, 9, "補足の全文\r\n省略しない")
	h := Health{}
	f.r.processConversations(context.Background(), &h)
	row, _ := f.r.todo(42)
	if len(h.Errors) != 0 || h.ConversationReplies != 2 || !strings.Contains(row.Note, a.Body) || !strings.Contains(row.Note, b.Body) {
		t.Fatal("a second observed reply was left behind for another round", h.Errors)
	}
}

func TestConversationPollConcurrencyDoesNotLetOneUnavailableQuestionBlockAnother(t *testing.T) {
	f := conversationTest(t, true)
	q := f.ask(t)
	index, err := f.r.loadConversations()
	if err != nil {
		t.Fatal(err)
	}
	second := q
	second.RequestID, second.SenderID = "later-wait", "later-sender"
	root := filepath.Join(f.r.Config.Home, "conversations", second.RequestID)
	second.BodyFile, second.DecisionFile, second.ReceiptFile = filepath.Join(root, "question.txt"), filepath.Join(root, "decision.json"), filepath.Join(root, "question.json")
	index.Questions[second.RequestID] = second
	if err := publish(f.r.conversationPath(), index); err != nil {
		t.Fatal(err)
	}
	firstWaiting, secondRead := make(chan struct{}), make(chan struct{})
	f.r.Run = func(ctx context.Context, _ string, args ...string) (string, error) {
		if argValue(args, "--request-id") == q.SenderID {
			close(firstWaiting)
			select {
			case <-secondRead:
			case <-ctx.Done():
				return "", ctx.Err()
			}
		} else {
			select {
			case <-firstWaiting:
			case <-ctx.Done():
				return "", ctx.Err()
			}
			close(secondRead)
		}
		return "", errors.New("both reads fail without making changes")
	}
	h := Health{}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f.r.processConversations(ctx, &h)
	select {
	case <-secondRead:
	default:
		t.Fatal("a waiting API blocked the other question's minute read")
	}
	if h.ConversationChecked != 2 || h.ConversationPollDeferred != 2 {
		t.Fatal("not all attempted reads were accounted for", h)
	}
}

func TestConversationPeriodicBudgetLeavesTheNextMinuteStartAvailable(t *testing.T) {
	if periodicTickBudget >= conversationPollInterval || periodicTickBudget <= periodicNormalBudget+periodicAgentResultBudget {
		t.Fatal("the existing IgnoreNew task would skip a minute or starve its bounded queue")
	}
	if periodicTickBudget-periodicNormalBudget-periodicAgentResultBudget < civ6QueueMinRemaining || conversationPollStageBudget > periodicNormalBudget || conversationPollStageBudget < conversationPollTimeout {
		t.Fatal("periodic phases have no usable bounded time")
	}
}

type conversationReadCounter struct {
	base  http.RoundTripper
	reads map[string]int
}

func (c *conversationReadCounter) RoundTrip(req *http.Request) (*http.Response, error) {
	c.reads[req.URL.Path]++
	return c.base.RoundTrip(req)
}

func TestConversationNewReplyIsDeliveredBeforeAnAlreadyDeliveredHistory(t *testing.T) {
	f := conversationTest(t, false)
	f.ask(t)
	at := f.r.Now().Add(5 * time.Minute)
	f.r.Now = func() time.Time { return at }
	var old []conversationReply
	for i := 0; i < 100; i++ {
		reply := f.reply(t, i+3, fmt.Sprintf("過去の回答-%d", i))
		old = append(old, reply)
		f.r.State.Requests[reply.Message] = &Request{Thread: f.question, Guild: "100", RecordVersion: 2, Message: reply.Message, Author: "300", Created: reply.Created, Text: reply.Body, Key: requestKey(f.question, reply.Message), Conversation: f.q.RequestID, Todo: 42, Phase: "conversation_reply_delivered"}
	}
	latest := f.reply(t, 110, "この新しい回答をすぐ届ける")
	reads := &conversationReadCounter{base: http.DefaultTransport, reads: map[string]int{}}
	f.r.Client.Transport = reads
	h := Health{}
	f.r.processConversations(context.Background(), &h)
	if f.r.State.Requests[latest.Message] == nil || f.r.State.Requests[latest.Message].Phase != "conversation_reply_delivered" {
		t.Fatal("old delivered text starved a newly observed answer", h.Errors)
	}
	for _, reply := range old {
		if reads.reads["/channels/"+f.question+"/messages/"+reply.Message] != 0 {
			t.Fatal("a delivered history was re-read before the new answer")
		}
	}
}

func TestConversationFailedDeliveryDoesNotAdvanceItsReplyCursor(t *testing.T) {
	f := conversationTest(t, false)
	f.ask(t)
	a, b := f.reply(t, 8, "先に配送すべき回答"), f.reply(t, 9, "後から届いた補足")
	run := f.r.Run
	failed := false
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "edit" && !failed {
			failed = true
			return "", errors.New("temporary task lock")
		}
		return run(ctx, path, args...)
	}
	f.r.processConversations(context.Background(), &Health{})
	index, err := f.r.loadConversations()
	if err != nil || index.Questions[f.q.RequestID].ReplyCursor != "" {
		t.Fatal("failed answer was skipped by a persisted cursor")
	}
	h := Health{}
	f.r.processConversations(context.Background(), &h)
	for _, reply := range []conversationReply{a, b} {
		if f.r.State.Requests[reply.Message] == nil || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
			t.Fatal("delivery retry lost a reply", h.Errors)
		}
	}
}

func TestConversationOlderSenderStillDetectsATextPreservingEdit(t *testing.T) {
	f := conversationTest(t, false)
	f.ask(t)
	reply := f.reply(t, 8, "同じ本文の編集版")
	f.r.processConversations(context.Background(), &Health{})
	edited := f.r.Now().UTC().Format(time.RFC3339Nano)
	f.edited = map[string]string{reply.Message: edited}
	run := f.r.Run
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		raw, err := run(ctx, path, args...)
		if err != nil || len(args) == 0 || args[0] != "recovery-replies" {
			return raw, err
		}
		var proof map[string]json.RawMessage
		if json.Unmarshal([]byte(raw), &proof) != nil {
			t.Fatal("fixture proof malformed")
		}
		delete(proof, "reply_versions")
		body, _ := json.Marshal(proof)
		return string(body), nil
	}
	h := Health{}
	f.r.processConversations(context.Background(), &h)
	if len(h.Errors) != 0 || f.r.State.Requests[reply.Message].Edited != edited || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
		t.Fatal("missing sender edit metadata hid the new version", h.Errors)
	}
}
