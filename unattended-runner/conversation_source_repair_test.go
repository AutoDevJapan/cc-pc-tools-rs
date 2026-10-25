package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sourceRepairFixture(t *testing.T) (*conversationFixture, []string, []byte) {
	t.Helper()
	f := conversationTest(t, true)
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
	server(t, f.r, func(w http.ResponseWriter, req *http.Request) {
		reply := f.replies[0]
		if req.URL.Path != "/channels/"+f.q.Thread+"/messages/"+reply.Message {
			t.Errorf("unexpected API %s", req.URL.Path)
			w.WriteHeader(404)
			return
		}
		respond(w, map[string]any{"id": reply.Message, "channel_id": f.q.Thread, "author": map[string]any{"id": f.r.State.Destination.Owner}, "content": reply.Body, "timestamp": reply.Created, "edited_timestamp": f.edited[reply.Message], "type": 0, "attachments": []any{map[string]any{"id": "501", "size": 123, "filename": "synthetic.png", "content_type": "image/png"}}})
	})
	args[0] = "source-repair"
	args = append(args, "--body-sha256", bytesHash([]byte(f.replies[0].Body)))
	return f, args, source
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
