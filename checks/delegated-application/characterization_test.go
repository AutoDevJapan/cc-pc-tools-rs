// This independent check is loaded into unattended-runner with Go's -overlay.
// It is deliberately outside the production package and is not a writer.
// Every task, receipt, job, and HTTP peer below belongs to a test fixture.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// installIndependentTasks registers a separate actor while retaining the
// cancelled origin. It does not add the actor's job to the origin's jobs.
func installIndependentTasks(t *testing.T, f *conversationCloseFixture) []byte {
	t.Helper()
	f.r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	projectRoot := t.TempDir()
	db := map[string]any{
		"projects": map[string]string{"cc-pc-tools-rs": projectRoot},
		"items": []any{
			map[string]any{
				"id": 42, "project": "cc-pc-tools-rs", "status": "block",
				"note": "The owner cancelled the original work. Keep its scope and stop.",
				"jobs": []string{"worker-42"}, "wait_on": []int{44},
				"owner_question_wait": map[string]any{"question": f.q.Message},
				"scheduler":           map[string]any{"owner_stopped": true, "stop_kind": "owner_cancelled"},
			},
			map[string]any{
				"id": 43, "project": "cc-pc-tools-rs", "status": "doing",
				"jobs":           []string{"actor-43"},
				"worker_receipt": map[string]any{"job": "actor-43"},
				"scheduler":      map[string]any{"job": "actor-43"},
			},
		},
	}
	if err := publish(f.r.TodoPath, db); err != nil {
		t.Fatal("cannot install synthetic tasks")
	}
	if err := publish(filepath.Join(f.r.JobsPath, "actor-43", "job.json"), map[string]any{
		"id": "actor-43", "status": "running", "todo_id": 43,
		"cwd": projectRoot,
	}); err != nil {
		t.Fatal("cannot install synthetic actor job")
	}
	before, err := os.ReadFile(f.r.TodoPath)
	if err != nil {
		t.Fatal(err)
	}
	return before
}

func requireIndependentTasksUnchanged(t *testing.T, f *conversationCloseFixture, before []byte) {
	t.Helper()
	after, err := os.ReadFile(f.r.TodoPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("the cancelled origin, dependencies, actor ownership, or jobs changed")
	}
}

// These are observations of the pinned implementation, not acceptance of the
// requested repair. The separate AcceptanceRed tests express missing behavior.
func TestIndependentCheckCurrentDelegatedReceiptIsRejected(t *testing.T) {
	f := conversationCloseTest(t)
	reply := f.addReply(t, "Apply the separate policy while retaining the cancellation.", true)
	before := installIndependentTasks(t, f)
	f.changeApplication(t, reply.Message, func(receipt map[string]any) {
		receipt["todo_id"] = 43
		receipt["job_id"] = "actor-43"
	})
	closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
	if closed || !errors.Is(err, errConversationCloseApplied) {
		t.Fatalf("expected the pinned implementation's actor rejection: closed=%v err=%v", closed, err)
	}
	requireIndependentTasksUnchanged(t, f, before)
	f.check(t, 0)
}

func TestIndependentCheckCurrentSavedReplyHidesExistingSource(t *testing.T) {
	f := conversationCloseTest(t)
	reply := f.addReply(t, "Apply the separate policy while retaining the cancellation.", true)
	before := installIndependentTasks(t, f)
	req := f.r.State.Requests[reply.Message]
	req.Phase, req.Todo = "conversation_reply_saved", 0
	stamp, ready, err := f.r.conversationReplySource(context.Background(), f.q, f.current(), reply)
	if err != nil || ready || stamp.SourceSHA != "" {
		t.Fatalf("expected the saved-only source gate: ready=%v err=%v", ready, err)
	}
	// The fixture contains a real canonical source, so absence from this result
	// cannot establish that the live original source file is absent.
	if _, err := os.Stat(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+".json")); err != nil {
		t.Fatal("control source is missing")
	}
	closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
	if err != nil || closed || f.record(t).Phase != "pending" {
		t.Fatalf("saved-only input must remain pending: closed=%v err=%v", closed, err)
	}
	requireIndependentTasksUnchanged(t, f, before)
	f.check(t, 0)
}

func TestIndependentCheckCurrentReceiptDoesNotVerifyCurrentJobOwnership(t *testing.T) {
	f := conversationCloseTest(t)
	f.addReply(t, "Apply the separate policy while retaining the cancellation.", true)
	before := installIndependentTasks(t, f)
	// The synthetic applied file names worker-42, but the only real synthetic
	// current job is actor-43. No worker-42 job metadata exists.
	closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
	if err != nil || !closed || f.record(t).Phase != "verified" {
		t.Fatalf("the pinned legacy branch unexpectedly verified real job ownership: closed=%v err=%v", closed, err)
	}
	requireIndependentTasksUnchanged(t, f, before)
	f.check(t, 1)
}

func TestIndependentCheckCurrentCLIHasNoImplementationRecordEntry(t *testing.T) {
	_, err := parseConversationCLI([]string{"implementation-record"})
	if !errors.Is(err, errConversationArguments) {
		t.Fatal("the pinned CLI unexpectedly contains this implementation writer")
	}
	// This tests the current command list; it is not a proposed command contract.
}

// These two tests MUST fail on the pinned legacy validator. They deliberately
// are not added to production CI. They are an isolated reproduction for the
// implementation owner, not a Green result or a complete new-writer fixture.
func TestIndependentCheckAcceptanceRedHonestSeparateActorCanClose(t *testing.T) {
	f := conversationCloseTest(t)
	reply := f.addReply(t, "Apply the separate policy while retaining the cancellation.", true)
	before := installIndependentTasks(t, f)
	f.changeApplication(t, reply.Message, func(receipt map[string]any) {
		receipt["todo_id"] = 43
		receipt["job_id"] = "actor-43"
	})
	closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
	// Check preservation independently of the expected Red assertion.
	requireIndependentTasksUnchanged(t, f, before)
	if err != nil || !closed {
		f.check(t, 0)
		t.Fatalf("missing separate-actor support: closed=%v err=%v", closed, err)
	}
	f.check(t, 1)
}

func TestIndependentCheckAcceptanceRedUnownedHistoricalJobCannotClose(t *testing.T) {
	f := conversationCloseTest(t)
	f.addReply(t, "Apply the separate policy while retaining the cancellation.", true)
	before := installIndependentTasks(t, f)
	closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
	requireIndependentTasksUnchanged(t, f, before)
	if closed || err == nil {
		f.check(t, 1)
		t.Fatalf("missing current actor/job verification: closed=%v err=%v", closed, err)
	}
	f.check(t, 0)
}
