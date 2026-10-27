package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// All question text and files below are synthetic. Official input verification
// is a mocked boundary here; native ToDo transactions are tested separately.
func currentActorTest(t *testing.T, native bool) *actorFixture {
	t.Helper()
	f := actorTest(t, native)
	f.input.Todo, f.input.ActorTodo, f.input.ActorJob = 3264, 3350, "worker-3350"
	f.decision = json.RawMessage(`{"todo_id":3264,"decision":"direction","scope":{"origin_todo_id":3264,"actor_todo_id":3350}}`)
	if err := os.WriteFile(f.input.DecisionFile, f.decision, 0600); err != nil {
		t.Fatal(err)
	}
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("store")
	}
	rows := db["items"].([]any)
	origin, actor := rows[0].(map[string]any), rows[1].(map[string]any)
	origin["id"], origin["project"], origin["status"], origin["jobs"], origin["wait_on"] = 3264, "localai-web-rs", "wait", []string{}, []uint64{3350}
	actor["id"], actor["project"], actor["jobs"], actor["wait_on"] = 3350, "localai-web-rs", []string{"worker-3350"}, []uint64{4084}
	actor["scheduler"].(map[string]any)["job"] = "worker-3350"
	root := f.r.Config.Home
	for _, row := range []map[string]any{origin, actor} {
		path := filepath.Join(filepath.Dir(f.r.TodoPath), "specs", fmt.Sprintf("%v-0123456789abcdef0123456789abcdef.md", row["id"]))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(fmt.Sprintf("current synthetic assignment %v", row["id"])), 0600); err != nil {
			t.Fatal(err)
		}
		row["spec"] = path
	}
	sourceJob := "20261027-104546-todo-4084"
	sourceSpec := filepath.Join(filepath.Dir(f.r.TodoPath), "specs", "4084-0123456789abcdef0123456789abcdef.md")
	rows[2] = map[string]any{"id": 4084, "project": "cc-pc-tools-rs", "status": "doing", "spec": sourceSpec, "jobs": []string{sourceJob}, "scheduler": map[string]any{"job": sourceJob, "worktree": root}}
	db["projects"] = map[string]string{"localai-web-rs": root, "cc-pc-tools-rs": root}
	if publish(f.r.TodoPath, db) != nil {
		t.Fatal("store")
	}
	for id, todo := range map[string]uint64{"worker-3350": 3350, sourceJob: 4084} {
		project := "localai-web-rs"
		if todo == 4084 {
			project = "cc-pc-tools-rs"
		}
		if publish(filepath.Join(f.r.JobsPath, id, "job.json"), map[string]any{"id": id, "todo_id": todo, "project": project, "registered_cwd": root, "cwd": root, "status": "running"}) != nil {
			t.Fatal("job")
		}
	}
	base := f.r.Run
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if strings.HasSuffix(path, "received_input.exe") {
			var request map[string]any
			if readJSON(argValue(args, "--request-file"), 64<<10, &request) != nil {
				t.Fatal("input request")
			}
			id := uint64(request["todo_id"].(float64))
			row, err := f.r.todo(id)
			if err != nil {
				return "", err
			}
			var spec string
			_ = json.Unmarshal(row.Spec, &spec)
			sha := "98c276cb0fd75854830f3736a810058a47b355b79b44d981990333fba6f85491"
			if id == 3350 {
				b, _ := os.ReadFile(spec)
				sha = bytesHash(b)
			}
			p := map[string]any{"domain": "received-input-v1", "ok": true, "todo_id": id, "job_id": request["job_id"], "source_kind": "scheduler_dispatch", "registered_scope": root, "human_identity_attested": false, "asset_qualified": false, "delegation_edge_recorded": false, "version": map[string]any{"spec_ref": filepath.Base(spec), "spec_sha256": sha, "dispatch_sha256": strings.Repeat("b", 64), "received_sha256": strings.Repeat("c", 64), "binding_sha256": strings.Repeat("d", 64), "parent_edge": nil}}
			b, _ := json.Marshal(p)
			return string(b), nil
		}
		if path == "git" {
			return "https://github.com/AutoDevJapan/local-ai-console.git", nil
		}
		if strings.HasSuffix(path, "gh.exe") {
			return `{"id":1429803672,"full_name":"AutoDevJapan/local-ai-console","owner":{"login":"AutoDevJapan"},"private":true}`, nil
		}
		return base(ctx, path, args...)
	}
	return f
}

