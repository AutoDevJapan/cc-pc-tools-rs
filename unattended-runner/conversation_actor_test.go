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

func TestConversationActorCLIRequiresCompletePair(t *testing.T) {
	base := conversationAskArgs()
	args := append(append([]string{}, base...), "--scheduler-item", "3277", "--scheduler-job", "20261025-063120-todo-3277")
	if _, err := parseConversationCLI(args); err != nil {
		t.Fatal("a complete actor pair must reach scope verification")
	}
	for _, tail := range [][]string{{"--scheduler-item", "3277"}, {"--scheduler-job", "job-3277"}, {"--scheduler-item", "0", "--scheduler-job", "job-3277"}, {"--scheduler-item", "3277", "--scheduler-job", "../private"}} {
		if _, err := parseConversationCLI(append(append([]string{}, base...), tail...)); err == nil {
			t.Fatal("incomplete or malformed actor accepted")
		}
	}
}

type actorFixture struct {
	*conversationFixture
	proof map[string]any
}

func actorTest(t *testing.T, native bool) *actorFixture {
	t.Helper()
	f := conversationTest(t, true)
	f.input.Todo, f.input.ActorTodo, f.input.ActorJob = 1171, 3277, "worker-3277"
	f.decision = json.RawMessage(`{"action":"choose","todo_id":1171}`)
	if os.WriteFile(f.input.DecisionFile, f.decision, 0600) != nil {
		t.Fatal("decision")
	}
	actorSpec, originSpec := filepath.Join(f.r.Config.Home, "actor.md"), filepath.Join(f.r.Config.Home, "origin.md")
	if os.WriteFile(actorSpec, []byte("finite original 1171 work"), 0600) != nil || os.WriteFile(originSpec, []byte("retained original"), 0600) != nil {
		t.Fatal("specs")
	}
	f.r.JobsPath = filepath.Join(f.r.Config.Home, "isolated-jobs")
	job := map[string]any{"id": "worker-3277", "todo_id": 3277, "cwd": f.r.Config.Home, "status": "succeeded"}
	if publish(filepath.Join(f.r.JobsPath, "worker-3277", "job.json"), job) != nil {
		t.Fatal("job")
	}
	origin := map[string]any{"id": 1171, "project": "GitDo", "status": "recovery_unverified", "spec": originSpec, "note": "元の回答と未完を保持", "jobs": []string{"old-original"}}
	actor := map[string]any{"id": 3277, "project": "GitDo", "status": "wait", "spec": actorSpec, "note": "修復の依存待ちを保持", "jobs": []string{"worker-3277"}, "wait_on": []uint64{3751}, "scheduler": map[string]any{"job": "worker-3277", "worktree": f.r.Config.Home}}
	db := map[string]any{"projects": map[string]string{"GitDo": f.r.Config.Home}, "items": []any{origin, actor, map[string]any{"id": 3751, "project": "GitDo", "status": "doing"}}}
	if publish(f.r.TodoPath, db) != nil {
		t.Fatal("store")
	}
	a := &actorFixture{conversationFixture: f, proof: map[string]any{"domain": "current-product-succession-v1", "verified": true, "origin_todo_id": 3638, "actor_todo_id": 3650, "todo_id": 3277, "project": "GitDo", "registered_path_sha256": conversationPathSHA(f.r.Config.Home), "spec_path_sha256": conversationPathSHA(actorSpec), "spec_sha256": bytesHash([]byte("finite original 1171 work")), "original_registry_recovered": false, "source_kind": "native_user", "source_session": "01a16c0c-18a9-7570-ab4e-9522ffa8d6d0", "source_event_sha256": "e0cff4ef85745ffb93ddcc36d1fb9626c4c098967406d8875a672d58c1684465", "source_at": "2026-10-24T11:26:20.672Z"}}
	backend := f.r.Run
	if native {
		if os.Getenv("CC_CONVERSATION_TODO_BINARY") == "" {
			t.Skip("fresh canonical ToDo binary not supplied")
		}
		f.r.Now = time.Now
	}
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if strings.Contains(path, "scrub391") && args[0] == "--verify-current-succession" {
			b, _ := json.Marshal(a.proof)
			return string(b), nil
		}
		if path != conversationTodo {
			return backend(ctx, path, args...)
		}
		if native {
			cmd := exec.CommandContext(ctx, os.Getenv("CC_CONVERSATION_TODO_BINARY"), args...)
			hide(cmd)
			cmd.Env = append(os.Environ(), "CC_TODO_HOME="+f.r.Config.Home, "CC_TODO_JOBS="+f.r.JobsPath)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Logf("synthetic canonical rejection: %s", out)
			}
			return string(out), err
		}
		if args[0] != "edit" {
			return "", errors.New("canonical wait needs explicit native fixture")
		}
		var db map[string]any
		var expected map[string]any
		var scope struct {
			OriginFile string `json:"origin_state_file"`
		}
		if readJSON(f.r.TodoPath, maxTodo, &db) != nil || readJSON(argValue(args, "--expected-state-file"), maxTodo, &expected) != nil || readJSON(argValue(args, "--scope-file"), maxTodo, &scope) != nil {
			return "", errors.New("snapshot")
		}
		rows := db["items"].([]any)
		var original json.RawMessage
		if readJSON(scope.OriginFile, maxTodo, &original) != nil || canonicalHash(rows[0]) != canonicalHash(original) || canonicalHash(rows[1]) != canonicalHash(expected) {
			return "", errors.New("changed")
		}
		body, err := os.ReadFile(argValue(args, "--note-file"))
		if err != nil {
			return "", err
		}
		rows[1].(map[string]any)["note"] = string(body)
		return "{}", publish(f.r.TodoPath, db)
	}
	return a
}

