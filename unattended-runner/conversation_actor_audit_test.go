package main

// Formal synthetic regressions from integration review ToDo4009.
//
// A Wait=false question bound to the finite resumed recipient (1171 -> 3277)
// must keep main's no-op for an already completed reply when the existing
// maintenance round-robin audits that reply again.

import (
	"context"
	"strings"
	"testing"
)

func repro4009Delivered(t *testing.T) (*actorFixture, ConversationQuestion, conversationReply) {
	t.Helper()
	f := actorTest(t, false)
	f.input.Wait = false
	q := f.ask(t)
	if q.Actor == nil || q.Wait || q.Phase != "posted" {
		t.Fatalf("setup question: actor=%v wait=%v phase=%s", q.Actor != nil, q.Wait, q.Phase)
	}
	reply := f.reply(t, 8, "a：元の条件で進めてください。")
	f.replies[0].Todo = 1171
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	req := f.r.conversationRequest(reply.Message)
	actor, _ := f.r.todo(3277)
	if len(health.Errors) != 0 || req == nil || req.Phase != "conversation_reply_delivered" || req.Todo != 3277 ||
		strings.Count(actor.Note, "<discord-owner-answer>") != 1 || strings.Count(actor.conversationInstruction(), "<discord-owner-answer>") != 1 {
		t.Fatalf("setup delivery: errors=%v request=%+v", health.Errors, req)
	}
	index, err := f.r.loadConversations()
	if err != nil {
		t.Fatal(err)
	}
	return f, index.Questions[q.RequestID], reply
}

func TestConversationCompletedActorNonwaitingAudit(t *testing.T) {
	t.Run("applied_phase_is_not_rewritten", func(t *testing.T) {
		f, q, reply := repro4009Delivered(t)
		req := f.r.conversationRequest(reply.Message)
		req.Phase = "conversation_reply_applied"
		if err := f.r.save(); err != nil {
			t.Fatal(err)
		}
		if err := f.r.deliverConversationReply(context.Background(), q, reply, req); err != nil {
			t.Fatalf("completed reply audit failed: %v", err)
		}
		if got := f.r.conversationRequest(reply.Message).Phase; got != "conversation_reply_applied" {
			t.Fatalf("completed reply phase rewritten to %s", got)
		}
	})
	t.Run("finished_recipient_audit_has_no_delivery_error", func(t *testing.T) {
		f, _, _ := repro4009Delivered(t)
		f.mutate(t, 3277, func(row map[string]any) { row["status"] = "done" })
		health := Health{}
		f.r.NoticeAttempted = false
		f.r.processConversations(context.Background(), &health)
		for _, e := range health.Errors {
			if e == "conversation_delivery_deferred" {
				t.Fatalf("finished recipient made the completed reply audit fail: %v", health.Errors)
			}
		}
	})
	t.Run("consumed_instruction_with_kept_note_has_no_delivery_error", func(t *testing.T) {
		f, _, _ := repro4009Delivered(t)
		f.mutate(t, 3277, func(row map[string]any) { row["scheduler"].(map[string]any)["owner_instruction"] = "" })
		health := Health{}
		f.r.NoticeAttempted = false
		f.r.processConversations(context.Background(), &health)
		for _, e := range health.Errors {
			if e == "conversation_delivery_deferred" {
				t.Fatalf("consumed instruction made the completed reply audit fail: %v", health.Errors)
			}
		}
	})
	t.Run("rewritten_note_is_not_redelivered", func(t *testing.T) {
		f, _, _ := repro4009Delivered(t)
		f.mutate(t, 3277, func(row map[string]any) {
			row["note"] = "受信担当が整理したメモ"
			row["scheduler"].(map[string]any)["owner_instruction"] = ""
		})
		f.r.NoticeAttempted = false
		f.r.processConversations(context.Background(), &Health{})
		actor, _ := f.r.todo(3277)
		if strings.Contains(actor.Note, "<discord-owner-answer>") || strings.Contains(actor.conversationInstruction(), "<discord-owner-answer>") {
			t.Fatal("an already delivered answer was appended to the recipient again")
		}
	})
}
