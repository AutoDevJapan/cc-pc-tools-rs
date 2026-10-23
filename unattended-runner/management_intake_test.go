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

func TestManagementScanRetainsLateUnseenOwnerBelowWatermark(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "遅れて取得できた追加指示")
	r.State.Scans["500"] = &Scan{After: snowflake(c.Add(10 * time.Second))}
	server(t, r, func(w http.ResponseWriter, q *http.Request) { respond(w, []Message{m}) })
	n, err := r.scan(context.Background(), Thread{ID: "500"})
	if err != nil || n != 1 || r.State.Requests[m.ID] == nil {
		t.Fatalf("late owner input was skipped: captured=%d err=%v", n, err)
	}
	if r.State.Scans["500"].After != snowflake(c.Add(10*time.Second)) {
		t.Fatal("late input rewound the original watermark")
	}
}

func TestManagementScanCapturesRegisteredEditWithoutNewTask(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "訂正した指示")
	m.Edited = c.Add(20 * time.Second).Format(time.RFC3339Nano)
	old := captured(m, "reported", c)
	old.Text, old.Edited, old.Todo, old.Reported = "元の指示", "", 42, "complete"
	r.State.Requests[m.ID] = old
	r.State.Scans["500"] = &Scan{After: m.ID}
	server(t, r, func(w http.ResponseWriter, q *http.Request) { respond(w, []Message{m}) })
	if _, err := r.scan(context.Background(), Thread{ID: "500"}); err != nil {
		t.Fatal(err)
	}
	got := r.State.Requests[m.ID]
	if got.Todo != 42 || got.Text != m.Content || got.Edited != m.Edited || got.Phase != "source_edit_saved" || got.Reported != "" || len(got.PriorEdits) != 1 || got.PriorVersions[0] != "元の指示" {
		t.Fatalf("edit was lost or inherited the old completion: %+v", got)
	}
	var restored State
	if err := readJSON(filepath.Join(r.Config.Home, "unattended-state.json"), maxState, &restored); err != nil {
		t.Fatal(err)
	}
	r.State = &restored
	if n, err := r.scan(context.Background(), Thread{ID: "500"}); err != nil || n != 0 || len(r.State.Requests) != 1 || len(r.State.Requests[m.ID].PriorEdits) != 1 {
		t.Fatal("replay after restart duplicated an edit", n, err)
	}
}

func captured(m Message, phase string, c time.Time) *Request {
	return &Request{Thread: m.Channel, Guild: "100", RecordVersion: 2, Message: m.ID, Author: m.Author.ID, Created: m.Timestamp, Edited: m.Edited, Acquired: c.Add(time.Minute).Format(time.RFC3339Nano), Source: "discord-api-v10", Text: m.Content, Key: requestKey(m.Channel, m.ID), Phase: phase}
}

func TestManagementBlockedOriginalReceivesFollowupWithoutNewTask(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "同じ依頼への補足")
	req := captured(m, "captured", c)
	r.State.Requests[m.ID] = req
	r.State.Requests["600"] = &Request{Thread: "500", Message: "600", Todo: 42}
	if err := os.WriteFile(r.TodoPath, []byte(`{"items":[{"id":42,"project":"cc-pc-tools-rs","status":"block","note":"本人の停止を保持"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Parent: requestForum, Guild: "100", Type: 11, Owner: "300"})
		}
	})
	commands := 0
	r.Run = func(context.Context, string, ...string) (string, error) { commands++; return "added 99", nil }
	if err := r.register(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	row, err := r.todo(42)
	if err != nil || req.Todo != 42 || req.Phase != "steering_saved" || commands != 0 || row.Status != "block" {
		t.Fatalf("a followup replaced its stopped task: req=%+v row=%+v commands=%d err=%v", req, row, commands, err)
	}
}

func TestManagementUnboundFollowupStaysPendingAndIsNotCountedRegistered(t *testing.T) {
	r, q, _, calls := ordinaryFixture(t)
	q.Phase = "captured"
	if err := r.register(context.Background(), q); err != nil {
		t.Fatal(err)
	}
	if q.Todo != 0 || q.Phase != "origin_pending" || q.PendingOwner != "OMB" || q.NextAction == "" || *calls != 0 {
		t.Fatal("unbound followup became a new task")
	}
	var h Health
	r.admit(context.Background(), &h)
	if h.Registered != 0 {
		t.Fatal("pending routing was counted as task registration")
	}
}

func TestManagementNewOwnerStarterRegistersOneTask(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "新しい依頼を実装して")
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
	r.Run = func(_ context.Context, p string, a ...string) (string, error) {
		if len(a) == 0 || a[0] != "add" {
			t.Fatal("unexpected command")
		}
		adds++
		return "added 99", nil
	}
	if err := r.register(context.Background(), q); err != nil || q.Todo != 99 || adds != 1 {
		t.Fatal("verified new starter not registered", err)
	}
	if err := r.register(context.Background(), q); err != nil || adds != 1 {
		t.Fatal("starter replay created another task", err)
	}
}

func TestManagementValidateReadableTextVoiceAndThirdForum(t *testing.T) {
	for _, typ := range []int{0, 2, 11, 12} {
		t.Run(fmt.Sprint(typ), func(t *testing.T) {
			r, c := fixture(t)
			m := msg(c.Add(time.Second), "500", "300", "元の依頼へ反映する指示")
			req := captured(m, "origin_pending", c)
			r.State.Requests[m.ID] = req
			server(t, r, func(w http.ResponseWriter, q *http.Request) {
				switch q.URL.Path {
				case "/channels/500":
					respond(w, map[string]any{"id": "500", "guild_id": "100", "type": typ, "parent_id": "800", "owner_id": "300", "permission_overwrites": []any{}})
				case "/channels/800":
					respond(w, map[string]any{"id": "800", "guild_id": "100", "type": 0, "permission_overwrites": []any{}})
				case "/guilds/100/roles":
					respond(w, []any{map[string]any{"id": "100", "permissions": fmt.Sprint(monthlyView | monthlyHistory | monthlyConnect | monthlyManageThreads)}})
				case "/guilds/100/members/400":
					respond(w, map[string]any{"user": map[string]any{"id": "400", "bot": true}, "roles": []string{}})
				default:
					if q.URL.Path == "/channels/500/messages/"+m.ID {
						respond(w, m)
					} else {
						t.Errorf("unexpected source read %s", q.URL.Path)
						w.WriteHeader(404)
					}
				}
			})
			if err := r.validateRequest(context.Background(), req); err != nil {
				t.Fatal("readable guild source rejected", err)
			}
		})
	}
}
