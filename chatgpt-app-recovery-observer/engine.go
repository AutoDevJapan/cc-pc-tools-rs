package observer

import (
	"context"
	"errors"
	"time"
)

var (
	ErrBusy     = errors.New("state_busy")
	ErrCorrupt  = errors.New("state_corrupt")
	ErrLogReset = errors.New("event_log_reset")
)

type Clock interface{ Now() time.Time }
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

type ProcessReader interface {
	ReadProcess(context.Context) (ProcessSample, error)
}
type EventReader interface {
	ReadEvents(context.Context, Cursor, ProcessSample, string) (Cursor, *Event, error)
}
type DirectiveReader interface {
	ReadDirectives(context.Context) (Directives, error)
}

// UnknownDirectives is intentional until a regular stop-generation source is
// connected in a separately authorized integration. It never invents consent.
type UnknownDirectives struct{}

func (UnknownDirectives) ReadDirectives(context.Context) (Directives, error) {
	return Directives{}, nil
}

type Store interface {
	Lock(context.Context) (func(), error)
	Load() (State, error)
	Save(State) error
}

type Engine struct {
	Store      Store
	Clock      Clock
	Processes  ProcessReader
	Events     EventReader
	Directives DirectiveReader
}

// Tick performs one finite observation. There is deliberately no activation
// interface, executable launch, retry loop, timer, or sleeping goroutine here.
func (e Engine) Tick(ctx context.Context) Result {
	r := Result{At: e.Clock.Now(), Stage: "lock", ConsumeState: "unknown"}
	if ctx.Err() != nil {
		r.Decision = "unknown"
		r.Reason = "deadline"
		return r
	}
	unlock, err := e.Store.Lock(ctx)
	if err != nil {
		r.Decision = "busy"
		r.Reason = "state_lock_unavailable"
		return r
	}
	defer unlock()
	s, err := e.Store.Load()
	if err != nil {
		r.Stage = "load"
		r.Decision = "unknown"
		r.Reason = "state_unreadable"
		return r
	}
	p, perr := e.Processes.ReadProcess(ctx)
	c, ev, everr := e.Events.ReadEvents(ctx, s.Cursor, p, s.LastInstance)
	d, derr := e.Directives.ReadDirectives(ctx)
	if perr != nil {
		p.Presence = Unknown
		p.ScopeKnown = false
	}
	if derr != nil {
		d.Known = false
		d.Fresh = false
	}
	if everr != nil {
		c = Cursor{}
		ev = nil
	}
	if everr == nil && ev == nil && s.Pending != nil && c.Epoch == s.Pending.Epoch {
		copy := *s.Pending
		ev = &copy
	}
	o := Observation{At: e.Clock.Now(), Process: p, Directives: d, Cursor: c, Event: ev, Intent: IntentUnknown,
		LogReset:        errors.Is(everr, ErrLogReset),
		InstanceMatches: ev != nil && ev.Instance == s.LastInstance && s.LastInstance != "",
		LaterInstance:   ev != nil && p.Presence == Present && p.Instance != "" && ev.Instance != p.Instance}
	return e.finish(ctx, s, o)
}

