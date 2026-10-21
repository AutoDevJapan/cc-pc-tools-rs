package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentResultRunningWorkerReturnsEachNewCleanupDeferral(t *testing.T) {
	r, _ := fixture(t)
	r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	r.AgentResultGuard = func(context.Context, string) bool { return true }
	e := resultEvent()
	e.Source = "codex_jobs"
	e.Child = "worker-3257"
	path := filepath.Join(r.JobsPath, e.Child, "job.json")
	item := AgentDeferredItem{ID: "owned-test-output-3257", Kind: "build_output", Transition: "hold-1", Reason: "cleanup_busy", Bytes: 4096, Scope: "partial", Next: "retry", PrivateRef: filepath.Join(r.Config.Home, "private-item.json")}
	save := func(items []AgentDeferredItem) {
		t.Helper()
		if publish(path, map[string]any{"id": e.Child, "status": "running", "item_deferred": items}) != nil {
			t.Fatal("fixture")
		}
	}
	save(nil)
	if r.watchAgentResult(e) != nil {
		t.Fatal("watch")
	}
	save([]AgentDeferredItem{item})
	before, _ := os.ReadFile(path)
	r.observeAgentResults(context.Background(), &Health{})
	files, _ := agentResultFiles(r.Config.Home, "records")
	if len(files) != 1 {
		t.Fatal("a running worker's held item did not reach the parent")
	}
	p := r.mustAgentResult(t, files[0])
	body := agentResultBody(p)
	if p.Event.State != "item_deferred" || !strings.Contains(body, "4096 bytes（確認できた範囲のみ）") || !strings.Contains(body, item.ID) || strings.Contains(body, item.PrivateRef) || !strings.Contains(body, "担当全体の終了とは別") {
		t.Fatal(body)
	}
	r.observeAgentResults(context.Background(), &Health{})
	again, _ := agentResultFiles(r.Config.Home, "records")
	after, _ := os.ReadFile(path)
	if len(again) != 1 || string(before) != string(after) {
		t.Fatal("same item transition duplicated or worker changed")
	}
	item.Transition = "hold-2"
	save([]AgentDeferredItem{item})
	r.observeAgentResults(context.Background(), &Health{})
	again, _ = agentResultFiles(r.Config.Home, "records")
	if len(again) != 2 {
		t.Fatal("new item hold did not get its own result")
	}
}

func TestAgentResultDoesNotBackfillOldCleanupHolds(t *testing.T) {
	r, _ := fixture(t)
	r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	r.AgentResultGuard = func(context.Context, string) bool { return true }
	e := resultEvent()
	e.Source = "codex_jobs"
	e.Child = "worker-3257"
	item := AgentDeferredItem{ID: "old-item", Kind: "scratch", Transition: "old-hold", Reason: "cleanup_ownership_unverified", Scope: "unknown", Next: "review_result"}
	path := filepath.Join(r.JobsPath, e.Child, "job.json")
	if publish(path, map[string]any{"id": e.Child, "status": "running", "item_deferred": []AgentDeferredItem{item}}) != nil {
		t.Fatal("fixture")
	}
	if r.watchAgentResult(e) != nil {
		t.Fatal("watch")
	}
	r.observeAgentResults(context.Background(), &Health{})
	files, _ := agentResultFiles(r.Config.Home, "records")
	if len(files) != 0 {
		t.Fatal("historical cleanup hold reposted")
	}
	e = agentDeferredEvent(resultEvent(), item)
	p, err := r.recordAgentResult(e)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(agentResultBody(p), "未計測") {
		t.Fatal("unknown capacity invented")
	}
}
