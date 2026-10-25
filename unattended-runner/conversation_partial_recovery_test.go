package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ordinaryProjectFixture(t *testing.T) (*conversationFixture, []string, []byte) {
	t.Helper()
	f, args, source := sameActorIntakeLinkFixture(t)
	return prepareOrdinaryProjectFixture(t, f, args, source)
}

func prepareOrdinaryProjectFixture(t *testing.T, f *conversationFixture, args []string, source []byte) (*conversationFixture, []string, []byte) {
	t.Helper()
	proof, err := f.r.conversationProof(context.Background(), f.q)
	if err != nil {
		t.Fatal(err)
	}
	var saved Request
	if json.Unmarshal(source, &saved) != nil {
		t.Fatal("source fixture")
	}
	contextBytes, _ := json.Marshal(proof)
	saved.Context = string(contextBytes)
	source, _ = json.Marshal(saved)
	sourcePath := filepath.Join(f.r.Config.Home, "requests", saved.Message+".json")
	if os.WriteFile(sourcePath, source, 0600) != nil {
		t.Fatal("source fixture")
	}
	f.r.State.Requests[saved.Message].Context = saved.Context
	for i, flag := range args {
		if flag == "--source-sha256" {
			args[i+1] = bytesHash(source)
		}
	}
	assignment := "Original API source: " + sourcePath + " thread " + saved.Thread + " reply " + saved.Message
	for _, path := range []string{filepath.Join(f.r.Config.Home, "link-spec.md"), filepath.Join(f.r.JobsPath, "link-worker", "prompt.md")} {
		if os.WriteFile(path, []byte(assignment), 0600) != nil {
			t.Fatal("assignment fixture")
		}
	}
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("todo fixture")
	}
	db["projects"].(map[string]any)["intake"] = f.r.Config.Home
	for _, raw := range db["items"].([]any) {
		row := raw.(map[string]any)
		if row["id"] == float64(43) {
			row["project"] = "intake"
			row["note"] = "unattended-owner-receipt:" + saved.Key
		}
	}
	if publish(f.r.TodoPath, db) != nil || f.r.save() != nil {
		t.Fatal("save fixture")
	}
	return f, args, source
}

func TestImplementationNativeOrdinaryProjectPreservesWaitAndCloses(t *testing.T) {
	f := conversationNativeTest(t)
	f.input.Wait = false
	f, args, source := prepareIntakeLinkFixture(t, f)
	message := f.replies[0].Message
	f.r.State.Requests[message].Todo = 43
	for i, flag := range args {
		if flag == "--intake-todo-id" {
			args[i+1] = "43"
		}
	}
	f, args, source = prepareOrdinaryProjectFixture(t, f, args, source)
	f.mutateTodo(t, func(row map[string]any) {
		row["status"] = "wait"
		row["wait_ready"] = map[string]any{"condition": "retry-time", "not_before": 9999999999}
	})
	before, _ := f.r.todo(42)
	actorBefore, _ := f.r.todo(43)
	if code, _ := intakeLinkCLI(t, f, args); code != 0 {
		t.Fatal("native independent project link failed")
	}
	b, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", message+".json"))
	evidence := filepath.Join(f.r.Config.Home, "ordinary-execution.json")
	if publish(evidence, conversationImplementationEvidence{Completion: "Existing owner's exact answer was applied.", Execution: "Original executor receipt retained; current continuation verified its real result.", Origin: "Both projects, original wait and source retained.", Identity: "Current API and ordinary source matched.", AppliedAt: f.r.Now().UTC().Format(time.RFC3339Nano)}) != nil {
		t.Fatal("fixture")
	}
	opts := conversationCLIOptions{Command: "implementation-record", RequestID: f.q.RequestID, Message: message, SourceSHA: bytesHash(b), Revision: strings.Repeat("b", 40), EvidenceFile: evidence}
	if code, result := implementationCLI(t, f, opts); code != 0 || result["recorded"] != true {
		t.Fatal("native independent project record failed", code, result)
	}
	opts.Command = "close"
	if code, result := implementationCLI(t, f, opts); code != 0 || result["question_closed_get_verified"] != true || f.close != 1 {
		t.Fatal("independent project close failed", code, result)
	}
	after, _ := f.r.todo(42)
	actorAfter, _ := f.r.todo(43)
	original, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "requests", message+".json"))
	if after.Status != before.Status || canonicalHash(after.WaitReady) != canonicalHash(before.WaitReady) || canonicalHash(after.Jobs) != canonicalHash(before.Jobs) || canonicalHash(actorBefore) != canonicalHash(actorAfter) || !bytes.Equal(source, original) || len(after.ImplementationRecords) != 1 {
		t.Fatal("native record or close changed ownership, wait or source")
	}
	var restarted State
	if readJSON(filepath.Join(f.r.Config.Home, "unattended-state.json"), maxState, &restarted) != nil {
		t.Fatal("restart fixture")
	}
	f.r.State = &restarted
	if code, result := implementationCLI(t, f, opts); code != 0 || result["question_closed_get_verified"] != true || f.close != 1 {
		t.Fatal("close replay did not retain GET-verified result", code, result)
	}
}