func TestCurrentConversationActorPreservesOriginalAndRetry(t *testing.T) {
	f := currentActorTest(t, false)
	before, _ := f.r.todo(3264)
	q := f.ask(t)
	if q.Todo != 3264 || q.Actor == nil || q.Actor.Todo != 3350 || !q.WaitDeferred {
		t.Fatal("current assignment did not retain original question and dependency")
	}
	f.ask(t)
	after, _ := f.r.todo(3264)
	if f.posts != 1 || canonicalHash(before) != canonicalHash(after) {
		t.Fatal("retry resent or changed the tracker")
	}
	var proof map[string]any
	b, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "conversations", q.RequestID, "actor-succession.json"))
	_ = json.Unmarshal(b, &proof)
	if proof["domain"] != "discord-current-assignment-v1" || proof["delegation_edge_recorded"] != false || proof["human_identity_attested"] != false || proof["asset_qualified"] != false {
		t.Fatal("current controller record manufactured a historical edge or asset qualification")
	}
}

func TestCurrentConversationActorRejectsChangedSourceScopeAndStops(t *testing.T) {
	for _, kind := range []string{"actor", "origin", "project", "root", "actor-spec", "origin-spec", "source-input", "source-stop", "actor-stop", "origin-stop", "job", "repository", "unavailable-input", "body", "decision"} {
		t.Run(kind, func(t *testing.T) {
			f := currentActorTest(t, false)
			f.ask(t) // Prove this fixture can send before injecting the failure.
			base := f.r.Run
			switch kind {
			case "actor":
				f.input.ActorTodo = 999
			case "origin":
				f.input.Todo = 3265
			case "project":
				f.mutate(t, 3350, func(row map[string]any) { row["project"] = "other" })
			case "root":
				var db map[string]any
				_ = readJSON(f.r.TodoPath, maxTodo, &db)
				db["projects"].(map[string]any)["localai-web-rs"] = filepath.Join(f.r.Config.Home, "other")
				_ = publish(f.r.TodoPath, db)
			case "actor-spec", "origin-spec":
				id := uint64(3350)
				if kind == "origin-spec" {
					id = 3264
				}
				row, _ := f.r.todo(id)
				var path string
				_ = json.Unmarshal(row.Spec, &path)
				_ = os.WriteFile(path, []byte("changed current spec"), 0600)
			case "source-stop":
				f.mutate(t, 4084, func(row map[string]any) { row["owner_stopped"] = true })
			case "actor-stop":
				f.mutate(t, 3350, func(row map[string]any) { row["owner_stopped"] = true })
			case "origin-stop":
				f.mutate(t, 3264, func(row map[string]any) { row["owner_stopped"] = true })
			case "job":
				f.input.ActorJob = "unregistered-worker"
			case "body":
				_ = os.WriteFile(f.input.BodyFile, []byte("a different question"), 0600)
			case "decision":
				_ = os.WriteFile(f.input.DecisionFile, []byte(`{"todo_id":3264,"decision":"publish"}`), 0600)
			}
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if kind == "repository" && strings.HasSuffix(path, "gh.exe") {
					return `{"id":1,"private":true}`, nil
				}
				if strings.HasSuffix(path, "received_input.exe") {
					if kind == "unavailable-input" {
						return "", errors.New("synthetic unavailable input")
					}
					raw, err := base(ctx, path, args...)
					if kind == "source-input" {
						var p map[string]any
						_ = json.Unmarshal([]byte(raw), &p)
						p["version"].(map[string]any)["spec_sha256"] = strings.Repeat("e", 64)
						b, _ := json.Marshal(p)
						return string(b), err
					}
					return raw, err
				}
				return base(ctx, path, args...)
			}
			before, _ := os.ReadFile(f.r.TodoPath)
			if _, err := f.r.conversationAsk(context.Background(), f.input); err == nil {
				t.Fatal("changed scope accepted")
			}
			after, _ := os.ReadFile(f.r.TodoPath)
			if string(before) != string(after) || f.posts != 1 {
				t.Fatal("refusal changed tasks or resent")
			}
		})
	}
}

