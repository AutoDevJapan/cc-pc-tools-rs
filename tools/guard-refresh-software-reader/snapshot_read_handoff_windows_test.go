//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
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
