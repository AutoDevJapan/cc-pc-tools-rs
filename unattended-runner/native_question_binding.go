package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func nativeQuestionProduction() nativeQuestionDeps {
	d := nativeQuestionDeps{Sessions: "C:/Users/makhmeto/.codex/sessions", Now: time.Now, Read: generatedQuestionRead, Clear: generatedQuestionSourceClear}
	d.Records = func(path string, bounds []nativeQuestionEvent) ([]json.RawMessage, error) {
		b := make([]generatedEventBound, len(bounds))
		for i, v := range bounds {
			b[i] = generatedEventBound{Line: v.Line, SHA: v.SHA}
		}
		return generatedQuestionRecords(path, b)
	}
	d.Content = func(e nativeQuestionEvidence, body, decision []byte) error {
		return nativeQuestionContent(e, body, decision, d)
	}
	d.Capture = func(e nativeQuestionEvidence) error { return nativeQuestionCapture(e, d) }
	d.ImageGuard = nativeQuestionImageGuard
	d.Association = nativeQuestionAssociationGap
	d.Bind = func(ctx context.Context, e nativeQuestionEvidence, _ json.RawMessage) error {
		return nativeQuestionBind(ctx, e, d, "D:/CC/Tools/codex-jobs/jobs", "D:/CC/Tools/todo/todo.json", func() (Destination, error) {
			var cfg Config
			var dst Destination
			if readJSON(conversationDefaultConfig, 1<<20, &cfg) != nil || readJSON(cfg.NotifyConfig, 1<<20, &dst) != nil {
				return dst, errConversationMedia
			}
			return dst, nil
		}, nativeQuestionRepositoryGuard)
	}
	return d
}

// The native record and original registered job supply origin, not a caller's
// "approved" field. Older ordinary jobs may lack todo_id; their saved prompt
// must then be the exact native first assignment with an explicit original ID.
func nativeQuestionBind(ctx context.Context, e nativeQuestionEvidence, d nativeQuestionDeps, jobs, todos string, destination func() (Destination, error), guard func(context.Context, string) (bool, error)) error {
	stage := "origin_destination"
	bad := func() error { return generatedFailure(stage, "native_evidence_unqualified") }
	dst, err := destination()
	if err != nil || dst.Owner != e.Scope.Owner || dst.Guild != e.Scope.Guild || dst.Forum != e.Scope.Forum {
		return bad()
	}
	stage = "origin_repository"
	allowed, err := guard(ctx, e.Worktree)
	if err != nil {
		return err
	}
	if !allowed {
		return bad()
	}
	stage = "origin_job"
	var job nativeQuestionOriginJob
	if readJSON(filepath.Join(jobs, e.Job, "job.json"), 1<<20, &job) != nil || job.ID != e.Job || job.Session != e.Session || !conversationMediaSamePath(filepath.FromSlash(job.Cwd), filepath.FromSlash(e.Worktree)) || (job.Todo != 0 && job.Todo != e.Scope.Todo) {
		return bad()
	}
	if nativeQuestionJobStopped(job) {
		return bad()
	}
	stage = "origin_assignment"
	if e.Assignment.Line <= 1 || e.Assignment.Line >= e.Question.Line || !shaPattern.MatchString(e.Assignment.SHA) {
		return bad()
	}
	records, err := d.Records(e.Rollout, []nativeQuestionEvent{e.Assignment})
	if err != nil || len(records) != 1 {
		return bad()
	}
	var assignment nativeQuestionRecord
	if json.Unmarshal(records[0], &assignment) != nil || assignment.Type != "response_item" || assignment.Payload.Type != "message" || assignment.Payload.Role != "user" {
		return bad()
	}
	var text string
	for _, part := range assignment.Payload.Content {
		if part.Type == "input_text" {
			text += part.Text + "\n"
		}
	}
	stage = "origin_prompt"
	prompt, err := d.Read(filepath.Join(jobs, e.Job, "prompt.md"), 128<<10)
	if err != nil || len(prompt) == 0 {
		return bad()
	}
	saved := strings.TrimSpace(strings.TrimPrefix(string(prompt), "\ufeff"))
	if !strings.HasPrefix(strings.TrimSpace(text), saved) {
		return bad()
	}
	first := strings.SplitN(saved, "\n", 2)[0]
	if !nativeQuestionAssignmentTodo(first, e.Scope.Todo) {
		return bad()
	}
	// Every earlier substantive user assignment would make a caller-selected
	// later message unsafe. Reuse the original dedicated-session scan.
	stage = "origin_first_assignment"
	spec := generatedQuestionSpec{Rollout: e.Rollout, Assignment: generatedEventBound{Line: e.Assignment.Line, SHA: e.Assignment.SHA}}
	if generatedFirstAssignment(spec) != nil {
		return bad()
	}
	stage = "origin_task"
	var db struct {
		Items []struct {
			ID           uint64          `json:"id"`
			Status       string          `json:"status"`
			Stopped      bool            `json:"stopped"`
			OwnerStopped bool            `json:"owner_stopped"`
			Cancelled    bool            `json:"cancel_requested"`
			StopKind     string          `json:"stop_kind"`
			Postponed    bool            `json:"owner_postponement_recorded"`
			Decision     json.RawMessage `json:"decision_wait"`
			Scheduler    struct {
				OwnerStop json.RawMessage `json:"owner_stop_job"`
			} `json:"scheduler"`
		}
	}
	if readJSON(todos, maxTodo, &db) != nil {
		return bad()
	}
	found := 0
	for _, row := range db.Items {
		if row.ID == e.Scope.Todo {
			found++
			nonnull := func(b json.RawMessage) bool { return len(b) > 0 && strings.TrimSpace(string(b)) != "null" }
			if row.Stopped || row.OwnerStopped || row.Cancelled || row.Postponed || row.StopKind != "" || nonnull(row.Scheduler.OwnerStop) || nonnull(row.Decision) || (row.Status != "doing" && row.Status != "todo" && row.Status != "wait" && row.Status != "block") {
				return bad()
			}
		}
	}
	if found != 1 || ctx.Err() != nil {
		return bad()
	}
	return nil
}

