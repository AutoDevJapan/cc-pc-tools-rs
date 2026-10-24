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

func intakeLinkFixture(t *testing.T) (*conversationFixture, []string, []byte) {
	t.Helper()
	t.Setenv("CC_JOB_ID", "link-worker")
	f := conversationTest(t, false)
	return prepareIntakeLinkFixture(t, f)
}

func prepareIntakeLinkFixture(t *testing.T, f *conversationFixture) (*conversationFixture, []string, []byte) {
	t.Helper()
	t.Setenv("CC_JOB_ID", "link-worker")
	f.ask(t)
	reply := f.reply(t, 8, "a")
	old := &Request{Thread: f.q.Thread, Guild: f.r.State.Destination.Guild, RecordVersion: 2,
		Message: reply.Message, Author: reply.Author, Created: reply.Created, Acquired: f.r.Now().Format("2006-01-02T15:04:05Z07:00"),
		Source: "discord-api-v10", Text: reply.Body, Key: requestKey(f.q.Thread, reply.Message), Phase: "captured"}
	source, _ := json.Marshal(old)
	if err := writeConversationOnce(filepath.Join(f.r.Config.Home, "requests", reply.Message+".json"), source); err != nil {
		t.Fatal(err)
	}
	old.Todo, old.Phase = 44, "registered"
	f.r.State.Requests[reply.Message] = old
	f.r.JobsPath = filepath.Join(f.r.Config.Home, "isolated-jobs")
	jobDir := filepath.Join(f.r.JobsPath, "link-worker")
	spec := filepath.Join(f.r.Config.Home, "link-spec.md")
	assignment := "元ToDo42 " + f.q.RequestID + " question=" + f.q.Message + " reply=" + reply.Message
	if os.MkdirAll(jobDir, 0700) != nil || os.WriteFile(spec, []byte(assignment), 0600) != nil || os.WriteFile(filepath.Join(jobDir, "prompt.md"), []byte(assignment), 0600) != nil ||
		publish(filepath.Join(jobDir, "job.json"), map[string]any{"id": "link-worker", "todo_id": 43, "status": "running", "cwd": f.r.Config.Home}) != nil {
		t.Fatal("assignment fixture")
	}
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("todo fixture")
	}
	db["items"] = append(db["items"].([]any),
		map[string]any{"id": 43, "project": f.q.Project, "status": "doing", "spec": spec, "jobs": []string{"link-worker"}, "scheduler": map[string]any{"job": "link-worker", "worktree": f.r.Config.Home}},
		map[string]any{"id": 44, "project": f.q.Project, "status": "wait", "spec": "original intake", "jobs": []string{"intake-worker"}, "wait_on": []int{43}})
	if publish(f.r.TodoPath, db) != nil || f.r.save() != nil {
		t.Fatal("save fixture")
	}
	args := []string{"source-link", "--request-id", f.q.RequestID, "--message-id", reply.Message, "--actor-todo-id", "43", "--intake-todo-id", "44", "--source-sha256", bytesHash(source), "--revision", strings.Repeat("b", 40)}
	return f, args, source
}

func intakeLinkCLI(t *testing.T, f *conversationFixture, args []string) (int, map[string]any) {
	t.Helper()
	var out bytes.Buffer
	code := conversationCLIWithLoader(args, &out, func(string) (conversationCLIBackend, func(), error) { return f.r, func() {}, nil })
	var result map[string]any
	if code == 0 && json.Unmarshal(out.Bytes(), &result) != nil {
		t.Fatal("non-JSON link result")
	}
	if strings.Contains(out.String(), "aかb") || strings.Contains(out.String(), "fixture-token") {
		t.Fatal("private text leaked")
	}
	return code, result
}

func sameActorIntakeLinkFixture(t *testing.T) (*conversationFixture, []string, []byte) {
	t.Helper()
	f, args, source := intakeLinkFixture(t)
	f.r.State.Requests[f.replies[0].Message].Todo = 43
	for i, arg := range args {
		if arg == "--intake-todo-id" {
			args[i+1] = "43"
		}
	}
	if err := f.r.save(); err != nil {
		t.Fatal(err)
	}
	return f, args, source
}

