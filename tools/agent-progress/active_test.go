package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestActiveOnlyUsesWorkerEvidenceAndPreservesDiagnosticView(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","progress":37,"jobs":["active","queued"]},{"id":2,"status":"wait","jobs":["waiting"]},{"id":3,"status":"block","jobs":["blocked"]},{"id":4,"status":"done","jobs":["done"]},{"id":5,"status":"doing","jobs":["report"]},{"id":6,"status":"doing","jobs":["stale"]}]}`)
	created := uint64(10)
	probe := func(uint32) processObservation {
		return processObservation{alive: true, known: true, created: &created}
	}
	for _, name := range []string{"active", "waiting", "blocked", "done", "report", "stale", "standalone"} {
		activity, at := "running: private command", testNow.Add(-time.Second)
		if name == "report" {
			activity = "said: private report"
		}
		if name == "stale" {
			at = testNow.Add(-31 * time.Second)
		}
		f.job(t, name, fmt.Sprintf(`{"id":%q,"status":"running","codex_pid":7,"codex_pid_created":10,"last_event_at":%q,"activity":%q}`, name, at.Format(time.RFC3339), activity))
	}
	f.job(t, "queued", `{"id":"queued","status":"queued","created_at":"2026-10-13T10:00:00Z","queue_wait_reason":{"code":"running_limit"}}`)
	f.job(t, "new-report", `{"id":"new-report","todo_id":1,"status":"running","created_at":"2026-10-13T10:00:00Z","codex_pid":7,"codex_pid_created":10,"last_event_at":"2026-10-13T10:00:00Z","activity":"said: fixture"}`)
	diagnostic := collect(context.Background(), f.opts, testNow, probe)
	if len(diagnostic.Tasks) != 5 {
		t.Fatal("diagnostic view changed")
	}
	if len(diagnostic.Tasks[0].WaitReasons) == 0 {
		t.Fatal("diagnostic wait reasons were removed")
	}
	f.opts.ActiveOnly, f.opts.IncludeDone, f.opts.MaxRows = true, true, 1
	s := collect(context.Background(), f.opts, testNow, probe)
	if s.TaskTotal != 1 || s.JobTotal != 2 || len(s.Tasks) != 1 || s.Tasks[0].ID != 1 ||
		s.Tasks[0].Percent == nil || *s.Tasks[0].Percent != 37 || s.Tasks[0].Phase != "tool" ||
		len(s.Tasks[0].WaitReasons) != 0 || len(s.Tasks[0].JobKeys) != 1 || !s.Truncated {
		t.Fatal("active projection, measurements or pre-limit totals are incorrect")
	}
	f.opts.MaxRows = 100
	s = collect(context.Background(), f.opts, testNow, probe)
	keys := []string{}
	for _, j := range s.Jobs {
		keys = append(keys, j.Key)
	}
	want := map[string]bool{jobKey("active"): true, jobKey("standalone"): true}
	for _, k := range keys {
		if !want[k] {
			t.Fatal("excluded work reappeared as a standalone job")
		}
	}
	if len(keys) != 2 {
		t.Fatal("confirmed standalone work was lost")
	}
}

func TestProcessingJobRejectsUnconfirmedAndNonprocessingEvidence(t *testing.T) {
	at := testNow.Add(-time.Second).Format(time.RFC3339)
	base := Job{RecordedStatus: "running", Execution: "running", WorkerPresence: "verified_alive", Phase: "tool", Source: Source{Status: "ok", Complete: true}, LastActivity: Clock{At: &at, State: "fresh"}}
	if !processingJob(&base, testNow) {
		t.Fatal("positive control missing")
	}
	cases := map[string]func(*Job){
		"queued":           func(j *Job) { j.RecordedStatus = "queued" },
		"lost":             func(j *Job) { j.RecordedStatus = "lost" },
		"finished":         func(j *Job) { j.Execution = "finished" },
		"supervisor":       func(j *Job) { j.Execution = "supervising" },
		"unknown identity": func(j *Job) { j.WorkerPresence = "alive_identity_unknown" },
		"cancelled":        func(j *Job) { j.cancelRequested = true },
		"failed source":    func(j *Job) { j.Source.Status = "read_failed" },
		"partial source":   func(j *Job) { j.Source.Complete = false },
		"resource wait":    func(j *Job) { j.WaitReasons = []WaitReason{{Kind: "execution_slot"}} },
		"report only":      func(j *Job) { j.Phase = "reporting" },
		"unknown phase":    func(j *Job) { j.Phase = "unknown" },
		"old clock":        func(j *Job) { v := testNow.Add(-31 * time.Second).Format(time.RFC3339); j.LastActivity.At = &v },
		"future clock":     func(j *Job) { v := testNow.Add(time.Second).Format(time.RFC3339); j.LastActivity.At = &v },
		"missing clock":    func(j *Job) { j.LastActivity.At = nil },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			j := base
			change(&j)
			if processingJob(&j, testNow) {
				t.Fatal("unconfirmed worker became active")
			}
		})
	}
}

