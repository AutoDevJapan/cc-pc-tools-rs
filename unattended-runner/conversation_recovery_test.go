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

func TestConversationEditedSourceWithoutThisVersionAttemptStillDelivers(t *testing.T) {
	for _, wait := range []bool{false, true} {
		t.Run(fmt.Sprint(wait), func(t *testing.T) {
			f := conversationTest(t, wait)
			f.ask(t)
			reply := f.reply(t, 8, "a")
			health := Health{}
			f.r.processConversations(context.Background(), &health)
			if len(health.Errors) != 0 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
				t.Fatal("first version did not deliver")
			}
			// The prior delivery's input survives, but this edit has only been
			// captured. No edit or question-answer for this version has run.
			f.replies[0].Body = "b"
			f.edited = map[string]string{reply.Message: f.r.Now().Format(time.RFC3339Nano)}
			proof, err := f.r.conversationProof(context.Background(), f.q)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = f.r.captureConversationReply(context.Background(), f.q, proof, f.replies[0]); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+".json")
			before, _ := os.ReadFile(path)
			delete(f.r.State.Requests, reply.Message)
			health = Health{}
			f.r.processConversations(context.Background(), &health)
			row, err := f.r.todo(42)
			after, _ := os.ReadFile(path)
			if err != nil || len(health.Errors) != 0 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" || string(before) != string(after) || strings.Count(row.Note, "conversation-owner-reply:") != 2 || !strings.Contains(row.Note, canonicalHash([]string{"b", f.edited[reply.Message]})[:16]) {
				t.Fatalf("old version's input prevented delivery of an unattempted edit: %v", health.Errors)
			}
		})
	}
}

func TestConversationReacquiredRecordedAnswerRetainsUnresolvedSource(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
		t.Fatal("first answer did not deliver")
	}
	path := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+".json")
	var source Request
	if readJSON(path, maxTodo, &source) != nil {
		t.Fatal("original source unavailable")
	}
	// A legacy recapture replaced the source acquisition while the original
	// canonical answer and ready input survived. Delivery is already proven;
	// application/closing for the different source SHA is still unverified.
	now := f.r.Now().Add(time.Second)
	f.r.Now = func() time.Time { return now }
	source.Acquired = now.Format(time.RFC3339Nano)
	if publish(path, &source) != nil {
		t.Fatal("legacy overwritten-source fixture unavailable")
	}
	currentSource, _ := os.ReadFile(path)
	before, _ := os.ReadFile(f.r.TodoPath)
	for i := 0; i < 2; i++ {
		delete(f.r.State.Requests, reply.Message)
		health = Health{}
		f.r.processConversations(context.Background(), &health)
		after, _ := os.ReadFile(f.r.TodoPath)
		req := f.r.State.Requests[reply.Message]
		if len(health.Errors) != 0 || req.Phase != "conversation_reply_delivered" || string(before) != string(after) || !strings.Contains(req.ConversationRecoveryNote, "source_receipt_sha256="+bytesHash(currentSource)) {
			t.Fatal("recorded-answer recovery lost the unresolved current source or changed the ToDo")
		}
		stateBytes, _ := json.Marshal(f.r.State)
		var restarted State
		if json.Unmarshal(stateBytes, &restarted) != nil {
			t.Fatal("state restart fixture unavailable")
		}
		f.r.State = &restarted
	}
}

func TestConversationScanPendingRecoveryWithoutNativeBinary(t *testing.T) {
	for _, wait := range []bool{false, true} {
		for _, replaced := range []bool{false, true} {
			t.Run(fmt.Sprintf("wait=%t/replaced=%t", wait, replaced), func(t *testing.T) {
				f := conversationTest(t, wait)
				f.ask(t)
				reply := f.reply(t, 8, "a")
				health := Health{}
				f.r.processConversations(context.Background(), &health)
				if len(health.Errors) != 0 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
					t.Fatal("first delivery unavailable")
				}
				path := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+".json")
				source, _ := os.ReadFile(path)
				f.r.State.Requests[reply.Message] = &Request{Thread: reply.Thread, Author: reply.Author, Message: reply.Message, Conversation: f.q.RequestID, Phase: "conversation_capture_pending"}
				if replaced {
					f.mutateTodo(t, func(row map[string]any) {
						row["status"], row["note"], row["owner_stopped"] = "block", "新しい直接停止", true
					})
				}
				before, _ := os.ReadFile(f.r.TodoPath)
				now := f.r.Now().Add(time.Second)
				f.r.Now = func() time.Time { return now }
				health = Health{}
				f.r.processConversations(context.Background(), &health)
				after, _ := os.ReadFile(f.r.TodoPath)
				currentSource, _ := os.ReadFile(path)
				if string(source) != string(currentSource) || string(before) != string(after) {
					t.Fatalf("scan-pending recovery rewrote the original source or current task: %v", health.Errors)
				}
				if replaced && !wait {
					if len(health.Errors) == 0 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_saved" {
						t.Fatal("unproven ordinary delivery was acknowledged after a newer stop")
					}
				} else if len(health.Errors) != 0 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
					t.Fatalf("unchanged source could not recover its delivery: %v", health.Errors)
				}
			})
		}
	}
}
