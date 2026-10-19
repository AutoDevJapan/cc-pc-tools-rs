package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var testNow = time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)

type fixture struct {
	opts Options
	root string
}

func fixtureFor(t *testing.T, todo string) fixture {
	t.Helper()
	root := t.TempDir()
	o := defaults()
	o.TodoFile = filepath.Join(root, "todo", "todo.json")
	o.JobsDir = filepath.Join(root, "jobs")
	o.PriorityFile = ""
	o.ResourceQueueDir = filepath.Join(root, "resource-queue")
	for _, dir := range []string{filepath.Dir(o.TodoFile), o.JobsDir, filepath.Join(root, "output")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture(t, o.TodoFile, []byte(todo))
	return fixture{o, root}
}

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, testNow, testNow); err != nil {
		t.Fatal(err)
	}
}

func (f fixture) job(t *testing.T, id, metadata string) {
	t.Helper()
	dir := filepath.Join(f.opts.JobsDir, id)
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(dir, "job.json"), []byte(metadata))
}

func absentProbe(uint32) processObservation { return processObservation{known: true} }

func snap(f fixture) Snapshot {
	return collect(context.Background(), f.opts, testNow, absentProbe)
}

func TestMeasuredProgressOnly(t *testing.T) {
	for _, tc := range []struct {
		status, raw, state string
		want               *int
	}{
		{"doing", "", "not_measured", nil}, {"doing", "null", "not_measured", nil},
		{"doing", "0", "measured", intPtr(0)}, {"wait", "87", "measured", intPtr(87)},
		{"doing", "100", "measured", intPtr(100)}, {"done", "12", "done", intPtr(100)},
		{"doing", "-1", "invalid", nil}, {"doing", "101", "invalid", nil},
		{"doing", "14.5", "invalid", nil}, {"doing", `"80"`, "invalid", nil},
		{"doing", "true", "invalid", nil}, {"doing", "{}", "invalid", nil},
	} {
		t.Run(tc.status+"-"+tc.raw, func(t *testing.T) {
			n, state := measuredProgress(todoItem{Status: tc.status, Progress: json.RawMessage(tc.raw)})
			if !reflect.DeepEqual(n, tc.want) || state != tc.state {
				t.Fatalf("progress/state: %v/%s", n, state)
			}
		})
	}
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","progress_percent":95,"overall_progress":99,"duration_ms":999999,"tokens":999999}]}`)
	s := snap(f)
	if s.Tasks[0].Percent != nil || s.Tasks[0].Execution != "not_observed" {
		t.Fatal("duration, tokens, aliases, and doing cannot manufacture completion or a running job")
	}
}

func intPtr(n int) *int { return &n }

func TestClockEvidenceAndFuture(t *testing.T) {
	for _, tc := range []struct{ input, state string }{
		{"", "unknown"}, {"untrusted", "invalid"},
		{"2026-10-13 19:00", "fresh"}, {"2026-10-13T10:00:00Z", "fresh"},
		{"2026-10-13T09:40:00Z", "stale"}, {"2026-10-13T10:00:01Z", "future"},
	} {
		c := timeClock(tc.input, testNow, 10*time.Minute)
		if c.State != tc.state || tc.state == "future" && c.AgeSeconds != nil {
			t.Fatalf("clock state %s, expected %s", c.State, tc.state)
		}
	}
	c := valueClock(json.RawMessage(`1791885600`), testNow, time.Minute)
	if c.State != "fresh" || c.AgeSeconds == nil || *c.AgeSeconds != 0 {
		t.Fatal("numeric epoch timestamps in the operational ToDo store must be supported")
	}
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","updated":1791885600,"owner_project":[],"subwork":false}]}`)
	s := snap(f)
	if s.Status != "ok" || s.Tasks[0].RecordUpdated.State != "fresh" || s.Tasks[0].ParentTodoID != nil {
		t.Fatal("legacy optional metadata must not make a valid store disappear")
	}
}

