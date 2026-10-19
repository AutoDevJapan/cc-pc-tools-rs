package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ProcID is a process identity: the PID alone is never enough because the OS
// reuses PIDs. Both fields must match for a process to count as the recorded one.
type ProcID struct {
	PID     uint32 `json:"pid"`
	Created uint64 `json:"created"`
}

func (p ProcID) zero() bool { return p.PID == 0 }

// Entry states. reserved -> launching -> running -> collecting -> a terminal
// state. Terminal states are never left, so nothing is ever run twice.
const (
	StateReserved      = "reserved"
	StateLaunching     = "launching"
	StateRunning       = "running"
	StateCollecting    = "collecting"
	StateSucceeded     = "succeeded"
	StateFailed        = "failed"
	StateRefused       = "refused"
	StateNeedsRecovery = "needs_recovery"
)

func activeState(s string) bool {
	return s == StateLaunching || s == StateRunning || s == StateCollecting
}

func terminalState(s string) bool {
	return s == StateSucceeded || s == StateFailed || s == StateRefused
}

// QueuedRec, StartRec, OfficialRec and CleanupRec are recorded separately:
// being queued, being started, the official success and the cleanup are four
// different facts and none implies another.
type QueuedRec struct {
	At string `json:"at"`
}
type StartRec struct {
	IntentAt   string `json:"intent_at,omitempty"`
	Attempts   int    `json:"attempts"`
	Supervisor ProcID `json:"supervisor"`
	Breakaway  bool   `json:"breakaway,omitempty"`
	HelperAt   string `json:"helper_at,omitempty"`
	Helper     ProcID `json:"helper"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	ExitedAt   string `json:"exited_at,omitempty"`
}
type OfficialRec struct {
	CheckedAt string `json:"checked_at,omitempty"`
	Success   bool   `json:"success"`
	Reason    string `json:"reason,omitempty"`
}
type CleanupRec struct {
	State        string `json:"state"` // none, create_intent, owned, collected, evidence, removed, quarantined, failed
	Instance     string `json:"instance,omitempty"`
	SafeDeleteID string `json:"safe_delete_id,omitempty"`
	Root         string `json:"root,omitempty"`
	Evidence     string `json:"evidence,omitempty"`
	ManifestSHA  string `json:"manifest_sha256,omitempty"`
	Detail       string `json:"detail,omitempty"`
}

type Entry struct {
	Key      string      `json:"key"`
	Plan     Plan        `json:"plan"`
	OS       string      `json:"os"`
	Phase    string      `json:"phase"` // initial, normal, continuation
	State    string      `json:"state"`
	Queued   QueuedRec   `json:"queued"`
	Started  StartRec    `json:"started"`
	Official OfficialRec `json:"official"`
	Cleanup  CleanupRec  `json:"cleanup"`
	Reason   string      `json:"reason,omitempty"`
}

// Budget is the persistent, one-time reservation: a key is appended exactly
// once, in the same atomic ledger write that creates its entry.
type Budget struct {
	Initial      []string `json:"initial"`
	Normal       []string `json:"normal"`
	Continuation []string `json:"continuation,omitempty"`
}

type Fence struct {
	Key    string `json:"key"`
	Reason string `json:"reason"`
	At     string `json:"at"`
}

type Refusal struct {
	At     string `json:"at"`
	Reason string `json:"reason"`
	Count  int    `json:"count"`
	Next   string `json:"next"`
}

type Ledger struct {
	Version  int                `json:"version"`
	Budget   Budget             `json:"budget"`
	Entries  map[string]*Entry  `json:"entries"`
	Fences   map[string]Fence   `json:"fences"`
	Refusals map[string]Refusal `json:"refusals"`
}

func newLedger() *Ledger {
	return &Ledger{Version: 1, Entries: map[string]*Entry{}, Fences: map[string]Fence{}, Refusals: map[string]Refusal{}}
}

const maxLedger = 4 << 20
const maxRefusals = 128

// Store serialises every ledger change behind one short OS lock and replaces
// the file atomically, so a crash leaves either the old or the new ledger.
type Store struct {
	Path string
	Now  func() time.Time
}

func (s *Store) lockPath() string { return s.Path + ".lock" }

func (s *Store) load() (*Ledger, error) {
	f, e := os.Open(s.Path)
	if os.IsNotExist(e) {
		return newLedger(), nil
	}
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, maxLedger+1))
	if e != nil || len(b) > maxLedger {
		return nil, errors.New("ledger unreadable or too large")
	}
	l := newLedger()
	if e = json.Unmarshal(b, l); e != nil || l.Version != 1 {
		return nil, errors.New("ledger rejected")
	}
	if l.Entries == nil {
		l.Entries = map[string]*Entry{}
	}
	if l.Fences == nil {
		l.Fences = map[string]Fence{}
	}
	if l.Refusals == nil {
		l.Refusals = map[string]Refusal{}
	}
	return l, nil
}

func (s *Store) save(l *Ledger) error {
	b, e := json.MarshalIndent(l, "", "  ")
	if e != nil {
		return e
	}
	if len(b) > maxLedger {
		return errors.New("ledger limit exceeded")
	}
	if e = os.MkdirAll(filepath.Dir(s.Path), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(s.Path), ".ledger-*.tmp")
	if e != nil {
		return e
	}
	name := f.Name()
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	if c := f.Close(); e == nil {
		e = c
	}
	if e == nil {
		e = replaceFile(name, s.Path)
	}
	if e != nil {
		_ = os.Remove(name)
	}
	return e
}

// Update runs fn under the lock; the ledger is written only when fn succeeds.
func (s *Store) Update(fn func(*Ledger) error) error {
	if e := os.MkdirAll(filepath.Dir(s.Path), 0700); e != nil {
		return e
	}
	var unlock func()
	var e error
	for i := 0; i < 100; i++ {
		if unlock, e = tryLock(s.lockPath()); e == nil {
			break
		}
		if e != errLockBusy {
			return e
		}
		time.Sleep(50 * time.Millisecond)
	}
	if e != nil {
		return errors.New("ledger lock busy")
	}
	defer unlock()
	l, e := s.load()
	if e != nil {
		return e
	}
	if e = fn(l); e != nil {
		return e
	}
	pruneRefusals(l)
	return s.save(l)
}

// View is a snapshot taken under the same lock discipline (read only).
func (s *Store) View() (*Ledger, error) {
	var out *Ledger
	err := s.Update(func(l *Ledger) error {
		b, e := json.Marshal(l)
		if e != nil {
			return e
		}
		out = newLedger()
		if e = json.Unmarshal(b, out); e != nil {
			return e
		}
		return errNoWrite
	})
	if err == errNoWrite {
		err = nil
	}
	return out, err
}

var errNoWrite = errors.New("no write")

func pruneRefusals(l *Ledger) {
	if len(l.Refusals) <= maxRefusals {
		return
	}
	type kv struct{ k, at string }
	var all []kv
	for k, r := range l.Refusals {
		all = append(all, kv{k, r.At})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].at < all[j].at })
	for _, x := range all[:len(all)-maxRefusals] {
		delete(l.Refusals, x.k)
	}
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// reserve admits a verified plan once. It returns false (and changes nothing)
// when the key is already known or the one-time budget is exhausted.
func (l *Ledger) reserve(p Plan, osName string, continuation bool, now time.Time) (*Entry, string) {
	key := p.Key()
	if _, ok := l.Entries[key]; ok {
		return nil, "duplicate"
	}
	phase := "normal"
	var list *[]string
	var limit int
	if p.PR == OriginalPR && p.Event != "push" {
		phase, list, limit = "initial", &l.Budget.Initial, InitialBudget
	} else {
		list, limit = &l.Budget.Normal, NormalBudget
	}
	if len(*list) >= limit {
		if !continuation {
			return nil, "budget_exhausted"
		}
		phase, list, limit = "continuation", &l.Budget.Continuation, 1<<30
	}
	*list = append(*list, key)
	e := &Entry{Key: key, Plan: p, OS: osName, Phase: phase, State: StateReserved, Queued: QueuedRec{At: stamp(now)}, Cleanup: CleanupRec{State: "none"}}
	l.Entries[key] = e
	delete(l.Refusals, key)
	return e, ""
}

func (l *Ledger) activeFor(osName string) *Entry {
	for _, e := range l.Entries {
		if e.OS == osName && (activeState(e.State) || e.State == StateNeedsRecovery) {
			return e
		}
	}
	return nil
}

func (l *Ledger) nextReserved(osName string) *Entry {
	var best *Entry
	for _, e := range l.Entries {
		if e.OS != osName || e.State != StateReserved || e.Started.Attempts != 0 {
			continue
		}
		if best == nil || e.Plan.RunID < best.Plan.RunID || (e.Plan.RunID == best.Plan.RunID && e.Plan.JobID < best.Plan.JobID) {
			best = e
		}
	}
	return best
}
