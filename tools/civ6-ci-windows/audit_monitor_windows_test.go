//go:build windows && amd64

package main

import (
	contract "cc/civ6-ci-contract"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"
)

type fakeRepo struct {
	runners []map[string]any
	total   *int
	job     apiJob
	jobs    int
}

func (f *fakeRepo) read(ep string, out any) error {
	var v any
	switch ep {
	case "repos/" + repository + "/actions/runners?per_page=100":
		total := len(f.runners)
		if f.total != nil {
			total = *f.total
		}
		v = map[string]any{"total_count": total, "runners": f.runners}
	default:
		if ep == fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100", repository, f.job.RunID, f.job.Attempt) {
			f.jobs++
			v = map[string]any{"total_count": 1, "jobs": []apiJob{f.job}}
			break
		}
		return fmt.Errorf("unexpected endpoint %s", ep)
	}
	b, _ := json.Marshal(v)
	return json.Unmarshal(b, out)
}

func stubVerify(err error) func(context.Context, Plan, readAPI) error {
	return func(context.Context, Plan, readAPI) error { return err }
}

func ownRunnerRow(p Plan, labels ...string) map[string]any {
	var rows []map[string]string
	for _, l := range labels {
		rows = append(rows, map[string]string{"name": l})
	}
	return map[string]any{"id": 5, "name": p.name(), "labels": rows}
}

func TestAuditPhasesDecideRegistrationAndJobState(t *testing.T) {
	p := fixturePlan()
	labels := contract.Labels(p.Plan)
	queued := apiJob{ID: p.JobID, RunID: p.RunID, Attempt: p.Attempt, Name: p.JobName, Labels: labels, Status: "queued"}
	running := queued
	running.Status, running.Runner = "in_progress", p.name()
	done := running
	done.Status, done.Conclusion = "completed", "success"
	other := map[string]any{"id": 1, "name": "long-lived-windows", "labels": []any{}}
	ctx := context.Background()
	cases := []struct {
		name    string
		phase   auditPhase
		repo    fakeRepo
		verify  error
		pending bool
		ok      bool
	}{
		{"before: queued job, only foreign runners", phaseBefore, fakeRepo{runners: []map[string]any{other}, job: queued}, nil, false, true},
		{"before: own registration already exists", phaseBefore, fakeRepo{runners: []map[string]any{ownRunnerRow(p, labels...)}, job: queued}, nil, false, false},
		{"before: job already picked up", phaseBefore, fakeRepo{job: running}, nil, false, false},
		{"before: contract mismatch", phaseBefore, fakeRepo{job: queued}, errors.New("source differs"), false, false},
		{"running: own runner with exact labels", phaseRunning, fakeRepo{runners: []map[string]any{ownRunnerRow(p, labels...)}, job: running}, nil, false, true},
		{"running: label drift", phaseRunning, fakeRepo{runners: []map[string]any{ownRunnerRow(p, "self-hosted", "Windows", "X64", "civ6-ci-linux")}, job: running}, nil, false, false},
		{"running: extra label", phaseRunning, fakeRepo{runners: []map[string]any{ownRunnerRow(p, append(append([]string{}, labels...), "gpu")...)}, job: running}, nil, false, false},
		{"running: job on a foreign runner", phaseRunning, fakeRepo{job: apiJob{ID: p.JobID, RunID: p.RunID, Attempt: p.Attempt, Name: p.JobName, Labels: labels, Status: "in_progress", Runner: "other"}}, nil, false, false},
		{"finished: success and registration gone", phaseFinished, fakeRepo{runners: []map[string]any{other}, job: done}, nil, false, true},
		{"finished: ephemeral registration remains", phaseFinished, fakeRepo{runners: []map[string]any{ownRunnerRow(p, labels...)}, job: done}, nil, true, false},
		{"finished: job failed", phaseFinished, fakeRepo{job: func() apiJob { j := done; j.Conclusion = "failure"; return j }()}, nil, false, false},
		{"finished: still running", phaseFinished, fakeRepo{job: running}, nil, true, false},
		{"incomplete runner inventory", phaseBefore, fakeRepo{job: queued}, nil, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := c.repo
			if c.name == "incomplete runner inventory" {
				n := 3
				repo.total = &n
			}
			e := auditWith(ctx, p, c.phase, repo.read, stubVerify(c.verify))
			if (e == nil) != c.ok {
				t.Fatalf("ok=%v err=%v", c.ok, e)
			}
			if c.pending != errors.Is(e, errPending) {
				t.Fatalf("pending=%v err=%v", c.pending, e)
			}
		})
	}
	f := p
	f.Fixture = "normal"
	if auditWith(ctx, f, phaseBefore, (&fakeRepo{}).read, stubVerify(nil)) == nil {
		t.Fatal("fixture treated as identity evidence")
	}
}

func TestVerifyAdapterStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	e := verifyWith(ctx, fixturePlan(), func(string, any) error { calls++; return nil })
	if e == nil || calls != 0 {
		t.Fatalf("cancelled verification reached the API: %v calls=%d", e, calls)
	}
	var first string
	_ = verifyWith(context.Background(), fixturePlan(), func(ep string, _ any) error {
		if first == "" {
			first = ep
		}
		return errors.New("stop")
	})
	if first != fmt.Sprintf("repositories/%d", repositoryID) {
		t.Fatalf("contract did not start from the repository id: %s", first)
	}
}

func TestMonitorVerifiesStartAndCompletionOnceAndTolerantlyHandlesReadFailures(t *testing.T) {
	p := fixturePlan()
	now := time.Unix(1000, 0)
	var states []string
	var audits int
	next := func() (string, string, error) {
		s := states[0]
		if len(states) > 1 {
			states = states[1:]
		}
		switch s {
		case "read-error":
			return "", "", &apiReadError{errors.New("transport")}
		case "queued":
			return "queued", "", nil
		default:
			return s, p.name(), nil
		}
	}
	m := &monitor{p: p, progress: func(context.Context, Plan) (string, string, error) { return next() }, audit: func(Plan) error { audits++; return nil }}
	step := func() error { now = now.Add(monitorEvery); return m.tick(now) }
	states = []string{"queued", "in_progress", "in_progress", "completed", "completed"}
	for i := 0; i < 5; i++ {
		if e := step(); e != nil {
			t.Fatal(e)
		}
	}
	if !m.started || !m.completed || audits != 2 {
		// exactly one start check and one completion check
		t.Fatalf("started=%v completed=%v audits=%d", m.started, m.completed, audits)
	}
	// Ticks inside the polling interval make no request.
	called := false
	m2 := &monitor{p: p, progress: func(context.Context, Plan) (string, string, error) { called = true; return "queued", "", nil }, audit: func(Plan) error { return nil }}
	_ = m2.tick(now)
	called = false
	_ = m2.tick(now.Add(time.Second))
	if called {
		t.Fatal("monitor polled inside its interval")
	}
	// A short outage is tolerated, a sustained one is not.
	states = []string{"read-error"}
	m3 := &monitor{p: p, progress: func(context.Context, Plan) (string, string, error) { return next() }, audit: func(Plan) error { return nil }}
	var last error
	for i := 0; i < monitorSoftLimit; i++ {
		last = step2(m3, &now)
		if i < monitorSoftLimit-1 && last != nil {
			t.Fatalf("tolerable outage rejected at %d: %v", i, last)
		}
	}
	if last == nil {
		t.Fatal("sustained GitHub outage was tolerated")
	}
	// A policy mismatch is fatal immediately, never counted as an outage.
	m4 := &monitor{p: p, progress: func(context.Context, Plan) (string, string, error) { return "in_progress", p.name(), nil }, audit: func(Plan) error { return errors.New("branch moved") }}
	if e := step2(m4, &now); e == nil {
		t.Fatal("identity mismatch ignored")
	}
	m5 := &monitor{p: p, progress: func(context.Context, Plan) (string, string, error) {
		return "", "", errors.New("target job identity or runner assignment changed")
	}, audit: func(Plan) error { return nil }}
	if e := step2(m5, &now); e == nil {
		t.Fatal("foreign runner assignment ignored")
	}
}

func step2(m *monitor, now *time.Time) error {
	*now = now.Add(monitorEvery)
	return m.tick(*now)
}
