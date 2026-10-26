package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// Independently pinned synthetic index/tree/object, deliberately absent from
// the candidate catalog. Repinning mutations tests semantics, not just hashes.
func skillsSupportFixture(t *testing.T, mutate func(*skillsSavedSource, *skillsManifest, *[]map[string]string)) (skillsAcquiredTestData, skillsManifest) {
	t.Helper()
	f := skillsAcquiredFixture(t, []byte("primary\n"), []byte("root notice\n"), false)
	m := skillsManifestWithOriginals(t, []byte("primary\n"), []byte("root notice\n"))
	raw := []byte("already acquired Markdown procedure\n")
	blob := sha1.Sum(append([]byte("blob "+strconv.Itoa(len(raw))+"\x00"), raw...))
	row := skillsSavedSource{Repo: "6mile/skills", Commit: skillsPublicSources["6mile/skills"], Path: "skills/sample/references/implementation.md", Role: "support_text", Blob: hex.EncodeToString(blob[:]), SHA: hashBytes(raw), Bytes: int64(len(raw)), Content: "objects/" + hashBytes(raw) + ".gz"}
	support := m.Sources[0]
	support.Path, support.SHA = row.Path, row.SHA
	m.Sources = append(m.Sources, support)
	m.Files = append(m.Files, skillsOutput{Target: "sample/references/support.md", SHA: row.SHA, body: raw})
	var tree struct {
		SHA       string              `json:"sha"`
		Tree      []map[string]string `json:"tree"`
		Truncated bool                `json:"truncated"`
	}
	b, e := os.ReadFile(f.tree)
	if e != nil || json.Unmarshal(b, &tree) != nil {
		t.Fatal("fixture tree")
	}
	tree.Tree = append(tree.Tree, map[string]string{"path": row.Path, "sha": row.Blob, "type": "blob", "mode": "100644"})
	if mutate != nil {
		mutate(&row, &m, &tree.Tree)
	}
	root := filepath.Dir(f.index)
	var compressed bytes.Buffer
	z := gzip.NewWriter(&compressed)
	_, _ = z.Write(raw)
	_ = z.Close()
	if e = os.WriteFile(filepath.Join(root, filepath.FromSlash(row.Content)), compressed.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(f.index)
	line, _ := json.Marshal(row)
	b = append(b, append(line, '\n')...)
	if e = os.WriteFile(f.index, b, 0600); e != nil {
		t.Fatal(e)
	}
	a := f.anchors[row.Repo]
	a.IndexSHA = hashBytes(b)
	// Mutations may change the repo field; retain the actual fixture anchor.
	if a.CommitSHA == "" {
		a = f.anchors["6mile/skills"]
		a.IndexSHA = hashBytes(b)
	}
	b, _ = json.Marshal(tree)
	a.TreeSHA = hashBytes(b)
	if e = os.WriteFile(f.tree, b, 0600); e != nil {
		t.Fatal(e)
	}
	f.anchors["6mile/skills"] = a
	return f, m
}

func skillsCheckSupport(t *testing.T, f skillsAcquiredTestData, m skillsManifest) bool {
	t.Helper()
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	return skillsSourcesBoundAt(m, &fs, f.root, f.catalogSHA, f.anchors)
}

func TestSkills3080SavedSupportWithCatalogPrimary(t *testing.T) {
	f, m := skillsSupportFixture(t, nil)
	if !skillsCheckSupport(t, f, m) {
		t.Fatal("saved support in the same primary package was rejected")
	}
}

func TestSkills3080SavedSupportRetainsRootAndIndividualNotices(t *testing.T) {
	for _, retain := range []bool{true, false} {
		t.Run(strconv.FormatBool(retain), func(t *testing.T) {
			f, m := skillsSupportFixture(t, nil)
			raw := []byte("individual package notice\n")
			blob := sha1.Sum(append([]byte("blob "+strconv.Itoa(len(raw))+"\x00"), raw...))
			row := skillsSavedSource{Repo: "6mile/skills", Commit: skillsPublicSources["6mile/skills"], Path: "skills/sample/NOTICE.md", Role: "license_or_notice", Blob: hex.EncodeToString(blob[:]), SHA: hashBytes(raw), Bytes: int64(len(raw)), Content: "objects/" + hashBytes(raw) + ".gz"}
			var compressed bytes.Buffer
			z := gzip.NewWriter(&compressed)
			_, _ = z.Write(raw)
			_ = z.Close()
			if e := os.WriteFile(filepath.Join(filepath.Dir(f.index), filepath.FromSlash(row.Content)), compressed.Bytes(), 0600); e != nil {
				t.Fatal(e)
			}
			index, _ := os.ReadFile(f.index)
			line, _ := json.Marshal(row)
			index = append(index, append(line, '\n')...)
			if e := os.WriteFile(f.index, index, 0600); e != nil {
				t.Fatal(e)
			}
			var tree struct {
				SHA       string              `json:"sha"`
				Tree      []map[string]string `json:"tree"`
				Truncated bool                `json:"truncated"`
			}
			b, _ := os.ReadFile(f.tree)
			if json.Unmarshal(b, &tree) != nil {
				t.Fatal("fixture tree")
			}
			tree.Tree = append(tree.Tree, map[string]string{"path": row.Path, "sha": row.Blob, "type": "blob", "mode": "100644"})
			b, _ = json.Marshal(tree)
			if e := os.WriteFile(f.tree, b, 0600); e != nil {
				t.Fatal(e)
			}
			a := f.anchors[row.Repo]
			a.IndexSHA, a.TreeSHA = hashBytes(index), hashBytes(b)
			f.anchors[row.Repo] = a
			if retain {
				m.Files = append(m.Files, skillsOutput{Target: "sample/references/NOTICE.md", SHA: row.SHA, body: raw})
			}
			if skillsCheckSupport(t, f, m) != retain {
				t.Fatal("applicable individual notice was not tied to retained bytes")
			}
		})
	}
}

func TestSkills3080SavedSupportRejectsUnboundInputs(t *testing.T) {
	for _, kind := range []string{"no_primary", "other_package", "other_repo", "role", "script", "bytes", "blob", "hash", "notice", "source_not_preserved", "notice_not_preserved", "nested_package", "unretained_notice", "missing_object", "catalog_hold", "catalog_digest", "catalog_eof", "protected"} {
		t.Run(kind, func(t *testing.T) {
			f, m := skillsSupportFixture(t, func(row *skillsSavedSource, m *skillsManifest, tree *[]map[string]string) {
				switch kind {
				case "no_primary":
					m.Sources = m.Sources[1:]
				case "other_package":
					row.Path = "skills/other/references/implementation.md"
					m.Sources[1].Path = row.Path
					(*tree)[2]["path"] = row.Path
				case "other_repo":
					row.Repo = "MengTo/Skills"
				case "role":
					row.Role = "support_script"
				case "script":
					row.Path = "skills/sample/references/run.js"
					m.Sources[1].Path = row.Path
					(*tree)[2]["path"] = row.Path
				case "bytes":
					row.Bytes++
				case "blob":
					row.Blob = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
					(*tree)[2]["sha"] = row.Blob
				case "hash":
					m.Sources[1].SHA = m.Sources[0].SHA
				case "notice":
					m.Sources[1].License.SHA = m.Sources[0].SHA
				case "source_not_preserved":
					m.Files = m.Files[:len(m.Files)-1]
				case "notice_not_preserved":
					m.Files = append(m.Files[:2], m.Files[3:]...)
				case "nested_package":
					*tree = append(*tree, map[string]string{"path": "skills/sample/references/SKILL.md", "sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "type": "blob", "mode": "100644"})
				case "unretained_notice":
					*tree = append(*tree, map[string]string{"path": "skills/sample/NOTICE.md", "sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "type": "blob", "mode": "100644"})
				}
			})
			switch kind {
			case "missing_object":
				if e := os.Remove(filepath.Join(filepath.Dir(f.index), "objects/"+m.Sources[1].SHA+".gz")); e != nil {
					t.Fatal(e)
				}
			case "catalog_hold":
				path := filepath.Join(f.root, "phase-a-catalog.jsonl")
				b, _ := os.ReadFile(path)
				r := map[string]any{"repo": "6mile/skills", "commit": m.Sources[1].Commit, "path": m.Sources[1].Path, "sha256": m.Sources[1].SHA, "machine_body_read": false, "inventory_unit": "unread_additional_text_support_shape"}
				line, _ := json.Marshal(r)
				b = append(b, append(line, '\n')...)
				_ = os.WriteFile(path, b, 0600)
				f.catalogSHA = hashBytes(b)
			case "catalog_digest", "catalog_eof":
				path := filepath.Join(f.root, "phase-a-catalog.jsonl")
				b, _ := os.ReadFile(path)
				if kind == "catalog_eof" {
					b = append(b, []byte("not-json\n")...)
					f.catalogSHA = hashBytes(b)
				} else {
					b = append(b, ' ')
				}
				_ = os.WriteFile(path, b, 0600)
			case "protected":
				fs := skillsFS{blocked: func(p string) bool { return filepath.Base(p) == m.Sources[1].SHA+".gz" }}
				defer fs.close()
				if skillsSourcesBoundAt(m, &fs, f.root, f.catalogSHA, f.anchors) {
					t.Fatal("protected support accepted")
				}
				return
			}
			if skillsCheckSupport(t, f, m) {
				t.Fatal("unbound support accepted")
			}
		})
	}
}
