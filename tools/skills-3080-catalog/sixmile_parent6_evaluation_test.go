package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

var parent6ReadingSpec = receivedReadingSpec{
	"parent6", "1e5eb71680aad5f4e1f7c99270e0adbf600c2697dfb749ce2ec205ef65e3e986",
	"b185d57f222d12fc109bfcbad9e23a5c045e8f8e0a7b75914c424c74770f6066",
	"2c1ef9ff661f322a8fccc825378b1b71f26fb6deda78569ce67ffae4e7c3e729",
	6, 5, 6, 1, 0, 57, 27, 14, 17, 3, 0, 30,
}

const threeCohortSnapshotBlob = "3c9b26478a118f1e43e8896a3f77d9741b9fbf81"
const amendedPrimaryID = "github:2e24172100594e6036b2cc464d7bda679bf008c6bbb8cd128e5a92262c691339"

func loadThreeCohortSnapshot(t *testing.T) sixmileCohortIndex {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "docs", "skills-3080-sixmile-three-cohort-snapshot.json"))
	if err != nil {
		t.Fatal(err)
	}
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(b))
	h.Write(b)
	if hex.EncodeToString(h.Sum(nil)) != threeCohortSnapshotBlob {
		t.Fatal("historical three-cohort index differs from accepted ad8151c Git blob")
	}
	var snapshot sixmileCohortIndex
	if err = json.Unmarshal(b, &snapshot); err != nil {
		t.Fatal(err)
	}
	return snapshot
}

type primaryReadingAmendments struct {
	Source  string `json:"source"`
	Commit  string `json:"commit"`
	Tree    string `json:"tree_sha"`
	Entries []struct {
		ID       string `json:"inventory_id"`
		Source   string `json:"source"`
		Commit   string `json:"commit"`
		Path     string `json:"path"`
		SHA      string `json:"source_sha256"`
		Blob     string `json:"git_blob"`
		Bytes    int    `json:"bytes"`
		URL      string `json:"source_url"`
		Original struct {
			Cohort     string  `json:"cohort"`
			Artifact   string  `json:"artifact"`
			Read       *bool   `json:"body_read_complete"`
			Workflow   *bool   `json:"body_workflow_read_complete"`
			ReadBy     *string `json:"body_read_by"`
			WorkflowBy *string `json:"body_workflow_read_by"`
			Mask       *int    `json:"masked_value_count"`
		} `json:"original_reading"`
		New struct {
			Read       *bool   `json:"body_read_complete"`
			Workflow   *bool   `json:"body_workflow_read_complete"`
			ReadBy     *string `json:"body_read_by"`
			WorkflowBy *string `json:"body_workflow_read_by"`
			Mask       *int    `json:"masked_value_count"`
			Observed   string  `json:"read_observed_at"`
		} `json:"new_reading"`
		NoticeSHA              string `json:"notice_sha256"`
		NoticeBytes            int    `json:"notice_bytes"`
		Copy                   *bool  `json:"copy_license_accepted"`
		Executed               *bool  `json:"source_code_executed"`
		NewCandidate           *bool  `json:"new_candidate_path"`
		OriginalUnchanged      *bool  `json:"original_reading_unchanged"`
		SupportActorsUnchanged *bool  `json:"support_reading_actors_unchanged"`
	} `json:"entries"`
	Counts struct {
		Amendments int `json:"amendments"`
		Paths      int `json:"new_candidate_paths"`
		Raw        int `json:"raw_primary_added"`
		Workflow   int `json:"workflow_primary_added"`
		Metadata   int `json:"metadata_only_resolved"`
		Masked     int `json:"masked_workflow_resolved"`
		Support    int `json:"new_support_paths"`
	} `json:"counts"`
}

