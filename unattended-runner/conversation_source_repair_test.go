package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func sourceRepairFixture(t *testing.T) (*conversationFixture, []string, []byte) {
	t.Helper()
	return prepareSourceRepairFixture(t, conversationTest(t, true))
}

func prepareSourceRepairFixture(t *testing.T, f *conversationFixture) (*conversationFixture, []string, []byte) {
	t.Helper()
	f, args, source := prepareIntakeLinkFixture(t, f)
	t.Chdir(f.r.Config.Home)
	f.q.Project = "origin-product"
	index, _ := f.r.loadConversations()
	index.Questions[f.q.RequestID] = f.q
	if publish(f.r.conversationPath(), index) != nil {
		t.Fatal("index")
	}
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("db")
	}
	db["projects"].(map[string]any)[f.q.Project] = filepath.Join(f.r.Config.Home, "origin")
	items := db["items"].([]any)
	items[0].(map[string]any)["project"] = f.q.Project
	actor := items[1].(map[string]any)
	actor["scheduler"].(map[string]any)["key"] = "actor-key"
	intake := items[2].(map[string]any)
	intake["wait_on"] = []uint64{f.q.Todo}
	intake["scheduler"] = map[string]any{"job": "intake-worker", "key": "intake-key", "phase": "running", "worktree": f.r.Config.Home}
	spec := filepath.Join(f.r.Config.Home, "intake-spec.md")
	assignment := "thread=" + f.q.Thread + " message=" + f.replies[0].Message
	intake["spec"] = spec
	if os.WriteFile(spec, []byte(assignment), 0600) != nil || publish(f.r.TodoPath, db) != nil {
		t.Fatal("intake")
	}
	jobDir := filepath.Join(f.r.JobsPath, "intake-worker")
	if os.MkdirAll(jobDir, 0700) != nil || os.WriteFile(filepath.Join(jobDir, "prompt.md"), []byte(assignment), 0600) != nil {
		t.Fatal("job")
	}
	job := map[string]any{"id": "intake-worker", "todo_id": 44, "project": "cc-pc-tools-rs", "registered_cwd": f.r.Config.Home, "cwd": f.r.Config.Home, "launch_key": "intake-key", "status": "succeeded", "exit_code": 0, "started_at": f.r.Now().Add(-time.Minute).Format(time.RFC3339Nano), "finished_at": f.r.Now().Add(-time.Second).Format(time.RFC3339Nano)}
	if publish(filepath.Join(jobDir, "job.json"), job) != nil {
		t.Fatal("job")
	}
	var actorJob map[string]any
	actorPath := filepath.Join(f.r.JobsPath, "link-worker", "job.json")
	if readJSON(actorPath, 1<<20, &actorJob) != nil {
		t.Fatal("actor")
	}
	actorJob["project"], actorJob["registered_cwd"], actorJob["launch_key"] = "cc-pc-tools-rs", f.r.Config.Home, "actor-key"
	if publish(actorPath, actorJob) != nil {
		t.Fatal("actor")
	}
	backend := f.r.Run
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path == "git" {
			return strings.Repeat("b", 40), nil
		}
		return backend(ctx, path, args...)
	}
	repairLiveFixture(t, f, nil)
	args[0] = "source-repair"
	for i, arg := range args {
		if arg == "--source-sha256" {
			args[i+1] = ordinaryVersion(f.r.State.Requests[f.replies[0].Message])
		}
	}
	args = append(args, "--body-sha256", bytesHash([]byte(f.replies[0].Body)))
	return f, args, source
}

func repairLiveFixture(t *testing.T, f *conversationFixture, change func(map[string]any)) {
	t.Helper()
	server(t, f.r, func(w http.ResponseWriter, req *http.Request) {
		reply := f.replies[0]
		if req.URL.Path != "/channels/"+f.q.Thread+"/messages/"+reply.Message {
			t.Errorf("unexpected API %s", req.URL.Path)
			w.WriteHeader(404)
			return
		}
		post := map[string]any{"id": reply.Message, "channel_id": f.q.Thread, "author": map[string]any{"id": f.r.State.Destination.Owner}, "content": reply.Body, "timestamp": reply.Created, "edited_timestamp": f.edited[reply.Message], "type": 0, "attachments": []any{map[string]any{"id": "501", "size": 123, "filename": "synthetic.png", "content_type": "image/png"}}}
		if change != nil {
			change(post)
		}
		respond(w, post)
	})
}

