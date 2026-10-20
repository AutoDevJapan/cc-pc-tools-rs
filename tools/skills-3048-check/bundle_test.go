package main

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func bundleFile(path, body string) File {
	h := sha256.Sum256([]byte(body))
	return File{Path: path, SHA: hex.EncodeToString(h[:]), Content: &body}
}
func TestBundleUsesExistingChecksWithoutInstalledFiles(t *testing.T) {
	entry := "---\nname: sample\ndescription: 資料の例を確認するときに使う。\n---\n\n[詳細](references/guide.md)\n"
	m := Manifest{Targets: []Target{{Name: "sample", Positive: "正例", Negative: "負例", Files: []File{bundleFile("SKILL.md", entry), bundleFile("references/guide.md", "例を確認する。")}}}}
	if f := checkBundle(m); len(f) != 0 {
		t.Fatalf("valid virtual bundle rejected: %v", f)
	}
	m.Targets[0].Files = m.Targets[0].Files[:1]
	if f := strings.Join(checkBundle(m), " "); !strings.Contains(f, "broken link") {
		t.Fatalf("missing reference accepted: %s", f)
	}
	m.Targets[0].Files[0].SHA = strings.Repeat("0", 64)
	if f := strings.Join(checkBundle(m), " "); !strings.Contains(f, "hash mismatch") {
		t.Fatalf("corrupt hash accepted: %s", f)
	}
}
