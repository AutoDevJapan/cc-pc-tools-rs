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

func TestIntakeReviewSchemaObservationPreservesUnregisteredPhases(t *testing.T) {
	for _, phase := range []string{"captured", "context_pending", "status_answered", "empty"} {
		t.Run(phase, func(t *testing.T) {
			r, c := fixture(t)
			m := msg(c.Add(time.Second), "500", "300", "same source")
			q := captured(m, phase, c)
			q.Context = "original context"
			typ, attachments := 0, []historyReadAttachment{}
			changed, err := r.observeOrdinary(q, historyReadMessage{Message: m, Type: &typ, Attachments: &attachments})
			if err != nil || changed || q.Phase != phase || q.Context != "original context" || len(q.PriorEdits) != 0 {
				t.Fatalf("schema observation changed saved work: changed=%v phase=%s err=%v", changed, q.Phase, err)
			}
		})
	}
}

func TestIntakeReviewEditBeforeRegistrationKeepsStarterAdmissible(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "old")
	m.Channel = m.ID
	q := captured(m, "captured", c)
	r.State.Requests[m.ID] = q
	m.Content, m.Edited = "corrected original request", c.Add(2*time.Second).Format(time.RFC3339Nano)
	changed, err := r.observeOrdinary(q, historyReadMessage{Message: m})
	if err != nil || !changed || q.Phase != "captured" || len(q.PriorEdits) != 1 {
		t.Fatalf("edited starter lost admission: phase=%s err=%v", q.Phase, err)
	}
}

func TestIntakeReviewExactQuestionReachesRegistrationOnce(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "a")
	q := captured(m, "context_pending", c)
	r.State.Requests[m.ID] = q
	r.KnownQuestionThreads = map[string]bool{"500": true}
	ledger := map[string]any{"domain": "new-owner-recovery-messages", "events": []any{map[string]any{"event": "verified", "request_id": "original-question", "phase": "question:0", "receipt": map[string]any{"thread_id": "500", "message_id": "600", "author_id": "400", "timestamp": c.Format(time.RFC3339Nano)}}}}
	if err := publish(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), ledger); err != nil {
		t.Fatal(err)
	}
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/messages/") {
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Parent: requestForum, Guild: "100", Type: 11, Owner: "400"})
		}
	})
	adds := 0
	r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		if args[0] == "add" {
			adds++
			return "added 42", nil
		}
		if args[0] != "recovery-replies" {
			return "", errors.New("unexpected command")
		}
		b, _ := json.Marshal(map[string]any{"domain": "new-owner-recovery-messages", "source": "discord-api-v10", "thread_id": "500", "owner_id": "300", "question_message_id": "600", "owner_replies": []any{map[string]any{"message_id": m.ID, "author_id": "300", "question_thread_id": "500", "question_message_id": "600", "body": "a"}}})
		return string(b), nil
	}
	var h Health
	r.admit(context.Background(), &h)
	r.admit(context.Background(), &h)
	if adds != 1 || q.Todo != 42 || q.Context == "" || h.Registered != 1 {
		t.Fatalf("exact answer stranded or duplicated: phase=%s adds=%d", q.Phase, adds)
	}
}

func TestIntakeReviewParkedBindingsDoNotConsumeAdmission(t *testing.T) {
	r, c := fixture(t)
	for i := 0; i < 12; i++ {
		q := &Request{Message: fmt.Sprint(i + 1)}
		r.parkUnbound(q)
		r.State.Requests[q.Message] = q
	}
	m := msg(c.Add(time.Second), "500", "300", "new original request")
	m.Channel = m.ID
	q := captured(m, "captured", c)
	r.State.Requests[m.ID] = q
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/messages/") {
			respond(w, m)
		} else {
			respond(w, Thread{ID: m.ID, Parent: requestForum, Guild: "100", Type: 11, Owner: "300"})
		}
	})
	adds := 0
	r.Run = func(context.Context, string, ...string) (string, error) { adds++; return "added 42", nil }
	var h Health
	r.admit(context.Background(), &h)
	if q.Todo != 42 || adds != 1 {
		t.Fatalf("parked bindings consumed admission: phase=%s adds=%d", q.Phase, adds)
	}
}

func TestIntakeReviewTransientTaskReadRetriesWithoutBinding(t *testing.T) {
	r, q, _, _ := ordinaryFixture(t)
	q.Phase = "captured"
	r.State.Requests["600"] = &Request{Thread: q.Thread, Message: "600", Todo: 42}
	path := r.TodoPath
	r.TodoPath = filepath.Join(r.Config.Home, "temporarily-unavailable.json")
	if err := r.register(context.Background(), q); err == nil || q.Phase != "captured" || q.Todo != 0 {
		t.Fatal("temporary read failure became binding hold", q.Phase, err)
	}
	r.TodoPath = path
	if err := r.register(context.Background(), q); err != nil || q.Todo != 42 || q.Phase != "steering_saved" {
		t.Fatal("temporary failure did not resume original routing", err)
	}
}

