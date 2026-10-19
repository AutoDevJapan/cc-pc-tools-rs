package main

import (
	"context"
	"errors"
	"os"
	"sort"
	"time"
)

type TickReport struct {
	Status        string     `json:"status"`
	Scan          ScanReport `json:"scan"`
	Admitted      int        `json:"admitted"`
	BudgetBlocked int        `json:"budget_blocked"`
	Launched      int        `json:"launched"`
	Reconciled    int        `json:"reconciled"`
	Active        int        `json:"active"`
	Fenced        []string   `json:"fenced,omitempty"`
	Overdue       []string   `json:"overdue,omitempty"`
	Residue       int        `json:"residue_count,omitempty"`
	Initial       int        `json:"budget_initial_used"`
	Normal        int        `json:"budget_normal_used"`
	Errors        []string   `json:"errors,omitempty"`
}

var errTickBusy = errors.New("tick busy")

// Tick is one finite pass: reconcile, start what is already reserved, observe
// and admit, start what was just admitted. It never waits for a heavy job.
func (d *Deps) Tick(ctx context.Context) TickReport {
	rep := TickReport{Status: "ok"}
	if e := os.MkdirAll(d.Cfg.Home, 0700); e != nil {
		rep.Status, rep.Errors = "error", append(rep.Errors, "home_unavailable")
		return rep
	}
	unlock, e := tryLock(d.home("tick.lock"))
	if e == errLockBusy {
		rep.Status = "busy"
		return rep
	}
	if e != nil {
		rep.Status, rep.Errors = "error", append(rep.Errors, "tick_lock_unavailable")
		return rep
	}
	defer unlock()

	d.reconcile(ctx, &rep)
	d.launchPending(ctx, &rep)
	if ctx.Err() == nil {
		d.scanAndAdmit(ctx, &rep)
		d.launchPending(ctx, &rep)
	}
	if l, e := d.Store.View(); e == nil {
		rep.Initial, rep.Normal = len(l.Budget.Initial), len(l.Budget.Normal)
		rep.Residue = l.ResidueCount
		for _, en := range l.Entries {
			if activeState(en.State) {
				rep.Active++
			}
		}
		for osName, f := range l.Fences {
			rep.Fenced = append(rep.Fenced, osName+":"+f.Key+":"+f.Reason)
		}
		sort.Strings(rep.Fenced)
	}
	return rep
}

func (d *Deps) launchPending(ctx context.Context, rep *TickReport) {
	for _, osName := range []string{"Linux", "Windows"} {
		if ctx.Err() != nil {
			return
		}
		var key string
		err := d.Store.Update(func(l *Ledger) error {
			if _, fenced := l.Fences[osName]; fenced || d.Helpers[osName] == nil || l.activeFor(osName) != nil {
				return errNoWrite
			}
			e := l.nextReserved(osName)
			if e == nil {
				return errNoWrite
			}
			// The single launch attempt is recorded BEFORE the process exists.
			e.State, e.Started.Attempts, e.Started.IntentAt = StateLaunching, 1, stamp(d.Now())
			key = e.Key
			return nil
		})
		if err != nil && err != errNoWrite {
			rep.Errors = append(rep.Errors, "launch_ledger_deferred")
			continue
		}
		if key == "" {
			continue
		}
		id, breakaway, serr := d.Spawn(key)
		_ = d.mutate(key, func(l *Ledger, e *Entry) {
			if serr != nil {
				if e.State == StateLaunching && e.Started.Supervisor.zero() {
					e.State, e.Reason = StateFailed, "supervisor spawn failed"
				}
				return
			}
			if e.Started.Supervisor.zero() {
				e.Started.Supervisor = id
			}
			e.Started.Breakaway = breakaway
		})
		if serr == nil {
			rep.Launched++
		} else {
			rep.Errors = append(rep.Errors, "supervisor_spawn_failed")
		}
	}
}

