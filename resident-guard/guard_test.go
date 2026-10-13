package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeBackend struct {
	probes      map[string]probeResult
	task        taskResult
	taskErr     error
	startErr    error
	starts      []string
	taskCalls   int
	intentSaved *bool
	intentFirst bool
}

func (fake *fakeBackend) Probe(_ context.Context, definition service) probeResult {
	return fake.probes[definition.ID]
}
func (fake *fakeBackend) Task(context.Context, service) (taskResult, error) {
	fake.taskCalls++
	return fake.task, fake.taskErr
}
func (fake *fakeBackend) Start(_ context.Context, definition service) error {
	if fake.intentSaved != nil {
		fake.intentFirst = *fake.intentSaved
	}
	fake.starts = append(fake.starts, definition.ID)
	return fake.startErr
}

func newState() guardState { return guardState{Version: 1, Services: map[string]serviceState{}} }
func newAbsentBackend() *fakeBackend {
	return &fakeBackend{probes: map[string]probeResult{"test": {Port: "closed", Code: "connection_refused"}},
		task: taskResult{Valid: true, Enabled: true}}
}
func definitions() []service     { return []service{{ID: "test"}} }
func noPersist(guardState) error { return nil }

func TestRestartNeedsRepeatedConfirmedAbsenceAndDurableIntent(t *testing.T) {
	now := time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)
	state := newState()
	fake := newAbsentBackend()
	saved := false
	fake.intentSaved = &saved
	persist := func(state guardState) error {
		saved = len(state.Services["test"].Attempts) == 1
		return nil
	}
	first := evaluate(context.Background(), definitions(), &state, fake, now, defaultPolicy(), persist)
	if first.Services[0].Action != "confirmation_pending" || len(fake.starts) != 0 || fake.taskCalls != 0 {
		t.Fatalf("first absence incorrectly started a task: %+v", first)
	}
	second := evaluate(context.Background(), definitions(), &state, fake, now.Add(time.Minute), defaultPolicy(), persist)
	if len(fake.starts) != 1 || !fake.intentFirst || second.Services[0].Action != "task_start_requested" {
		t.Fatalf("confirmed absence did not record intent before starting: %+v", second)
	}
}

func TestRestartBudgetAndCooldownSurviveReload(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	state := newState()
	state.Services["test"] = serviceState{Failures: 2}
	fake := newAbsentBackend()
	statePath := filepath.Join(t.TempDir(), "state.json")
	persist := func(state guardState) error { return saveJSON(statePath, state) }
	for _, step := range []struct {
		after  time.Duration
		action string
	}{
		{0, "task_start_requested"}, {time.Minute, "cooldown"},
		{10 * time.Minute, "task_start_requested"}, {20 * time.Minute, "task_start_requested"},
		{30 * time.Minute, "restart_budget_exhausted"}, {61 * time.Minute, "task_start_requested"},
	} {
		out := evaluate(context.Background(), definitions(), &state, fake, now.Add(step.after), defaultPolicy(), persist)
		if out.Services[0].Action != step.action {
			t.Fatalf("after %s: wanted %s, got %s", step.after, step.action, out.Services[0].Action)
		}
		if err := persist(state); err != nil {
			t.Fatal(err)
		}
		var err error
		state, err = loadState(statePath, definitions())
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(fake.starts) != 4 {
		t.Fatalf("unexpected starts: %d", len(fake.starts))
	}
}

func TestPortUncertaintyAndExistingTaskNeverTriggerDuplicateStartup(t *testing.T) {
	tests := []struct {
		name   string
		probe  probeResult
		task   taskResult
		err    error
		action string
	}{
		{"listening", probeResult{Port: "open"}, taskResult{Valid: true, Enabled: true}, nil, "listening_or_unconfirmed_port_retained"},
		{"unknown", probeResult{Port: "unknown"}, taskResult{Valid: true, Enabled: true}, nil, "listening_or_unconfirmed_port_retained"},
		{"running", probeResult{Port: "closed"}, taskResult{Valid: true, Enabled: true, Running: true}, nil, "delegated_to_running_task"},
		{"disabled", probeResult{Port: "closed"}, taskResult{Valid: true}, nil, "task_disabled"},
		{"changed", probeResult{Port: "closed"}, taskResult{Enabled: true}, nil, "task_definition_mismatch"},
		{"query_failed", probeResult{Port: "closed"}, taskResult{}, errors.New("private detail must not escape"), "task_query_failed"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := newState()
			state.Services["test"] = serviceState{Failures: 2}
			fake := &fakeBackend{probes: map[string]probeResult{"test": test.probe}, task: test.task, taskErr: test.err}
			out := evaluate(context.Background(), definitions(), &state, fake, time.Now().UTC(), defaultPolicy(), noPersist)
			data, _ := json.Marshal(out)
			if out.Services[0].Action != test.action || len(fake.starts) != 0 || strings.Contains(string(data), "private") {
				t.Fatalf("unexpected action or leaked diagnostic: %s", data)
			}
		})
	}
}

