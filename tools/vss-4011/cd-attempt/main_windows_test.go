//go:build windows && amd64

package main

import "testing"

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
func TestChildOutputIsBounded(t *testing.T) {
	var b limited
	p := make([]byte, 70<<10)
	n, err := b.Write(p)
	if err != nil || n != len(p) || len(b.buf) != 64<<10 || !b.truncated {
		t.Fatal("bounded capture contract failed")
	}
}
