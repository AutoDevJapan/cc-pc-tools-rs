package main

import (
	"errors"
	"fmt"
	"path"
	"strings"
	"testing"
)

// Frozen from the reconciled private75 reading and fixed80 physical proof,
// before public reflection. They are independent of the live disposition ledger.
const next75Bodies = "67965769a6294925605e3c1167671fe330b31c2626b2704ea952b4a2ccde3f25"
const next75Actors = "2153a3201143699202efae4cd003d0c3f25d720d29521b5f5d66342bf249f0bc"
const next75SupportStates = "20740fc09ffd790bf51ee59f08837253c8cce466ed970546d4472f8d48a7de0e"
const next75NoticeIdentities = "b04b185ca0b8b14e801ccbda8514115cf8186dc225bd6e725c7fded25368563f"

type next75NoticeRepresentative struct {
	Ordinal                     int
	ID, Path, SHA, Blob, Reader string
	Bytes                       int
}

func equalNoticeRepresentatives(a, b *next75NoticeRepresentative) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

type next75Support struct {
	extra45Support
	State          string                      `json:"state"`
	Partial        *bool                       `json:"partial_read"`
	MaskKnown      *bool                       `json:"mask_count_known"`
	PartialBy      *string                     `json:"partial_read_by"`
	CompareBy      *string                     `json:"comparison_read_by"`
	Representative *next75NoticeRepresentative `json:"reuse_representative"`
}
type next75Notice struct {
	Path           string                      `json:"path"`
	SHA            string                      `json:"source_sha256"`
	Blob           string                      `json:"git_blob"`
	Bytes          int                         `json:"bytes"`
	Read           *bool                       `json:"body_read_complete"`
	Workflow       *bool                       `json:"workflow_read_complete"`
	Method         string                      `json:"read_method"`
	Reader         string                      `json:"reader_job"`
	Mask           *int                        `json:"masked_value_count"`
	MaskKnown      *bool                       `json:"mask_count_known"`
	Kind           string                      `json:"license_kind"`
	Copy           *bool                       `json:"copy_license_accepted"`
	Support        *bool                       `json:"third_party_support_or_service_rights_accepted"`
	Representative *next75NoticeRepresentative `json:"reuse_representative"`
}
type next75Entry struct {
	extra45Entry
	ReaderTodo         int             `json:"reader_todo"`
	Reader             string          `json:"reader_job"`
	ReaderRow          int             `json:"reader_row"`
	Ordinal            int             `json:"one_based_ordinal"`
	NameState          string          `json:"name_state"`
	PackageFingerprint string          `json:"package_tree_fingerprint"`
	Positive           string          `json:"positive_candidate"`
	Negative           string          `json:"negative_candidate"`
	Support            []next75Support `json:"supporting_resources"`
	Rights             struct {
		Notices    []next75Notice `json:"notices"`
		Copy       *bool          `json:"copy_license_accepted"`
		Support    *bool          `json:"third_party_support_or_service_rights_accepted"`
		Acceptance *struct {
			NoticePath string `json:"notice_path"`
			NoticeSHA  string `json:"notice_sha256"`
			Scope      string `json:"scope"`
		} `json:"copy_acceptance"`
	} `json:"individual_rights_review"`
}
type next75Assessment struct {
	Source  string        `json:"source"`
	Commit  string        `json:"commit"`
	Tree    string        `json:"tree_sha"`
	Entries []next75Entry `json:"entries"`
	Anchors struct {
		Bodies     string `json:"body_rows"`
		Actors     string `json:"actor_rows"`
		Support    string `json:"support_state_rows"`
		Notices    string `json:"notice_identity_rows"`
		Prepared   string `json:"independent_private75_sha256"`
		Physical   string `json:"fixed80_proof_sha256"`
		Refinement string `json:"notice_reuse_refinement_sha256"`
	} `json:"anchors"`
	Counts struct {
		Candidates         int `json:"candidates"`
		PrimaryBytes       int `json:"primary_bytes"`
		Raw                int `json:"primary_raw"`
		Masked             int `json:"primary_masked"`
		Workflow           int `json:"primary_workflow_including_raw"`
		Notices            int `json:"notices"`
		RosterNotices      int `json:"roster_notice_paths"`
		ExtraNotices       int `json:"extra_notice_paths"`
		MaskedNotices      int `json:"notice_masked_workflow"`
		UnknownNoticeMasks int `json:"notice_redaction_count_unknown_paths"`
		Support            int `json:"support"`
		Saved              int `json:"saved"`
		RawSupport         int `json:"support_raw_unmasked_full_read"`
		MaskedSupport      int `json:"masked_full_support"`
		SameNotices        int `json:"notice_same_bytes_reused"`
		DiffNotices        int `json:"notice_ordered_diff_reported"`
		WorkflowSupport    int `json:"full_workflow_support"`
		Scan               int `json:"scan_or_line_difference_only"`
		Partial            int `json:"partial_support"`
		Unread             int `json:"saved_unread"`
		Unsaved            int `json:"unsaved"`
	} `json:"counts"`
	Installed  int `json:"installed"`
	Integrated int `json:"integrated"`
	Excluded   int `json:"excluded"`
}

