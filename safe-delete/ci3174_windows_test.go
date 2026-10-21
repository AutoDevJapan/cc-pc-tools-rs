//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const ci3174Opaque = "runtime/sdk/bootstrap/bin/cargo-clippy.exe"

func ci3174Fixture(t *testing.T) (*ci3174Scope, string) {
	t.Helper()
	return ci3174FixtureAt(t, t.TempDir())
}
func ci3174FixtureAt(t *testing.T, root string) (*ci3174Scope, string) {
	t.Helper()
	opaque := filepath.Join(root, filepath.FromSlash(ci3174Opaque))
	if e := os.MkdirAll(filepath.Dir(opaque), 0700); e != nil {
		t.Fatal(e)
	}
	outside := t.TempDir()
	write(t, filepath.Join(outside, "must-not-enumerate"), "outside")
	if e := os.Symlink(outside, opaque); e != nil {
		t.Fatal(e)
	}
	rootMeta, e := ci3174Identity(root)
	if e != nil {
		t.Fatal(e)
	}
	s := &ci3174Scope{volume: rootMeta.Volume, rootID: rootMeta.ID, rootBirth: ci3174Birth(rootMeta), maxEntries: 100, settled: func() error { return nil }}
	s.identity = func(p string) (ci3174Entry, error) {
		i, e := ci3174Identity(p)
		if e != nil {
			return i, e
		}
		i.Owner = "S-1-5-32-544"
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
	r := receipt{identityRecord: identityRecord{Volume: s.volume, FileID: s.rootID}}
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
			want := "CI_OBJECT_CHANGED"
			if change == "root" {
				want = "CI_ROOT_CHANGED"
			}
			code(t, e, want)
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
	s, root = ci3174Fixture(t)
	before, _, e = s.snapshot(root)
	if e != nil {
		t.Fatal(e)
	}
	before["missing-file"] = before["."]
	s.baseline = before
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

func TestCI3174QuarantinePreservationAndReceipt(t *testing.T) {
	for _, fault := range []string{"none", "settled", "preservation", "record"} {
		t.Run(fault, func(t *testing.T) {
			c, root := fixture(t)
			r := owned(t, c, root)
			s, _ := ci3174FixtureAt(t, r.Path)
			before, _, e := s.snapshot(r.Path)
			if e != nil {
				t.Fatal(e)
			}
			s.baseline = before
			raw, _ := json.Marshal(r)
			s.originalReceipt = func() ([]byte, error) { return raw, nil }
			if fault == "settled" {
				s.settled = func() error { return fail("CLIENT_RESOURCE_OR_GUARD_FAILED") }
			}
			original := s.identity
			injected := false
			s.identity = func(p string) (ci3174Entry, error) {
				i, e := original(p)
				if !strings.HasPrefix(strings.ToLower(p), strings.ToLower(r.Path)) && i.ID == r.FileID {
					if fault == "preservation" {
						i.ID++
					}
					if fault == "record" && !injected {
						injected = true
						recordPath, _ := c.recordPath(r.ID)
						if e := os.Mkdir(recordPath+".writing", 0700); e != nil {
							t.Fatal(e)
						}
					}
				}
				return i, e
			}
			result, e := c.quarantinePreserved(r.ID, "windows", nil, s)
			if fault != "none" {
				if fault == "settled" {
					failure := code(t, e, "CLIENT_RESOURCE_OR_GUARD_FAILED")
					if !exists(r.Path) {
						t.Fatal("guard rejection moved source")
					}
					if _, e := c.load(r.ID, true); e != nil {
						t.Fatal("guard rejection changed owned receipt", e)
					}
					if exists(filepath.Join(failure.Details["destination"].(string), "files")) {
						t.Fatal("guard rejection created moved files")
					}
					return
				}
				expected := "CI_INVENTORY_CHANGED"
				if fault == "record" {
					expected = "RECEIPT_WRITE_FAILED"
				}
				failure := code(t, e, expected)
				if exists(r.Path) {
					t.Fatal("ordinary move did not happen")
				}
				if _, e := c.load(r.ID, false); e != nil {
					t.Fatal("failed operation closed original receipt", e)
				}
				why, _ := os.ReadFile(filepath.Join(failure.Details["destination"].(string), "理由.txt"))
				if fault == "preservation" && strings.Contains(string(why), "データの移動は確認済み") {
					t.Fatal("preservation failure reported as verified move")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			dest := result["destination"].(string)
			if exists(r.Path) {
				t.Fatal("source remains")
			}
			if e := s.verify(filepath.Join(dest, "files"), before, r); e != nil {
				t.Fatal(e)
			}
			record, _ := os.ReadFile(filepath.Join(c.Store, r.ID+".json"))
			var closed receipt
			if json.Unmarshal(record, &closed) != nil || closed.State != "quarantined" || closed.Integrity != r.Integrity || closed.Destination != dest {
				t.Fatal("original receipt not closed correctly")
			}
			preserved, _ := os.ReadFile(filepath.Join(dest, "original-receipt.json"))
			if string(preserved) != string(raw) {
				t.Fatal("original bytes not preserved")
			}
			if _, e := os.Stat(filepath.Join(dest, "preservation.json")); e != nil {
				t.Fatal(e)
			}
		})
	}
}