func TestActiveOnlyEmptyCLIAndSchema(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","progress":83}]}`)
	f.opts.ActiveOnly = true
	s := snap(f)
	if len(s.Tasks) != 0 || len(s.Jobs) != 0 || s.TaskTotal != 0 || s.JobTotal != 0 {
		t.Fatal("a doing label manufactured work")
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var roundtrip Snapshot
	if json.Unmarshal(data, &roundtrip) != nil || !reflect.DeepEqual(s.Tasks, roundtrip.Tasks) {
		t.Fatal("empty lists must remain arrays")
	}
	var stdout, stderr bytes.Buffer
	args := []string{"--once", "--active-only", "--todo", f.opts.TodoFile, "--jobs", f.opts.JobsDir,
		"--priority-file", "", "--resource-queue", f.opts.ResourceQueueDir}
	if run(args, &stdout, &stderr) != 0 || stderr.Len() != 0 ||
		json.Unmarshal(stdout.Bytes(), &roundtrip) != nil || len(roundtrip.Tasks) != 0 || roundtrip.TaskTotal != 0 {
		t.Fatal("CLI did not apply the active-only projection")
	}
	stdout.Reset()
	stderr.Reset()
	if run(append(args, "--active-only=false"), &stdout, &stderr) != 0 ||
		json.Unmarshal(stdout.Bytes(), &roundtrip) != nil || len(roundtrip.Tasks) != 1 || *roundtrip.Tasks[0].Percent != 83 {
		t.Fatal("positive control lost the diagnostic row or its measured percent")
	}
}

func TestProcessingResourcesUsesLiveIdentitiesAndWholeFlight(t *testing.T) {
	f := fixtureFor(t, `{"items":[]}`)
	created := uint64(10)
	probe := func(uint32) processObservation {
		return processObservation{alive: true, known: true, created: &created}
	}
	check := func(want bool) {
		t.Helper()
		if processingResources(context.Background(), f.opts, "job", testNow, probe) != want {
			t.Fatal("resource evidence was misclassified")
		}
	}
	check(true)
	dir := filepath.Join(f.opts.JobsDir, "job", "resource-waits")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	check(true)
	wait := filepath.Join(dir, "wait.json")
	writeFixture(t, wait, []byte(`{"job_id":"job","kind":"cargo","pid":1,"pid_created":10}`))
	check(false)
	active := filepath.Join(dir, "active.json")
	writeFixture(t, active, []byte(`{"job_id":"job","kind":"cargo-active","pid":2,"pid_created":10}`))
	check(true)
	writeFixture(t, active, []byte(`{"job_id":"job","kind":"cargo-active","pid":2,"pid_created":9}`))
	check(false)
	writeFixture(t, wait, []byte(`{"job_id":"job","kind":"cargo","pid":1,"pid_created":9}`))
	check(true)
	writeFixture(t, wait, []byte(`{"job_id":"job","kind":"cargo","pid":1}`))
	check(false)
	writeFixture(t, wait, []byte(`broken`))
	check(false)
}

func TestProcessingResourcesExcludesLiveChainWaitWithoutUsingNames(t *testing.T) {
	f := fixtureFor(t, `{"items":[]}`)
	if err := os.MkdirAll(f.opts.ResourceQueueDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.opts.ResourceQueueDir, "queue.json")
	if safeOutput(path, f.opts) {
		t.Fatal("output accepted a resource queue source")
	}
	created := uint64(10)
	probe := func(uint32) processObservation {
		return processObservation{alive: true, known: true, created: &created}
	}
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{`{"job":"job","pid":1,"pid_created_filetime":10}`, false},
		{`{"job":"job","pid":1,"pid_created_filetime":9}`, true},
		{`{"job":"other","pid":1,"pid_created_filetime":10}`, true},
		{`{"job":"job","pid":1}`, false},
		{`broken`, false},
	} {
		writeFixture(t, path, []byte(tc.raw))
		if processingResources(context.Background(), f.opts, "job", testNow, probe) != tc.want {
			t.Fatal("queue identity or unknown resource evidence was misclassified")
		}
	}
}