// finish is package-local so synthetic fixtures can supply assumptions without
// adding a production flag or a public no-stop evidence constructor.
func (e Engine) finish(ctx context.Context, s State, o Observation) Result {
	r := Decide(s, o)
	if ctx.Err() != nil {
		r.Decision = "unknown"
		r.Reason = "deadline"
		return r
	}
	n := Advance(s, o, r)
	if r.Decision != "eligible_in_fake_only" {
		if err := e.Store.Save(n); err != nil {
			r.Decision = "persist_failed"
			r.Reason = "state_save_failed"
			r.Stage = "save"
		}
		return r
	}
	// One recheck per invocation, at most two in the persisted three-minute
	// window. A subsequent caller receives the saved next-check time.
	receipt := n.Receipts[r.Incident]
	if receipt.PreflightStarted.IsZero() {
		receipt.PreflightStarted = o.At
	}
	receipt.Rechecks++
	p, perr := e.Processes.ReadProcess(ctx)
	d, derr := e.Directives.ReadDirectives(ctx)
	now := e.Clock.Now()
	check := o
	check.At = now
	check.Process = p
	check.Directives = d
	check.InstanceMatches = perr == nil && p.Presence == Absent && p.Target == o.Process.Target && p.AbsenceFor == o.Event.Instance
	if perr != nil {
		check.Process.Presence = Unknown
	}
	if derr != nil {
		check.Directives.Known = false
		check.Directives.Fresh = false
	}
	check.LaterInstance = p.Presence == Present && p.Instance != o.Event.Instance
	pre := Decide(s, check)
	if d.Stop || (d.Known && d.Fresh && d.Generation != o.Directives.Generation) {
		pre.Decision = "suppressed"
		pre.Reason = "stop_generation_changed"
	}
	if pre.Decision != "suppressed" && (now.Before(o.At) || now.Sub(receipt.PreflightStarted) >= PreflightWindow || ctx.Err() != nil) {
		pre.Decision = "unknown"
		pre.Reason = "preflight_deadline"
	}
	if pre.Decision != "eligible_in_fake_only" {
		if now.After(n.LastAt) {
			n.LastAt = now
		}
		if pre.Decision == "suppressed" || pre.Decision == "superseded" {
			receipt.Attempt = pre.Decision
			receipt.At = n.LastAt
		}
		if pre.Decision != "suppressed" && pre.Decision != "superseded" && (receipt.Rechecks >= PreflightLimit || pre.Reason == "preflight_deadline") {
			receipt.Attempt = "unknown_exhausted"
			receipt.At = n.LastAt
		}
		receipt.NextCheck = now.Add(time.Minute)
		if receipt.Attempt != "" {
			receipt.NextCheck = time.Time{}
		}
		n.Receipts[r.Incident] = receipt
		pre.NextCheck = receipt.NextCheck
		pre.Stage = "preflight"
		if err := e.Store.Save(n); err != nil {
			pre.Decision = "persist_failed"
			pre.Reason = "state_save_failed"
		}
		return pre
	}
	receipt.Attempt = "intent_committed"
	receipt.At = now
	receipt.NextCheck = time.Time{}
	n.Receipts[r.Incident] = receipt
	n.Pending = nil
	n.LastAt = now
	n.Requests = append(n.Requests, now)
	// Durable consume must succeed before even a virtual request is returned.
	if err := e.Store.Save(n); err != nil {
		r.Decision = "persist_failed"
		r.Reason = "consume_save_failed"
		r.Stage = "consume"
		r.ConsumeState = "unknown"
		return r
	}
	r.Consumed = true
	r.ConsumeState = "intent_committed"
	r.Attempts = 1
	r.Stage = "consumed"
	// A stop/new process during durable saving invalidates the old plan too.
	p, perr = e.Processes.ReadProcess(ctx)
	d, derr = e.Directives.ReadDirectives(ctx)
	finalAt := e.Clock.Now()
	if d.Stop || (d.Known && d.Fresh && d.Generation != o.Directives.Generation) {
		r.Decision = "suppressed"
		r.Reason = "stop_during_consume"
		return r
	}
	if derr != nil || !d.Known || !d.Fresh || perr != nil || !p.ScopeKnown || !p.InScope || p.Target != o.Process.Target || (p.Presence != Absent && p.Presence != Present) {
		r.Decision = "unknown"
		r.Reason = "post_consume_observation_unknown"
		return r
	}
	if p.Presence == Present {
		r.Decision = "superseded"
		r.Reason = "process_during_consume"
		return r
	}
	if p.AbsenceFor != o.Event.Instance {
		r.Decision = "unknown"
		r.Reason = "post_consume_generation_unknown"
		return r
	}
	if ctx.Err() != nil || finalAt.Before(now) || finalAt.Sub(o.Event.At) > EventTTL || finalAt.Sub(receipt.PreflightStarted) >= PreflightWindow {
		r.Decision = "unknown"
		r.Reason = "post_consume_deadline"
		return r
	}
	r.VirtualRequest = true
	return r
}