func (f *actorFixture) mutate(t *testing.T, id uint64, change func(map[string]any)) {
	t.Helper()
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("store")
	}
	for _, v := range db["items"].([]any) {
		row := v.(map[string]any)
		if row["id"] == float64(id) {
			change(row)
		}
	}
	if publish(f.r.TodoPath, db) != nil {
		t.Fatal("save")
	}
}

func TestConversationActorPreservesOriginDependencyAndFullReply(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			f := actorTest(t, native)
			before, _ := f.r.todo(1171)
			q := f.ask(t)
			if q.Todo != 1171 || q.Actor == nil || q.Actor.Todo != 3277 || !q.WaitDeferred || q.Phase != "posted" {
				t.Fatal("original/recipient mapping or wait changed")
			}
			q = f.ask(t)
			if f.posts != 1 || !q.WaitDeferred {
				t.Fatal("same question was resent")
			}
			reply := f.reply(t, 8, "a：元の条件で進めてください。\n追記も省略しないでください。")
			f.replies[0].Todo = 1171
			health := Health{}
			f.r.processConversations(context.Background(), &health)
			if len(health.Errors) != 0 {
				t.Fatal(health.Errors)
			}
			origin, _ := f.r.todo(1171)
			actor, _ := f.r.todo(3277)
			if canonicalHash(origin) != canonicalHash(before) || actor.Status != "wait" || canonicalHash(actor.WaitOn) != canonicalHash([]uint64{3751}) || len(actor.QuestionWait) != 0 {
				t.Fatal("original or dependency was changed")
			}
			if !strings.Contains(actor.Note, "<discord-owner-answer>\n"+reply.Body+"\n</discord-owner-answer>") || !strings.Contains(actor.Note, "origin_todo_id=1171, todo_id=3277") || f.r.State.Requests[reply.Message].Todo != 3277 {
				t.Fatal("full reply did not reach the actual actor")
			}
			f.r.NoticeAttempted = false
			f.r.processConversations(context.Background(), &Health{})
			again, _ := f.r.todo(3277)
			if strings.Count(again.Note, "<discord-owner-answer>") != 1 || f.posts != 1 {
				t.Fatal("reply delivery or question duplicated")
			}
		})
	}
}

func TestConversationActorRejectsForeignChangedOrStoppedScope(t *testing.T) {
	for _, kind := range []string{"actor", "project", "origin", "spec", "root", "source", "job", "owner-stop", "origin-stop", "body", "decision"} {
		t.Run(kind, func(t *testing.T) {
			f := actorTest(t, false)
			f.ask(t) // A proven positive path prevents a vacuous rejection test.
			switch kind {
			case "actor":
				f.input.ActorTodo = 999
			case "project":
				f.mutate(t, 3277, func(row map[string]any) { row["project"] = "other" })
			case "origin":
				f.input.Todo = 1889
			case "spec":
				f.proof["spec_sha256"] = strings.Repeat("a", 64)
			case "root":
				f.proof["registered_path_sha256"] = strings.Repeat("a", 64)
			case "source":
				f.proof["source_kind"] = "assistant_quote"
			case "job":
				f.input.ActorJob = "foreign-worker"
			case "owner-stop":
				f.mutate(t, 3277, func(row map[string]any) { row["owner_stopped"] = true })
			case "origin-stop":
				f.mutate(t, 1171, func(row map[string]any) { row["stopped"] = true })
			case "body":
				if os.WriteFile(f.input.BodyFile, []byte("different original question"), 0600) != nil {
					t.Fatal("body")
				}
			case "decision":
				if os.WriteFile(f.input.DecisionFile, []byte(`{"todo_id":1171,"action":"different"}`), 0600) != nil {
					t.Fatal("decision")
				}
			}
			before, _ := os.ReadFile(f.r.TodoPath)
			if _, err := f.r.conversationAsk(context.Background(), f.input); err == nil {
				t.Fatal("changed scope accepted")
			}
			after, _ := os.ReadFile(f.r.TodoPath)
			if string(before) != string(after) || f.posts != 1 {
				t.Fatal("refusal changed tasks or resent question")
			}
		})
	}
}