func TestConversationSourceRepairFinishedIntakeKeepsForeignBoundary(t *testing.T) {
	f, args, original := sourceRepairFixture(t)
	reply := f.replies[0]
	proof, err := f.r.conversationProof(context.Background(), f.q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.r.captureConversationReply(context.Background(), f.q, proof, reply); err == nil || !strings.Contains(err.Error(), "existing receipt association differs") {
		t.Fatal("ordinary foreign boundary changed", err)
	}
	ordinary, _ := json.Marshal(f.r.State.Requests[reply.Message])
	intake, _ := f.r.todo(44)
	if code, result := intakeLinkCLI(t, f, args); code != 0 || result["linked"] != true || result["application_inferred"] != false {
		t.Fatal("finite repair did not deliver", code, result)
	}
	current, _ := json.Marshal(f.r.State.Requests[reply.Message])
	saved, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "requests", reply.Message+".json"))
	intakeAfter, _ := f.r.todo(44)
	if !bytes.Equal(ordinary, current) || !bytes.Equal(original, saved) || canonicalHash(intake) != canonicalHash(intakeAfter) || f.close != 0 {
		t.Fatal("repair stole ordinary ownership or closed question")
	}
	row, _ := f.r.todo(f.q.Todo)
	if !strings.Contains(row.Note, "<discord-owner-answer>\n"+reply.Body+"\n</discord-owner-answer>") {
		t.Fatal("full instruction missing")
	}
	before, _ := os.ReadFile(f.r.TodoPath)
	if code, _ := intakeLinkCLI(t, f, args); code != 0 {
		t.Fatal("replay")
	}
	after, _ := os.ReadFile(f.r.TodoPath)
	if !bytes.Equal(before, after) {
		t.Fatal("duplicate delivery")
	}
}

func repairMutateRow(t *testing.T, f *conversationFixture, id uint64, change func(map[string]any)) {
	t.Helper()
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("db")
	}
	for _, item := range db["items"].([]any) {
		row := item.(map[string]any)
		if row["id"] == float64(id) {
			change(row)
		}
	}
	if publish(f.r.TodoPath, db) != nil {
		t.Fatal("db")
	}
}

func repairMutateJob(t *testing.T, f *conversationFixture, id string, change func(map[string]any)) {
	t.Helper()
	path := filepath.Join(f.r.JobsPath, id, "job.json")
	var job map[string]any
	if readJSON(path, 1<<20, &job) != nil {
		t.Fatal("job")
	}
	change(job)
	if publish(path, job) != nil {
		t.Fatal("job")
	}
}

