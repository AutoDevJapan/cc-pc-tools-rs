package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// hardLimitFor is the longest a supervisor may legitimately live.
func hardLimitFor(h Helper) time.Duration {
	cl := h.CleanupSeconds()
	if cl == 0 {
		cl = 1800
	}
	return time.Duration(h.Seconds()+600+cl+600) * time.Second
}

type stepOutcome struct {
	ran  bool
	exit int
	err  error
}

func (o stepOutcome) ok() bool { return o.ran && o.err == nil && o.exit == 0 }

// step runs one fixed helper step and keeps its original output bytes in the
// supervisor's entry directory for the evidence manifest.
func (d *Deps) step(ctx context.Context, h Helper, key, verb, root string, limit time.Duration, stdin []byte, onStart func(ProcID)) stepOutcome {
	sctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	res, err := d.Step(sctx, h, verb, root, stdin, onStart)
	dir := d.home("entries", safeKey(key))
	if os.MkdirAll(dir, 0700) == nil {
		_ = os.WriteFile(filepath.Join(dir, "step-"+verb+".log"), res.Output, 0600)
	}
	return stepOutcome{ran: true, exit: res.Exit, err: err}
}

func intp(n int) *int { return &n }

// finalize writes the result file first, then the ledger; a crash between the
// two is reconciled from the file by the next tick.
func (d *Deps) finalize(key string, r Result) error {
	r.Key, r.At = key, stamp(d.Now())
	if e := writeAtomic(d.resultPath(key), r); e != nil {
		return e
	}
	return d.applyResult(key, r)
}

func (d *Deps) applyResult(key string, r Result) error {
	return d.mutate(key, func(l *Ledger, e *Entry) {
		if terminalState(e.State) {
			return
		}
		e.State, e.Reason, e.Official, e.Cleanup = r.State, r.Reason, r.Official, r.Cleanup
		if r.ExitCode != nil {
			e.Started.ExitCode = r.ExitCode
		}
		if e.Started.ExitedAt == "" && r.ExitCode != nil {
			e.Started.ExitedAt = r.At
		}
		if f, ok := l.Fences[e.OS]; ok && f.Key == key {
			if r.State == StateNeedsRecovery {
				l.Fences[e.OS] = Fence{Key: key, Reason: r.Reason, At: r.At}
			} else {
				delete(l.Fences, e.OS)
			}
		} else if r.State == StateNeedsRecovery {
			l.Fences[e.OS] = Fence{Key: key, Reason: r.Reason, At: r.At}
		}
	})
}

func (d *Deps) fence(key, reason string) error {
	return d.mutate(key, func(l *Ledger, e *Entry) {
		l.Fences[e.OS] = Fence{Key: key, Reason: reason, At: stamp(d.Now())}
	})
}

func (d *Deps) unfence(key string) error {
	return d.mutate(key, func(l *Ledger, e *Entry) {
		if f, ok := l.Fences[e.OS]; ok && f.Key == key {
			delete(l.Fences, e.OS)
		}
	})
}

