package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectionCLIRequiresCurrentActorAndExactSource(t *testing.T) {
	args := []string{"direction-record", "--request-id", "normal-question", "--message-id", "124",
		"--actor-todo-id", "2", "--source-sha256", strings.Repeat("a", 64), "--revision", strings.Repeat("b", 40)}
	if _, err := parseConversationCLI(args); err != nil {
		t.Fatal("normal direction record command unavailable")
	}
	for _, extra := range [][]string{{"--job-id", "borrowed-worker"}, {"--notify"}, {"--attach", "unapproved.png"}, {"--kind", "implementation"}} {
		if _, err := parseConversationCLI(append(append([]string{}, args...), extra...)); err == nil {
			t.Fatal("direction accepts arbitrary job, notifications, images or promotion")
		}
	}
}

func directionNative(t *testing.T) (*conversationFixture, conversationCLIOptions) {
	t.Helper()
	t.Setenv("CC_JOB_ID", "direction-worker")
	f := conversationNativeTest(t)
	f.decision = json.RawMessage(`{"todo_id":42,"decision":"direction","scope":{"specification":"current structure only"}}`)
	if os.WriteFile(f.input.DecisionFile, f.decision, 0600) != nil {
		t.Fatal("could not write direction question fixture")
	}
	f.ask(t)
	reply := f.reply(t, 8, "a")
	h := Health{}
	f.r.processConversations(context.Background(), &h)
	if len(h.Errors) != 0 {
		t.Fatalf("native delivery failed: %v", h.Errors)
	}
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("task fixture unavailable")
	}
	original := db["items"].([]any)[0].(map[string]any)
	original["status"] = "wait"
	original["wait_on"] = []uint64{44}
	scope := f.q.RequestID + " " + f.q.Message + " " + reply.Message
	actorSpec := filepath.Join(f.r.Config.Home, "actor.md")
	jobs := filepath.Join(f.r.Config.Home, "isolated-jobs", "direction-worker")
	if os.MkdirAll(jobs, 0700) != nil || os.WriteFile(actorSpec, []byte(scope), 0600) != nil || os.WriteFile(filepath.Join(jobs, "prompt.md"), []byte(scope), 0600) != nil {
		t.Fatal("actor assignment fixture unavailable")
	}
	if publish(filepath.Join(jobs, "job.json"), map[string]any{"id": "direction-worker", "todo_id": 43, "status": "running", "cwd": f.r.Config.Home}) != nil {
		t.Fatal("actual job fixture unavailable")
	}
	db["items"] = append(db["items"].([]any), map[string]any{"id": 43, "project": f.q.Project, "status": "doing", "note": "direction only", "jobs": []string{"direction-worker"}, "spec": actorSpec, "scheduler": map[string]any{"job": "direction-worker", "worktree": f.r.Config.Home}}, map[string]any{"id": 44, "project": f.q.Project, "status": "todo", "note": "unfinished product"})
	if publish(f.r.TodoPath, db) != nil {
		t.Fatal("actual assignment fixture unavailable")
	}
	source, err := os.ReadFile(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+".json"))
	if err != nil {
		t.Fatal("canonical delivery capture absent")
	}
	return f, conversationCLIOptions{Command: "direction-record", RequestID: f.q.RequestID, Message: reply.Message, Todo: 43, SourceSHA: bytesHash(source), Revision: strings.Repeat("a", 40)}
}

func directionCLI(t *testing.T, f *conversationFixture, opts conversationCLIOptions) (int, map[string]any) {
	t.Helper()
	args := []string{"direction-record", "--request-id", opts.RequestID, "--message-id", opts.Message, "--actor-todo-id", "43", "--source-sha256", opts.SourceSHA, "--revision", opts.Revision}
	var out bytes.Buffer
	code := conversationCLIWithLoader(args, &out, func(string) (conversationCLIBackend, func(), error) { return f.r, func() {}, nil })
	var result map[string]any
	if code == 0 && json.Unmarshal(out.Bytes(), &result) != nil {
		t.Fatal("direction result is not JSON metadata")
	}
	if strings.Contains(out.String(), "aかb") || strings.Contains(out.String(), "fixture-token") {
		t.Fatal("direction CLI leaked original body or credential")
	}
	return code, result
}

