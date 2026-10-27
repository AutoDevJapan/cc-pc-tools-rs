package main

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func skillsMaskedFixtureOutput(raw []byte) skillsOutput {
	return skillsOutput{Target: "sample/references/original-skill.masked.txt", SHA: hashBytes(raw), Content: base64.StdEncoding.EncodeToString(raw), body: raw}
}

func TestSkills3080MaskedSourcePreservesAcquiredOriginal(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		t.Run(strings.ReplaceAll(ending, "\r", "CR"), func(t *testing.T) {
			original := []byte("# Public procedure" + ending + "fixture-only withheld line" + ending + "Compare the same conditions." + ending)
			notice := []byte("synthetic individual notice\n")
			f := skillsAcquiredFixture(t, original, notice, true)
			for _, kind := range []string{"valid", "changed_visible", "wrong_ordinal", "omitted_line", "extra_line", "reordered_lines", "all_withheld", "missing_notice", "masked_notice", "other_target", "not_masked_name", "changed_original_sha"} {
				t.Run(kind, func(t *testing.T) {
					view := []byte("# Public procedure\n[WITHHELD physical line 2]\nCompare the same conditions.\n")
					m := skillsManifestWithOriginals(t, original, notice)
					switch kind {
					case "changed_visible":
						view = []byte("# Changed procedure\n[WITHHELD physical line 2]\nCompare the same conditions.\n")
					case "wrong_ordinal":
						view = []byte("# Public procedure\n[WITHHELD physical line 3]\nCompare the same conditions.\n")
					case "omitted_line":
						view = []byte("# Public procedure\nCompare the same conditions.\n")
					case "extra_line":
						view = append(view, []byte("Invented extra line.\n")...)
					case "reordered_lines":
						view = []byte("Compare the same conditions.\n[WITHHELD physical line 2]\n# Public procedure\n")
					case "all_withheld":
						view = []byte("[WITHHELD physical line 1]\n[WITHHELD physical line 2]\n[WITHHELD physical line 3]\n")
					}
					m.Files[1] = skillsMaskedFixtureOutput(view)
					switch kind {
					case "missing_notice":
						m.Files = m.Files[:2]
					case "masked_notice":
						m.Files[2] = skillsMaskedFixtureOutput([]byte("[WITHHELD physical line 1]\n"))
						m.Files[2].Target = "sample/references/LICENSE.txt"
					case "other_target":
						m.Files[1].Target = "other/references/original-skill.masked.txt"
					case "not_masked_name":
						m.Files[1].Target = "sample/references/summary.txt"
					case "changed_original_sha":
						m.Sources[0].SHA = strings.Repeat("d", 64)
					}
					fs := skillsFS{blocked: func(string) bool { return false }}
					defer fs.close()
					got := skillsSourcesBoundAt(m, &fs, f.root, f.catalogSHA, f.anchors)
					if got != (kind == "valid") {
						t.Fatalf("masked preservation %s accepted=%v stage=%s", kind, got, fs.failureStage)
					}
				})
			}
		})
	}
}

func TestSkills3080Travel3MaskedClaimsUseUnchangedAcceptedManifest(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docs", "skills-3080-operations", "create-travel3-214717.json"))
	if err != nil {
		t.Fatal(err)
	}
	if hashBytes(raw) != "11070b772c86f7ffb459be2f75eaf6d0a436ce9dfdf0d9247eb81c6e433ede51" {
		t.Fatal("original full-reviewed input changed")
	}
	m, ok := skillsParseManifest(raw)
	if !ok || len(m.Sources) != 3 || len(m.Files) != 18 {
		t.Fatal("unchanged3/18 manifest shape differs")
	}
	if !skillsSourceClaimsPreserved(m) {
		t.Fatal("accepted masked views cannot preserve claims without republishing unseen raw lines")
	}
}

func TestSkills3080MaskedViewDoesNotBecomeArbitrarySummary(t *testing.T) {
	for _, c := range []struct {
		name, raw, view string
		accepted        bool
	}{
		{"no_final_LF", "Visible\nhidden", "Visible\n[WITHHELD physical line 2]", true},
		{"CRLF_preserved_as_LF", "Visible\r\nhidden\r\n", "Visible\n[WITHHELD physical line 2]\n", true},
		{"missing_final_LF", "Visible\nhidden\n", "Visible\n[WITHHELD physical line 2]", false},
		{"invented_final_LF", "Visible\nhidden", "Visible\n[WITHHELD physical line 2]\n", false},
		{"whitespace_is_not_visible", " \nhidden\n", " \n[WITHHELD physical line 2]\n", false},
		{"no_mask_is_not_new_branch", "Visible\nhidden\n", "Visible\nhidden\n", false},
		{"short_marker", "Visible\nhidden\n", "Visible\n[WITHHELD]\n", false},
		{"number_leading_zero", "Visible\nhidden\n", "Visible\n[WITHHELD physical line 02]\n", false},
		{"lone_CR_source", "Visible\rhidden\n", "Visible\n[WITHHELD physical line 2]\n", false},
		{"NUL", "Visible\nhidden\n", "Visible\n[WITHHELD physical line 2]\x00\n", false},
		{"invalid_UTF8", "Visible\n\xff\n", "Visible\n[WITHHELD physical line 2]\n", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := skillsMaskedSourceBytes([]byte(c.raw), []byte(c.view)); got != c.accepted {
				t.Fatalf("exact line view accepted=%v, want %v", got, c.accepted)
			}
		})
	}
}

func TestSkills3080MaskedViewCannotReplaceNoticeOrSupportingScript(t *testing.T) {
	raw, view := []byte("Visible\nhidden\n"), []byte("Visible\n[WITHHELD physical line 2]\n")
	for _, c := range []struct{ role, path string }{
		{"license_or_notice", "skills/sample/SKILL.md"},
		{"support_text", "skills/sample/SKILL.md"},
		{"skill_entry", "skills/sample/script.mjs"},
		{"skill_entry", "skills/sample/skill.md"},
		{"skill_entry", "skills/sample/../SKILL.md"},
	} {
		t.Run(c.role+"/"+c.path, func(t *testing.T) {
			if skillsMaskedSourceObject(skillsSavedSource{Role: c.role, Path: c.path}, raw, []skillsOutput{skillsMaskedFixtureOutput(view)}) {
				t.Fatal("masked primary view admitted another role or source path")
			}
		})
	}
}
