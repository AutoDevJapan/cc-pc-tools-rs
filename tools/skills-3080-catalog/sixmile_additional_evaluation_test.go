package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"testing"
)

const extra45Bodies = "d41a412c5b6e0f6d38004a09ef57611fb917379afbb3bc31039e06c100ce0e9c"
const extra45Supports = "f928e93fb320780912880304bae59c4ad704a6f5c5579e381ce25df07cf06e38"
const extra45PrimaryActors = "d61534da6c644463e66fdebc4f6badd821c9fb889bfa0a9ed8f9ff4012f35bc7"

// These anchors preserve the private normalization after the independent
// fixed-tree/index/catalog/raw-object checks, not digests derived from the
// eventual live public ledger. Raw, redacted workflow and scan stay separate.
type extra45Support struct {
	Path       string  `json:"path"`
	Blob       string  `json:"git_blob"`
	SHA        string  `json:"source_sha256"`
	Bytes      *int    `json:"bytes"`
	Saved      *bool   `json:"body_saved"`
	Read       *bool   `json:"body_read"`
	Workflow   *bool   `json:"workflow_read_complete"`
	Scan       *bool   `json:"scan_only"`
	Mask       *int    `json:"masked_value_count"`
	ReadBy     *string `json:"read_by"`
	WorkflowBy *string `json:"workflow_read_by"`
	ScanBy     *string `json:"scan_by"`
	Executed   *bool   `json:"executed"`
}
type extra45Entry struct {
	ID         string  `json:"inventory_id"`
	Path       string  `json:"path"`
	SHA        string  `json:"source_sha256"`
	Blob       string  `json:"git_blob"`
	Bytes      int     `json:"bytes"`
	Name       string  `json:"name"`
	Use        string  `json:"use"`
	Reason     string  `json:"reason"`
	Commit     string  `json:"commit"`
	URL        string  `json:"source_url"`
	Read       *bool   `json:"body_read_complete"`
	Workflow   *bool   `json:"body_workflow_read_complete"`
	Mask       *int    `json:"masked_value_count"`
	ReadBy     *string `json:"body_read_by"`
	WorkflowBy *string `json:"body_workflow_read_by"`
	Executed   *bool   `json:"source_code_executed"`
	Installed  bool    `json:"installed_by_todo3080"`
	Integrated bool    `json:"integrated_by_todo3080"`
	Excluded   bool    `json:"excluded_by_todo3080"`
	Rights     struct {
		Path    string  `json:"notice_path"`
		SHA     string  `json:"notice_sha256"`
		Blob    string  `json:"notice_git_blob"`
		Bytes   int     `json:"notice_bytes"`
		Read    *bool   `json:"notice_read_complete"`
		ReadBy  *string `json:"notice_read_by"`
		Kind    string  `json:"license_kind"`
		Copy    *bool   `json:"copy_license_accepted"`
		Support *bool   `json:"third_party_support_or_service_rights_accepted"`
	} `json:"individual_rights_review"`
	Support []extra45Support `json:"supporting_resources"`
}
type extra45Assessment struct {
	Source     string         `json:"source"`
	Commit     string         `json:"commit"`
	Tree       string         `json:"tree_sha"`
	Installed  int            `json:"installed"`
	Integrated int            `json:"integrated"`
	Excluded   int            `json:"excluded"`
	Entries    []extra45Entry `json:"entries"`
	Counts     struct {
		Candidates      int `json:"candidate_assessments"`
		Raw             int `json:"raw_unmasked_primary_full_read"`
		Workflow        int `json:"workflow_primary_full_read"`
		Masked          int `json:"masked_primary"`
		Metadata        int `json:"metadata_only_primary"`
		Notices         int `json:"individual_notices"`
		Support         int `json:"support"`
		Saved           int `json:"support_saved"`
		Read            int `json:"support_raw_unmasked_full_read"`
		WorkflowSupport int `json:"support_workflow_full_read"`
		MaskedSupport   int `json:"support_masked_workflow_full_read"`
		Scan            int `json:"support_scan_only"`
		Unsaved         int `json:"support_unsaved"`
	} `json:"counts"`
}