func nativeQuestionRepositoryGuard(ctx context.Context, path string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := runCommand(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", path)
	if err != nil {
		return false, generatedFailure("origin_repository_command", "native_evidence_unqualified")
	}
	var p struct {
		Client     *bool `json:"client_resource"`
		Unverified *bool `json:"classification_unverified"`
	}
	if len(raw) > 64<<10 || json.Unmarshal([]byte(raw), &p) != nil || p.Client == nil || p.Unverified == nil {
		return false, generatedFailure("origin_repository_metadata", "native_evidence_unqualified")
	}
	if *p.Client {
		return false, generatedFailure("origin_repository_protected", "native_evidence_unqualified")
	}
	if *p.Unverified {
		return false, generatedFailure("origin_repository_unverified", "native_evidence_unqualified")
	}
	return true, nil
}

func nativeQuestionAssignmentTodo(first string, todo uint64) bool {
	// Do not consume the trailing delimiter: it may introduce a second ID.
	id := regexp.MustCompile(`(?:元|ToDo(?:番号)?)([0-9]+)`).FindAllStringSubmatchIndex(first, -1)
	count := 0
	for _, m := range id {
		if first[m[2]:m[3]] != fmtUint(todo) || (m[1] < len(first) && !strings.ContainsAny(first[m[1]:m[1]+1], " \r\n\t") && !strings.HasPrefix(first[m[1]:], "、") && !strings.HasPrefix(first[m[1]:], "。")) {
			return false
		}
		count++
	}
	return count == 1
}

type nativeQuestionOriginJob struct {
	ID           string `json:"id"`
	Todo         uint64 `json:"todo_id"`
	Cwd          string `json:"cwd"`
	Session      string `json:"thread_id"`
	Stopped      bool   `json:"stopped"`
	OwnerStopped bool   `json:"owner_stopped"`
	Cancelled    bool   `json:"cancel_requested"`
	CancelReason string `json:"cancel_reason"`
}

// Match the shared cancellation policy's explicit manual/person stops even
// when the original task itself has not yet received its stop flag.
func nativeQuestionJobStopped(job nativeQuestionOriginJob) bool {
	return job.Stopped || job.OwnerStopped || job.Cancelled || job.CancelReason == "manual" || job.CancelReason == "person"
}
