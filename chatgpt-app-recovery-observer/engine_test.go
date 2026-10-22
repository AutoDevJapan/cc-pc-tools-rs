//go:build windows

package observer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type clockFunc func() time.Time

func (f clockFunc) Now() time.Time { return f() }

type processFunc func(context.Context) (ProcessSample, error)

func (f processFunc) ReadProcess(c context.Context) (ProcessSample, error) { return f(c) }

type directiveFunc func(context.Context) (Directives, error)

func (f directiveFunc) ReadDirectives(c context.Context) (Directives, error) { return f(c) }

type eventFunc func(context.Context, Cursor, ProcessSample, string) (Cursor, *Event, error)

func (f eventFunc) ReadEvents(c context.Context, k Cursor, p ProcessSample, i string) (Cursor, *Event, error) {
	return f(c, k, p, i)
}

func testEngine(t *testing.T, s State, o Observation) Engine {
	t.Helper()
	st := &FileStore{Path: filepath.Join(t.TempDir(), "state.json")}
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	return Engine{Store: st, Clock: clockFunc(func() time.Time { return o.At }),
		Processes:  processFunc(func(context.Context) (ProcessSample, error) { return o.Process, nil }),
		Directives: directiveFunc(func(context.Context) (Directives, error) { return o.Directives, nil }),
		Events: eventFunc(func(context.Context, Cursor, ProcessSample, string) (Cursor, *Event, error) {
			return o.Cursor, o.Event, nil
		})}
}

func runSynthetic(e Engine, o Observation) Result {
	unlock, err := e.Store.Lock(context.Background())
	if err != nil {
		return Result{Decision: "busy", Reason: "state_lock_unavailable"}
	}
	defer unlock()
	s, err := e.Store.Load()
	if err != nil {
		return Result{Decision: "unknown", Reason: "state_unreadable"}
	}
	return e.finish(context.Background(), s, o)
}

// This is the only activation adapter in the module. It exists only in tests.
type fakeAdapter struct{ calls atomic.Int32 }

func (f *fakeAdapter) deliver(t *testing.T, e Engine, o Observation, r Result) {
	t.Helper()
	if !r.VirtualRequest {
		return
	}
	s, err := e.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Receipts[Incident(o)].Attempt != "intent_committed" {
		t.Fatal("request preceded durable consume")
	}
	if r.LiveActivationEnabled || r.LiveActivationCalls != 0 {
		t.Fatal("live adapter enabled")
	}
	f.calls.Add(1)
}

func TestConcurrentCallsConsumeOnce(t *testing.T) {
	s, o := synthetic()
	e := testEngine(t, s, o)
	var f fakeAdapter
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; r := runSynthetic(e, o); f.deliver(t, e, o, r) }()
	}
	close(start)
	wg.Wait()
	if f.calls.Load() != 1 {
		t.Fatalf("fake requests=%d", f.calls.Load())
	}
	reloaded, err := e.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := Decide(reloaded, o); got.Decision != "reconcile_only" {
		t.Fatal(got)
	}
}

func TestSaveFailureAndCrashNeverReplay(t *testing.T) {
	for _, stage := range []string{"create", "write", "flush", "replace", "after_replace"} {
		t.Run(stage, func(t *testing.T) {
			s, o := synthetic()
			e := testEngine(t, s, o)
			st := e.Store.(*FileStore)
			st.fault = func(p string) error {
				if p == stage {
					return errors.New("injected_save_failure")
				}
				return nil
			}
			r := runSynthetic(e, o)
			if r.Decision != "persist_failed" || r.VirtualRequest {
				t.Fatal(r)
			}
			st.fault = nil
			loaded, err := st.Load()
			if err != nil {
				t.Fatal(err)
			}
			if stage == "after_replace" && Decide(loaded, o).Decision != "reconcile_only" {
				t.Fatal("published consume must survive an uncertain return")
			}
			if stage != "after_replace" && len(loaded.Receipts) != 0 {
				t.Fatal("failed publish replaced prior state")
			}
		})
	}
}

