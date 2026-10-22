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
			if path != "" && !within("D:/CC/RuntimeSources/todo-3344", path) {
				t.Fatalf("%s runtime still depends on an expiring source: %s", leaf.kind, path)
			}
		}
	}
}

func TestPermanentMirrorBoundaryNeedsNoOldSource(t *testing.T) {
	f := newModelUpdateFixture(t)
	old := filepath.Join(f.root, "absent-old-temporary-source")
	if within(old, f.leaf.candidate.root) || strings.Contains(f.leaf.candidate.path, old) {
		t.Fatal("fixture depends on the old source")
	}
	r := f.result(f.leaf.target)
	if r["qualified"] != true || r["already_current"] != false {
		t.Fatalf("new independent source without an old source failed: %v", r)
	}
	for _, path := range []string{old, filepath.Join(old, "new.exe"), f.root, f.candidate + ".bak", f.candidate + ":stream"} {
		requireModelUpdateRefused(t, f.result(path))
	}
}