func TestDirectionActualCLIRecordsActorAndArchivesAfterReadback(t *testing.T) {
	f, opts := directionNative(t)
	before, _ := f.r.todo(42)
	code, result := directionCLI(t, f, opts)
	if code != 0 {
		t.Fatal("normal direction CLI rejected qualified assignment")
	}
	if result["recorded"] != true || result["question_closed_get_verified"] != true || result["functional_implementation"] != "incomplete" || result["image_approval"] != false {
		t.Fatal("direction stages or scope differ")
	}
	after, _ := f.r.todo(42)
	if after.Status != before.Status || canonicalHash(after.WaitOn) != canonicalHash(before.WaitOn) || len(after.DirectionRecords) != 1 || !strings.Contains(after.Note, "仕様へ記録、機能は未完了") || !strings.Contains(after.Note, "本人回答（原文）:\na") || !bytes.Equal(after.Spec, before.Spec) || canonicalHash(after.Jobs) != canonicalHash(before.Jobs) {
		t.Fatal("direction did not preserve unfinished product work or record actual original")
	}
	if f.close != 1 || !f.archived || f.r.State.Requests[opts.Message].Phase != "conversation_direction_recorded" {
		t.Fatal("archive or separate direction phase not verified")
	}
	if _, err := os.Stat(filepath.Join(f.r.Config.Home, "applied", opts.Message+".json")); !os.IsNotExist(err) {
		t.Fatal("direction manufactured functional applied evidence")
	}
	raw, _ := os.ReadFile(f.r.TodoPath)
	f.r.NoticeAttempted = false
	code, result = directionCLI(t, f, opts)
	replay, _ := os.ReadFile(f.r.TodoPath)
	if code != 0 || result["changed"] != false || f.close != 1 || !bytes.Equal(raw, replay) {
		t.Fatal("replay duplicated note, history or archive")
	}
}

func TestDirectionActualCLINeverClosesStaleSourcesOrForeignActors(t *testing.T) {
	for _, change := range []string{"sha", "edit", "body", "foreign-job", "unknown-scope", "new-hold", "same-note-project"} {
		t.Run(change, func(t *testing.T) {
			f, opts := directionNative(t)
			switch change {
			case "sha":
				opts.SourceSHA = strings.Repeat("f", 64)
			case "edit":
				f.edited = map[string]string{opts.Message: f.r.Now().Format("2006-01-02T15:04:05.999999999Z07:00")}
			case "body":
				f.replies[0].Body = "b"
			case "foreign-job":
				t.Setenv("CC_JOB_ID", "worker-42")
			case "unknown-scope":
				f.deny = true
			}
			backend := f.r.Run
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if path == conversationTodo && args[0] == "direction-record" && (change == "new-hold" || change == "same-note-project") {
					f.mutateTodo(t, func(row map[string]any) {
						if change == "new-hold" {
							row["owner_stopped"] = true
						} else {
							row["project"] = "another-project"
						}
					})
					// The competing mutation is expected to survive the CAS rejection.
				}
				return backend(ctx, path, args...)
			}
			before, _ := f.r.todo(42)
			code, _ := directionCLI(t, f, opts)
			after, _ := f.r.todo(42)
			if code == 0 || f.close != 0 || len(after.DirectionRecords) != 0 || after.Note != before.Note || !bytes.Equal(after.Spec, before.Spec) || canonicalHash(after.WaitOn) != canonicalHash(before.WaitOn) {
				t.Fatal("invalid source/actor or changed guarded snapshot mutated product or closed question")
			}
		})
	}
}

