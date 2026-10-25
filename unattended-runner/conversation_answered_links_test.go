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

func TestAnsweredLinksRecoveryCLIRequiresExactScope(t *testing.T) {
	valid := []string{"recover-answered", "--request-id", "normal-question", "--thread", "123", "--message-id", "124", "--actor-todo-id", "43", "--intake-todo-id", "44", "--source-sha256", strings.Repeat("a", 64), "--revision", strings.Repeat("b", 40)}
	if _, err := parseConversationCLI(valid); err != nil {
		t.Fatal("answered auxiliary-link recovery entry is unavailable", err)
	}
	for _, flag := range []string{"--thread", "--message-id", "--actor-todo-id", "--intake-todo-id", "--source-sha256", "--revision"} {
		args := append([]string(nil), valid...)
		for i := 1; i < len(args); i += 2 {
			if args[i] == flag {
				args = append(args[:i], args[i+2:]...)
				break
			}
		}
		if _, err := parseConversationCLI(args); err == nil {
			t.Fatalf("accepted missing %s", flag)
		}
	}
}

func answeredLinksFixture(t *testing.T, native bool, change string) (*conversationFixture, []string, []byte) {
	t.Helper()
	var f *conversationFixture
	if native {
		f = conversationNativeTest(t)
		f.input.Wait = false
	} else {
		f = conversationTest(t, false)
	}
	f, args, raw := prepareIntakeLinkFixture(t, f)
	index, err := f.r.loadConversations()
	if err != nil {
		t.Fatal(err)
	}
	q := index.Questions[f.q.RequestID]
	q.Wait, q.Phase = true, "reserved"
	body, _ := boundedRegular(q.BodyFile)
	decision, _ := boundedRegular(q.DecisionFile)
	q.InputSHA = canonicalHash([]any{q.Todo, q.OriginThread, bytesHash(body), canonicalHash(json.RawMessage(decision)), q.Title, q.Wait})
	q.Thread, q.Message, q.Progress, q.Fingerprint, q.DecisionSHA, q.QuestionSHA, q.ReceiptFile, q.ReceiptSHA = "", "", "", "", "", "", "", ""
	index.Questions[q.RequestID] = q
	if publish(f.r.conversationPath(), index) != nil {
		t.Fatal("reserved fixture")
	}
	backend := f.r.Run
	f.r.Run = func(ctx context.Context, path string, argv ...string) (string, error) {
		out, err := backend(ctx, path, argv...)
		if err != nil || path != conversationSender || argv[0] != "recovery-replies" {
			return out, err
		}
		var proof map[string]any
		if json.Unmarshal([]byte(out), &proof) != nil {
			t.Fatal("sender fixture")
		}
		proof["question_delivery_verified"] = change != "body"
		proof["sender_delivery_complete"] = change == "complete"
		proof["auxiliary_links_unconfirmed"] = []string{"question-links", "origin-links"}
		if change == "unknown-phase" {
			proof["auxiliary_links_unconfirmed"] = []string{"question-continuation:1"}
		}
		proof["question_fingerprint"], proof["origin_thread_id"] = strings.Repeat("a", 64), q.OriginThread
		proof["image_sha256"], proof["original_decision"] = []string{}, f.decision
		b, _ := json.Marshal(proof)
		return string(b), nil
	}
	args[0] = "recover-answered"
	args = append(args, "--thread", f.q.Thread)
	return f, args, raw
}

