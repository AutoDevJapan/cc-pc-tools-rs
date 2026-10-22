package main

import (
	"path/filepath"
	"strings"
	"testing"
)

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