func TestDirectionActualCLILostSaveResponseReconcilesWithoutDuplicateAppend(t *testing.T) {
	f, opts := directionNative(t)
	backend := f.r.Run
	lost := false
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		out, err := backend(ctx, path, args...)
		if path == conversationTodo && args[0] == "direction-record" && !lost && err == nil {
			lost = true
			return "", errors.New("lost successful response")
		}
		return out, err
	}
	if code, _ := directionCLI(t, f, opts); code == 0 || !lost || f.close != 0 {
		t.Fatal("lost save response was reported as complete")
	}
	row, _ := f.r.todo(42)
	if len(row.DirectionRecords) != 1 {
		t.Fatal("fixture did not persist real specification")
	}
	raw, _ := os.ReadFile(f.r.TodoPath)
	f.r.Run = backend
	if code, result := directionCLI(t, f, opts); code != 0 || result["changed"] != false || result["question_closed_get_verified"] != true {
		t.Fatal("lost result did not recover actual receipt")
	}
	replay, _ := os.ReadFile(f.r.TodoPath)
	if !bytes.Equal(raw, replay) {
		t.Fatal("lost result appended specification or history twice")
	}
}

type directionTransport func(*http.Request) (*http.Response, error)

func (f directionTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDirectionActualCLILostArchiveResponseResumesWithGET(t *testing.T) {
	f, opts := directionNative(t)
	base := f.r.Client.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	lost := false
	f.r.Client.Transport = directionTransport(func(req *http.Request) (*http.Response, error) {
		resp, err := base.RoundTrip(req)
		if req.Method == http.MethodPatch && !lost && err == nil {
			lost = true
			resp.Body.Close()
			return nil, errors.New("lost archive response")
		}
		return resp, err
	})
	if code, _ := directionCLI(t, f, opts); code == 0 || !lost || !f.archived || f.close != 1 {
		t.Fatal("lost archive response was not retained as unresolved")
	}
	f.r.NoticeAttempted = false
	if code, result := directionCLI(t, f, opts); code != 0 || result["question_closed_get_verified"] != true || f.close != 1 {
		t.Fatal("archive GET did not recover without a second PATCH")
	}
}

func TestDirectionActualCLICannotPromoteDirectionReceiptToFunctionalApplication(t *testing.T) {
	f, opts := directionNative(t)
	if code, _ := directionCLI(t, f, opts); code != 0 {
		t.Fatal("direction fixture did not record")
	}
	f.archived = false
	// Even all ordinary fields plus the original product job cannot elevate a
	// direction kind to implementation. The real direction receipt remains separate.
	app := map[string]any{"kind": directionKind, "source": "discord-api-v10", "request_id": f.q.SenderID,
		"origin_todo_id": 42, "todo_id": 42, "thread_id": f.q.Thread, "question_message_id": f.q.Message,
		"message_id": opts.Message, "author_id": f.r.State.Destination.Owner, "job_id": "worker-42",
		"revision": opts.Revision, "applied_at": f.r.Now().Format("2006-01-02T15:04:05.999999999Z07:00"),
		"completion": "direction only", "reply_sha256": canonicalHash(f.replies[0]), "source_receipt_sha256": opts.SourceSHA,
		"verification": map[string]string{"execution": "direction only", "origin_saved": "note", "identity": "owner"}}
	if publish(filepath.Join(f.r.Config.Home, "applied", opts.Message+".json"), app) != nil {
		t.Fatal("promotion fixture unavailable")
	}
	proof, err := f.r.conversationProof(context.Background(), f.q)
	if err != nil {
		t.Fatal(err)
	}
	if closed, err := f.r.closeConversation(context.Background(), f.q, proof); closed || err == nil || f.archived {
		t.Fatal("direction kind was accepted as functional application")
	}
}

func TestDirectionActualCLIAllCurrentRepliesMustBeRecordedBeforeArchive(t *testing.T) {
	f, opts := directionNative(t)
	second := f.reply(t, 10, "現在の構成を維持する")
	// Both exact messages are explicitly in this finite assignment before recording.
	for _, path := range []string{filepath.Join(f.r.Config.Home, "actor.md"), filepath.Join(f.r.Config.Home, "isolated-jobs", "direction-worker", "prompt.md")} {
		old, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if os.WriteFile(path, append(old, []byte(" "+second.Message)...), 0600) != nil {
			t.Fatal("assignment fixture unavailable")
		}
	}
	h := Health{}
	f.r.processConversations(context.Background(), &h)
	if len(h.Errors) != 0 {
		t.Fatalf("second delivery failed: %v", h.Errors)
	}
	if code, result := directionCLI(t, f, opts); code != 0 || result["recorded"] != true || result["question_closed_get_verified"] != false || f.archived {
		t.Fatal("one recorded direction closed a question with an unrecorded current answer")
	}
	stageNote := filepath.Join(f.r.Config.Home, "next-product-stage-note.txt")
	if os.WriteFile(stageNote, []byte("作業が残っています。資源の空きを確認して自動で続けます。"), 0600) != nil {
		t.Fatal("stage-note fixture unavailable")
	}
	if _, err := f.r.Run(context.Background(), conversationTodo, "edit", "42", "--note-file", stageNote); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", second.Message+".json"))
	if err != nil {
		t.Fatal(err)
	}
	opts.Message, opts.SourceSHA = second.Message, bytesHash(source)
	f.r.NoticeAttempted = false
	if code, result := directionCLI(t, f, opts); code != 0 || result["question_closed_get_verified"] != true || f.close != 1 {
		t.Fatal("all current recorded directions did not close with verified GET")
	}
}

func TestDirectionInspectionReturnsCanonicalSourceSHAButNeverApplication(t *testing.T) {
	f, opts := directionNative(t)
	before, _ := os.ReadFile(f.r.TodoPath)
	result, err := f.r.conversationRepliesSnapshot(context.Background(), f.q.RequestID, filepath.Join(f.r.Config.Home, "fresh.private.json"))
	if err != nil {
		t.Fatal("fresh direction inspection failed")
	}
	sources, ok := result["verified_source_receipts"].([]map[string]any)
	if !ok || len(sources) != 1 || sources[0]["message_id"] != opts.Message || sources[0]["source_receipt_sha256"] != opts.SourceSHA || result["receipt_sha256"] == opts.SourceSHA || result["application"] != "not_inferred" {
		t.Fatal("inspection SHA was substituted for canonical source or application was inferred")
	}
	after, _ := os.ReadFile(f.r.TodoPath)
	if !bytes.Equal(before, after) || f.close != 0 {
		t.Fatal("inspection mutated task or archived question")
	}
}

func TestDirectionCloseKeepsSilentProgressDeliveryReceipt(t *testing.T) {
	f, opts := directionNative(t)
	backend := f.r.Run
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		raw, err := backend(ctx, path, args...)
		if path == conversationSender && args[0] == "recovery-log" && err == nil {
			var report map[string]any
			if json.Unmarshal([]byte(raw), &report) != nil {
				return "", errors.New("invalid fixture log")
			}
			report["effective_notify"] = false
			b, _ := json.Marshal(report)
			return string(b), nil
		}
		return raw, err
	}
	if code, _ := directionCLI(t, f, opts); code != 0 {
		t.Fatal("direction fixture did not record")
	}
	index, err := f.r.loadConversations()
	if err != nil {
		t.Fatal(err)
	}
	key := "reply-" + bytesHash([]byte(f.q.RequestID + opts.Message + "conversation_direction_recorded"))[:32]
	log, ok := index.Logs[key]
	if !ok || log.Message == "" || log.VerifiedThread != f.q.Progress || log.EffectiveNotify == nil || *log.EffectiveNotify {
		t.Fatal("closing the question lost the verified silent progress delivery receipt")
	}
}