func TestIntakeReviewRefreshPreservesTerminalDisposition(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "same source")
	q := captured(m, "status_answered", c)
	r.State.Requests[m.ID] = q
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		if strings.Contains(req.URL.Path, "/messages/") {
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Parent: "200", Guild: "100", Type: 11, Owner: "400"})
		}
	})
	var h Health
	r.refreshOrdinary(context.Background(), &h)
	if q.Phase != "status_answered" || len(h.Errors) != 0 {
		t.Fatal("refresh changed disposition", q.Phase, h.Errors)
	}
}

func TestIntakeReviewFailedDiscoveryDoesNotRereadSources(t *testing.T) {
	r, q, _, _ := ordinaryFixture(t)
	q.Phase = "status_answered"
	reads := 0
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/channels/500") {
			reads++
		}
		w.WriteHeader(503)
	})
	r.Config.ManagementIntake = true
	r.Run = func(context.Context, string, ...string) (string, error) { return `{}`, nil }
	r.tick(context.Background())
	if reads != 0 || q.Phase != "status_answered" {
		t.Fatal("source processing continued after discovery failure", reads, q.Phase)
	}
}

func TestIntakeReviewCLILoadsQuestionReceiptsBeforeLongBinding(t *testing.T) {
	for _, available := range []bool{true, false} {
		t.Run(fmt.Sprint(available), func(t *testing.T) {
			r, q, _, calls := ordinaryFixture(t)
			if available {
				ledger := map[string]any{"domain": "new-owner-recovery-messages", "events": []any{map[string]any{"event": "verified", "phase": "question:0", "receipt": map[string]any{"thread_id": q.Thread, "author_id": "400", "timestamp": r.Now().Add(-24 * time.Hour).Format(time.RFC3339Nano)}}}}
				if err := publish(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), ledger); err != nil {
					t.Fatal(err)
				}
			}
			plan := intakeBindInput{Domain: "discord-intake-binding-v1", Message: q.Message, Thread: q.Thread, Author: q.Author, IntakeBinding: IntakeBinding{Todo: 42, Project: "cc-pc-tools-rs", SourceSHA: ordinaryVersion(q), Evidence: "exact original context"}}
			path := filepath.Join(r.Config.Home, "binding.json")
			if err := publish(path, plan); err != nil {
				t.Fatal(err)
			}
			var out strings.Builder
			code := intakeCLIWith([]string{"bind", "--input", path}, &out, func(string, bool) (*Runner, func(), error) { return r, func() {}, nil }, func(context.Context, string) bool { return true })
			if code == 0 || q.Todo != 0 || *calls != 0 || available && !r.KnownQuestionThreads[q.Thread] {
				t.Fatal("CLI omitted exact question load", code, q.Todo, *calls)
			}
		})
	}
}

func TestIntakeReviewManagedScanningHasIndependentBudgetAndHonorsDisable(t *testing.T) {
	r, c := fixture(t)
	for i := 1; i < 20; i++ {
		id := fmt.Sprint(i)
		r.State.Threads[id] = Thread{ID: id, Managed: true}
	}
	m := msg(c.Add(time.Second), "500", "300", "new request")
	r.State.Threads["500"] = Thread{ID: "500"}
	reads := []string{}
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		reads = append(reads, req.URL.Path)
		respond(w, []Message{m})
	})
	var h Health
	r.scanIntakeThreads(context.Background(), &h, false, 8)
	if len(reads) != 1 || reads[0] != "/channels/500/messages" || h.Captured != 1 {
		t.Fatal("managed channels consumed legacy budget", reads, h)
	}
	r.replayManagement(context.Background(), &h)
	for _, p := range reads {
		if p != "/channels/500/messages" {
			t.Fatal("disabled managed channel was read", p)
		}
	}
}

func TestIntakeReviewProductScopeCannotUseOrdinaryDelivery(t *testing.T) {
	r, q, _, calls := ordinaryFixture(t)
	q.Origin = &ProgressOrigin{Scope: productReportScope, Todo: 42}
	plan := IntakeBinding{Todo: 42, Project: "cc-pc-tools-rs", SourceSHA: ordinaryVersion(q), Evidence: "original scope"}
	if err := r.bindOrdinary(context.Background(), q.Message, plan); err == nil {
		t.Fatal("product scope bypassed finite steering binding")
	}
	q.Todo = 42
	if err := r.deliverOrdinary(context.Background(), q); err == nil || *calls != 0 {
		t.Fatal("product scope bypassed finite steering delivery")
	}
	if q.Phase != "source_context_pending" || q.NextAction != "review_original_product_link_and_task_association" {
		t.Fatal("dedicated product disposition was not saved")
	}
}