func TestRunnerDisappearsAfterCommit(t *testing.T) {
	s, o := synthetic()
	e := testEngine(t, s, o)
	r := runSynthetic(e, o)
	if !r.VirtualRequest {
		t.Fatal(r)
	}
	// Simulate losing the runner without delivering the returned virtual request.
	newStore := &FileStore{Path: e.Store.(*FileStore).Path}
	e.Store = newStore
	if again := runSynthetic(e, o); again.VirtualRequest || again.Decision != "reconcile_only" {
		t.Fatal(again)
	}
	// Even a new record for the same process generation cannot repeat it.
	o.Event.Record++
	o.Cursor.Record++
	if again := runSynthetic(e, o); again.VirtualRequest || again.Decision != "reconcile_only" {
		t.Fatal(again)
	}
}

func TestStopOrNewProcessDuringSave(t *testing.T) {
	for _, scenario := range []string{"stop", "generation", "new_process", "unknown", "timeout"} {
		t.Run(scenario, func(t *testing.T) {
			s, o := synthetic()
			e := testEngine(t, s, o)
			changed := false
			e.Store.(*FileStore).fault = func(stage string) error {
				if stage == "after_replace" {
					changed = true
				}
				return nil
			}
			e.Processes = processFunc(func(context.Context) (ProcessSample, error) {
				p := o.Process
				if changed && scenario == "new_process" {
					p.Presence = Present
					p.Instance = Fingerprint("new")
				}
				if changed && scenario == "unknown" {
					p.Presence = Unknown
				}
				return p, nil
			})
			e.Directives = directiveFunc(func(context.Context) (Directives, error) {
				d := o.Directives
				if changed && scenario == "stop" {
					d.Stop = true
				}
				if changed && scenario == "generation" {
					d.Generation++
				}
				return d, nil
			})
			e.Clock = clockFunc(func() time.Time {
				if changed && scenario == "timeout" {
					return o.At.Add(6 * time.Minute)
				}
				return o.At
			})
			r := runSynthetic(e, o)
			if r.VirtualRequest || !r.Consumed {
				t.Fatal(r)
			}
			if again := runSynthetic(e, o); again.VirtualRequest {
				t.Fatal("replayed after suppression", again)
			}
		})
	}
}

func TestPreflightBudgetIsDurable(t *testing.T) {
	s, o := synthetic()
	e := testEngine(t, s, o)
	e.Processes = processFunc(func(context.Context) (ProcessSample, error) { p := o.Process; p.Presence = Unknown; return p, nil })
	r := runSynthetic(e, o)
	if r.Decision != "unknown" || r.NextCheck.IsZero() {
		t.Fatal(r)
	}
	first, err := e.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if first.Receipts[Incident(o)].Rechecks != 1 {
		t.Fatal("preflight count not persisted")
	}
	// Before the saved time there is no extra read/recheck.
	if got := runSynthetic(e, o); got.Reason != "preflight_not_due" {
		t.Fatal(got)
	}
	o.At = o.At.Add(time.Minute)
	e.Clock = clockFunc(func() time.Time { return o.At })
	r = runSynthetic(e, o)
	second, err := e.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if r.VirtualRequest || second.Receipts[Incident(o)].Rechecks != 2 || second.Receipts[Incident(o)].Attempt != "unknown_exhausted" {
		t.Fatal(r, second.Receipts)
	}
	// Time alone cannot release an exhausted/unknown native-intent incident.
	o.At = o.At.Add(time.Minute)
	if got := runSynthetic(e, o); got.Reason != "preflight_exhausted" {
		t.Fatal(got)
	}
}