func TestCanonicalWaitReasons(t *testing.T) {
	f := fixtureFor(t, `{"items":[
	{"id":1,"status":"wait","wait_on":[2,3,900],"work_wait_on":[4]},
	{"id":2,"status":"done"},{"id":3,"status":"doing"},{"id":4,"status":"todo"},
	{"id":5,"project":"owner","status":"block"},
	{"id":6,"status":"block","note":"owner reply needed"},
	{"id":7,"status":"wait","wait_ready":{"condition":"chatgpt-plus"}},
	{"id":8,"status":"wait","wait_ready":{"condition":"retry-time","not_before":1791885660}}
	]}`)
	s := snap(f)
	byID := map[int64]Task{}
	for _, row := range s.Tasks {
		byID[row.ID] = row
	}
	if !reflect.DeepEqual(byID[1].WaitReasons[0].TodoIDs, []int64{3, 4, 900}) {
		t.Fatal("only known completed dependencies can be omitted")
	}
	if byID[5].WaitReasons[0].Kind != "owner_reply" || byID[6].WaitReasons[0].Kind != "blocked_unspecified" {
		t.Fatal("arbitrary note text is not owner-wait evidence")
	}
	if byID[7].WaitReasons[0].Kind != "subscription_readiness" || byID[8].WaitReasons[0].Kind != "technical_retry" || byID[8].WaitReasons[0].NotBefore.State != "future" {
		t.Fatal("readiness and technical retry must remain distinct from dependency waits")
	}
	for _, tc := range []struct{ code, kind string }{
		{"todo_dependency", "dependency"}, {"work_owner", "dependency"}, {"intake_owner", "other_worker"},
		{"running_limit", "execution_slot"}, {"supervisor_missing", "supervisor_restart"},
		{"todo_store_unavailable", "source_unavailable"}, {"todo_readiness", "readiness"},
		{"secret\u001b[31m", "unknown"},
	} {
		w := jobWaits(jobMeta{Status: "queued", QueueWaitReason: &queueWait{Code: tc.code}}, testNow, time.Minute)
		if len(w) != 1 || w[0].Kind != tc.kind {
			t.Fatalf("wait mapping expected %s", tc.kind)
		}
	}
	w := jobWaits(jobMeta{Status: "queued", Activity: "順番待ち: モデルの混雑によりuntrusted", RetryNotBefore: json.RawMessage(`1791885660`)}, testNow, time.Minute)
	if w[0].Kind != "model_capacity" || w[1].Kind != "rate_limit" {
		t.Fatal("model capacity and 429 cooldown are different evidence")
	}
	if len(jobWaits(jobMeta{Status: "running", LastFailureKind: "capacity"}, testNow, time.Minute)) != 0 {
		t.Fatal("an old capacity failure is not a current wait")
	}
	w = jobWaits(jobMeta{Status: "queued", WorkWaitOn: []int64{9}, QueueWaitReason: &queueWait{Code: "work_owner", OwnerTodoID: 8}}, testNow, time.Minute)
	if !reflect.DeepEqual(w[0].TodoIDs, []int64{8, 9}) {
		t.Fatal("canonical work-owner dependencies must retain recorded numeric links")
	}
}

func TestProcessIdentityAndUnknownLiveJob(t *testing.T) {
	created := uint64(50)
	for _, tc := range []struct {
		observed processObservation
		expected *uint64
		want     string
	}{
		{processObservation{}, &created, "unknown"},
		{processObservation{known: true}, &created, "absent"},
		{processObservation{known: true, alive: true}, &created, "alive_identity_unknown"},
		{processObservation{known: true, alive: true, created: &created}, nil, "alive_identity_unknown"},
		{processObservation{known: true, alive: true, created: &created}, &created, "verified_alive"},
		{processObservation{known: true, alive: true, created: &created}, uintPtr(51), "identity_mismatch"},
	} {
		if got := presence(99, tc.expected, func(uint32) processObservation { return tc.observed }); got != tc.want {
			t.Fatalf("presence %s; expected %s", got, tc.want)
		}
	}
	f := fixtureFor(t, `{"items":[]}`)
	f.job(t, "private-slug", `{"id":"private-slug","status":"running","project":"UNTRUSTED_LABEL","codex_pid":99,"last_event_at":"2026-10-13T09:59:00Z","activity":"thinking"}`)
	s := collect(context.Background(), f.opts, testNow, func(uint32) processObservation {
		return processObservation{known: true, alive: true}
	})
	if len(s.Jobs) != 1 || s.Jobs[0].Execution != "alive_identity_unknown" || s.Jobs[0].PublicLabel != "名称未確認" || len(s.Jobs[0].TodoIDs) != 0 {
		t.Fatal("a live job with unknown task/process identity cannot be discarded")
	}
	if s.Jobs[0].LastActivity.State != "fresh" || strings.Contains(s.Jobs[0].Key, "private") {
		t.Fatal("activity evidence must survive with an opaque job key")
	}
}