func validatePrimaryReadingAmendment(a primaryReadingAmendments, old legacy21Projection, extra extra45Assessment) error {
	if a.Source != extra.Source || a.Commit != extra.Commit || a.Tree != extra.Tree || len(a.Entries) != 1 || a.Counts.Amendments != 1 || a.Counts.Paths != 0 || a.Counts.Raw != 1 || a.Counts.Workflow != 1 || a.Counts.Metadata != 1 || a.Counts.Masked != 0 || a.Counts.Support != 0 {
		return errors.New("reading amendment: fixed scope or category deltas differ")
	}
	r := a.Entries[0]
	var prior *extra45Entry
	for i := range extra.Entries {
		if extra.Entries[i].ID == r.ID {
			prior = &extra.Entries[i]
		}
	}
	if prior == nil || r.ID != amendedPrimaryID || r.Source != extra.Source || r.Commit != extra.Commit || r.Path != prior.Path || r.SHA != prior.SHA || r.Blob != prior.Blob || r.Bytes != prior.Bytes || r.Bytes != 7068 || r.URL != prior.URL || r.NoticeSHA != prior.Rights.SHA || r.NoticeBytes != prior.Rights.Bytes || r.NoticeSHA != "79f6d8f5b427252fa3b1c11ecdbdb6bf610b944f7530b4de78f770f38741cfaa" {
		return errors.New("reading amendment: fixed original primary identity differs")
	}
	if r.Original.Cohort != "next45" || r.Original.Artifact != "skills-3080-sixmile-next45-evaluation.json" || !x45False(prior.Read) || !x45False(prior.Workflow) || prior.ReadBy != nil || prior.WorkflowBy != nil || prior.Mask != nil || !x45False(r.Original.Read) || !x45False(r.Original.Workflow) || r.Original.ReadBy != nil || r.Original.WorkflowBy != nil || r.Original.Mask != nil || !x45True(r.OriginalUnchanged) || !x45True(r.SupportActorsUnchanged) {
		return errors.New("reading amendment: historical unread state or support actors were rewritten")
	}
	if !x45True(r.New.Read) || !x45True(r.New.Workflow) || r.New.Mask == nil || *r.New.Mask != 0 || x45String(r.New.ReadBy) != "20261024-123649-todo-3080" || r.New.WorkflowBy != nil || !x45False(r.Copy) || !x45False(r.Executed) || !x45False(r.NewCandidate) {
		return errors.New("reading amendment: parent raw actor, grant or execution state differs")
	}
	if _, err := time.Parse(time.RFC3339Nano, r.New.Observed); err != nil {
		return errors.New("reading amendment: observed timestamp is absent or not a timestamp")
	}
	for _, e := range old.Entries {
		if e.ID == r.ID || e.Path == r.Path {
			return errors.New("reading amendment: already-read historical primary counted again")
		}
	}
	return nil
}

// Historical tests filter only for their bounded cohort; the current union below
// checks every live source row before this filter can discard anything.
func oldThreeCohortRows(old legacy21Projection, extra, next20 extra45Assessment, rows []cohortLedgerRow) []cohortLedgerRow {
	known := map[string]bool{}
	for _, group := range [][]extra45Entry{old.Entries, extra.Entries, next20.Entries} {
		for _, e := range group {
			known[e.ID] = true
		}
	}
	var selected []cohortLedgerRow
	for _, r := range rows {
		if r.Source != "6mile/skills" || known[r.ID] {
			selected = append(selected, r)
		}
	}
	return selected
}

func validateParent6Dispositions(a extra45Assessment, rows []cohortLedgerRow) error {
	known := map[string]extra45Entry{}
	matched := map[string]bool{}
	for _, e := range a.Entries {
		known[e.ID] = e
	}
	installed, integrated, excluded := 0, 0, 0
	for _, r := range rows {
		e, ok := known[r.ID]
		if !ok {
			continue
		}
		if matched[r.ID] || r.Source != a.Source || r.Commit != e.Commit || r.Path != e.Path || r.SHA != e.SHA {
			return errors.New("parent6 dispositions: identity differs")
		}
		matched[r.ID] = true
		switch r.Disposition {
		case "installed_new_adaptation":
			if !e.Installed || e.Integrated || e.Excluded {
				return errors.New("parent6 dispositions: installation flag differs")
			}
			installed++
		case "integrated_existing":
			if !e.Integrated || e.Installed || e.Excluded {
				return errors.New("parent6 dispositions: integration flag differs")
			}
			integrated++
		case "excluded":
			if !e.Excluded || e.Installed || e.Integrated {
				return errors.New("parent6 dispositions: exclusion flag differs")
			}
			excluded++
		default:
			return errors.New("parent6 dispositions: unfinished decision counted as final")
		}
	}
	for _, e := range a.Entries {
		if (e.Installed || e.Integrated || e.Excluded) && !matched[e.ID] {
			return errors.New("parent6 dispositions: final flag lacks a live ledger row")
		}
	}
	if installed != a.Installed || integrated != a.Integrated || excluded != a.Excluded {
		return errors.New("parent6 dispositions: counters differ from live ledger")
	}
	return nil
}

