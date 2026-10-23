package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestUncreatedRecoveryRejectsCreatedOrUnboundFlights(t *testing.T) {
	id := strings.Repeat("a", 32)
	root := filepath.Join(t.TempDir(), "job-"+id)
	q := linuxJobRequest{Source: fixedSourcePin(), RunID: 17, Attempt: 1, Role: "ci-gate", RouteLease: strings.Repeat("b", 32)}
	i := uncreatedIntent{Instance: id, Attempt: root, RunID: q.RunID, Source: q.Source}
	v := vmState{Instance: id, Name: "cc-ci-2737-" + id, Root: root, Pipe: `\\.\pipe\cc-ci-2737-` + id}
	j := v
	j.Stage = "intent"
	c := uncreatedCleanup{RegistrationGone: true}
	if e := validateUncreatedRecovery(root, q, i, v, j, c); e != nil {
		t.Fatal("uncreated positive control rejected", e)
	}
	for _, tc := range []struct {
		name string
		edit func(*linuxJobRequest, *uncreatedIntent, *vmState, *vmState, *uncreatedCleanup)
	}{
		{"other-run", func(q *linuxJobRequest, _ *uncreatedIntent, _, _ *vmState, _ *uncreatedCleanup) { q.RunID++ }},
		{"windows-job", func(q *linuxJobRequest, _ *uncreatedIntent, _, _ *vmState, _ *uncreatedCleanup) {
			q.Role = "windows-native-check"
		}},
		{"other-source", func(_ *linuxJobRequest, i *uncreatedIntent, _, _ *vmState, _ *uncreatedCleanup) {
			i.Source.HeadSHA = strings.Repeat("f", 40)
		}},
		{"other-path", func(_ *linuxJobRequest, i *uncreatedIntent, _, _ *vmState, _ *uncreatedCleanup) {
			i.Attempt = filepath.Dir(i.Attempt)
		}},
		{"intent-vm-id", func(_ *linuxJobRequest, _ *uncreatedIntent, v, _ *vmState, _ *uncreatedCleanup) {
			v.ID = strings.Repeat("c", 36)
		}},
		{"journal-vm-id", func(_ *linuxJobRequest, _ *uncreatedIntent, _, j *vmState, _ *uncreatedCleanup) {
			j.ID = strings.Repeat("c", 36)
		}},
		{"created-journal", func(_ *linuxJobRequest, _ *uncreatedIntent, _, j *vmState, _ *uncreatedCleanup) { j.Stage = "created" }},
		{"registration-unknown", func(_ *linuxJobRequest, _ *uncreatedIntent, _, _ *vmState, c *uncreatedCleanup) {
			c.RegistrationGone = false
		}},
		{"registration-started", func(_ *linuxJobRequest, _ *uncreatedIntent, _, _ *vmState, c *uncreatedCleanup) {
			c.RegistrationMayExist = true
		}},
		{"runner-created", func(_ *linuxJobRequest, _ *uncreatedIntent, _, _ *vmState, c *uncreatedCleanup) { c.RunnerID = 1 }},
		{"successful-job", func(_ *linuxJobRequest, _ *uncreatedIntent, _, _ *vmState, c *uncreatedCleanup) {
			c.OfficialCISuccess = true
		}},
		{"already-clean", func(_ *linuxJobRequest, _ *uncreatedIntent, _, _ *vmState, c *uncreatedCleanup) { c.VMGone = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			qq, ii, vv, jj, cc := q, i, v, j, c
			tc.edit(&qq, &ii, &vv, &jj, &cc)
			if validateUncreatedRecovery(root, qq, ii, vv, jj, cc) == nil {
				t.Fatal("substituted or possibly created flight accepted")
			}
		})
	}
}

func TestRecoveryJSONRequiresCompleteKnownReceipt(t *testing.T) {
	var v vmState
	if e := decodeRecoveryJSON([]byte(`{"Stage":"intent"}`+strings.Repeat(" ", 65000)), &v); e != nil || v.Stage != "intent" {
		t.Fatal("fixed-size journal positive control rejected", e)
	}
	for _, raw := range []string{`{"Stage":"intent"} {}`, `{"Stage":"intent","Unexpected":true}`, `{"Stage":`, strings.Repeat(" ", 65537)} {
		if decodeRecoveryJSON([]byte(raw), &v) == nil {
			t.Fatal("incomplete or substituted receipt accepted")
		}
	}
}