func TestCorruptStateIsPreserved(t *testing.T) {
	s, o := synthetic()
	e := testEngine(t, s, o)
	st := e.Store.(*FileStore)
	b, err := os.ReadFile(st.Path)
	if err != nil {
		t.Fatal(err)
	}
	b[len(b)/2] ^= 1
	if err = os.WriteFile(st.Path, b, 0600); err != nil {
		t.Fatal(err)
	}
	r := e.Tick(context.Background())
	if r.Decision != "unknown" || r.Reason != "state_unreadable" {
		t.Fatal(r)
	}
	after, err := os.ReadFile(st.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(b) {
		t.Fatal("corrupt evidence overwritten")
	}
}

func TestProductionTickCannotManufactureNoStop(t *testing.T) {
	s, o := synthetic()
	e := testEngine(t, s, o)
	// All fake OS signals are positive, including a fresh exact event and
	// confirmed absence. The production entrypoint still has no native proof.
	r := e.Tick(context.Background())
	if r.Decision != "unknown" || r.Reason != "native_intent_unknown" || r.VirtualRequest || r.LiveActivationEnabled || r.LiveActivationCalls != 0 {
		t.Fatal(r)
	}
}

func TestBaselineLogResetAndClock(t *testing.T) {
	s, o := synthetic()
	s.Initialized = false
	e := testEngine(t, s, o)
	e.Directives = UnknownDirectives{}
	if r := e.Tick(context.Background()); r.Decision != "baseline_only" || r.VirtualRequest {
		t.Fatal(r)
	}
	loaded, err := e.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Cursor != o.Cursor || loaded.Pending != nil {
		t.Fatal("baseline replayed old events")
	}
	o.Directives.Generation = loaded.StopGeneration // Keep this test about log/time integrity, not a new stop generation.
	o.Cursor.Epoch = "reset"
	if r := Decide(loaded, o); r.Reason != "log_reset" {
		t.Fatal(r)
	}
	o.Cursor = loaded.Cursor
	o.At = loaded.LastAt.Add(-time.Second)
	if r := Decide(loaded, o); r.Reason != "clock_reversed" {
		t.Fatal(r)
	}
}

func TestIntegrityUnknownDoesNotExpireIntoPermission(t *testing.T) {
	for _, problem := range []string{"clock", "log", "future_event"} {
		t.Run(problem, func(t *testing.T) {
			s, o := synthetic()
			e := testEngine(t, s, o)
			if problem == "clock" {
				o.At = s.LastAt.Add(-time.Second)
			}
			if problem == "log" {
				o.LogReset = true
			}
			if problem == "future_event" {
				o.Event.At = o.At.Add(time.Minute)
			}
			if r := runSynthetic(e, o); r.Decision != "unknown" || r.VirtualRequest {
				t.Fatal(r)
			}
			_, o = synthetic()
			o.At = o.At.Add(2 * time.Minute)
			o.Event.At = o.At.Add(-time.Second)
			if r := runSynthetic(e, o); r.Reason != "state_integrity_hold" || r.VirtualRequest {
				t.Fatal(r)
			}
		})
	}
}

func TestPreflightPIDReusedAndStopHold(t *testing.T) {
	s, o := synthetic()
	e := testEngine(t, s, o)
	e.Processes = processFunc(func(context.Context) (ProcessSample, error) {
		p := o.Process
		p.AbsenceFor = Fingerprint("reused-pid")
		return p, nil
	})
	if r := runSynthetic(e, o); r.VirtualRequest || r.Decision != "unknown" {
		t.Fatal(r)
	}
	s, o = synthetic()
	e = testEngine(t, s, o)
	o.Directives.Stop = true
	if r := runSynthetic(e, o); r.Decision != "suppressed" {
		t.Fatal(r)
	}
	o.Directives.Stop = false
	o.At = o.At.Add(2 * time.Minute)
	if r := runSynthetic(e, o); r.VirtualRequest || r.Decision != "suppressed" {
		t.Fatal(r)
	}
}

func TestResetErrorIsPersistedAsIntegrityHold(t *testing.T) {
	s, o := synthetic()
	e := testEngine(t, s, o)
	e.Events = eventFunc(func(context.Context, Cursor, ProcessSample, string) (Cursor, *Event, error) {
		return Cursor{}, nil, ErrLogReset
	})
	if r := e.Tick(context.Background()); r.Decision != "unknown" {
		t.Fatal(r)
	}
	state, err := e.Store.Load()
	if err != nil || state.IntegrityHold != "log_reset" {
		t.Fatal(state, err)
	}
}

func TestSuppressionKeepsDurableConsumptionVisible(t *testing.T) {
	s, o := synthetic()
	e := testEngine(t, s, o)
	if r := runSynthetic(e, o); !r.Consumed || !r.VirtualRequest {
		t.Fatal(r)
	}
	o.Directives.Stop = true
	r := runSynthetic(e, o)
	if r.Decision != "suppressed" || !r.Consumed || r.ConsumeState != "intent_committed" || r.Attempts != 1 || r.VirtualRequest {
		t.Fatal(r)
	}
}

func TestStopGenerationAcrossThreeTicks(t *testing.T) {
	for _, early := range []string{"not_due", "present", "process_unknown", "scope_unknown", "no_event"} {
		t.Run(early, func(t *testing.T) {
			s, o := synthetic()
			e := testEngine(t, s, o)
			e.Processes = processFunc(func(context.Context) (ProcessSample, error) { p := o.Process; p.Presence = Unknown; return p, nil })
			if r := runSynthetic(e, o); r.Decision != "unknown" || r.NextCheck.IsZero() {
				t.Fatal(r)
			}
			// The second tick sees a changed directive generation while returning
			// before eligibility or while the persisted next-read time is pending.
			second := o
			second.At = o.At.Add(30 * time.Second)
			second.Directives.Generation = 8
			if early == "present" {
				second.Process.Presence = Present
			}
			if early == "process_unknown" {
				second.Process.Presence = Unknown
			}
			if early == "scope_unknown" {
				second.Process.ScopeKnown = false
			}
			if early == "no_event" {
				second.Event = nil
			}
			if r := runSynthetic(e, second); r.VirtualRequest {
				t.Fatal("request on generation change", r)
			}
			// Reload, as a new runner would. A delayed old candidate must remain
			// invalid even after the preflight next-read time has passed.
			e.Store = &FileStore{Path: e.Store.(*FileStore).Path}
			third := o
			third.At = o.At.Add(70 * time.Second)
			third.Directives.Generation = 8
			e.Clock = clockFunc(func() time.Time { return third.At })
			e.Processes = processFunc(func(context.Context) (ProcessSample, error) { return third.Process, nil })
			e.Directives = directiveFunc(func(context.Context) (Directives, error) { return third.Directives, nil })
			if r := runSynthetic(e, third); r.VirtualRequest || r.Decision != "suppressed" {
				t.Fatal("old candidate survived generation change", r)
			}
		})
	}
}

func TestEventReadFailureDoesNotCreateIntegrityHold(t *testing.T) {
	s, o := synthetic()
	e := testEngine(t, s, o)
	e.Events = eventFunc(func(context.Context, Cursor, ProcessSample, string) (Cursor, *Event, error) {
		ev := *o.Event
		ev.Epoch = "partial-untrusted-epoch"
		return Cursor{}, &ev, errors.New("temporary_read_failure")
	})
	if r := e.Tick(context.Background()); r.Decision != "unknown" || r.VirtualRequest {
		t.Fatal(r)
	}
	loaded, err := e.Store.Load()
	if err != nil || loaded.IntegrityHold != "" {
		t.Fatal("transient I/O became a permanent hold", loaded.IntegrityHold, err)
	}
	e.Events = eventFunc(func(context.Context, Cursor, ProcessSample, string) (Cursor, *Event, error) {
		return o.Cursor, o.Event, nil
	})
	if r := e.Tick(context.Background()); r.Reason != "native_intent_unknown" || r.VirtualRequest {
		t.Fatal(r)
	}
}

func TestStopWinsOverPreflightDeadline(t *testing.T) {
	s, o := synthetic()
	e := testEngine(t, s, o)
	e.Clock = clockFunc(func() time.Time { return o.At.Add(PreflightWindow + time.Second) })
	e.Directives = directiveFunc(func(context.Context) (Directives, error) { d := o.Directives; d.Stop = true; return d, nil })
	r := runSynthetic(e, o)
	if r.Decision != "suppressed" || r.VirtualRequest {
		t.Fatal(r)
	}
	loaded, err := e.Store.Load()
	if err != nil || loaded.Receipts[Incident(o)].Attempt != "suppressed" {
		t.Fatal(loaded.Receipts, err)
	}
}

func TestTickRejectsMismatchedGenerationAsUnknown(t *testing.T) {
	s, o := synthetic()
	e := testEngine(t, s, o)
	e.Events = eventFunc(func(context.Context, Cursor, ProcessSample, string) (Cursor, *Event, error) {
		ev := *o.Event
		ev.Instance = Fingerprint("another-process-generation")
		ev.SessionMatches = false
		return o.Cursor, &ev, nil
	})
	r := e.Tick(context.Background())
	if r.Decision != "unknown" || r.Reason != "process_generation_or_exit_unverified" || r.VirtualRequest {
		t.Fatal(r)
	}
}

func TestCandidateGenerationRemainsPinnedWhenScopeIsLost(t *testing.T) {
	s, o := synthetic()
	e := testEngine(t, s, o)
	e.Processes = processFunc(func(context.Context) (ProcessSample, error) { p := o.Process; p.Presence = Unknown; return p, nil })
	if r := runSynthetic(e, o); r.Decision != "unknown" {
		t.Fatal(r)
	}
	now := o.At.Add(30 * time.Second)
	e.Clock = clockFunc(func() time.Time { return now })
	e.Directives = directiveFunc(func(context.Context) (Directives, error) { d := o.Directives; d.Generation = 8; return d, nil })
	e.Processes = processFunc(func(context.Context) (ProcessSample, error) {
		return ProcessSample{}, errors.New("temporary_process_failure")
	})
	if r := e.Tick(context.Background()); r.VirtualRequest {
		t.Fatal(r)
	}
	loaded, err := e.Store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if rec := loaded.Receipts[Incident(o)]; !rec.GenerationKnown || rec.Generation != 7 || rec.Attempt != "suppressed" {
		t.Fatal("candidate adopted a newer generation", rec)
	}
	third := o
	third.At = o.At.Add(70 * time.Second)
	third.Directives.Generation = 8
	if r := runSynthetic(e, third); r.VirtualRequest || r.Decision != "suppressed" {
		t.Fatal(r)
	}
}

func TestUnknownCandidateGenerationCannotBeAdoptedOnReload(t *testing.T) {
	s, o := synthetic()
	e := testEngine(t, s, o)
	first := o
	first.Directives.Fresh = false
	if r := runSynthetic(e, first); r.Decision != "unknown" {
		t.Fatal(r)
	}
	o.At = o.At.Add(time.Minute)
	if r := runSynthetic(e, o); r.VirtualRequest || r.Reason != "candidate_stop_generation_unknown" {
		t.Fatal(r)
	}
}

func TestPruneOnlyExpiredRollingRequests(t *testing.T) {
	s, o := synthetic()
	s.Requests = []time.Time{o.At.Add(-2 * time.Hour), o.At.Add(-time.Hour), o.At.Add(-30 * time.Minute)}
	n := Advance(s, o, Decide(s, o))
	if len(n.Requests) != 1 || !n.Requests[0].Equal(o.At.Add(-30*time.Minute)) || len(s.Requests) != 3 {
		t.Fatal(n.Requests, s.Requests)
	}
	if _, ok := n.Receipts[Incident(o)]; !ok {
		t.Fatal("candidate receipt missing")
	}
}

func TestStoredStateExplicitlyKeepsLiveActivationDisabled(t *testing.T) {
	s, o := synthetic()
	e := testEngine(t, s, o)
	if r := runSynthetic(e, o); !r.VirtualRequest {
		t.Fatal(r)
	}
	st := e.Store.(*FileStore)
	b, err := os.ReadFile(st.Path)
	if err != nil {
		t.Fatal(err)
	}
	var env envelope
	if json.Unmarshal(b, &env) != nil {
		t.Fatal("invalid envelope")
	}
	var fields map[string]any
	if json.Unmarshal(env.State, &fields) != nil || fields["live_activation_enabled"] != false || fields["live_activation_calls"] != float64(0) {
		t.Fatal("state omitted disabled live activation fields")
	}
	loaded, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	loaded.LiveActivationEnabled = true
	if st.Save(loaded) == nil {
		t.Fatal("state enabled live activation")
	}
	loaded.LiveActivationEnabled = false
	loaded.LiveActivationCalls = 1
	if st.Save(loaded) == nil {
		t.Fatal("state accepted live activation calls")
	}
}

func TestStopCannotRelabelChangedScopeOrLogAsTrusted(t *testing.T) {
	for _, change := range []string{"target", "log"} {
		t.Run(change, func(t *testing.T) {
			s, o := synthetic()
			e := testEngine(t, s, o)
			o.Directives.Generation++
			if change == "target" {
				o.Process.Target = Fingerprint("other-session")
			} else {
				o.Cursor.Epoch = "reset"
				o.Event.Epoch = "reset"
			}
			if r := runSynthetic(e, o); r.Decision != "suppressed" || r.VirtualRequest {
				t.Fatal(r)
			}
			loaded, err := e.Store.Load()
			if err != nil || loaded.IntegrityHold == "" || loaded.Target != s.Target || loaded.Cursor != s.Cursor {
				t.Fatal("early stop adopted changed scope/log", loaded, err)
			}
		})
	}
}
