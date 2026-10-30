package main

import (
	"errors"
	"strings"
	"testing"
)

const sixmilePrimaryRightsError = "next10 individual primary rights: "

func recordedTrue(v *bool) bool           { return v != nil && *v }
func recordedFalse(v *bool) bool          { return v != nil && !*v }
func recordedCount(v *int, want int) bool { return v != nil && *v == want }

func validateSixmileNext10Rights(a sixmileAssessment) error {
	copied, selected := 0, 0
	for _, e := range a.Entries {
		if e.Reader != "20261024-095453-todo-3554" {
			continue
		}
		selected++
		if e.CopyRights == nil || !recordedFalse(e.SupportRights) || !recordedFalse(e.ParentArchiveRead) || !recordedTrue(e.NoticeVerified) {
			return errors.New(sixmilePrimaryRightsError + "missing explicit primary/support/archive scope")
		}
		if !*e.CopyRights {
			continue
		}
		copied++
		g := e.Grant
		if e.Path != "skills/aabbcc456aa/hxl-code-reviewer/SKILL.md" || e.SHA != "0f4592dcb53cf2b5d6b7febee6b4152018b565551a1c29e3c612f57b218ab295" || e.Blob != "8a1a77a47d141967b246adb4da4f91037578ff7d" || e.Bytes != 9092 || !recordedFalse(e.ParentArchiveAlias) || !recordedTrue(e.ParentOfficialRead) || !recordedTrue(e.ParentOfficialPrimary) || g.ID != "Apache-2.0" || g.Repo != "anthropics/skills" || g.Commit != "dbd4588f9e1033efb41dad4bef2f7947c8993d44" || g.Path != "skills/mcp-builder/LICENSE.txt" || g.SHA != "bc6b3af2f331cbc7fb0da1344efb2cbe5877a31498b4d70dbc7000f3405a1362" || g.Blob != "4f881c52d1f72f4cfb720e339e2d35c3058d01a9" || g.Bytes != 11345 || !recordedTrue(g.Read) || e.Official.Commit != g.Commit || !recordedTrue(e.Official.Identical) {
			return errors.New(sixmilePrimaryRightsError + "official grant was confused with archive or other entrypoint")
		}
		primary, notice := 0, 0
		for _, f := range e.Official.Files {
			switch f.Path {
			case "skills/mcp-builder/SKILL.md":
				if f.SHA != e.SHA || f.Blob != e.Blob || f.Bytes != e.Bytes {
					return errors.New(sixmilePrimaryRightsError + "official primary identity changed")
				}
				primary++
			case "skills/mcp-builder/LICENSE.txt":
				if f.SHA != g.SHA || f.Blob != g.Blob || f.Bytes != g.Bytes {
					return errors.New(sixmilePrimaryRightsError + "official individual notice changed")
				}
				notice++
			}
		}
		if primary != 1 || notice != 1 {
			return errors.New(sixmilePrimaryRightsError + "official primary/notice correspondence absent or repeated")
		}
	}
	if selected != 10 || copied != 1 {
		return errors.New(sixmilePrimaryRightsError + "only the fixed official MCP primary has accepted copy rights in this cohort")
	}
	return nil
}

