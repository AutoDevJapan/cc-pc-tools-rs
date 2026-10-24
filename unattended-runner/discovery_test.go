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

func TestArchivePassContinuesAcrossTicksAndRestart(t *testing.T) {
	r, c := fixture(t)
	var cursors []string
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/guilds/") {
			respond(w, map[string]any{"threads": []Thread{}})
			return
		}
		if !strings.Contains(q.URL.Path, requestForum) {
			respond(w, map[string]any{"threads": []Thread{}, "has_more": false})
			return
		}
		before := q.URL.Query().Get("before")
		cursors = append(cursors, before)
		page := 0
		for i := 1; i < 3; i++ {
			if before == c.Add(-time.Duration(i)*time.Hour).Format(time.RFC3339Nano) {
				page = i
			}
		}
		if q.URL.Query().Get("limit") != "100" {
			t.Error("archive limit differs")
		}
		var threads []Thread
		for i := 0; i < 2; i++ {
			th := Thread{ID: fmt.Sprint(500 + page*2 + i), Parent: requestForum, Guild: "100", Owner: "300", Type: 11}
			th.Metadata.ArchiveTimestamp = c.Add(-time.Duration(page+1) * time.Hour).Format(time.RFC3339Nano)
			threads = append(threads, th)
		}
		respond(w, map[string]any{"threads": threads, "has_more": page < 2})
	})
	r.discoverPages(context.Background(), nil)
	if r.State.Discovery[requestForum].Complete || r.State.Discovery[requestForum].Next == "" || len(r.State.Threads) != 2 {
		t.Fatal("first bounded page reported full coverage")
	}
	var restored State
	if err := readJSON(filepath.Join(r.Config.Home, "unattended-state.json"), maxState, &restored); err != nil {
		t.Fatal(err)
	}
	r.State = &restored
	r.discoverPages(context.Background(), nil)
	r.discoverPages(context.Background(), nil)
	page := r.State.Discovery[requestForum]
	if len(r.State.Threads) != 6 || !page.Complete || page.Next != "" || page.Pages != 3 || len(cursors) != 3 || cursors[1] == "" || cursors[2] == cursors[1] {
		t.Fatal("later archived threads were starved", page, cursors)
	}
	r.discoverPages(context.Background(), nil)
	if cursors[3] != "" {
		t.Fatal("completed archive pass did not wrap to new head")
	}
}

func TestFailedArchivePageRetainsCursorAndOtherForumStillAdvances(t *testing.T) {
	r, c := fixture(t)
	prior := DiscoveryPage{Next: c.Format(time.RFC3339Nano), Pages: 3}
	r.State.Discovery = map[string]DiscoveryPage{requestForum: prior}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, requestForum) {
			w.WriteHeader(503)
			return
		}
		respond(w, map[string]any{"threads": []Thread{}, "has_more": false})
	})
	r.discoverPages(context.Background(), nil)
	if r.State.Discovery[requestForum] != prior || !r.State.Discovery["200"].Complete || len(r.DiscoveryErrors) != 1 {
		t.Fatal("failed page lost continuation or blocked other forum")
	}
}

