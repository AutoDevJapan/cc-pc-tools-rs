package main

import (
	"errors"
	"testing"
)

const comparedOnlyMCPID = "github:9a4792733ddd9ccbd3883f80adaa44d2f3214af2c971850d106896cf0238c55f"

type mcpComparedOccurrence struct {
	ID               string `json:"inventory_id"`
	Source           string `json:"source"`
	Commit           string `json:"commit"`
	Path             string `json:"path"`
	SHA              string `json:"source_sha256"`
	Blob             string `json:"git_blob"`
	Bytes            int    `json:"bytes"`
	Canonical        string `json:"canonical_inventory_id"`
	Complete         *bool  `json:"comparison_only_complete"`
	RawAdded         *int   `json:"raw_primary_read_added"`
	WorkflowAdded    *int   `json:"workflow_primary_read_added"`
	BodyEqual        *bool  `json:"body_bytes_equal_canonical"`
	SupportEqual     *bool  `json:"support_identity_equal_canonical"`
	AddedSupport     *int   `json:"candidate_only_support_paths"`
	PackagePaths     int    `json:"package_blob_paths"`
	SupportPaths     int    `json:"support_paths_excluding_publication_identity"`
	PublicationPaths int    `json:"publication_identity_paths"`
	Notice           *bool  `json:"individual_notice_present"`
	Copy             *bool  `json:"copy_license_accepted"`
	Copied           *bool  `json:"new_body_copied"`
	Executed         *bool  `json:"source_scripts_executed"`
	Installed        *bool  `json:"installed_by_todo3080"`
	Integrated       *bool  `json:"integrated_by_todo3080"`
	Excluded         *bool  `json:"excluded_by_todo3080"`
}
type mcpComparisonOnly struct {
	Entries []mcpComparedOccurrence `json:"comparison_only_occurrences"`
}

func validateMCPComparisonOnly(a mcpComparisonOnly, old legacy21Projection) error {
	if len(a.Entries) != 1 {
		return errors.New("comparison-only source: fixed occurrence missing or duplicated")
	}
	r := a.Entries[0]
	var canonical extra45Entry
	for _, e := range old.Entries {
		if e.ID == r.Canonical {
			canonical = e
		}
	}
	if r.ID != comparedOnlyMCPID || r.Source != old.Source || r.Commit != old.Commit || r.Path != "skills/uniquevme/unique-mcp-builder-test/SKILL.md" || r.SHA != "0f4592dcb53cf2b5d6b7febee6b4152018b565551a1c29e3c612f57b218ab295" || r.Blob != "8a1a77a47d141967b246adb4da4f91037578ff7d" || r.Bytes != 9092 || canonical.ID != "github:e4ade76a7932447efe3dd6e777ab300a5a9a6f1cf13a392614b31c621906a016" || !x45True(canonical.Read) || canonical.SHA != r.SHA || canonical.Blob != r.Blob || canonical.Bytes != r.Bytes {
		return errors.New("comparison-only source: exact raw identity or fully read canonical differs")
	}
	if !x45True(r.Complete) || !x45True(r.BodyEqual) || !x45True(r.SupportEqual) || r.PackagePaths != 10 || r.SupportPaths != 8 || r.PublicationPaths != 1 || !x45False(r.Notice) || r.AddedSupport == nil || *r.AddedSupport != 0 {
		return errors.New("comparison-only source: package scope is unknown or has an unaccounted delta")
	}
	if r.RawAdded == nil || *r.RawAdded != 0 || r.WorkflowAdded == nil || *r.WorkflowAdded != 0 || !x45False(r.Copy) || !x45False(r.Copied) || !x45False(r.Executed) {
		return errors.New("comparison-only source: byte comparison became a new reading, grant or execution")
	}
	if !x45False(r.Installed) || !x45False(r.Integrated) || !x45True(r.Excluded) {
		return errors.New("comparison-only source: occurrence disposition differs")
	}
	return nil
}
func comparisonEntriesForUnion(a mcpComparisonOnly) []extra45Entry {
	var entries []extra45Entry
	for _, r := range a.Entries {
		entries = append(entries, extra45Entry{ID: r.ID, Path: r.Path, SHA: r.SHA, Blob: r.Blob, Bytes: r.Bytes, Commit: r.Commit, Excluded: x45True(r.Excluded)})
	}
	return entries
}
func TestComparisonOnlyOccurrenceIsNotAnInventedRawReadOrGrant(t *testing.T) {
	for _, name := range []string{"valid", "raw_read_claimed", "workflow_read_claimed", "notice_invented", "grant_claimed", "unread_canonical", "support_delta", "wrong_identity"} {
		t.Run(name, func(t *testing.T) {
			var a mcpComparisonOnly
			var old legacy21Projection
			loadPublicReceipt(t, "skills-3080-mcp-sibling-duplicate-evidence.json", &a)
			loadPublicReceipt(t, "skills-3080-sixmile-partial-evaluation.json", &old)
			if len(a.Entries) != 1 {
				t.Fatal("fixed comparison fixture absent")
			}
			r := &a.Entries[0]
			want := ""
			yes := true
			one := 1
			switch name {
			case "raw_read_claimed":
				r.RawAdded = &one
				want = "comparison-only source: byte comparison became a new reading, grant or execution"
			case "workflow_read_claimed":
				r.WorkflowAdded = &one
				want = "comparison-only source: byte comparison became a new reading, grant or execution"
			case "notice_invented":
				r.Notice = &yes
				want = "comparison-only source: package scope is unknown or has an unaccounted delta"
			case "grant_claimed":
				r.Copy = &yes
				want = "comparison-only source: byte comparison became a new reading, grant or execution"
			case "unread_canonical":
				for i := range old.Entries {
					if old.Entries[i].ID == r.Canonical {
						no := false
						old.Entries[i].Read = &no
					}
				}
				want = "comparison-only source: exact raw identity or fully read canonical differs"
			case "support_delta":
				r.AddedSupport = &one
				want = "comparison-only source: package scope is unknown or has an unaccounted delta"
			case "wrong_identity":
				r.SHA = "wrong"
				want = "comparison-only source: exact raw identity or fully read canonical differs"
			}
			err := validateMCPComparisonOnly(a, old)
			if want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != want {
				t.Fatalf("%s reached another guard: %v", name, err)
			}
		})
	}
}

