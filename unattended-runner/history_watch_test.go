package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func historyWatchFixture(t *testing.T) (*Runner, *Message, HistoryWatchInput, *int) {
	t.Helper()
	r, c := fixture(t)
	r.State.Destination.Guild, r.State.Destination.Owner = monthlyGuild, monthlyOwner
	m := msg(c.Add(-12*24*time.Hour), "500", monthlyOwner, "元の履歴投稿")
	s := monthlyAuditScope{Domain: "discord-monthly-history-read-page-v1", Todo: 3387, Guild: monthlyGuild, Owner: monthlyOwner, Since: "2026-09-23T00:00:00+09:00", Until: c.Format(time.RFC3339Nano)}
	typ, attachments := 0, []historyReadAttachment{}
	p := monthlyReadPage{monthlyAuditScope: s, Source: "discord-api-v10", ScopeSHA: strings.Repeat("a", 64), Bot: "400", Channel: "500", Type: 0, Scanned: 1, Messages: []historyReadMessage{{Message: m, Type: &typ, Attachments: &attachments}}, Acquired: c.Format(time.RFC3339Nano), Permission: "effective_parent_permissions_verified", Complete: true}
	path := filepath.Join(r.Config.Home, "original-monthly.json")
	if err := publish(path, p); err != nil {
		t.Fatal(err)
	}
	b, err := boundedRegular(path)
	if err != nil {
		t.Fatal(err)
	}
	in := HistoryWatchInput{Domain: "discord-intake-history-watch-v1", Message: m.ID, Receipt: path, ReceiptSHA: bytesHash(b)}
	messageReads := new(int)
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/users/@me":
			respond(w, map[string]any{"id": "400", "bot": true})
		case "/channels/500":
			respond(w, map[string]any{"id": "500", "guild_id": monthlyGuild, "type": 0, "permission_overwrites": []any{}})
		case "/guilds/" + monthlyGuild + "/roles":
			respond(w, []any{map[string]any{"id": monthlyGuild, "permissions": fmt.Sprint(monthlyView | monthlyHistory)}})
		case "/guilds/" + monthlyGuild + "/members/400":
			respond(w, map[string]any{"user": map[string]any{"id": "400", "bot": true}, "roles": []string{}})
		case "/channels/500/messages/" + m.ID:
			*messageReads++
			respond(w, historyReadMessage{Message: m, Type: &typ, Attachments: &attachments})
		default:
			w.WriteHeader(404)
		}
	})
	return r, &m, in, messageReads
}

func TestHistoryWatchPreservesFrozenReceiptAndWaitsForActualEdit(t *testing.T) {
	r, m, in, _ := historyWatchFixture(t)
	guard := func(context.Context, string) bool { return true }
	if err := r.watchHistory(context.Background(), in, guard); err != nil {
		t.Fatal(err)
	}
	q := r.State.Requests[m.ID]
	if q == nil || q.Phase != "history_watch" || q.Todo != 0 {
		t.Fatal("baseline became a task", q)
	}
	before := ordinaryVersion(q)
	if err := r.watchHistory(context.Background(), in, guard); err != nil || len(r.State.Requests) != 1 || ordinaryVersion(q) != before {
		t.Fatal("watch reread duplicated or changed baseline", err)
	}
	var state State
	if err := readJSON(filepath.Join(r.Config.Home, "unattended-state.json"), maxState, &state); err != nil {
		t.Fatal(err)
	}
	r.State = &state
	q = r.State.Requests[m.ID]
	var h Health
	r.refreshOrdinary(context.Background(), &h)
	if q.Phase != "history_watch" || len(h.Errors) != 0 {
		t.Fatal("unchanged historical source was re-admitted", q.Phase, h.Errors)
	}
	m.Content, m.Edited = "元の投稿への新しい訂正", r.Now().Add(-time.Second).Format(time.RFC3339Nano)
	r.refreshOrdinary(context.Background(), &h)
	if q.Phase != "task_binding_pending" || q.Todo != 0 || q.PendingOwner != "OMB" || len(q.PriorEdits) != 1 || q.Text != m.Content {
		t.Fatal("old edited input was lost or interpreted as a new task", q.Phase, h.Errors)
	}
	b, err := boundedRegular(in.Receipt)
	if err != nil || bytesHash(b) != in.ReceiptSHA {
		t.Fatal("original monthly receipt changed")
	}
	r.admit(context.Background(), &h)
	if h.Registered != 0 {
		t.Fatal("historical edit automatically created work")
	}
}

