package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func linkBudgetFixture(t *testing.T, count int) (*Runner, []ProgressLink) {
	t.Helper()
	r, c := fixture(t)
	if err := publish(r.TodoPath, map[string]any{"items": []any{map[string]any{"id": 42, "project": "cc-pc-tools-rs", "status": "doing", "jobs": []string{"worker-42"}}}}); err != nil {
		t.Fatal(err)
	}
	links := []ProgressLink{}
	events := []any{}
	for i := 0; i < count; i++ {
		thread := fmt.Sprint(501 + i)
		link := ProgressLink{ProgressOrigin: ProgressOrigin{RequestID: fmt.Sprint("link-", i), Guild: "100", Thread: thread, Message: thread, Todo: 42, Job: "worker-42"}, Author: "400", Created: c.Format("2006-01-02T15:04:05Z07:00")}
		links = append(links, link)
		events = append(events, map[string]any{"event": "verified", "request_id": link.RequestID, "phase": "log:0", "receipt": map[string]any{"thread_id": thread, "message_id": thread, "author_id": "400", "timestamp": link.Created, "flags": 4100}})
	}
	if publish(filepath.Join(r.Config.Home, "progress-links.json"), progressLinkFile{Domain: progressLinksDomain, Links: links}) != nil || publish(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), map[string]any{"domain": "new-owner-recovery-messages", "events": events}) != nil {
		t.Fatal("fixture files")
	}
	return r, links
}

func TestHistoricalLinkDeadlineDoesNotConsumeDiscoveryBeforeOwnerCapture(t *testing.T) {
	r, links := linkBudgetFixture(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	activeSeen := false
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		switch q.URL.Path {
		case "/users/@me":
			respond(w, map[string]any{"id": "400", "bot": true})
		case "/channels/" + requestForum:
			respond(w, Thread{ID: requestForum, Guild: "100", Type: 15})
		case "/guilds/100/threads/active":
			activeSeen = true
			respond(w, map[string]any{"threads": []Thread{{ID: "700", Guild: "100", Parent: requestForum, Owner: "300", Type: 11}, {ID: links[0].Thread, Guild: "100", Parent: "200", Owner: "400", Type: 11}}})
		case "/channels/" + links[0].Thread:
			if !activeSeen {
				t.Error("historical link validation ran before live owner discovery")
			}
			cancel()
			<-q.Context().Done()
		default:
			if strings.Contains(q.URL.Path, "/threads/archived/") {
				respond(w, map[string]any{"threads": []Thread{}, "has_more": false})
				return
			}
			t.Errorf("unexpected route %s", q.URL.Path)
			w.WriteHeader(404)
		}
	})
	if err := r.discover(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.State.Threads["700"]; !ok || !activeSeen {
		t.Fatal("owner source discovery was starved")
	}
	if r.ProgressLinkRegistryInvalid || r.ProgressLinks[links[0].Thread].Todo != 0 || !r.BlockedProgressThreads[links[0].Thread] {
		t.Fatal("deferred link became actionable or corrupted the registry")
	}
}

func TestProgressLinkVerificationContinuesPastOneBatchAndRestart(t *testing.T) {
	r, links := linkBudgetFixture(t, 8)
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		parts := strings.Split(strings.Trim(q.URL.Path, "/"), "/")
		if len(parts) < 2 {
			w.WriteHeader(404)
			return
		}
		id := parts[1]
		if len(parts) == 2 {
			respond(w, Thread{ID: id, Guild: "100", Parent: "200", Owner: "400", Type: 11})
			return
		}
		m := Message{ID: id, Channel: id, Content: "verified progress", Timestamp: links[0].Created}
		m.Author.ID = "400"
		m.Author.Bot = true
		respond(w, m)
	})
	seen := map[string]bool{}
	for pass := 0; pass < 2; pass++ {
		r.loadProgressLinks(context.Background(), map[string]bool{})
		if len(r.ProgressLinks) != 4 {
			t.Fatalf("verification batch not bounded: %d", len(r.ProgressLinks))
		}
		for id := range r.ProgressLinks {
			seen[id] = true
		}
		if pass == 0 {
			var state State
			if readJSON(filepath.Join(r.Config.Home, "unattended-state.json"), maxState, &state) != nil {
				t.Fatal("cursor not saved")
			}
			r.State = &state
		}
	}
	if len(seen) != 8 {
		t.Fatal("later progress links starved")
	}
	if _, err := os.Stat(filepath.Join(r.Config.Home, "progress-links.json")); err != nil {
		t.Fatal("source link registry lost")
	}
}