func TestConversationSourceRepairRejectsUnrelatedAndUnownedInputs(t *testing.T) {
	for _, kind := range []string{"foreign-intake", "foreign-conversation", "question", "decision", "sender", "body", "expected-body", "expected-source", "edit", "live-owner", "live-thread", "live-created", "live-reference", "bot", "type", "unknown-attachments", "attachment-secret", "running", "queued", "wrong-job-todo", "wrong-job-key", "missing-exit", "missing-finish", "failed", "reservation", "different-wait", "worker", "intake-spec", "actor-spec", "actor-job", "actor-source", "stopped-origin", "canonical-conflict"} {
		t.Run(kind, func(t *testing.T) {
			f, args, _ := sourceRepairFixture(t)
			message := f.replies[0].Message
			switch kind {
			case "foreign-intake":
				f.r.State.Requests[message].Todo = 45
			case "foreign-conversation":
				f.r.State.Requests[message].Conversation = "other-question"
			case "question":
				f.replies[0].Question = "999"
			case "decision":
				f.decision = json.RawMessage(`{"action":"other","todo_id":42}`)
			case "sender":
				index, _ := f.r.loadConversations()
				q := index.Questions[f.q.RequestID]
				q.SenderID = "different-sender"
				index.Questions[f.q.RequestID] = q
				if publish(f.r.conversationPath(), index) != nil {
					t.Fatal("index")
				}
			case "body":
				f.replies[0].Body = "b"
			case "expected-body":
				args[len(args)-1] = strings.Repeat("c", 64)
			case "expected-source":
				for i, arg := range args {
					if arg == "--source-sha256" {
						args[i+1] = strings.Repeat("c", 64)
					}
				}
			case "edit":
				f.edited = map[string]string{message: f.r.Now().Format(time.RFC3339Nano)}
			case "live-owner":
				repairLiveFixture(t, f, func(p map[string]any) { p["author"] = map[string]any{"id": "999"} })
			case "bot":
				repairLiveFixture(t, f, func(p map[string]any) { p["author"] = map[string]any{"id": f.r.State.Destination.Owner, "bot": true} })
			case "live-thread":
				repairLiveFixture(t, f, func(p map[string]any) { p["channel_id"] = "999" })
			case "live-created":
				repairLiveFixture(t, f, func(p map[string]any) { p["timestamp"] = f.r.Now().Add(time.Second).Format(time.RFC3339Nano) })
			case "live-reference":
				repairLiveFixture(t, f, func(p map[string]any) { p["message_reference"] = map[string]any{"channel_id": "999"} })
			case "type":
				repairLiveFixture(t, f, func(p map[string]any) { p["type"] = 7 })
			case "unknown-attachments":
				repairLiveFixture(t, f, func(p map[string]any) { delete(p, "attachments") })
			case "attachment-secret":
				repairLiveFixture(t, f, func(p map[string]any) { p["attachments"].([]any)[0].(map[string]any)["filename"] = "fixture-token" })
			case "running", "queued":
				repairMutateJob(t, f, "intake-worker", func(j map[string]any) { j["status"] = kind })
			case "wrong-job-todo":
				repairMutateJob(t, f, "intake-worker", func(j map[string]any) { j["todo_id"] = 45 })
			case "wrong-job-key":
				repairMutateJob(t, f, "intake-worker", func(j map[string]any) { j["launch_key"] = "foreign-key" })
			case "missing-exit":
				repairMutateJob(t, f, "intake-worker", func(j map[string]any) { delete(j, "exit_code") })
			case "missing-finish":
				repairMutateJob(t, f, "intake-worker", func(j map[string]any) { delete(j, "finished_at") })
			case "failed":
				repairMutateJob(t, f, "intake-worker", func(j map[string]any) { j["status"], j["exit_code"] = "failed", 1 })
			case "reservation":
				repairMutateRow(t, f, 44, func(row map[string]any) { row["scheduler"].(map[string]any)["phase"] = "reserved" })
			case "different-wait":
				repairMutateRow(t, f, 44, func(row map[string]any) { row["wait_on"] = []uint64{45} })
			case "worker":
				repairMutateRow(t, f, 44, func(row map[string]any) { row["worker_receipt"] = map[string]any{"job": "unknown-worker"} })
			case "intake-spec":
				if os.WriteFile(filepath.Join(f.r.Config.Home, "intake-spec.md"), []byte("different source"), 0600) != nil {
					t.Fatal("spec")
				}
			case "actor-spec":
				if os.WriteFile(filepath.Join(f.r.Config.Home, "link-spec.md"), []byte("different question"), 0600) != nil {
					t.Fatal("spec")
				}
			case "actor-job":
				t.Setenv("CC_JOB_ID", "different-worker")
			case "actor-source":
				f.deny = true
			case "stopped-origin":
				f.mutateTodo(t, func(row map[string]any) { row["owner_stopped"] = true })
			case "canonical-conflict":
				path := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", message+".json")
				if writeConversationOnce(path, []byte(`{"text":"different"}`)) != nil {
					t.Fatal("canonical")
				}
			}
			before, _ := os.ReadFile(f.r.TodoPath)
			ordinary, _ := json.Marshal(f.r.State.Requests[message])
			stateBefore, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "unattended-state.json"))
			if code, _ := intakeLinkCLI(t, f, args); code == 0 {
				t.Fatal("rejected input accepted", kind)
			}
			after, _ := os.ReadFile(f.r.TodoPath)
			current, _ := json.Marshal(f.r.State.Requests[message])
			stateAfter, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "unattended-state.json"))
			if !bytes.Equal(before, after) || !bytes.Equal(ordinary, current) || !bytes.Equal(stateBefore, stateAfter) || len(f.r.State.ConversationCaptures) != 0 || f.close != 0 {
				t.Fatal("rejection changed source, state, ownership or question")
			}
		})
	}
}

