package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func resultEvent() AgentResultEvent {
	return AgentResultEvent{RequestID: "todo3257-acceptance", Todo: 3257, Project: "cc-pc-tools-rs", Thread: "610",
		Source: "multi_agent_v1", Parent: "parent-session", Child: "child-session", Attempt: "attempt-1", Transition: "stop-1",
		State: "stopped", Reason: "validation_stop", Remaining: "verification", Next: "review_result"}
}

func TestAgentResultParentReceiptForEveryStopAndSuccess(t *testing.T) {
	for _, state := range []string{"completed", "stopped", "rejected", "start_failed", "failed", "timeout", "capacity_wait", "dependency_wait", "cancelled", "lost", "parent_stopped"} {
		t.Run(state, func(t *testing.T) {
			r, _ := fixture(t)
			e := resultEvent()
			e.State = state
			if state == "completed" {
				e.Reason = "completed"
				e.Remaining = "none"
				e.Next = "none"
			}
			if state == "start_failed" {
				e.Child = ""
				e.Parent = ""
				e.Source = "launcher"
			}
			p, err := r.recordAgentResult(e)
			if err != nil {
				t.Fatal(err)
			}
			if p.ParentReceived == "" || p.Verified != "" {
				t.Fatal("receipt must be durable and delivery pending")
			}
			var saved AgentResultRecord
			if readJSON(r.agentResultPath(p.ID), 1<<20, &saved) != nil || saved.Event != e {
				t.Fatal("exact parent receipt lost")
			}
			again, err := r.recordAgentResult(e)
			if err != nil || again.ID != p.ID {
				t.Fatal("same transition duplicated")
			}
			e.Next = "retry"
			if _, err = r.recordAgentResult(e); err == nil {
				t.Fatal("different input reused a transition")
			}
		})
	}
}

func TestAgentResultPublicBodyExcludesPrivateReferencesAndRawErrors(t *testing.T) {
	r, _ := fixture(t)
	e := resultEvent()
	e.PrivateRef = filepath.Join(t.TempDir(), "private-card-token-client-receipt.json")
	p, err := r.recordAgentResult(e)
	if err != nil {
		t.Fatal(err)
	}
	body := agentResultBody(p)
	if strings.Contains(body, e.PrivateRef) || strings.Contains(body, "private-card") || !strings.Contains(body, "残作業:") || !strings.Contains(body, "次の処置:") || !strings.Contains(body, "本人の判断: 不要") {
		t.Fatal(body)
	}
	e.Reason = "blocked by policy quoted token=SECRET"
	if _, err = r.recordAgentResult(e); err == nil {
		t.Fatal("free-text errors accepted")
	}
}

func TestAgentResultDeliverySurvivesParentAndRequiresReadback(t *testing.T) {
	r, _ := fixture(t)
	p, err := r.recordAgentResult(resultEvent())
	if err != nil {
		t.Fatal(err)
	}
	fail := true
	wrong := false
	sends := 0
	message := "711"
	r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] != "recovery-log" {
			t.Fatalf("unexpected command %v", args)
		}
		if argValue(args, "--notify") != "" {
			t.Fatal("notification requested")
		}
		sends++
		if fail {
			return "", errors.New("private upstream error")
		}
		return fmt.Sprintf(`{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"%s","url":"https://discord.com/channels/100/610/%s"}`, message, message), nil
	}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if q.URL.Path != "/channels/610/messages/"+message {
			t.Errorf("wrong readback %s", q.URL.Path)
		}
		m := msg(r.Now(), "610", "400", agentResultBody(p))
		m.ID = message
		m.Author.Bot = true
		m.Flags = 4096 | 4
		if wrong {
			m.Content = "wrong"
		}
		respond(w, m)
	})
	h := Health{}
	r.agentResultPhase(context.Background(), &h)
	saved := r.mustAgentResult(t, p.ID)
	if saved.Verified != "" || saved.Attempts != 1 || saved.LastError != "delivery_unverified" {
		t.Fatal("failed send was reported")
	}
	// A new parent/runner uses only persisted receipts, not in-memory completion.
	copyRunner := *r
	r = &copyRunner
	now := r.Now().Add(time.Hour)
	r.Now = func() time.Time { return now }
	fail = false
	wrong = true
	r.agentResultPhase(context.Background(), &h)
	if r.mustAgentResult(t, p.ID).Verified != "" {
		t.Fatal("mismatched readback accepted")
	}
	wrong = false
	now = now.Add(time.Hour)
	r.agentResultPhase(context.Background(), &h)
	saved = r.mustAgentResult(t, p.ID)
	if saved.Verified == "" || saved.Message != message || saved.URL == "" {
		t.Fatal("verified receipt missing")
	}
	r.agentResultPhase(context.Background(), &h)
	if sends != 3 {
		t.Fatalf("verified event sent again %d", sends)
	}
}

