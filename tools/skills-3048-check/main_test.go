package main

import (
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
