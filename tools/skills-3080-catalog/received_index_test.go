package main

import (
	"errors"
	"testing"
)

func loadRegisteredReceivedIndex(t *testing.T) []ReceivedCohort {
	t.Helper()
	cohorts := make([]ReceivedCohort, len(receivedRegistrations))
	for i, r := range receivedRegistrations {
		loadPublicReceipt(t, r.Artifact, &cohorts[i])
	}
	return cohorts
}

// Check all current extensions before projecting the accepted historical view.
// Unknown source rows cannot be hidden by the historical filter.
func withoutReceivedIndex(current sixmileCohortIndex, cohorts []ReceivedCohort) (sixmileCohortIndex, []ReceivedEntry, error) {
	bad := func() (sixmileCohortIndex, []ReceivedEntry, error) {
		return current, nil, errors.New("received index: missing or inconsistent extension")
	}
	if len(cohorts) != len(receivedRegistrations) || len(current.Cohorts) != 5+len(cohorts) {
		return bad()
	}
	entries := []ReceivedEntry{}
	ids, paths := map[string]bool{}, map[string]bool{}
	for i, a := range cohorts {
		r := receivedRegistrations[i]
		own, err := validateReceivedCohort(a, r.Spec)
		if err != nil {
			return current, nil, err
		}
		for _, e := range own {
			if ids[e.ID] || paths[e.Path] {
				return bad()
			}
			ids[e.ID], paths[e.Path] = true, true
			entries = append(entries, e)
		}
		c, n := current.Cohorts[5+i], a.Counts
		if c.ID != r.ID || c.Artifact != r.Artifact || c.Paths != n["candidates"] || c.Raw != n["primary_raw"] || c.Workflow != n["primary_workflow"] || c.Metadata != n["primary_saved_unread"] || c.PrimaryPartial != n["primary_partial"] || c.Support != n["support"] || c.Saved != n["saved"] || c.Read != n["direct_raw_support"] || c.MaskedSupport != n["direct_masked_support"] || c.WorkflowSupport != n["full_workflow_support"] || c.Reused != n["same_byte_notice_reuse"]+n["same_byte_support_reuse"]+n["ordered_notice_difference"] || c.Scan != n["scan"] || c.Partial != n["partial"] || c.SavedUnread != n["saved_unread"] || c.Unsaved != n["unsaved"] {
			return bad()
		}
		current.Paths -= c.Paths
		current.Raw -= c.Raw
		current.Workflow -= c.Workflow
		current.Masked -= c.Workflow - c.Raw
		current.Metadata -= c.Metadata
		current.PrimaryPartial -= c.PrimaryPartial
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
		current.SavedUnread -= c.SavedUnread
		current.Unsaved -= c.Unsaved
	}
	current.Cohorts = current.Cohorts[:5]
	if current.PrimaryPartial != 0 {
		return bad()
	}
	return current, entries, nil
}

func TestReceivedIndexExtensionsRetainAllCurrentCategories(t *testing.T) {
	for _, name := range []string{"valid", "missing", "extra_unregistered", "raw_includes_masked", "reused_as_raw", "partial_as_workflow", "unread_as_raw", "wrong_artifact"} {
		for slot := range receivedRegistrations {
			t.Run(receivedRegistrations[slot].ID+"/"+name, func(t *testing.T) {
				var current sixmileCohortIndex
				loadPublicReceipt(t, "skills-3080-sixmile-evaluation-index.json", &current)
				cohorts := loadRegisteredReceivedIndex(t)
				c := &current.Cohorts[5+slot]
				switch name {
				case "missing":
					current.Cohorts = current.Cohorts[:5]
				case "extra_unregistered":
					current.Cohorts = append(current.Cohorts, current.Cohorts[5])
				case "raw_includes_masked":
					c.Raw += cohorts[slot].Counts["primary_masked"]
				case "reused_as_raw":
					c.Read++
					c.Reused--
				case "partial_as_workflow":
					c.Partial--
					c.WorkflowSupport++
				case "unread_as_raw":
					c.SavedUnread--
					c.Read++
				case "wrong_artifact":
					c.Artifact = "another.json"
				}
				base, entries, err := withoutReceivedIndex(current, cohorts)
				if name == "valid" {
					if err != nil {
						t.Fatal(err)
					}
					wantEntries := 0
					for _, r := range receivedRegistrations {
						wantEntries += r.Spec.Counts["candidates"]
					}
					if base.Paths != 168 || base.Raw != 153 || base.Workflow != 167 || len(entries) != wantEntries {
						t.Fatal("registered extensions changed historical coverage")
					}
				} else if err == nil {
					t.Fatal("inconsistent extension accepted")
				}
			})
		}
	}
}
