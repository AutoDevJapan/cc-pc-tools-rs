package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// hardLimitFor is the longest a supervisor may legitimately live.
func hardLimitFor(h Helper) time.Duration {
	cl := h.CleanupSeconds()
	if cl == 0 {
		cl = 1800
	}
	return time.Duration(h.Seconds()+1200+2*cl+600) * time.Second
}

func intp(n int) *int { return &n }

var errAdmissionDeferred = errors.New("registration queue temporarily occupied")

// No formal job ran: either no resource existed yet, or the strict unstarted
// collector proved absence and the root was retired. Keep its one reservation.
// This is not a retry/rerun of an assigned GitHub job.
func (d *Deps) deferUnstarted(key, reason string, cleanup *CleanupRec) error {
	return d.mutate(key, func(l *Ledger, e *Entry) {
		e.Queued.Deferrals++
		shift := e.Queued.Deferrals - 1
		if shift > 3 {
			shift = 3
		}
		e.Queued.RetryAt = stamp(d.Now().Add(5 * time.Minute * time.Duration(1<<shift)))
		e.State, e.Started, e.Reason = StateReserved, StartRec{}, reason
		if cleanup != nil {
			e.Cleanup = *cleanup
		}
		if f, ok := l.Fences[e.OS]; ok && f.Key == key {
			delete(l.Fences, e.OS)
		}
	})
}

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
		if r.State == StateNeedsRecovery {
			l.Fences[e.OS] = Fence{Key: key, Reason: r.Reason, At: r.At}
		} else if f, ok := l.Fences[e.OS]; ok && f.Key == key {
			delete(l.Fences, e.OS)
		}
	})
}