func TestConversationIntakeLinkSameActorPreservesSourceAndAssignment(t *testing.T) {
	f, args, original := sameActorIntakeLinkFixture(t)
	message := f.replies[0].Message
	ordinary, _ := json.Marshal(f.r.State.Requests[message])
	actorBefore, _ := f.r.todo(43)
	if code, result := intakeLinkCLI(t, f, args); code != 0 || result["linked"] != true || result["ordinary_intake_retained"] != true || result["actor_todo_id"] != float64(43) || result["intake_todo_id"] != float64(43) {
		t.Fatal("assigned intake worker could not link its own current reply", code, result)
	}
	after, _ := json.Marshal(f.r.State.Requests[message])
	saved, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "requests", message+".json"))
	actorAfter, _ := f.r.todo(43)
	if !bytes.Equal(ordinary, after) || !bytes.Equal(original, saved) || canonicalHash(actorBefore) != canonicalHash(actorAfter) || f.close != 0 {
		t.Fatal("link changed ordinary intake, original source, assignment or question state")
	}
	var restarted State
	if readJSON(filepath.Join(f.r.Config.Home, "unattended-state.json"), maxState, &restarted) != nil {
		t.Fatal("restart fixture")
	}
	f.r.State = &restarted
	if code, result := intakeLinkCLI(t, f, []string{"source-verify", "--request-id", f.q.RequestID, "--message-id", message}); code != 0 || result["canonical_current"] != true {
		t.Fatal("same-worker capture did not survive restart", code, result)
	}
	beforeReplay, _ := os.ReadFile(f.r.TodoPath)
	if code, _ := intakeLinkCLI(t, f, args); code != 0 {
		t.Fatal("same-worker replay failed")
	}
	afterReplay, _ := os.ReadFile(f.r.TodoPath)
	if !bytes.Equal(beforeReplay, afterReplay) {
		t.Fatal("same-worker replay duplicated delivery")
	}
}

func TestConversationIntakeLinkSameActorStillRequiresCurrentAssignment(t *testing.T) {
	for _, change := range []string{"stopped", "foreign-job", "spec", "prompt", "origin-as-actor", "origin-as-intake"} {
		t.Run(change, func(t *testing.T) {
			f, args, _ := sameActorIntakeLinkFixture(t)
			message := f.replies[0].Message
			switch change {
			case "foreign-job":
				t.Setenv("CC_JOB_ID", "not-the-assigned-job")
			case "spec":
				if os.WriteFile(filepath.Join(f.r.Config.Home, "link-spec.md"), []byte("unrelated assignment"), 0600) != nil {
					t.Fatal("fixture")
				}
			case "prompt":
				if os.WriteFile(filepath.Join(f.r.JobsPath, "link-worker", "prompt.md"), []byte("unrelated assignment"), 0600) != nil {
					t.Fatal("fixture")
				}
			case "origin-as-actor", "origin-as-intake":
				flag := "--actor-todo-id"
				if change == "origin-as-intake" {
					flag = "--intake-todo-id"
					f.r.State.Requests[message].Todo = f.q.Todo
				}
				for i, arg := range args {
					if arg == flag {
						args[i+1] = "42"
					}
				}
			case "stopped":
				var db map[string]any
				if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
					t.Fatal("fixture")
				}
				for _, item := range db["items"].([]any) {
					row := item.(map[string]any)
					if row["id"] == float64(43) {
						row["owner_stopped"] = true
					}
				}
				if publish(f.r.TodoPath, db) != nil {
					t.Fatal("fixture")
				}
			}
			before, _ := os.ReadFile(f.r.TodoPath)
			ordinary, _ := json.Marshal(f.r.State.Requests[message])
			if code, _ := intakeLinkCLI(t, f, args); code == 0 {
				t.Fatal("same-worker link ignored assignment or origin separation")
			}
			after, _ := os.ReadFile(f.r.TodoPath)
			current, _ := json.Marshal(f.r.State.Requests[message])
			if !bytes.Equal(before, after) || !bytes.Equal(ordinary, current) || len(f.r.State.ConversationCaptures) != 0 || f.close != 0 {
				t.Fatal("rejected same-worker link changed original records")
			}
		})
	}
}

