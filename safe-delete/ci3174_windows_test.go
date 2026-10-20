//go:build windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func ci3174Fixture(t *testing.T) (*ci3174Scope, string) {
	t.Helper()
	root := t.TempDir()
	opaque := filepath.Join(root, filepath.FromSlash(ci3174Opaque))
	if e := os.MkdirAll(filepath.Dir(opaque), 0700); e != nil {
		t.Fatal(e)
	}
	outside := t.TempDir()
	write(t, filepath.Join(outside, "must-not-enumerate"), "outside")
	if e := os.Symlink(outside, opaque); e != nil {
		t.Fatal(e)
	}
	s := &ci3174Scope{root: root, maxEntries: 100, settled: func() error { return nil }}
	s.identity = func(p string) (ci3174Entry, error) {
		i, e := ci3174Identity(p)
		if e != nil {
			return i, e
		}
		i.Volume = 2229119083
		i.Owner = "S-1-5-32-544"
		if same(p, root) {
			i.ID = 45317471266384470
			i.Created = syscall.Filetime{LowDateTime: uint32(134369735429421378 & 0xffffffff), HighDateTime: uint32(134369735429421378 >> 32)}
		}
		if same(p, opaque) {
			i.ID = 5629499550387750
			i.Attributes = 1056
		}
		return i, nil
	}
	i, e := s.identity(opaque)
	if e != nil {
		t.Fatal(e)
	}
	s.opaque = map[string]ci3174Entry{ci3174Opaque: i}
	return s, root
}

func TestCI3174OpaqueSnapshotAndPreservation(t *testing.T) {
	s, root := ci3174Fixture(t)
	before, m, e := s.snapshot(root)
	if e != nil {
		t.Fatal(e)
	}
	if len(before) != 6 || m.Entries != 5 {
		t.Fatalf("unexpected enumeration: %d/%d", len(before), m.Entries)
	}
	for p := range before {
		if filepath.Base(p) == "must-not-enumerate" {
			t.Fatal("followed reparse")
		}
	}
	r := receipt{identityRecord: identityRecord{Volume: 2229119083, FileID: 45317471266384470}}
	if e = s.verify(root, before, r); e != nil {
		t.Fatal(e)
	}
	x := before[ci3174Opaque]
	x.ID++
	before[ci3174Opaque] = x
	code(t, s.verify(root, before, r), "PRESERVATION_MISMATCH")
}

func TestCI3174OtherReparseAndLimitsStayClosed(t *testing.T) {
	s, root := ci3174Fixture(t)
	if e := os.Symlink(t.TempDir(), filepath.Join(root, "other")); e != nil {
		t.Fatal(e)
	}
	_, _, e := s.snapshot(root)
	code(t, e, "LINK_OR_REPARSE_POINT")
	s, root = ci3174Fixture(t)
	s.maxEntries = 1
	_, _, e = s.snapshot(root)
	code(t, e, "ENTRY_LIMIT_EXCEEDED")
}

func TestCI3174RejectsHardlinksAndChangedOwner(t *testing.T) {
	for _, change := range []string{"owner", "links", "volume", "root"} {
		t.Run(change, func(t *testing.T) {
			s, root := ci3174Fixture(t)
			original := s.identity
			s.identity = func(p string) (ci3174Entry, error) {
				i, e := original(p)
				switch change {
				case "owner":
					i.Owner = "S-1-5-18"
				case "links":
					i.Links = 2
				case "volume":
					i.Volume++
				case "root":
					i.ID++
				}
				return i, e
			}
			_, _, e := s.snapshot(root)
			if e == nil {
				t.Fatal("changed identity accepted")
			}
		})
	}
}

func TestCI3174QualificationNeverRegistersOtherRoots(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	_, _, e := c.ci3174Recovery(r.ID)
	code(t, e, "CI_SCOPE_MISMATCH")
	_, _, e = c.ci3174Recovery(ci3174ID)
	code(t, e, "CI_SCOPE_MISMATCH")
	if ci3174ReceiptMatches(r) {
		t.Fatal("fixture admitted as real receipt")
	}
}

func TestCI3174NormalPlannerStillRejectsOpaque(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	if e := os.Symlink(t.TempDir(), filepath.Join(r.Path, "link")); e != nil {
		t.Fatal(e)
	}
	_, e := c.plan(r.ID)
	code(t, e, "LINK_OR_REPARSE_POINT")
	_, e = c.quarantine(r.ID, "windows")
	code(t, e, "LINK_OR_REPARSE_POINT")
}

func TestCI3174FrozenInventoryRejectsNewAndMissingNames(t *testing.T) {
	s, root := ci3174Fixture(t)
	before, _, e := s.snapshot(root)
	if e != nil {
		t.Fatal(e)
	}
	s.baseline = before
	write(t, filepath.Join(root, "new-file"), "new")
	_, _, e = s.snapshot(root)
	code(t, e, "CI_INVENTORY_CHANGED")
}

func TestCI3174OpaqueIdentityIsItsOwnObject(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "link")
	if e := os.Symlink(outside, link); e != nil {
		t.Fatal(e)
	}
	i, e := ci3174Identity(link)
	if e != nil {
		t.Fatal(e)
	}
	target, e := ci3174Identity(outside)
	if e != nil {
		t.Fatal(e)
	}
	if i.ID == target.ID || i.Attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		t.Fatal("link target identity used")
	}
}
