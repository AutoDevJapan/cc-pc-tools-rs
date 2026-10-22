// Package observer makes finite, conservative app-lifecycle observations. It has
// no operating-system activation adapter. Synthetic evidence is test-only.
package observer

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

const AUMID = "OpenAI.Codex_2p2nqsd0c76g0!App"
const PackageFamily = "OpenAI.Codex_2p2nqsd0c76g0"

const (
	EventTTL        = 5 * time.Minute
	IncidentGap     = 10 * time.Minute
	RollingWindow   = time.Hour
	RollingLimit    = 3
	PreflightLimit  = 2
	PreflightWindow = 3 * time.Minute
)

type Presence string

const (
	Unknown Presence = "unknown"
	Present Presence = "present"
	Absent  Presence = "absent"
)

type NativeIntent string

const (
	IntentUnknown NativeIntent = "unknown"
	Stopped       NativeIntent = "stopped"
	SignedOut     NativeIntent = "signed_out"
)

type Cursor struct {
	Epoch  string `json:"epoch"`
	Record uint64 `json:"record"`
	Anchor string `json:"anchor"`
}

// ProcessSample contains only scoped fingerprints, never command lines.
type ProcessSample struct {
	Presence   Presence
	Target     string
	Instance   string
	ScopeKnown bool
	InScope    bool
	AbsenceFor string // Revalidated missing PID/start generation, if available.
}

type Directives struct {
	Known      bool
	Fresh      bool
	Generation uint64
	Stop       bool // Includes cancellation and deferral.
}

type Event struct {
	Epoch          string    `json:"epoch"`
	Record         uint64    `json:"record"`
	At             time.Time `json:"at"`
	Instance       string    `json:"instance"`
	PackageMatches bool      `json:"package_matches"`
	SessionMatches bool      `json:"session_matches"`
	SchemaKnown    bool      `json:"schema_known"`
	ExitVerified   bool      `json:"exit_verified"`
}

type Observation struct {
	At              time.Time
	Process         ProcessSample
	Directives      Directives
	Intent          NativeIntent
	Cursor          Cursor
	Event           *Event
	InstanceMatches bool
	LaterInstance   bool
	LogReset        bool
	// Only package-local synthetic fixtures set this. No exported constructor,
	// CLI flag, config, or production observer can assert native no-stop proof.
	syntheticNoStop bool
}

type Receipt struct {
	Attempt          string    `json:"attempt"`
	At               time.Time `json:"at"`
	Rechecks         int       `json:"rechecks"`
	PreflightStarted time.Time `json:"preflight_started"`
	NextCheck        time.Time `json:"next_check"`
}

type State struct {
	Version        int                `json:"version"`
	Target         string             `json:"target"`
	Initialized    bool               `json:"initialized"`
	Cursor         Cursor             `json:"cursor"`
	LastAt         time.Time          `json:"last_at"`
	LastInstance   string             `json:"last_instance"`
	StopGeneration uint64             `json:"stop_generation"`
	Receipts       map[string]Receipt `json:"receipts"`
	Requests       []time.Time        `json:"requests"`
	Pending        *Event             `json:"pending,omitempty"`
	IntegrityHold  string             `json:"integrity_hold,omitempty"`
}

type Result struct {
	At                    time.Time `json:"at"`
	Target                string    `json:"target_fingerprint,omitempty"`
	Incident              string    `json:"incident_fingerprint,omitempty"`
	Stage                 string    `json:"stage"`
	Decision              string    `json:"decision"`
	Reason                string    `json:"reason"`
	Consumed              bool      `json:"consumed"`
	ConsumeState          string    `json:"consume_state"`
	Attempts              int       `json:"attempts"`
	NextCheck             time.Time `json:"next_check,omitempty"`
	LiveActivationEnabled bool      `json:"live_activation_enabled"`
	LiveActivationCalls   int       `json:"live_activation_calls"`
	// A virtual request is returned only after the durable consume barrier.
	// It is handled by a fake adapter in _test.go, never by production code.
	VirtualRequest bool `json:"-"`
}

