package main

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIntakeFollowupQuestionEditParksExactGenerationOnce(t *testing.T) {
	r, q, m, calls := ordinaryFixture(t)
	q.Todo, q.Phase = 42, "registered"
	r.KnownQuestionThreads = map[string]bool{q.Thread: true}
	m.Content, m.Edited = "b", r.Now().Add(-time.Second).Format(time.RFC3339Nano)
	var h Health
	r.refreshOrdinary(context.Background(), &h)
	r.processOrdinaryPending(context.Background(), &h)
	if q.Phase != "source_context_pending" || q.NextAction != "verify_exact_original_question_generation" || q.Todo != 42 || *calls != 0 {
		t.Fatal("edited question entered ordinary delivery", q.Phase, q.NextAction, *calls)
	}
	h = Health{}
	r.refreshOrdinary(context.Background(), &h)
	r.processOrdinaryPending(context.Background(), &h)
	if len(h.Errors) != 0 || *calls != 0 || len(q.PriorEdits) != 1 {
		t.Fatal("question edit repeatedly retried or changed", h.Errors)
	}
}

func TestIntakeFollowupLegacyQuestionPendingUsesCanonicalContext(t *testing.T) {
	r, c := fixture(t)
	// This positive legacy case has the same discovery metadata as its live API.
	r.State.Threads["500"] = Thread{ID: "500", Parent: "200", Guild: "100", Type: 11, Owner: "400"}
	m := msg(c.Add(time.Second), "500", "300", "a")
	q := captured(m, "origin_pending", c)
	q.PendingOwner, q.NextAction = "OMB", "read_source_and_bind_original_task"
	r.State.Requests[m.ID] = q
	ledger := map[string]any{"domain": "new-owner-recovery-messages", "events": []any{map[string]any{"event": "verified", "request_id": "original-question", "phase": "question:0", "receipt": map[string]any{"thread_id": "500", "message_id": "600", "author_id": "400", "timestamp": c.Add(-time.Hour).Format(time.RFC3339Nano)}}}}
	if err := publish(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), ledger); err != nil {
		t.Fatal(err)
	}
	if _, err := r.knownBotThreads(); err != nil {
		t.Fatal(err)
	}
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/messages/") {
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Parent: "200", Guild: "100", Type: 11, Owner: "400"})
		}
	})
	adds := 0
	r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "add" {
			adds++
			return "added 42", nil
		}
		b, _ := json.Marshal(map[string]any{"domain": "new-owner-recovery-messages", "source": "discord-api-v10", "thread_id": "500", "owner_id": "300", "question_message_id": "600", "owner_replies": []any{map[string]any{"message_id": m.ID, "author_id": "300", "question_thread_id": "500", "question_message_id": "600", "body": "a"}}})
		return string(b), nil
	}
	var h Health
	r.admit(context.Background(), &h)
	if q.Todo != 42 || adds != 1 || q.QuestionContextVersion != ordinaryVersion(q) {
		t.Fatal("old pending question stranded", q.Phase, h.Errors)
	}
}

func TestIntakeFollowupArchiveBoundarySurvivesTerminalLowerPage(t *testing.T) {
	r, c := fixture(t)
	r.State.Discovery = map[string]DiscoveryPage{}
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/guilds/100/channels":
			respond(w, []any{map[string]any{"id": "800", "guild_id": "100", "type": 15}})
		case "/channels/800":
			respond(w, map[string]any{"id": "800", "guild_id": "100", "type": 15})
		default:
			stamp := c.Add(time.Second)
			id := "700"
			more := true
			if req.URL.Query().Get("before") != "" {
				stamp = c.Add(-time.Second)
				id = "600"
				more = false
			}
			respond(w, map[string]any{"threads": []any{map[string]any{"id": id, "parent_id": "800", "type": 11, "thread_metadata": map[string]any{"archive_timestamp": stamp.Format(time.RFC3339Nano)}}}, "has_more": more})
		}
	})
	r.discoverManagement(context.Background(), nil)
	r.discoverManagement(context.Background(), nil)
	p := r.State.Discovery["management:800/public-archived"]
	if p.Complete || !p.BoundaryUnresolved || p.Next != "" || p.Coverage != "archive_lower_bound_reached" {
		t.Fatal("terminal page erased an unresolved timestamp boundary", p)
	}
}

func TestIntakeFollowupInventoryLastIsMemoryOnly(t *testing.T) {
	p := monthlyChannel{ID: "500", Last: "600"}
	typ := 0
	p.Type = &typ
	m := p.structural()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		ID      string `json:"id"`
		Type    int    `json:"type"`
		Parent  string `json:"parent_id"`
		Archive string `json:"archive_timestamp,omitempty"`
	}
	if m.Last != "600" || strings.Contains(string(b), "last_message_id") || historyStrictJSON(b, &legacy) != nil {
		t.Fatal("v1 audit receipt changed or in-memory Last lost", string(b))
	}
}

