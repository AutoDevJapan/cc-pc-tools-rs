package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func nextWorkFixture() (object, nextWorkIO) {
	db := object{"projects": object{"civ6-banseiki": nextWorkRoot}, "items": []any{
		object{"id": float64(2636), "project": "cc-pc-tools-rs", "spec": nextWorkOwnerSpec},
		object{"id": float64(2637), "project": "civ6-banseiki", "spec": nextWorkSpec},
		object{"id": float64(880), "project": "civ6-banseiki", "spec": nextWorkOriginalSpec, "status": "doing", "wait_on": []any{float64(2256)}},
	}}
	anchors := map[string]string{nextWorkOwnerSpec: nextWorkOwnerHash, nextWorkSpec: nextWorkHash, nextWorkOriginalSpec: nextWorkOriginalHash}
	access := nextWorkIO{
		readObject: func(string, int64) (object, error) { return db, nil },
		read:       func(path string, _ int64) ([]byte, error) { return []byte(anchors[path]), nil },
		physical:   func(path string) (string, error) { return path, nil },
		origin:     func(string) string { return nextWorkOrigin },
		branch:     func(string) string { return "scheduler/todo-2637-prepare-880-2636" },
		protected:  func(string) bool { return false },
		// Fixture bytes represent independently anchored digests. Production
		// always uses SHA256; changed fixture bytes still fail the comparison.
		digest: func(b []byte) string { return string(b) },
		at:     time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC),
	}
	return db, access
}

func TestNextWorkRejectsUnknownBeforeAnyEvidenceRead(t *testing.T) {
	_, access := nextWorkFixture()
	access.readObject = func(string, int64) (object, error) { t.Fatal("unknown scope read ledger"); return nil, nil }
	for _, path := range []string{nextWorkRoot + "-other", filepath.Dir(nextWorkRoot), nextWorkTree + "-other", filepath.Join(filepath.Dir(nextWorkTree), "todo-9999-work")} {
		if nextWorkPathWith(path, access) {
			t.Fatal("unknown resource allowed")
		}
	}
}

func TestNextWorkIndependentAnchorsExpiryOriginAndRegistry(t *testing.T) {
	for _, path := range []string{nextWorkRoot, filepath.Join(nextWorkOriginalTree, "src", "main.rs"), nextWorkTree, nextWorkOwnerSpec, nextWorkSpec, nextWorkOriginalSpec} {
		_, access := nextWorkFixture()
		if !nextWorkPathWith(path, access) {
			t.Fatal("finite scope rejected")
		}
	}
	for _, mutate := range []func(object, *nextWorkIO){
		func(_ object, a *nextWorkIO) { a.at = time.Date(2026, 10, 14, 14, 59, 59, 0, time.UTC) },
		func(_ object, a *nextWorkIO) { a.at = time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC) },
		func(_ object, a *nextWorkIO) {
			a.origin = func(string) string { return "git@github.com:other/civ6-banseiki.git" }
		},
		func(_ object, a *nextWorkIO) {
			a.origin = func(string) string { return "https://github.com/AutoDevJapan/civ6-banseiki.git" }
		},
		func(_ object, a *nextWorkIO) { a.protected = func(string) bool { return true } },
		func(_ object, a *nextWorkIO) {
			a.readObject = func(string, int64) (object, error) { return nil, errors.New("unreadable") }
		},
		func(db object, _ *nextWorkIO) { obj(db["projects"])["civ6-banseiki"] = "C:/other" },
		func(db object, _ *nextWorkIO) { itemAt(db, 2637)["spec"] = "different" },
		func(db object, _ *nextWorkIO) { itemAt(db, 880)["project"] = "other" },
		func(db object, _ *nextWorkIO) { db["items"] = append(db["items"].([]any), itemAt(db, 2637)) },
	} {
		db, access := nextWorkFixture()
		mutate(db, &access)
		if nextWorkPathWith(nextWorkRoot, access) {
			t.Fatal("changed evidence accepted")
		}
	}
	for _, path := range []string{nextWorkOwnerSpec, nextWorkSpec, nextWorkOriginalSpec} {
		_, access := nextWorkFixture()
		read := access.read
		access.read = func(p string, n int64) ([]byte, error) {
			if same(p, path) {
				return []byte("changed"), nil
			}
			return read(p, n)
		}
		if nextWorkPathWith(nextWorkRoot, access) {
			t.Fatal("modified anchored bytes accepted")
		}
	}
}

func TestNextWorkPhysicalEscapeAndExactAbsentTree(t *testing.T) {
	for _, altered := range []string{nextWorkRoot, nextWorkTree, nextWorkSpec, filepath.Join(nextWorkTree, "escape")} {
		_, access := nextWorkFixture()
		access.physical = func(p string) (string, error) {
			if same(p, altered) {
				return "C:/outside", nil
			}
			return p, nil
		}
		if nextWorkPathWith(filepath.Join(nextWorkTree, "escape"), access) {
			t.Fatal("physical escape accepted")
		}
	}
	_, access := nextWorkFixture()
	access.physical = func(p string) (string, error) {
		if same(p, nextWorkTree) {
			return "", os.ErrNotExist
		}
		return p, nil
	}
	if !nextWorkPathWith(nextWorkTree, access) {
		t.Fatal("exact planned allocation rejected")
	}
	if nextWorkPathWith(filepath.Join(nextWorkTree, "child"), access) {
		t.Fatal("missing child allowed")
	}
	access.physical = func(p string) (string, error) {
		if same(p, nextWorkTree) {
			return "", os.ErrNotExist
		}
		if same(p, filepath.Dir(nextWorkTree)) {
			return "C:/outside", nil
		}
		return p, nil
	}
	if nextWorkPathWith(nextWorkTree, access) {
		t.Fatal("escaped allocation parent accepted")
	}
}

func TestNextWorkLeavesOriginalStateUntouched(t *testing.T) {
	db, access := nextWorkFixture()
	original := itemAt(db, 880)
	if !nextWorkPathWith(nextWorkRoot, access) {
		t.Fatal("scope rejected")
	}
	if original["status"] != "doing" || original["wait_on"].([]any)[0] != float64(2256) {
		t.Fatal("original history or dependency changed")
	}
}

func TestNextWorkRejectsDifferentAllocationAndBranch(t *testing.T) {
	for _, mutate := range []func(object, *nextWorkIO){
		func(db object, _ *nextWorkIO) {
			itemAt(db, 2637)["scheduler"] = object{"worktree": nextWorkTree + "-other"}
		},
		func(db object, _ *nextWorkIO) {
			itemAt(db, 2637)["scheduler"] = object{"branch": "scheduler/todo-9999-work"}
		},
		func(_ object, a *nextWorkIO) { a.branch = func(string) string { return "main" } },
	} {
		db, a := nextWorkFixture()
		mutate(db, &a)
		if nextWorkPathWith(nextWorkTree, a) {
			t.Fatal("different allocation or branch allowed")
		}
	}
}
