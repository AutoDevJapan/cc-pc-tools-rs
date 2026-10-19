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
}

func newMonitor(p Plan) *monitor {
	return &monitor{p: p, progress: jobProgress, audit: auditRunning}
}

const monitorEvery = 10 * time.Second
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

// serviceStatus reports the SCM state only, read-only for the queue's fence.
func statusReport(p Plan) error {
	out := map[string]any{"instance": p.Instance, "service": p.name()}
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
