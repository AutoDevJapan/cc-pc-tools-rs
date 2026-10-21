package windowsrunner

import (
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/normalci"
	"cc/product-ci/pressci"
	"strings"
	"testing"
	"time"
)

func TestPressWindowsSeedAndHooksRequireExactRoleMergeAndAttempt(t *testing.T) {
	for _, tc := range []struct{ workflow, role string }{{"rust-v2-https.yml", "rust-v2-https-windows-2022"}, {"windows-hidden-fixtures.yml", "junction-fixtures"}} {
		t.Run(tc.role, func(t *testing.T) {
			pin := sourcepacket.Pin{Product: sourcepacket.PressProduct, Workflow: tc.workflow, HeadSHA: strings.Repeat("a", 40), TreeSHA: strings.Repeat("b", 40), WorkflowSHA256: strings.Repeat("c", 64), PullHeadSHA: strings.Repeat("d", 40), MergeBaseSHA: strings.Repeat("e", 40)}
			j, _ := pressci.RoleJob(tc.workflow, tc.role)
			b := normalci.Binding{RepoID: 1386702183, PR: 175, RunID: 7, Attempt: 2, JobID: 9, SHA: pin.HeadSHA, Tree: pin.TreeSHA, WorkflowSHA: pin.WorkflowSHA256, Role: tc.role, OS: "Windows", Label: pressci.RuntimeLabel(tc.workflow, tc.role, 7, 2), JobName: j.Name, TimeoutMinutes: 90}
			s, e := NewSeed(pin, b, strings.Repeat("1", 32), strings.Repeat("2", 64), 1, 1)
			if e != nil || s.Plan.timeoutMinutes() != 90 {
				t.Fatalf("Press seed refused: %v", e)
			}
			if s.Plan.sessionTimeout() != 96*time.Minute {
				t.Fatal("Windows session truncates original timeout")
			}
			bad := b
			bad.JobName = "other"
			if ValidateBinding(s.Plan, bad) == nil {
				t.Fatal("changed official job name accepted")
			}
			c := HookContext{Repository: "AutoDevJapan/GoDD-Press", SHA: pin.HeadSHA, WorkflowSHA: pin.HeadSHA, Ref: "refs/pull/175/merge", Event: "pull_request", Role: j.ID, Name: s.Plan.Name, RunID: 7, Attempt: 2}
			if c.Validate(s.Plan) != nil {
				t.Fatal("actual merge hook refused")
			}
			c.SHA = pin.PullHeadSHA
			if c.Validate(s.Plan) == nil {
				t.Fatal("candidate relabeled as merge")
			}
			p := s.Plan
			p.Attempt = 3
			if p.Validate() == nil {
				t.Fatal("old attempt label reused")
			}
			p = s.Plan
			p.Role = Role
			if p.Validate() == nil {
				t.Fatal("MCP role admitted in Press seed")
			}
			p = s.Plan
			p.Source.Product = "other"
			if p.Validate() == nil {
				t.Fatal("unknown product admitted")
			}
			p = s.Plan
			p.Source.PullHeadSHA = ""
			if p.Validate() == nil {
				t.Fatal("PR execution lost merge source")
			}
			proof := sourcepacket.Receipt{RepoID: 1386702183, PR: 175, HeadSHA: pin.HeadSHA, TreeSHA: pin.TreeSHA, BlobCount: 1, PhysicalBlobsVerified: true}
			if !validProof(s.Plan, proof) {
				t.Fatal("physical Press proof refused")
			}
			proof.RepoID = 1154066087
			if validProof(s.Plan, proof) {
				t.Fatal("MCP proof reused")
			}
		})
	}
	if testSeed().Plan.sessionTimeout() != 26*time.Minute {
		t.Fatal("alpha session timeout changed")
	}
}
