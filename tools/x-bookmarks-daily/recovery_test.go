package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFailedTodoLinkRetriesWithoutLaunchingAnotherJob(t *testing.T) {
	statePath := testPaths(t)
	starts := 0
	run := func(exe string, args ...string) (string, error) {
		if exe == todoExe && args[0] == "add" {
			return "added 456", nil
		}
		if exe == jobsExe {
			if starts > 0 {
				t.Fatal("must not relaunch the accepted worker")
			}
			if err := os.MkdirAll(filepath.Join(jobsRoot, "accepted-job"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(jobsRoot, "accepted-job", "job.json"), []byte(`{"status":"running"}`), 0600); err != nil {
				t.Fatal(err)
			}
			return "accepted-job", nil
		}
		if exe != todoExe || len(args) != 4 || args[0] != "start" || args[1] != "456" || args[3] != "accepted-job" {
			t.Fatal(exe, args)
		}
		starts++
		if starts == 1 {
			return "", errors.New("temporary link failure")
		}
		return "", nil
	}
	if _, _, err := execute("2026-10-21", State{}, statePath, run, func(string) (bool, error) { return true, nil }); err == nil {
		t.Fatal("link failure must fail")
	}
	saved, err := readState(statePath)
	if err != nil || saved.Job != "accepted-job" || saved.TodoStarted {
		t.Fatal(saved, err)
	}
	if _, _, err := execute("2026-10-21", saved, statePath, run, func(string) (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	saved, err = readState(statePath)
	if err != nil || !saved.TodoStarted || starts != 2 {
		t.Fatal(saved, starts, err)
	}
}

func TestManualAdoptionDoesNotOverwriteExistingOwnership(t *testing.T) {
	statePath := testPaths(t)
	if err := os.MkdirAll(filepath.Join(root, "runs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "runs", "2026-10-21.json"), []byte(`{"date":"2026-10-21","todo":3244,"mode":"dedicated-manual","completed_count":1,"target_count":10,"entries":[{"done_registered":true,"unbookmarked":true}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, previous := range []string{`{"date":"2026-10-21","job":"accepted-job","todo":"3244"}`, `{"date":"2026-10-21","todo":"9999"}`, `broken`} {
		if err := os.WriteFile(statePath, []byte(previous), 0600); err != nil {
			t.Fatal(err)
		}
		if err := adoptManual("2026-10-21", 3244, statePath); err == nil {
			t.Fatal("existing ownership or corrupt state accepted")
		}
		after, err := os.ReadFile(statePath)
		if err != nil || string(after) != previous {
			t.Fatal("state changed", string(after), err)
		}
	}
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if err := adoptManual("2026-10-21", 3244, statePath); err != nil {
		t.Fatal(err)
	}
	s, err := readState(statePath)
	if err != nil || s.Job != "" || s.Mode != "manual" || s.Todo != "3244" {
		t.Fatal(s, err)
	}
}

func TestHiddenRunKeepsWarningsOutOfTheReceipt(t *testing.T) {
	out, err := hiddenRun(os.Getenv("COMSPEC"), "/d", "/c", "echo test-job & echo harmless-warning 1>&2")
	if err != nil || out != "test-job" {
		t.Fatal(out, err)
	}
}
