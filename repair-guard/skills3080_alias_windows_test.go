package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func skillsActualNotice4(t *testing.T) skillsManifest {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join("..", "docs", "skills-3080-operations", "create-notice-next4-175645.json"))
	if e != nil || hashBytes(raw) != "5ab55cfca8778374e86862905e9c39d5bfe11bea2ae6e3e33ec82f82e7607f89" {
		t.Fatal("immutable public actual4 manifest")
	}
	m, ok := skillsParseManifest(raw)
	if !ok || len(m.Sources) != 4 || len(m.Files) != 28 {
		t.Fatal("actual4/28 parse")
	}
	return m
}
func TestSkills3080ActualNotice4AllSourceClaimsPreserved(t *testing.T) {
	m := skillsActualNotice4(t)
	bound, ok := skillsMaskedBindings(m)
	if !ok || len(bound) != 4 {
		t.Fatal("actual4 explicit bindings missing")
	}
	expected := map[string]string{"rlm-controller": "rlm-controller", "parse-qr": "parse-payment-qr", "payroll": "payroll-batch-reconciliation", "payment-status": "payment-status-reconciliation"}
	for _, s := range m.Sources {
		parts := strings.Split(s.Path, "/")
		name := parts[len(parts)-2]
		if bound[skillsSourceKey(s.Repository, s.Commit, s.Path)] != expected[name]+"/references/original-skill.masked.txt" {
			t.Fatal("wrong actual namespace correspondence")
		}
	}
	if !skillsSourceClaimsPreserved(m) {
		t.Fatal("actual immutable4/28 manifest rejected despite explicit same-manifest source metadata")
	}
}
func skillsAliasReplaceOutput(t *testing.T, m *skillsManifest, target string, body []byte) {
	t.Helper()
	for i := range m.Files {
		if m.Files[i].Target == target {
			m.Files[i].body = body
			m.Files[i].SHA = hashBytes(body)
			m.Files[i].Content = base64.StdEncoding.EncodeToString(body)
			return
		}
	}
	t.Fatal("fixture output absent")
}
func TestSkills3080ActualAliasMetadataCannotBeMissingConflictingOrCrossBound(t *testing.T) {
	for _, kind := range []string{"missing_meta", "missing_writer", "wrong_writer", "repo", "commit", "path", "sha", "acquisition", "notice_path", "notice_sha", "notice_scope", "notice_accepted", "unknown_source_field", "duplicate_source_key", "duplicate_binding", "other_namespace", "missing_entry", "missing_notice", "notice_bytes", "missing_masked", "masked_target", "manifest_writer"} {
		t.Run(kind, func(t *testing.T) {
			m := skillsActualNotice4(t)
			target := "parse-payment-qr/references/source.json"
			var raw []byte
			for _, f := range m.Files {
				if f.Target == target {
					raw = f.body
				}
			}
			var meta map[string]any
			if json.Unmarshal(raw, &meta) != nil {
				t.Fatal("actual metadata")
			}
			source := meta["source"].(map[string]any)
			notice := source["license"].(map[string]any)
			remove := ""
			switch kind {
			case "missing_meta":
				remove = target
			case "missing_writer":
				delete(meta, "reformulation_writer")
			case "wrong_writer":
				meta["reformulation_writer"] = "20261028-103853-todo-3686"
			case "repo":
				source["repository"] = "other/skills"
			case "commit":
				source["commit"] = strings.Repeat("a", 40)
			case "path":
				source["path"] = "skills/other/SKILL.md"
			case "sha":
				source["sha256"] = strings.Repeat("b", 64)
			case "acquisition":
				source["acquisition"] = "newly-acquired"
			case "notice_path":
				notice["path"] = "LICENSE"
			case "notice_sha":
				notice["sha256"] = strings.Repeat("c", 64)
			case "notice_scope":
				notice["scope"] = "arbitrary broader permission"
			case "notice_accepted":
				notice["accepted"] = false
			case "unknown_source_field":
				source["alias"] = "other"
			case "duplicate_source_key":
				b, _ := json.Marshal(meta)
				b = []byte(strings.Replace(string(b), "\"reformulation_writer\":", "\"reformulation_writer\":\"duplicate\",\"reformulation_writer\":", 1))
				skillsAliasReplaceOutput(t, &m, target, b)
				if skillsSourceClaimsPreserved(m) {
					t.Fatal("duplicate metadata key accepted")
				}
				return
			case "duplicate_binding":
				other := "payment-status-reconciliation/references/source.json"
				skillsAliasReplaceOutput(t, &m, other, raw)
			case "other_namespace":
				for i := range m.Files {
					if m.Files[i].Target == target {
						m.Files[i].Target = "unbound/references/source.json"
					}
				}
			case "missing_entry":
				remove = "parse-payment-qr/SKILL.md"
			case "missing_notice":
				remove = "parse-payment-qr/references/LICENSE.txt"
			case "notice_bytes":
				skillsAliasReplaceOutput(t, &m, "parse-payment-qr/references/LICENSE.txt", []byte("different notice"))
			case "missing_masked":
				remove = "parse-payment-qr/references/original-skill.masked.txt"
			case "masked_target":
				for i := range m.Files {
					if m.Files[i].Target == "parse-payment-qr/references/original-skill.masked.txt" {
						m.Files[i].Target = "other/references/original-skill.masked.txt"
					}
				}
			case "manifest_writer":
				m.Writer.Job = "20261028-103853-todo-3686"
			}
			if remove != "" {
				files := m.Files[:0]
				for _, f := range m.Files {
					if f.Target != remove {
						files = append(files, f)
					}
				}
				m.Files = files
			} else if kind != "other_namespace" && kind != "masked_target" {
				b, _ := json.Marshal(meta)
				skillsAliasReplaceOutput(t, &m, target, b)
			}
			if skillsSourceClaimsPreserved(m) {
				t.Fatalf("unbound actual alias %s accepted", kind)
			}
		})
	}
}
func TestSkills3080RenamedReferenceStillUsesSameAcquiredObjectAndLines(t *testing.T) {
	raw, notice := []byte("Visible\nfixture hidden\n"), []byte("exact package notice\n")
	f := skillsAcquiredFixture(t, raw, notice, true)
	m := skillsManifestWithOriginals(t, raw, notice)
	m.Files[1] = skillsMaskedFixtureOutput([]byte(skillsModificationNoticePrefix + "Visible\n[WITHHELD physical line 2]\n"))
	for i := range m.Files {
		m.Files[i].Target = strings.Replace(m.Files[i].Target, "sample/", "renamed/", 1)
	}
	meta, _ := json.Marshal(map[string]any{"source": m.Sources[0], "reformulation_writer": m.Writer.Job})
	m.Files = append(m.Files, skillsOutput{Target: "renamed/references/source.json", SHA: hashBytes(meta), body: meta})
	check := func(m skillsManifest) bool {
		fs := skillsFS{blocked: func(string) bool { return false }}
		defer fs.close()
		return skillsSourcesBoundAt(m, &fs, f.root, f.catalogSHA, f.anchors)
	}
	if !check(m) {
		t.Fatal("same acquired source/object/notice with explicit renamed namespace rejected")
	}
	// Raw source in a different output cannot excuse missing alias metadata or
	// a damaged masked reference. This is synthetic source, not acquired corpus.
	m.Files = append(m.Files, skillsOutput{Target: "renamed/references/raw-fixture.txt", SHA: hashBytes(raw), body: raw})
	missing := m
	missing.Files = append([]skillsOutput(nil), m.Files[:len(m.Files)-2]...)
	missing.Files = append(missing.Files, m.Files[len(m.Files)-1])
	if check(missing) {
		t.Fatal("raw copy bypassed missing explicit alias metadata")
	}
	wrongWriter := m
	wrongWriter.Files = append([]skillsOutput(nil), m.Files...)
	badMeta, _ := json.Marshal(map[string]any{"source": m.Sources[0], "reformulation_writer": "20261028-103853-todo-3686"})
	skillsAliasReplaceOutput(t, &wrongWriter, "renamed/references/source.json", badMeta)
	if check(wrongWriter) {
		t.Fatal("raw copy bypassed the original metadata writer")
	}
	dual := m
	dual.Files = append([]skillsOutput(nil), m.Files...)
	entry := m.Files[0]
	entry.Target = "sample/SKILL.md"
	dual.Files = append(dual.Files, entry, skillsMaskedFixtureOutput([]byte("Visible\n[WITHHELD physical line 2]\n")))
	skillsAliasReplaceOutput(t, &dual, "renamed/references/original-skill.masked.txt", []byte("Changed alias\n[WITHHELD physical line 2]\n"))
	for i := 0; i < 32; i++ {
		if _, ok := skillsMaskedBindings(dual); ok || check(dual) {
			t.Fatal("legacy fallback overwrote explicit alias or permitted ambiguous dual references")
		}
	}
	for _, view := range []string{skillsModificationNoticePrefix + "Changed\n[WITHHELD physical line 2]\n", skillsModificationNoticePrefix + "Visible\n[WITHHELD physical line 3]\n"} {
		c := m
		c.Files = append([]skillsOutput(nil), m.Files...)
		skillsAliasReplaceOutput(t, &c, "renamed/references/original-skill.masked.txt", []byte(view))
		if check(c) {
			t.Fatal("renamed target bypassed original line/marker check")
		}
	}
}
func TestSkills3080AliasSuccessorRetainsOnlyKnownInstalledPair(t *testing.T) {
	const sha = "50c817a829ecb33c30e4ee2b129dee3716f822fc4627dd01df6f24cb98a7925b"
	const head = "34cd8877587596539e8b4a8847468fc74fca8e57"
	if skillsGuardBaselines[sha] != head || !official3894InstalledBaseline(sha, head) {
		t.Fatal("independently installed PR615 pair lost")
	}
	if skillsGuardBaselines[sha] == strings.Repeat("a", 40) || official3894InstalledBaseline(sha, strings.Repeat("a", 40)) || official3894InstalledBaseline(strings.Repeat("b", 64), head) {
		t.Fatal("unknown software pair accepted")
	}
}