func TestComparisonOnlyProjectionDeclaresItsSeparateReadingAndSupportScope(t *testing.T) {
	var index struct {
		Comparison  int    `json:"comparison_only_primary"`
		SupportKind string `json:"support_counting_kind"`
		Evidence    struct {
			Artifact string `json:"artifact"`
			Paths    int    `json:"candidate_paths"`
			Raw      int    `json:"raw_primary_read_added"`
			Workflow int    `json:"workflow_primary_read_added"`
			Claims   int    `json:"source_reading_claims_added"`
		} `json:"comparison_only_evaluation"`
	}
	var progress struct {
		Comparison struct {
			Source   string `json:"source"`
			Paths    int    `json:"candidate_paths"`
			Raw      int    `json:"new_raw_readings"`
			Workflow int    `json:"new_workflow_readings"`
			Excluded int    `json:"excluded"`
			Copied   *bool  `json:"new_body_copied"`
			Executed *bool  `json:"source_scripts_executed"`
		} `json:"sixmile_comparison_only_evaluation"`
	}
	loadPublicReceipt(t, "skills-3080-sixmile-evaluation-index.json", &index)
	loadPublicReceipt(t, "skills-3080-progress.json", &progress)
	i, p := index.Evidence, progress.Comparison
	if index.Comparison != 1 || index.SupportKind != "direct_reading_cohorts_only" || i.Artifact != "skills-3080-mcp-sibling-duplicate-evidence.json" || i.Paths != 1 || i.Raw != 0 || i.Workflow != 0 || i.Claims != 0 || p.Source != i.Artifact || p.Paths != 1 || p.Raw != 0 || p.Workflow != 0 || p.Excluded != 1 || !x45False(p.Copied) || !x45False(p.Executed) {
		t.Fatal("comparison-only projection became direct reading/support totals or an unqualified operation")
	}
}