func TestDependencyAndHealthyRecovery(t *testing.T) {
	state := newState()
	state.Services["test"] = serviceState{Failures: 2}
	state.Services["upstream"] = serviceState{Failures: 2}
	fake := newAbsentBackend()
	fake.probes["upstream"] = probeResult{Port: "open", Code: "http_unhealthy"}
	services := []service{{ID: "upstream"}, {ID: "test", Dependency: "upstream"}}
	now := time.Now().UTC()
	out := evaluate(context.Background(), services, &state, fake, now, defaultPolicy(), noPersist)
	if out.Services[1].Action != "dependency_unhealthy" || len(fake.starts) != 0 {
		t.Fatal(out)
	}
	fake.probes["upstream"] = probeResult{Healthy: true, Port: "open"}
	out = evaluate(context.Background(), services, &state, fake, now.Add(time.Minute), defaultPolicy(), noPersist)
	if out.Services[1].Action != "task_start_requested" || len(fake.starts) != 1 {
		t.Fatal(out)
	}
	fake.probes["test"] = probeResult{Healthy: true, Port: "open"}
	out = evaluate(context.Background(), services, &state, fake, now.Add(2*time.Minute), defaultPolicy(), noPersist)
	if !out.Healthy || state.Services["test"].Failures != 0 || state.Services["test"].LastHealthy == nil || len(state.Services["test"].Attempts) != 1 {
		t.Fatal(out)
	}
}

func TestIntentFailureAndAmbiguousStartupRemainBounded(t *testing.T) {
	now := time.Now().UTC()
	for _, intentFails := range []bool{true, false} {
		state := newState()
		state.Services["test"] = serviceState{Failures: 2}
		fake := newAbsentBackend()
		fake.startErr = errors.New("ambiguous startup")
		persist := func(guardState) error {
			if intentFails {
				return errors.New("disk full")
			}
			return nil
		}
		out := evaluate(context.Background(), definitions(), &state, fake, now, defaultPolicy(), persist)
		if intentFails && (out.Services[0].Action != "restart_intent_write_failed" || len(fake.starts) != 0) {
			t.Fatal(out)
		}
		if !intentFails && (out.Services[0].Action != "task_start_unconfirmed" || len(fake.starts) != 1) {
			t.Fatal(out)
		}
		second := evaluate(context.Background(), definitions(), &state, fake, now.Add(time.Minute), defaultPolicy(), persist)
		if second.Services[0].Action != "cooldown" {
			t.Fatal(second)
		}
	}
}

func TestBackwardsClockAndUnsortedAttemptsKeepCooldown(t *testing.T) {
	now := time.Now().UTC()
	state := newState()
	state.Services["test"] = serviceState{Failures: 2, Attempts: []time.Time{now.Add(time.Hour), now.Add(-30 * time.Minute)}}
	fake := newAbsentBackend()
	out := evaluate(context.Background(), definitions(), &state, fake, now, defaultPolicy(), noPersist)
	if out.Services[0].Action != "cooldown" || len(fake.starts) != 0 {
		t.Fatal(out)
	}
}

func TestCancelledTickDoesNotLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	state := newState()
	state.Services["test"] = serviceState{Failures: 2}
	fake := newAbsentBackend()
	out := evaluate(ctx, definitions(), &state, fake, time.Now().UTC(), defaultPolicy(), noPersist)
	if out.Services[0].Action != "deadline" || fake.taskCalls != 0 || len(fake.starts) != 0 {
		t.Fatal(out)
	}
}

