package main

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"
)

func resultReadback(r *Runner, p AgentResultRecord, notify bool) map[string]any {
	body := agentResultBody(p)
	flags := 4100
	mentions := []map[string]string{}
	if notify {
		body += "\n<@" + r.State.Destination.Owner + ">"
		flags = 4
		mentions = append(mentions, map[string]string{"id": r.State.Destination.Owner})
	}
	return map[string]any{"id": "735", "channel_id": p.Event.Thread,
		"author":  map[string]any{"id": r.State.Destination.Bot, "bot": true},
		"content": body + "\n\n-# recovery:aaaaaaaaaaaaaaaaaaaaaaaa:log:0",
		"flags":   flags, "mentions": mentions, "mention_roles": []string{}, "mention_everyone": false}
}

func legacyAgentReceipt(t *testing.T, r *Runner, e AgentResultEvent) AgentResultRecord {
	t.Helper()
	p, err := r.recordAgentResult(e)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var old map[string]any
	if json.Unmarshal(b, &old) != nil {
		t.Fatal("fixture")
	}
	delete(old, "notify_owner") // Actual pre-correction persisted format.
	if err := publish(r.agentResultPath(p.ID), old); err != nil {
		t.Fatal(err)
	}
	return r.mustAgentResult(t, p.ID)
}

func TestAgentResultNewRepliesStopsAndItemsNotifyOwner(t *testing.T) {
	for _, state := range []string{"completed", "stopped", "start_failed", "item_deferred"} {
		t.Run(state, func(t *testing.T) {
			r, _ := fixture(t)
			e := resultEvent()
			e.State = state
			if state == "completed" {
				e.Reason, e.Remaining, e.Next = "completed", "none", "none"
			}
			if state == "start_failed" {
				e.Source, e.Parent, e.Child, e.Reason = "launcher", "", "", "initialization_rejected"
			}
			if state == "item_deferred" {
				e = agentDeferredEvent(e, AgentDeferredItem{ID: "owned-item", Kind: "scratch",
					Transition: "hold", Reason: "cleanup_busy", Scope: "unknown", Next: "retry"})
			}
			p, err := r.recordAgentResult(e)
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(p)
			var receipt map[string]any
			_ = json.Unmarshal(b, &receipt)
			if receipt["notify_owner"] != true {
				t.Error("new parent receipt did not persist the owner-notification decision")
			}
			calls := 0
			r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
				calls++
				if !slices.Contains(args, "--notify") {
					t.Error("new result did not use the normal notifying sender")
				}
				return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"735"}`, nil
			}
			server(t, r, func(w http.ResponseWriter, req *http.Request) {
				if req.Method != "GET" || req.URL.Path != "/channels/610/messages/735" {
					t.Errorf("unexpected readback %s %s", req.Method, req.URL.Path)
				}
				respond(w, resultReadback(r, p, true))
			})
			r.agentResultPhase(context.Background(), &Health{})
			saved := r.mustAgentResult(t, p.ID)
			if saved.Verified == "" || saved.Message != "735" {
				t.Fatal("owner mention was not GET-verified")
			}
			again, err := r.recordAgentResult(e)
			if err != nil || again.ID != p.ID || again.Verified == "" {
				t.Fatal("same transition lost its verified receipt")
			}
			r.agentResultPhase(context.Background(), &Health{})
			if calls != 1 {
				t.Fatal("same verified transition notified more than once")
			}
		})
	}
}

func TestAgentResultNotifyRequiresExactOwnerReadback(t *testing.T) {
	for _, mismatch := range []string{"silent", "missing", "foreign", "extra", "duplicate_body", "role", "everyone"} {
		t.Run(mismatch, func(t *testing.T) {
			r, _ := fixture(t)
			p, _ := r.recordAgentResult(resultEvent())
			r.Run = func(context.Context, string, ...string) (string, error) {
				return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"735"}`, nil
			}
			server(t, r, func(w http.ResponseWriter, _ *http.Request) {
				m := resultReadback(r, p, true)
				switch mismatch {
				case "silent":
					m["flags"] = 4100
				case "missing":
					m["mentions"] = []any{}
				case "foreign":
					m["mentions"] = []map[string]string{{"id": "999"}}
				case "extra":
					m["mentions"] = []map[string]string{{"id": "300"}, {"id": "999"}}
				case "duplicate_body":
					m["content"] = strings.Replace(m["content"].(string), "<@300>", "<@300>\n<@300>", 1)
				case "role":
					m["mention_roles"] = []string{"999"}
				case "everyone":
					m["mention_everyone"] = true
				}
				respond(w, m)
			})
			r.agentResultPhase(context.Background(), &Health{})
			saved := r.mustAgentResult(t, p.ID)
			if saved.Verified != "" || saved.LastError != "delivery_unverified" {
				t.Fatal("mismatched owner notification was marked reported")
			}
		})
	}
}

func TestAgentResultLegacyReceiptsDoNotAcquireNewMentions(t *testing.T) {
	r, _ := fixture(t)
	p := legacyAgentReceipt(t, r, resultEvent())
	calls := 0
	r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		calls++
		if slices.Contains(args, "--notify") {
			t.Error("old reserved input was changed into a mention")
		}
		return `{"domain":"new-owner-recovery-messages","delivery":"verified","thread_id":"610","message_id":"735"}`, nil
	}
	server(t, r, func(w http.ResponseWriter, _ *http.Request) { respond(w, resultReadback(r, p, false)) })
	r.agentResultPhase(context.Background(), &Health{})
	if r.mustAgentResult(t, p.ID).Verified == "" {
		t.Fatal("legacy silent reservation could not finish unchanged")
	}
	now := r.Now().Add(time.Hour)
	r.Now = func() time.Time { return now }
	_, err := r.recordAgentResult(resultEvent())
	if err != nil {
		t.Fatal(err)
	}
	r.agentResultPhase(context.Background(), &Health{})
	if calls != 1 {
		t.Fatal("old verified receipt was notified again")
	}
}
