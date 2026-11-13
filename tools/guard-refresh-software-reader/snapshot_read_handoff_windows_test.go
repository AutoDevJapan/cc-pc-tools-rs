//go:build windows

package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestSnapshotReadHandoffKeepsIdentityAndRejectsWriters(t *testing.T) {
	for _, fault := range []string{"normal", "before_restrict", "restricted", "bridge_held", "creator_closed", "reader_held", "restored"} {
		t.Run(fault, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			path := filepath.Join(root, "source.txt")
			s := &softwareBuildSnapshot{Root: root, paths: map[string]bool{root: true}}
			defer s.Close()
			ptr, e := syscall.UTF16PtrFromString(path)
			if e != nil {
				t.Fatal(e)
			}
			h, e := syscall.CreateFile(ptr, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ, nil, syscall.CREATE_NEW, 0x00200000, 0)
			if e != nil {
				t.Fatal(e)
			}
			f := os.NewFile(uintptr(h), path)
			if _, e := f.Write([]byte("held source\n")); e != nil || f.Sync() != nil {
				t.Fatal("creatorwrite")
			}
			s.files = append(s.files, f)
			s.paths[path] = true
			id, e := fileSoftwareIdentity(f)
			if e != nil {
				t.Fatal(e)
			}
			calls := 0
			reader, err := s.readOnlySnapshotFile(f, func(stage string) error {
				calls++
				if os.WriteFile(path, []byte("changed"), 0600) == nil || os.Rename(path, path+".foreign") == nil {
					t.Fatal("writer or rename allowed during handoff", stage)
				}
				if stage == fault {
					return errors.New("injected_handoff_cancel")
				}
				return nil
			})
			if fault != "normal" {
				if err == nil || reader != nil {
					t.Fatal("injected failure succeeded")
				}
				// The helper may close its creator before an injected failure.
				// This direct helper fixture owns that entry and removes it from
				// the enclosing snapshot's cleanup list after explicitly closing.
				f.Close()
				s.files = nil
				return
			}
			if err != nil || reader == nil || calls < 5 {
				t.Fatal("handoff failed", err, calls)
			}
			s.files[0] = reader
			if got, e := fileSoftwareIdentity(reader); e != nil || got != id {
				t.Fatal("generation changed")
			}
			if os.WriteFile(path, []byte("changed"), 0600) == nil || os.Rename(path, path+".foreign") == nil {
				t.Fatal("handoff lost writer protection")
			}
		})
	}
}

func TestSnapshotReadHandoffAllowsMSVC(t *testing.T) {
	cl := "C:/Program Files (x86)/Microsoft Visual Studio/2022/BuildTools/VC/Tools/MSVC/14.44.35207/bin/HostX64/x64/cl.exe"
	if _, e := os.Stat(cl); os.IsNotExist(e) {
		t.Skip("explicit workstation native compiler")
	}
	root := physicalSoftwareFixture(t)
	s := &softwareBuildSnapshot{Root: root, paths: map[string]bool{root: true}}
	defer s.Close()
	for _, entry := range []struct{ name, body string }{{"source.c", "#include \"owned.h\"\nint owned(void){return OWNED;}\n"}, {"owned.h", "#define OWNED 7\n"}} {
		if e := s.create(filepath.Join(root, entry.name), []byte(entry.body)); e != nil {
			t.Fatal(e)
		}
	}
	cmd := exec.Command(cl, "/nologo", "/c", filepath.Join(root, "source.c"), "/Fo"+filepath.Join(root, "source.obj"))
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	raw, e := cmd.CombinedOutput()
	defer clear(raw)
	if e != nil {
		cause := ""
		for _, line := range strings.Split(string(raw), "\n") {
			if i := strings.Index(line, "fatal error"); i >= 0 {
				cause = line[i:]
				break
			}
		}
		t.Fatal("handoff compiler failed", cause)
	}
}

func TestSnapshotReadHandoffRejectsOwnerACLMutation(t *testing.T) {
	root := physicalSoftwareFixture(t)
	path := filepath.Join(root, "source.txt")
	ptr, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		t.Fatal(e)
	}
	h, e := syscall.CreateFile(ptr, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ, nil, syscall.CREATE_NEW, 0x00200000, 0)
	if e != nil {
		t.Fatal(e)
	}
	f := os.NewFile(uintptr(h), path)
	defer f.Close()
	if _, e := f.Write([]byte("original")); e != nil {
		t.Fatal(e)
	}
	f.Sync()
	acl, e := updateFileACL(f)
	if e != nil {
		t.Fatal(e)
	}
	s := &softwareBuildSnapshot{Root: root, paths: map[string]bool{root: true}}
	defer s.Close()
	mutated := false
	reader, err := s.readOnlySnapshotFile(f, func(stage string) error {
		if stage != "creator_closed" {
			return nil
		}
		control, e := syscall.CreateFile(ptr, 0x00040000|0x00020000, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0x00200000, 0)
		if e != nil {
			t.Fatal("owned ACL control", e)
		}
		defer syscall.CloseHandle(control)
		text, e := syscall.UTF16PtrFromString(acl)
		if e != nil {
			t.Fatal(e)
		}
		var sd uintptr
		ok, _, _ := updateStringSD.Call(uintptr(unsafe.Pointer(text)), 1, uintptr(unsafe.Pointer(&sd)), 0)
		if ok == 0 {
			t.Fatal("ACL parse")
		}
		defer updateLocalFree.Call(sd)
		code, _, _ := updateNTSetSecurity.Call(uintptr(control), 4, sd)
		if code != 0 {
			t.Fatal("ACL mutation", code)
		}
		if os.WriteFile(path, []byte("replaced"), 0600) != nil {
			t.Fatal("owned mutation not reached")
		}
		mutated = true
		return nil
	})
	if reader != nil {
		defer reader.Close()
		reader.Seek(0, io.SeekStart)
	}
	if !mutated || err == nil {
		t.Fatal("owner ACL rewrite was not rejected by final content check", mutated, err)
	}
}