func TestDuplicateProgressThreadNeverBecomesActionableAfterSuccessfulLookup(t *testing.T) {
	r, links := linkBudgetFixture(t, 1)
	if publish(filepath.Join(r.Config.Home, "progress-links.json"), progressLinkFile{Domain: progressLinksDomain, Links: []ProgressLink{links[0], links[0]}}) != nil {
		t.Fatal("fixture")
	}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		t.Error("ambiguous link reached remote verification")
		w.WriteHeader(500)
	})
	r.loadProgressLinks(context.Background(), map[string]bool{links[0].Thread: true})
	if !r.ProgressLinkRegistryInvalid || !r.BlockedProgressThreads[links[0].Thread] || r.ProgressLinks[links[0].Thread].Todo != 0 {
		t.Fatal("structural block was erased")
	}
}

func TestVerifiedPreCutoverLinkIsStillDiscoveredAfterExploration(t *testing.T) {
	r, links := linkBudgetFixture(t, 1)
	old := links[0]
	stamp, _ := time.Parse(time.RFC3339Nano, old.Created)
	old.Created = stamp.Add(-time.Hour).Format(time.RFC3339Nano)
	if publish(filepath.Join(r.Config.Home, "progress-links.json"), progressLinkFile{Domain: progressLinksDomain, Links: []ProgressLink{old}}) != nil {
		t.Fatal("fixture")
	}
	if publish(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), map[string]any{"domain": "new-owner-recovery-messages", "events": []any{map[string]any{"event": "verified", "request_id": old.RequestID, "phase": "log:0", "receipt": map[string]any{"thread_id": old.Thread, "message_id": old.Message, "author_id": "400", "timestamp": old.Created, "flags": 4100}}}}) != nil {
		t.Fatal("fixture")
	}
	thread := Thread{ID: old.Thread, Guild: "100", Parent: requestForum, Owner: "400", Type: 11}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		switch q.URL.Path {
		case "/users/@me":
			respond(w, map[string]any{"id": "400", "bot": true})
		case "/channels/" + requestForum:
			respond(w, Thread{ID: requestForum, Guild: "100", Type: 15})
		case "/guilds/100/threads/active":
			respond(w, map[string]any{"threads": []Thread{thread}})
		case "/channels/" + old.Thread:
			respond(w, thread)
		case "/channels/" + old.Thread + "/messages/" + old.Message:
			m := Message{ID: old.Message, Channel: old.Thread, Content: "verified old progress", Timestamp: old.Created}
			m.Author.ID = "400"
			m.Author.Bot = true
			respond(w, m)
		default:
			if strings.Contains(q.URL.Path, "/threads/archived/") {
				respond(w, map[string]any{"threads": []Thread{}, "has_more": false})
				return
			}
			t.Error("unexpected discovery lookup")
			w.WriteHeader(404)
		}
	})
	if err := r.discover(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.State.Threads[old.Thread]; !ok || r.ProgressLinks[old.Thread].Todo != 42 {
		t.Fatal("verified old link was lost by discovery order")
	}
}

func TestDeferredLinkContextDoesNotPreventNewOwnerMessageCapture(t *testing.T) {
	r, links := linkBudgetFixture(t, 1)
	parent := context.Background()
	linkCtx, cancelLink := context.WithCancel(parent)
	defer cancelLink()
	created, _ := time.Parse(time.RFC3339Nano, links[0].Created)
	owner := msg(created.Add(time.Second), "700", "300", "新しい実施依頼です")
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if q.URL.Path == "/channels/"+links[0].Thread {
			cancelLink()
			<-q.Context().Done()
			return
		}
		if q.URL.Path == "/channels/700/messages" {
			respond(w, []Message{owner})
			return
		}
		t.Error("unexpected owner capture route")
		w.WriteHeader(404)
	})
	r.loadProgressLinks(linkCtx, map[string]bool{links[0].Thread: true})
	count, err := r.scan(parent, Thread{ID: "700", Guild: "100", Parent: requestForum, Owner: "300", Type: 11})
	if err != nil || count != 1 || r.State.Requests[owner.ID] == nil || r.State.Requests[owner.ID].Text != owner.Content {
		t.Fatal("new owner instruction was starved by a deferred historical link")
	}
}