func TestConversationIntakeLinkSameActorNativeImplementationAndClose(t *testing.T) {
	f := conversationNativeTest(t)
	f.input.Wait = false
	f, args, original := prepareIntakeLinkFixture(t, f)
	message := f.replies[0].Message
	f.r.State.Requests[message].Todo = 43
	for i, arg := range args {
		if arg == "--intake-todo-id" {
			args[i+1] = "43"
		}
	}
	if err := f.r.save(); err != nil {
		t.Fatal(err)
	}
	originBefore, _ := f.r.todo(42)
	actorBefore, _ := f.r.todo(43)
	if code, _ := intakeLinkCLI(t, f, args); code != 0 {
		t.Fatal("native same-worker source link failed")
	}
	source, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", message+".json"))
	evidence := filepath.Join(f.r.Config.Home, "same-worker-execution.json")
	if publish(evidence, conversationImplementationEvidence{Completion: "The assigned model was applied to the limited review scope.", Execution: "Fixed review input and actual model verified.", Origin: "Original work and Must findings remain unfinished.", Identity: "Current question and owner reply verified.", AppliedAt: f.r.Now().UTC().Format(time.RFC3339Nano)}) != nil {
		t.Fatal("evidence fixture")
	}
	opts := conversationCLIOptions{Command: "implementation-record", RequestID: f.q.RequestID, Message: message, SourceSHA: bytesHash(source), Revision: strings.Repeat("b", 40), EvidenceFile: evidence}
	if code, result := implementationCLI(t, f, opts); code != 0 || result["recorded"] != true || f.close != 0 {
		t.Fatal("same-worker implementation was not recorded separately from close", code, result)
	}
	opts.Command = "close"
	if code, result := implementationCLI(t, f, opts); code != 0 || result["question_closed_get_verified"] != true || f.close != 1 || !f.archived {
		t.Fatal("same-worker implementation could not close with GET verification", code, result)
	}
	originAfter, _ := f.r.todo(42)
	actorAfter, _ := f.r.todo(43)
	saved, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "requests", message+".json"))
	if originAfter.Status != originBefore.Status || canonicalHash(originAfter.Jobs) != canonicalHash(originBefore.Jobs) || canonicalHash(originAfter.WaitOn) != canonicalHash(originBefore.WaitOn) || len(originAfter.ImplementationRecords) != 1 || canonicalHash(actorBefore) != canonicalHash(actorAfter) || f.r.State.Requests[message].Todo != 43 || !bytes.Equal(saved, original) {
		t.Fatal("same-worker record/close changed original work, intake or raw source")
	}
}

func TestConversationIntakeLinkPreservesOrdinaryAndSurvivesRestart(t *testing.T) {
	f, args, source := intakeLinkFixture(t)
	message := f.replies[0].Message
	old, _ := json.Marshal(f.r.State.Requests[message])
	intakeBefore, _ := f.r.todo(44)
	if code, result := intakeLinkCLI(t, f, args); code != 0 || result["linked"] != true || result["ordinary_intake_retained"] != true {
		t.Fatal("same current owner reply could not be linked without taking over ordinary intake", code, result)
	}
	after, _ := json.Marshal(f.r.State.Requests[message])
	saved, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "requests", message+".json"))
	intakeAfter, _ := f.r.todo(44)
	if !bytes.Equal(old, after) || !bytes.Equal(source, saved) || canonicalHash(intakeBefore) != canonicalHash(intakeAfter) {
		t.Fatal("original intake or raw source changed")
	}
	row, _ := f.r.todo(42)
	if row.Status != "doing" || len(row.Jobs) != 1 || !strings.Contains(row.Note, "conversation-owner-reply:") || f.close != 0 {
		t.Fatal("delivery changed owner/status or inferred application")
	}
	var restarted State
	if readJSON(filepath.Join(f.r.Config.Home, "unattended-state.json"), maxState, &restarted) != nil {
		t.Fatal("restart fixture")
	}
	f.r.State = &restarted
	if code, result := intakeLinkCLI(t, f, []string{"source-verify", "--request-id", f.q.RequestID, "--message-id", message}); code != 0 || result["canonical_current"] != true {
		t.Fatal("linked source lost after restart", result)
	}
	before, _ := os.ReadFile(f.r.TodoPath)
	if code, _ := intakeLinkCLI(t, f, args); code != 0 {
		t.Fatal("same link could not replay")
	}
	replayed, _ := os.ReadFile(f.r.TodoPath)
	if !bytes.Equal(before, replayed) {
		t.Fatal("link replay duplicated delivery")
	}
}

