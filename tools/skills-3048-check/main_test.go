package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEntryContract(t *testing.T) {
	good := "---\nname: sample\ndescription: \"日本語UIの行幅を調べるときに使う。\"\n---\n短い手順\n"
	for _, tc := range []struct {
		name, body string
		valid      bool
	}{
		{"valid", good, true},
		{"mismatch", strings.Replace(good, "name: sample", "name: other", 1), false},
		{"long-description", strings.Replace(good, "日本語UIの行幅を調べるときに使う。", strings.Repeat("あ", 61), 1), false},
		{"missing-frontmatter", "name: sample", false},
		{"duplicate", strings.Replace(good, "name: sample", "name: sample\nname: sample", 1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if (checkEntry("sample", []byte(tc.body)) == nil) != tc.valid {
				t.Fatal("entry contract")
			}
		})
	}
}

func TestMissingAndDuplicateReceipts(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "sample")
	if err := os.Mkdir(base, 0700); err != nil {
		t.Fatal(err)
	}
	body := []byte("---\nname: sample\ndescription: 資料を検査するときに使う。\n---\n本文\n")
	if err := os.WriteFile(filepath.Join(base, "SKILL.md"), body, 0600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(body)
	file := File{Path: "SKILL.md", SHA: hex.EncodeToString(h[:])}
	target := Target{Name: "sample", Positive: "正例", Negative: "負例", Files: []File{file}}
	if got := checkOptions(root, Manifest{Targets: []Target{target}}, true); len(got) != 0 {
		t.Fatal(got)
	}
	for _, tc := range []struct {
		name     string
		mutate   func(*Manifest)
		expected string
	}{
		{"files", func(m *Manifest) { m.Targets[0].Files = nil }, "no files"},
		{"entry", func(m *Manifest) { m.Targets[0].Files = []File{{Path: "other.md"}} }, "SKILL.md not manifested"},
		{"scenarios", func(m *Manifest) { m.Targets[0].Positive = "" }, "scenario pair missing"},
		{"targets", func(m *Manifest) { m.Targets = append(m.Targets, m.Targets[0]) }, "duplicate target"},
		{"files-duplicate", func(m *Manifest) { m.Targets[0].Files = append(m.Targets[0].Files, m.Targets[0].Files[0]) }, "duplicate file"},
		{"hash", func(m *Manifest) { m.Targets[0].Files[0].SHA = "" }, "sha256 missing or invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Manifest{Targets: []Target{target}}
			m.Targets[0].Files = append([]File(nil), target.Files...)
			tc.mutate(&m)
			got := strings.Join(checkOptions(root, m, true), "\n")
			if !strings.Contains(got, tc.expected) {
				t.Fatalf("wanted %s, got %s", tc.expected, got)
			}
		})
	}
}

func TestManifestBoundary(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "sample")
	if err := os.Mkdir(base, 0700); err != nil {
		t.Fatal(err)
	}
	entry := "---\nname: sample\ndescription: 日本語の資料を検査するときに使う。\n---\n[詳細](missing.md)\n"
	if err := os.WriteFile(filepath.Join(base, "SKILL.md"), []byte(entry), 0600); err != nil {
		t.Fatal(err)
	}
	m := Manifest{Targets: []Target{{Name: "sample", New: true, BeforeSHA: "absent", Positive: "Obsidianのノート", Negative: "普通の文章", Files: []File{{Path: "SKILL.md"}}}}}
	if got := check(root, m); len(got) != 1 || !strings.Contains(got[0], "broken link") {
		t.Fatalf("expected broken link, got %v", got)
	}
	if err := os.WriteFile(filepath.Join(base, "missing.md"), []byte("detail"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := check(root, m); len(got) != 0 {
		t.Fatal(got)
	}
	m.Targets[0].Files[0].SHA = strings.Repeat("0", 64)
	if got := check(root, m); len(got) != 1 || !strings.Contains(got[0], "hash mismatch") {
		t.Fatalf("expected hash failure: %v", got)
	}
	if _, err := relative(base, "../outside"); err == nil {
		t.Fatal("escape accepted")
	}
	if got := check(root, Manifest{}); len(got) == 0 {
		t.Fatal("empty manifest passed")
	}
}

func TestParentReferenceInsideSkill(t *testing.T) {
	root := t.TempDir()
	base := filepath.Join(root, "sample")
	if err := os.MkdirAll(filepath.Join(base, "references"), 0700); err != nil {
		t.Fatal(err)
	}
	entry := "---\nname: sample\ndescription: 資料を照合するときに使う。\n---\n[詳細](references/usage.md)\n"
	for path, body := range map[string]string{"SKILL.md": entry, "references/usage.md": "[入口](../SKILL.md)\n"} {
		if err := os.WriteFile(filepath.Join(base, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m := Manifest{Targets: []Target{{Name: "sample", Positive: "正例", Negative: "負例", Files: []File{{Path: "SKILL.md"}, {Path: "references/usage.md"}}}}}
	if got := check(root, m); len(got) != 0 {
		t.Fatalf("valid parent reference rejected: %v", got)
	}
	if err := os.WriteFile(filepath.Join(base, "references/usage.md"), []byte("[外](../../outside.md)\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := check(root, m); len(got) != 1 || !strings.Contains(got[0], "link path escape") {
		t.Fatalf("expected containment failure: %v", got)
	}
}
