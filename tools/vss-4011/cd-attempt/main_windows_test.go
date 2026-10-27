//go:build windows && amd64

package main

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func liveEmptyC() metadata {
	return metadata{Valid: true, Associations: 1, CC: true, Max: maxBytes, CFree: 300 << 30, DFree: 1 << 40, NTFS: true, DistinctNVMe: true, DReady: true, Provider: true, Protection: true, MinUnset: true}
}
func TestPermitRequiresPreservedLiveEmptyAssociation(t *testing.T) {
	if !permit(liveEmptyC()) {
		t.Fatal("valid empty C-to-C 50GiB should be eligible after coordinator exclusion")
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
			if permit(m) {
				t.Fatal("must preserve changed or unverified state without writing")
			}
		})
	}
}

func TestResizeReasonSurvivesWithoutIdentifiers(t *testing.T) {
	raw := []byte(`Error: The specified shadow copy storage association was not found. \\?\Volume{12345678-1234-5678-9abc-123456789abc}\`)
	x := resizeReasons(raw)
	if len(x) != 1 || x[0] != "association_not_found" {
		t.Fatal("refusal reason lost")
	}
	clean, e := base64.StdEncoding.DecodeString(resizeDiagnostic(raw))
	if e != nil || strings.Contains(string(clean), "12345678") || !strings.Contains(string(clean), "association was not found") {
		t.Fatal("diagnostic not preserved without identifiers")
	}
	long := resizeDiagnostic([]byte(strings.Repeat("x", 5000)))
	b, e := base64.StdEncoding.DecodeString(long)
	if e != nil || len(b) != 4096 {
		t.Fatal("private diagnostic not bounded")
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
	processDeadline = time.Now().Add(-time.Second)
	defer func() { processDeadline = time.Time{} }()
	r := process("not-a-real-vss-owned-child.exe", time.Second)
	if r.Started || !r.Timeout || r.WaitVerified {
		t.Fatal("expired overall deadline must not start a child")
	}
}
