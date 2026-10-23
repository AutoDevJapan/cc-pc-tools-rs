//go:build windows

package observer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

var fixtureNow = time.Date(2030, 1, 2, 12, 0, 0, 0, time.UTC)

func synthetic() (State, Observation) {
	s := NewState()
	s.Initialized = true
	s.Target = Fingerprint("synthetic-target")
	s.Cursor = Cursor{Epoch: "epoch-one", Record: 100}
	s.LastAt = fixtureNow.Add(-time.Minute)
	s.LastInstance = Fingerprint("synthetic-instance")
	s.StopGeneration = 7
	o := Observation{At: fixtureNow, Process: ProcessSample{Presence: Absent, Target: s.Target, Instance: s.LastInstance, ScopeKnown: true, InScope: true, AbsenceFor: s.LastInstance},
		Directives: Directives{true, true, 7, false}, Cursor: Cursor{Epoch: "epoch-one", Record: 101},
		Intent: IntentUnknown, InstanceMatches: true, syntheticNoStop: true,
		Event: &Event{"epoch-one", 101, fixtureNow.Add(-time.Second), s.LastInstance, true, true, true, true}}
	return s, o
}

type acceptanceCase struct {
	ID       string                     `json:"id"`
	Given    map[string]json.RawMessage `json:"given"`
	Expected struct {
		Decision string `json:"decision"`
		Fake     int    `json:"fake_activation_calls"`
		Live     int    `json:"live_activation_calls"`
	} `json:"expected"`
}

func loadCases(t *testing.T) []acceptanceCase {
	t.Helper()
	b, err := os.ReadFile("testdata/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []acceptanceCase `json:"cases"`
	}
	if err = json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	if len(f.Cases) != 32 {
		t.Fatalf("want 32 acceptance cases, got %d", len(f.Cases))
	}
	seen := map[string]bool{}
	for _, c := range f.Cases {
		if seen[c.ID] {
			t.Fatal("duplicate ID", c.ID)
		}
		seen[c.ID] = true
	}
	return f.Cases
}

func applyCase(t *testing.T, c acceptanceCase) (State, Observation) {
	t.Helper()
	s, o := synthetic()
	for k, v := range c.Given {
		var b bool
		_ = json.Unmarshal(v, &b)
		var str string
		_ = json.Unmarshal(v, &str)
		r := s.Receipts[Incident(o)]
		switch k {
		case "process":
			o.Process.Presence = Presence(str)
		case "event":
			if str == "missing" {
				o.Event = nil
			}
			if str == "hang" {
				o.Event.ExitVerified = false
			}
		case "fresh":
			if !b {
				o.Event.At = fixtureNow.Add(-6 * time.Minute)
			}
		case "consumed", "episode_consumed":
			r.Attempt = "completed"
			r.At = fixtureNow.Add(-time.Minute)
		case "record_changed":
			o.Event.Record++
			o.Cursor.Record = o.Event.Record
		case "package_matches":
			o.Event.PackageMatches = b
		case "session_matches":
			o.Event.SessionMatches = b
		case "instance_matches":
			o.InstanceMatches = b
		case "exit_verified":
			o.Event.ExitVerified = b
		case "intent":
			o.syntheticNoStop = false
			o.Intent = NativeIntent(str)
		case "owner_stop":
			o.Directives.Stop = b
		case "directives_fresh":
			o.Directives.Fresh = b
		case "session_known":
			o.Process.ScopeKnown = b
		case "log_epoch_matches":
			if !b {
				o.Cursor.Epoch = "new-epoch"
				o.Event.Epoch = "new-epoch"
			}
		case "time_trustworthy":
			if !b {
				o.At = s.LastAt.Add(-time.Second)
			}
		case "attempt_state":
			r.Attempt = str
			r.At = fixtureNow.Add(-time.Minute)
		case "cooldown_active":
			if b {
				s.Requests = []time.Time{fixtureNow.Add(-time.Minute)}
			}
		case "incidents_last_hour":
			var n int
			_ = json.Unmarshal(v, &n)
			for i := 0; i < n; i++ {
				s.Requests = append(s.Requests, fixtureNow.Add(time.Duration(-55+i*15)*time.Minute))
			}
		case "later_instance_observed":
			o.LaterInstance = b
		case "first_run":
			s.Initialized = !b
		case "event_before_baseline": // Initial cursor must ignore this event.
		case "event_record":
			_ = json.Unmarshal(v, &o.Event.Record)
		case "already_manually_recovered", "cleanup_may_follow_signout", "runner_restarted": // Scenario context, not evidence.
		case "persist_success", "stop_generation_changed", "lock_acquired": // Engine-boundary cases.
		default:
			t.Fatal("unhandled fixture field", k)
		}
		if r.Attempt != "" {
			s.Receipts[Incident(o)] = r
		}
	}
	return s, o
}

func TestAcceptance(t *testing.T) {
	for _, c := range loadCases(t) {
		t.Run(c.ID, func(t *testing.T) {
			s, o := applyCase(t, c)
			e := testEngine(t, s, o)
			if v, ok := c.Given["persist_success"]; ok && string(v) == "false" {
				e.Store.(*FileStore).fault = func(string) error { return errors.New("injected") }
			}
			if v, ok := c.Given["stop_generation_changed"]; ok && string(v) == "true" {
				e.Directives = directiveFunc(func(context.Context) (Directives, error) { d := o.Directives; d.Generation++; return d, nil })
			}
			if v, ok := c.Given["lock_acquired"]; ok && string(v) == "false" {
				unlock, err := e.Store.Lock(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer unlock()
			}
			got := runSynthetic(e, o)
			if got.Decision != c.Expected.Decision {
				t.Fatalf("decision %q, want %q", got.Decision, c.Expected.Decision)
			}
			if got.LiveActivationEnabled || got.LiveActivationCalls != c.Expected.Live {
				t.Fatal("live activation must stay disabled")
			}
			var fake fakeAdapter
			fake.deliver(t, e, o, got)
			if int(fake.calls.Load()) != c.Expected.Fake {
				t.Fatalf("fake requests=%d, want %d", fake.calls.Load(), c.Expected.Fake)
			}
		})
	}
}