func TestSixmileOfficialPrimaryRightsDoNotApproveArchiveSupportOrOtherEntries(t *testing.T) {
	for _, kind := range []string{"valid", "other_entry_copy", "archive_parent_read", "official_notice_sha", "support_rights", "missing_copy_scope", "official_primary_read_missing", "official_primary_blob", "official_commit"} {
		t.Run(kind, func(t *testing.T) {
			a, _ := loadSixmileProvenanceFixture(t)
			changed := kind == "valid"
			yes := true
			for i := range a.Entries {
				e := &a.Entries[i]
				if e.Reader != "20261024-095453-todo-3554" {
					continue
				}
				mcp := e.Path == "skills/aabbcc456aa/hxl-code-reviewer/SKILL.md"
				if !mcp && (kind == "other_entry_copy" || kind == "missing_copy_scope" || kind == "support_rights") {
					switch kind {
					case "other_entry_copy":
						e.CopyRights = &yes
					case "missing_copy_scope":
						e.CopyRights = nil
					case "support_rights":
						e.SupportRights = &yes
					}
					changed = true
					break
				}
				if !mcp {
					continue
				}
				switch kind {
				case "archive_parent_read":
					e.ParentArchiveRead = &yes
					changed = true
				case "official_notice_sha":
					e.Grant.SHA = strings.Repeat("e", 64)
					changed = true
				case "official_primary_read_missing":
					e.ParentOfficialPrimary = nil
					changed = true
				case "official_commit":
					e.Official.Commit = strings.Repeat("e", 40)
					changed = true
				case "official_primary_blob":
					for j := range e.Official.Files {
						if e.Official.Files[j].Path == "skills/mcp-builder/SKILL.md" {
							e.Official.Files[j].Blob = strings.Repeat("e", 40)
							changed = true
							break
						}
					}
				}
				if changed && kind != "valid" {
					break
				}
			}
			if !changed {
				t.Fatal("no applicable fixed rights record to mutate")
			}
			err := validateSixmileNext10Rights(a)
			if (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
			if kind != "valid" && !strings.Contains(err.Error(), sixmilePrimaryRightsError) {
				t.Fatalf("mutation failed for another reason: %v", err)
			}
		})
	}
}

func loadSixmileProvenanceFixture(t *testing.T) (sixmileAssessment, []partialAdoption) {
	t.Helper()
	var a sixmileAssessment
	var ledger struct {
		Rows     []partialAdoption `json:"entries"`
		Excluded []partialAdoption `json:"excluded_entries"`
	}
	loadPublicReceipt(t, "skills-3080-sixmile-partial-evaluation.json", &a)
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	return a, append(ledger.Rows, ledger.Excluded...)
}

func TestSixmileNext10ReadersAndScannersRemainDistinct(t *testing.T) {
	for _, kind := range []string{"valid", "wrong_scanner", "scan_claims_reader", "read_loses_actor", "read_claims_scanner", "unsaved_claims_actor"} {
		t.Run(kind, func(t *testing.T) {
			a, ledger := loadSixmileProvenanceFixture(t)
			changed := kind == "valid"
			for i := range a.Entries {
				e := &a.Entries[i]
				if changed || e.Reader != "20261024-095453-todo-3554" {
					continue
				}
				for j := range e.Support {
					s := &e.Support[j]
					if ((kind == "wrong_scanner" || kind == "scan_claims_reader") && !s.Scan) || ((kind == "read_loses_actor" || kind == "read_claims_scanner") && !s.Read) || (kind == "unsaved_claims_actor" && s.Saved) {
						continue
					}
					actor := e.Reader
					switch kind {
					case "wrong_scanner":
						actor = "different-job"
						s.ScanBy = &actor
					case "scan_claims_reader", "unsaved_claims_actor":
						s.ReadBy = &actor
					case "read_loses_actor":
						s.ReadBy = nil
					case "read_claims_scanner":
						s.ScanBy = &actor
					}
					changed = true
					break
				}
			}
			if !changed {
				t.Fatal("no eligible actor provenance to mutate")
			}
			err := validateSixmileAssessment(a, ledger)
			if (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
			if kind != "valid" && !strings.Contains(err.Error(), resourceActorError) {
				t.Fatalf("mutation failed for a different reason: %v", err)
			}
		})
	}
}

func TestPrimaryReadersRemainBoundToIndependentPathRosters(t *testing.T) {
	for _, kind := range []string{"valid", "sixmile_missing", "sixmile_changed", "sixmile_same_count_other_path", "mengto_swapped"} {
		t.Run(kind, func(t *testing.T) {
			a, ledger := loadSixmileProvenanceFixture(t)
			var m struct {
				Entries []resourceReview `json:"entries"`
			}
			loadPublicReceipt(t, "skills-3080-mengto-partial-evaluation.json", &m)
			changed := kind == "valid"
			if strings.HasPrefix(kind, "sixmile_") {
				for i := range a.Entries {
					if a.Entries[i].Reader == "20261024-095453-todo-3554" {
						a.Entries[i].Reader = ""
						if kind == "sixmile_changed" {
							a.Entries[i].Reader = "different-job"
						}
						changed = true
						break
					}
				}
			}
			if kind == "sixmile_same_count_other_path" {
				for i := range a.Entries {
					if a.Entries[i].Reader != "20261024-095453-todo-3554" && a.Entries[i].Reader != "" {
						a.Entries[i].Reader = "20261024-095453-todo-3554"
						break
					}
				}
				count := 0
				for _, e := range a.Entries {
					if e.Reader == "20261024-095453-todo-3554" {
						count++
					}
				}
				if count != 10 {
					t.Fatal("mutation must preserve the ten-reader count")
				}
			}
			if kind == "mengto_swapped" {
				for i := range m.Entries {
					if m.Entries[i].Reader == "20261024-081950-todo-3535" {
						m.Entries[i].Reader = "20261024-082550-todo-3536"
						changed = true
						break
					}
				}
			}
			if !changed {
				t.Fatal("no eligible primary reader to mutate")
			}
			err := validateSixmileAssessment(a, ledger)
			if kind == "mengto_swapped" {
				err = validateMengtoFinal50Resources(m.Entries)
			}
			if (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
			if kind != "valid" && !strings.Contains(err.Error(), "primary reader attribution") {
				t.Fatalf("mutation failed for a different reason: %v", err)
			}
		})
	}
}