func TestConversationIntakeLinkRejectsConflictsWithoutMutation(t *testing.T) {
	for _, change := range []string{"question", "author", "body", "edit", "source", "actor", "assignment", "stopped-origin", "stopped-actor", "stopped-intake", "guard", "other-conversation", "other-intake"} {
		t.Run(change, func(t *testing.T) {
			f, args, _ := intakeLinkFixture(t)
			message := f.replies[0].Message
			switch change {
			case "question":
				f.replies[0].Question = "999"
			case "author":
				f.replies[0].Author = "999"
			case "body":
				f.replies[0].Body = "b"
			case "edit":
				f.edited = map[string]string{message: f.r.Now().Format("2006-01-02T15:04:05Z07:00")}
			case "source":
				args[len(args)-3] = strings.Repeat("f", 64)
			case "actor":
				t.Setenv("CC_JOB_ID", "foreign-worker")
			case "assignment":
				if os.WriteFile(filepath.Join(f.r.JobsPath, "link-worker", "prompt.md"), []byte("another question"), 0600) != nil {
					t.Fatal("fixture")
				}
			case "guard":
				f.deny = true
			case "other-conversation":
				f.r.State.Requests[message].Conversation = "foreign-question"
			case "other-intake":
				f.r.State.Requests[message].Todo = 45
			default:
				id := float64(42)
				if change == "stopped-actor" {
					id = 43
				}
				if change == "stopped-intake" {
					id = 44
				}
				var db map[string]any
				if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
					t.Fatal("fixture")
				}
				for _, item := range db["items"].([]any) {
					row := item.(map[string]any)
					if row["id"] == id {
						row["owner_stopped"] = true
					}
				}
				if publish(f.r.TodoPath, db) != nil {
					t.Fatal("fixture")
				}
			}
			before, _ := os.ReadFile(f.r.TodoPath)
			old, _ := json.Marshal(f.r.State.Requests[message])
			if code, _ := intakeLinkCLI(t, f, args); code == 0 {
				t.Fatal("conflicting link accepted")
			}
			after, _ := os.ReadFile(f.r.TodoPath)
			current, _ := json.Marshal(f.r.State.Requests[message])
			if !bytes.Equal(before, after) || !bytes.Equal(old, current) || f.close != 0 {
				t.Fatal("rejection mutated original")
			}
		})
	}
}

func TestConversationIntakeLinkStateFailureRetriesRetainedSource(t *testing.T) {
	f, args, _ := intakeLinkFixture(t)
	path := filepath.Join(f.r.Config.Home, "unattended-state.json")
	if os.Rename(path, path+".before") != nil || os.Mkdir(path, 0700) != nil {
		t.Fatal("fixture")
	}
	if code, _ := intakeLinkCLI(t, f, args); code == 0 {
		t.Fatal("failed state save reported success")
	}
	if os.Remove(path) != nil || os.Rename(path+".before", path) != nil {
		t.Fatal("restore fixture")
	}
	if code, _ := intakeLinkCLI(t, f, args); code != 0 {
		t.Fatal("partial source acquisition could not retry")
	}
	proof, err := f.r.conversationProof(context.Background(), f.q)
	if err != nil {
		t.Fatal(err)
	}
	if _, ready, err := f.r.conversationReplySource(context.Background(), f.q, proof, f.replies[0]); err != nil || !ready {
		t.Fatal("retry source is not verifiable", err)
	}
}