func TestConversationActorNewSchedulerJobRetainsOriginalBinding(t *testing.T) {
	f := actorTest(t, false)
	first := f.ask(t)
	f.mutate(t, 3277, func(row map[string]any) {
		row["jobs"] = []string{"worker-3277", "resumed-worker-3277"}
		row["scheduler"].(map[string]any)["job"] = "resumed-worker-3277"
	})
	if publish(filepath.Join(f.r.JobsPath, "resumed-worker-3277", "job.json"), map[string]any{"id": "resumed-worker-3277", "todo_id": 3277, "cwd": f.r.Config.Home, "status": "running"}) != nil {
		t.Fatal("job")
	}
	f.input.ActorJob = "resumed-worker-3277"
	q, err := f.r.conversationAsk(context.Background(), f.input)
	if err != nil || !sameConversationActor(first.Actor, q.Actor) || q.RequestID != first.RequestID || q.Todo != 1171 || f.posts != 1 {
		t.Fatalf("normal resumed job lost the canonical question: %v", err)
	}
}

func TestConversationActorCanonicalWaitAfterDependencyAndOriginStopRace(t *testing.T) {
	for _, race := range []bool{false, true} {
		t.Run(fmt.Sprint(race), func(t *testing.T) {
			f := actorTest(t, true)
			f.ask(t)
			f.mutate(t, 3277, func(row map[string]any) { row["status"] = "doing" })
			f.mutate(t, 3751, func(row map[string]any) { row["status"] = "done" })
			backend := f.r.Run
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if race && path == conversationTodo && args[0] == "question-wait" {
					f.mutate(t, 1171, func(row map[string]any) { row["stopped"] = true })
				}
				return backend(ctx, path, args...)
			}
			q, err := f.r.conversationAsk(context.Background(), f.input)
			actor, _ := f.r.todo(3277)
			if race {
				if err == nil || actor.Status != "doing" || len(actor.QuestionWait) != 0 {
					t.Fatal("origin stop race was not refused under canonical lock")
				}
				return
			}
			if err != nil || q.Phase != "waiting" || q.WaitDeferred || actor.Status != "block" {
				t.Fatalf("recipient wait not registered: %v", err)
			}
			f.q = q
			reply := f.reply(t, 8, "a")
			f.replies[0].Todo = 1171
			health := Health{}
			f.r.processConversations(context.Background(), &health)
			actor, _ = f.r.todo(3277)
			if len(health.Errors) != 0 || actor.Status != "todo" || f.r.State.Requests[reply.Message].Todo != 3277 || len(actor.QuestionAnswers) != 1 {
				t.Fatalf("actual recipient answer failed: %v", health.Errors)
			}
		})
	}
}

func TestConversationActorApplicationKeepsOriginalIdentityAndCloses(t *testing.T) {
	f := actorTest(t, false)
	q := f.ask(t)
	reply := f.reply(t, 8, "a")
	f.replies[0].Todo = 1171
	f.r.processConversations(context.Background(), &Health{})
	if f.close != 0 {
		t.Fatal("receipt alone closed the question")
	}
	var request Request
	path := filepath.Join(f.r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".json")
	b, err := boundedRegular(path)
	if err != nil || json.Unmarshal(b, &request) != nil {
		t.Fatal("source")
	}
	application := map[string]any{"source": "discord-api-v10", "request_id": q.SenderID, "origin_todo_id": 1171, "todo_id": 3277, "thread_id": q.Thread, "question_message_id": q.Message, "message_id": reply.Message, "author_id": reply.Author, "job_id": "worker-3277", "revision": strings.Repeat("a", 40), "applied_at": f.r.Now().Format(time.RFC3339Nano), "completion": "same original scope applied", "reply_sha256": canonicalHash(f.replies[0]), "source_receipt_sha256": bytesHash(b), "verification": map[string]string{"execution": "checked", "origin_saved": "retained", "identity": "same question"}}
	if publish(filepath.Join(f.r.Config.Home, "applied", reply.Message+".json"), application) != nil {
		t.Fatal("application")
	}
	f.r.NoticeAttempted = false
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	index, err := f.r.loadConversations()
	if err != nil || len(health.Errors) != 0 || index.Questions[q.RequestID].Phase != "closed" {
		t.Fatalf("actor application did not close the original question: %v %v", err, health.Errors)
	}
	origin, _ := f.r.todo(1171)
	actor, _ := f.r.todo(3277)
	if origin.Status != "recovery_unverified" || actor.Status != "wait" || canonicalHash(actor.WaitOn) != canonicalHash([]uint64{3751}) {
		t.Fatal("closing changed original work or dependency")
	}
}
