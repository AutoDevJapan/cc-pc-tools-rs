package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImplementationCLIIsPurposeBound(t *testing.T) {
	for _, args := range [][]string{
		{"source-verify", "--request-id", "normal-question", "--message-id", "124"},
		{"implementation-record", "--request-id", "normal-question", "--message-id", "124", "--actor-todo-id", "43", "--source-sha256", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--revision", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", "--evidence-file", "result.private.json"},
		{"close", "--request-id", "normal-question"},
	} {
		if _, err := parseConversationCLI(args); err != nil {
			t.Errorf("normal implementation entry unavailable: %v", err)
		}
	}
}

func implementationNative(t *testing.T) (*conversationFixture, conversationCLIOptions) {
	t.Helper()
	f, opts := directionNative(t)
	opts.Command = "implementation-record"
	opts.EvidenceFile = filepath.Join(f.r.Config.Home, "execution.private.json")
	if publish(opts.EvidenceFile, conversationImplementationEvidence{Completion: "Separate assigned policy applied.", Execution: "Exact revision built, installed hash and normal acceptance verified.", Origin: "Cancellation, old options and dependencies preserved.", Identity: "Current API question/reply version equals canonical source.", AppliedAt: f.r.Now().UTC().Format(time.RFC3339Nano)}) != nil {
		t.Fatal("evidence fixture")
	}
	for _, path := range []string{filepath.Join(f.r.Config.Home, "actor.md"), filepath.Join(f.r.Config.Home, "isolated-jobs", "direction-worker", "prompt.md")} {
		if os.WriteFile(path, []byte("origin 42 question "+f.q.Message+" reply "+opts.Message), 0600) != nil {
			t.Fatal("assignment fixture")
		}
	}
	f.mutateTodo(t, func(row map[string]any) {
		row["status"] = "block"
		row["owner_stopped"] = true
		row["note"] = "Owner cancelled original work; retain wait and original options."
		row["scheduler"] = map[string]any{"owner_stopped": true, "stop_kind": "owner_cancelled"}
	})
	req := f.r.State.Requests[opts.Message]
	req.Todo = 0
	req.Phase = "conversation_reply_saved"
	return f, opts
}
func implementationCLI(t *testing.T, f *conversationFixture, opts conversationCLIOptions) (int, map[string]any) {
	t.Helper()
	args := []string{opts.Command, "--request-id", opts.RequestID}
	if opts.Command != "close" {
		args = append(args, "--message-id", opts.Message)
	}
	if opts.Command == "implementation-record" {
		args = append(args, "--actor-todo-id", "43", "--source-sha256", opts.SourceSHA, "--revision", opts.Revision, "--evidence-file", opts.EvidenceFile)
	}
	var out bytes.Buffer
	code := conversationCLIWithLoader(args, &out, func(string) (conversationCLIBackend, func(), error) { return f.r, func() {}, nil })
	var result map[string]any
	if code == 0 && json.Unmarshal(out.Bytes(), &result) != nil {
		t.Fatal("normal result not metadata JSON")
	}
	if strings.Contains(out.String(), "aかb") || strings.Contains(out.String(), "fixture-token") {
		t.Fatal("private text leaked")
	}
	return code, result
}
func TestImplementationNativeSavedSourceAndCancelledOriginRoundTrip(t *testing.T) {
	f, opts := implementationNative(t)
	sourceOpts := opts
	sourceOpts.Command = "source-verify"
	code, source := implementationCLI(t, f, sourceOpts)
	if code != 0 || source["source_receipt_sha256"] != opts.SourceSHA || source["canonical_current"] != true {
		t.Fatal("saved/todo0 canonical source was hidden", code, source)
	}
	before, _ := f.r.todo(42)
	code, result := implementationCLI(t, f, opts)
	if code != 0 || result["recorded"] != true || result["question_closed_get_verified"] != false || f.close != 0 {
		t.Fatal("separate actor record failed or prematurely closed", code, result)
	}
	after, _ := f.r.todo(42)
	if after.Status != before.Status || !bytes.Equal(after.Spec, before.Spec) || canonicalHash(after.WaitOn) != canonicalHash(before.WaitOn) || canonicalHash(after.Jobs) != canonicalHash(before.Jobs) || len(after.ImplementationRecords) != 1 || f.r.State.Requests[opts.Message].Todo != 0 {
		t.Fatal("original cancellation or real actor ownership changed")
	}
	if closed, err := f.r.closeConversation(context.Background(), f.q, &conversationProof{}); err == nil || closed {
		t.Fatal("invalid proof closed")
	}
	proof, err := f.r.conversationProof(context.Background(), f.q)
	if err != nil {
		t.Fatal(err)
	}
	if closed, err := f.r.closeConversation(context.Background(), f.q, proof); err != nil || closed || f.close != 0 {
		t.Fatal("tick closed before actor's explicit close", err)
	}
	closeOpts := opts
	closeOpts.Command = "close"
	code, result = implementationCLI(t, f, closeOpts)
	if code != 0 || result["question_closed_get_verified"] != true || f.close != 1 {
		t.Fatal("explicit PATCH/GET close failed", code, result)
	}
	old, _ := os.ReadFile(f.r.TodoPath)
	code, result = implementationCLI(t, f, opts)
	again, _ := os.ReadFile(f.r.TodoPath)
	if code != 0 || result["changed"] != false || !bytes.Equal(old, again) || f.close != 1 {
		t.Fatal("idempotent receipt readback changed original")
	}
}
func TestImplementationNativeRejectsVersionSourceAndActorMismatch(t *testing.T) {
	for _, change := range []string{"sha", "same-text-edit", "other-question", "source-missing", "source-changed", "foreign-current-job", "job-owner", "missing-job", "actor-project", "competing-stop"} {
		t.Run(change, func(t *testing.T) {
			f, opts := implementationNative(t)
			switch change {
			case "sha":
				opts.SourceSHA = strings.Repeat("f", 64)
			case "same-text-edit":
				f.edited = map[string]string{opts.Message: f.r.Now().UTC().Format(time.RFC3339Nano)}
			case "other-question":
				opts.RequestID = "other-question"
			case "source-missing":
				if err := os.Remove(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", opts.Message+".json")); err != nil {
					t.Fatal(err)
				}
			case "source-changed":
				if os.WriteFile(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", opts.Message+".json"), []byte(`{"changed":true}`), 0600) != nil {
					t.Fatal("source fixture")
				}
			case "foreign-current-job":
				t.Setenv("CC_JOB_ID", "unrelated-worker")
			case "missing-job":
				if err := os.Remove(filepath.Join(f.r.Config.Home, "isolated-jobs", "direction-worker", "job.json")); err != nil {
					t.Fatal(err)
				}
			case "job-owner":
				if publish(filepath.Join(f.r.Config.Home, "isolated-jobs", "direction-worker", "job.json"), map[string]any{"id": "direction-worker", "todo_id": 42, "cwd": f.r.Config.Home, "status": "running"}) != nil {
					t.Fatal("job fixture")
				}
			case "actor-project":
				var db map[string]any
				_ = readJSON(f.r.TodoPath, maxTodo, &db)
				db["items"].([]any)[1].(map[string]any)["project"] = "other"
				if publish(f.r.TodoPath, db) != nil {
					t.Fatal("fixture")
				}
			}
			if change == "competing-stop" {
				backend := f.r.Run
				f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
					if path == conversationTodo && args[0] == "implementation-record" {
						f.mutateTodo(t, func(row map[string]any) { row["cancel_requested"] = true })
					}
					return backend(ctx, path, args...)
				}
			}
			before, _ := f.r.todo(42)
			code, _ := implementationCLI(t, f, opts)
			after, _ := f.r.todo(42)
			if code == 0 || f.close != 0 || len(after.ImplementationRecords) != 0 || after.Note != before.Note || canonicalHash(after.Jobs) != canonicalHash(before.Jobs) || after.Status != before.Status {
				t.Fatal("mismatch wrote or closed original")
			}
		})
	}
}
func TestImplementationNativeLostWriteResponseReplaysAndNewReplyHoldsClose(t *testing.T) {
	f, opts := implementationNative(t)
	backend := f.r.Run
	lost := false
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		out, err := backend(ctx, path, args...)
		if path == conversationTodo && args[0] == "implementation-record" && err == nil && !lost {
			lost = true
			return "", errors.New("committed reply lost")
		}
		return out, err
	}
	if code, _ := implementationCLI(t, f, opts); code == 0 || !lost {
		t.Fatal("did not exercise committed response loss")
	}
	code, result := implementationCLI(t, f, opts)
	if code != 0 || result["changed"] != false {
		t.Fatal("lost response replay not reconciled", result)
	}
	f.reply(t, 9, "A further current reply needs separate implementation.")
	closeOpts := opts
	closeOpts.Command = "close"
	code, result = implementationCLI(t, f, closeOpts)
	if code != 0 || result["question_closed_get_verified"] != false || f.close != 0 {
		t.Fatal("new unapplied reply did not hold close", result)
	}
}

