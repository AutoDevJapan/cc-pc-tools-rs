package main

import (
	"context"
	"net/http"
	"path/filepath"
	"slices"
	"testing"
)

func TestAgentResultOldNotifyIntentAcceptsQuietSenderReceipt(t *testing.T) {
	r, _ := fixture(t)
	p, err := r.recordAgentResult(resultEvent())
	if err != nil {
		t.Fatal(err)
	}
	p.NotifyOwner = true // The exact old reservation remains unchanged.
	if err := publish(r.agentResultPath(p.ID), p); err != nil {
		t.Fatal(err)
	}
	calls := 0
	r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		calls++
		if !slices.Contains(args, "--notify") {
			t.Error("original retry input changed")
		}
		return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"735","requested_notify":true,"effective_notify":false}`, nil
	}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		m := resultReadback(r, p, false)
		m["channel_id"] = "610"
		respond(w, m)
	})
	r.agentResultPhase(context.Background(), &Health{})
	saved := r.mustAgentResult(t, p.ID)
	if saved.Verified == "" || !saved.NotifyOwner || saved.EffectiveNotify == nil || *saved.EffectiveNotify {
		t.Fatal("quiet actual delivery or original notify input was not preserved")
	}
	r.agentResultPhase(context.Background(), &Health{})
	if calls != 1 {
		t.Fatal("verified old intent was resent")
	}
}

func TestAgentResultConcreteOwnerQuestionCannotBeVerifiedAsSilent(t *testing.T) {
	r, _ := fixture(t)
	p, err := r.recordAgentResult(actionRequiredResultEvent())
	if err != nil {
		t.Fatal(err)
	}
	sends := 0
	r.Run = func(context.Context, string, ...string) (string, error) {
		sends++
		return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"735","effective_notify":false}`, nil
	}
	reads := 0
	server(t, r, func(w http.ResponseWriter, q *http.Request) { reads++; respond(w, resultReadback(r, p, false)) })
	if r.deliverAgentResult(context.Background(), &p) == nil || p.Verified != "" {
		t.Fatal("required owner action was verified without its mention")
	}
	if reads != 0 {
		t.Fatal("an inconsistent sender receipt reached final GET verification")
	}
	r.agentResultPhase(context.Background(), &Health{})
	saved := r.mustAgentResult(t, p.ID)
	if saved.LastError != "owner_action_notification_missing" || saved.NextAttempt != "" || saved.Verified != "" {
		t.Fatal("missing owner notification not retained as a specific technical result")
	}
	before := sends
	r.agentResultPhase(context.Background(), &Health{})
	if sends != before {
		t.Fatal("silent concrete question was automatically retried")
	}
}

func TestAgentResultDependencyChangeIsNewSilentResult(t *testing.T) {
	r, _ := fixture(t)
	r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	r.AgentResultGuard = func(context.Context, string) bool { return true }
	e := resultEvent()
	e.Source, e.Child = "codex_jobs", "worker-dependency"
	path := filepath.Join(r.JobsPath, e.Child, "job.json")
	if publish(path, map[string]any{"id": e.Child, "status": "running"}) != nil || r.watchAgentResult(e) != nil {
		t.Fatal("fixture")
	}
	for i, wait := range [][]int{{10}, {10}, {11}} {
		if publish(path, map[string]any{"id": e.Child, "status": "queued", "work_wait_on": wait, "attempts": i + 1, "capacity_retry_at": i}) != nil {
			t.Fatal("fixture")
		}
		r.observeAgentResults(context.Background(), &Health{})
	}
	files, _ := agentResultFiles(r.Config.Home, "records")
	if len(files) != 2 {
		t.Fatalf("changed dependency should report once; got %d", len(files))
	}
	for _, id := range files {
		if r.mustAgentResult(t, id).NotifyOwner {
			t.Fatal("dependency change mentioned owner")
		}
	}
}

func TestAgentResultTrimmedIdenticalChoicesAreRejected(t *testing.T) {
	r, _ := fixture(t)
	e := actionRequiredResultEvent()
	e.OwnerQuestion.ChoiceA, e.OwnerQuestion.ChoiceB = " 保持する ", "保持する"
	if _, err := r.recordAgentResult(e); err == nil {
		t.Fatal("equivalent choices accepted")
	}
}