func TestBotRequestThreadRequiresKnownReceiptAndPreservesExactOrigin(t *testing.T) {
	r, c := fixture(t)
	rows := []Thread{{ID: "500", Type: 11, Guild: "100", Parent: requestForum, Owner: "300"}, {ID: "501", Type: 11, Guild: "100", Parent: requestForum, Owner: "400"}, {ID: "502", Type: 11, Guild: "100", Parent: requestForum, Owner: "400"}, {ID: "503", Type: 11, Guild: "999", Parent: requestForum, Owner: "300"}}
	m := msg(c.Add(time.Second), "501", "300", "追加指示")
	r.ProgressLinks = map[string]ProgressLink{"501": {ProgressOrigin: ProgressOrigin{RequestID: "exact-new-receipt", Guild: "100", Thread: "501", Message: "600", Todo: 2639, Job: "original"}}}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages") {
			other := m
			other.Author.ID = "999"
			other.ID = snowflake(c.Add(2 * time.Second))
			bot := m
			bot.ID = snowflake(c.Add(3 * time.Second))
			bot.Author.Bot = true
			respond(w, []Message{m, other, bot})
			return
		}
		if strings.Contains(q.URL.Path, "/guilds/") {
			respond(w, map[string]any{"threads": rows})
			return
		}
		respond(w, map[string]any{"threads": []Thread{}, "has_more": false})
	})
	r.discoverPages(context.Background(), map[string]bool{"501": true})
	if len(r.State.Threads) != 2 || r.State.Threads["502"].ID != "" {
		t.Fatal("legacy or foreign request thread gained intake scope")
	}
	n, err := r.scan(context.Background(), r.State.Threads["501"])
	if err != nil || n != 1 || r.State.Requests[m.ID].Origin == nil || r.State.Requests[m.ID].Origin.RequestID != "exact-new-receipt" || r.State.Requests[m.ID].Origin.Todo != 2639 {
		t.Fatal("bot-thread owner reply lost the exact original association", err)
	}
}

func TestUnknownBotProgressReplyIsCapturedWithoutInventingAnApproval(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "a")
	server(t, r, func(w http.ResponseWriter, q *http.Request) { respond(w, []Message{m}) })
	if _, err := r.scan(context.Background(), Thread{ID: "500", Owner: "400"}); err != nil {
		t.Fatal(err)
	}
	req := r.State.Requests[m.ID]
	if req.Phase != "origin_pending" || req.Origin != nil {
		t.Fatal("unknown old answer gained arbitrary new context")
	}
	r.Run = func(context.Context, string, ...string) (string, error) {
		t.Fatal("unassociated answer reached an external command")
		return "", nil
	}
	var h Health
	r.admit(context.Background(), &h)
	if req.Todo != 0 || h.Registered != 0 {
		t.Fatal("unassociated answer registered a task")
	}
}

func TestHistoryFailuresDoNotStarveLaterThreads(t *testing.T) {
	r, _ := fixture(t)
	r.Config.Home = filepath.Join(t.TempDir(), "unattended")
	if err := os.MkdirAll(r.Config.Home, 0700); err != nil {
		t.Fatal(err)
	}
	if err := publish(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), newMessageLedger{Domain: "new-owner-recovery-messages"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 12; i++ {
		id := fmt.Sprint(7000 + i)
		r.State.Threads[id] = Thread{ID: id, Type: 11, Guild: "100", Parent: "200", Owner: "400"}
		r.State.Scans[id] = &Scan{After: snowflake(mustCutover(t, r))}
	}
	attempts := map[string]int{}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		switch q.URL.Path {
		case "/users/@me":
			respond(w, map[string]any{"id": "400", "bot": true})
		case "/channels/" + requestForum:
			respond(w, Thread{ID: requestForum, Type: 15, Guild: "100"})
		default:
			if strings.Contains(q.URL.Path, "/messages") {
				id := strings.Split(q.URL.Path, "/")[2]
				attempts[id]++
				w.WriteHeader(503)
				return
			}
			respond(w, map[string]any{"threads": []Thread{}, "has_more": false})
		}
	})
	r.Run = func(context.Context, string, ...string) (string, error) {
		return `{"repair_mode":true,"error":null}`, nil
	}
	r.tick(context.Background())
	r.tick(context.Background())
	if len(attempts) != 12 {
		t.Fatal("repeated first-page failures starved the back of the thread queue", len(attempts))
	}
	for _, scan := range r.State.Scans {
		if scan.After != snowflake(mustCutover(t, r)) || scan.Before != "" {
			t.Fatal("failed history page advanced message watermark")
		}
	}
}
func mustCutover(t *testing.T, r *Runner) time.Time {
	t.Helper()
	c, e := time.Parse(time.RFC3339Nano, r.State.Since)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
