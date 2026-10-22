package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func continuedNative(t *testing.T) *conversationFixture {
	t.Helper()
	f := conversationNativeTest(t)
	f.mutateTodo(t, func(row map[string]any) { row["resume_generation"] = 1 })
	f.ask(t)
	f.mutateTodo(t, func(row map[string]any) {
		row["status"] = "doing"
		row["resume_generation"] = 2
		row["worker_receipt"] = map[string]any{"job": nil, "generation": 2}
		row["note"] = "新しい直接指示を優先。元の全依頼と途中成果を保持する。"
	})
	return f
}

func importedNative(t *testing.T) *conversationFixture {
	t.Helper()
	w := withdrawalTest(t, true)
	w.allowAnswer = true
	w.f.mutateTodo(t, func(row map[string]any) {
		delete(row["owner_question_wait"].(map[string]any), "origin")
		row["worker_receipt"] = map[string]any{"job": nil, "generation": 7}
		row["note"] = "ジョブの結果に判断が必要です。"
		row["scheduler"].(map[string]any)["phase"] = "finished"
		row["processed_results"] = map[string]any{"worker-42": map[string]any{"generation": 7, "reason": "block"}}
	})
	return w.f
}

func TestConversationActualCanonicalLargeHistoryDoesNotPreventDelivery(t *testing.T) {
	for _, kind := range []string{"normal", "imported", "continued"} {
		t.Run(kind, func(t *testing.T) {
			var f *conversationFixture
			switch kind {
			case "normal":
				f = conversationNativeTest(t)
				f.ask(t)
			case "imported":
				f = importedNative(t)
			case "continued":
				f = continuedNative(t)
			}
			history := []any{map[string]any{"note": strings.Repeat("saved history ", 90_000)}}
			f.mutateTodo(t, func(row map[string]any) { row["note_history"] = history })
			row, err := f.r.conversationTask(context.Background(), 42)
			if err != nil || len(row.conversationState) <= 1<<20 || len(row.conversationState) >= maxTodo {
				t.Fatal("did not exercise a row larger than the former snapshot ceiling")
			}
			reply := f.reply(t, 8, "a")
			health := Health{}
			f.r.processConversations(context.Background(), &health)
			if len(health.Errors) != 0 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
				t.Fatalf("large history prevented %s delivery: %v", kind, health.Errors)
			}
			var db map[string]any
			if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
				t.Fatal("readback unavailable")
			}
			saved := db["items"].([]any)[0].(map[string]any)["note_history"].([]any)
			if len(saved) < 1 || canonicalHash(saved[0]) != canonicalHash(history[0]) {
				t.Fatal("delivery lost the original large history")
			}
		})
	}
}

func TestConversationActualCanonicalAnswerReplayNeverRestoresReplacedNote(t *testing.T) {
	for _, kind := range []string{"normal", "imported"} {
		t.Run(kind, func(t *testing.T) {
			var f *conversationFixture
			if kind == "imported" {
				f = importedNative(t)
			} else {
				f = conversationNativeTest(t)
				f.ask(t)
			}
			reply := f.reply(t, 8, "a")
			backend, answers := f.r.Run, 0
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				out, err := backend(ctx, path, args...)
				if path == conversationTodo && args[0] == "question-answer" && err == nil {
					answers++
					return "", errors.New("answer committed but response lost")
				}
				return out, err
			}
			health := Health{}
			f.r.processConversations(context.Background(), &health)
			if answers != 1 || len(health.Errors) == 0 || f.r.State.Requests[reply.Message].Todo != 0 {
				t.Fatal("did not exercise a committed answer with an uncertain response")
			}
			f.mutateTodo(t, func(row map[string]any) {
				row["status"] = "block"
				row["note"] = "ジョブの結果に判断が必要です。"
				row["owner_stopped"] = true
				row["owner_question_wait"] = map[string]any{"newer_question": true}
			})
			before, _ := os.ReadFile(f.r.TodoPath)
			health = Health{}
			f.r.processConversations(context.Background(), &health)
			after, _ := os.ReadFile(f.r.TodoPath)
			if len(health.Errors) != 0 || string(before) != string(after) || answers != 1 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
				t.Fatalf("%s replay altered a later hold or restored an old reference: %v", kind, health.Errors)
			}
		})
	}
}