func TestIntakeFollowupReplayDenialReplacesOnlyReplayCoverage(t *testing.T) {
	r, _ := fixture(t)
	thread := Thread{ID: "500", Type: 12, Guild: "100", Parent: "700", Managed: true}
	r.State.Discovery = map[string]DiscoveryPage{"read:500": {Complete: true, Coverage: "kept"}, "replay:500": {Complete: true}}
	server(t, r, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(403) })
	if _, err := r.scanPage(context.Background(), thread, true); err == nil {
		t.Fatal("denial accepted")
	}
	p := r.State.Discovery["replay:500"]
	if p.Complete || p.HTTPStatus != 403 || p.FailureStage == "" || r.State.Discovery["read:500"].Coverage != "kept" {
		t.Fatal("replay denial left a successful replay or replaced normal coverage", p)
	}
}

func TestIntakeFollowupMissingOriginalTaskIsBindingHold(t *testing.T) {
	r, q, _, calls := ordinaryFixture(t)
	q.Phase = "captured"
	r.State.Requests["600"] = &Request{Message: "600", Thread: q.Thread, Todo: 99}
	if err := r.register(context.Background(), q); err == nil || q.Phase != "task_binding_pending" || q.PendingOwner != "OMB" || *calls != 0 {
		t.Fatal("missing original repeatedly retried", q.Phase)
	}
}

func TestIntakeFollowupEmptyStarterEditKeepsAdmission(t *testing.T) {
	for _, text := range []string{"new request", "a"} {
		t.Run(text, func(t *testing.T) {
			r, c := fixture(t)
			m := msg(c.Add(time.Second), "500", "300", "")
			m.Channel = m.ID
			q := captured(m, "empty", c)
			knownLegacyThreadForTest(r, q.Thread)
			m.Content, m.Edited = text, c.Add(2*time.Second).Format(time.RFC3339Nano)
			if _, err := r.observeOrdinary(q, historyReadMessage{Message: m}); err != nil {
				t.Fatal(err)
			}
			want := "captured"
			if text == "a" {
				want = "context_pending"
			}
			if q.Phase != want {
				t.Fatal("edited starter stranded", q.Phase)
			}
		})
	}
}

func TestIntakeFollowupPrecapturedEditAcceptsExactLegacyReceipt(t *testing.T) {
	r, q, _, _ := ordinaryFixture(t)
	q.Todo, q.Phase, q.Edited = 42, "steering_saved", r.Now().Add(-time.Second).Format(time.RFC3339Nano)
	path := r.steeringReceiptPath(42, q.Message)
	if err := publish(path, q); err != nil {
		t.Fatal(err)
	}
	b, _ := boundedRegular(path)
	proof := map[string]any{"todo_id": 42, "message_id": q.Message, "job_id": "worker42", "revision": "existing revision", "verification": "existing actual result", "receipt_sha256": bytesHash(b)}
	if err := publish(filepath.Join(r.Config.Home, "applied", q.Message+".json"), proof); err != nil {
		t.Fatal(err)
	}
	row, _ := r.todo(42)
	ok, err := r.observeLegacySteeringApplication(q, row)
	if err != nil || !ok || q.Phase != "steering_applied" {
		t.Fatal("matching current receipt was rejected", ok, err)
	}
}

func TestIntakeFollowupLegacyApplicationStillReportsDoneOnce(t *testing.T) {
	r, q, _, _ := ordinaryFixture(t)
	q.Todo, q.Phase = 42, "steering_saved"
	db := map[string]any{"items": []any{map[string]any{"id": 42, "project": "cc-pc-tools-rs", "status": "done", "jobs": []string{"worker42"}}}}
	if err := publish(r.TodoPath, db); err != nil {
		t.Fatal(err)
	}
	proof := map[string]any{"todo_id": 42, "message_id": q.Message, "job_id": "worker42", "revision": "existing revision", "verification": "existing actual result"}
	if err := publish(filepath.Join(r.Config.Home, "applied", q.Message+".json"), proof); err != nil {
		t.Fatal(err)
	}
	notices := 0
	r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] != "recovery-log" {
			t.Error("unexpected command")
		}
		notices++
		return `{"delivery":"verified","thread_id":"600"}`, nil
	}
	if err := r.results(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.results(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.Phase != "reported" || q.Reported != "complete" || notices != 1 {
		t.Fatal("legacy application hid or duplicated its result", q.Phase, q.Reported, notices)
	}
}
