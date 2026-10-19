//go:build windows && amd64

package main

import (
	"errors"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCollectionRequiresStoppedPIDZeroAndNeverStartedAbort(t *testing.T) {
	for _, tt := range []struct {
		name             string
		state, pid, code uint32
		unstarted, ok    bool
	}{
		{"fresh-never-started", 1, 0, 1077, true, true},
		{"normal-exit-is-not-never-started", 1, 0, 0, true, false},
		{"failed-exit-is-not-never-started", 1, 0, 5, true, false},
		{"running-with-never-started-code", 4, 0, 1077, true, false},
		{"stopped-with-live-pid", 1, 42, 1077, true, false},
		{"ordinary-success", 1, 0, 0, false, true},
		{"ordinary-failure-still-needs-launch-report", 1, 0, 1, false, true},
		{"ordinary-running", 4, 42, 0, false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := collectionServiceStatus(tt.state, tt.pid, tt.code, tt.unstarted); (got == nil) != tt.ok {
				t.Fatalf("status accepted=%v, want %v", got == nil, tt.ok)
			}
		})
	}
}

func TestCollectionDeleteBOOLAndLastErrorAreIndependent(t *testing.T) {
	if err := collectionDeleteResult(1, syscall.Errno(5)); err != nil {
		t.Fatal("successful BOOL rejected because of stale last error", err)
	}
	for _, err := range []error{nil, syscall.Errno(0)} {
		if collectionDeleteResult(0, err) == nil {
			t.Fatal("failed BOOL without a meaningful last error was accepted")
		}
	}
	if err := collectionDeleteResult(0, syscall.Errno(5)); !errors.Is(err, syscall.Errno(5)) {
		t.Fatal("actual Win32 failure was lost", err)
	}
}

func TestCollectionJournalsPreventFailedUncertainAndCompletedReplay(t *testing.T) {
	for _, prior := range []string{"failed", "uncertain", "completed"} {
		t.Run(prior, func(t *testing.T) {
			p := fixturePlan()
			p.Root = t.TempDir()
			calls := 0
			action := func() error {
				calls++
				if prior == "failed" {
					return syscall.Errno(1332)
				}
				return nil
			}
			if prior == "uncertain" {
				if err := saveNew(filepath.Join(p.Root, "step-attempt.json"), map[string]bool{"single_attempt": true}); err != nil {
					t.Fatal(err)
				}
			} else {
				first := collectionAction(p, "step", action)
				if (first == nil) != (prior == "completed") {
					t.Fatal("first action result was not propagated", first)
				}
			}
			err := collectionAction(p, "step", action)
			if (err == nil) != (prior == "completed") {
				t.Fatal("saved action result was not preserved", err)
			}
			want := 1
			if prior == "uncertain" {
				want = 0
			}
			if calls != want {
				t.Fatalf("action replayed: calls=%d, want %d", calls, want)
			}
		})
	}
}