func TestDirectionLostResponseResumesAfterNormalWorkTransitionAndLaterTick(t *testing.T) {
	f, opts := directionNative(t)
	backend := f.r.Run
	lost := false
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		raw, err := backend(ctx, path, args...)
		if path == conversationTodo && args[0] == "direction-record" && !lost && err == nil {
			lost = true
			return "", errors.New("lost saved result")
		}
		return raw, err
	}
	if code, _ := directionCLI(t, f, opts); code == 0 || !lost {
		t.Fatal("fixture did not lose saved result")
	}
	f.mutateTodo(t, func(row map[string]any) {
		row["status"] = "doing"
		delete(row, "wait_on")
		row["spec"] = "new-product-spec.md"
		row["scheduler"] = map[string]any{"job": "next-product-worker", "worktree": "D:/next-worker"}
		row["jobs"] = []string{"worker-42", "next-product-worker"}
		row["resume_generation"] = 2
	})
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("fixture unavailable")
	}
	actor := db["items"].([]any)[1].(map[string]any)
	actor["status"] = "done"
	actor["scheduler"] = map[string]any{"job": nil}
	actor["spec"] = "next-assignment.md"
	if publish(f.r.TodoPath, db) != nil {
		t.Fatal("fixture transition unavailable")
	}
	if publish(filepath.Join(f.r.Config.Home, "isolated-jobs", "direction-worker", "job.json"), map[string]any{"id": "direction-worker", "todo_id": 43, "status": "cancelled", "cancel_reason": "parent-ended", "cwd": f.r.Config.Home}) != nil {
		t.Fatal("fixture retirement unavailable")
	}
	t.Setenv("CC_JOB_ID", "resumed-worker")
	f.r.Run = backend
	if code, result := directionCLI(t, f, opts); code != 0 || result["question_closed_get_verified"] != true || result["changed"] != false {
		t.Fatal("normal work transition invalidated actual recording")
	}
	h := Health{}
	f.r.processConversations(context.Background(), &h)
	if len(h.Errors) != 0 || f.close != 1 {
		t.Fatalf("later tick invalidated recorded direction: %v", h.Errors)
	}
}