func TestAnsweredLinksRecoveryNativeSourceImplementationAndClose(t *testing.T) {
	f, args, raw := answeredLinksFixture(t, true, "")
	before, _ := os.ReadFile(f.r.TodoPath)
	ordinary, _ := json.Marshal(f.r.State.Requests[f.replies[0].Message])
	code, result := intakeLinkCLI(t, f, args)
	if code != 0 || result["question_delivery_verified"] != true || result["application_inferred"] != false || f.close != 0 {
		t.Fatal("canonical delivery/source recovery failed", code, result)
	}
	after, _ := os.ReadFile(f.r.TodoPath)
	if !bytes.Equal(before, after) {
		t.Fatal("delivery recovery changed original ownership/wait/dependencies")
	}
	index, err := f.r.loadConversations()
	if err != nil || index.Questions[f.q.RequestID].Phase != "link_recovery" || !index.Questions[f.q.RequestID].Wait {
		t.Fatal("original wait request was lost", err)
	}
	opts := conversationCLIOptions{RequestID: f.q.RequestID, Message: f.replies[0].Message, Revision: strings.Repeat("b", 40), SourceSHA: result["source_receipt_sha256"].(string), Command: "close"}
	// A pre-existing arbitrary applied JSON must not authorize close.
	oldApplied := []byte(`{"closure":false,"repair_pending":true,"completion":"untrusted"}`)
	appliedPath := filepath.Join(f.r.Config.Home, "applied", opts.Message+".json")
	if os.MkdirAll(filepath.Dir(appliedPath), 0700) != nil || os.WriteFile(appliedPath, oldApplied, 0600) != nil {
		t.Fatal("old receipt fixture")
	}
	if code, result := implementationCLI(t, f, opts); code != 0 || result["question_closed_get_verified"] != false || f.close != 0 {
		t.Fatal("unapplied recovery closed question", code, result)
	}
	var state State
	if readJSON(filepath.Join(f.r.Config.Home, "unattended-state.json"), maxState, &state) != nil {
		t.Fatal("restart source")
	}
	f.r.State = &state
	if code, _ := intakeLinkCLI(t, f, args); code != 0 {
		t.Fatal("retained source recovery did not replay")
	}
	opts.Command, opts.EvidenceFile = "implementation-record", filepath.Join(f.r.Config.Home, "actual-result.json")
	if publish(opts.EvidenceFile, conversationImplementationEvidence{Completion: "Scoped operation already executed; recovery verified it without rerunning it.", Execution: "Original outcome hash, exit status and current source verified.", Origin: "Original work, actual worker, wait and dependencies retained.", Identity: "Full question and unedited current owner reply checked by API.", AppliedAt: f.r.Now().UTC().Format(time.RFC3339Nano)}) != nil {
		t.Fatal("evidence fixture")
	}
	origin, _ := f.r.todo(42)
	if code, result := implementationCLI(t, f, opts); code != 0 || result["recorded"] != true {
		t.Fatal("canonical implementation did not record", code, result)
	}
	opts.Command = "close"
	if code, result := implementationCLI(t, f, opts); code != 0 || result["question_closed_get_verified"] != true || f.close != 1 || !f.archived {
		t.Fatal("PATCH/GET close failed", code, result)
	}
	end, _ := f.r.todo(42)
	saved, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "requests", opts.Message+".json"))
	intake, _ := json.Marshal(f.r.State.Requests[opts.Message])
	applied, _ := os.ReadFile(appliedPath)
	if !bytes.Equal(raw, saved) || !bytes.Equal(ordinary, intake) || !bytes.Equal(applied, oldApplied) || origin.Status != end.Status || canonicalHash(origin.Jobs) != canonicalHash(end.Jobs) || canonicalHash(origin.WaitOn) != canonicalHash(end.WaitOn) || !bytes.Equal(origin.Spec, end.Spec) || len(end.ImplementationRecords) != 1 || f.posts != 1 {
		t.Fatal("recovery changed original work/intake/source or retried question")
	}
}

func TestAnsweredLinksRecoveryRejectsIncompleteOrDifferentScope(t *testing.T) {
	for _, change := range []string{"body", "complete", "unknown-phase", "edited", "other-owner", "multiple", "no-answer", "source", "assignment", "stopped"} {
		t.Run(change, func(t *testing.T) {
			f, args, _ := answeredLinksFixture(t, false, change)
			switch change {
			case "edited":
				f.edited = map[string]string{f.replies[0].Message: f.r.Now().Format(time.RFC3339Nano)}
			case "other-owner":
				f.replies[0].Author = "999"
			case "multiple":
				f.reply(t, 9, "b")
			case "no-answer":
				f.replies = nil
			case "source":
				for i, arg := range args {
					if arg == "--source-sha256" {
						args[i+1] = strings.Repeat("f", 64)
					}
				}
			case "assignment":
				if os.WriteFile(filepath.Join(f.r.JobsPath, "link-worker", "prompt.md"), []byte("old unrelated audit"), 0600) != nil {
					t.Fatal("fixture")
				}
			case "stopped":
				f.mutateTodo(t, func(row map[string]any) { row["owner_stopped"] = true })
			}
			before, _ := os.ReadFile(f.r.TodoPath)
			index, _ := os.ReadFile(f.r.conversationPath())
			if code, _ := intakeLinkCLI(t, f, args); code == 0 {
				t.Fatal("conflicting recovery accepted")
			}
			after, _ := os.ReadFile(f.r.TodoPath)
			next, _ := os.ReadFile(f.r.conversationPath())
			if !bytes.Equal(before, after) || !bytes.Equal(index, next) || f.close != 0 {
				t.Fatal("rejection changed original task/index or closed")
			}
		})
	}
}
