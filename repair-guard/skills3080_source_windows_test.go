package main

import (
	"bytes"
	"compress/gzip"
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

type skillsAcquiredTestData struct {
	root, catalogSHA                                string
	anchors                                         map[string]skillsSourceAnchor
	index, tree, commit, sourceObject, noticeObject string
}

func skillsAcquiredFixture(t *testing.T, source, notice []byte, flat bool) skillsAcquiredTestData {
	t.Helper()
	f := skillsAcquiredTestData{root: skillsFixtureRoot(t), anchors: map[string]skillsSourceAnchor{}}
	repo := "6mile/skills"
	root := filepath.Join(f.root, "github", "6mile__skills")
	if e := os.MkdirAll(filepath.Join(root, "objects"), 0700); e != nil {
		t.Fatal(e)
	}
	write := func(path string, raw []byte) string {
		t.Helper()
		if e := os.WriteFile(path, raw, 0600); e != nil {
			t.Fatal(e)
		}
		return hashBytes(raw)
	}
	jsonBytes := func(v any) []byte {
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	rows := []skillsSavedSource{}
	for i, raw := range [][]byte{source, notice} {
		path, role := "skills/sample/SKILL.md", "skill_entry"
		if i == 1 {
			path, role = "LICENSE", "license_or_notice"
		}
		blob := sha1.Sum(append([]byte("blob "+strconv.Itoa(len(raw))+"\x00"), raw...))
		row := skillsSavedSource{Repo: repo, Commit: skillsPublicSources[repo], Path: path, Role: role, Blob: hex.EncodeToString(blob[:]), SHA: hashBytes(raw), Bytes: int64(len(raw)), Content: "objects/" + hashBytes(raw) + ".gz"}
		rows = append(rows, row)
		var compressed bytes.Buffer
		z := gzip.NewWriter(&compressed)
		if _, e := z.Write(raw); e != nil {
			t.Fatal(e)
		}
		if e := z.Close(); e != nil {
			t.Fatal(e)
		}
		objectPath := filepath.Join(root, filepath.FromSlash(row.Content))
		write(objectPath, compressed.Bytes())
		if i == 0 {
			f.sourceObject = objectPath
		} else {
			f.noticeObject = objectPath
		}
	}
	catalogRow := map[string]any{"repo": repo, "commit": rows[0].Commit, "path": rows[0].Path, "sha256": rows[0].SHA, "git_blob": rows[0].Blob, "bytes": rows[0].Bytes, "content_path": "github/6mile__skills/" + rows[0].Content, "machine_body_read": true, "inventory_unit": "github_skill_entry"}
	f.catalogSHA = write(filepath.Join(f.root, "phase-a-catalog.jsonl"), append(jsonBytes(catalogRow), '\n'))
	f.index, f.commit = filepath.Join(root, "content-index.jsonl"), filepath.Join(root, "commit.json")
	indexRaw := append(append(jsonBytes(rows[0]), '\n'), append(jsonBytes(rows[1]), '\n')...)
	a := skillsSourceAnchor{IndexSHA: write(f.index, indexRaw), FlatTree: flat}
	rootSHA := strings.Repeat("c", 40)
	a.CommitSHA = write(f.commit, jsonBytes(map[string]any{"sha": rows[0].Commit, "commit": map[string]any{"tree": map[string]any{"sha": rootSHA}}}))
	entries := []map[string]string{}
	for _, row := range rows {
		entries = append(entries, map[string]string{"path": row.Path, "sha": row.Blob, "type": "blob", "mode": "100644"})
	}
	f.tree = filepath.Join(root, "tree-recursive.json")
	var treeRaw []byte
	if flat {
		f.tree = filepath.Join(root, "tree-complete.json")
		treeRaw = jsonBytes(entries)
	} else {
		treeRaw = jsonBytes(map[string]any{"sha": rootSHA, "tree": entries, "truncated": false})
	}
	a.TreeSHA = write(f.tree, treeRaw)
	f.anchors[repo] = a
	return f
}

func skillsManifestWithOriginals(t *testing.T, source, notice []byte) skillsManifest {
	t.Helper()
	m := skillsParsedFixture(t)
	m.Sources[0].Repository, m.Sources[0].Commit = "6mile/skills", skillsPublicSources["6mile/skills"]
	m.Sources[0].SHA, m.Sources[0].License.SHA = hashBytes(source), hashBytes(notice)
	for i, raw := range [][]byte{source, notice} {
		path := "sample/references/upstream.txt"
		if i == 1 {
			path = "sample/references/LICENSE.txt"
		}
		m.Files = append(m.Files, skillsOutput{Target: path, SHA: hashBytes(raw), Content: base64.StdEncoding.EncodeToString(raw), body: append([]byte(nil), raw...)})
	}
	return m
}

func TestSkills3080AcquiredSourceIdentityAndBytes(t *testing.T) {
	for _, flat := range []bool{false, true} {
		t.Run(strconv.FormatBool(flat), func(t *testing.T) {
			original, notice := []byte("acquired original\n"), []byte("individual notice\n")
			f := skillsAcquiredFixture(t, original, notice, flat)
			m := skillsManifestWithOriginals(t, original, notice)
			check := func(m skillsManifest) bool {
				fs := skillsFS{blocked: func(string) bool { return false }}
				defer fs.close()
				return skillsSourcesBoundAt(m, &fs, f.root, f.catalogSHA, f.anchors)
			}
			if !check(m) {
				t.Fatal("fixed acquired source and exact preserved bytes rejected")
			}
			for _, mutate := range []func(*skillsManifest){
				func(m *skillsManifest) { m.Sources[0].Path = "skills/other/SKILL.md" },
				func(m *skillsManifest) { m.Sources[0].License.Path = "other/LICENSE" },
				func(m *skillsManifest) {
					m.Sources[0].SHA = hashBytes([]byte("invented"))
					m.Files[1].SHA = m.Sources[0].SHA
					m.Files[1].body = []byte("invented")
				},
				func(m *skillsManifest) { m.Files[1].body = []byte("different consumed bytes") },
				func(m *skillsManifest) { m.Sources[0].License.Accepted = false },
			} {
				copy := m
				copy.Sources = append([]skillsSource(nil), m.Sources...)
				copy.Files = append([]skillsOutput(nil), m.Files...)
				mutate(&copy)
				if check(copy) {
					t.Fatal("unbound path, notice or bytes accepted")
				}
			}
		})
	}
}

func TestSkills3080AcquiredEvidenceTamperingAndAbsence(t *testing.T) {
	for _, which := range []string{"catalog", "index", "tree", "commit", "source", "notice", "missing", "alias"} {
		t.Run(which, func(t *testing.T) {
			original, notice := []byte("saved source\n"), []byte("saved notice\n")
			f := skillsAcquiredFixture(t, original, notice, false)
			m := skillsManifestWithOriginals(t, original, notice)
			path := map[string]string{"catalog": filepath.Join(f.root, "phase-a-catalog.jsonl"), "index": f.index, "tree": f.tree, "commit": f.commit, "source": f.sourceObject, "notice": f.noticeObject, "missing": f.noticeObject, "alias": f.noticeObject}[which]
			if which == "missing" {
				if e := os.Remove(path); e != nil {
					t.Fatal(e)
				}
			} else if which == "alias" {
				if e := os.Link(path, path+".alias"); e != nil {
					t.Fatal(e)
				}
			} else {
				raw, e := os.ReadFile(path)
				if e != nil {
					t.Fatal(e)
				}
				raw = append(raw, ' ')
				if e = os.WriteFile(path, raw, 0600); e != nil {
					t.Fatal(e)
				}
			}
			fs := skillsFS{blocked: func(string) bool { return false }}
			defer fs.close()
			if skillsSourcesBoundAt(m, &fs, f.root, f.catalogSHA, f.anchors) {
				t.Fatal("changed or unavailable original evidence accepted")
			}
		})
	}
}