func TestCurrentConversationActorNormalResumeKeepsOriginalQuestion(t *testing.T) {
	f := currentActorTest(t, false)
	first := f.ask(t)
	f.mutate(t, 3350, func(row map[string]any) {
		row["jobs"] = []string{"worker-3350", "resumed-3350"}
		row["scheduler"].(map[string]any)["job"] = "resumed-3350"
	})
	_ = publish(filepath.Join(f.r.JobsPath, "resumed-3350", "job.json"), map[string]any{"id": "resumed-3350", "todo_id": 3350, "project": "localai-web-rs", "registered_cwd": f.r.Config.Home, "cwd": f.r.Config.Home, "status": "running"})
	f.input.ActorJob = "resumed-3350"
	q := f.ask(t)
	if !sameConversationActor(first.Actor, q.Actor) || f.posts != 1 {
		t.Fatal("resume changed the original binding")
	}
}

func TestCurrentConversationActorNativeFullReplyReachesTrackerAndWorker(t *testing.T) {
	f := currentActorTest(t, true)
	q := f.ask(t)
	f.q = q
	reply := f.reply(t, 8, "a：この画像で進めてください。\n全文を保ちます。")
	f.replies[0].Todo = 3264
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 {
		t.Fatal(health.Errors)
	}
	for _, id := range []uint64{3264, 3350} {
		row, err := f.r.todo(id)
		if err != nil || row.Status != "wait" || strings.Count(row.Note, "<discord-owner-answer>") != 1 || !strings.Contains(row.Note, reply.Body) || !strings.Contains(row.Note, "<discord-original-question>") || !strings.Contains(row.Note, "author_id="+reply.Author) {
			t.Fatalf("full source missing at %d: %v", id, err)
		}
		if id == 3264 && (len(row.Jobs) != 0 || canonicalHash(row.WaitOn) != canonicalHash([]uint64{3350})) {
			t.Fatal("tracker became a writer or lost dependency")
		}
		if id == 3350 && (!strings.Contains(row.conversationInstruction(), reply.Body) || canonicalHash(row.WaitOn) != canonicalHash([]uint64{4084})) {
			t.Fatal("worker prompt/dependency lost")
		}
	}
	f.r.processConversations(context.Background(), &Health{})
	for _, id := range []uint64{3264, 3350} {
		row, _ := f.r.todo(id)
		if strings.Count(row.Note, "<discord-owner-answer>") != 1 {
			t.Fatal("reply duplicated")
		}
	}
}

func TestCurrentConversationActorImageReadbackKeepsSameAttachment(t *testing.T) {
	for _, verified := range []bool{false, true} {
		t.Run(fmt.Sprint(verified), func(t *testing.T) {
			f := currentActorTest(t, false)
			path := conversationQuestionImagePath(t, f.conversationFixture)
			data := []byte("\x89PNG\r\n\x1a\nsynthetic-current-image")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			f.input.Attachments = []string{path}
			base := f.r.Run
			f.r.Run = func(ctx context.Context, exe string, args ...string) (string, error) {
				if len(args) > 0 && args[0] == "recovery-ask" && argValue(args, "--attach") != path {
					t.Fatal("original attachment was replaced")
				}
				raw, err := base(ctx, exe, args...)
				if len(args) > 0 && args[0] == "recovery-ask" && verified {
					var report map[string]any
					_ = json.Unmarshal([]byte(raw), &report)
					report["image_sha256"] = []string{bytesHash(data)}
					b, _ := json.Marshal(report)
					return string(b), err
				}
				return raw, err
			}
			q, err := f.r.conversationAsk(context.Background(), f.input)
			if !verified {
				if err == nil {
					t.Fatal("missing image readback accepted")
				}
				return
			}
			if err != nil || q.Todo != 3264 || len(q.AttachmentHashes) != 1 || q.AttachmentHashes[0] != bytesHash(data) {
				t.Fatal("image/source binding lost", err)
			}
			f.ask(t)
			if f.posts != 1 {
				t.Fatal("image question duplicated")
			}
		})
	}
}