func TestActiveOnlyExcludesCollaborationWait(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","jobs":["waiter"]}]}`)
	f.opts.ActiveOnly = true
	f.job(t, "waiter", `{"id":"waiter","status":"running","codex_pid":1,"codex_pid_created":10,"last_event_at":"2026-10-13T10:00:00Z","activity":"agent: wait"}`)
	created := uint64(10)
	probe := func(uint32) processObservation {
		return processObservation{alive: true, known: true, created: &created}
	}
	s := collect(context.Background(), f.opts, testNow, probe)
	if len(s.Tasks) != 0 || len(s.Jobs) != 0 {
		t.Fatal("waiting for a child is not active processing")
	}
	writeFixture(t, filepath.Join(f.opts.JobsDir, "waiter", "job.json"), []byte(`{"id":"waiter","status":"running","codex_pid":1,"codex_pid_created":10,"last_event_at":"2026-10-13T10:00:00Z","activity":"thinking"}`))
	s = collect(context.Background(), f.opts, testNow, probe)
	if len(s.Tasks) != 1 || len(s.Jobs) != 1 {
		t.Fatal("positive control did not resume after the child wait")
	}
}

func TestActiveOnlyMixedTaskRelationsStayConsistent(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","jobs":["active"]},{"id":2,"status":"wait","jobs":["active"]}]}`)
	f.opts.ActiveOnly = true
	f.job(t, "active", `{"id":"active","status":"running","codex_pid":1,"codex_pid_created":10,"last_event_at":"2026-10-13T10:00:00Z","activity":"running: fixture"}`)
	created := uint64(10)
	probe := func(uint32) processObservation {
		return processObservation{alive: true, known: true, created: &created}
	}
	s := collect(context.Background(), f.opts, testNow, probe)
	if len(s.Tasks) != 1 || s.Tasks[0].ID != 1 || len(s.Jobs) != 1 ||
		!reflect.DeepEqual(s.Jobs[0].TodoIDs, []int64{1}) || !reflect.DeepEqual(s.Tasks[0].JobKeys, []string{s.Jobs[0].Key}) {
		t.Fatal("active task/job relations must agree without resurrecting the waiting task")
	}
}

func TestActiveOnlyResourceMarkersAreWiredThroughCollection(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","progress":50,"jobs":["worker"]}]}`)
	f.opts.ActiveOnly = true
	f.job(t, "worker", `{"id":"worker","status":"running","codex_pid":1,"codex_pid_created":10,"last_event_at":"2026-10-13T10:00:00Z","activity":"running: fixture"}`)
	created := uint64(10)
	probe := func(uint32) processObservation {
		return processObservation{alive: true, known: true, created: &created}
	}
	check := func(want int) {
		t.Helper()
		s := collect(context.Background(), f.opts, testNow, probe)
		if len(s.Tasks) != want || len(s.Jobs) != want {
			t.Fatal("resource evidence did not reach the active projection")
		}
		if want == 1 && *s.Tasks[0].Percent != 50 {
			t.Fatal("resource observation changed measured progress")
		}
	}
	check(1)
	dir := filepath.Join(f.opts.JobsDir, "worker", "resource-waits")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(dir, "wait.json"), []byte(`{"job_id":"worker","kind":"cargo","pid":2,"pid_created":10}`))
	check(0)
	writeFixture(t, filepath.Join(dir, "active.json"), []byte(`{"job_id":"worker","kind":"cargo-active","pid":3,"pid_created":10}`))
	check(1)
	if err := os.Mkdir(f.opts.ResourceQueueDir, 0700); err != nil {
		t.Fatal(err)
	}
	queue := filepath.Join(f.opts.ResourceQueueDir, "queue.json")
	writeFixture(t, queue, []byte(`{"job":"worker","pid":4,"pid_created_filetime":10}`))
	check(1)
	writeFixture(t, queue, []byte(`broken`))
	s := collect(context.Background(), f.opts, testNow, probe)
	if s.Status != "degraded" || s.Sources["jobs"].Complete || len(s.Tasks) != 0 ||
		!reflect.DeepEqual(s.Issues, []string{"jobs_resource_unverified"}) {
		t.Fatal("unknown resource evidence must be visible as incomplete acquisition")
	}
	f.job(t, "zzz-history", `{"id":"zzz-history","status":"queued"}`)
	metadata, err := os.ReadFile(filepath.Join(f.opts.JobsDir, "worker", "job.json"))
	if err != nil {
		t.Fatal(err)
	}
	f.opts.JobsBytes = int64(len(metadata))
	s = collect(context.Background(), f.opts, testNow, probe)
	if s.Sources["jobs"].Status != "byte_limit" {
		t.Fatal("resource uncertainty overwrote the original acquisition failure")
	}
}