func TestImplementationNativeSourceGenerationRetainsMissingOrDifferentOriginal(t *testing.T) {
	for _, change := range []string{"missing", "same-text-edit", "different-source"} {
		t.Run(change, func(t *testing.T) {
			f, opts := implementationNative(t)
			sourcePath := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", opts.Message+".json")
			original, err := os.ReadFile(sourcePath)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "missing":
				if os.Remove(sourcePath) != nil {
					t.Fatal("fixture")
				}
			case "same-text-edit":
				f.edited = map[string]string{opts.Message: f.r.Now().UTC().Format(time.RFC3339Nano)}
			case "different-source":
				original = []byte(`{"original":"different retained generation"}`)
				if os.WriteFile(sourcePath, original, 0600) != nil {
					t.Fatal("fixture")
				}
			}
			before, _ := os.ReadFile(f.r.TodoPath)
			sourceOpts := opts
			sourceOpts.Command = "source-verify"
			if code, _ := implementationCLI(t, f, sourceOpts); code == 0 {
				t.Fatal("wrong original verified")
			}
			sourceOpts.Command = "source-reacquire"
			code, result := implementationCLI(t, f, sourceOpts)
			if code != 0 || result["reacquired"] != true || result["original_source_overwritten"] != false || result["source_receipt_sha256"] == opts.SourceSHA {
				t.Fatal("new proper source generation unavailable", code, result)
			}
			after, _ := os.ReadFile(f.r.TodoPath)
			if !bytes.Equal(before, after) || f.close != 0 {
				t.Fatal("source reacquisition released original")
			}
			saved, err := os.ReadFile(sourcePath)
			if change == "missing" {
				if !os.IsNotExist(err) {
					t.Fatal("missing original was recreated")
				}
			} else if err != nil || !bytes.Equal(saved, original) {
				t.Fatal("old original overwritten")
			}
			code, again := implementationCLI(t, f, sourceOpts)
			if code != 0 || again["reacquired"] != false || again["source_receipt_sha256"] != result["source_receipt_sha256"] {
				t.Fatal("replay changed canonical generation")
			}
		})
	}
}

func TestImplementationNativeNewStopDefersExplicitCloseAndPreservesReceipt(t *testing.T) {
	f, opts := implementationNative(t)
	if code, _ := implementationCLI(t, f, opts); code != 0 {
		t.Fatal("record fixture")
	}
	f.mutateTodo(t, func(row map[string]any) { row["cancel_requested"] = true })
	before, _ := os.ReadFile(f.r.TodoPath)
	opts.Command = "close"
	code, result := implementationCLI(t, f, opts)
	after, _ := os.ReadFile(f.r.TodoPath)
	if code != 0 || result["question_closed_get_verified"] != false || f.close != 0 || !bytes.Equal(before, after) {
		t.Fatal("new stop lost or falsely closed", code, result)
	}
}

func TestImplementationRejectsLegacyJobNameWithoutRealJob(t *testing.T) {
	f := conversationCloseTest(t)
	f.addReply(t, "Retain cancellation and apply only assigned policy.", true)
	if err := os.Remove(filepath.Join(f.r.JobsPath, "worker-42", "job.json")); err != nil {
		t.Fatal(err)
	}
	closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
	if closed || err == nil {
		t.Fatal("job name alone closed a question without a real job record")
	}
	f.check(t, 0)
}