func TestConversationActualCanonicalRecaptureDoesNotWriteARecoveryProbe(t *testing.T) {
	f := conversationNativeTest(t)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
		t.Fatal("initial canonical delivery unavailable")
	}
	before, _ := os.ReadFile(f.r.TodoPath)
	// Simulate a lost intake index entry, while its canonical receipt and the
	// immutable ready file survived. A new acquisition has different bytes.
	ready, _ := filepath.Glob(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+"-*.ready.json"))
	if len(ready) != 1 {
		t.Fatal("immutable delivery input absent")
	}
	readyBefore, _ := os.ReadFile(ready[0])
	delete(f.r.State.Requests, reply.Message)
	now := time.Now().Add(time.Second)
	f.r.Now = func() time.Time { return now }
	health = Health{}
	f.r.processConversations(context.Background(), &health)
	after, _ := os.ReadFile(f.r.TodoPath)
	readyAfter, _ := os.ReadFile(ready[0])
	if len(health.Errors) != 0 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" || string(before) != string(after) || string(readyBefore) != string(readyAfter) {
		t.Fatalf("read-only recovery probe conflicted with an earlier acquisition: %v", health.Errors)
	}
}

func TestConversationActualCanonicalRecaptureKeepsReplacedNotesAndNewHolds(t *testing.T) {
	for _, kind := range []string{"normal", "imported", "continued"} {
		t.Run(kind, func(t *testing.T) {
			var f *conversationFixture
			switch kind {
			case "normal":
				f = conversationNativeTest(t)
				f.ask(t)
			case "imported":
				f = importedNative(t)
			case "continued":
				f = continuedNative(t)
			}
			reply := f.reply(t, 8, "a")
			health := Health{}
			f.r.processConversations(context.Background(), &health)
			if len(health.Errors) != 0 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
				t.Fatal("initial delivery unavailable")
			}
			delete(f.r.State.Requests, reply.Message)
			now := time.Now().Add(time.Second)
			f.r.Now = func() time.Time { return now }
			f.mutateTodo(t, func(row map[string]any) {
				row["status"] = "block"
				row["owner_stopped"] = true
				row["note"] = "ジョブの結果に判断が必要です。"
				row["owner_question_wait"] = map[string]any{"newer_question": true}
				row["scheduler"] = map[string]any{"owner_instruction": "新しい担当への指示"}
			})
			before, _ := os.ReadFile(f.r.TodoPath)
			health = Health{}
			f.r.processConversations(context.Background(), &health)
			after, _ := os.ReadFile(f.r.TodoPath)
			if len(health.Errors) != 0 || string(before) != string(after) || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
				t.Fatalf("%s recapture rewrote a newer task or lost a committed delivery: %v", kind, health.Errors)
			}
		})
	}
}

func TestConversationActualCanonicalRecapturedSecondReplyWithoutProofStaysPending(t *testing.T) {
	f := conversationNativeTest(t)
	f.ask(t)
	f.reply(t, 8, "a")
	f.r.processConversations(context.Background(), &Health{})
	reply := f.reply(t, 9, "元の範囲で続けて")
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
		t.Fatal("second reply was not delivered")
	}
	delete(f.r.State.Requests, reply.Message)
	now := time.Now().Add(time.Second)
	f.r.Now = func() time.Time { return now }
	f.mutateTodo(t, func(row map[string]any) {
		row["status"] = "block"
		row["owner_stopped"] = true
		row["note"] = "新しい直接停止"
	})
	before, _ := os.ReadFile(f.r.TodoPath)
	health = Health{}
	// The existing per-reply cursor visits the first reply before this one.
	for i := 0; i < 2; i++ {
		f.r.processConversations(context.Background(), &health)
	}
	after, _ := os.ReadFile(f.r.TodoPath)
	if string(before) != string(after) || len(health.Errors) == 0 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_saved" {
		t.Fatal("an unproven second reply was rewritten or acknowledged after intake loss")
	}
	// A normal saved state/restart must retain the missing-delivery-proof bound.
	stateBytes, _ := json.Marshal(f.r.State)
	var restarted State
	if json.Unmarshal(stateBytes, &restarted) != nil {
		t.Fatal("restart fixture unavailable")
	}
	f.r.State = &restarted
	for i := 0; i < 2; i++ {
		f.r.processConversations(context.Background(), &Health{})
	}
	after, _ = os.ReadFile(f.r.TodoPath)
	if string(before) != string(after) || f.r.State.Requests[reply.Message].Phase != "conversation_reply_saved" {
		t.Fatal("restart lost the unproven source-recovery boundary")
	}
}