// Supervise is the single worker of one OS. It owns the OS lock for its whole
// life, runs the fixed helper lifecycle once, and never starts a second attempt.
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
		// Nothing was created or started by THIS process. Only an entry that no
		// supervisor has claimed goes back to "reserved"; a claimed entry belongs
		// to the worker that holds the lock and is left exactly as it is.
		return d.mutate(key, func(l *Ledger, e *Entry) {
			if e.State == StateLaunching && !e.Started.Claimed {
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
	// Claim the single attempt under the OS lock, against the CURRENT ledger: a
	// stale read taken before another worker finished can never run it again.
	claimed := false
	if e := d.mutate(key, func(l *Ledger, e *Entry) {
		if e.State == StateLaunching && e.Started.Attempts == 1 && !e.Started.Claimed {
			e.Started.Claimed, e.Started.Supervisor, claimed = true, me, true
		}
	}); e != nil {
		return e
	}
	if !claimed {
		return errors.New("entry is already claimed or finished")
	}
	ctx, cancel := context.WithTimeout(ctx, hardLimitFor(h))
	defer cancel()

	// 1. The same fixed plan is verified again right before anything is created.
	vctx, vcancel := context.WithTimeout(ctx, 90*time.Second)
	rd := d.Reader()
	rd.MaxCalls = 120
	verr := d.C.Verify(vctx, plan, rd.API())
	vcancel()
	if verr != nil {
		if transient(verr) {
			return d.deferUnstarted(key, "prestart verify deferred: "+short(verr), nil)
		}
		return d.finalize(key, Result{State: StateRefused, Reason: "verify: " + short(verr), Cleanup: CleanupRec{State: "none"}})
	}

	// 2. Create a NEW owned root. The fence is saved first because a crash
	//    after the record is made but before it is stored cannot be told apart
	//    from a root that was never made.
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

	// 3. The OS lifecycle. It fences before any service, registration or
	//    namespace can exist and reports whether collection was confirmed.
	entryDir := d.home("entries", safeKey(key))
	evidenceDir := d.home("evidence", safeKey(key))
	if ent.Queued.Deferrals > 0 {
		part := fmt.Sprintf("prestart-%d", ent.Queued.Deferrals)
		entryDir = filepath.Join(entryDir, part)
		evidenceDir = filepath.Join(evidenceDir, part)
	}
	if e := os.MkdirAll(entryDir, 0700); e != nil {
		return d.finalize(key, Result{State: StateNeedsRecovery, Reason: "entry directory unavailable", Cleanup: cl})
	}
	lc := &LifeCtx{Key: key, Plan: plan, Instance: rec.ID, Root: rec.Path, EntryDir: entryDir, Exec: d.Exec, Admin: d.Admin,
		Fence:   func(reason string) { _ = d.fence(key, reason) },
		Running: func() { _ = d.mutate(key, func(l *Ledger, e *Entry) { e.State = StateRunning }) },
		OnHelper: func(id ProcID) {
			_ = d.mutate(key, func(l *Ledger, e *Entry) { e.Started.Helper, e.Started.HelperAt = id, stamp(d.Now()) })
		},
		Log: func(name string, b []byte) { _ = os.WriteFile(filepath.Join(entryDir, name), b, 0600) },
	}
	out := h.Lifecycle(ctx, lc)
	_ = d.mutate(key, func(l *Ledger, e *Entry) {
		e.State = StateCollecting
		if out.Started {
			e.Started.ExitCode, e.Started.ExitedAt = intp(out.Exit), stamp(d.Now())
		}
	})
	var ec *int
	if out.Started {
		ec = intp(out.Exit)
	}
	if !out.Collected {
		cl.State, cl.Detail = "failed", "collection not confirmed"
		return d.finalize(key, Result{State: StateNeedsRecovery, Reason: "collect not confirmed", ExitCode: ec, Official: OfficialRec{CheckedAt: stamp(d.Now()), Reason: "not checked"}, Cleanup: cl})
	}

	// 4. The official result is a separate check; the process exit alone never
	//    counts as success.
	off := OfficialRec{CheckedAt: stamp(d.Now()), Reason: "not started"}
	if out.Started && out.Runner != "" {
		octx, oc := context.WithTimeout(ctx, 90*time.Second)
		r2 := d.Reader()
		r2.MaxCalls = 120
		if e := d.C.RequireSuccess(octx, plan, out.Runner, r2.API()); e != nil {
			off = OfficialRec{CheckedAt: stamp(d.Now()), Reason: short(e)}
		} else {
			off = OfficialRec{CheckedAt: stamp(d.Now()), Success: true}
		}
		oc()
	}
	cl.State = "collected"
	residueBlocked := false
	if out.Residue {
		cl.Detail = "residue_only: profile, hive or classes remain"
		residueBlocked = !d.allowResidue(osName)
		if !residueBlocked {
			_ = d.mutate(key, func(l *Ledger, e *Entry) { l.ResidueCount++ })
		}
	}

	// 5. Keep the original logs and hashes, then retire the owned root.
	manifest, incomplete, e := preserveEvidence(rec.Path, entryDir, evidenceDir)
	if e != nil {
		cl.State, cl.Detail = "failed", "evidence not preserved: "+short(e)
		return d.finalize(key, Result{State: StateNeedsRecovery, Reason: "evidence not preserved", ExitCode: ec, Official: off, Cleanup: cl})
	}
	detail := cl.Detail
	cl.State, cl.Evidence, cl.ManifestSHA = "evidence", evidenceDir, manifest
	if incomplete > 0 {
		// Some original bytes could not be copied (for example files the
		// non-admin controller may not read): keep the whole root by moving it
		// to the quarantine instead of deleting it.
		if qe := d.SD.Quarantine(ctx, cl.SafeDeleteID, "windows"); qe != nil {
			cl.State, cl.Detail = "failed", "evidence incomplete and quarantine failed"
		} else {
			cl.State, cl.Detail = "quarantined", fmt.Sprintf("evidence incomplete: %d entries kept in the quarantined root", incomplete)
		}
	} else {
		cl = d.retire(ctx, cl)
	}
	if cl.State != "removed" && cl.State != "quarantined" {
		return d.finalize(key, Result{State: StateNeedsRecovery, Reason: "owned root not retired", ExitCode: ec, Official: off, Cleanup: cl})
	}
	if detail != "" {
		cl.Detail = detail + "; " + cl.Detail
	}
	if residueBlocked {
		return d.finalize(key, Result{State: StateNeedsRecovery, Reason: "collection left residue that is not allowed", ExitCode: ec, Official: off, Cleanup: cl})
	}
	if out.Deferred && !out.Started {
		return d.deferUnstarted(key, "confirmed unstarted resource retired; supply deferred", &cl)
	}

	state, reason := StateSucceeded, ""
	if !out.Started || !out.HelperOK || out.Exit != 0 || !off.Success {
		state = StateFailed
		reason = out.Failure
		if reason == "" {
			reason = "official success not confirmed: " + off.Reason
		}
	}
	return d.finalize(key, Result{State: state, Reason: reason, ExitCode: ec, Official: off, Cleanup: cl})
}

func (d *Deps) allowResidue(osName string) bool {
	return osName == "Windows" && d.Cfg.Helpers["Windows"].AllowResidueOnly
}

func (d *Deps) fence(key, reason string) error {
	return d.mutate(key, func(l *Ledger, e *Entry) {
		l.Fences[e.OS] = Fence{Key: key, Reason: reason, At: stamp(d.Now())}
	})
}

// retire follows the safe-delete lifecycle: plan, delete exactly the planned
// root, finish. A refusal is never retried; the root is quarantined instead.
func (d *Deps) retire(ctx context.Context, cl CleanupRec) CleanupRec {
	plan, e := d.SD.Plan(ctx, cl.SafeDeleteID)
	if e == nil && plan.Action != "submit_to_exec_tool" {
		// The tool itself asked for quarantine (attributes, size, ...): follow it
		// exactly and never delete.
		reason := plan.Reason
		if plan.Action != "quarantine" || !quarantineReasons[reason] {
			cl.State, cl.Detail = "failed", "deletion plan action not understood"
			return cl
		}
		if qe := d.SD.Quarantine(ctx, cl.SafeDeleteID, reason); qe != nil {
			cl.State, cl.Detail = "failed", "quarantine requested by the plan failed"
			return cl
		}
		cl.State, cl.Detail = "quarantined", "plan requested quarantine: "+reason
		return cl
	}
	if e == nil {
		e = removeOwned(cl.Root, plan.Path)
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