func validateNext75Reading(a next75Assessment) error {
	bad := func(s string) error { return errors.New("next75 reading: " + s) }
	if a.Source != "6mile/skills" || a.Commit != "920714f17d81e0a95c7a9bc51faf0a7921cd982d" || a.Tree != "5782ae7deffe5024e15b6e090327385c10f76273" || len(a.Entries) != 75 {
		return bad("fixed source or cohort differs")
	}
	if a.Anchors.Bodies != next75Bodies || a.Anchors.Actors != next75Actors || a.Anchors.Support != next75SupportStates || a.Anchors.Notices != next75NoticeIdentities || a.Anchors.Prepared != "661ebbd875f0e6a1406f460df0311e62c6289b6f3e6a228cb8b1f3abd582a4a4" || a.Anchors.Physical != "8169bf43d8b63207027dc93bdc02a2637248199655a47816ca6325c9ecc8746a" || a.Anchors.Refinement != "ae2c1c14ee09853fe64d454d7538a8cd534520c5edf349a77c246428c2b6b017" {
		return bad("independent anchor labels differ")
	}
	ids, paths := map[string]bool{}, map[string]bool{}
	var bodyRows, actorRows, supportRows, noticeRows []string
	counts := map[string]int{}
	priorDirectNotices := map[string]next75NoticeRepresentative{}
	for ordinal, e := range a.Entries {
		expectedTodo := []int{3618, 3621, 3631}[ordinal/25]
		expectedReader := map[int]string{3618: "20261024-183318-todo-3618", 3621: "20261024-184433-todo-3621", 3631: "20261024-195114-todo-3631"}[expectedTodo]
		if e.ID == "" || ids[e.ID] || paths[e.Path] || path.Clean(e.Path) != e.Path || !strings.HasPrefix(e.Path, "skills/") || !strings.EqualFold(path.Base(e.Path), "SKILL.md") || strings.ContainsAny(e.Path, "\\:") || !x45Hex(e.SHA, 64) || !x45Hex(e.Blob, 40) || e.Bytes < 1 || e.Commit != a.Commit || e.URL != "https://github.com/"+a.Source+"/blob/"+a.Commit+"/"+e.Path || e.Read == nil || !x45True(e.Workflow) || !x45False(e.Executed) || e.Use == "" || e.Reason == "" || e.Positive == "" || e.Negative == "" {
			return bad("primary identity, examples or explicit state missing")
		}
		if e.ReaderTodo != expectedTodo || e.Reader != expectedReader || e.ReaderRow != ordinal%25+1 || e.Ordinal != ordinal+1 {
			return bad("reader provenance or ordinal differs")
		}
		ids[e.ID], paths[e.Path] = true, true
		counts["primary_bytes"] += e.Bytes
		if e.Name == "" {
			if e.NameState != "unavailable_in_received_assessment_not_invented" {
				return bad("unknown name was invented or lost its qualifier")
			}
			counts["unknown_name"]++
		} else if e.NameState != "received_declared_name" {
			return bad("name provenance missing")
		}
		if *e.Read {
			if e.Mask == nil || *e.Mask != 0 || x45String(e.ReadBy) != e.Reader || e.WorkflowBy != nil {
				return bad("raw primary actor or masking differs")
			}
			counts["primary_raw"]++
		} else {
			if e.Mask == nil || *e.Mask < 1 || e.ReadBy != nil || x45String(e.WorkflowBy) != e.Reader {
				return bad("masked primary became raw or lost its actor")
			}
			counts["primary_masked"]++
		}
		bodyRows = append(bodyRows, fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%t\t%t\n", e.Path, e.ID, e.SHA, e.Blob, e.Bytes, *e.Read, *e.Workflow))
		actorRows = append(actorRows, e.Path+"\t"+x45String(e.ReadBy)+"\t"+x45String(e.WorkflowBy)+"\n")
		supportByPath := map[string]next75Support{}
		pkgRows := []string{e.Path + "\t" + e.Blob + "\n"}
		for _, s := range e.Support {
			if _, exists := supportByPath[s.Path]; exists {
				return bad("support path repeated")
			}
			if path.Clean(s.Path) != s.Path || !strings.HasPrefix(s.Path, path.Dir(e.Path)+"/") || s.Path == e.Path || strings.ContainsAny(s.Path, "\\:") || !x45Hex(s.Blob, 40) || s.Saved == nil || s.Read == nil || s.Workflow == nil || s.Scan == nil || s.Partial == nil || s.MaskKnown == nil || (*s.MaskKnown != (s.Mask != nil)) || s.Mask != nil && *s.Mask < 0 || !x45False(s.Executed) {
				return bad("support identity, mask knowledge or explicit state missing")
			}
			stored := *s.Saved
			if stored {
				if s.Bytes == nil || *s.Bytes < 1 || !x45Hex(s.SHA, 64) {
					return bad("saved support bytes or SHA missing")
				}
				counts["saved"]++
			} else if s.Bytes != nil || s.SHA != "" || s.Mask != nil || s.State != "unsaved" {
				return bad("unsaved bytes, hash or reading were invented")
			}
			read, wf, scan, partial := false, false, false, false
			var rb, wb, sb, pb, cb string
			switch s.State {
			case "raw":
				read, wf = true, true
				rb = e.Reader
				if s.Mask == nil || *s.Mask != 0 {
					return bad("raw support is masked or unknown")
				}
			case "masked":
				wf = true
				wb = e.Reader
				if s.Mask != nil && *s.Mask < 1 {
					return bad("masked support unknown count became zero")
				}
			case "notice_ordered_diff_reported", "notice_same_bytes_reused":
				wf = true
				cb = e.Reader
				if s.Mask == nil || *s.Mask != 0 {
					return bad("notice comparison mask state differs")
				}
			case "scan", "difference_only":
				scan = true
				sb = e.Reader
			case "partial":
				partial = true
				pb = e.Reader
			case "unsaved":
				if stored {
					return bad("unsaved state has stored content")
				}
			default:
				return bad("unknown support reading method")
			}
			if !stored && (read || wf || scan || partial) || *s.Read != read || *s.Workflow != wf || *s.Scan != scan || *s.Partial != partial || x45String(s.ReadBy) != rb || x45String(s.WorkflowBy) != wb || x45String(s.ScanBy) != sb || x45String(s.PartialBy) != pb || x45String(s.CompareBy) != cb {
				return bad("partial, scan, comparison or reading actors were conflated")
			}
			counts[s.State]++
			counts["support"]++
			if wf {
				counts["workflow"]++
			}
			supportByPath[s.Path] = s
			pkgRows = append(pkgRows, s.Path+"\t"+s.Blob+"\n")
			mask, size := "unknown", "unknown"
			if s.Mask != nil {
				mask = fmt.Sprint(*s.Mask)
			}
			if s.Bytes != nil {
				size = fmt.Sprint(*s.Bytes)
			}
			supportRows = append(supportRows, fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%s\n", s.Path, s.Blob, s.SHA, size, s.State, mask, e.Reader))
		}
		if x45Digest(pkgRows) != e.PackageFingerprint {
			return bad("complete package path/blob fingerprint differs")
		}
		if len(e.Rights.Notices) == 0 || e.Rights.Copy == nil || !x45False(e.Rights.Support) {
			return bad("individual notice or rights scope absent")
		}
		noticeSeen := map[string]bool{}
		for _, n := range e.Rights.Notices {
			s, ok := supportByPath[n.Path]
			if !ok || noticeSeen[n.Path] || !x45True(s.Saved) || !x45True(s.Workflow) || n.SHA != s.SHA || n.Blob != s.Blob || s.Bytes == nil || n.Bytes != *s.Bytes || n.Read == nil || *n.Read != *s.Read || !x45True(n.Workflow) || n.Method != s.State || n.Reader != e.Reader || n.MaskKnown == nil || *n.MaskKnown != *s.MaskKnown || (n.Mask == nil) != (s.Mask == nil) || n.Mask != nil && *n.Mask != *s.Mask || n.Kind == "" || n.Copy == nil || !x45False(n.Support) || !equalNoticeRepresentatives(n.Representative, s.Representative) {
				return bad("notice identity, method, mask knowledge or actor differs")
			}
			noticeSeen[n.Path] = true
			counts["notices"]++
			if n.Method == "masked" {
				counts["masked_notices"]++
				if n.Mask == nil {
					counts["unknown_notice_masks"]++
				}
			}
			key := fmt.Sprintf("%s\t%s\t%s\t%d", e.Reader, n.SHA, n.Blob, n.Bytes)
			if n.Method == "raw" {
				if _, exists := priorDirectNotices[key]; !exists {
					priorDirectNotices[key] = next75NoticeRepresentative{e.Ordinal, e.ID, n.Path, n.SHA, n.Blob, e.Reader, n.Bytes}
				}
			}
			if n.Method == "notice_same_bytes_reused" {
				rep, exists := priorDirectNotices[key]
				if !exists || e.Ordinal <= 50 && n.Representative == nil || n.Representative != nil && *n.Representative != rep {
					return bad("notice reuse lacks the declared same-reader exact representative")
				}
			}
			if n.Method == "notice_ordered_diff_reported" && (e.Ordinal != 52 && e.Ordinal != 65 || !x45True(a.Entries[50].Rights.Notices[0].Read)) {
				return bad("ordered notice difference lacks its reported baseline")
			}
			noticeRows = append(noticeRows, fmt.Sprintf("%s\t%s\t%s\t%s\t%d\t%s\t%s\n", e.Path, n.Path, n.SHA, n.Blob, n.Bytes, n.Method, e.Reader))
		}
		if x45True(e.Rights.Copy) {
			accepted := e.Rights.Acceptance
			found := false
			if accepted != nil && accepted.Scope != "" {
				for _, n := range e.Rights.Notices {
					if n.Path == accepted.NoticePath && n.SHA == accepted.NoticeSHA && x45True(n.Copy) {
						found = true
					}
				}
			}
			if !found {
				return bad("copy grant lacks matching individual notice and scope")
			}
		}
	}
	expected := map[string]int{"primary_bytes": 373043, "primary_raw": 63, "primary_masked": 12, "unknown_name": 1, "support": 775, "saved": 466, "raw": 168, "masked": 61, "workflow": 249, "scan": 211, "difference_only": 4, "partial": 2, "unsaved": 309, "notices": 78, "masked_notices": 5, "unknown_notice_masks": 2, "notice_ordered_diff_reported": 2, "notice_same_bytes_reused": 18}
	for k, want := range expected {
		if counts[k] != want {
			return bad("independently received category counts differ")
		}
	}
	c := a.Counts
	if c.Candidates != 75 || c.PrimaryBytes != counts["primary_bytes"] || c.Raw != 63 || c.Masked != 12 || c.Workflow != 75 || c.Notices != 78 || c.RosterNotices != 77 || c.ExtraNotices != 1 || c.MaskedNotices != 5 || c.UnknownNoticeMasks != 2 || c.Support != 775 || c.Saved != 466 || c.RawSupport != 168 || c.MaskedSupport != 61 || c.SameNotices != 18 || c.DiffNotices != 2 || c.WorkflowSupport != 249 || c.Scan != 215 || c.Partial != 2 || c.Unread != 0 || c.Unsaved != 309 {
		return bad("public category projection differs")
	}
	if x45Digest(bodyRows) != next75Bodies || x45Digest(actorRows) != next75Actors || x45Digest(supportRows) != next75SupportStates || x45Digest(noticeRows) != next75NoticeIdentities {
		return bad("independent identity, actor or state anchor differs")
	}
	return nil
}

func next75PrimaryProjection(a next75Assessment) []extra45Entry {
	out := make([]extra45Entry, len(a.Entries))
	for i, e := range a.Entries {
		out[i] = e.extra45Entry
	}
	return out
}

func validateNext75Dispositions(a next75Assessment, rows []cohortLedgerRow) error {
	known := map[string]next75Entry{}
	for _, e := range a.Entries {
		known[e.ID] = e
	}
	matched := map[string]bool{}
	installed, integrated, excluded := 0, 0, 0
	for _, r := range rows {
		e, ok := known[r.ID]
		if !ok {
			continue
		}
		if matched[r.ID] || r.Source != a.Source || r.Commit != e.Commit || r.Path != e.Path || r.SHA != e.SHA {
			return errors.New("next75 disposition identity differs")
		}
		matched[r.ID] = true
		switch r.Disposition {
		case "installed_new_adaptation":
			if !e.Installed || e.Integrated || e.Excluded {
				return errors.New("next75 disposition flag differs")
			}
			installed++
		case "integrated_existing":
			if !e.Integrated || e.Installed || e.Excluded {
				return errors.New("next75 disposition flag differs")
			}
			integrated++
		case "excluded":
			if !e.Excluded || e.Installed || e.Integrated {
				return errors.New("next75 disposition flag differs")
			}
			excluded++
		default:
			return errors.New("next75 unfinished disposition counted as final")
		}
	}
	for _, e := range a.Entries {
		if (e.Installed || e.Integrated || e.Excluded) && !matched[e.ID] {
			return errors.New("next75 final flag lacks a live ledger row")
		}
	}
	if installed != a.Installed || integrated != a.Integrated || excluded != a.Excluded {
		return errors.New("next75 disposition counts differ")
	}
	return nil
}

func TestNext75KeepsPartialScanUnknownAndReusedNoticeEvidenceDistinct(t *testing.T) {
	for _, name := range []string{"valid", "masked_primary_as_raw", "forged_consistent_reader", "partial_as_full", "scan_as_full", "unknown_notice_as_zero", "unknown_unsaved_bytes", "comparison_as_raw", "forged_package", "forged_anchor_label", "changed_primary_case", "declared_representative", "unsupported_grant", "execution_claim"} {
		t.Run(name, func(t *testing.T) {
			var a next75Assessment
			loadPublicReceipt(t, "skills-3080-sixmile-next75-evaluation.json", &a)
			yes := true
			zero := 0
			changed := name == "valid"
			switch name {
			case "masked_primary_as_raw":
				for i := range a.Entries {
					e := &a.Entries[i]
					if x45False(e.Read) {
						e.Read = &yes
						changed = true
						break
					}
				}
			case "forged_consistent_reader":
				a.Entries[0].Reader = "forged"
				forged := "forged"
				e := &a.Entries[0]
				if x45True(e.Read) {
					e.ReadBy = &forged
				} else {
					e.WorkflowBy = &forged
				}
				for i := range e.Support {
					s := &e.Support[i]
					if s.ReadBy != nil {
						s.ReadBy = &forged
					}
					if s.WorkflowBy != nil {
						s.WorkflowBy = &forged
					}
					if s.ScanBy != nil {
						s.ScanBy = &forged
					}
					if s.PartialBy != nil {
						s.PartialBy = &forged
					}
					if s.CompareBy != nil {
						s.CompareBy = &forged
					}
				}
				for i := range e.Rights.Notices {
					e.Rights.Notices[i].Reader = forged
				}
				changed = true
			case "forged_package":
				a.Entries[0].PackageFingerprint = strings.Repeat("0", 64)
				changed = true
			case "forged_anchor_label":
				a.Anchors.Actors = "forged"
				changed = true
			case "changed_primary_case":
				e := &a.Entries[0]
				e.Path = strings.TrimSuffix(e.Path, "SKILL.md") + "skill.md"
				e.URL = "https://github.com/" + a.Source + "/blob/" + a.Commit + "/" + e.Path
				changed = true
			case "declared_representative":
				for i := range a.Entries {
					e := &a.Entries[i]
					for j := range e.Rights.Notices {
						n := &e.Rights.Notices[j]
						if n.Representative != nil {
							wrong := *n.Representative
							wrong.Ordinal++
							n.Representative = &wrong
							for k := range e.Support {
								if e.Support[k].Path == n.Path {
									e.Support[k].Representative = &wrong
								}
							}
							changed = true
							break
						}
					}
					if changed {
						break
					}
				}
			case "unsupported_grant":
				a.Entries[0].Rights.Copy = &yes
				changed = true
			case "execution_claim":
				a.Entries[0].Executed = &yes
				changed = true
			default:
				for i := range a.Entries {
					for j := range a.Entries[i].Support {
						s := &a.Entries[i].Support[j]
						if name == "partial_as_full" && s.State == "partial" || name == "scan_as_full" && s.State == "scan" || name == "comparison_as_raw" && s.State == "notice_ordered_diff_reported" {
							s.Read, s.Workflow = &yes, &yes
							changed = true
						} else if name == "unknown_notice_as_zero" && s.State == "masked" && s.Mask == nil {
							s.Mask = &zero
							s.MaskKnown = &yes
							changed = true
						} else if name == "unknown_unsaved_bytes" && x45False(s.Saved) {
							s.Bytes = &zero
							changed = true
						}
						if changed {
							break
						}
					}
					if changed {
						break
					}
				}
			}
			if !changed {
				t.Fatal("mutation fixture absent")
			}
			err := validateNext75Reading(a)
			if name == "valid" && err != nil {
				t.Fatal(err)
			}
			if name != "valid" && err == nil {
				t.Fatalf("%s accepted", name)
			}
		})
	}
}

func TestNext75DispositionRelationshipsAllowFutureDecisionsWithoutInventingPresentOnes(t *testing.T) {
	for _, name := range []string{"current", "synthetic_future_adoption", "missing_row", "unknown_disposition", "wrong_identity", "wrong_flag", "wrong_count", "repeated_row"} {
		t.Run(name, func(t *testing.T) {
			var a next75Assessment
			loadPublicReceipt(t, "skills-3080-sixmile-next75-evaluation.json", &a)
			var ledger struct {
				Entries  []cohortLedgerRow `json:"entries"`
				Excluded []cohortLedgerRow `json:"excluded_entries"`
			}
			loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
			rows := append(append([]cohortLedgerRow(nil), ledger.Entries...), ledger.Excluded...)
			if name != "current" {
				// Relationship fixture only, not a licence grant, deployment or actual adoption.
				rows = nil
				a.Installed, a.Integrated, a.Excluded = 0, 0, 0
				for i := range a.Entries {
					a.Entries[i].Installed, a.Entries[i].Integrated, a.Entries[i].Excluded = false, false, false
				}
				e := &a.Entries[0]
				e.Installed = true
				a.Installed = 1
				r := cohortLedgerRow{ID: e.ID, Source: a.Source, Commit: e.Commit, Path: e.Path, SHA: e.SHA, Disposition: "installed_new_adaptation"}
				if name == "unknown_disposition" {
					r.Disposition = "pending"
				}
				if name == "wrong_identity" {
					r.Path = "wrong/SKILL.md"
				}
				if name == "wrong_flag" {
					e.Installed = false
				}
				if name == "wrong_count" {
					a.Installed = 0
				}
				if name != "missing_row" {
					rows = append(rows, r)
				}
				if name == "repeated_row" {
					rows = append(rows, r)
				}
			}
			err := validateNext75Dispositions(a, rows)
			valid := name == "current" || name == "synthetic_future_adoption"
			if valid && err != nil {
				t.Fatal(err)
			}
			if !valid && err == nil {
				t.Fatalf("%s accepted", name)
			}
		})
	}
}