func validateExpandedSixmileIndex(current, snapshot sixmileCohortIndex, old legacy21Projection, extra, next20, parent extra45Assessment, next75 next75Assessment, amendment primaryReadingAmendments, comparison mcpComparisonOnly, rows []cohortLedgerRow, received []ReceivedCohort, accepted map[string]cohortLedgerRow) error {
	base, receivedEntries, err := withoutReceivedIndex(current, received)
	if err != nil {
		return err
	}
	current = base
	if err := validateNext75Reading(next75); err != nil {
		return err
	}
	if err := validateReceivedReading(parent, parent6ReadingSpec); err != nil {
		return err
	}
	if err := validateMCPComparisonOnly(comparison, old); err != nil {
		return err
	}
	if len(current.Cohorts) != 5 || len(snapshot.Cohorts) != 3 || current.Cohorts[3].ID != "parent6" || current.Cohorts[3].Artifact != "skills-3080-sixmile-parent6-evaluation.json" || current.Cohorts[4].ID != "next75" || current.Cohorts[4].Artifact != "skills-3080-sixmile-next75-evaluation.json" || !reflect.DeepEqual(current.Cohorts[:3], snapshot.Cohorts) {
		return errors.New("expanded sixmile index: current cohorts do not include exact parent6 and next75 extensions")
	}
	known := map[string]extra45Entry{}
	paths := map[string]bool{}
	for _, group := range [][]extra45Entry{old.Entries, extra.Entries, next20.Entries, parent.Entries, next75PrimaryProjection(next75), comparisonEntriesForUnion(comparison)} {
		for _, e := range group {
			if e.ID == "" || known[e.ID].ID != "" || paths[e.Path] {
				return errors.New("expanded sixmile index: assessed identity crosses cohorts")
			}
			known[e.ID] = e
			paths[e.Path] = true
		}
	}
	matched := map[string]bool{}
	for _, e := range receivedEntries {
		if known[e.ID].ID != "" || paths[e.Path] {
			return errors.New("expanded sixmile index: received identity crosses cohorts")
		}
		for _, r := range rows {
			if r.ID == e.ID {
				// The received record remains immutable and does not acquire a
				// permission or final flag. A separately validated actual creation,
				// scoped notice, review, discovery and use receipt supplies this
				// exact current decision, never all equal-body siblings.
				a, ok := accepted[e.ID]
				if !ok || a != r || r.Source != e.Source || r.Commit != e.Commit || r.Path != e.Path || r.SHA != e.SHA || r.Disposition != "installed_new_adaptation" {
					return errors.New("expanded sixmile index: immutable received reading lacks a current final acceptance")
				}
				known[e.ID] = extra45Entry{ID: e.ID, Commit: e.Commit, Path: e.Path, SHA: e.SHA}
				paths[e.Path] = true
			}
		}
	}
	// A later actual placement may come from a separately received primary
	// cohort. Its independent full-review/Create/current-buffer/discovery/use
	// acceptance is required; never rewrite the historic reading index.
	for id, r := range accepted {
		if _, exists := known[id]; exists || r.Source != "6mile/skills" {
			continue
		}
		if id != r.ID || paths[r.Path] || r.Disposition != "installed_new_adaptation" || r.Commit != current.Commit || r.Path == "" || r.SHA == "" {
			return errors.New("expanded sixmile index: separate current acceptance identity differs")
		}
		known[id] = extra45Entry{ID: id, Commit: r.Commit, Path: r.Path, SHA: r.SHA}
		paths[r.Path] = true
	}
	for _, r := range rows {
		e, ok := known[r.ID]
		if r.Source != "6mile/skills" {
			if ok {
				return errors.New("expanded sixmile index: selected source identity was reassigned")
			}
			continue
		}
		if !ok || matched[r.ID] || r.Commit != e.Commit || r.Path != e.Path || r.SHA != e.SHA {
			return errors.New("expanded sixmile index: live source row is unknown, repeated or mismatched")
		}
		matched[r.ID] = true
	}
	if err := validateSixmileCohortIndex(snapshot, old, extra, next20, oldThreeCohortRows(old, extra, next20, rows)); err != nil {
		return err
	}
	if err := validateParent6Dispositions(parent, rows); err != nil {
		return err
	}
	if err := validateNext75Dispositions(next75, rows); err != nil {
		return err
	}
	if err := validatePrimaryReadingAmendment(amendment, old, extra); err != nil {
		return err
	}
	for _, c := range comparison.Entries {
		if !matched[c.ID] {
			return errors.New("expanded sixmile index: comparison exclusion lacks a live ledger row")
		}
		for _, r := range rows {
			if r.ID == c.ID && r.Disposition != "excluded" {
				return errors.New("expanded sixmile index: comparison exclusion became an adoption or unfinished decision")
			}
		}
	}
	c := current.Cohorts[3]
	p := parent.Counts
	m := amendment.Counts
	n := next75.Counts
	nc := current.Cohorts[4]
	if nc.Paths != n.Candidates || nc.Raw != n.Raw || nc.Workflow != n.Workflow || nc.Support != n.Support || nc.Saved != n.Saved || nc.Read != n.RawSupport || nc.MaskedSupport != n.MaskedSupport || nc.Scan != n.Scan || nc.Unsaved != n.Unsaved || nc.Partial != n.Partial || nc.Reused != n.SameNotices+n.DiffNotices || nc.WorkflowSupport != n.WorkflowSupport {
		return errors.New("expanded sixmile index: next75 cohort counters differ")
	}
	if c.Paths != p.Candidates || c.Raw != p.Raw || c.Workflow != p.Workflow || c.Support != p.Support || c.Saved != p.Saved || c.Read != p.Read || c.MaskedSupport != p.MaskedSupport || c.Scan != p.Scan || c.Unsaved != p.Unsaved {
		return errors.New("expanded sixmile index: parent6 cohort counters differ")
	}
	if current.Source != snapshot.Source || current.Source != parent.Source || current.Commit != snapshot.Commit || current.Commit != parent.Commit || current.Tree != snapshot.Tree || current.Tree != parent.Tree || current.Expected != snapshot.Expected || !x45True(current.NotCap) || current.Comparison != len(comparison.Entries) || current.Paths != snapshot.Paths+p.Candidates+n.Candidates+current.Comparison || current.Paths != current.Raw+current.Masked+current.Metadata+current.Comparison || current.Raw != snapshot.Raw+p.Raw+m.Raw+n.Raw || current.Workflow != snapshot.Workflow+p.Workflow+m.Workflow+n.Workflow || current.Masked != snapshot.Masked+p.Masked-m.Masked+n.Masked || current.Metadata != snapshot.Metadata+p.Metadata-m.Metadata || current.NotRaw != current.Expected-current.Raw || current.Unassessed != current.Expected-current.Paths || current.Support != snapshot.Support+p.Support+n.Support || current.Saved != snapshot.Saved+p.Saved+n.Saved || current.Read != snapshot.Read+p.Read+n.RawSupport || current.MaskedSupport != snapshot.MaskedSupport+p.MaskedSupport+n.MaskedSupport || current.WorkflowSupport != current.Read+current.MaskedSupport+current.Reused || current.Scan != snapshot.Scan+p.Scan+n.Scan || current.Unsaved != snapshot.Unsaved+p.Unsaved+n.Unsaved || current.SavedUnread != snapshot.SavedUnread+p.Saved-p.Read-p.MaskedSupport-p.Scan+n.Saved-n.WorkflowSupport-n.Scan-n.Partial || current.Partial != n.Partial || current.Reused != n.SameNotices+n.DiffNotices {
		return errors.New("expanded sixmile index: amendment was omitted, duplicated or conflated with a new candidate")
	}
	return nil
}

