package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func skillsActualNotice4Manifest(t *testing.T) skillsManifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "docs", "skills-3080-operations", "create-notice-next4-175645.json"))
	if err != nil {
		t.Fatal(err)
	}
	if hashBytes(raw) != "5ab55cfca8778374e86862905e9c39d5bfe11bea2ae6e3e33ec82f82e7607f89" {
		t.Fatal("full-reviewed immutable actual4 manifest changed")
	}
	m, ok := skillsParseManifest(raw)
	if !ok || len(m.Sources) != 4 || len(m.Files) != 28 {
		t.Fatal("same accepted4/28 manifest shape differs")
	}
	return m
}

func TestSkills3080ActualNotice4AdaptedTargetsPreserveClaims(t *testing.T) {
	if !skillsSourceClaimsPreserved(skillsActualNotice4Manifest(t)) {
		t.Fatal("accepted actual4 source/target aliases lack exact source metadata association")
	}
}

func skillsAliasedFixture(t *testing.T, flat bool) (skillsManifest, skillsAcquiredTestData) {
	t.Helper()
	raw, notice := []byte("Visible procedure\nfixture-only withheld\nKeep the original conclusion.\n"), []byte("Synthetic individual notice\n")
	f := skillsAcquiredFixture(t, raw, notice, flat)
	m := skillsManifestWithOriginals(t, raw, notice)
	m.Files[0].Target = "adapted-name/SKILL.md"
	m.Files[1] = skillsMaskedFixtureOutput([]byte("Visible procedure\n[WITHHELD physical line 2]\nKeep the original conclusion.\n"))
	m.Files[1].Target = "adapted-name/references/original-skill.masked.txt"
	m.Files[2].Target = "adapted-name/references/LICENSE.txt"
	blob := sha1.Sum(append([]byte("blob "+strconv.Itoa(len(raw))+"\x00"), raw...))
	meta := map[string]any{"source": m.Sources[0], "reformulation_writer": m.Writer.Job, "source_bytes": len(raw), "source_git_blob": hex.EncodeToString(blob[:])}
	b, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	m.Files = append(m.Files, skillsOutput{Target: "adapted-name/references/source.json"})
	skillsAliasSetBody(&m.Files[3], b)
	return m, f
}

func skillsAliasSetBody(output *skillsOutput, body []byte) {
	output.body = body
	output.SHA = hashBytes(body)
	output.Content = base64.StdEncoding.EncodeToString(body)
}