func TestConversationSourceRepairFailureAndLostResponseResume(t *testing.T) {
	for _, failure := range []string{"canonical", "audit", "state", "delivery", "lost-answer-response"} {
		t.Run(failure, func(t *testing.T) {
			f, args, source := sourceRepairFixture(t)
			message := f.replies[0].Message
			base := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", message)
			path := base + ".json"
			if failure == "audit" {
				path = base + ".source-repair.json"
			}
			if failure == "state" {
				path = filepath.Join(f.r.Config.Home, "unattended-state.json")
				if os.Rename(path, path+".before") != nil {
					t.Fatal("state")
				}
			}
			backend := f.r.Run
			if failure == "canonical" || failure == "audit" || failure == "state" {
				if os.MkdirAll(path, 0700) != nil {
					t.Fatal("failure fixture")
				}
			} else {
				f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
					if path == conversationTodo && args[0] == "edit" && failure == "delivery" {
						return "", errors.New("synthetic save failure")
					}
					out, err := backend(ctx, path, args...)
					if path == conversationTodo && args[0] == "question-answer" && err == nil && failure == "lost-answer-response" {
						return "", errors.New("synthetic response lost after commit")
					}
					return out, err
				}
			}
			if code, _ := intakeLinkCLI(t, f, args); code == 0 {
				t.Fatal("failure reported success")
			}
			if failure == "canonical" || failure == "audit" || failure == "state" {
				if os.Remove(path) != nil {
					t.Fatal("restore")
				}
				if failure == "state" && os.Rename(path+".before", path) != nil {
					t.Fatal("restore")
				}
			}
			f.r.Run = backend
			var restarted State
			if readJSON(filepath.Join(f.r.Config.Home, "unattended-state.json"), maxState, &restarted) != nil {
				t.Fatal("restart")
			}
			f.r.State = &restarted
			if code, _ := intakeLinkCLI(t, f, args); code != 0 {
				t.Fatal("resume failed", failure)
			}
			if code, result := intakeLinkCLI(t, f, []string{"source-verify", "--request-id", f.q.RequestID, "--message-id", message}); code != 0 || result["canonical_current"] != true {
				t.Fatal("source verification after restart", code, result)
			}
			original, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "requests", message+".json"))
			row, _ := f.r.todo(42)
			if !bytes.Equal(source, original) || len(row.QuestionAnswers) != 1 || strings.Count(row.Note, "<discord-owner-answer>") != 1 || !bytes.Contains(f.r.State.ConversationCaptures[message].ConversationAttachmentMetadata, []byte("synthetic.png")) {
				t.Fatal("source/metadata lost or answer repeated")
			}
		})
	}
}

func TestConversationSourceRepairNativePreservesRunningOriginAndLaterStop(t *testing.T) {
	f := conversationNativeTest(t)
	f.mutateTodo(t, func(row map[string]any) {
		row["resume_generation"] = 7
		row["worker_receipt"] = map[string]any{"job": "worker-42", "generation": 7}
		row["scheduler"] = map[string]any{"job": "worker-42", "key": "origin-key", "phase": "running"}
	})
	f, args, source := prepareSourceRepairFixture(t, f)
	workerPath := filepath.Join(f.r.JobsPath, "worker-42", "job.json")
	if os.MkdirAll(filepath.Dir(workerPath), 0700) != nil || publish(workerPath, map[string]any{"id": "worker-42", "todo_id": 42, "launch_key": "origin-key", "status": "running"}) != nil {
		t.Fatal("running original job")
	}
	workerBefore, _ := os.ReadFile(workerPath)
	before, _ := f.r.conversationTask(context.Background(), 42)
	if code, _ := intakeLinkCLI(t, f, args); code != 0 {
		t.Fatal("native delivery failed")
	}
	after, _ := f.r.conversationTask(context.Background(), 42)
	if after.Status != "todo" || string(before.ResumeGeneration) != string(after.ResumeGeneration) || before.Worker.Job != after.Worker.Job || before.Scheduler.Job != after.Scheduler.Job || len(after.QuestionAnswers) != 1 {
		t.Fatalf("origin preservation: status=%s generation=%s/%s worker=%s/%s scheduler=%s/%s deliveries=%d answers=%d", after.Status, before.ResumeGeneration, after.ResumeGeneration, before.Worker.Job, after.Worker.Job, before.Scheduler.Job, after.Scheduler.Job, len(after.QuestionDeliveries), len(after.QuestionAnswers))
	}
	workerAfter, _ := os.ReadFile(workerPath)
	var raw struct {
		Scheduler struct {
			Instruction string `json:"owner_instruction"`
		} `json:"scheduler"`
	}
	if json.Unmarshal(after.conversationState, &raw) != nil || !strings.Contains(raw.Scheduler.Instruction, "<discord-owner-answer>\n"+f.replies[0].Body+"\n</discord-owner-answer>") || !bytes.Equal(workerBefore, workerAfter) {
		t.Fatal("full instruction missing or active worker changed")
	}
	bytesBefore, _ := os.ReadFile(f.r.TodoPath)
	if code, _ := intakeLinkCLI(t, f, args); code != 0 {
		t.Fatal("native replay failed")
	}
	bytesAfter, _ := os.ReadFile(f.r.TodoPath)
	if !bytes.Equal(bytesBefore, bytesAfter) {
		t.Fatal("native delivery repeated")
	}
	f.mutateTodo(t, func(row map[string]any) { row["owner_stopped"] = true; row["status"] = "block" })
	bytesBefore, _ = os.ReadFile(f.r.TodoPath)
	if code, _ := intakeLinkCLI(t, f, args); code == 0 {
		t.Fatal("later stop ignored")
	}
	bytesAfter, _ = os.ReadFile(f.r.TodoPath)
	original, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "requests", f.replies[0].Message+".json"))
	if !bytes.Equal(bytesBefore, bytesAfter) || !bytes.Equal(source, original) {
		t.Fatal("later stop or ordinary source changed")
	}
}