func TestExpandedSixmileIndexAddsNewCohortAndReadingAmendmentExactlyOnce(t *testing.T) {
	for _, name := range []string{"valid", "synthetic_future_parent_adoption", "missing_parent_cohort", "cross_cohort_identity", "unknown_live_row", "repeated_live_row", "parent_final_without_row", "parent_unknown_disposition", "comparison_row_missing", "comparison_became_adoption", "duplicate_amendment", "amendment_forged_actor", "amendment_rewrites_history", "amendment_wrong_identity", "omitted_amendment", "double_amendment", "received_without_current_acceptance", "received_wrong_acceptance_identity"} {
		t.Run(name, func(t *testing.T) {
			var current sixmileCohortIndex
			snapshot := loadThreeCohortSnapshot(t)
			var old legacy21Projection
			var extra, next20, parent extra45Assessment
			var next75 next75Assessment
			var amendment primaryReadingAmendments
			var comparison mcpComparisonOnly
			var ledger struct {
				Entries  []cohortLedgerRow `json:"entries"`
				Excluded []cohortLedgerRow `json:"excluded_entries"`
			}
			loadPublicReceipt(t, "skills-3080-sixmile-evaluation-index.json", &current)
			loadPublicReceipt(t, "skills-3080-sixmile-partial-evaluation.json", &old)
			loadPublicReceipt(t, "skills-3080-sixmile-next45-evaluation.json", &extra)
			loadPublicReceipt(t, "skills-3080-sixmile-next20-evaluation.json", &next20)
			loadPublicReceipt(t, "skills-3080-sixmile-parent6-evaluation.json", &parent)
			loadPublicReceipt(t, "skills-3080-sixmile-next75-evaluation.json", &next75)
			loadPublicReceipt(t, "skills-3080-primary-reading-amendments.json", &amendment)
			loadPublicReceipt(t, "skills-3080-mcp-sibling-duplicate-evidence.json", &comparison)
			loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
			rows := append(append([]cohortLedgerRow(nil), ledger.Entries...), ledger.Excluded...)
			if name != "valid" {
				selected := map[string]bool{}
				for i := range parent.Entries {
					e := &parent.Entries[i]
					selected[e.ID] = true
					e.Installed, e.Integrated, e.Excluded = false, false, false
				}
				parent.Installed, parent.Integrated, parent.Excluded = 0, 0, 0
				var filtered []cohortLedgerRow
				for _, r := range rows {
					if !selected[r.ID] {
						filtered = append(filtered, r)
					}
				}
				rows = filtered
			}
			want := ""
			switch name {
			case "synthetic_future_parent_adoption", "parent_unknown_disposition":
				// Relationship fixture only; no permission or real deployment is added.
				e := &parent.Entries[0]
				e.Installed = true
				parent.Installed = 1
				r := cohortLedgerRow{ID: e.ID, Source: parent.Source, Commit: e.Commit, Path: e.Path, SHA: e.SHA, Disposition: "installed_new_adaptation"}
				if name == "parent_unknown_disposition" {
					r.Disposition = "pending"
					want = "parent6 dispositions: unfinished decision counted as final"
				}
				rows = append(rows, r)
			case "missing_parent_cohort":
				current.Cohorts = current.Cohorts[:3]
				want = "received index: missing or inconsistent extension"
			case "cross_cohort_identity":
				for i := range old.Entries {
					if old.Entries[i].ID != "github:e4ade76a7932447efe3dd6e777ab300a5a9a6f1cf13a392614b31c621906a016" {
						old.Entries[i].ID = parent.Entries[0].ID
						old.Entries[i].Path = parent.Entries[0].Path
						break
					}
				}
				want = "expanded sixmile index: assessed identity crosses cohorts"
			case "unknown_live_row":
				rows = append(rows, cohortLedgerRow{ID: "unknown", Source: "6mile/skills", Disposition: "excluded"})
				want = "expanded sixmile index: live source row is unknown, repeated or mismatched"
			case "repeated_live_row":
				for _, r := range rows {
					if r.Source == "6mile/skills" {
						rows = append(rows, r)
						break
					}
				}
				want = "expanded sixmile index: live source row is unknown, repeated or mismatched"
			case "parent_final_without_row":
				parent.Entries[0].Installed = true
				parent.Installed = 1
				want = "parent6 dispositions: final flag lacks a live ledger row"
			case "comparison_row_missing":
				var filtered []cohortLedgerRow
				for _, r := range rows {
					if r.ID != comparedOnlyMCPID {
						filtered = append(filtered, r)
					}
				}
				rows = filtered
				want = "expanded sixmile index: comparison exclusion lacks a live ledger row"
			case "comparison_became_adoption":
				for i := range rows {
					if rows[i].ID == comparedOnlyMCPID {
						rows[i].Disposition = "installed_new_adaptation"
					}
				}
				want = "expanded sixmile index: comparison exclusion became an adoption or unfinished decision"
			case "duplicate_amendment":
				amendment.Entries = append(amendment.Entries, amendment.Entries[0])
				want = "reading amendment: fixed scope or category deltas differ"
			case "amendment_forged_actor":
				bad := "forged"
				amendment.Entries[0].New.ReadBy = &bad
				want = "reading amendment: parent raw actor, grant or execution state differs"
			case "amendment_rewrites_history":
				yes := true
				amendment.Entries[0].Original.Read = &yes
				want = "reading amendment: historical unread state or support actors were rewritten"
			case "amendment_wrong_identity":
				amendment.Entries[0].SHA = "wrong"
				want = "reading amendment: fixed original primary identity differs"
			case "omitted_amendment":
				current.Raw--
				current.Workflow--
				current.Metadata++
				want = "expanded sixmile index: amendment was omitted, duplicated or conflated with a new candidate"
			case "double_amendment":
				current.Raw++
				current.Workflow++
				current.NotRaw--
				want = "expanded sixmile index: amendment was omitted, duplicated or conflated with a new candidate"
			}
			received := loadRegisteredReceivedIndex(t)
			accepted := validatedPurposeFourDispositions(t)
			for id, row := range validatedTravel3Dispositions(t) {
				if _, duplicate := accepted[id]; duplicate {
					t.Fatal("separate current acceptances overlap")
				}
				accepted[id] = row
			}
			for id, row := range validatedNext3Dispositions(t) {
				if _, duplicate := accepted[id]; duplicate {
					t.Fatal("separate current next3 acceptances overlap")
				}
				accepted[id] = row
			}
			for id, row := range validatedNoticeNext4Dispositions(t) {
				if _, duplicate := accepted[id]; duplicate {
					t.Fatal("separate current notice4 acceptances overlap")
				}
				accepted[id] = row
			}
			switch name {
			case "received_without_current_acceptance":
				accepted = nil
				want = "expanded sixmile index: immutable received reading lacks a current final acceptance"
			case "received_wrong_acceptance_identity":
				for id, r := range accepted {
					r.SHA = "wrong"
					accepted[id] = r
				}
				want = "expanded sixmile index: immutable received reading lacks a current final acceptance"
			}
			err := validateExpandedSixmileIndex(current, snapshot, old, extra, next20, parent, next75, amendment, comparison, rows, received, accepted)
			if want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != want {
				t.Fatalf("%s reached another guard: %v; want %s", name, err, want)
			}
		})
	}
}