func x45String(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
func x45True(v *bool) bool  { return v != nil && *v }
func x45False(v *bool) bool { return v != nil && !*v }
func x45Hex(s string, n int) bool {
	b, e := hex.DecodeString(s)
	return e == nil && len(s) == n && len(b)*2 == n
}
func x45Digest(rows []string) string {
	copyRows := append([]string(nil), rows...)
	sort.Strings(copyRows)
	h := sha256.Sum256([]byte(strings.Join(copyRows, "")))
	return hex.EncodeToString(h[:])
}

func validateExtra45Reading(a extra45Assessment) error {
	if a.Source != "6mile/skills" || a.Commit != "920714f17d81e0a95c7a9bc51faf0a7921cd982d" || a.Tree != "5782ae7deffe5024e15b6e090327385c10f76273" || len(a.Entries) != 45 {
		return errors.New("extra45 reading: fixed cohort identity differs")
	}
	ids, paths, supportPaths := map[string]bool{}, map[string]bool{}, map[string]bool{}
	var bodyRows, actorRows, supportRows []string
	raw, workflow, masked, metadata, notices, support, saved, read, workflowSupport, maskedSupport, scan, unsaved := 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
	for _, e := range a.Entries {
		if e.ID == "" || ids[e.ID] || paths[e.Path] || !strings.HasPrefix(e.Path, "skills/") || path.Clean(e.Path) != e.Path || strings.ContainsAny(e.Path, "\\:") || !strings.HasSuffix(e.Path, "/SKILL.md") || !x45Hex(e.SHA, 64) || !x45Hex(e.Blob, 40) || e.Bytes < 1 || e.Commit != a.Commit || e.URL != "https://github.com/"+a.Source+"/blob/"+a.Commit+"/"+e.Path || e.Name == "" || e.Use == "" || e.Reason == "" || e.Read == nil || e.Workflow == nil || !x45False(e.Executed) {
			return errors.New("extra45 reading: primary identity/state is absent or conflated")
		}
		ids[e.ID], paths[e.Path] = true, true
		full, wf := *e.Read, *e.Workflow
		actor := ""
		switch {
		case full:
			if !wf || e.Mask == nil || *e.Mask != 0 || x45String(e.ReadBy) == "" || e.WorkflowBy != nil {
				return errors.New("extra45 reading: raw primary acquired masking or lost its reader")
			}
			raw++
			workflow++
			actor = *e.ReadBy
		case wf:
			if e.Mask == nil || *e.Mask < 1 || e.ReadBy != nil || x45String(e.WorkflowBy) == "" {
				return errors.New("extra45 reading: masked workflow primary was claimed as raw or lost its actor")
			}
			workflow++
			masked++
			actor = *e.WorkflowBy
		default:
			if e.ReadBy != nil || e.WorkflowBy != nil || e.Mask != nil {
				return errors.New("extra45 reading: metadata-only primary acquired a text reading claim")
			}
			metadata++
		}
		r := e.Rights
		if !x45True(r.Read) || x45String(r.ReadBy) == "" || r.Kind == "" || r.Copy == nil || !x45False(r.Support) || !strings.HasPrefix(r.Path, path.Dir(e.Path)+"/") || !x45Hex(r.SHA, 64) || !x45Hex(r.Blob, 40) || r.Bytes < 1 {
			return errors.New("extra45 reading: individual notice reading or rights scope is missing")
		}
		if actor == "" {
			actor = *r.ReadBy
		} else if *r.ReadBy != actor {
			return errors.New("extra45 reading: notice actor differs from this received reading cohort")
		}
		notices++
		bodyRows = append(bodyRows, fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%t\t%t\n", e.Path, e.ID, e.SHA, e.Blob, e.Bytes, full, wf))
		actorRows = append(actorRows, e.Path+"\t"+x45String(e.ReadBy)+"\t"+x45String(e.WorkflowBy)+"\n")
		noticeMatches := 0
		for _, s := range e.Support {
			if !strings.HasPrefix(s.Path, path.Dir(e.Path)+"/") || s.Path == e.Path || supportPaths[s.Path] || !x45Hex(s.Blob, 40) || s.Saved == nil || s.Read == nil || s.Workflow == nil || s.Scan == nil || s.Mask == nil || *s.Mask < 0 || !x45False(s.Executed) {
				return errors.New("extra45 reading: support identity or explicit state is missing")
			}
			supportPaths[s.Path] = true
			support++
			stored, fullSupport, wfSupport, scanned := *s.Saved, *s.Read, *s.Workflow, *s.Scan
			if stored {
				if s.Bytes == nil || *s.Bytes < 1 || !x45Hex(s.SHA, 64) {
					return errors.New("extra45 reading: saved support identity is incomplete")
				}
				saved++
			} else {
				if s.Bytes != nil || s.SHA != "" || fullSupport || wfSupport || scanned || *s.Mask != 0 {
					return errors.New("extra45 reading: unknown support bytes or reading were invented")
				}
				unsaved++
			}
			switch {
			case fullSupport:
				if !wfSupport || scanned || *s.Mask != 0 || x45String(s.ReadBy) != actor || s.WorkflowBy != nil || s.ScanBy != nil {
					return errors.New("extra45 reading: raw support acquired masking/scanning or a wrong reader")
				}
				read++
				workflowSupport++
			case wfSupport:
				if !stored || scanned || *s.Mask < 1 || s.ReadBy != nil || x45String(s.WorkflowBy) != actor || s.ScanBy != nil {
					return errors.New("extra45 reading: masked workflow support was claimed as raw or lost its actor")
				}
				workflowSupport++
				maskedSupport++
			case scanned:
				if !stored || s.ReadBy != nil || s.WorkflowBy != nil || x45String(s.ScanBy) != actor {
					return errors.New("extra45 reading: scan-only support acquired a full reader")
				}
				scan++
			default:
				if *s.Mask != 0 || s.ReadBy != nil || s.WorkflowBy != nil || s.ScanBy != nil {
					return errors.New("extra45 reading: unread support acquired a reader/scanner")
				}
			}
			if s.Path == r.Path {
				if !stored || !fullSupport || s.SHA != r.SHA || s.Blob != r.Blob || s.Bytes == nil || *s.Bytes != r.Bytes {
					return errors.New("extra45 reading: notice does not match its independently verified package support")
				}
				noticeMatches++
			}
			size := "?"
			if s.Bytes != nil {
				size = fmt.Sprint(*s.Bytes)
			}
			supportRows = append(supportRows, fmt.Sprintf("%s\t%s\t%s\t%s\t%t\t%t\t%t\t%d\t%t\t%s\t%s\t%s\n", s.Path, s.Blob, size, s.SHA, stored, fullSupport, wfSupport, *s.Mask, scanned, x45String(s.ReadBy), x45String(s.WorkflowBy), x45String(s.ScanBy)))
		}
		if noticeMatches != 1 {
			return errors.New("extra45 reading: individual notice missing or repeated")
		}
	}
	c := a.Counts
	if raw != 43 || workflow != 44 || masked != 1 || metadata != 1 || notices != 45 || support != 422 || saved != 246 || read != 126 || workflowSupport != 149 || maskedSupport != 23 || scan != 85 || unsaved != 176 || c.Candidates != 45 || c.Raw != raw || c.Workflow != workflow || c.Masked != masked || c.Metadata != metadata || c.Notices != notices || c.Support != support || c.Saved != saved || c.Read != read || c.WorkflowSupport != workflowSupport || c.MaskedSupport != maskedSupport || c.Scan != scan || c.Unsaved != unsaved {
		return errors.New("extra45 reading: raw/masked/metadata/scan cohort counters differ")
	}
	if x45Digest(bodyRows) != extra45Bodies || x45Digest(actorRows) != extra45PrimaryActors || x45Digest(supportRows) != extra45Supports {
		return errors.New("extra45 reading: fixed independent identities or reading actors changed")
	}
	return nil
}

func TestAdditionalSixmile45RejectsRawMaskedScanAndMetadataConflation(t *testing.T) {
	wanted := map[string]string{
		"masked_primary_claims_raw":      "extra45 reading: raw primary acquired masking or lost its reader",
		"workflow_primary_loses_actor":   "extra45 reading: masked workflow primary was claimed as raw or lost its actor",
		"metadata_primary_claims_reader": "extra45 reading: metadata-only primary acquired a text reading claim",
		"raw_support_claims_scan_actor":  "extra45 reading: raw support acquired masking/scanning or a wrong reader",
		"masked_support_claims_raw":      "extra45 reading: raw support acquired masking/scanning or a wrong reader",
		"scan_claims_full":               "extra45 reading: raw support acquired masking/scanning or a wrong reader",
		"unsaved_claims_workflow":        "extra45 reading: unknown support bytes or reading were invented",
		"missing_execution":              "extra45 reading: support identity or explicit state is missing",
		"unknown_bytes_became_zero":      "extra45 reading: unknown support bytes or reading were invented",
		"body_hash_changed":              "extra45 reading: fixed independent identities or reading actors changed",
		"source_url_changed":             "extra45 reading: primary identity/state is absent or conflated",
	}
	for _, kind := range []string{"valid", "masked_primary_claims_raw", "workflow_primary_loses_actor", "metadata_primary_claims_reader", "raw_support_claims_scan_actor", "masked_support_claims_raw", "scan_claims_full", "unsaved_claims_workflow", "missing_execution", "unknown_bytes_became_zero", "body_hash_changed", "source_url_changed"} {
		t.Run(kind, func(t *testing.T) {
			var a extra45Assessment
			loadPublicReceipt(t, "skills-3080-sixmile-next45-evaluation.json", &a)
			yes := true
			changed := kind == "valid"
			for i := range a.Entries {
				e := &a.Entries[i]
				if changed {
					break
				}
				switch kind {
				case "masked_primary_claims_raw":
					if !x45True(e.Read) && x45True(e.Workflow) {
						e.Read = &yes
						changed = true
					}
				case "workflow_primary_loses_actor":
					if !x45True(e.Read) && x45True(e.Workflow) {
						e.WorkflowBy = nil
						changed = true
					}
				case "metadata_primary_claims_reader":
					if !x45True(e.Read) && !x45True(e.Workflow) {
						actor := "invented"
						e.ReadBy = &actor
						changed = true
					}
				case "body_hash_changed":
					e.SHA = strings.Repeat("e", 64)
					changed = true
				case "source_url_changed":
					e.URL = "https://example.invalid/unrelated"
					changed = true
				}
				if changed {
					break
				}
				for j := range e.Support {
					s := &e.Support[j]
					switch kind {
					case "raw_support_claims_scan_actor":
						if x45True(s.Read) {
							actor := x45String(s.ReadBy)
							s.ScanBy = &actor
							changed = true
						}
					case "masked_support_claims_raw":
						if !x45True(s.Read) && x45True(s.Workflow) {
							s.Read = &yes
							changed = true
						}
					case "scan_claims_full":
						if x45True(s.Scan) {
							s.Read = &yes
							changed = true
						}
					case "unsaved_claims_workflow":
						if !x45True(s.Saved) {
							s.Workflow = &yes
							changed = true
						}
					case "missing_execution":
						s.Executed = nil
						changed = true
					case "unknown_bytes_became_zero":
						if !x45True(s.Saved) {
							v := 0
							s.Bytes = &v
							changed = true
						}
					}
					if changed {
						break
					}
				}
			}
			if !changed {
				t.Fatal("no matching independently verified fixture to mutate")
			}
			err := validateExtra45Reading(a)
			if (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
			if kind != "valid" && (wanted[kind] == "" || err.Error() != wanted[kind]) {
				t.Fatalf("%s reached another guard: got %v, want %q", kind, err, wanted[kind])
			}
		})
	}
}

type sixmileCohortIndex struct {
	Source          string `json:"source"`
	Commit          string `json:"commit"`
	Tree            string `json:"tree_sha"`
	Expected        int    `json:"entrypoints_expected"`
	Paths           int    `json:"candidate_paths_assessed_or_notice_read"`
	Raw             int    `json:"raw_unmasked_primary_full_read"`
	Workflow        int    `json:"workflow_primary_full_read"`
	Masked          int    `json:"masked_workflow_primary"`
	Metadata        int    `json:"metadata_only_primary"`
	NotRaw          int    `json:"primary_not_raw_full_read"`
	NotCap          *bool  `json:"no_adoption_cap"`
	Support         int    `json:"support"`
	Saved           int    `json:"support_saved"`
	Read            int    `json:"support_raw_unmasked_full_read"`
	MaskedSupport   int    `json:"support_masked_workflow_full_read"`
	Scan            int    `json:"support_scan_only"`
	SavedUnread     int    `json:"support_saved_unread"`
	Unsaved         int    `json:"support_unsaved"`
	WorkflowSupport int    `json:"support_workflow_full_read"`
	Unassessed      int    `json:"candidate_paths_without_assessment"`
	Cohorts         []struct {
		ID            string `json:"id"`
		Artifact      string `json:"artifact"`
		Paths         int    `json:"candidate_paths"`
		Raw           int    `json:"raw_unmasked_primary_full_read"`
		Workflow      int    `json:"workflow_primary_full_read"`
		Support       int    `json:"support"`
		Saved         int    `json:"support_saved"`
		Read          int    `json:"support_raw_unmasked_full_read"`
		MaskedSupport int    `json:"support_masked_workflow_full_read"`
		Scan          int    `json:"support_scan_only"`
		Unsaved       int    `json:"support_unsaved"`
	} `json:"cohorts"`
}
type legacy21Projection struct {
	Source     string         `json:"source"`
	Commit     string         `json:"commit"`
	Tree       string         `json:"tree_sha"`
	Read       int            `json:"entrypoints_fulltext_read"`
	Installed  int            `json:"installed"`
	Integrated int            `json:"integrated"`
	Excluded   int            `json:"excluded"`
	Entries    []extra45Entry `json:"entries"`
}
type cohortLedgerRow struct {
	ID          string `json:"inventory_id"`
	Source      string `json:"source_repo"`
	Commit      string `json:"source_commit"`
	Path        string `json:"source_path"`
	SHA         string `json:"source_sha256"`
	Disposition string `json:"disposition"`
}

func validateSixmileCohortIndex(index sixmileCohortIndex, old legacy21Projection, extra extra45Assessment, ledger []cohortLedgerRow) error {
	if err := validateExtra45Reading(extra); err != nil {
		return err
	}
	if index.Source != old.Source || index.Source != extra.Source || index.Commit != old.Commit || index.Commit != extra.Commit || index.Tree != old.Tree || index.Tree != extra.Tree || index.Expected != 55623 || !x45True(index.NotCap) || len(index.Cohorts) != 2 || len(old.Entries) != 21 || old.Read != 21 {
		return errors.New("sixmile cohorts: source or historical boundary differs")
	}
	if index.Cohorts[0].ID != "first21" || index.Cohorts[0].Artifact != "skills-3080-sixmile-partial-evaluation.json" || index.Cohorts[1].ID != "next45" || index.Cohorts[1].Artifact != "skills-3080-sixmile-next45-evaluation.json" {
		return errors.New("sixmile cohorts: missing/repeated/unbound evidence cohort")
	}
	known := map[string]extra45Entry{}
	paths := map[string]bool{}
	allPaths, raw, workflow, masked, metadata, support, saved, read, maskedSupport, scan, unsaved, savedUnread := 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0
	for i, entries := range [][]extra45Entry{old.Entries, extra.Entries} {
		c := index.Cohorts[i]
		cRaw, cWorkflow, cSupport, cSaved, cRead, cMasked, cScan, cUnsaved := 0, 0, 0, 0, 0, 0, 0, 0
		for _, e := range entries {
			if known[e.ID].ID != "" || paths[e.Path] || e.ID == "" || e.Path == "" || e.Commit != index.Commit {
				return errors.New("sixmile cohorts: repeated or conflicting assessed identity")
			}
			known[e.ID] = e
			paths[e.Path] = true
			allPaths++
			full := x45True(e.Read)
			wf := full || x45True(e.Workflow)
			if i == 0 && !full {
				return errors.New("sixmile cohorts: historical raw reading was changed")
			}
			if full {
				raw++
				cRaw++
			}
			if wf {
				workflow++
				cWorkflow++
			} else {
				metadata++
			}
			if wf && !full {
				masked++
			}
			for _, s := range e.Support {
				support++
				cSupport++
				stored := x45True(s.Saved)
				fullSupport := x45True(s.Read)
				maskedFull := !fullSupport && x45True(s.Workflow)
				scanned := x45True(s.Scan)
				if stored {
					saved++
					cSaved++
				} else {
					unsaved++
					cUnsaved++
				}
				if fullSupport {
					read++
					cRead++
				}
				if maskedFull {
					maskedSupport++
					cMasked++
				}
				if scanned {
					scan++
					cScan++
				}
				if stored && !fullSupport && !maskedFull && !scanned {
					savedUnread++
				}
			}
		}
		if c.Paths != len(entries) || c.Raw != cRaw || c.Workflow != cWorkflow || c.Support != cSupport || c.Saved != cSaved || c.Read != cRead || c.MaskedSupport != cMasked || c.Scan != cScan || c.Unsaved != cUnsaved {
			return errors.New("sixmile cohorts: per-cohort raw/masked/support projection differs")
		}
	}
	if allPaths != 66 || raw != 64 || workflow != 65 || masked != 1 || metadata != 1 || index.Paths != allPaths || index.Raw != raw || index.Workflow != workflow || index.Masked != masked || index.Metadata != metadata || index.NotRaw != index.Expected-raw || index.Support != support || index.Saved != saved || index.Read != read || index.MaskedSupport != maskedSupport || index.Scan != scan || index.Unsaved != unsaved || index.SavedUnread != savedUnread || index.WorkflowSupport != read+maskedSupport || index.Unassessed != index.Expected-allPaths {
		return errors.New("sixmile cohorts: aggregate coverage double-counted or conflated a reading category")
	}
	matched := map[string]bool{}
	oldInstalled, oldIntegrated, oldExcluded, newInstalled, newIntegrated, newExcluded := 0, 0, 0, 0, 0, 0
	oldIDs := map[string]bool{}
	for _, e := range old.Entries {
		oldIDs[e.ID] = true
	}
	for _, row := range ledger {
		if row.Source != index.Source {
			continue
		}
		e, ok := known[row.ID]
		if !ok || matched[row.ID] || row.Commit != e.Commit || row.Path != e.Path || row.SHA != e.SHA {
			return errors.New("sixmile cohorts: live source disposition is outside all assessed cohorts or repeated")
		}
		matched[row.ID] = true
		switch row.Disposition {
		case "installed_new_adaptation":
			if !e.Installed || e.Integrated || e.Excluded {
				return errors.New("sixmile cohorts: installation flag differs")
			}
			if oldIDs[e.ID] {
				oldInstalled++
			} else {
				newInstalled++
			}
		case "integrated_existing":
			if !e.Integrated || e.Installed || e.Excluded {
				return errors.New("sixmile cohorts: integration flag differs")
			}
			if oldIDs[e.ID] {
				oldIntegrated++
			} else {
				newIntegrated++
			}
		case "excluded":
			if !e.Excluded || e.Installed || e.Integrated {
				return errors.New("sixmile cohorts: exclusion flag differs")
			}
			if oldIDs[e.ID] {
				oldExcluded++
			} else {
				newExcluded++
			}
		default:
			return errors.New("sixmile cohorts: unfinished disposition counted as final")
		}
	}
	for _, e := range known {
		if (e.Installed || e.Integrated || e.Excluded) && !matched[e.ID] {
			return errors.New("sixmile cohorts: source flag has no corresponding live disposition")
		}
	}
	if oldInstalled != old.Installed || oldIntegrated != old.Integrated || oldExcluded != old.Excluded || newInstalled != extra.Installed || newIntegrated != extra.Integrated || newExcluded != extra.Excluded {
		return errors.New("sixmile cohorts: bounded cohort disposition counters differ from union ledger")
	}
	return nil
}

func TestSixmileCohortIndexConservesReadingCategoriesAndAllLiveSourceRows(t *testing.T) {
	wanted := map[string]string{
		"raw_includes_masked":      "sixmile cohorts: aggregate coverage double-counted or conflated a reading category",
		"workflow_count":           "sixmile cohorts: aggregate coverage double-counted or conflated a reading category",
		"unknown_size_count":       "sixmile cohorts: aggregate coverage double-counted or conflated a reading category",
		"workflow_support_missing": "sixmile cohorts: aggregate coverage double-counted or conflated a reading category",
		"unassessed_count":         "sixmile cohorts: aggregate coverage double-counted or conflated a reading category",
		"cohort_overlap":           "sixmile cohorts: repeated or conflicting assessed identity",
		"unknown_adoption":         "sixmile cohorts: live source disposition is outside all assessed cohorts or repeated",
		"duplicate_source_row":     "sixmile cohorts: live source disposition is outside all assessed cohorts or repeated",
		"extra_source_flag":        "sixmile cohorts: source flag has no corresponding live disposition",
	}
	for _, kind := range []string{"valid", "raw_includes_masked", "workflow_count", "unknown_size_count", "workflow_support_missing", "unassessed_count", "cohort_overlap", "unknown_adoption", "duplicate_source_row", "extra_source_flag"} {
		t.Run(kind, func(t *testing.T) {
			var index sixmileCohortIndex
			var old legacy21Projection
			var extra extra45Assessment
			var ledger struct {
				Entries  []cohortLedgerRow `json:"entries"`
				Excluded []cohortLedgerRow `json:"excluded_entries"`
			}
			loadPublicReceipt(t, "skills-3080-sixmile-evaluation-index.json", &index)
			loadPublicReceipt(t, "skills-3080-sixmile-partial-evaluation.json", &old)
			loadPublicReceipt(t, "skills-3080-sixmile-next45-evaluation.json", &extra)
			loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
			rows := append(ledger.Entries, ledger.Excluded...)
			switch kind {
			case "raw_includes_masked":
				index.Raw++
			case "workflow_count":
				index.Workflow--
			case "unknown_size_count":
				index.Unsaved--
			case "workflow_support_missing":
				index.WorkflowSupport = 0
			case "unassessed_count":
				index.Unassessed++
			case "cohort_overlap":
				// Keep the independently validated new45 intact, so this reaches
				// the union collision check rather than its URL/hash validation.
				old.Entries[0].ID = extra.Entries[0].ID
				old.Entries[0].Path = extra.Entries[0].Path
			case "unknown_adoption":
				rows = append(rows, cohortLedgerRow{ID: "unassessed", Source: "6mile/skills", Disposition: "installed_new_adaptation"})
			case "duplicate_source_row":
				changed := false
				for _, r := range rows {
					if r.Source == "6mile/skills" {
						rows = append(rows, r)
						changed = true
						break
					}
				}
				if !changed {
					t.Fatal("no existing source row")
				}
			case "extra_source_flag":
				extra.Entries[0].Installed = true
			}
			err := validateSixmileCohortIndex(index, old, extra, rows)
			if (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
			if kind != "valid" && (wanted[kind] == "" || err.Error() != wanted[kind]) {
				t.Fatalf("%s reached another guard: got %v, want %q", kind, err, wanted[kind])
			}
		})
	}
}

type sixmileReadProjection struct {
	Raw      int `json:"all_fulltext_primary_paths_including_partial_sources"`
	Workflow int `json:"all_workflow_primary_paths_including_partial_sources"`
	Complete struct {
		Read int `json:"case_insensitive_skill_named_paths"`
	} `json:"full_source_body_read_coverage"`
	Mizchi struct {
		Read int `json:"full_body_read"`
	} `json:"mizchi_partial_entrypoint_evaluation"`
	Mengto struct {
		Read int `json:"full_body_read"`
	} `json:"mengto_partial_entrypoint_evaluation"`
	Legacy struct {
		Source     string `json:"source"`
		Read       int    `json:"full_body_read"`
		Historical *bool  `json:"historical_cohort_only"`
	} `json:"sixmile_partial_entrypoint_evaluation"`
	Extra struct {
		Source   string `json:"source"`
		Read     int    `json:"full_body_read"`
		Workflow int    `json:"workflow_body_read"`
		Masked   int    `json:"masked_workflow_body"`
		Metadata int    `json:"metadata_only_primary"`
	} `json:"sixmile_additional_entrypoint_evaluation"`
	Aggregate struct {
		Source      string `json:"source"`
		Expected    int    `json:"expected"`
		Paths       int    `json:"candidate_paths_assessed_or_notice_read"`
		Read        int    `json:"full_body_read"`
		Workflow    int    `json:"workflow_body_read"`
		Masked      int    `json:"masked_workflow_body"`
		Metadata    int    `json:"metadata_only_primary"`
		NotRaw      int    `json:"without_raw_full_read"`
		NotAssessed int    `json:"without_assessment"`
		Complete    *bool  `json:"complete"`
	} `json:"sixmile_body_read_coverage"`
}

func validateSixmileReadProjection(p sixmileReadProjection, index sixmileCohortIndex, old legacy21Projection, extra extra45Assessment) error {
	a := p.Aggregate
	if a.Source != "skills-3080-sixmile-evaluation-index.json" || a.Expected != index.Expected || a.Paths != index.Paths || a.Read != index.Raw || a.Workflow != index.Workflow || a.Masked != index.Masked || a.Metadata != index.Metadata || a.NotRaw != index.NotRaw || a.NotAssessed != index.Expected-index.Paths || !x45False(a.Complete) {
		return errors.New("sixmile read projection: aggregate is not the disjoint source index")
	}
	if p.Legacy.Source != "skills-3080-sixmile-partial-evaluation.json" || p.Legacy.Read != old.Read || !x45True(p.Legacy.Historical) || p.Extra.Source != "skills-3080-sixmile-next45-evaluation.json" || p.Extra.Read != extra.Counts.Raw || p.Extra.Workflow != extra.Counts.Workflow || p.Extra.Masked != extra.Counts.Masked || p.Extra.Metadata != extra.Counts.Metadata {
		return errors.New("sixmile read projection: current source aggregate was confused with one evidence cohort")
	}
	other := p.Complete.Read + p.Mizchi.Read + p.Mengto.Read
	if p.Raw != other+index.Raw || p.Workflow != other+index.Workflow {
		return errors.New("sixmile read projection: all-source raw and workflow totals differ")
	}
	return nil
}
func TestSixmileGlobalReadProjectionUsesRawCoverageAndPreservesHistoricalCohorts(t *testing.T) {
	for _, kind := range []string{"valid", "raw_includes_masked", "all_raw_total", "all_workflow_total", "lost_legacy_scope", "extra_raw_total", "wrong_index", "wrong_unassessed"} {
		t.Run(kind, func(t *testing.T) {
			var p sixmileReadProjection
			var index sixmileCohortIndex
			var old legacy21Projection
			var extra extra45Assessment
			loadPublicReceipt(t, "skills-3080-progress.json", &p)
			loadPublicReceipt(t, "skills-3080-sixmile-evaluation-index.json", &index)
			loadPublicReceipt(t, "skills-3080-sixmile-partial-evaluation.json", &old)
			loadPublicReceipt(t, "skills-3080-sixmile-next45-evaluation.json", &extra)
			switch kind {
			case "raw_includes_masked":
				p.Aggregate.Read++
			case "all_raw_total":
				p.Raw++
			case "all_workflow_total":
				p.Workflow--
			case "lost_legacy_scope":
				p.Legacy.Historical = nil
			case "extra_raw_total":
				p.Extra.Read++
			case "wrong_index":
				p.Aggregate.Source = "skills-3080-sixmile-partial-evaluation.json"
			case "wrong_unassessed":
				p.Aggregate.NotAssessed = p.Aggregate.NotRaw
			}
			err := validateSixmileReadProjection(p, index, old, extra)
			if (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
			if kind != "valid" && !strings.HasPrefix(err.Error(), "sixmile read projection:") {
				t.Fatalf("mutation failed for another reason: %v", err)
			}
		})
	}
}
