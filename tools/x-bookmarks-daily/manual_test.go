package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManualBatchRequiresActualItemCompletionAndNeverStartsAWorker(t *testing.T) {
	statePath := testPaths(t)
	if err := os.MkdirAll(filepath.Join(root, "runs"), 0700); err != nil {
		t.Fatal(err)
	}
	receipt := `{"date":"2026-10-21","todo":3244,"mode":"dedicated-manual","completed_count":2,"target_count":10,"entries":[{"done_registered":true,"unbookmarked":true},{"done_registered":true,"unbookmarked":true}]}`
	path := filepath.Join(root, "runs", "2026-10-21.json")
	if err := os.WriteFile(path, []byte(receipt), 0600); err != nil {
		t.Fatal(err)
	}
	state := State{Date: "2026-10-21", Todo: "3244", Mode: "manual"}
	_, message, err := execute(state.Date, state, statePath, func(string, ...string) (string, error) { t.Fatal("must not start a second worker"); return "", nil }, func(string) (bool, error) { t.Fatal("manual run has no invented job"); return false, nil })
	if err != nil || !strings.Contains(message, "2/10") || !strings.Contains(message, "no duplicate") {
		t.Fatal(message, err)
	}
	for _, invalid := range []string{strings.Replace(receipt, `"unbookmarked":true`, `"unbookmarked":false`, 1), strings.Replace(receipt, `"todo":3244`, `"todo":9999`, 1), strings.Replace(receipt, `"completed_count":2`, `"completed_count":0`, 1), "broken"} {
		if err := os.WriteFile(path, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := manualRun(state); err == nil {
			t.Fatal("unverified manual receipt accepted")
		}
	}
}

func TestManualReceiptMissingFailsInsteadOfReportingSuccess(t *testing.T) {
	statePath := testPaths(t)
	state := State{Date: "2026-10-21", Todo: "3244", Mode: "manual"}
	if _, _, err := execute(state.Date, state, statePath, func(string, ...string) (string, error) { t.Fatal("missing proof must not launch"); return "", nil }, func(string) (bool, error) { return false, nil }); err == nil {
		t.Fatal("missing proof returned success")
	}
}

func TestFailedRecordedJobIsNotReportedAsDailySuccess(t *testing.T) {
	statePath := testPaths(t)
	if err := os.MkdirAll(filepath.Join(jobsRoot, "failed-job"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobsRoot, "failed-job", "job.json"), []byte(`{"status":"failed"}`), 0600); err != nil {
		t.Fatal(err)
	}
	state := State{Date: "2026-10-21", Todo: "3244", Job: "failed-job"}
	if _, _, err := execute(state.Date, state, statePath, func(string, ...string) (string, error) {
		t.Fatal("keep the original failed job and ToDo")
		return "", nil
	}, func(string) (bool, error) { return false, nil }); err == nil {
		t.Fatal("failed job was reported as success")
	}
}

func TestMissingStateWithHistoryDoesNotBecomeAnEmptyFreshDay(t *testing.T) {
	for _, history := range []string{"runs", "done.txt"} {
		t.Run(history, func(t *testing.T) {
			dir := t.TempDir()
			if history == "runs" {
				if err := os.MkdirAll(filepath.Join(dir, "runs"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "runs", "2026-10-12.json"), []byte(`{"completed_count":7}`), 0600); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(filepath.Join(dir, "done.txt"), []byte("https://x.com/example/status/1\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := readState(filepath.Join(dir, "state.json")); err == nil {
				t.Fatal("missing state erased the distinction from a fresh installation")
			}
		})
	}
}