func TestSkills3080AliasedReferenceKeepsOriginalObjectAndNamespace(t *testing.T) {
	for _, flat := range []bool{false, true} {
		for _, kind := range []string{"valid", "legacy_valid", "raw_valid", "missing_metadata", "flat_alias_is_not_legacy", "different_repo", "different_commit", "different_path", "different_sha", "different_notice", "different_scope", "different_writer", "legacy_bad_writer", "raw_bad_writer", "legacy_bad_view", "raw_bad_view", "different_bytes", "fractional_bytes", "different_blob", "duplicate_key", "case_alias", "source_case_alias", "unknown_source_field", "duplicate_binding", "changed_metadata_hash", "other_metadata_namespace", "other_reference_namespace", "other_notice_namespace", "changed_visible_line", "wrong_marker", "all_withheld"} {
			t.Run(strconv.FormatBool(flat)+"/"+kind, func(t *testing.T) {
				m, f := skillsAliasedFixture(t, flat)
				meta, ok := conversationJSON(m.Files[3].body)
				if !ok {
					t.Fatal("synthetic source metadata invalid")
				}
				source := obj(meta["source"])
				switch kind {
				case "flat_alias_is_not_legacy":
					delete(meta, "source")
					for _, key := range []string{"repository", "commit", "path", "sha256"} {
						meta[key] = source[key]
					}
					meta["license_path"] = obj(source["license"])["path"]
					meta["license_sha256"] = obj(source["license"])["sha256"]
				case "different_repo":
					source["repository"] = "other/public"
				case "different_commit":
					source["commit"] = strings.Repeat("a", 40)
				case "different_path":
					source["path"] = "skills/foreign/SKILL.md"
				case "different_sha":
					source["sha256"] = strings.Repeat("a", 64)
				case "different_notice":
					obj(source["license"])["path"] = "foreign/LICENSE"
				case "different_scope":
					obj(source["license"])["scope"] = "different scope"
				case "different_writer", "legacy_bad_writer", "raw_bad_writer":
					meta["reformulation_writer"] = "20261024-000001-todo-3080"
				case "different_bytes":
					meta["source_bytes"] = 1
				case "fractional_bytes":
					meta["source_bytes"] = 1.5
				case "different_blob":
					meta["source_git_blob"] = strings.Repeat("a", 40)
				case "case_alias":
					meta["Source"] = meta["source"]
				case "source_case_alias":
					source["Repository"] = source["repository"]
				case "unknown_source_field":
					source["new_permission"] = true
				}
				b, err := json.Marshal(meta)
				if err != nil {
					t.Fatal(err)
				}
				if kind == "duplicate_key" {
					b = bytes.Replace(b, []byte(`"source":`), []byte(`"source":null,"source":`), 1)
				}
				skillsAliasSetBody(&m.Files[3], b)
				switch kind {
				case "legacy_bad_writer", "legacy_valid", "legacy_bad_view":
					legacy := m.Files[1]
					legacy.Target = "sample/references/original-skill.masked.txt"
					m.Files = append(m.Files, legacy)
				case "raw_bad_writer", "raw_valid", "raw_bad_view":
					original := skillsOutput{Target: "sample/references/upstream.txt"}
					skillsAliasSetBody(&original, []byte("Visible procedure\nfixture-only withheld\nKeep the original conclusion.\n"))
					m.Files = append(m.Files, original)
				case "missing_metadata":
					m.Files = m.Files[:3]
				case "duplicate_binding":
					for _, output := range append([]skillsOutput(nil), m.Files...) {
						output.Target = strings.Replace(output.Target, "adapted-name/", "second-name/", 1)
						m.Files = append(m.Files, output)
					}
				case "changed_metadata_hash":
					m.Files[3].body = append(m.Files[3].body, ' ')
				case "other_metadata_namespace":
					m.Files[3].Target = "foreign/references/source.json"
				case "other_reference_namespace":
					m.Files[1].Target = "foreign/references/original-skill.masked.txt"
				case "other_notice_namespace":
					m.Files[2].Target = "foreign/references/LICENSE.txt"
				case "changed_visible_line":
					skillsAliasSetBody(&m.Files[1], []byte("Changed procedure\n[WITHHELD physical line 2]\nKeep the original conclusion.\n"))
				case "wrong_marker":
					skillsAliasSetBody(&m.Files[1], []byte("Visible procedure\n[WITHHELD physical line 3]\nKeep the original conclusion.\n"))
				case "all_withheld":
					skillsAliasSetBody(&m.Files[1], []byte("[WITHHELD physical line 1]\n[WITHHELD physical line 2]\n[WITHHELD physical line 3]\n"))
				}
				if kind == "legacy_bad_view" || kind == "raw_bad_view" {
					skillsAliasSetBody(&m.Files[1], []byte("Changed procedure\n[WITHHELD physical line 2]\nKeep the original conclusion.\n"))
				}
				fs := skillsFS{blocked: func(string) bool { return false }}
				defer fs.close()
				got := skillsSourcesBoundAt(m, &fs, f.root, f.catalogSHA, f.anchors)
				if got != (kind == "valid" || kind == "legacy_valid" || kind == "raw_valid") {
					t.Fatalf("alias tuple/object/namespace case %s accepted=%v stage=%s", kind, got, fs.failureStage)
				}
			})
		}
	}
}
