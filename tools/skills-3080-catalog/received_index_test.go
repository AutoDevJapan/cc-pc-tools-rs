package main

import (
	"errors"
	"testing"
)

// Validate the extension before passing the historical part to its already
// accepted bounded validator. No source row is hidden by the historical filter.
func withoutReceived50Index(current sixmileCohortIndex, a ReceivedCohort) (sixmileCohortIndex, []ReceivedEntry, error) {
	entries, err := validateReceivedCohort(a, received50Spec)
	if err != nil {
		return current, nil, err
	}
	bad := func() (sixmileCohortIndex, []ReceivedEntry, error) {
		return current, nil, errors.New("received index: missing or inconsistent extension")
	}
	if len(current.Cohorts) != 6 {
		return bad()
	}
	c := current.Cohorts[5]
	n := a.Counts
	if c.ID != "received50" || c.Artifact != "skills-3080-sixmile-received50-evaluation.json" || c.Paths != n["candidates"] || c.Raw != n["primary_raw"] || c.Workflow != n["primary_workflow"] || c.Support != n["support"] || c.Saved != n["saved"] || c.Read != n["direct_raw_support"] || c.MaskedSupport != n["direct_masked_support"] || c.WorkflowSupport != n["full_workflow_support"] || c.Reused != n["same_byte_notice_reuse"]+n["ordered_notice_difference"] || c.Scan != n["scan"] || c.Partial != n["partial"] || c.Unsaved != n["unsaved"] {
		return bad()
	}
	current.Cohorts = current.Cohorts[:5]
	current.Paths -= c.Paths
	current.Raw -= c.Raw
	current.Workflow -= c.Workflow
	current.Masked -= c.Workflow - c.Raw
	current.NotRaw += c.Raw
	current.Unassessed += c.Paths
	current.Support -= c.Support
	current.Saved -= c.Saved
	current.Read -= c.Read
	current.MaskedSupport -= c.MaskedSupport
	current.WorkflowSupport -= c.WorkflowSupport
	current.Reused -= c.Reused
	current.Scan -= c.Scan
	current.Partial -= c.Partial
	current.Unsaved -= c.Unsaved
	return current, entries, nil
}

func TestReceivedIndexExtensionMustRetainAllCategories(t *testing.T) {
	for _, name := range []string{"valid", "missing", "raw_includes_masked", "reused_as_raw", "partial_as_workflow", "wrong_artifact"} {
		t.Run(name, func(t *testing.T) {
			var current sixmileCohortIndex
			var a ReceivedCohort
			loadPublicReceipt(t, "skills-3080-sixmile-evaluation-index.json", &current)
			loadPublicReceipt(t, "skills-3080-sixmile-received50-evaluation.json", &a)
			switch name {
			case "missing":
				current.Cohorts = current.Cohorts[:5]
			case "raw_includes_masked":
				current.Cohorts[5].Raw += 24
			case "reused_as_raw":
				current.Cohorts[5].Read += 18
				current.Cohorts[5].Reused -= 18
			case "partial_as_workflow":
				current.Cohorts[5].Partial--
				current.Cohorts[5].WorkflowSupport++
			case "wrong_artifact":
				current.Cohorts[5].Artifact = "another.json"
			}
			base, entries, err := withoutReceived50Index(current, a)
			if name == "valid" {
				if err != nil {
					t.Fatal(err)
				}
				if base.Paths != 168 || base.Raw != 153 || base.Workflow != 167 || len(entries) != 50 {
					t.Fatal("extension changed historical coverage")
				}
			} else if err == nil {
				t.Fatal("inconsistent extension accepted")
			}
		})
	}
}