func TestConversationPartialRecoveryKeepsTechnicalWaitAndNeverSends(t *testing.T) {
	f, _, _ := ordinaryProjectFixture(t)
	index, err := f.r.loadConversations()
	if err != nil {
		t.Fatal(err)
	}
	q := index.Questions[f.q.RequestID]
	q.Phase = "reserved"
	q.Thread = ""
	q.Message = ""
	q.Progress = ""
	q.Fingerprint = ""
	q.DecisionSHA = ""
	q.QuestionSHA = ""
	q.ReceiptFile = ""
	q.ReceiptSHA = ""
	index.Questions[q.RequestID] = q
	if publish(f.r.conversationPath(), index) != nil {
		t.Fatal("reservation fixture")
	}
	f.mutateTodo(t, func(row map[string]any) {
		row["status"] = "wait"
		row["wait_ready"] = map[string]any{"condition": "retry-time", "not_before": 9999999999}
	})
	before, _ := os.ReadFile(f.r.TodoPath)
	backend := f.r.Run
	sends := 0
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path == conversationSender && args[0] == "recovery-ask" {
			sends++
		}
		raw, err := backend(ctx, path, args...)
		if err == nil && path == conversationSender && args[0] == "recovery-replies" {
			var proof map[string]any
			if json.Unmarshal([]byte(raw), &proof) == nil {
				proof["question_fingerprint"] = f.q.Fingerprint
				b, _ := json.Marshal(proof)
				raw = string(b)
			}
		}
		return raw, err
	}
	code, result := intakeLinkCLI(t, f, []string{"recover-question", "--request-id", q.RequestID, "--message-id", f.replies[0].Message})
	after, _ := os.ReadFile(f.r.TodoPath)
	if code != 0 || result["recovered"] != true || !bytes.Equal(before, after) || sends != 0 {
		t.Fatal("posted question did not recover without changing technical wait", code, result, sends)
	}
	restored, err := f.r.loadConversations()
	if err != nil || restored.Questions[q.RequestID].Thread != f.q.Thread {
		t.Fatal("recovered question not durable", err)
	}
	code, _ = intakeLinkCLI(t, f, []string{"recover-question", "--request-id", q.RequestID, "--message-id", f.replies[0].Message})
	if code != 0 || sends != 0 {
		t.Fatal("recovery replay sent or failed")
	}
}

func TestConversationOrdinaryCrossProjectAssignmentLinksWithoutMovingEitherTask(t *testing.T) {
	f, args, source := ordinaryProjectFixture(t)
	before, _ := os.ReadFile(f.r.TodoPath)
	code, result := intakeLinkCLI(t, f, args)
	if code != 0 || result["linked"] != true {
		t.Fatal("verified ordinary intake assignment rejected", code, result)
	}
	after, _ := os.ReadFile(f.r.TodoPath)
	saved, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "requests", f.replies[0].Message+".json"))
	if !bytes.Equal(before, after) || !bytes.Equal(source, saved) || f.close != 0 {
		t.Fatal("cross-project link changed source, either task or question")
	}
	if code, result = intakeLinkCLI(t, f, []string{"source-verify", "--request-id", f.q.RequestID, "--message-id", f.replies[0].Message}); code != 0 || result["canonical_current"] != true {
		t.Fatal("canonical linked source unavailable", code, result)
	}
}

