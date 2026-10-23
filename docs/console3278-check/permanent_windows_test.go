package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPermanentSourceBindsRealGitOriginCommitAndTrackedBytes(t *testing.T) {
	root, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	currentFixtureGit(t, root, "init", "-q")
	currentFixtureGit(t, root, "remote", "add", "origin", "https://github.com/AutoDevJapan/cc-pc-tools-rs.git")
	path := filepath.Join(root, "source.go")
	if err := os.WriteFile(path, []byte("package fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	currentFixtureGit(t, root, "add", "source.go")
	currentFixtureGit(t, root, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "source")
	head, err := civ6GitAt(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	profiles := []permanentSource{{root, strings.TrimSpace(string(head))}}
	verify := func() bool { return permanentSourceVerifiedWith(root, profiles) }
	if !verify() {
		t.Fatal("clean fixed public-origin fixture rejected")
	}
	profiles[0].head = strings.Repeat("b", 40)
	if verify() {
		t.Fatal("different commit accepted")
	}
	profiles[0].head = strings.TrimSpace(string(head))
	currentFixtureGit(t, root, "remote", "set-url", "origin", "https://github.com/another-owner/cc-pc-tools-rs.git")
	if verify() {
		t.Fatal("different owner accepted")
	}
	currentFixtureGit(t, root, "remote", "set-url", "origin", "https://github.com/AutoDevJapan/cc-pc-tools-rs.git")
	if !verify() {
		t.Fatal("restored positive control rejected")
	}
	if err := os.WriteFile(path, []byte("package modified\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if verify() {
		t.Fatal("modified tracked source accepted")
	}
	currentFixtureGit(t, root, "add", "source.go")
	if verify() {
		t.Fatal("staged modification accepted")
	}
	if permanentSourceVerifiedWith(filepath.Dir(root), profiles) || permanentSourceVerifiedWith(root, nil) {
		t.Fatal("parent or undeclared source accepted")
	}
}

func TestModelUpdateRuntimeInputsArePermanent(t *testing.T) {
	for _, leaf := range model3081UpdateLeaves {
		paths := []string{leaf.candidate.root, leaf.candidate.path}
		for _, pin := range leaf.baselines {
			for _, evidence := range pin.evidence {
				paths = append(paths, evidence.root, evidence.path)
			}
		}
		for _, evidence := range leaf.candidate.evidence {
			paths = append(paths, evidence.root, evidence.path)
		}
		for _, path := range paths {
			if path != "" && !within("D:/CC/Work/unattended-operations-20261013", path) && !within("D:/CC/Work/unattended-operations-20261013-wt/todo-3278-console", path) {
				t.Fatalf("%s reader input escaped the two explicitly retained original roots: %s", leaf.kind, path)
			}
		}
	}
}

func TestPermanentMirrorBoundaryNeedsNoOldSource(t *testing.T) {
	f := newModelUpdateFixture(t)
	old := filepath.Join(f.root, "old-temporary-source")
	permanent := filepath.Join(f.root, "permanent-source")
	for _, root := range []string{old, permanent} {
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
	}
	currentFixtureGit(t, old, "init", "-q")
	currentFixtureGit(t, old, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(old, "fixture.go"), []byte("package fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	currentFixtureGit(t, old, "add", "fixture.go")
	currentFixtureGit(t, old, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "source")
	currentFixtureGit(t, f.root, "clone", "--no-hardlinks", "--no-local", old, permanent)
	currentFixtureGit(t, permanent, "remote", "set-url", "origin", "https://github.com/AutoDevJapan/cc-pc-tools-rs.git")
	head, err := civ6GitAt(permanent, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"baseline.json", "build.json", "new.exe"} {
		body := map[string]string{"baseline.json": "independent source receipt", "build.json": "independent reviewed build", "new.exe": "new model software"}[name]
		for _, root := range []string{old, permanent} {
			if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Actual fixture files are copied into the new independent source, then the
	// old synthetic source is removed. No live source is moved or deleted.
	baselineBytes, err := os.ReadFile(f.receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{old, permanent} {
		if err := os.WriteFile(filepath.Join(root, "baseline.json"), baselineBytes, 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.leaf.baselines[0].evidence = []consoleEvidence{{permanent, filepath.Join(permanent, "baseline.json"), hashBytes(baselineBytes)}}
	f.leaf.candidate.root = permanent
	f.leaf.candidate.head = strings.TrimSpace(string(head))
	f.leaf.candidate.path = filepath.Join(permanent, "new.exe")
	f.leaf.candidate.evidence = []consoleEvidence{{permanent, filepath.Join(permanent, "build.json"), hashBytes([]byte("independent reviewed build"))}}
	if err := os.RemoveAll(old); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old synthetic source still exists")
	}
	verify := func(path string) object {
		return modelRuntimeUpdateWith(path, []modelUpdateLeaf{f.leaf}, func(string) bool { return false }, func(root string) bool {
			return permanentSourceVerifiedWith(root, []permanentSource{{permanent, f.leaf.candidate.head}})
		}, modelSourceHead)
	}
	r := verify(f.leaf.target)
	if r["qualified"] != true || r["already_current"] != false {
		t.Fatalf("new independent source without an old source failed: %v", r)
	}
	unlisted := modelRuntimeUpdateWith(f.leaf.target, []modelUpdateLeaf{f.leaf}, func(string) bool { return false }, func(root string) bool { return permanentSourceVerifiedWith(root, nil) }, modelSourceHead)
	requireModelUpdateRefused(t, unlisted)
	for _, path := range []string{old, filepath.Join(old, "new.exe"), f.root, f.candidate + ".bak", f.candidate + ":stream"} {
		requireModelUpdateRefused(t, verify(path))
	}
}