func TestConversationActualCanonicalFutureEditDoesNotReplaceTheOriginalSource(t *testing.T) {
	f := conversationNativeTest(t)
	base := time.Now()
	now := base.Add(-5 * time.Second)
	f.r.Now = func() time.Time { return now }
	f.ask(t)
	reply := f.reply(t, 8, "a")
	f.r.processConversations(context.Background(), &Health{})
	path := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+".json")
	before, _ := os.ReadFile(path)
	now = base.Add(-3 * time.Second)
	edited := base.Add(-2 * time.Second)
	f.edited = map[string]string{reply.Message: edited.UTC().Format(time.RFC3339Nano)}
	f.r.processConversations(context.Background(), &Health{})
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("an edit ahead of the local clock overwrote the original acquisition")
	}
	now = base.Add(-time.Second)
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	var saved Request
	if readJSON(path, 1<<20, &saved) != nil || len(health.Errors) != 0 || saved.Edited != edited.UTC().Format(time.RFC3339Nano) || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
		t.Fatal("clock catch-up never admitted the current edit")
	}
}

func TestConversationActualCanonicalRecaptureRejectsDifferentSavedInput(t *testing.T) {
	for _, change := range []string{"question", "unknown-field", "acquired", "receipt-sha"} {
		t.Run(change, func(t *testing.T) {
			f := conversationNativeTest(t)
			f.ask(t)
			reply := f.reply(t, 8, "a")
			f.r.processConversations(context.Background(), &Health{})
			ready, _ := filepath.Glob(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+"-*.ready.json"))
			if len(ready) != 1 {
				t.Fatal("delivery input absent")
			}
			var input conversationAnswerReceipt
			if readJSON(ready[0], 1<<20, &input) != nil {
				t.Fatal("delivery input unavailable")
			}
			switch change {
			case "question":
				input.Question = "999"
			case "acquired":
				input.Acquired = "invalid"
			case "receipt-sha":
				input.ReceiptSHA = "invalid"
			}
			invalid, _ := json.MarshalIndent(input, "", "  ")
			if change == "unknown-field" {
				invalid = []byte(strings.TrimSuffix(string(invalid), "\n}") + ",\n  \"extra\": \"unrecognized\"\n}")
			}
			if os.WriteFile(ready[0], invalid, 0600) != nil {
				t.Fatal("invalid fixture not saved")
			}
			delete(f.r.State.Requests, reply.Message)
			f.mutateTodo(t, func(row map[string]any) { row["note"] = "新しい停止"; row["owner_stopped"] = true })
			before, _ := os.ReadFile(f.r.TodoPath)
			health := Health{}
			f.r.processConversations(context.Background(), &health)
			after, _ := os.ReadFile(f.r.TodoPath)
			if len(health.Errors) == 0 || string(before) != string(after) || f.r.State.Requests[reply.Message].Phase != "conversation_reply_saved" {
				t.Fatal("different immutable input was trusted or changed a later task")
			}
		})
	}
}

func TestConversationActualCanonicalRecaptureCompletesOnlyTheOriginalPartialAnswer(t *testing.T) {
	for _, stopped := range []bool{false, true} {
		t.Run(fmt.Sprint(stopped), func(t *testing.T) {
			f := conversationNativeTest(t)
			f.ask(t)
			reply := f.reply(t, 8, "a")
			backend := f.r.Run
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if path == conversationTodo && args[0] == "question-answer" {
					return "", errors.New("interrupted after note edit, before answer commit")
				}
				return backend(ctx, path, args...)
			}
			f.r.processConversations(context.Background(), &Health{})
			row, _ := f.r.todo(42)
			if len(row.QuestionAnswers) != 0 || !strings.Contains(row.Note, "conversation-owner-reply:") {
				t.Fatal("did not exercise partial delivery")
			}
			delete(f.r.State.Requests, reply.Message)
			// Reproduce the prior recapture which published a new acquisition.
			path := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+".json")
			if os.Remove(path) != nil {
				t.Fatal("isolated lost-source fixture unavailable")
			}
			f.r.Run = backend
			now := time.Now().Add(time.Second)
			f.r.Now = func() time.Time { return now }
			if stopped {
				f.mutateTodo(t, func(row map[string]any) { row["owner_stopped"] = true })
			}
			before, _ := os.ReadFile(f.r.TodoPath)
			health := Health{}
			f.r.processConversations(context.Background(), &health)
			row, _ = f.r.todo(42)
			if stopped {
				after, _ := os.ReadFile(f.r.TodoPath)
				if len(health.Errors) == 0 || string(before) != string(after) || f.r.State.Requests[reply.Message].Phase != "conversation_reply_saved" {
					t.Fatal("partial note acknowledged delivery or released a newer stop")
				}
			} else {
				source, _ := os.ReadFile(path)
				if len(health.Errors) != 0 || len(row.QuestionAnswers) != 1 || row.Status != "todo" || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" || !strings.Contains(f.r.State.Requests[reply.Message].ConversationRecoveryNote, "source_receipt_sha256="+bytesHash(source)) {
					t.Fatal("intact original partial answer lacks its release or unresolved-source record")
				}
			}
		})
	}
}