func TestDirectionVerifiedCloseSurvivesNormalDoneAndLaterBlockNotes(t *testing.T) {
	for _, command := range [][]string{{"done", "42", "--note", "product stage completed"}, {"block", "42", "a new independent owner hold"}} {
		t.Run(command[0], func(t *testing.T) {
			f, opts := directionNative(t)
			if code, _ := directionCLI(t, f, opts); code != 0 {
				t.Fatal("direction fixture did not close")
			}
			if _, err := f.r.Run(context.Background(), conversationTodo, command...); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(f.r.TodoPath)
			h := Health{}
			f.r.processConversations(context.Background(), &h)
			after, _ := os.ReadFile(f.r.TodoPath)
			if len(h.Errors) != 0 || f.close != 1 || !bytes.Equal(before, after) {
				t.Fatalf("completed question was invalidated by ordinary note replacement: %v", h.Errors)
			}
		})
	}
}

func TestDirectionLostWriteResponseRecoversAfterActualBlockAndStart(t *testing.T) {
	f, opts := directionNative(t)
	backend := f.r.Run
	lost := false
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		out, err := backend(ctx, path, args...)
		if path == conversationTodo && args[0] == "direction-record" && !lost && err == nil {
			lost = true
			return "", errors.New("lost saved reply")
		}
		return out, err
	}
	if code, _ := directionCLI(t, f, opts); code == 0 || !lost {
		t.Fatal("lost-save fixture did not execute")
	}
	f.r.Run = backend
	for _, args := range [][]string{{"done", "44"}, {"block", "42", "new owner hold"}} {
		if _, err := backend(context.Background(), conversationTodo, args...); err != nil {
			t.Fatal(err)
		}
	}
	if code, result := directionCLI(t, f, opts); code != 0 || result["recorded"] != true || result["question_closed_get_verified"] != false || f.archived {
		t.Fatal("new hold undid durable recording or closed first-time question")
	}
	if _, err := backend(context.Background(), conversationTodo, "start", "42"); err != nil {
		t.Fatal(err)
	}
	f.r.NoticeAttempted = false
	if code, result := directionCLI(t, f, opts); code != 0 || result["changed"] != false || result["question_closed_get_verified"] != true {
		t.Fatal("actual block/start left the question permanently unclosable")
	}
	row, _ := f.r.todo(42)
	if row.Note != "new owner hold" || len(row.DirectionRecords) != 1 {
		t.Fatal("recovery restored an old note or duplicated durable specification")
	}
}