// Supervise is the single worker of one OS. It owns the OS lock for its whole
// life, runs the fixed helper steps once, and never starts a second attempt.
func (d *Deps) Supervise(ctx context.Context, key string) error {
	l, err := d.Store.View()
	if err != nil {
		return err
	}
	ent := l.Entries[key]
	if ent == nil || ent.State != StateLaunching || ent.Started.Attempts != 1 {
		return errors.New("entry is not a launching first attempt")
	}
	plan, osName := ent.Plan, ent.OS
	h := d.Helpers[osName]
	if h == nil {
		return d.finalize(key, Result{State: StateFailed, Reason: "no helper configured for this OS", Cleanup: CleanupRec{State: "none"}})
	}
	unlock, err := d.LockOS(osName)
	if err == errLockBusy {
		// Nothing was created or started: hand the entry back untouched.
		return d.mutate(key, func(l *Ledger, e *Entry) {
			if e.State == StateLaunching && e.Started.HelperAt == "" {
				e.State, e.Started = StateReserved, StartRec{}
				e.Reason = "OS worker lock busy"
			}
		})
	}
	if err != nil {
		return d.finalize(key, Result{State: StateFailed, Reason: "OS lock unavailable", Cleanup: CleanupRec{State: "none"}})
	}
	defer unlock()
	me := d.Self()
	_ = d.mutate(key, func(l *Ledger, e *Entry) { e.Started.Supervisor = me })
	ctx, cancel := context.WithTimeout(ctx, hardLimitFor(h))
	defer cancel()

	// 1. The same fixed plan is verified again right before anything is created.
	vctx, vcancel := context.WithTimeout(ctx, 90*time.Second)
	rd := d.Reader()
	rd.MaxCalls = 120
	verr := d.C.Verify(vctx, plan, rd.API())
	vcancel()
	if verr != nil {
		return d.finalize(key, Result{State: StateRefused, Reason: "verify: " + short(verr), Cleanup: CleanupRec{State: "none"}})
	}

	// 2. Create a NEW owned root. The fence is saved first because a crash
	//    after the record is made but before it is stored is unrecoverable
	//    without human-readable evidence.
	if e := d.mutate(key, func(l *Ledger, e *Entry) {
		e.Cleanup.State = "create_intent"
		l.Fences[osName] = Fence{Key: key, Reason: "owned root creation unconfirmed", At: stamp(d.Now())}
	}); e != nil {
		return e
	}
	rec, cerr := d.SD.Create(ctx, "civ6-ci-queue", "job-"+safeKey(key))
	if cerr != nil {
		return d.finalize(key, Result{State: StateNeedsRecovery, Reason: "owned root creation unconfirmed", Cleanup: CleanupRec{State: "create_intent"}})
	}
	cl := CleanupRec{State: "owned", Instance: rec.ID, SafeDeleteID: rec.ID, Root: rec.Path}
	if e := d.mutate(key, func(l *Ledger, e *Entry) {
		e.Cleanup = cl
		if f, ok := l.Fences[osName]; ok && f.Key == key {
			delete(l.Fences, osName)
		}
	}); e != nil {
		return d.finalize(key, Result{State: StateNeedsRecovery, Reason: "owned root record not stored", Cleanup: cl})
	}

	started := false
	exit := -1
	var failure string
	prepared := false
	if e := h.WritePlan(rec.Path, rec.ID, plan); e != nil {
		failure = "plan could not be written"
	} else if o := d.step(ctx, h, key, StepPrepare, rec.Path, 10*time.Minute, nil, nil); !o.ok() {
		failure = "prepare failed"
	} else {
		prepared = true
	}

	if prepared {
		// From here a service, registration or namespace may exist. Fence until
		// the collect step proves otherwise.
		_ = d.mutate(key, func(l *Ledger, e *Entry) {
			e.State = StateRunning
			l.Fences[osName] = Fence{Key: key, Reason: "registration or namespace may exist until collect succeeds", At: stamp(d.Now())}
		})
		tok, terr := d.Token.Registration(ctx)
		if terr != nil {
			failure = "registration credential unavailable"
		} else {
			started = true
			o := d.step(ctx, h, key, StepExecute, rec.Path, time.Duration(h.Seconds()+120)*time.Second, tok, func(id ProcID) {
				_ = d.mutate(key, func(l *Ledger, e *Entry) { e.Started.Helper, e.Started.HelperAt = id, stamp(d.Now()) })
			})
			zero(tok)
			exit = o.exit
			if o.err != nil {
				exit = -1
				failure = "execute did not finish: " + short(o.err)
				_ = d.step(ctx, h, key, StepStop, rec.Path, 5*time.Minute, nil, nil)
			} else if o.exit != 0 {
				failure = fmt.Sprintf("execute exited %d", o.exit)
			}
		}
	}
	if started {
		_ = d.mutate(key, func(l *Ledger, e *Entry) {
			e.Started.ExitCode, e.Started.ExitedAt = intp(exit), stamp(d.Now())
		})
	}

	// 3. Collection: the helper verifies PID 0 / registration / service / ACL /
	//    profile absence. Without a successful collect the OS stays fenced.
	_ = d.mutate(key, func(l *Ledger, e *Entry) { e.State = StateCollecting })
	verb := StepCollectUnstarted
	if started {
		verb = StepCollect
	}
	col := d.step(ctx, h, key, verb, rec.Path, time.Duration(cleanupSeconds(h))*time.Second, nil, nil)

	// 4. The official result is a separate check; the process exit alone never
	//    counts as success.
	off := OfficialRec{CheckedAt: stamp(d.Now()), Reason: "not started"}
	if started {
		orx, oc := context.WithTimeout(ctx, 90*time.Second)
		r2 := d.Reader()
		r2.MaxCalls = 120
		if e := d.C.RequireSuccess(orx, plan, h.RunnerName(rec.ID), r2.API()); e != nil {
			off = OfficialRec{CheckedAt: stamp(d.Now()), Reason: short(e)}
		} else {
			off = OfficialRec{CheckedAt: stamp(d.Now()), Success: true}
		}
		oc()
	}

	var ec *int
	if started {
		ec = intp(exit)
	}
	if !col.ok() {
		cl.State, cl.Detail = "failed", "collect did not confirm cleanup"
		return d.finalize(key, Result{State: StateNeedsRecovery, Reason: "collect not confirmed", ExitCode: ec, Official: off, Cleanup: cl})
	}
	cl.State = "collected"

	// 5. Keep the original logs and hashes, then retire the owned root.
	manifest, e := preserveEvidence(rec.Path, d.home("entries", safeKey(key)), d.home("evidence", safeKey(key)))
	if e != nil {
		cl.State, cl.Detail = "failed", "evidence not preserved: "+short(e)
		return d.finalize(key, Result{State: StateNeedsRecovery, Reason: "evidence not preserved", ExitCode: ec, Official: off, Cleanup: cl})
	}
	cl.State, cl.Evidence, cl.ManifestSHA = "evidence", d.home("evidence", safeKey(key)), manifest
	cl = d.retire(ctx, cl)
	if cl.State != "removed" && cl.State != "quarantined" {
		return d.finalize(key, Result{State: StateNeedsRecovery, Reason: "owned root not retired", ExitCode: ec, Official: off, Cleanup: cl})
	}

	state, reason := StateSucceeded, ""
	if !started || exit != 0 || !off.Success {
		state = StateFailed
		reason = failure
		if reason == "" {
			reason = "official success not confirmed: " + off.Reason
		}
	}
	return d.finalize(key, Result{State: state, Reason: reason, ExitCode: ec, Official: off, Cleanup: cl})
}

func cleanupSeconds(h Helper) int {
	if n := h.CleanupSeconds(); n != 0 {
		return n
	}
	return 1800
}

// retire follows the safe-delete lifecycle: plan, delete exactly the planned
// root, finish. A refusal is never retried; the root is quarantined instead.
func (d *Deps) retire(ctx context.Context, cl CleanupRec) CleanupRec {
	planned, e := d.SD.Plan(ctx, cl.SafeDeleteID)
	if e == nil {
		e = removeOwned(cl.Root, planned)
	}
	if e == nil {
		if fe := d.SD.Finish(ctx, cl.SafeDeleteID); fe != nil {
			cl.State, cl.Detail = "failed", "removed but finish not recorded"
			return cl
		}
		cl.State = "removed"
		return cl
	}
	if qe := d.SD.Quarantine(ctx, cl.SafeDeleteID, "windows"); qe != nil {
		cl.State, cl.Detail = "failed", "removal refused and quarantine failed"
		return cl
	}
	cl.State, cl.Detail = "quarantined", "removal refused: "+short(e)
	return cl
}