func uintPtr(n uint64) *uint64 { return &n }

func TestRunningDoesNotFollowDoingOrNewestQueued(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","jobs":["old","new"]},{"id":2,"status":"doing"}]}`)
	f.job(t, "old", `{"id":"old","status":"running","created_at":"2026-10-13T09:00:00Z","codex_pid":99,"codex_pid_created":50,"last_event_at":"2026-10-13T09:59:00Z","activity":"running: SECRET"}`)
	f.job(t, "new", `{"id":"new","status":"queued","created_at":"2026-10-13T09:59:00Z","last_event_at":"2026-10-13T09:59:30Z"}`)
	s := collect(context.Background(), f.opts, testNow, func(uint32) processObservation {
		return processObservation{known: true, alive: true, created: uintPtr(50)}
	})
	if s.Tasks[0].Execution != "running" || s.Tasks[0].Phase != "tool" || len(s.Tasks[0].JobKeys) != 2 || s.Tasks[1].Execution != "not_observed" {
		t.Fatal("verified running job should survive a newer queued controller")
	}
	for _, j := range s.Jobs {
		if j.RecordedStatus == "queued" && (j.LastActivity.At != nil || j.LastHeartbeat.State != "fresh") {
			t.Fatal("a waiting supervisor heartbeat is not worker activity")
		}
	}
}