func TestConversationActualCanonicalFutureReplyWaitsBeforeCreatingItsReceipt(t *testing.T) {
	f := conversationNativeTest(t)
	base := time.Now()
	now := base.Add(-5 * time.Second)
	f.r.Now = func() time.Time { return now }
	f.ask(t)
	reply := f.reply(t, 8, "a")
	created := base.Add(-2 * time.Second)
	now = created.Add(-time.Second)
	reply.Created, reply.Message = created.Format(time.RFC3339Nano), snowflake(created)
	f.replies[0] = reply
	f.r.Now = func() time.Time { return now }
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	path := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+".json")
	if _, err := os.Stat(path); !os.IsNotExist(err) || f.r.State.Requests[reply.Message] != nil || len(health.Errors) == 0 {
		t.Fatal("future API post created a permanently invalid acquisition")
	}
	now = created.Add(time.Second)
	health = Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
		t.Fatal("clock catch-up did not deliver the unchanged reply")
	}
}

func TestConversationActualCanonicalRecapturePreservesTheOriginalSourceBytes(t *testing.T) {
	f := conversationNativeTest(t)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	f.r.processConversations(context.Background(), &Health{})
	path := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+".json")
	before, _ := os.ReadFile(path)
	delete(f.r.State.Requests, reply.Message)
	now := time.Now().Add(time.Second)
	f.r.Now = func() time.Time { return now }
	f.r.processConversations(context.Background(), &Health{})
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("intake index loss overwrote the original per-reply application source")
	}
}

func TestConversationActualCanonicalScanPendingRecoveryKeepsSourceAndNewerTask(t *testing.T) {
	for _, kind := range []string{"normal", "imported", "continued"} {
		t.Run(kind, func(t *testing.T) {
			var f *conversationFixture
			switch kind {
			case "normal":
				f = conversationNativeTest(t)
				f.ask(t)
			case "imported":
				f = importedNative(t)
			case "continued":
				f = continuedNative(t)
			}
			reply := f.reply(t, 8, "a")
			f.r.processConversations(context.Background(), &Health{})
			path := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+".json")
			source, _ := os.ReadFile(path)
			f.r.State.Requests[reply.Message] = &Request{Thread: reply.Thread, Author: reply.Author, Message: reply.Message, Conversation: f.q.RequestID, Phase: "conversation_capture_pending"}
			f.mutateTodo(t, func(row map[string]any) {
				row["status"] = "block"
				row["owner_stopped"] = true
				row["note"] = "新しい直接停止"
			})
			before, _ := os.ReadFile(f.r.TodoPath)
			health := Health{}
			f.r.processConversations(context.Background(), &health)
			after, _ := os.ReadFile(f.r.TodoPath)
			currentSource, _ := os.ReadFile(path)
			if len(health.Errors) != 0 || string(source) != string(currentSource) || string(before) != string(after) || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
				t.Fatalf("scan-pending %s bypassed original-source recovery: %v", kind, health.Errors)
			}
		})
	}
}

func TestConversationUnattemptedRecoveredSourceCanStillBeDelivered(t *testing.T) {
	f := conversationTest(t, false)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	proof, err := f.r.conversationProof(context.Background(), f.q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.r.captureConversationReply(context.Background(), f.q, proof, reply); err != nil {
		t.Fatal(err)
	}
	delete(f.r.State.Requests, reply.Message)
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
		t.Fatal("source saved before the first delivery was permanently held")
	}
}