func TestConversationOrdinaryCrossProjectAssignmentRejectsUnrelatedEvidence(t *testing.T) {
	for _, change := range []string{"receipt-key", "spec", "prompt", "context-question", "context-owner", "source-version"} {
		t.Run(change, func(t *testing.T) {
			f, args, _ := ordinaryProjectFixture(t)
			message := f.replies[0].Message
			switch change {
			case "receipt-key":
				var db map[string]any
				_ = readJSON(f.r.TodoPath, maxTodo, &db)
				for _, raw := range db["items"].([]any) {
					row := raw.(map[string]any)
					if row["id"] == float64(43) {
						row["note"] = "unrelated receipt"
					}
				}
				if publish(f.r.TodoPath, db) != nil {
					t.Fatal("fixture")
				}
			case "spec", "prompt":
				path := filepath.Join(f.r.Config.Home, "link-spec.md")
				if change == "prompt" {
					path = filepath.Join(f.r.JobsPath, "link-worker", "prompt.md")
				}
				if os.WriteFile(path, []byte("unrelated work"), 0600) != nil {
					t.Fatal("fixture")
				}
			default:
				path := filepath.Join(f.r.Config.Home, "requests", message+".json")
				var saved Request
				b, _ := os.ReadFile(path)
				_ = json.Unmarshal(b, &saved)
				if change == "source-version" {
					saved.Text = "different reply"
				} else {
					var proof conversationProof
					_ = json.Unmarshal([]byte(saved.Context), &proof)
					if change == "context-question" {
						proof.Question = "999"
					} else {
						proof.Owner = "999"
					}
					b, _ = json.Marshal(proof)
					saved.Context = string(b)
				}
				b, _ = json.Marshal(saved)
				if os.WriteFile(path, b, 0600) != nil {
					t.Fatal("fixture")
				}
				for i, flag := range args {
					if flag == "--source-sha256" {
						args[i+1] = bytesHash(b)
					}
				}
			}
			before, _ := os.ReadFile(f.r.TodoPath)
			code, _ := intakeLinkCLI(t, f, args)
			after, _ := os.ReadFile(f.r.TodoPath)
			if code == 0 || !bytes.Equal(before, after) || f.r.State.ConversationCaptures[message] != nil {
				t.Fatal("unrelated evidence linked or changed original")
			}
		})
	}
}

func TestConversationPartialRecoveryRejectsChangedReplyAndQuestion(t *testing.T) {
	for _, change := range []string{"reply-body", "same-text-edit", "question-owner", "question-body"} {
		t.Run(change, func(t *testing.T) {
			f, _, _ := ordinaryProjectFixture(t)
			index, err := f.r.loadConversations()
			if err != nil {
				t.Fatal(err)
			}
			q := index.Questions[f.q.RequestID]
			q.Phase = "reserved"
			index.Questions[q.RequestID] = q
			if publish(f.r.conversationPath(), index) != nil {
				t.Fatal("fixture")
			}
			message := f.replies[0].Message
			if change == "reply-body" {
				f.replies[0].Body = "changed answer"
			}
			if change == "same-text-edit" {
				f.edited = map[string]string{message: f.r.Now().UTC().Format(time.RFC3339Nano)}
			}
			backend := f.r.Run
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				raw, err := backend(ctx, path, args...)
				if err == nil && path == conversationSender && args[0] == "recovery-replies" {
					var proof map[string]any
					_ = json.Unmarshal([]byte(raw), &proof)
					proof["question_fingerprint"] = f.q.Fingerprint
					if change == "question-owner" {
						proof["owner_id"] = "999"
					}
					if change == "question-body" {
						proof["original_question"] = "changed question"
					}
					b, _ := json.Marshal(proof)
					raw = string(b)
				}
				return raw, err
			}
			before, _ := os.ReadFile(f.r.TodoPath)
			code, _ := intakeLinkCLI(t, f, []string{"recover-question", "--request-id", q.RequestID, "--message-id", message})
			after, _ := os.ReadFile(f.r.TodoPath)
			latest, err := f.r.loadConversations()
			if code == 0 || err != nil || !bytes.Equal(before, after) || latest.Questions[q.RequestID].Phase != "reserved" || f.posts != 1 {
				t.Fatal("changed source recovered or caused a send/task mutation", code, err)
			}
		})
	}
}