func TestIntakeReviewOrdinaryDeliveryReachesWorkerSteering(t *testing.T) {
	r, q, _, _ := ordinaryFixture(t)
	q.Origin = &ProgressOrigin{Todo: 42}
	q.ProgressThread = "700"
	plan := IntakeBinding{Todo: 42, Project: "cc-pc-tools-rs", SourceSHA: ordinaryVersion(q), Evidence: "original scope"}
	if err := r.bindOrdinary(context.Background(), q.Message, plan); err != nil {
		t.Fatal(err)
	}
	var steering Request
	if err := readJSON(r.steeringReceiptPath(42, q.Message), maxState, &steering); err != nil || steering.Todo != 42 || ordinaryVersion(&steering) != ordinaryVersion(q) {
		t.Fatal("live worker steering is missing", err)
	}
	files, err := filepath.Glob(filepath.Join(r.Config.Home, "requests", "versions", "*.json"))
	if err != nil || len(files) != 1 {
		t.Fatal(files, err)
	}
	var source Request
	if err := readJSON(files[0], maxState, &source); err != nil || source.Origin != nil || source.ProgressThread != "" || source.Todo != 0 || source.Binding != nil {
		t.Fatal("source receipt contains mutable routing", err)
	}
}

func TestIntakeReviewLegacySteeringApplicationDoesNotRedispatch(t *testing.T) {
	r, q, _, calls := ordinaryFixture(t)
	q.Todo, q.Phase = 42, "steering_saved"
	if err := publish(r.steeringReceiptPath(42, q.Message), q); err != nil {
		t.Fatal(err)
	}
	proof := map[string]any{"todo_id": 42, "message_id": q.Message, "job_id": "worker42", "revision": "legacy revision", "verification": "verified actual result"}
	if err := publish(filepath.Join(r.Config.Home, "applied", q.Message+".json"), proof); err != nil {
		t.Fatal(err)
	}
	var h Health
	r.refreshOrdinary(context.Background(), &h)
	if err := r.results(context.Background()); err != nil {
		t.Fatal(err)
	}
	if q.Phase != "steering_applied" || q.Reported != "applied" || *calls != 0 {
		t.Fatal("legacy application redispatched", q.Phase, *calls, h.Errors)
	}
	if _, err := os.Stat(filepath.Join(r.Config.Home, "requests", "delivery")); !os.IsNotExist(err) {
		t.Fatal("legacy proof created a new delivery")
	}
}

func TestIntakeReviewPendingDeliveryDoesNotWaitForHistoricalRefresh(t *testing.T) {
	r, q, _, calls := ordinaryFixture(t)
	q.Todo, q.Phase = 42, "source_edit_saved"
	for i := 0; i < 100; i++ {
		id := fmt.Sprint(i + 1)
		r.State.Requests[id] = &Request{Message: id, Phase: "reported", Todo: 99}
	}
	var h Health
	r.processOrdinaryPending(context.Background(), &h)
	if *calls != 1 || q.Phase != "source_edit_delivered" || len(h.Errors) != 0 {
		t.Fatal("historical receipts delayed delivery", *calls, q.Phase, h.Errors)
	}
}

func TestIntakeReviewManagedDiscoveryRetainsLastAndStopsBeforeCutover(t *testing.T) {
	r, c := fixture(t)
	r.State.Discovery = map[string]DiscoveryPage{}
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/guilds/100/channels":
			respond(w, []any{map[string]any{"id": "800", "guild_id": "100", "type": 15}, map[string]any{"id": "900", "guild_id": "100", "type": 2, "last_message_id": "999"}})
		case "/channels/800":
			respond(w, map[string]any{"id": "800", "guild_id": "100", "type": 15})
		default:
			respond(w, map[string]any{"threads": []any{map[string]any{"id": "700", "parent_id": "800", "type": 11, "last_message_id": "777", "thread_metadata": map[string]any{"archive_timestamp": c.Add(time.Second).Format(time.RFC3339Nano)}}, map[string]any{"id": "600", "parent_id": "800", "type": 11, "last_message_id": "666", "thread_metadata": map[string]any{"archive_timestamp": c.Add(-time.Second).Format(time.RFC3339Nano)}}}, "has_more": true})
		}
	})
	r.discoverManagement(context.Background(), nil)
	p := r.State.Discovery["management:800/public-archived"]
	if r.State.Threads["900"].Last != "999" || r.State.Threads["700"].Last != "777" || r.State.Threads["600"].ID != "" || p.Complete || p.Next != "" || !p.BoundaryUnresolved || p.Coverage != "archive_lower_bound_reached" {
		t.Fatal("discovery discarded last or crossed old cutoff", r.State.Threads, p, r.DiscoveryErrors)
	}
}
