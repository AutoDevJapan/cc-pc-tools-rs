//go:build windows

package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"unsafe"
)

func fixture(t *testing.T) leaf {
	t.Helper()
	root := t.TempDir()
	l := leaf{"fixture", filepath.Join(root, "candidate.bin"), filepath.Join(root, "current.bin"), sha([]byte("old")), sha([]byte("new verified bytes"))}
	if e := os.WriteFile(l.Source, []byte("new verified bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(l.Target, []byte("old"), 0600); e != nil {
		t.Fatal(e)
	}
	return l
}
func content(t *testing.T, path, want string) {
	t.Helper()
	p, e := openPin(path, syscall.GENERIC_READ, 5, 3)
	if e != nil {
		t.Fatal(e)
	}
	defer p.close()
	b, e := io.ReadAll(p.f)
	if e != nil || string(b) != want {
		t.Fatalf("fixture content mismatch: %v", e)
	}
}
func descriptor(t *testing.T, p *pin) []byte {
	t.Helper()
	var sd unsafe.Pointer
	code, _, _ := advapi.NewProc("GetSecurityInfo").Call(p.f.Fd(), 1, 7, 0, 0, 0, 0, uintptr(unsafe.Pointer(&sd)))
	if code != 0 {
		t.Fatal("fixture descriptor read")
	}
	defer kernel.NewProc("LocalFree").Call(uintptr(sd))
	n, _, _ := advapi.NewProc("GetSecurityDescriptorLength").Call(uintptr(sd))
	if n == 0 || n > 65536 {
		t.Fatal("fixture descriptor size")
	}
	return append([]byte{}, unsafe.Slice((*byte)(sd), int(n))...)
}

func TestPinnedSoftwareUpdatePreservesOldObjectSecurityAndSource(t *testing.T) {
	l := fixture(t)
	p, e := prepare(l)
	if e != nil {
		t.Fatal(e)
	}
	defer p.close()
	before := descriptor(t, p.old)
	id := p.old.id()
	if e = p.install(hooks{}); e != nil {
		t.Fatal(e)
	}
	content(t, l.Target, "new verified bytes")
	content(t, l.Target+".before-todo3757", "old")
	content(t, l.Source, "new verified bytes")
	installed, e := openPin(l.Target, syscall.GENERIC_READ|0x20000, 1, 3)
	if e != nil {
		t.Fatal(e)
	}
	defer installed.close()
	if !bytes.Equal(before, descriptor(t, installed)) {
		t.Fatal("software ACL/owner/group changed")
	}
	backup, e := openPin(l.Target+".before-todo3757", 0x80, 5, 3)
	if e != nil {
		t.Fatal(e)
	}
	defer backup.close()
	if backup.id() != id {
		t.Fatal("original inode was not preserved")
	}
}
func TestSoftwareUpdateRefusesCandidateAndOldPinChangesBeforeStaging(t *testing.T) {
	for _, which := range []string{"old", "new", "stage", "backup"} {
		t.Run(which, func(t *testing.T) {
			l := fixture(t)
			switch which {
			case "old":
				os.WriteFile(l.Target, []byte("another update"), 0600)
			case "new":
				os.WriteFile(l.Source, []byte("changed"), 0600)
			case "stage":
				os.WriteFile(l.Target+".stage-todo3757", []byte("keep"), 0600)
			case "backup":
				os.WriteFile(l.Target+".before-todo3757", []byte("keep"), 0600)
			}
			if p, e := prepare(l); e == nil {
				p.close()
				t.Fatal("changed or incomplete software accepted")
			}
			if which == "old" || which == "new" {
				if !missing(l.Target + ".stage-todo3757") {
					t.Fatal("stage created before qualification")
				}
			}
		})
	}
}
func TestSoftwareUpdateDoesNotOverwriteConcurrentNamespaces(t *testing.T) {
	for _, phase := range []string{"target-before-retire", "backup", "target-before-publish"} {
		t.Run(phase, func(t *testing.T) {
			l := fixture(t)
			p, e := prepare(l)
			if e != nil {
				t.Fatal(e)
			}
			defer p.close()
			h := hooks{}
			switch phase {
			case "target-before-retire":
				h.beforeRetire = func() {
					if e := os.Rename(l.Target, l.Target+".other-owner-original"); e != nil {
						t.Fatal(e)
					}
					if e := os.WriteFile(l.Target, []byte("concurrent"), 0600); e != nil {
						t.Fatal(e)
					}
				}
			case "backup":
				h.beforeRetire = func() {
					if e := os.WriteFile(l.Target+".before-todo3757", []byte("concurrent"), 0600); e != nil {
						t.Fatal(e)
					}
				}
			case "target-before-publish":
				h.beforePublish = func() {
					if e := os.WriteFile(l.Target, []byte("concurrent"), 0600); e != nil {
						t.Fatal(e)
					}
				}
			}
			if e = p.install(h); e == nil {
				t.Fatal("concurrent namespace accepted")
			}
			switch phase {
			case "target-before-retire":
				content(t, l.Target, "concurrent")
				content(t, l.Target+".other-owner-original", "old")
			case "backup":
				content(t, l.Target, "old")
				content(t, l.Target+".before-todo3757", "concurrent")
			case "target-before-publish":
				content(t, l.Target, "concurrent")
				content(t, l.Target+".before-todo3757", "old")
			}
		})
	}
}
func TestSoftwarePinsDenyWritersAndExternalAliases(t *testing.T) {
	l := fixture(t)
	p, e := prepare(l)
	if e != nil {
		t.Fatal(e)
	}
	if f, e := os.OpenFile(l.Target, os.O_WRONLY, 0600); e == nil {
		f.Close()
		p.close()
		t.Fatal("old software could be written while pinned")
	}
	p.close()
	if e = os.Link(l.Target, l.Target+".external"); e != nil {
		t.Fatal(e)
	}
	if p, e = prepare(l); e == nil {
		p.close()
		t.Fatal("hardlink accepted")
	}
}