func TestConversationActualCanonicalContinuedDeliveryPreservesOriginalTask(t *testing.T) {
	f := continuedNative(t)
	reply := f.reply(t, 8, "a")
	before, err := os.ReadFile(f.r.TodoPath)
	if err != nil {
		t.Fatal(err)
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 {
		t.Fatalf("continued owner reply not delivered: %v", health.Errors)
	}
	row, err := f.r.todo(42)
	if err != nil || row.Status != "doing" || !strings.HasPrefix(row.Note, "新しい直接指示を優先。") || !strings.Contains(row.Note, "conversation-owner-reply:") || !conversationWaitRecorded(row, f.q) || len(row.QuestionAnswers) != 0 {
		t.Fatal("continued delivery released a hold or lost the original instructions")
	}
	if req := f.r.State.Requests[reply.Message]; req == nil || req.Todo != 42 || req.Phase != "conversation_reply_delivered" {
		t.Fatal("continued delivery not linked to the original task")
	}
	var original, after map[string]any
	if json.Unmarshal(before, &original) != nil || readJSON(f.r.TodoPath, maxTodo, &after) != nil {
		t.Fatal("readback unavailable")
	}
	afterRow := after["items"].([]any)[0].(map[string]any)
	if len(afterRow["owner_question_delivery_receipts"].([]any)) != 1 {
		t.Fatal("durable delivery receipt absent")
	}
	for _, key := range []string{"note", "updated", "owner_question_delivery_receipts"} {
		delete(original["items"].([]any)[0].(map[string]any), key)
		delete(afterRow, key)
	}
	instruction, ok := afterRow["scheduler"].(map[string]any)["owner_instruction"].(string)
	if !ok || !strings.Contains(instruction, "conversation-owner-reply:") {
		t.Fatal("the next worker has no persistent answer reference")
	}
	delete(afterRow, "scheduler") // continuedNative starts without a scheduler object.
	if canonicalHash(original) != canonicalHash(after) {
		t.Fatal("delivery changed original task/owner/holds/work")
	}
	if f.posts != 1 || f.close != 0 {
		t.Fatal("delivery resent a question or invented implementation")
	}
}

func TestConversationActualCanonicalContinuedDeliverySurvivesNoteReplacementAndFutureDelay(t *testing.T) {
	f := continuedNative(t)
	f.mutateTodo(t, func(row map[string]any) {
		row["scheduler"] = map[string]any{"owner_instruction": "現在の直接指示", "not_before": float64(10_000_000_000)}
	})
	f.reply(t, 8, "a")
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 {
		t.Fatalf("time-only hold prevented delivery: %v", health.Errors)
	}
	f.mutateTodo(t, func(row map[string]any) { row["note"] = "通常の担当終了後のメモ" })
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("missing readback")
	}
	row := db["items"].([]any)[0].(map[string]any)
	scheduler := row["scheduler"].(map[string]any)
	if !strings.HasPrefix(scheduler["owner_instruction"].(string), "現在の直接指示\n\n") || !strings.Contains(scheduler["owner_instruction"].(string), "conversation-owner-reply:") || scheduler["not_before"] != float64(10_000_000_000) {
		t.Fatal("persistent instructions or original time hold changed")
	}
	before, _ := os.ReadFile(f.r.TodoPath)
	f.r.processConversations(context.Background(), &Health{})
	after, _ := os.ReadFile(f.r.TodoPath)
	if string(before) != string(after) || f.close != 0 || f.posts != 1 {
		t.Fatal("delivered answer was lost, duplicated or treated as implemented")
	}
}

func TestConversationActualCanonicalDifferentGenerationBlockIsRejectedBeforeNoteEdit(t *testing.T) {
	f := conversationNativeTest(t)
	f.mutateTodo(t, func(row map[string]any) { row["resume_generation"] = 1 })
	f.ask(t)
	f.mutateTodo(t, func(row map[string]any) { row["resume_generation"] = 2 })
	f.reply(t, 8, "a")
	before, _ := os.ReadFile(f.r.TodoPath)
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	after, _ := os.ReadFile(f.r.TodoPath)
	if len(health.Errors) == 0 || string(before) != string(after) {
		t.Fatal("a later blocked generation was edited before canonical refusal")
	}
}

func TestConversationUnrelatedGenerationShapeDoesNotBreakOriginalTaskRead(t *testing.T) {
	f := conversationTest(t, false)
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("missing fixture")
	}
	db["items"] = append(db["items"].([]any), map[string]any{"id": 99, "project": "cc-pc-tools-rs", "status": "doing", "resume_generation": "unverified"})
	if err := publish(f.r.TodoPath, db); err != nil {
		t.Fatal(err)
	}
	if row, err := f.r.todo(42); err != nil || row.ID != 42 {
		t.Fatal("unrelated malformed generation prevented original task read")
	}
}

