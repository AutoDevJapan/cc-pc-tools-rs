package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeExecutableOnlyQualifiesTheExistingExactFile(t *testing.T) {
	root, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	known := filepath.Join(root, "gh_gate.exe")
	if err := os.WriteFile(known, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if !runtimeExecutable(known, known, known) {
		t.Fatal("existing exact executable rejected")
	}
	for _, candidate := range []string{root, known + ".bak", filepath.Join(root, "config.json"), filepath.Join(root, "other.exe")} {
		if runtimeExecutable(candidate, candidate, known) {
			t.Fatal("unrelated resource accepted")
		}
	}
	if runtimeExecutable(known, filepath.Join(root, "redirected.exe"), known) {
		t.Fatal("resolved target mismatch accepted")
	}
	missing := filepath.Join(root, "missing.exe")
	if runtimeExecutable(missing, missing, missing) {
		t.Fatal("uncreated executable accepted")
	}
	dir := filepath.Join(root, "directory.exe")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if runtimeExecutable(dir, dir, dir) {
		t.Fatal("directory accepted as executable")
	}
}
