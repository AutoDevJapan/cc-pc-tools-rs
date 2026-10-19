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
		{"queued: no registration yet", phaseQueued, fakeRepo{job: queued}, nil, false, true},
		{"queued: own registration waiting", phaseQueued, fakeRepo{runners: []map[string]any{ownRunnerRow(p, labels...)}, job: queued}, nil, false, true},
		{"queued: label drift", phaseQueued, fakeRepo{runners: []map[string]any{ownRunnerRow(p, "self-hosted", "Windows", "X64")}, job: queued}, nil, false, false},
		{"queued: contract mismatch", phaseQueued, fakeRepo{job: queued}, errors.New("branch moved"), false, false},
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

func TestMonitorStopsWhenTheJobEndsBeforeAssignmentAndAuditsWhileQueued(t *testing.T) {
	p := fixturePlan()
	now := time.Unix(5000, 0)
	status := "queued"
	queuedAudits := 0
	var auditErr error
	m := &monitor{p: p, lastQueued: now,
		progress:    func(context.Context, Plan) (string, string, error) { return status, "", nil },
		audit:       func(Plan) error { t.Fatal("running audit before assignment"); return nil },
		auditQueued: func(Plan) error { queuedAudits++; return auditErr }}
	for i := 0; i < 12; i++ { // 120 s of queued polling
		if e := step2(m, &now); e != nil {
			t.Fatal(e)
		}
	}
	if queuedAudits != 2 {
		t.Fatalf("queued audits %d, want one per minute", queuedAudits)
	}
	auditErr = errors.New("branch moved")
	var e error
	for i := 0; i < 6 && e == nil; i++ {
		e = step2(m, &now)
	}
	if e == nil {
		t.Fatal("policy mismatch while queued ignored")
	}
	// A job that completed (cancelled) with no runner must end the run at once.
	auditErr = nil
	status = "completed"
	m2 := &monitor{p: p, lastQueued: now, progress: m.progress, audit: m.audit, auditQueued: m.auditQueued}
	if e := step2(m2, &now); e == nil {
		t.Fatal("job ended before assignment kept the slot")
	}
}

// fakeHold drives holdUntilDrained with a virtual clock: every sleep advances it.
type fakeHold struct {
	clock    time.Time
	states   []func() (uint32, error)
	drained  func() (bool, error)
	census   func() (int, error)
	sleeps   int
	queries  int
	censuses int
}

func (f *fakeHold) ops() holdOps {
	return holdOps{
		query: func() (uint32, error) {
			s := f.states[min(f.queries, len(f.states)-1)]
			f.queries++
			return s()
		},
		drain:  func() (bool, error) { return f.drained() },
		census: func() (int, error) { f.censuses++; return f.census() },
		now:    func() time.Time { return f.clock },
		sleep:  func(d time.Duration) { f.sleeps++; f.clock = f.clock.Add(d) },
	}
}
func state(n uint32) func() (uint32, error) { return func() (uint32, error) { return n, nil } }
func stateErr() func() (uint32, error) {
	return func() (uint32, error) { return 0, errors.New("scm query") }
}

func TestHoldWaitsForStopAndDrainProof(t *testing.T) {
	start := time.Unix(0, 0)
	until := holdDeadline(start, 600)
	f := &fakeHold{clock: start, states: []func() (uint32, error){state(4), stateErr(), state(3), state(1)}, drained: func() (bool, error) { return true, nil }, census: func() (int, error) { t.Fatal("census not needed"); return 0, nil }}
	res := holdUntilDrained(f.ops(), until, time.Second)
	if res.Err != nil || !res.Stopped || !res.Drained || f.queries != 4 {
		t.Fatalf("%+v queries=%d", res, f.queries)
	}
}

// R1: unreadable SCM state is not a stop. The hold lasts to the deadline and
// then reports an uncertain end.
func TestHoldDoesNotReleaseOnUnreadableServiceState(t *testing.T) {
	start := time.Unix(0, 0)
	until := holdDeadline(start, 60)
	f := &fakeHold{clock: start, states: []func() (uint32, error){stateErr()}, drained: func() (bool, error) { return true, nil }, census: func() (int, error) { return 0, nil }}
	res := holdUntilDrained(f.ops(), until, time.Second)
	if res.Err == nil || res.Stopped || res.Drained {
		t.Fatalf("%+v", res)
	}
	if f.clock.Before(until) {
		t.Fatalf("released at %v, before the deadline %v", f.clock.Sub(start), until.Sub(start))
	}
	// A running service holds just the same.
	f = &fakeHold{clock: start, states: []func() (uint32, error){state(4)}, drained: func() (bool, error) { return false, nil }, census: func() (int, error) { return 3, nil }}
	if res = holdUntilDrained(f.ops(), until, time.Second); res.Err == nil || res.Stopped || f.clock.Before(until) {
		t.Fatalf("running service released early: %+v", res)
	}
}

