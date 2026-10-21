package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAgentResultDelayedResponseIsPendingAndBoundedByParentDeadline(t *testing.T) {
	r, _ := fixture(t)
	p, _ := r.recordAgentResult(resultEvent())
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r.Run = func(child context.Context, _ string, _ ...string) (string, error) {
		if _, ok := child.Deadline(); !ok {
			t.Fatal("delivery has no parent deadline")
		}
		cancel()
		<-child.Done()
		return "", child.Err()
	}
	started := time.Now()
	r.agentResultPhase(ctx, &Health{})
	if time.Since(started) > 2*time.Second {
		t.Fatal("parent waited beyond its finite deadline")
	}
	saved := r.mustAgentResult(t, p.ID)
	if saved.Verified != "" || saved.Attempts != 1 || saved.LastError != "delivery_unverified" || saved.NextAttempt == "" {
		t.Fatal("a delayed response was reported as delivered or erased")
	}
}

func TestAgentResultRepeatedWaitsAreDistinctNewTransitions(t *testing.T) {
	r, _ := fixture(t)
	r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	r.AgentResultGuard = func(context.Context, string) bool { return true }
	e := resultEvent()
	e.Source = "codex_jobs"
	e.Child = "worker-3257"
	path := filepath.Join(r.JobsPath, e.Child, "job.json")
	save := func(status string, waiting bool) {
		t.Helper()
		v := map[string]any{"id": e.Child, "status": status}
		if waiting {
			v["work_wait_on"] = []int{17}
		}
		if publish(path, v) != nil {
			t.Fatal("fixture")
		}
	}
	save("running", false)
	if err := r.watchAgentResult(e); err != nil {
		t.Fatal(err)
	}
	save("queued", true)
	r.observeAgentResults(context.Background(), &Health{})
	save("running", false)
	r.observeAgentResults(context.Background(), &Health{})
	save("queued", true)
	r.observeAgentResults(context.Background(), &Health{})
	files, _ := agentResultFiles(r.Config.Home, "records")
	if len(files) != 2 {
		t.Fatalf("new wait collapsed into an old transition: %d", len(files))
	}
}

func TestAgentResultExternalWaitHasFiniteParentResultDeadline(t *testing.T) {
	r, _ := fixture(t)
	r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	r.AgentResultGuard = func(context.Context, string) bool { return true }
	e := resultEvent()
	e.Source = "codex_jobs"
	e.Child = "worker-3257"
	path := filepath.Join(r.JobsPath, e.Child, "job.json")
	if publish(path, map[string]any{"id": e.Child, "status": "running"}) != nil {
		t.Fatal("fixture")
	}
	before, _ := os.ReadFile(path)
	if r.watchAgentResult(e) != nil {
		t.Fatal("watch")
	}
	now := r.Now().Add(2 * time.Hour)
	r.Now = func() time.Time { return now }
	r.observeAgentResults(context.Background(), &Health{})
	files, _ := agentResultFiles(r.Config.Home, "records")
	if len(files) != 1 || r.mustAgentResult(t, files[0]).Event.Reason != "result_wait_limit" {
		t.Fatal("parent waited without a finite result")
	}
	r.observeAgentResults(context.Background(), &Health{})
	again, _ := agentResultFiles(r.Config.Home, "records")
	after, _ := os.ReadFile(path)
	if len(again) != 1 || string(before) != string(after) {
		t.Fatal("deadline repeated a notice or changed the worker")
	}
}

func TestAgentResultObservationRefusalReturnsUnknownOnceWithoutRewritingChild(t *testing.T) {
	r, _ := fixture(t)
	r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	allow := true
	r.AgentResultGuard = func(context.Context, string) bool { return allow }
	e := resultEvent()
	e.Source = "codex_jobs"
	e.Child = "worker-3257"
	path := filepath.Join(r.JobsPath, e.Child, "job.json")
	if publish(path, map[string]any{"id": e.Child, "status": "running"}) != nil {
		t.Fatal("fixture")
	}
	before, _ := os.ReadFile(path)
	if err := r.watchAgentResult(e); err != nil {
		t.Fatal(err)
	}
	allow = false
	for range 2 {
		r.observeAgentResults(context.Background(), &Health{})
	}
	files, _ := agentResultFiles(r.Config.Home, "records")
	if len(files) != 1 {
		t.Fatal("observation failure not returned once")
	}
	p := r.mustAgentResult(t, files[0])
	if p.Event.State != "unverified" || p.Event.Reason != "observation_unavailable" {
		t.Fatal("observation refusal was made a child failure")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("observer changed child state")
	}
}

func TestAgentResultOrdinarySenderKeepsOnePostWhenResponseIsLost(t *testing.T) {
	r, _ := fixture(t)
	p, _ := r.recordAgentResult(resultEvent())
	posts := map[string]string{}
	first := true
	attempts := 0
	r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		attempts++
		key := argValue(args, "--request-id")
		if key == "" || argValue(args, "--thread") != "" {
			t.Fatal("normal stable delivery ID or ordinary topic route lost")
		}
		posts[key] = "721"
		if first {
			first = false
			return "", errors.New("response lost after normal sender posted")
		}
		b, _ := json.Marshal(map[string]any{"domain": "new-owner-recovery-messages", "delivery": "verified", "thread_id": "610", "message_id": posts[key]})
		return string(b), nil
	}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		m := resultReadback(r, p, true)
		m["id"] = "721"
		respond(w, m)
	})
	now := r.Now()
	r.Now = func() time.Time { return now }
	r.agentResultPhase(context.Background(), &Health{})
	if r.mustAgentResult(t, p.ID).Verified != "" {
		t.Fatal("reservation treated as delivery")
	}
	now = now.Add(time.Hour)
	restarted := *r
	restarted.agentResultPhase(context.Background(), &Health{})
	if len(posts) != 1 || attempts != 2 || restarted.mustAgentResult(t, p.ID).Message != "721" {
		t.Fatal("lost response did not use ordinary idempotent sender")
	}
}

func TestAgentResultCursorReachesPendingAfterCompletedHistory(t *testing.T) {
	r, _ := fixture(t)
	for i := 0; i < 20; i++ {
		e := resultEvent()
		e.Transition = string(rune('a'+i)) + "-transition"
		p, err := r.recordAgentResult(e)
		if err != nil {
			t.Fatal(err)
		}
		p.Verified = r.Now().Format(time.RFC3339Nano)
		if publish(r.agentResultPath(p.ID), p) != nil {
			t.Fatal("fixture")
		}
	}
	pending, _ := r.recordAgentResult(resultEvent())
	calls := 0
	r.Run = func(context.Context, string, ...string) (string, error) {
		calls++
		return "", errors.New("finite failure")
	}
	for range 3 {
		r.agentResultPhase(context.Background(), &Health{})
	}
	if calls != 1 || r.mustAgentResult(t, pending.ID).Attempts != 1 {
		t.Fatal("old records starved pending delivery")
	}
}