func TestHealthyResponseRequiresRecognizedServiceAndNoRedirect(t *testing.T) {
	for _, test := range []struct {
		name, body       string
		code             int
		console, healthy bool
	}{
		{"opencodex", `{"status":"ok","service":"opencodex"}`, 200, false, true},
		{"operations", `{"status":"ok","console":{"online":true},"partial":true}`, 200, true, true},
		{"false_console", `{"status":"ok","console":{"online":false}}`, 200, true, false},
		{"wrong_service", `{"status":"ok","service":"another"}`, 200, false, false},
		{"html_fallback", `<html>ok</html>`, 200, false, false},
		{"oversized", strings.Repeat(" ", 64*1024) + `{"status":"ok","service":"opencodex"}`, 200, false, false},
		{"service_unavailable", `{"status":"ok","service":"opencodex"}`, 503, false, false},
		{"redirect", ``, 302, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.code == 302 {
					w.Header().Set("Location", "http://127.0.0.1:1/private")
				}
				w.WriteHeader(test.code)
				w.Write([]byte(test.body))
			}))
			defer server.Close()
			out := probeHTTP(context.Background(), service{URL: server.URL, Address: strings.TrimPrefix(server.URL, "http://"), Console: test.console}, time.Second)
			if out.Healthy != test.healthy || out.Port != "open" {
				t.Fatalf("unexpected probe: %+v", out)
			}
		})
	}
}

func TestHTTPDeadlineKeepsListeningService(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(started); <-release }))
	defer server.Close()
	defer close(release)
	before := time.Now()
	out := probeHTTP(context.Background(), service{URL: server.URL, Address: strings.TrimPrefix(server.URL, "http://")}, 100*time.Millisecond)
	select {
	case <-started:
	default:
		t.Fatal("request did not reach server")
	}
	if out.Healthy || out.Port != "open" || time.Since(before) > 2*time.Second {
		t.Fatalf("deadline or port classification failed: %+v", out)
	}
}

func TestConfirmedRefusalIsDifferentFromCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	listener.Close()
	target := service{URL: "http://" + address, Address: address}
	out := probeHTTP(context.Background(), target, time.Second)
	if out.Port != "closed" {
		t.Fatalf("expected confirmed absence: %+v", out)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out = probeHTTP(ctx, target, time.Second)
	if out.Port != "unknown" {
		t.Fatalf("cancelled check was mistaken for absence: %+v", out)
	}
}

func TestCorruptAndOversizedStatePreservedWithoutLaunching(t *testing.T) {
	for _, data := range []string{`{`, `{"version":1,"services":{"unknown":{"failures":3}}}`, `{"version":1,"services":{"test":{"failures":-1}}}`, strings.Repeat("x", stateLimit+1)} {
		directory := t.TempDir()
		path := filepath.Join(directory, "state.json")
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		fake := newAbsentBackend()
		out, err := runCheck(context.Background(), directory, definitions(), fake, defaultPolicy())
		remaining, readErr := os.ReadFile(path)
		if err == nil || out.StateError != "state_invalid_or_unreadable" || len(fake.starts) != 0 || readErr != nil || string(remaining) != data {
			t.Fatalf("invalid state was changed or used: %+v, error=%v", out, err)
		}
		if _, err := os.Stat(filepath.Join(directory, "status.json")); err != nil {
			t.Fatal("failure heartbeat missing")
		}
	}
}

func TestStateDirectoryRejectsSymlinkAncestor(t *testing.T) {
	directory := t.TempDir()
	target := filepath.Join(directory, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "linked")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if err := validateStateDirectory(filepath.Join(link, "state")); err == nil {
		t.Fatal("linked ancestor was accepted")
	}
	if err := validateStateDirectory(filepath.Join(target, "state")); err != nil {
		t.Fatal("physical state directory was rejected", err)
	}
}

func TestLogRotationAndAtomicStateReplacement(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "events.jsonl")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", logLimit)), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".1", []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	out := report{Version: 1, CheckedAt: time.Now().UTC(), Services: []serviceReport{}}
	if err := appendReport(path, out); err != nil {
		t.Fatal(err)
	}
	previous, _ := os.Stat(path + ".1")
	current, _ := os.Stat(path)
	if previous.Size() != logLimit || current.Size() >= 1024 {
		t.Fatal("log did not rotate into one bounded generation")
	}
	state := newState()
	state.Services["test"] = serviceState{Failures: 2}
	statePath := filepath.Join(directory, "state.json")
	if err := saveJSON(statePath, state); err != nil {
		t.Fatal(err)
	}
	state.Services["test"] = serviceState{Failures: 3}
	if err := saveJSON(statePath, state); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadState(statePath, definitions())
	if err != nil || loaded.Services["test"].Failures != 3 {
		t.Fatal("state replacement did not persist")
	}
	entries, _ := os.ReadDir(directory)
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatal("temporary file leaked")
		}
	}
}

func TestFixturePortsExcludeProductionAndPrivilegedEndpoints(t *testing.T) {
	for _, port := range []int{0, 80, 10012, 10100, 10101, 19999, 65536} {
		if validFixturePort(port) {
			t.Fatalf("unsafe fixture port: %d", port)
		}
	}
	if !validFixturePort(24090) {
		t.Fatal("valid fixture port rejected")
	}
}
