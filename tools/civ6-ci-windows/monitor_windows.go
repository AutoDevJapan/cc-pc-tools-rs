//go:build windows && amd64

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// emitEvent writes one credential-free JSON line for the queue's durable ledger.
// A begin without a matching clean end (or fence=true) marks uncertain work.
func emitEvent(name string, p Plan, extra map[string]any) {
	row := map[string]any{"event": name, "instance": p.Instance, "run_id": p.RunID, "attempt": p.Attempt, "job_id": p.JobID, "time": time.Now().UTC().Format(time.RFC3339)}
	for k, v := range extra {
		row[k] = v
	}
	if b, e := json.Marshal(row); e == nil {
		fmt.Fprintln(os.Stdout, string(b))
	}
}

// monitor re-verifies the fixed plan while the official job runs: once when the
// job starts on this unique runner and once when it completes. A policy mismatch
// is fatal at once; a failed GitHub read is tolerated only briefly.
type monitor struct {
	p                  Plan
	next               time.Time
	started, completed bool
	soft               int
	progress           func(context.Context, Plan) (string, string, error)
	audit              func(Plan) error
	auditQueued        func(Plan) error
	lastQueued         time.Time
}

func newMonitor(p Plan) *monitor {
	return &monitor{p: p, progress: jobProgress, audit: auditRunning, auditQueued: auditQueued, lastQueued: time.Now()}
}

const monitorEvery = 10 * time.Second
const monitorQueuedAuditEvery = time.Minute

// monitorSoftLimit consecutive failed GitHub reads end the run; the first
// monitorSoftLimit-1 are tolerated.
const monitorSoftLimit = 6

func (m *monitor) tick(now time.Time) error {
	if m.completed || now.Before(m.next) {
		return nil
	}
	m.next = now.Add(monitorEvery)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	status, runner, e := m.progress(ctx, m.p)
	if e == nil && runner == m.p.name() && (status == "in_progress" || status == "completed") {
		if !m.started {
			if e = m.audit(m.p); e == nil {
				m.started = true
				emitEvent("job-start-verified", m.p, nil)
			}
		}
		if e == nil && status == "completed" {
			if e = m.audit(m.p); e == nil {
				m.completed = true
				emitEvent("job-complete-verified", m.p, nil)
			}
		}
	}
	if e == nil && runner == "" && status == "completed" {
		// Cancelled or finished before any runner took it: nothing will ever
		// arrive, so do not hold the Cargo slot and Windows worker until the deadline.
		e = errors.New("target job ended before it was assigned to this runner")
	}
	if e == nil && runner == "" && m.auditQueued != nil && now.Sub(m.lastQueued) >= monitorQueuedAuditEvery {
		m.lastQueued = now
		e = m.auditQueued(m.p)
	}
	if e == nil {
		m.soft = 0
		return nil
	}
	var read *apiReadError
	if errors.As(e, &read) {
		if m.soft++; m.soft < monitorSoftLimit {
			return nil
		}
		return errors.Join(errors.New("GitHub reads kept failing during the run"), e)
	}
	return e
}

// statusReport reports the SCM state only, read-only for the queue's fence.
func statusReport(p Plan) error {
	out := map[string]any{"instance": p.Instance, "service": p.svc()}
	s, e := openService(p, 4)
	switch {
	case errors.Is(e, syscall.Errno(1060)):
		out["service_present"] = false
	case e != nil:
		return e
	default:
		defer closeSC(s)
		state, pid, code, err := queryService(s)
		if err != nil {
			return err
		}
		out["service_present"], out["state"], out["pid"], out["exit_code"] = true, state, pid, code
	}
	for _, f := range []string{"prepared.json"} {
		_, err := os.Lstat(p.Root + string(os.PathSeparator) + f)
		out[f] = err == nil
	}
	b, _ := json.Marshal(out)
	fmt.Fprintln(os.Stdout, string(b))
	return nil
}

// holdDeadline is the latest moment the service can still legitimately run:
// service start, up to 60 s of startup (registration pipe 30 s + token/Job
// setup), the plan's run seconds counted from the worker resume, and the
// launcher's 30 s Job drain plus slack. It is anchored at the service start, so
// time spent before it (API audits) never shortens the hold.
func holdDeadline(serviceStart time.Time, seconds int) time.Time {
	return serviceStart.Add(60*time.Second + time.Duration(seconds)*time.Second + 90*time.Second)
}

// holdOps abstracts the three read-only observations so the stop boundary is
// testable with an injected clock.
type holdOps struct {
	query  func() (state uint32, err error)
	drain  func() (drained bool, err error)
	census func() (count int, err error)
	now    func() time.Time
	sleep  func(time.Duration)
}

type holdResult struct {
	Stopped, Drained, CensusZero bool
	Err                          error
}

// holdUntilDrained keeps the caller (and so the shared Cargo gate slot) alive
// until the service is STOPPED and the protected launcher report proves the Job
// Object drained, or until the absolute deadline. It never issues a stop. An SCM
// read failure is not evidence of a stop: it is remembered and polling continues.
// A STOPPED service without drain proof is held until no process of its account
// remains (weaker evidence, reported as such) and is never reported as drained.
func holdUntilDrained(ops holdOps, until time.Time, tick time.Duration) holdResult {
	var res holdResult
	var lastErr error
	for {
		state, e := ops.query()
		if e != nil {
			lastErr = e
		} else if state == 1 {
			res.Stopped = true
			break
		}
		if !ops.now().Before(until) {
			res.Err = errors.Join(errors.New("service stop not confirmed by the hold deadline; retain resource and fence"), lastErr)
			return res
		}
		ops.sleep(tick)
	}
	if drained, e := ops.drain(); e == nil && drained {
		res.Drained = true
		return res
	}
	for {
		if n, e := ops.census(); e == nil && n == 0 {
			res.CensusZero = true
			res.Err = errors.New("launcher drain proof is missing or false; no process of the account remains, fence kept")
			return res
		} else if e != nil {
			lastErr = e
		}
		if !ops.now().Before(until) {
			res.Err = errors.Join(errors.New("service stopped but neither drain proof nor an empty account was confirmed by the hold deadline; retain resource and fence"), lastErr)
			return res
		}
		ops.sleep(tick)
	}
}

func realHoldOps(p Plan) holdOps {
	return holdOps{
		query: func() (uint32, error) {
			s, e := openService(p, 4)
			if e != nil {
				return 0, e
			}
			defer closeSC(s)
			state, _, _, e := queryService(s)
			return state, e
		},
		drain: func() (bool, error) {
			r, e := readLaunchResult(p)
			if e != nil {
				return false, e
			}
			sid, e := serviceSID(p)
			if e != nil {
				return false, e
			}
			return r.Drained && r.AccountSID == sid, nil
		},
		census: func() (int, error) {
			sid, e := serviceSID(p)
			if e != nil {
				return 0, e
			}
			return ownedProcessCount(sid)
		},
		now:   time.Now,
		sleep: time.Sleep,
	}
}
