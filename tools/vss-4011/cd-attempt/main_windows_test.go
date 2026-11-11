//go:build windows && amd64

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func liveEmptyC() metadata {
	return metadata{Valid: true, Associations: 1, CC: true, Max: maxBytes, CFree: 300 << 30, DFree: 1 << 40, NTFS: true, DistinctNVMe: true, DReady: true, Provider: true, Protection: true, MinUnset: true}
}
func TestEmptyObservationRequiresUnchangedAssociation(t *testing.T) {
	if !emptyObservationMatches(liveEmptyC()) {
		t.Fatal("valid empty C-to-C observation should match; this grants no write capability")
	}
	for _, tc := range []struct {
		name   string
		change func(*metadata)
	}{
		{"query_failed", func(m *metadata) { m.Valid = false }},
		{"new_C_snapshot", func(m *metadata) { m.ShadowCount = 1; m.CShadowCount = 1 }},
		{"other_snapshot", func(m *metadata) { m.ShadowCount = 1 }},
		{"restore_point", func(m *metadata) { m.PointCount = 1 }},
		{"other_association", func(m *metadata) { m.Associations = 2 }},
		{"already_on_D", func(m *metadata) { m.CC = false; m.CD = true }},
		{"maximum_changed", func(m *metadata) { m.Max = 60 << 30 }},
		{"remaining_allocation", func(m *metadata) { m.Allocated = 1 }},
		{"remaining_used", func(m *metadata) { m.Used = 1 }},
		{"low_C_free", func(m *metadata) { m.CFree = 199 << 30 }},
		{"not_ntfs", func(m *metadata) { m.NTFS = false }},
		{"D_not_ready", func(m *metadata) { m.DReady = false }},
		{"low_D_free", func(m *metadata) { m.DFree = 200 << 30 }},
		{"same_disk", func(m *metadata) { m.DistinctNVMe = false }},
		{"provider_changed", func(m *metadata) { m.Provider = false }},
		{"owner_disabled_protection", func(m *metadata) { m.Protection = false }},
		{"minimum_changed", func(m *metadata) { m.MinUnset = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := liveEmptyC()
			tc.change(&m)
			if emptyObservationMatches(m) {
				t.Fatal("must preserve changed or unverified state without writing")
			}
		})
	}
}

func TestStorageMutationFlagIsRejectedBeforeAnyOperation(t *testing.T) {
	for _, args := range [][]string{
		{"--attempt-cd", "preflight.ps1", "4011/os-v2"},
		{"--attempt-cd", "preflight.ps1"},
		{"--inspect-cd", ""}, nil,
	} {
		if _, err := inspectionScript(args); err == nil {
			t.Fatalf("unsupported arguments accepted: %q", args)
		}
	}
	if s, err := inspectionScript([]string{"--inspect-cd", "preflight.ps1"}); err != nil || s != "preflight.ps1" {
		t.Fatal("read-only inspection not accepted")
	}
	if s, err := inspectionScript([]string{"--contender"}); err != nil || s != "" {
		t.Fatal("read-only contender not accepted")
	}
}
func TestBlockedRequiresReleasedForeignObject(t *testing.T) {
	r := report{Phase: "start_unavailable_no_retry", Released: 1, Calls: []call{{"StartSnapshotSet", "0x80042316"}}}
	if !blocked(r) {
		t.Fatal("expected exclusion rejected")
	}
	r.Owned = 1
	if blocked(r) {
		t.Fatal("successful contender cannot prove exclusion")
	}
}
func TestChildOutputIsBounded(t *testing.T) {
	var b limited
	p := make([]byte, 70<<10)
	n, err := b.Write(p)
	if err != nil || n != len(p) || len(b.buf) != 64<<10 || !b.truncated {
		t.Fatal("bounded capture contract failed")
	}
}

func TestExpiredOverallDeadlineStartsNoChild(t *testing.T) {
	defer func() { processDeadline = time.Time{} }()
	cmd := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	if _, err := os.Stat(cmd); err != nil {
		t.Fatalf("real executable required to distinguish deadline from lookup failure: %v", err)
	}
	processDeadline = time.Time{}
	control := process(cmd, time.Second, "/c", "exit", "0")
	if !control.Started || !control.WaitVerified || control.Timeout || control.Exit != 0 {
		t.Fatal("positive control executable did not complete")
	}
	processDeadline = time.Now().Add(-time.Second)
	r := process(cmd, time.Second, "/c", "exit", "0")
	if r.Started || !r.Timeout || r.WaitVerified {
		t.Fatal("expired overall deadline must not start a child")
	}
}