func (r *Runner) mustAgentResult(t *testing.T, id string) AgentResultRecord {
	t.Helper()
	var p AgentResultRecord
	if err := readJSON(r.agentResultPath(id), 1<<20, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestAgentResultRetryIsFiniteAndCanResumeTheSameDelivery(t *testing.T) {
	r, _ := fixture(t)
	p, _ := r.recordAgentResult(resultEvent())
	n := 0
	r.Run = func(context.Context, string, ...string) (string, error) { n++; return "", errors.New("denied") }
	now := r.Now()
	r.Now = func() time.Time { return now }
	for range 8 {
		r.agentResultPhase(context.Background(), &Health{})
		now = now.Add(time.Hour)
	}
	if n != 4 || r.mustAgentResult(t, p.ID).Verified != "" {
		t.Fatalf("unbounded retry: %d", n)
	}
	if err := r.resumeAgentResult(p.ID); err != nil {
		t.Fatal(err)
	}
	r.agentResultPhase(context.Background(), &Health{})
	if n != 5 {
		t.Fatal("same saved transition did not resume")
	}
}

func TestAgentResultExternalWatchOnlyNewTransitions(t *testing.T) {
	r, _ := fixture(t)
	r.JobsPath = filepath.Join(t.TempDir(), "jobs")
	r.AgentResultGuard = func(context.Context, string) bool { return true }
	e := resultEvent()
	e.Source = "codex_jobs"
	e.Child = "worker-3257"
	path := filepath.Join(r.JobsPath, e.Child, "job.json")
	save := func(v map[string]any) {
		t.Helper()
		v["id"] = e.Child
		if err := publish(path, v); err != nil {
			t.Fatal(err)
		}
	}
	save(map[string]any{"status": "succeeded", "last_message": "状態: 完了"})
	if err := r.watchAgentResult(e); err == nil {
		t.Fatal("past terminal registered")
	}
	save(map[string]any{"status": "running"})
	if err := r.watchAgentResult(e); err != nil {
		t.Fatal(err)
	}
	save(map[string]any{"status": "succeeded", "last_message": "状態: 継続中\n例として blocked by policy を引用。", "finished_at": "2026-10-21T00:00:00Z"})
	r.observeAgentResults(context.Background(), &Health{})
	entries, err := os.ReadDir(filepath.Join(r.Config.Home, "agent-results", "records"))
	if err != nil || len(entries) != 1 {
		t.Fatal("new terminal not returned", err)
	}
	var p AgentResultRecord
	if readJSON(filepath.Join(r.Config.Home, "agent-results", "records", entries[0].Name()), 1<<20, &p) != nil {
		t.Fatal("missing receipt")
	}
	if p.Event.State != "stopped" || p.Event.Reason == "policy_rejected" {
		t.Fatal("CLI success or quoted policy changed outcome")
	}
	r.observeAgentResults(context.Background(), &Health{})
	again, _ := os.ReadDir(filepath.Join(r.Config.Home, "agent-results", "records"))
	if len(again) != 1 {
		t.Fatal("same transition duplicated")
	}
}

func TestAgentResultStructuredExternalOutcomes(t *testing.T) {
	cases := []struct{ raw, state, reason string }{
		{`{"status":"succeeded","last_message":"状態: 完了\n検査済み"}`, "completed", "completed"},
		{`{"status":"succeeded","last_message":"README quotes blocked by policy"}`, "stopped", "work_incomplete"},
		{`{"status":"failed","last_failure_kind":"policy","last_error":"private"}`, "rejected", "policy_rejected"},
		{`{"status":"failed","last_error":"blocked by policy example"}`, "failed", "execution_failed"},
		{`{"status":"queued","work_wait_on":[123]}`, "dependency_wait", "dependency"},
		{`{"status":"queued","last_failure_kind":"capacity","capacity_retry_at":"later"}`, "capacity_wait", "capacity"},
		{`{"status":"cancelled"}`, "cancelled", "cancelled"},
		{`{"status":"lost"}`, "lost", "result_missing"},
	}
	for _, c := range cases {
		var job map[string]json.RawMessage
		_ = json.Unmarshal([]byte(c.raw), &job)
		state, reason := agentJobOutcome(job)
		if state != c.state || reason != c.reason {
			t.Fatalf("%s: %s/%s", c.raw, state, reason)
		}
	}
}