func TestConversationActualCanonicalAllDeliveryPathsKeepGuardedProjectAndRoot(t *testing.T) {
	for _, pathKind := range []string{"normal", "imported", "continued"} {
		for _, change := range []string{"project", "root"} {
			t.Run(pathKind+"/"+change, func(t *testing.T) {
				var f *conversationFixture
				if pathKind == "imported" {
					w := withdrawalTest(t, true)
					w.allowAnswer = true
					f = w.f
					f.mutateTodo(t, func(row map[string]any) {
						row["note"] = "ジョブの結果に判断が必要です。"
						row["scheduler"].(map[string]any)["phase"] = "finished"
						row["processed_results"] = map[string]any{"worker-42": map[string]any{"generation": 7, "reason": "block"}}
					})
				} else if pathKind == "continued" {
					f = continuedNative(t)
				} else {
					f = conversationNativeTest(t)
					f.ask(t)
				}
				f.r.NoticeAttempted = true
				f.reply(t, 8, "a")
				backend := f.r.Run
				var changed []byte
				f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
					if path == conversationTodo && (args[0] == "edit" || args[0] == "question-answer") && changed == nil {
						var db map[string]any
						if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
							t.Fatal("missing store")
						}
						if change == "project" {
							db["projects"].(map[string]any)["other-project"] = f.r.Config.Home
							db["items"].([]any)[0].(map[string]any)["project"] = "other-project"
						} else {
							db["projects"].(map[string]any)["cc-pc-tools-rs"] = filepath.Join(f.r.Config.Home, "other-root")
						}
						if err := publish(f.r.TodoPath, db); err != nil {
							t.Fatal(err)
						}
						changed, _ = os.ReadFile(f.r.TodoPath)
					}
					return backend(ctx, path, args...)
				}
				health := Health{}
				f.r.processConversations(context.Background(), &health)
				after, _ := os.ReadFile(f.r.TodoPath)
				if changed == nil || len(health.Errors) == 0 || string(changed) != string(after) {
					t.Fatal("guarded project/root change was mutated before refusal")
				}
			})
		}
	}
}

func TestConversationActualCanonicalWaitAndWithdrawalKeepGuardedRoot(t *testing.T) {
	for _, kind := range []string{"wait", "withdraw"} {
		t.Run(kind, func(t *testing.T) {
			var f *conversationFixture
			var w *withdrawalFixture
			if kind == "wait" {
				f = conversationNativeTest(t)
			} else {
				w = withdrawalTest(t, true)
				f = w.f
			}
			backend := f.r.Run
			var changed []byte
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if path == conversationTodo && args[0] == "question-"+kind {
					var db map[string]any
					if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
						t.Fatal("missing fixture")
					}
					db["projects"].(map[string]any)["cc-pc-tools-rs"] = filepath.Join(f.r.Config.Home, "different-root")
					if err := publish(f.r.TodoPath, db); err != nil {
						t.Fatal(err)
					}
					changed, _ = os.ReadFile(f.r.TodoPath)
				}
				return backend(ctx, path, args...)
			}
			var err error
			if kind == "wait" {
				_, err = f.r.conversationAsk(context.Background(), f.input)
			} else {
				_, err = w.run()
			}
			after, _ := os.ReadFile(f.r.TodoPath)
			if changed == nil || err == nil || string(changed) != string(after) {
				t.Fatal("scope change allowed wait or withdrawal mutation")
			}
		})
	}
}

func TestConversationActualCanonicalSameGenerationNewOwnerIsNotEdited(t *testing.T) {
	f := conversationNativeTest(t)
	f.mutateTodo(t, func(row map[string]any) { row["resume_generation"] = 1 })
	f.ask(t)
	f.mutateTodo(t, func(row map[string]any) { row["worker_receipt"] = map[string]any{"job": "new-worker", "generation": 1} })
	f.reply(t, 8, "a")
	before, _ := os.ReadFile(f.r.TodoPath)
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	after, _ := os.ReadFile(f.r.TodoPath)
	if len(health.Errors) == 0 || string(before) != string(after) {
		t.Fatal("same-generation replacement owner was edited")
	}
}

