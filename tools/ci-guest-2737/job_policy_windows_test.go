package main

import (
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/normalci"
	"strings"
	"testing"
)

func pressFixturePin(workflow string) sourcepacket.Pin {
	p := sourcepacket.Pin{Product: sourcepacket.PressProduct, Workflow: workflow, HeadSHA: strings.Repeat("a", 40), TreeSHA: strings.Repeat("b", 40), WorkflowSHA256: strings.Repeat("c", 64)}
	if workflow != "local-checks.yml" {
		p.PullHeadSHA, p.MergeBaseSHA = strings.Repeat("d", 40), strings.Repeat("e", 40)
	}
	return p
}

func TestPressGuestPoliciesBindAllOriginalRolesWithoutCrossProductFallback(t *testing.T) {
	roles := map[string][]string{
		"local-checks.yml":            {"rust-core", "rust-integration-1", "rust-integration-2", "rust-integration-3", "rust-integration-4", "rust-field-read-signing", "rust-aux", "browser-package", "browser-cold-start", "local-gates"},
		"rust-v2-https.yml":           {"rust-v2-https-ubuntu-24.04", "rust-v2-https-windows-2022"},
		"windows-hidden-fixtures.yml": {"junction-fixtures"},
	}
	count := 0
	for workflow, items := range roles {
		for _, role := range items {
			t.Run(workflow+"/"+role, func(t *testing.T) {
				pin := pressFixturePin(workflow)
				instance := strings.Repeat("1", 32)
				policy, ok := jobPolicy(pin, role)
				if !ok {
					t.Fatal("original role omitted")
				}
				count++
				plan := linuxJobPlan{Source: pin, RunID: 7, JobID: 9, Attempt: 2, Jobs: 1, Threads: 1, Role: role, Name: "todo2737-" + instance, Label: jobLabel(pin, role, 7, 2)}
				if validLinuxJob(plan, pin, instance) != (policy.OS == "Linux") {
					t.Fatal("cross OS admission changed")
				}
				event, ref := jobEventRef(pin)
				profile, _ := pin.Profile()
				ctx := jobHookContext{Repository: profile.Repository, SHA: pin.HeadSHA, WorkflowSHA: pin.HeadSHA, Ref: ref, Event: event, RunID: "7", Attempt: "2", Job: policy.ID, Name: plan.Name}
				if validateJobContext(plan, ctx) != nil {
					t.Fatal("fixed execution hook rejected")
				}
				if pin.PullHeadSHA != "" {
					other := ctx
					other.SHA = pin.PullHeadSHA
					if validateJobContext(plan, other) == nil {
						t.Fatal("candidate SHA substituted for physical merge")
					}
				}
				q := linuxJobRequest{Source: pin, RunID: 7, Attempt: 2, Role: role, RouteLease: strings.Repeat("2", 32)}
				b := normalci.Binding{RepoID: 1386702183, PR: 175, RunID: 7, Attempt: 2, JobID: 9, SHA: pin.HeadSHA, Tree: pin.TreeSHA, WorkflowSHA: pin.WorkflowSHA256, Role: role, OS: policy.OS, Label: plan.Label, JobName: policy.Name, TimeoutMinutes: policy.TimeoutMinutes}
				if !validOfficialBinding(q, b) {
					t.Fatal("fixed binding refused")
				}
				b.RepoID = 1154066087
				if validOfficialBinding(q, b) {
					t.Fatal("MCP alpha binding reused for Press")
				}
				proof := sourcepacket.Receipt{RepoID: 1386702183, PR: 175, HeadSHA: pin.HeadSHA, TreeSHA: pin.TreeSHA, BlobCount: 1, PhysicalBlobsVerified: true}
				if !checkPhysicalProof(proof, q) {
					t.Fatal("Press physical proof refused")
				}
				proof.HeadSHA = "other"
				if checkPhysicalProof(proof, q) {
					t.Fatal("unbound proof accepted")
				}
				if _, ok := jobPolicy(pin, "verify"); ok {
					t.Fatal("MCP role selected for Press")
				}
				plan.Attempt = 3
				if validLinuxJob(plan, pin, instance) {
					t.Fatal("runner label reused across attempts")
				}
			})
		}
	}
	if count != 13 {
		t.Fatal("original role count changed")
	}
	for _, p := range []sourcepacket.Pin{
		{Product: "other", HeadSHA: strings.Repeat("a", 40), TreeSHA: strings.Repeat("b", 40), WorkflowSHA256: strings.Repeat("c", 64)},
		{Workflow: "local-checks.yml", HeadSHA: strings.Repeat("a", 40), TreeSHA: strings.Repeat("b", 40), WorkflowSHA256: strings.Repeat("c", 64)},
	} {
		if validSourcePin(p) {
			t.Fatal("unknown/partial profile admitted")
		}
	}
}
