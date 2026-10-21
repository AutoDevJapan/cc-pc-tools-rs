package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testPaths(t *testing.T) string {
	t.Helper()
	oldRoot, oldJobs := root, jobsRoot
	root = t.TempDir()
	jobsRoot = filepath.Join(root, "jobs")
	t.Cleanup(func() { root = oldRoot; jobsRoot = oldJobs })
	if e := os.WriteFile(filepath.Join(root, "daily-prompt.md"), []byte("same workflow"), 0600); e != nil {
		t.Fatal(e)
	}
	return filepath.Join(root, "state.json")
}
func TestLaunchAndRecovery(t *testing.T) {
	statePath := testPaths(t)
	calls := 0
	run := func(exe string, args ...string) (string, error) {
		calls++
		if exe == todoExe && args[0] == "add" {
			return "added 123", nil
		}
		if exe == jobsExe {
			joined := strings.Join(args, " ")
			for _, v := range []string{"--mcp-profile browser", "--launch-key " + launchKey("2026-10-05"), "--model gpt-6.1-sol", "--cwd D:/CC"} {
				if !strings.Contains(joined, v) {
					t.Fatal(joined)
				}
			}
			os.MkdirAll(filepath.Join(jobsRoot, "test-job"), 0700)
			os.WriteFile(filepath.Join(jobsRoot, "test-job", "job.json"), []byte(`{"status":"running"}`), 0600)
			return "test-job", nil
		}
		if exe == todoExe && args[0] == "start" && args[1] == "123" && args[3] == "test-job" {
			return "", nil
		}
		t.Fatal(exe, args)
		return "", nil
	}
	s, msg, e := execute("2026-10-05", State{}, statePath, run, func(string) (bool, error) { return false, nil })
	if e != nil || s.Job != "test-job" || calls != 3 || msg != "launched test-job" {
		t.Fatal(s, msg, e, calls)
	}
	b, _ := os.ReadFile(filepath.Join(root, "run-prompt.md"))
	if !strings.Contains(string(b), "123") {
		t.Fatal(string(b))
	}
	// A launch response lost before state save must reuse the stored ToDo and deterministic key.
	calls = 0
	_, _, e = execute("2026-10-05", State{Date: "2026-10-05", Todo: "123"}, statePath, run, func(string) (bool, error) { return false, nil })
	if e != nil || calls != 2 {
		t.Fatal(e, calls)
	}
}
func TestFailedLaunchKeepsTodoAndRetries(t *testing.T) {
	statePath := testPaths(t)
	run := func(exe string, args ...string) (string, error) {
		if exe == todoExe {
			return "added 456", nil
		}
		return "", errors.New("launch unavailable")
	}
	_, _, e := execute("2026-10-05", State{}, statePath, run, func(string) (bool, error) { return false, nil })
	if e == nil {
		t.Fatal("must fail")
	}
	s, e := readState(statePath)
	if e != nil || s.Todo != "456" || s.Job != "" {
		t.Fatal(s, e)
	}
}

func TestJSTBoundary(t *testing.T) {
	for _, c := range []struct{ at, day string }{{"2026-10-04T14:59:59Z", "2026-10-04"}, {"2026-10-04T15:00:00Z", "2026-10-05"}} {
		x, _ := time.Parse(time.RFC3339, c.at)
		if got := dayOf(x); got != c.day {
			t.Fatal(got)
		}
	}
}
func TestDayKey(t *testing.T) {
	a := launchKey("2026-10-05")
	if len(a) != 32 || a != launchKey("2026-10-05") || a == launchKey("2026-10-06") {
		t.Fatal(a)
	}
}
func TestNoDuplicateOrOverlap(t *testing.T) {
	_ = testPaths(t)
	if err := os.MkdirAll(filepath.Join(jobsRoot, "today"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobsRoot, "today", "job.json"), []byte(`{"status":"succeeded"}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		state  State
		active bool
		want   string
	}{{State{Date: "2026-10-05", Job: "today"}, false, "already launched today"}, {State{Date: "2026-10-04", Job: "old"}, true, "previous job still active"}} {
		_, msg, e := execute("2026-10-05", c.state, "unused", func(string, ...string) (string, error) { t.Fatal("must not launch"); return "", nil }, func(string) (bool, error) { return c.active, nil })
		if (c.active && e == nil) || (!c.active && e != nil) || msg != c.want {
			t.Fatal(msg, e)
		}
	}
}
func TestStateRoundTripAndCorrupt(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	s := State{Date: "2026-10-05", Job: "job", Todo: "123"}
	if e := saveState(p, s); e != nil {
		t.Fatal(e)
	}
	got, e := readState(p)
	if e != nil || got != s {
		t.Fatal(got, e)
	}
	os.WriteFile(p, []byte("broken"), 0600)
	if _, e = readState(p); e == nil {
		t.Fatal("corrupt state must not silently relaunch")
	}
}
