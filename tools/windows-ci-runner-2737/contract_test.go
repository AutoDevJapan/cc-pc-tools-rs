package windowsrunner

import (
	"bytes"
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/normalci"
	"encoding/binary"
	"strings"
	"testing"
)

func testSeed() Seed {
	return Seed{Plan: Plan{Source: sourcepacket.Pin{HeadSHA: strings.Repeat("a", 40), TreeSHA: strings.Repeat("b", 40), WorkflowSHA256: strings.Repeat("c", 64)}, Instance: strings.Repeat("d", 32), RunID: 123, JobID: 456, Attempt: 2, Jobs: 3, Threads: 4, Role: Role, Name: "todo2737-" + strings.Repeat("d", 32), Label: Label(123)}, ClientSHA256: strings.Repeat("e", 64)}
}
func binding(p Plan) normalci.Binding {
	return normalci.Binding{RepoID: 1154066087, PR: 3613, RunID: p.RunID, JobID: p.JobID, Attempt: p.Attempt, TimeoutMinutes: 20, SHA: p.Source.HeadSHA, Tree: p.Source.TreeSHA, WorkflowSHA: p.Source.WorkflowSHA256, Role: Role, Label: p.Label, OS: "Windows", JobName: Role}
}
func hookContext(p Plan) HookContext {
	return HookContext{Repository: Repository, SHA: p.Source.HeadSHA, Ref: "refs/heads/" + sourcepacket.HeadRef, Event: "workflow_dispatch", Role: Role, Name: p.Name, WorkflowSHA: p.Source.HeadSHA, RunID: p.RunID, Attempt: p.Attempt}
}
func receipt(p Plan) sourcepacket.Receipt {
	return sourcepacket.Receipt{RepoID: 1154066087, PR: 3613, HeadSHA: p.Source.HeadSHA, TreeSHA: p.Source.TreeSHA, BlobCount: 2, PhysicalBlobsVerified: true}
}

func TestPlanAndBindingRejectWrongRunRoleSourceAndLegacyLabel(t *testing.T) {
	s := testSeed()
	if s.Validate() != nil {
		t.Fatal("valid plan rejected")
	}
	for name, change := range map[string]func(*Plan){
		"run": func(p *Plan) { p.RunID++ }, "role": func(p *Plan) { p.Role = "verify" }, "legacy": func(p *Plan) { p.Label = "cc-product-2737-1154066087-windows" }, "runner": func(p *Plan) { p.RunnerID = 1 }, "inputs": func(p *Plan) { p.Inputs = map[string]string{"x": "y"} }, "nonce": func(p *Plan) { p.Instance = strings.Repeat("A", 32) }, "jobs": func(p *Plan) { p.Jobs = 11 }, "threads": func(p *Plan) { p.Threads = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			p := s.Plan
			change(&p)
			if p.Validate() == nil {
				t.Fatal("bad plan accepted")
			}
		})
	}
	b := binding(s.Plan)
	if _, e := NewSeed(s.Plan.Source, b, s.Plan.Instance, s.ClientSHA256, 3, 4); e != nil {
		t.Fatal(e)
	}
	b.SHA = strings.Repeat("f", 40)
	if ValidateBinding(s.Plan, b) == nil {
		t.Fatal("different source binding accepted")
	}
	b = binding(s.Plan)
	b.Label = Label(999)
	if ValidateBinding(s.Plan, b) == nil {
		t.Fatal("cross-run binding accepted")
	}
	c := hookContext(s.Plan)
	if c.Validate(s.Plan) != nil {
		t.Fatal("valid context rejected")
	}
	c.Attempt++
	if c.Validate(s.Plan) == nil {
		t.Fatal("wrong attempt accepted")
	}
}
func TestFrameRejectsOversizeDuplicateUnknownAndTruncated(t *testing.T) {
	for _, raw := range []string{`{"Cancel":true,"cancel":false}`, `{"Ack":{},"unexpected":1}`, `{} {}`, `{"Token":`} {
		var m message
		if decode([]byte(raw), &m) == nil {
			t.Fatal("malformed control accepted")
		}
	}
	var b bytes.Buffer
	binary.Write(&b, binary.BigEndian, uint32(maxControl+1))
	if _, e := readFrame(&b, maxControl); e == nil {
		t.Fatal("oversized allocation accepted")
	}
	b.Reset()
	binary.Write(&b, binary.BigEndian, uint32(3))
	b.WriteByte(1)
	if _, e := readFrame(&b, maxControl); e == nil {
		t.Fatal("truncated frame accepted")
	}
}