func TestHistoryWatchRejectsChangedReceiptAndLiveVersion(t *testing.T) {
	for _, change := range []string{"receipt", "live", "guard"} {
		t.Run(change, func(t *testing.T) {
			r, m, in, reads := historyWatchFixture(t)
			if change == "receipt" {
				in.ReceiptSHA = strings.Repeat("0", 64)
			}
			if change == "live" {
				m.Content = "取得後に変わった本文"
			}
			err := r.watchHistory(context.Background(), in, func(context.Context, string) bool { return change != "guard" })
			if err == nil || len(r.State.Requests) != 0 || change != "live" && *reads != 0 {
				t.Fatal("unqualified historical source was accepted", err, *reads)
			}
		})
	}
}

func TestHistoryWatchDoesNotReplaceExistingNormalSource(t *testing.T) {
	r, m, in, _ := historyWatchFixture(t)
	q := &Request{Message: m.ID, Todo: 42, Phase: "reported"}
	r.State.Requests[m.ID] = q
	if err := r.watchHistory(context.Background(), in, func(context.Context, string) bool { return true }); err == nil || r.State.Requests[m.ID] != q || q.Todo != 42 || q.Phase != "reported" {
		t.Fatal("watch replaced existing routing", err)
	}
}

func TestHistoryWatchCLIRegistersOnlyOneSourceAndPrintsMetadata(t *testing.T) {
	r, m, in, _ := historyWatchFixture(t)
	input := filepath.Join(r.Config.Home, "watch-input.json")
	if err := publish(input, in); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	load := func(_ string, readOnly bool) (*Runner, func(), error) {
		if readOnly {
			t.Error("watch omitted the writer lock")
		}
		return r, func() {}, nil
	}
	if code := intakeCLIWith([]string{"watch", "--input", input}, &out, load, func(context.Context, string) bool { return true }); code != 0 {
		t.Fatal("watch CLI rejected qualified input", out.String())
	}
	var result map[string]any
	if json.Unmarshal([]byte(out.String()), &result) != nil || result["message_id"] != m.ID || result["phase"] != "history_watch" || result["application"] != "not_inferred" || strings.Contains(out.String(), m.Content) {
		t.Fatal("watch CLI exposed body or claimed application", out.String())
	}
	var h Health
	r.admit(context.Background(), &h)
	if h.Registered != 0 || r.State.Requests[m.ID].Todo != 0 || len(r.State.Scans) != 0 {
		t.Fatal("watch changed admission or the old scan cursor")
	}
}

func TestHistoryWatchRefusesForeignScopeAndCurrentPermissionDenial(t *testing.T) {
	for _, kind := range []string{"scope", "denied"} {
		t.Run(kind, func(t *testing.T) {
			r, _, in, reads := historyWatchFixture(t)
			if kind == "scope" {
				var page monthlyReadPage
				if err := readJSON(in.Receipt, 4<<20, &page); err != nil {
					t.Fatal(err)
				}
				page.Owner = "999"
				if err := publish(in.Receipt, page); err != nil {
					t.Fatal(err)
				}
				b, _ := boundedRegular(in.Receipt)
				in.ReceiptSHA = bytesHash(b)
			} else {
				server(t, r, func(w http.ResponseWriter, req *http.Request) {
					if req.URL.Path == "/users/@me" {
						respond(w, map[string]any{"id": "400", "bot": true})
					} else {
						if strings.Contains(req.URL.Path, "/messages/") {
							*reads++
						}
						w.WriteHeader(403)
					}
				})
			}
			if err := r.watchHistory(context.Background(), in, func(context.Context, string) bool { return true }); err == nil || len(r.State.Requests) != 0 || *reads != 0 {
				t.Fatal("scope/permission failure became saved history", err, *reads)
			}
		})
	}
}