type parent6PublicLinks struct {
	Snapshot struct {
		Artifact string `json:"artifact"`
		Commit   string `json:"source_commit"`
		Path     string `json:"source_path"`
		Blob     string `json:"git_blob"`
		Current  *bool  `json:"is_current_total"`
	} `json:"historical_snapshot"`
	Amendment struct {
		Artifact string `json:"artifact"`
		Entries  int    `json:"entries"`
		Paths    int    `json:"new_candidate_paths"`
		Raw      int    `json:"raw_primary_added"`
		Workflow int    `json:"workflow_primary_added"`
		Metadata int    `json:"metadata_only_resolved"`
		Support  int    `json:"support_paths_added"`
	} `json:"reading_amendments"`
}
type parent6Progress struct {
	Parent struct {
		Source   string `json:"source"`
		Paths    int    `json:"candidate_paths"`
		Raw      int    `json:"full_body_read"`
		Workflow int    `json:"workflow_body_read"`
		Masked   int    `json:"masked_workflow_body"`
		Metadata int    `json:"metadata_only_primary"`
	} `json:"sixmile_parent6_entrypoint_evaluation"`
	Amendment struct {
		Source   string `json:"source"`
		Entries  int    `json:"amendments"`
		Raw      int    `json:"raw_primary_added"`
		Workflow int    `json:"workflow_primary_added"`
		Paths    int    `json:"new_candidate_paths"`
		Support  int    `json:"new_support_paths"`
		Actors   *bool  `json:"original_child_and_support_actors_preserved"`
	} `json:"primary_reading_amendments_summary"`
}

