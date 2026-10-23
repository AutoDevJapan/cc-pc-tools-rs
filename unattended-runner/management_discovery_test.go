package main

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestManagementDiscoveryIncludesOtherForumTextVoicePrivateAndContinuesArchives(t *testing.T) {
	r, c := fixture(t)
	r.State.Discovery = map[string]DiscoveryPage{}
	active := []Thread{{ID: "601", Guild: "100", Parent: "800", Type: 11}, {ID: "602", Parent: "700", Type: 12}}
	archiveCalls := []string{}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if q.URL.Path == "/guilds/100/channels" {
			respond(w, []any{map[string]any{"id": "800", "guild_id": "100", "type": 15}, map[string]any{"id": "700", "guild_id": "100", "type": 0}, map[string]any{"id": "900", "guild_id": "100", "type": 2}})
			return
		}
		if q.URL.Path == "/channels/700" || q.URL.Path == "/channels/800" {
			id := strings.Split(q.URL.Path, "/")[2]
			typ := 0
			if id == "800" {
				typ = 15
			}
			respond(w, map[string]any{"id": id, "guild_id": "100", "type": typ})
			return
		}
		archiveCalls = append(archiveCalls, q.URL.Path)
		if strings.Contains(q.URL.Path, "/users/@me/") {
			respond(w, map[string]any{"threads": []any{map[string]any{"id": "603", "parent_id": "700", "type": 12}}, "has_more": false})
		} else {
			respond(w, map[string]any{"threads": []any{}, "has_more": false})
		}
	})
	for range 4 {
		r.discoverManagement(context.Background(), active)
	}
	for _, id := range []string{"700", "900", "601", "602", "603"} {
		if !r.State.Threads[id].Managed {
			t.Errorf("source %s absent from ordinary intake", id)
		}
	}
	if len(archiveCalls) != 4 || archiveCalls[0] == archiveCalls[1] || len(r.DiscoveryErrors) != 0 {
		t.Fatal("archive rotation failed", archiveCalls, r.DiscoveryErrors)
	}
	if r.State.Discovery["management:channels"].Checked != c.Add(time.Minute).UTC().Format(time.RFC3339Nano) {
		t.Fatal("coverage receipt absent")
	}
}

func TestManagementDeniedReadDoesNotCreateEmptyCoverageOrMoveWatermark(t *testing.T) {
	r, c := fixture(t)
	thread := Thread{ID: "500", Type: 12, Guild: "100", Parent: "700", Managed: true}
	r.State.Scans["500"] = &Scan{After: snowflake(c)}
	reads := 0
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages") {
			reads++
		}
		w.WriteHeader(403)
	})
	if _, err := r.scan(context.Background(), thread); err == nil {
		t.Fatal("permission denial became an empty page")
	}
	if reads != 0 || r.State.Scans["500"].After != snowflake(c) || len(r.State.Requests) != 0 {
		t.Fatal("unread source advanced or reached message history")
	}
}

func TestManagementNormalTickRechecksReportedEditWithNoNewLastMessage(t *testing.T) {
	r, q, m, calls := ordinaryFixture(t)
	q.Todo, q.Phase, q.Reported = 42, "reported", "complete"
	q.Text = "編集前の本文"
	r.State.Threads["500"] = Thread{ID: "500", Type: 11, Guild: "100", Parent: requestForum, Owner: "300", Last: m.ID}
	r.State.Scans["500"] = &Scan{After: m.ID}
	base := r.Run
	r.Run = func(ctx context.Context, p string, a ...string) (string, error) {
		if strings.HasSuffix(p, "scheduler.exe") {
			return `{}`, nil
		}
		return base(ctx, p, a...)
	}
	r.tick(context.Background())
	r.tick(context.Background())
	if q.Text != m.Content || q.Todo != 42 || q.Phase != "source_edit_delivered" || *calls != 1 {
		t.Fatal("normal tick did not re-read/deliver completed input", q, *calls)
	}
}