func TestConversationSourceRepairRechecksIntakeAfterFreshAPI(t *testing.T) {
	f, args, _ := sourceRepairFixture(t)
	var once sync.Once
	repairLiveFixture(t, f, func(map[string]any) {
		once.Do(func() { repairMutateJob(t, f, "intake-worker", func(job map[string]any) { job["status"] = "running" }) })
	})
	before, _ := os.ReadFile(f.r.TodoPath)
	if code, _ := intakeLinkCLI(t, f, args); code == 0 {
		t.Fatal("intake resumed during API acquisition")
	}
	after, _ := os.ReadFile(f.r.TodoPath)
	if !bytes.Equal(before, after) || len(f.r.State.ConversationCaptures) != 0 {
		t.Fatal("resumed intake was claimed")
	}
}

func TestConversationSourceRepairCanonicalComesFromAPIAndKeepsFullMetadata(t *testing.T) {
	f, args, _ := sourceRepairFixture(t)
	path := filepath.Join(f.r.Config.Home, "requests", f.replies[0].Message+".json")
	retained := []byte("synthetic original retained without parsing or copying")
	if os.WriteFile(path, retained, 0600) != nil {
		t.Fatal("original")
	}
	if code, _ := intakeLinkCLI(t, f, args); code != 0 {
		t.Fatal("fresh API capture unavailable")
	}
	original, _ := os.ReadFile(path)
	canonical, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", f.replies[0].Message+".json"))
	var source Request
	if !bytes.Equal(original, retained) || json.Unmarshal(canonical, &source) != nil || source.Text != f.replies[0].Body || bytes.Contains(canonical, retained) || !bytes.Contains(source.ConversationAttachmentMetadata, []byte("synthetic.png")) || source.Attachments == nil || len(*source.Attachments) != 1 {
		t.Fatal("canonical copied an unverified original or dropped API metadata")
	}
	// Metadata edits must not silently reuse the previous canonical source.
	repairLiveFixture(t, f, func(post map[string]any) { post["attachments"].([]any)[0].(map[string]any)["filename"] = "edited.png" })
	if code, _ := intakeLinkCLI(t, f, []string{"source-verify", "--request-id", f.q.RequestID, "--message-id", f.replies[0].Message}); code == 0 {
		t.Fatal("changed attachment metadata reused old source")
	}
	f.replies[0].Body = "edited reply"
	proof, err := f.r.conversationProof(context.Background(), f.q)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "unattended-state.json"))
	if _, err := f.r.captureConversationReply(context.Background(), f.q, proof, f.replies[0]); err == nil {
		t.Fatal("poll overwrote repaired source")
	}
	if code, _ := intakeLinkCLI(t, f, []string{"source-reacquire", "--request-id", f.q.RequestID, "--message-id", f.replies[0].Message}); code == 0 {
		t.Fatal("reacquisition replaced repaired source")
	}
	after, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "unattended-state.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("different version modified repaired state")
	}
}