func validateParent6PublicLinks(links parent6PublicLinks, progress parent6Progress, parent extra45Assessment, amendments primaryReadingAmendments) error {
	s, a, p, m := links.Snapshot, links.Amendment, progress.Parent, progress.Amendment
	if s.Artifact != "skills-3080-sixmile-three-cohort-snapshot.json" || s.Commit != "ad8151c101a0820cbc365455ecfb85caef1d697a" || s.Path != "docs/skills-3080-sixmile-evaluation-index.json" || s.Blob != threeCohortSnapshotBlob || !x45False(s.Current) {
		return errors.New("parent6 public links: historical snapshot is absent or presented as current")
	}
	if a.Artifact != "skills-3080-primary-reading-amendments.json" || a.Entries != amendments.Counts.Amendments || a.Paths != amendments.Counts.Paths || a.Raw != amendments.Counts.Raw || a.Workflow != amendments.Counts.Workflow || a.Metadata != amendments.Counts.Metadata || a.Support != amendments.Counts.Support {
		return errors.New("parent6 public links: amendment link changes a reading into a new candidate")
	}
	if p.Source != "skills-3080-sixmile-parent6-evaluation.json" || p.Paths != parent.Counts.Candidates || p.Raw != parent.Counts.Raw || p.Workflow != parent.Counts.Workflow || p.Masked != parent.Counts.Masked || p.Metadata != parent.Counts.Metadata {
		return errors.New("parent6 public links: parent-cohort projection differs")
	}
	if m.Source != "skills-3080-primary-reading-amendments.json" || m.Entries != amendments.Counts.Amendments || m.Raw != amendments.Counts.Raw || m.Workflow != amendments.Counts.Workflow || m.Paths != amendments.Counts.Paths || m.Support != amendments.Counts.Support || !x45True(m.Actors) {
		return errors.New("parent6 public links: amendment projection lost its old actor boundary")
	}
	return nil
}
func TestParent6PublicLinksPreserveTheHistoricalBoundaryAndCurrentProjection(t *testing.T) {
	for _, name := range []string{"valid", "wrong_snapshot_blob", "snapshot_marked_current", "amendment_adds_candidate", "parent_raw_includes_masked", "amendment_loses_actor_boundary"} {
		t.Run(name, func(t *testing.T) {
			var links parent6PublicLinks
			var progress parent6Progress
			var parent extra45Assessment
			var amendments primaryReadingAmendments
			loadPublicReceipt(t, "skills-3080-sixmile-evaluation-index.json", &links)
			loadPublicReceipt(t, "skills-3080-progress.json", &progress)
			loadPublicReceipt(t, "skills-3080-sixmile-parent6-evaluation.json", &parent)
			loadPublicReceipt(t, "skills-3080-primary-reading-amendments.json", &amendments)
			want := ""
			switch name {
			case "wrong_snapshot_blob":
				links.Snapshot.Blob = "wrong"
				want = "parent6 public links: historical snapshot is absent or presented as current"
			case "snapshot_marked_current":
				yes := true
				links.Snapshot.Current = &yes
				want = "parent6 public links: historical snapshot is absent or presented as current"
			case "amendment_adds_candidate":
				links.Amendment.Paths++
				want = "parent6 public links: amendment link changes a reading into a new candidate"
			case "parent_raw_includes_masked":
				progress.Parent.Raw++
				want = "parent6 public links: parent-cohort projection differs"
			case "amendment_loses_actor_boundary":
				progress.Amendment.Actors = nil
				want = "parent6 public links: amendment projection lost its old actor boundary"
			}
			err := validateParent6PublicLinks(links, progress, parent, amendments)
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
func TestParent6PublicAnchorLabelsMatchTheIndependentPreparedReading(t *testing.T) {
	var a struct {
		Anchors struct {
			Bodies   string `json:"source_bodies"`
			Actors   string `json:"source_primary_actors"`
			Support  string `json:"source_support_states"`
			Prepared string `json:"independent_preparation_sha256"`
		} `json:"anchors"`
	}
	loadPublicReceipt(t, "skills-3080-sixmile-parent6-evaluation.json", &a)
	if a.Anchors.Bodies != parent6ReadingSpec.Bodies || a.Anchors.Actors != parent6ReadingSpec.Actors || a.Anchors.Support != parent6ReadingSpec.Supports || a.Anchors.Prepared != "563ced896eff80b425bdf49b8a315e8d26514029396e4f49a991a8b44785e535" {
		t.Fatal("parent6 public anchor labels differ from independent fixed proof")
	}
}