func TestCurrentConversationActorCanonicalWaitAndOriginalStopRace(t *testing.T) {
	for _, race := range []bool{false, true} {
		t.Run(fmt.Sprint(race), func(t *testing.T) {
			f := currentActorTest(t, true)
			f.ask(t)
			f.mutate(t, 4084, func(row map[string]any) { row["status"] = "done" })
			f.mutate(t, 3350, func(row map[string]any) { row["status"] = "doing" })
			base := f.r.Run
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if race && path == conversationTodo && args[0] == "question-wait" {
					f.mutate(t, 3264, func(row map[string]any) { row["owner_stopped"] = true })
				}
				return base(ctx, path, args...)
			}
			q, err := f.r.conversationAsk(context.Background(), f.input)
			actor, _ := f.r.todo(3350)
			if race {
				if err == nil || actor.Status != "doing" || len(actor.QuestionWait) != 0 {
					t.Fatal("canonical lock ignored original stop")
				}
				return
			}
			if err != nil || q.Phase != "waiting" || actor.Status != "block" {
				t.Fatal("normal current actor wait failed", err)
			}
		})
	}
}

// This opt-in acceptance performs only metadata/registered-input checks. It
// does not load a bot token, open question/image inputs, mutate ToDos or send.
func TestCurrentConversationActorOfficialInputs(t *testing.T) {
	home := os.Getenv("CC_CURRENT_ACTOR_ACCEPTANCE_HOME")
	if home == "" {
		t.Skip("explicit private acceptance workspace required")
	}
	r := &Runner{Config: Config{Home: home}, TodoPath: "D:/CC/Tools/todo/todo.json", JobsPath: "D:/CC/Tools/codex-jobs/jobs", Run: runCommand, Now: time.Now}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	origin, err := r.conversationTask(ctx, 3264)
	if err != nil {
		t.Fatal(err)
	}
	actor, err := r.conversationTask(ctx, 3350)
	if err != nil {
		t.Fatal(err)
	}
	if !r.repairGuard(ctx, actor.Scheduler.Worktree) {
		t.Fatal("official worktree guard unverified")
	}
	if !r.currentConversationRepository(ctx, actor.conversationRoot) {
		t.Fatal("official repository unverified")
	}
	source, err := r.conversationTask(ctx, 4084)
	if err != nil {
		t.Fatal("official request task unavailable")
	}
	if _, err = r.currentConversationInput(ctx, source, currentConversationSourceJob); err != nil {
		t.Fatal("official request input unverified")
	}
	if _, err = r.currentConversationInput(ctx, actor, actor.Scheduler.Job); err != nil {
		t.Fatal("official worker input unverified")
	}
	sj, se := r.repairJob(ctx, source, currentConversationSourceJob)
	if se != nil {
		t.Fatal("official source job projection unverified")
	}
	if sj.Status != "running" && sj.Status != "succeeded" {
		t.Fatal("official source job state unverified")
	}
	if _, err = r.currentConversationSpec(origin); err != nil {
		t.Fatal("official original spec reference unverified")
	}
	if _, err = r.currentConversationSpec(actor); err != nil {
		t.Fatal("official actor spec reference unverified")
	}
	if _, err = r.checkCurrentConversationActor(ctx, origin, actor, actor.Scheduler.Job, nil, nil, false); err != nil {
		t.Fatalf("official current handler unverified: %#v", err)
	}
	binding, err := r.verifyConversationActor(ctx, 3264, 3350, actor.Scheduler.Job, origin, nil)
	if err != nil || binding == nil {
		t.Fatal("official current registration unverified", err)
	}
	var proof map[string]any
	_ = json.Unmarshal(binding.proof, &proof)
	if proof["domain"] != "discord-current-assignment-v1" || proof["delegation_edge_recorded"] != false {
		t.Fatal("official current input misclassified")
	}
}
