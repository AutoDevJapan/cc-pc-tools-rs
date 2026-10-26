package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

type currentAdaptationFile struct {
	Index    int    `json:"index"`
	Target   string `json:"target"`
	SHA      string `json:"sha256"`
	Bytes    int    `json:"bytes"`
	Identity string `json:"created_and_current_object_identity_sha256"`
	Same     *bool  `json:"creation_ID_matches_current"`
	Buffer   *bool  `json:"same_handle_buffer_returned"`
	Reviewed *bool  `json:"reviewed_bytes_equal"`
}

func validateCurrentAdaptationFiles(rows []currentAdaptationFile, expected map[string]struct {
	sha   string
	bytes int
}, count int) error {
	if len(rows) != count || len(expected) != count {
		return errors.New("current adaptation file count differs")
	}
	seen := map[string]bool{}
	for i, row := range rows {
		e, ok := expected[row.Target]
		if !ok || row.Index != i || seen[row.Target] || e.sha != row.SHA || e.bytes != row.Bytes || !validHex(row.Identity, 64) || !recordedTrue(row.Same) || !recordedTrue(row.Buffer) || !recordedTrue(row.Reviewed) {
			return errors.New("current adaptation file lost exact output or current identity proof")
		}
		seen[row.Target] = true
	}
	return nil
}

func TestDesignAndCombatCurrentBuffersBindEveryReviewedOutput(t *testing.T) {
	for _, spec := range []struct {
		group, operation, manifest string
		count                      int
	}{
		{"design-reference", "create-design-reference-090101.json", "fc92df3fdb1338c8de904691161eeccacf8c31256dd5e5fd7a5a4eb9fad48f85", 52},
		{"combat6", "create-combat6-090101.json", "8078d9521ba61039000c915c7e904fd1aff9def8a832a19de377c4c0ac6c0758", 40},
		{"codex3", "create-codex3-090101.json", "e79f958f08c7ab4a28607ec29869d99b1fdc645758989dfa6cb08a5145cf5b06", 20},
	} {
		t.Run(spec.group, func(t *testing.T) {
			var op struct {
				Files []struct {
					Target string `json:"target"`
					SHA    string `json:"sha256"`
					Body   string `json:"content_base64"`
				} `json:"files"`
			}
			raw := loadPublicReceipt(t, "skills-3080-operations/"+spec.operation, &op)
			h := sha256.Sum256(raw)
			if hex.EncodeToString(h[:]) != spec.manifest {
				t.Fatal("reviewed operation changed")
			}
			expected := map[string]struct {
				sha   string
				bytes int
			}{}
			for _, f := range op.Files {
				b, e := base64.StdEncoding.DecodeString(f.Body)
				h := sha256.Sum256(b)
				if e != nil || hex.EncodeToString(h[:]) != f.SHA {
					t.Fatal("reviewed output bytes differ")
				}
				expected[f.Target] = struct {
					sha   string
					bytes int
				}{f.SHA, len(b)}
			}
			var result struct {
				Count      int                     `json:"qualified_files"`
				Rows       []currentAdaptationFile `json:"files"`
				Writer     string                  `json:"current_OS_writer"`
				Historical *bool                   `json:"historical_physical_continuity_proven"`
				General    *bool                   `json:"general_access_granted"`
				Registry   *bool                   `json:"original_registry_recovered"`
			}
			loadPublicReceipt(t, "skills-3080-"+spec.group+"-consume-evidence090101.json", &result)
			if result.Count != spec.count || result.Writer != "unproved" || !recordedFalse(result.Historical) || !recordedFalse(result.General) || !recordedFalse(result.Registry) {
				t.Fatal("current evidence promoted historical or general access")
			}
			for _, kind := range []string{"valid", "missing", "foreign_output", "duplicate", "changed_bytes", "no_current_ID", "no_returned_buffer", "wrong_hash"} {
				t.Run(kind, func(t *testing.T) {
					rows := append([]currentAdaptationFile(nil), result.Rows...)
					no := false
					switch kind {
					case "missing":
						rows = rows[:len(rows)-1]
					case "foreign_output":
						rows[0].Target = "unrelated/SKILL.md"
					case "duplicate":
						rows[1].Target = rows[0].Target
					case "changed_bytes":
						rows[0].Bytes++
					case "no_current_ID":
						rows[0].Same = &no
					case "no_returned_buffer":
						rows[0].Buffer = &no
					case "wrong_hash":
						rows[0].SHA = strings.Repeat("0", 64)
					}
					if err := validateCurrentAdaptationFiles(rows, expected, spec.count); (err != nil) != (kind != "valid") {
						t.Fatalf("%s: %v", kind, err)
					}
				})
			}
		})
	}
}

func TestDesign24PreservesOriginalReaderAndDoesNotInstall24Skills(t *testing.T) {
	var cohort struct {
		Reader  string `json:"reader_job"`
		Entries []struct {
			ID       string `json:"inventory_id"`
			SHA      string `json:"source_sha256"`
			Path     string `json:"path"`
			Reader   string `json:"reader_job"`
			Raw      bool   `json:"source_body_read_complete"`
			Workflow bool   `json:"source_body_workflow_read_complete"`
			Mask     int    `json:"source_body_mask_count"`
			Final    *bool  `json:"final_disposition"`
			Case     int    `json:"case_manifest_index"`
		} `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-voltagent-design24-evaluation090101.json", &cohort)
	raw, workflow := 0, 0
	ids := map[string]bool{}
	for i, e := range cohort.Entries {
		if e.ID == "" || ids[e.ID] || !validHex(e.SHA, 64) || e.Reader != "20261025-142809-todo-3778" || e.Path == "" || !recordedFalse(e.Final) || e.Case != i+3 {
			t.Fatal("source actor, scope or case identity lost")
		}
		ids[e.ID] = true
		if e.Raw {
			if e.Mask != 0 {
				t.Fatal("masked source was counted as raw")
			}
			raw++
		}
		if e.Workflow {
			workflow++
		}
	}
	if len(ids) != 24 || raw != 3 || workflow != 24 {
		t.Fatal("24 received source references were conflated with raw reads")
	}
	var p struct {
		Raw      int `json:"all_fulltext_primary_paths_including_partial_sources"`
		Workflow int `json:"all_workflow_primary_paths_including_partial_sources"`
		Design   struct {
			Raw      int `json:"full_body_read"`
			Workflow int `json:"workflow_body_read"`
		} `json:"voltagent_design24_evaluation"`
	}
	loadPublicReceipt(t, "skills-3080-progress.json", &p)
	if p.Design.Raw != raw || p.Design.Workflow != workflow {
		t.Fatal("global projection lacks the disjoint received cohort")
	}
	var designs struct {
		Targets []directSkillDesign `json:"targets"`
	}
	loadPublicReceipt(t, "skills-3080-design-reference-designs090101.json", &designs)
	if len(designs.Targets) != 1 || designs.Targets[0].Name != "design-reference-evidence" || !ids[designs.Targets[0].ID] {
		t.Fatal("reference cases became extra installed Skills")
	}
}