func TestConversationActualCanonicalGuardedProjectCannotChangeBeforeDeliverySnapshot(t *testing.T) {
	f := continuedNative(t)
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("missing fixture")
	}
	db["projects"].(map[string]any)["other-project"] = f.r.Config.Home
	if err := publish(f.r.TodoPath, db); err != nil {
		t.Fatal(err)
	}
	f.reply(t, 8, "a")
	backend := f.r.Run
	var afterProjectChange []byte
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		out, err := backend(ctx, path, args...)
		if strings.Contains(path, "scrub391") && err == nil {
			f.mutateTodo(t, func(row map[string]any) { row["project"] = "other-project" })
			afterProjectChange, _ = os.ReadFile(f.r.TodoPath)
		}
		return out, err
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	after, _ := os.ReadFile(f.r.TodoPath)
	if afterProjectChange == nil || len(health.Errors) == 0 || string(afterProjectChange) != string(after) {
		t.Fatal("delivery mutated a different project after the original project guard")
	}
}

func TestConversationActualCanonicalContinuedDeliveryRejectsRacesAndOtherHolds(t *testing.T) {
	for _, change := range []string{"cas-note", "cas-generation", "new-question", "missing-origin", "stop", "dependency", "guard"} {
		t.Run(change, func(t *testing.T) {
			f := continuedNative(t)
			reply := f.reply(t, 8, "a")
			if !strings.HasPrefix(change, "cas-") {
				f.mutateTodo(t, func(row map[string]any) {
					switch change {
					case "new-question":
						row["owner_question_wait"].(map[string]any)["question"].(map[string]any)["request_id"] = "another-question"
					case "missing-origin":
						delete(row["owner_question_wait"].(map[string]any), "origin")
					case "stop":
						row["owner_stopped"] = true
					case "dependency":
						row["wait_on"] = []any{99}
					case "guard":
						f.deny = true
					}
				})
			}
			var expected []byte
			backend := f.r.Run
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if path == conversationTodo && args[0] == "question-answer" && strings.HasPrefix(change, "cas-") {
					f.mutateTodo(t, func(row map[string]any) {
						if change == "cas-note" {
							row["note"] = "後から届いた直接指示"
						} else {
							row["resume_generation"] = 3
						}
					})
					expected, _ = os.ReadFile(f.r.TodoPath)
				}
				return backend(ctx, path, args...)
			}
			before, _ := os.ReadFile(f.r.TodoPath)
			health := Health{}
			f.r.processConversations(context.Background(), &health)
			after, _ := os.ReadFile(f.r.TodoPath)
			if expected == nil {
				expected = before
			}
			if len(health.Errors) == 0 || string(after) != string(expected) {
				t.Fatal("rejected delivery changed task or lost the intervening instruction")
			}
			if req := f.r.State.Requests[reply.Message]; req == nil || req.Phase != "conversation_reply_saved" || req.Todo != 0 {
				t.Fatal("rejection reported a delivery")
			}
			if f.posts != 1 || f.close != 0 {
				t.Fatal("rejection resent a question or closed an unapplied answer")
			}
		})
	}
}

func TestConversationActualCanonicalContinuedDeliveryRecoversLostResultAndSeparatesEdits(t *testing.T) {
	f := continuedNative(t)
	reply := f.reply(t, 8, "a")
	backend := f.r.Run
	deliveries := 0
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		out, err := backend(ctx, path, args...)
		if path == conversationTodo && args[0] == "question-answer" && err == nil {
			deliveries++
			return "", errors.New("lost response after committed delivery")
		}
		return out, err
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) == 0 || f.r.State.Requests[reply.Message].Todo != 0 {
		t.Fatal("lost response was acknowledged before readback")
	}
	f.mutateTodo(t, func(row map[string]any) {
		row["status"] = "block"
		row["owner_stopped"] = true
		row["note"] = "新しい停止と直接指示"
		row["scheduler"].(map[string]any)["owner_instruction"] = "新しい担当への直接指示"
	})
	before, _ := os.ReadFile(f.r.TodoPath)
	f.r.processConversations(context.Background(), &Health{})
	after, _ := os.ReadFile(f.r.TodoPath)
	if string(before) != string(after) || deliveries != 1 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
		t.Fatal("recovery changed a new stop or lost the committed delivery")
	}
	// A text-preserving edit changes the actual saved source bytes. It must
	// never inherit the first delivery or implementation result.
	f.edited = map[string]string{reply.Message: time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)}
	f.r.processConversations(context.Background(), &Health{})
	if req := f.r.State.Requests[reply.Message]; req.Phase != "conversation_reply_saved" {
		t.Fatal("edited answer inherited a stopped generation's delivery")
	}
	f.r.Run = backend
	f.mutateTodo(t, func(row map[string]any) { row["status"] = "doing"; delete(row, "owner_stopped") })
	health = Health{}
	f.r.processConversations(context.Background(), &health)
	var db struct {
		Items []struct {
			Records []json.RawMessage `json:"owner_question_delivery_receipts"`
		} `json:"items"`
	}
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil || len(health.Errors) != 0 || len(db.Items[0].Records) != 2 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
		t.Fatal("current edit did not receive its own durable delivery")
	}
	if f.close != 0 {
		t.Fatal("delivery claimed an implementation")
	}
}