// R2: the deadline is anchored at the service start and covers startup, the
// whole run and the Job drain, even when earlier API audits took minutes.
func TestHoldDeadlineCoversWorkerLifetime(t *testing.T) {
	start := time.Unix(1000, 0)
	for _, secs := range []int{1, 600, 14400} {
		d := holdDeadline(start, secs).Sub(start)
		worst := 31*time.Second + time.Duration(secs)*time.Second + 30*time.Second // pipe wait + run + Job drain wait
		if d < worst+20*time.Second {
			t.Fatalf("seconds=%d deadline %v does not cover %v with slack", secs, d, worst)
		}
	}
	// 150 s of audits before the start must not shorten the hold.
	begun := start.Add(-150 * time.Second)
	if holdDeadline(start, 600).Sub(begun) < holdDeadline(begun, 600).Sub(begun)+150*time.Second {
		t.Fatal("pre-start time reduced the hold")
	}
}

// R3: STOPPED alone is not a drain. Without launcher proof the hold waits for an
// empty account and still reports the fence; with neither it holds to the deadline.
func TestHoldDistinguishesStoppedFromDrained(t *testing.T) {
	start := time.Unix(0, 0)
	until := holdDeadline(start, 60)
	missing := func() (bool, error) { return false, errors.New("report missing") }
	f := &fakeHold{clock: start, states: []func() (uint32, error){state(1)}, drained: missing, census: func() (int, error) { return 0, nil }}
	res := holdUntilDrained(f.ops(), until, time.Second)
	if !res.Stopped || res.Drained || !res.CensusZero || res.Err == nil {
		t.Fatalf("empty account without drain proof: %+v", res)
	}
	falseReport := func() (bool, error) { return false, nil }
	n := 0
	f = &fakeHold{clock: start, states: []func() (uint32, error){state(1)}, drained: falseReport, census: func() (int, error) { n++; return min(n, 2), nil }}
	res = holdUntilDrained(f.ops(), until, time.Second)
	if res.Drained || res.CensusZero || res.Err == nil || f.clock.Before(until) {
		t.Fatalf("survivors were released early: %+v clock=%v", res, f.clock.Sub(start))
	}
}

// RR1: unclassifiable processes must not let an empty readable census release the slot.
func TestHoldIgnoresAnIncompleteCensusAndAcceptsOnlyAHonestNoWorkerReport(t *testing.T) {
	start := time.Unix(0, 0)
	until := holdDeadline(start, 60)
	missing := func() (bool, error) { return false, errors.New("no proof") }
	incomplete := func() (int, error) { return 0, errCensusIncomplete }
	f := &fakeHold{clock: start, states: []func() (uint32, error){state(1)}, drained: missing, census: incomplete}
	res := holdUntilDrained(f.ops(), until, time.Second)
	if res.Drained || res.CensusZero || res.Err == nil || f.clock.Before(until) {
		t.Fatalf("incomplete census released the slot: %+v clock=%v", res, f.clock.Sub(start))
	}
	// A report proving no worker was ever created is a drain by construction.
	f = &fakeHold{clock: start, states: []func() (uint32, error){state(1)}, drained: func() (bool, error) { return true, nil }, census: incomplete}
	if res = holdUntilDrained(f.ops(), until, time.Second); !res.Drained || res.Err != nil || f.sleeps != 0 {
		t.Fatalf("%+v", res)
	}
}

// RR4: the launcher's worker end is the controller's absolute deadline, so the
// hold (run deadline + drain budget) always covers it, however long startup took.
func TestWorkerDeadlineIsTheSharedAbsoluteDeadline(t *testing.T) {
	p := fixturePlan()
	p.Seconds = 600
	start := time.Unix(10_000, 0)
	a := admission{Deadline: runDeadline(start, p.Seconds).UnixNano()}
	for _, delay := range []time.Duration{time.Second, 40 * time.Second, 59 * time.Second} {
		now := start.Add(delay)
		d, e := workerDeadline(p, a, now)
		if e != nil {
			t.Fatal(e)
		}
		if d.After(holdDeadline(start, p.Seconds).Add(-90 * time.Second)) {
			t.Fatalf("worker end %v exceeds the run deadline", d)
		}
		if !holdDeadline(start, p.Seconds).After(d.Add(60 * time.Second)) {
			t.Fatal("hold does not cover the worker end plus the drain budget")
		}
	}
	// A late resume is refused instead of outliving the hold.
	if _, e := workerDeadline(p, a, runDeadline(start, p.Seconds).Add(time.Second)); e == nil {
		t.Fatal("worker resumed after the shared deadline")
	}
	for _, bad := range []admission{{}, {Deadline: start.Add(-time.Hour).UnixNano()}, {Deadline: start.Add(10 * time.Hour).UnixNano()}} {
		if _, e := workerDeadline(p, bad, start); e == nil {
			t.Fatalf("accepted deadline %d", bad.Deadline)
		}
	}
	f := p
	f.Fixture = "normal"
	if d, e := workerDeadline(f, admission{}, start); e != nil || !d.Equal(start.Add(600*time.Second)) {
		t.Fatalf("fixture keeps its local plan seconds: %v %v", d, e)
	}
}
