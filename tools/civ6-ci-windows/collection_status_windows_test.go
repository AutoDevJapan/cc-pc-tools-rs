//go:build windows && amd64

package main

import (
	"path/filepath"
	"syscall"
	"testing"
)

func TestCollectionRequiresStoppedPIDZero(t *testing.T) {
	for _, tt := range []struct {
		name       string
		state, pid uint32
		ok         bool
	}{
		{"stopped", 1, 0, true},
		{"running", 4, 0, false},
		{"stopping", 3, 0, false},
		{"stopped-with-live-pid", 1, 42, false},
		{"running-with-pid", 4, 42, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := collectionServiceStatus(tt.state, tt.pid); (got == nil) != tt.ok {
				t.Fatalf("status accepted=%v, want %v", got == nil, tt.ok)
			}
		})
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