func (d *Deps) scanAndAdmit(ctx context.Context, rep *TickReport) {
	if _, unwired := d.C.(unwiredContract); unwired {
		rep.Errors = append(rep.Errors, "contract_unwired")
		return
	}
	snap, err := d.Store.View()
	if err != nil {
		rep.Errors = append(rep.Errors, "ledger_unreadable")
		return
	}
	rd := d.Reader()
	read := func(c context.Context, ep string, out any) error {
		c2, cancel := context.WithTimeout(c, 12*time.Second)
		defer cancel()
		return rd.Read(c2, ep, out)
	}
	sc := &Scanner{Read: read, C: d.C, Now: d.Now, MaxRuns: 3, MaxVerify: 6}
	cands, srep, rejects := sc.Observe(ctx, snap)
	rep.Scan = srep
	rep.Errors = append(rep.Errors, srep.Errors...)
	now := d.Now()
	err = d.Store.Update(func(l *Ledger) error {
		for _, c := range cands {
			if d.Helpers[c.OS] == nil {
				rep.Errors = append(rep.Errors, "helper_unconfigured_"+c.OS)
				continue
			}
			if _, why := l.reserve(c.Plan, c.OS, d.Cfg.Continuation.Enabled, now); why == "" {
				rep.Admitted++
			} else if why == "budget_exhausted" {
				rep.BudgetBlocked++
				r := l.Refusals[c.Plan.Key()]
				r.Count++
				r.At, r.Reason, r.Next = stamp(now), why, stamp(now.Add(5*time.Minute))
				l.Refusals[c.Plan.Key()] = r
			}
		}
		for key, reason := range rejects {
			if _, known := l.Entries[key]; known {
				continue
			}
			r := l.Refusals[key]
			r.Count++
			r.At, r.Reason = stamp(now), reason
			wait := 10 * time.Minute
			if r.Count >= 5 {
				wait = 6 * time.Hour
			}
			r.Next = stamp(now.Add(wait))
			l.Refusals[key] = r
		}
		return nil
	})
	if err != nil {
		rep.Errors = append(rep.Errors, "admission_not_stored")
	}
}

// reconcile is read-only with respect to the outside world. It compares the
// recorded supervisor identity (PID plus creation time) with the live process
// table, imports a result file the supervisor wrote before dying, and
// otherwise fences the OS. A fence is released only by a result or by a helper
// status that proves everything is gone.
func (d *Deps) reconcile(ctx context.Context, rep *TickReport) {
	l, err := d.Store.View()
	if err != nil {
		rep.Errors = append(rep.Errors, "ledger_unreadable")
		return
	}
	keys := make([]string, 0)
	for k, e := range l.Entries {
		if activeState(e.State) || e.State == StateNeedsRecovery {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	now := d.Now()
	for _, k := range keys {
		e := l.Entries[k]
		if ctx.Err() != nil {
			return
		}
		if e.State != StateNeedsRecovery && d.Alive(e.Started.Supervisor) {
			if h := d.Helpers[e.OS]; h != nil {
				if at, pe := time.Parse(time.RFC3339Nano, e.Started.IntentAt); pe == nil && now.After(at.Add(hardLimitFor(h))) {
					rep.Overdue = append(rep.Overdue, k)
				}
			}
			continue
		}
		if r, ok := d.readResult(k); ok {
			if terminalState(e.State) {
				continue
			}
			if d.applyResult(k, r) == nil {
				rep.Reconciled++
			}
			continue
		}
		if e.State != StateNeedsRecovery {
			// The supervisor stores its fence and "create_intent" BEFORE it creates
			// anything, so a launching entry that still shows no cleanup record
			// proves that only reads happened: close it without fencing the OS.
			if e.State == StateLaunching && e.Cleanup.State == "none" && e.Started.HelperAt == "" {
				if d.mutate(k, func(l *Ledger, en *Entry) {
					if en.State == StateLaunching && en.Cleanup.State == "none" {
						en.State, en.Reason = StateFailed, "supervisor ended before creating any resource"
					}
				}) == nil {
					rep.Reconciled++
				}
				continue
			}
			reason := "supervisor gone without a final result while " + e.State
			if d.mutate(k, func(l *Ledger, en *Entry) {
				en.State, en.Reason = StateNeedsRecovery, reason
				l.Fences[en.OS] = Fence{Key: k, Reason: reason, At: stamp(now)}
			}) == nil {
				rep.Reconciled++
			}
			continue
		}
		// Already fenced and no result file: nothing here can prove the OS is
		// clean, so the fence stays until the parent's helper recovery mode (or
		// an operator) resolves it. The tick makes no outside call for this.
	}
}
