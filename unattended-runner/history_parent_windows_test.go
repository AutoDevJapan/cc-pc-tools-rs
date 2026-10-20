package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestHistoryReadRejectsJunctionAncestor(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(filepath.Join(target, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "junction")
	cmd := exec.Command("cmd.exe", "/c", "mklink", "/J", link, target)
	hide(cmd)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create owned test junction: %v %s", err, b)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	if !historyPlainParent(filepath.Join(target, "nested", "page.private.json")) {
		t.Fatal("ordinary directory rejected")
	}
	if historyPlainParent(filepath.Join(link, "nested", "page.private.json")) {
		t.Fatal("junction ancestor accepted as plain parent")
	}
}
