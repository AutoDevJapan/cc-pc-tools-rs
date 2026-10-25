//go:build windows

package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
	"unsafe"
)

func TestOriginalCleanupReferenceRejectsNewOrAmbiguousEvidence(t *testing.T) {
	history := map[string]any{"spec": cleanupHandoff, "updated": "2026-10-13 16:46"}
	row := map[string]any{"id": 2488, "project": "cc-pc-tools-rs", "spec_history": []any{history}}
	for _, kind := range []string{"original", "missing", "new-only", "wrong-project", "wrong-source", "wrong-time", "duplicate-id", "duplicate-reference", "unrelated-legacy-types"} {
		t.Run(kind, func(t *testing.T) {
			b, _ := json.Marshal(row)
			var candidate map[string]any
			json.Unmarshal(b, &candidate)
			items := []any{candidate}
			switch kind {
			case "missing":
				items = nil
			case "new-only":
				candidate["spec"] = cleanupHandoff
				delete(candidate, "spec_history")
			case "wrong-project":
				candidate["project"] = "other"
			case "wrong-source":
				candidate["spec_history"] = []any{map[string]any{"spec": cleanupHandoff + ".copy", "updated": "2026-10-13 16:46"}}
			case "wrong-time":
				candidate["spec_history"] = []any{map[string]any{"spec": cleanupHandoff, "updated": "2026-10-25 14:40"}}
			case "duplicate-id":
				items = append(items, row)
			case "duplicate-reference":
				candidate["spec_history"] = []any{history, history}
			case "unrelated-legacy-types":
				items = append(items, map[string]any{"id": 7, "project": 123, "spec_history": "old-schema"})
			}
			b, _ = json.Marshal(map[string]any{"items": items})
			e := cleanupReference(b)
			want := kind == "original" || kind == "unrelated-legacy-types"
			if (e == nil) != want {
				t.Fatalf("original reference accepted=%v want=%v", e == nil, want)
			}
		})
	}
}

func fixture(t *testing.T) leaf {
	t.Helper()
	// Hosted Windows may expose TEMP through RUNNER~1. Select the canonical
	// path of our own fixture; production must still reject a physical alias.
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	l := leaf{"fixture", filepath.Join(root, "candidate.bin"), filepath.Join(root, "current.bin"), sha([]byte("old")), sha([]byte("new verified bytes"))}
	if e := os.WriteFile(l.Source, []byte("new verified bytes"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(l.Target, []byte("old"), 0600); e != nil {
		t.Fatal(e)
	}
	// Give old software a security state different from a newly inherited
	// stage. Omitting copySecurity must fail this fixture, not pass by default.
	p, e := openPin(l.Target, syscall.GENERIC_READ|0x20000|0x40000, 7, 3)
	if e != nil {
		t.Fatal(e)
	}
	defer p.close()
	var dacl, sd unsafe.Pointer
	code, _, _ := advapi.NewProc("GetSecurityInfo").Call(p.f.Fd(), 1, 4, 0, 0, uintptr(unsafe.Pointer(&dacl)), 0, uintptr(unsafe.Pointer(&sd)))
	if code != 0 {
		t.Fatal("fixture original ACL read")
	}
	defer kernel.NewProc("LocalFree").Call(uintptr(sd))
	code, _, _ = advapi.NewProc("SetSecurityInfo").Call(p.f.Fd(), 1, 0x80000004, 0, 0, uintptr(dacl), 0)
	if code != 0 {
		t.Fatal("fixture protected ACL setup")
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

type securityView struct {
	Owner, Group, DACL []byte
	Protected          bool
}

func descriptor(t *testing.T, p *pin) securityView {
	t.Helper()
	var owner, group, dacl, sd unsafe.Pointer
	code, _, _ := advapi.NewProc("GetSecurityInfo").Call(p.f.Fd(), 1, 7, uintptr(unsafe.Pointer(&owner)), uintptr(unsafe.Pointer(&group)), uintptr(unsafe.Pointer(&dacl)), 0, uintptr(unsafe.Pointer(&sd)))
	if code != 0 {
		t.Fatal("fixture descriptor read")
	}
	defer kernel.NewProc("LocalFree").Call(uintptr(sd))
	sid := func(p unsafe.Pointer) []byte {
		n, _, _ := advapi.NewProc("GetLengthSid").Call(uintptr(p))
		if n == 0 || n > 1024 {
			t.Fatal("fixture SID size")
		}
		return append([]byte{}, unsafe.Slice((*byte)(p), int(n))...)
	}
	var control uint16
	var revision uint32
	ok, _, _ := advapi.NewProc("GetSecurityDescriptorControl").Call(uintptr(sd), uintptr(unsafe.Pointer(&control)), uintptr(unsafe.Pointer(&revision)))
	if ok == 0 {
		t.Fatal("fixture descriptor control")
	}
	var acl []byte
	if dacl != nil {
		n := *(*uint16)(unsafe.Add(dacl, 2))
		if n < 8 {
			t.Fatal("fixture ACL size")
		}
		acl = append([]byte{}, unsafe.Slice((*byte)(dacl), int(n))...)
	}
	// Self-relative descriptor offsets/padding and the OS's auto-inherited bit
	// are not access grants. Compare the exact owner/group SID, ACL and its
	// protected state rather than unrelated descriptor layout bytes.
	return securityView{sid(owner), sid(group), acl, control&0x1000 != 0}
}

func TestPinnedSoftwareUpdatePreservesOldObjectSecurityAndSource(t *testing.T) {
	l := fixture(t)
	p, e := prepare(l)
	if e != nil {
		t.Fatal(e)
	}
	defer p.close()
	before := descriptor(t, p.old)
	if !before.Protected {
		t.Fatal("old fixture must differ from inherited stage")
	}
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
	after := descriptor(t, installed)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("software security changed: owner=%v group=%v ACL=%v protected=%v", reflect.DeepEqual(before.Owner, after.Owner), reflect.DeepEqual(before.Group, after.Group), reflect.DeepEqual(before.DACL, after.DACL), before.Protected == after.Protected)
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