func Fingerprint(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func Incident(o Observation) string {
	if o.Event == nil {
		return ""
	}
	return Fingerprint(o.Process.Target, o.Event.Epoch, o.Event.Instance)
}

func NewState() State { return State{Version: 1, Receipts: map[string]Receipt{}} }

// Decide is pure. Persistence, revalidation and virtual delivery are separate.
func Decide(s State, o Observation) Result {
	r := Result{At: o.At, Target: o.Process.Target, Incident: Incident(o), Stage: "decision", ConsumeState: "not_consumed"}
	// Stop/error decisions must still report an earlier durable consume
	// accurately. Suppression does not undo a previously committed receipt.
	if prior, ok := s.Receipts[r.Incident]; ok {
		switch prior.Attempt {
		case "intent_committed", "unconfirmed", "completed", "failed_consumed":
			r.Consumed = true
			r.Attempts = 1
			r.ConsumeState = prior.Attempt
		}
	}
	answer := func(decision, reason string) Result { r.Decision = decision; r.Reason = reason; return r }
	// A positive stop dominates every other observation, including stale data.
	if o.Directives.Stop || o.Intent == Stopped || o.Intent == SignedOut {
		return answer("suppressed", "stop_or_signout")
	}
	if s.IntegrityHold != "" {
		return answer("unknown", "state_integrity_hold")
	}
	if !o.Process.ScopeKnown || o.Process.Target == "" {
		return answer("unknown", "target_scope_unknown")
	}
	if !o.Process.InScope {
		return answer("out_of_scope", "target_mismatch")
	}
	if o.At.IsZero() || (!s.LastAt.IsZero() && o.At.Before(s.LastAt)) {
		return answer("unknown", "clock_reversed")
	}
	if s.Initialized && (s.Version != 1 || s.Target != o.Process.Target) {
		return answer("unknown", "state_target_mismatch")
	}
	if o.LogReset {
		return answer("unknown", "log_reset")
	}
	if o.Cursor.Epoch == "" {
		return answer("unknown", "event_cursor_unknown")
	}
	if s.Initialized && (s.Cursor.Epoch != o.Cursor.Epoch || o.Cursor.Record < s.Cursor.Record) {
		return answer("unknown", "log_reset")
	}
	if !s.Initialized {
		return answer("baseline_only", "initial_baseline")
	}
	if !o.Directives.Known || !o.Directives.Fresh {
		return answer("unknown", "stop_cursor_unknown")
	}
	if o.LaterInstance {
		return answer("superseded", "new_process_generation")
	}
	if o.Process.Presence == Present {
		return answer("retain", "process_present")
	}
	if o.Process.Presence != Absent {
		return answer("unknown", "process_unknown")
	}
	if o.Event == nil {
		return answer("observe", "no_fresh_event")
	}
	e := o.Event
	if !e.PackageMatches || !e.SessionMatches {
		return answer("out_of_scope", "event_target_mismatch")
	}
	if e.At.IsZero() || e.At.After(o.At) || e.Epoch != o.Cursor.Epoch {
		return answer("unknown", "event_time_or_epoch_unknown")
	}
	if o.At.Sub(e.At) > EventTTL {
		return answer("expired", "event_ttl")
	}
	if e.Record > o.Cursor.Record {
		return answer("unknown", "event_cursor_unknown")
	}
	if old, ok := s.Receipts[r.Incident]; ok {
		switch old.Attempt {
		case "intent_committed", "unconfirmed":
			r.ConsumeState = old.Attempt
			r.Consumed = true
			r.Attempts = 1
			return answer("reconcile_only", "request_outcome_unknown")
		case "completed", "failed_consumed":
			r.ConsumeState = old.Attempt
			r.Consumed = true
			r.Attempts = 1
			return answer("already_consumed", "incident_consumed")
		case "suppressed":
			return answer("suppressed", "incident_stopped")
		case "superseded":
			return answer("superseded", "incident_superseded")
		case "unknown_exhausted":
			return answer("unknown", "preflight_exhausted")
		}
		if old.Rechecks >= PreflightLimit || (!old.PreflightStarted.IsZero() && o.At.Sub(old.PreflightStarted) >= PreflightWindow) {
			return answer("unknown", "preflight_exhausted")
		}
		if !old.NextCheck.IsZero() && o.At.Before(old.NextCheck) {
			r.NextCheck = old.NextCheck
			return answer("unknown", "preflight_not_due")
		}
	}
	if s.StopGeneration != o.Directives.Generation {
		return answer("suppressed", "stop_generation_changed")
	}
	// Pending events have already advanced the durable cursor, but have not
	// crossed the consume barrier. All other old records are never backfilled.
	pending := s.Pending != nil && s.Pending.Record == e.Record && s.Pending.Instance == e.Instance
	if e.Record == 0 || (e.Record <= s.Cursor.Record && !pending) {
		return answer("expired", "before_bookmark")
	}
	if !o.InstanceMatches || !e.SchemaKnown || !e.ExitVerified || e.Instance == "" {
		return answer("unknown", "process_generation_or_exit_unverified")
	}
	if !o.syntheticNoStop {
		return answer("unknown", "native_intent_unknown")
	}
	count := 0
	var latest, oldest time.Time
	for _, at := range s.Requests {
		if at.After(o.At) {
			return answer("unknown", "request_time_in_future")
		}
		if o.At.Sub(at) < RollingWindow {
			count++
			if oldest.IsZero() || at.Before(oldest) {
				oldest = at
			}
			if at.After(latest) {
				latest = at
			}
		}
	}
	if !latest.IsZero() && o.At.Sub(latest) < IncidentGap {
		r.NextCheck = latest.Add(IncidentGap)
		return answer("cooldown", "distinct_incident_gap")
	}
	if count >= RollingLimit {
		r.NextCheck = oldest.Add(RollingWindow)
		return answer("budget_exhausted", "rolling_hour_budget")
	}
	return answer("eligible_in_fake_only", "synthetic_evidence_only")
}

// Advance returns a new value; it never mutates the caller's state or maps.
func Advance(s State, o Observation, r Result) State {
	n := s
	n.Receipts = make(map[string]Receipt, len(s.Receipts)+1)
	for k, v := range s.Receipts {
		n.Receipts[k] = v
	}
	n.Requests = append([]time.Time(nil), s.Requests...)
	if o.Event != nil && (o.Event.At.After(o.At) || o.Event.Epoch != o.Cursor.Epoch) {
		n.IntegrityHold = "event_time_or_epoch_unknown"
		return n
	}
	if r.Reason == "clock_reversed" || r.Reason == "log_reset" || r.Reason == "state_target_mismatch" || r.Reason == "event_time_or_epoch_unknown" {
		n.IntegrityHold = r.Reason
		return n
	}
	if r.Reason == "clock_reversed" || r.Reason == "log_reset" || r.Reason == "state_target_mismatch" || r.Reason == "event_cursor_unknown" || r.Reason == "event_time_or_epoch_unknown" {
		return n
	}
	n.LastAt = o.At
	if o.Process.Target != "" {
		n.Target = o.Process.Target
	}
	if r.Decision == "baseline_only" {
		n.Initialized = true
		n.Cursor = o.Cursor
		n.Pending = nil
		n.StopGeneration = o.Directives.Generation
		if o.Process.Presence == Present {
			n.LastInstance = o.Process.Instance
		}
		return n
	}
	if !s.Initialized {
		return n
	}
	if o.Cursor.Epoch == s.Cursor.Epoch && o.Cursor.Record >= s.Cursor.Record {
		n.Cursor = o.Cursor
	}
	if o.Directives.Known && o.Directives.Fresh {
		n.StopGeneration = o.Directives.Generation
	}
	if o.Process.Presence == Present && o.Process.Instance != "" {
		n.LastInstance = o.Process.Instance
	}
	if o.Event != nil {
		e := *o.Event
		n.Pending = &e
	}
	if r.Incident != "" {
		receipt := n.Receipts[r.Incident]
		if receipt.Attempt == "" {
			switch r.Decision {
			case "suppressed", "superseded":
				receipt.Attempt = r.Decision
				receipt.At = o.At
			case "expired", "out_of_scope":
				n.Pending = nil
			}
		}
		if r.Reason == "preflight_exhausted" {
			receipt.Attempt = "unknown_exhausted"
			receipt.At = o.At
			n.Pending = nil
		}
		if receipt.Attempt != "" || receipt.Rechecks != 0 {
			n.Receipts[r.Incident] = receipt
		}
		if r.Consumed || receipt.Attempt == "suppressed" || receipt.Attempt == "superseded" {
			n.Pending = nil
		}
	}
	return n
}