func TestConversationSourceRepairResumeInNewQualifiedActorJob(t *testing.T) {
	for _, published := range []bool{false, true} {
		t.Run(fmt.Sprint(published), func(t *testing.T) {
			f, args, _ := sourceRepairFixture(t)
			state := filepath.Join(f.r.Config.Home, "unattended-state.json")
			if !published {
				if os.Rename(state, state+".before") != nil || os.Mkdir(state, 0700) != nil {
					t.Fatal("save failure")
				}
			}
			code, _ := intakeLinkCLI(t, f, args)
			if published && code != 0 || !published && code == 0 {
				t.Fatal("initial call", code)
			}
			if !published {
				if os.Remove(state) != nil || os.Rename(state+".before", state) != nil {
					t.Fatal("restore")
				}
			}
			linkPath := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", f.replies[0].Message+".intake-link.json")
			linkBefore, err := os.ReadFile(linkPath)
			if err != nil {
				t.Fatal(err)
			}
			repairMutateJob(t, f, "link-worker", func(job map[string]any) {
				job["status"], job["exit_code"], job["started_at"], job["finished_at"] = "failed", 1, f.r.Now().Add(-time.Minute).Format(time.RFC3339Nano), f.r.Now().Add(-time.Second).Format(time.RFC3339Nano)
			})
			oldDir := filepath.Join(f.r.JobsPath, "link-worker")
			newDir := filepath.Join(f.r.JobsPath, "resumed-worker")
			if os.Mkdir(newDir, 0700) != nil {
				t.Fatal("new job")
			}
			prompt, err := os.ReadFile(filepath.Join(oldDir, "prompt.md"))
			if err != nil || os.WriteFile(filepath.Join(newDir, "prompt.md"), prompt, 0600) != nil {
				t.Fatal("new prompt")
			}
			if publish(filepath.Join(newDir, "job.json"), map[string]any{"id": "resumed-worker", "todo_id": 43, "project": "cc-pc-tools-rs", "registered_cwd": f.r.Config.Home, "cwd": f.r.Config.Home, "launch_key": "resume-key", "status": "running"}) != nil {
				t.Fatal("new job")
			}
			repairMutateRow(t, f, 43, func(row map[string]any) {
				row["jobs"] = []string{"link-worker", "resumed-worker"}
				row["scheduler"].(map[string]any)["job"] = "resumed-worker"
				row["scheduler"].(map[string]any)["key"] = "resume-key"
			})
			t.Setenv("CC_JOB_ID", "resumed-worker")
			var restarted State
			if readJSON(state, maxState, &restarted) != nil {
				t.Fatal("restart")
			}
			f.r.State = &restarted
			repairMutateJob(t, f, "link-worker", func(job map[string]any) { job["status"] = "running" })
			before, _ := os.ReadFile(state)
			if code, _ := intakeLinkCLI(t, f, args); code == 0 {
				t.Fatal("resumed actor borrowed a still-running first association writer")
			}
			after, _ := os.ReadFile(state)
			if !bytes.Equal(before, after) {
				t.Fatal("active-writer rejection changed state")
			}
			repairMutateJob(t, f, "link-worker", func(job map[string]any) { job["status"] = "failed" })
			if code, _ := intakeLinkCLI(t, f, args); code != 0 {
				t.Fatal("new qualified actor could not resume same repair input")
			}
			linkAfter, _ := os.ReadFile(linkPath)
			if !bytes.Equal(linkBefore, linkAfter) {
				t.Fatal("original audit writer was replaced")
			}
			row, _ := f.r.todo(42)
			if len(row.QuestionAnswers) != 1 || strings.Count(row.Note, "<discord-owner-answer>") != 1 {
				t.Fatal("new actor repeated delivery")
			}
		})
	}
}

func TestConversationSourceRepairPollRejectsChangedAttachmentMetadata(t *testing.T) {
	f, args, _ := sourceRepairFixture(t)
	if code, _ := intakeLinkCLI(t, f, args); code != 0 {
		t.Fatal("initial repair")
	}
	state, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "unattended-state.json"))
	todo, _ := os.ReadFile(f.r.TodoPath)
	repairLiveFixture(t, f, func(post map[string]any) {
		post["attachments"].([]any)[0].(map[string]any)["filename"] = "edited-with-same-body-and-time.png"
	})
	proof, err := f.r.conversationProof(context.Background(), f.q)
	if err != nil {
		t.Fatal(err)
	}
	index, err := f.r.loadConversations()
	if err != nil {
		t.Fatal(err)
	}
	health := Health{}
	f.r.processConversationReplies(context.Background(), index, f.q.RequestID, f.q, proof, &health)
	afterState, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "unattended-state.json"))
	afterTodo, _ := os.ReadFile(f.r.TodoPath)
	if len(health.Errors) == 0 || !bytes.Equal(state, afterState) || !bytes.Equal(todo, afterTodo) {
		t.Fatal("ordinary poll treated changed metadata as the old delivered version", health.Errors)
	}
}