func TestPriorityPolicyAndTransitiveDependencies(t *testing.T) {
	f := fixtureFor(t, `{"items":[
	{"id":1,"project":"Re:X","status":"wait","wait_on":[2]},
	{"id":2,"status":"wait","wait_on":[3]},{"id":3,"status":"todo"},
	{"id":4,"project":"Skills","status":"todo"},
	{"id":5,"project":"GoDD-Press","status":"todo"},
	{"id":6,"project":"GoDD-MCP-Alpha","status":"todo"},
	{"id":7,"project":"GoDD-AO","status":"todo"},
	{"id":8,"project":"GoDD-Rosetta","status":"todo"},
	{"id":9,"project":"localai-web-rs","status":"todo"}
	]}`)
	f.opts.PriorityFile = filepath.Join(f.root, "priorities.json")
	writeFixture(t, f.opts.PriorityFile, []byte(`{"items":{"1":{"department":"受託部"}}}`))
	s := snap(f)
	want := []int64{1, 2, 3, 9, 4, 6, 5, 7, 8}
	var got []int64
	for _, row := range s.Tasks {
		got = append(got, row.ID)
		if row.ID == 4 && (row.Department != "情報システム部" || row.Priority != 50) {
			t.Fatal("Skills is within information systems while retaining its owner's priority")
		}
		if row.ID == 1 && row.PublicLabel != "受託案件" {
			t.Fatal("client department must mask even a public project label")
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("priority order: %v", got)
	}
}

func TestPrivacyAndControlCharacters(t *testing.T) {
	secret := "NEVER_OUTPUT_THIS"
	f := fixtureFor(t, `{"items":[{"id":1,"project":"NEVER_OUTPUT_THIS@example.invalid","title":"NEVER_OUTPUT_THIS","note":"NEVER_OUTPUT_THIS","spec":"NEVER_OUTPUT_THIS","status":"doing","jobs":["NEVER_OUTPUT_THIS","../escape",{"job":"NEVER_OUTPUT_THIS"}],"updated":"invalid\u001b[31m"}]}`)
	f.job(t, secret, `{"id":"NEVER_OUTPUT_THIS","status":"running","task_type":"NEVER_OUTPUT_THIS","project":"NEVER_OUTPUT_THIS","last_message":"NEVER_OUTPUT_THIS","prompt":"NEVER_OUTPUT_THIS","cwd":"NEVER_OUTPUT_THIS","last_error":"NEVER_OUTPUT_THIS","activity":"editing: NEVER_OUTPUT_THIS\u001b[31m","queue_wait_reason":{"code":"NEVER_OUTPUT_THIS"}}`)
	s := snap(f)
	data, _ := json.Marshal(s)
	if bytes.Contains(data, []byte(secret)) || bytes.Contains(data, []byte("example.invalid")) || bytes.Contains(data, []byte(`\u001b`)) || bytes.Contains(data, []byte("../escape")) {
		t.Fatal("private text or terminal controls escaped the closed output vocabulary")
	}
	if s.Tasks[0].Percent != nil || s.Tasks[0].PublicLabel != "名称未確認" || s.Jobs[0].Phase != "editing" {
		t.Fatal("private records should still have useful generic metadata")
	}
	if publicLabel("GoDD-Press-NEVER_OUTPUT_THIS").kind != "generic" || publicLabel("localai-web-rs\u001b[31m").kind != "generic" {
		t.Fatal("public labels may not be inferred from prefixes or control stripping")
	}
}

func TestSafeTaskTypeAndNumericDetailRoute(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","jobs":["a"]},{"id":2,"status":"todo","task_type":"design"},{"id":3,"status":"todo","task_type":"PRIVATE@example.invalid"}]}`)
	f.job(t, "a", `{"id":"a","status":"running","task_type":"code-review","activity":"thinking"}`)
	s := snap(f)
	byID := map[int64]Task{}
	for _, row := range s.Tasks {
		byID[row.ID] = row
		if row.Detail.Kind != "todo" || row.Detail.TaskID != row.ID {
			t.Fatal("numeric detail route must preserve original task identity")
		}
	}
	if byID[1].TaskType != "review" || byID[1].TaskTypeSource != "job" || byID[1].Phase != "thinking" ||
		byID[2].TaskType != "design" || byID[2].TaskTypeSource != "todo" || byID[3].TaskType != "unknown" {
		t.Fatal("safe task type must distinguish work without deriving titles")
	}
	for _, raw := range []string{`"review PRIVATE"`, `"review\u001b[31m"`, `{"type":"review"}`, `1`} {
		if safeTaskType(json.RawMessage(raw)) != "unknown" {
			t.Fatal("arbitrary task type text must not enter the public enum")
		}
	}
}

func TestSourceFailuresAreNotEmptySuccess(t *testing.T) {
	for _, tc := range []struct{ data, want string }{
		{"{broken", "invalid_json"}, {`{"items":null}`, "invalid_record"},
		{`{"items":[{"id":1,"status":"todo"},{"id":1,"status":"doing"}]}`, "invalid_record"},
		{`{"items":[{"id":0,"status":"todo"}]}`, "invalid_record"},
	} {
		f := fixtureFor(t, tc.data)
		f.job(t, "live", `{"id":"live","status":"running"}`)
		s := snap(f)
		if s.Status != "degraded" || s.Sources["todo"].Status != tc.want || len(s.Jobs) != 1 || len(s.Tasks) != 0 {
			t.Fatal("invalid ToDo input must report failure while preserving standalone job evidence")
		}
	}
	f := fixtureFor(t, `{"items":[{"id":1,"status":"doing","jobs":["broken"]}]}`)
	f.job(t, "broken", `{broken`)
	s := snap(f)
	if s.Status != "degraded" || s.Sources["jobs"].Complete || len(s.Jobs) != 1 || s.Jobs[0].RecordedStatus != "unknown" || s.Tasks[0].Execution != "unknown" {
		t.Fatal("corrupt job metadata must retain an unknown row rather than claim no worker")
	}
	if err := os.Remove(f.opts.TodoFile); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(f.opts.JobsDir); err != nil {
		t.Fatal(err)
	}
	s = snap(f)
	if s.Status != "unavailable" || s.Sources["todo"].Status != "missing" || s.Sources["jobs"].Status != "missing" {
		t.Fatal("missing stores are not an empty initialized system")
	}
}

func TestBoundedReadsBOMAndOlderSizeLimit(t *testing.T) {
	f := fixtureFor(t, `{"items":[]}`)
	data := append([]byte{0xef, 0xbb, 0xbf}, []byte(`{"items":[{"id":1,"status":"todo"}],"ignored":"`)...)
	data = append(data, bytes.Repeat([]byte{'x'}, 17<<20)...)
	data = append(data, []byte(`"}`)...)
	writeFixture(t, f.opts.TodoFile, data)
	s := snap(f)
	if s.Status != "ok" || len(s.Tasks) != 1 || s.Sources["todo"].BytesRead <= 16<<20 {
		t.Fatal("production uses 64MiB; a valid store over the retired 16MiB limit must work")
	}
	f.opts.TodoBytes = int64(len(data))
	if snap(f).Status != "ok" {
		t.Fatal("exact byte boundary, including BOM, must work")
	}
	f.opts.TodoBytes--
	if s := snap(f); s.Sources["todo"].Status != "too_large" {
		t.Fatal("byte overflow must not be decoded as successful partial data")
	}
	f = fixtureFor(t, `{"items":[]}`)
	f.job(t, "a", `{"id":"a","status":"running"}`)
	f.job(t, "b", `{"id":"b","status":"running"}`)
	f.opts.MaxJobs = 1
	if s := snap(f); s.Status != "degraded" || s.Sources["jobs"].Complete || s.Sources["jobs"].Status != "directory_limit" {
		t.Fatal("bounded directory reads must advertise partial acquisition")
	}
	f.opts.MaxJobs = 100
	f.opts.JobsBytes = 1
	if s := snap(f); s.Status != "degraded" || s.Sources["jobs"].Complete {
		t.Fatal("aggregate job byte limit must be finite and visible")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s := collect(ctx, f.opts, testNow, absentProbe); s.Status != "degraded" || s.Sources["jobs"].Status != "deadline" {
		t.Fatal("an expired scan cannot report an empty successful jobs list")
	}
}

func TestSourceBytesRemainUnchangedAndFiltering(t *testing.T) {
	f := fixtureFor(t, `{"items":[{"id":1,"status":"done"},{"id":2,"status":"doing","progress":47},{"id":3,"status":"todo"}]}`)
	f.job(t, "a", `{"id":"a","status":"succeeded","todo_id":1}`)
	paths := []string{f.opts.TodoFile, filepath.Join(f.opts.JobsDir, "a", "job.json")}
	before := map[string][32]byte{}
	for _, path := range paths {
		data, _ := os.ReadFile(path)
		before[path] = sha256.Sum256(data)
	}
	s := snap(f)
	if len(s.Tasks) != 2 || s.Tasks[0].ID != 2 {
		t.Fatal("default is unfinished tasks")
	}
	f.opts.TaskIDs[1] = true
	s = snap(f)
	if len(s.Tasks) != 1 || *s.Tasks[0].Percent != 100 || len(s.Jobs) != 1 {
		t.Fatal("an explicit task filter can inspect a completed task and its latest job")
	}
	f.opts.TaskIDs = map[int64]bool{}
	f.opts.IncludeDone, f.opts.MaxRows = true, 1
	s = snap(f)
	if !s.Truncated || s.Status != "degraded" || s.TaskTotal != 3 || len(s.Tasks) != 1 {
		t.Fatal("filter truncation cannot silently pretend to be a complete list")
	}
	for _, path := range paths {
		data, _ := os.ReadFile(path)
		if sha256.Sum256(data) != before[path] {
			t.Fatal("snapshot changed its source")
		}
	}
}