func TestConversationIntakeLinkReceiptFailureAndDeliveryFailureCanRetry(t *testing.T) {
	for _, failure := range []string{"receipt", "delivery"} {
		t.Run(failure, func(t *testing.T) {
			f, args, original := intakeLinkFixture(t)
			message := f.replies[0].Message
			path := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", message+".json")
			backend := f.r.Run
			if failure == "receipt" {
				if os.MkdirAll(path, 0700) != nil {
					t.Fatal("fixture")
				}
			} else {
				f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
					if path == conversationTodo && args[0] == "edit" {
						return "", errImplementation
					}
					return backend(ctx, path, args...)
				}
			}
			if code, _ := intakeLinkCLI(t, f, args); code == 0 {
				t.Fatal("failed write reported success")
			}
			if failure == "receipt" && os.Remove(path) != nil {
				t.Fatal("fixture")
			}
			f.r.Run = backend
			var restarted State
			if readJSON(filepath.Join(f.r.Config.Home, "unattended-state.json"), maxState, &restarted) != nil {
				t.Fatal("restart")
			}
			f.r.State = &restarted
			if code, _ := intakeLinkCLI(t, f, args); code != 0 {
				t.Fatal("failed write did not resume")
			}
			saved, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "requests", message+".json"))
			if !bytes.Equal(original, saved) || f.r.State.Requests[message].Todo != 44 {
				t.Fatal("retry lost ordinary original")
			}
		})
	}
}

func TestConversationIntakeLinkLaterVerificationRequiresRetainedEvidence(t *testing.T) {
	for _, change := range []string{"original", "link", "snapshot", "ordinary-owner", "live-edit"} {
		t.Run(change, func(t *testing.T) {
			f, args, _ := intakeLinkFixture(t)
			if code, _ := intakeLinkCLI(t, f, args); code != 0 {
				t.Fatal("link fixture")
			}
			message := f.replies[0].Message
			path := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", message+".intake-link.json")
			switch change {
			case "original":
				path = filepath.Join(f.r.Config.Home, "requests", message+".json")
			case "snapshot":
				var link conversationIntakeLinkReceipt
				if readJSON(path, maxState, &link) != nil {
					t.Fatal("fixture")
				}
				path = filepath.Join(filepath.Dir(path), message+".intake-"+link.IntakeSHA+".json")
			case "ordinary-owner":
				f.r.State.Requests[message].Todo = 45
			case "live-edit":
				f.edited = map[string]string{message: f.r.Now().Format("2006-01-02T15:04:05Z07:00")}
			}
			if change != "ordinary-owner" && change != "live-edit" {
				if os.WriteFile(path, []byte("changed evidence"), 0600) != nil {
					t.Fatal("fixture")
				}
			}
			if code, _ := intakeLinkCLI(t, f, []string{"source-verify", "--request-id", f.q.RequestID, "--message-id", message}); code == 0 {
				t.Fatal("verification ignored changed linked evidence")
			}
		})
	}
}

func TestConversationIntakeLinkNativeDirectionAndCloseKeepBothTasks(t *testing.T) {
	f := conversationNativeTest(t)
	f.input.Wait = false
	f.decision = json.RawMessage(`{"todo_id":42,"decision":"direction","scope":{"specification":"a and b from this question"}}`)
	if os.WriteFile(f.input.DecisionFile, f.decision, 0600) != nil {
		t.Fatal("fixture")
	}
	f, args, original := prepareIntakeLinkFixture(t, f)
	if code, _ := intakeLinkCLI(t, f, args); code != 0 {
		t.Fatal("native link/delivery failed")
	}
	message := f.replies[0].Message
	if code, result := intakeLinkCLI(t, f, []string{"source-verify", "--request-id", f.q.RequestID, "--message-id", message}); code != 0 || result["canonical_current"] != true {
		t.Fatal("native verify failed")
	}
	source, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", message+".json"))
	opts := conversationCLIOptions{RequestID: f.q.RequestID, Message: message, SourceSHA: bytesHash(source), Revision: strings.Repeat("b", 40)}
	if code, result := directionCLI(t, f, opts); code != 0 || result["question_closed_get_verified"] != true {
		t.Fatal("native direction/close failed", result)
	}
	row, _ := f.r.todo(42)
	intake, _ := f.r.todo(44)
	current, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "requests", message+".json"))
	if row.Status != "doing" || intake.Status != "wait" || len(intake.WaitOn) != 1 || intake.WaitOn[0] != 43 || len(row.DirectionRecords) != 1 || f.close != 1 || !f.archived || !bytes.Equal(current, original) || f.r.State.Requests[message].Todo != 44 {
		t.Fatal("direction/close changed original work or intake")
	}
	if code, result := intakeLinkCLI(t, f, []string{"close", "--request-id", f.q.RequestID}); code != 0 || result["question_closed_get_verified"] != true || f.close != 1 {
		t.Fatal("canonical close GET replay failed")
	}
}
