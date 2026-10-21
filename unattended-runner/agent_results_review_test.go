package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAgentResultRecordDoesNotWaitForThePeriodicRunnerLockOrState(t *testing.T) {
	r, _ := fixture(t)
	r.Config.Since = r.State.Since
	r.Config.NotifyConfig = filepath.Join(r.Config.Home, "not-needed.json")
	config := filepath.Join(r.Config.Home, "config.json")
	input := filepath.Join(r.Config.Home, "input.json")
	if publish(config, r.Config) != nil || publish(input, resultEvent()) != nil {
		t.Fatal("fixture")
	}
	unlock, err := acquire(filepath.Join(r.Config.Home, "runner.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	var out bytes.Buffer
	if code := agentResultCLI([]string{"--config", config, "record", "--input", input}, &out); code != 0 {
		t.Fatalf("parent could not save while supervisor was busy: %d %s", code, out.String())
	}
}

func TestAgentResultInvalidItemCannotHideTerminalOrOtherValidItems(t *testing.T) {
	r, _ := fixture(t)
	r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	r.AgentResultGuard = func(context.Context, string) bool { return true }
	e := resultEvent()
	e.Source = "codex_jobs"
	e.Child = "worker-3257"
	path := filepath.Join(r.JobsPath, e.Child, "job.json")
	if publish(path, map[string]any{"id": e.Child, "status": "running"}) != nil || r.watchAgentResult(e) != nil {
		t.Fatal("watch")
	}
	good := AgentDeferredItem{ID: "good-item", Kind: "scratch", Transition: "hold-1", Reason: "cleanup_busy", Scope: "unknown", Next: "retry"}
	bad := good
	bad.ID = "bad-item"
	bad.Kind = "not-a-known-kind"
	if publish(path, map[string]any{"id": e.Child, "status": "succeeded", "last_message": "状態: 完了", "item_deferred": []AgentDeferredItem{bad, good}}) != nil {
		t.Fatal("fixture")
	}
	h := Health{}
	r.observeAgentResults(context.Background(), &h)
	files, _ := agentResultFiles(r.Config.Home, "records")
	states := map[string]bool{}
	for _, id := range files {
		states[r.mustAgentResult(t, id).Event.State] = true
	}
	if len(files) != 2 || !states["completed"] || !states["item_deferred"] || len(h.Errors) == 0 {
		t.Fatal("bad item swallowed terminal or valid progress", states, h.Errors)
	}
}

func TestAgentResultFreezesItsBodyAndArchivesVerifiedHistory(t *testing.T) {
	r, _ := fixture(t)
	p, _ := r.recordAgentResult(resultEvent())
	before := agentResultBody(p)
	if p.Body == "" {
		t.Fatal("body was not frozen when parent received the result")
	}
	p.Event.Reason = "timeout" // A later renderer/vocabulary must not regenerate saved prose.
	if agentResultBody(p) != before {
		t.Fatal("saved delivery body was regenerated")
	}
	p.Event = resultEvent()
	p.Verified = r.Now().Format("2006-01-02T15:04:05Z07:00")
	if publish(r.agentResultPath(p.ID), p) != nil {
		t.Fatal("fixture")
	}
	r.agentResultPhase(context.Background(), &Health{})
	active, _ := agentResultFiles(r.Config.Home, "records")
	if len(active) != 0 {
		t.Fatal("verified history still consumes active delivery slots")
	}
	saved, err := r.recordAgentResult(resultEvent())
	if err != nil || saved.Verified == "" {
		t.Fatal("archived result was submitted again", err)
	}
	if _, err = os.Stat(r.agentResultPath(p.ID)); err != nil {
		t.Fatal("status lost its archived receipt")
	}
}

func TestAgentResultWaitLimitDoesNotCreateAnotherIdenticalWait(t *testing.T) {
	r, _ := fixture(t)
	r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	r.AgentResultGuard = func(context.Context, string) bool { return true }
	e := resultEvent()
	e.Source = "codex_jobs"
	e.Child = "worker-3257"
	path := filepath.Join(r.JobsPath, e.Child, "job.json")
	if publish(path, map[string]any{"id": e.Child, "status": "running"}) != nil || r.watchAgentResult(e) != nil {
		t.Fatal("watch")
	}
	if publish(path, map[string]any{"id": e.Child, "status": "queued", "work_wait_on": []int{7}}) != nil {
		t.Fatal("fixture")
	}
	r.observeAgentResults(context.Background(), &Health{})
	now := r.Now().Add(2 * time.Hour)
	r.Now = func() time.Time { return now }
	r.observeAgentResults(context.Background(), &Health{})
	r.observeAgentResults(context.Background(), &Health{})
	files, _ := agentResultFiles(r.Config.Home, "records")
	if len(files) != 2 {
		t.Fatalf("one wait and one wait-limit expected; got %d", len(files))
	}
}

func TestAgentResultReceiverOpenFailureIsNotReportedAsContention(t *testing.T) {
	r, _ := fixture(t)
	if os.MkdirAll(filepath.Join(r.Config.Home, "agent-results", "receive.lock"), 0700) != nil {
		t.Fatal("fixture")
	}
	started := time.Now()
	_, err := r.recordAgentResult(resultEvent())
	if err == nil || errors.Is(err, errAgentReceiverBusy) || time.Since(started) > time.Second {
		t.Fatal("permanent open failure was treated as busy")
	}
}

func TestAgentResultObservationRecoveryDoesNotDuplicateTheWorkerState(t *testing.T) {
	r, _ := fixture(t)
	r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	allow := true
	r.AgentResultGuard = func(context.Context, string) bool { return allow }
	e := resultEvent()
	e.Source = "codex_jobs"
	e.Child = "worker-3257"
	path := filepath.Join(r.JobsPath, e.Child, "job.json")
	if publish(path, map[string]any{"id": e.Child, "status": "running"}) != nil || r.watchAgentResult(e) != nil {
		t.Fatal("watch")
	}
	if publish(path, map[string]any{"id": e.Child, "status": "queued", "work_wait_on": []int{7}}) != nil {
		t.Fatal("fixture")
	}
	r.observeAgentResults(context.Background(), &Health{})
	for range 3 {
		allow = false
		r.observeAgentResults(context.Background(), &Health{})
		allow = true
		r.observeAgentResults(context.Background(), &Health{})
	}
	files, _ := agentResultFiles(r.Config.Home, "records")
	if len(files) != 2 {
		t.Fatalf("one wait and one observation notice expected, got %d", len(files))
	}
	if publish(path, map[string]any{"id": e.Child, "status": "running"}) != nil {
		t.Fatal("fixture")
	}
	r.observeAgentResults(context.Background(), &Health{})
	allow = false
	r.observeAgentResults(context.Background(), &Health{})
	files, _ = agentResultFiles(r.Config.Home, "records")
	if len(files) != 3 {
		t.Fatal("a new worker phase lost its own observation notice")
	}
}