// This optional acceptance check uses an actual freshly built canonical CLI
// against a temporary database. It never selects the production ToDo store.
func conversationNativeTest(t *testing.T) *conversationFixture {
	t.Helper()
	candidate := os.Getenv("CC_CONVERSATION_TODO_BINARY")
	if candidate == "" {
		t.Skip("fresh canonical ToDo binary not supplied")
	}
	if !filepath.IsAbs(candidate) {
		t.Fatal("candidate must be absolute")
	}
	f := conversationTest(t, true)
	f.r.Now = time.Now
	if err := publish(filepath.Join(f.r.Config.Home, "isolated-notify.json"), map[string]any{"guild_id": "100", "owner_user_id": "300", "application_id": "400", "task_channel_id": "200"}); err != nil {
		t.Fatal(err)
	}
	backend := f.r.Run
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path != conversationTodo {
			return backend(ctx, path, args...)
		}
		command := exec.CommandContext(ctx, candidate, args...)
		hide(command)
		command.WaitDelay = time.Second
		command.Env = append(os.Environ(), "CC_TODO_HOME="+f.r.Config.Home, "CC_TODO_JOBS="+filepath.Join(f.r.Config.Home, "isolated-jobs"), "CC_ASK_CONFIG="+filepath.Join(f.r.Config.Home, "isolated-notify.json"))
		out, err := command.Output()
		if err != nil {
			return "", errors.New("isolated canonical command rejected")
		}
		return string(out), nil
	}
	return f
}

func TestConversationActualCanonicalTodoRoundTrip(t *testing.T) {
	f := conversationNativeTest(t)
	f.ask(t)
	row, err := f.r.todo(42)
	if err != nil || row.Status != "block" || !conversationWaitRecorded(row, f.q) {
		t.Fatal("actual wait not recorded")
	}
	reply := f.reply(t, 8, "a")
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 {
		t.Fatalf("actual answer transition failed: %v", health.Errors)
	}
	row, err = f.r.todo(42)
	if err != nil || row.Status != "todo" || len(row.QuestionAnswers) != 1 || !strings.Contains(row.Note, "以前の作業を保持") || !strings.Contains(row.Note, "conversation-owner-reply:") || strings.Contains(row.Note, "[discord-conversation-question-v1 ") || string(row.Spec) != `"unchanged-original-spec"` || len(row.Jobs) != 1 {
		t.Fatal("actual canonical release lost task state")
	}
	if req := f.r.State.Requests[reply.Message]; req == nil || req.Todo != 42 || req.Phase != "conversation_reply_delivered" {
		t.Fatal("actual answer was not linked to original task")
	}
}

func TestConversationActualCanonicalTodoBlockWinsOverStaleDelivery(t *testing.T) {
	f := conversationNativeTest(t)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	backend := f.r.Run
	competed := false
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path == conversationTodo && args[0] == "edit" && !competed {
			competed = true
			if _, err := backend(ctx, path, "block", "42", "データ到着まで停止"); err != nil {
				return "", err
			}
		}
		return backend(ctx, path, args...)
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	row, err := f.r.todo(42)
	if !competed || err != nil || len(health.Errors) == 0 || row.Status != "block" || row.Note != "データ到着まで停止" || !conversationWaitRecorded(row, f.q) || len(row.QuestionAnswers) != 0 {
		t.Fatal("canonical block was overwritten or released by stale delivery")
	}
	if req := f.r.State.Requests[reply.Message]; req == nil || req.Todo != 0 || req.Phase != "conversation_reply_saved" {
		t.Fatal("canonical rejection was reported as delivered")
	}
}
