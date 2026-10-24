package main

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

func programQuestionProduction() programQuestionDeps {
	return programQuestionDeps{Sessions: "C:/Users/makhmeto/.codex/sessions", Read: generatedQuestionRead, Guard: conversationMediaLiveGuard, Clear: generatedQuestionSourceClear, Now: time.Now, Bind: func(ctx context.Context, e programQuestionEvidence) (bool, error) {
		// The existing configuration/credential loader stays internal. Only an
		// exact already-bound API receipt is read; no raw receipt is exported.
		var cfg Config
		var dst Destination
		if readJSON(conversationDefaultConfig, 1<<20, &cfg) != nil || readJSON(cfg.NotifyConfig, 1<<20, &dst) != nil {
			return false, errConversationMedia
		}
		state, err := loadIndependentState(cfg, dst)
		if err != nil {
			return false, errConversationMedia
		}
		r := &Runner{Config: cfg, State: state, Token: discordToken(), Client: &http.Client{Timeout: 6 * time.Second}, Base: "https://discord.com/api/v10", Now: time.Now, TodoPath: "D:/CC/Tools/todo/todo.json"}
		if r.Token == "" {
			return false, errConversationMedia
		}
		return programQuestionBinding(ctx, r, "D:/CC/Tools/codex-jobs/jobs", e)
	}}
}

func programQuestionBinding(ctx context.Context, r *Runner, jobs string, e programQuestionEvidence) (bool, error) {
	bad := func() (bool, error) { return false, errConversationMedia }
	dst := r.State.Destination
	if e.Scope.Owner != dst.Owner || e.Scope.Guild != dst.Guild || e.Scope.Forum != dst.Forum {
		return bad()
	}
	row, err := r.todo(e.Scope.Todo)
	if err != nil || row.Project != r.Config.Project || !conversationMediaSamePath(filepath.FromSlash(row.Scheduler.Worktree), filepath.FromSlash(e.Worktree)) {
		return bad()
	}
	var db struct {
		Items []struct {
			ID           uint64          `json:"id"`
			Status       string          `json:"status"`
			Stopped      bool            `json:"stopped"`
			OwnerStopped bool            `json:"owner_stopped"`
			Cancel       bool            `json:"cancel_requested"`
			Postponed    bool            `json:"owner_postponement_recorded"`
			StopKind     string          `json:"stop_kind"`
			Decision     json.RawMessage `json:"decision_wait"`
			Wait         []uint64        `json:"wait_on"`
			Question     json.RawMessage `json:"owner_question_wait"`
			Ready        json.RawMessage `json:"wait_ready"`
			Scheduler    struct {
				OwnerStop json.RawMessage `json:"owner_stop_job"`
			} `json:"scheduler"`
		} `json:"items"`
	}
	if readJSON(r.TodoPath, maxTodo, &db) != nil {
		return bad()
	}
	send, found := false, 0
	nonnull := func(b json.RawMessage) bool { return len(b) > 0 && strings.TrimSpace(string(b)) != "null" }
	found = 0
	for _, task := range db.Items {
		if task.ID != e.Scope.Todo {
			continue
		}
		found++
		if task.Status != "todo" && task.Status != "doing" && task.Status != "wait" {
			return bad()
		}
		found++
		if task.Stopped || task.OwnerStopped || task.Cancel || task.Postponed || task.StopKind != "" || nonnull(task.Scheduler.OwnerStop) || nonnull(task.Decision) || task.Status == "done" {
			return bad()
		}
		switch task.Status {
		case "todo", "doing":
			send = len(task.Wait) == 0 && !nonnull(task.Ready) && !nonnull(task.Question)
		case "wait":
			send = false
		default:
			return bad()
		}
	}
	if found != 1 {
		return bad()
	}
	owned := row.Scheduler.Job == e.Job || row.Worker.Job == e.Job
	for _, raw := range row.Jobs {
		var job string
		if json.Unmarshal(raw, &job) == nil && job == e.Job {
			owned = true
		}
	}
	var job struct {
		ID      string `json:"id"`
		Todo    uint64 `json:"todo_id"`
		Cwd     string `json:"cwd"`
		Session string `json:"thread_id"`
	}
	if !owned || readJSON(filepath.Join(jobs, e.Job, "job.json"), 1<<20, &job) != nil || job.ID != e.Job || job.Todo != e.Scope.Todo || job.Session != e.Session || !conversationMediaSamePath(filepath.FromSlash(job.Cwd), filepath.FromSlash(e.Worktree)) {
		return bad()
	}
	q := r.State.Requests[e.SourceMessage]
	if q == nil || q.Todo != e.Scope.Todo || q.Thread != e.Origin || q.Author != dst.Owner || q.Guild != dst.Guild || q.Source != "discord-api-v10" || ordinaryVersion(q) != e.SourceVersion || !strings.Contains(row.Note, "intake-owner-source:"+q.Message+":"+e.SourceVersion) {
		return bad()
	}
	// Purpose-bound read: derive the path from the verified current state and
	// task marker. This is not a general classifier permit for receipt files.
	path := filepath.Join(r.Config.Home, "requests", "versions", q.Message+"-"+e.SourceVersion+".json")
	b, err := generatedQuestionRead(path, 1<<20)
	var source Request
	if err != nil || json.Unmarshal(b, &source) != nil || ordinaryVersion(&source) != e.SourceVersion || source.Key != q.Key || !r.verifyReportRequest(ctx, &source) {
		return bad()
	}
	// The GETs above take time. Re-observe task controls afterward; a wait or
	// stop established during verification is not released by an older read.
	if readJSON(r.TodoPath, maxTodo, &db) != nil {
		return bad()
	}
	for _, task := range db.Items {
		if task.ID != e.Scope.Todo {
			continue
		}
		if task.Status != "todo" && task.Status != "doing" {
			send = false
		}
		if task.Stopped || task.OwnerStopped || task.Cancel || task.Postponed || task.StopKind != "" || nonnull(task.Decision) || nonnull(task.Scheduler.OwnerStop) {
			return bad()
		}
		if len(task.Wait) > 0 || nonnull(task.Ready) || nonnull(task.Question) {
			send = false
		}
	}
	if found != 1 {
		return bad()
	}
	return send, nil
}
