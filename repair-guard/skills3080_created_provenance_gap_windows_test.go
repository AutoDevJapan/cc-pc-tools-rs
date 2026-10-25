package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// These are counterexamples for the proposed post-creation qualification,
// not an implementation of that qualification. Only synthetic fixtures are
// opened. The existing successful CreateNew contract remains unchanged. The
// 64-bit fixture identity only distinguishes the two simultaneously retained
// local objects; it is NOT a historical, deletion-safe or ReFS qualification.
type skillsFixtureIdentity struct {
	volume uint32
	index  uint64
}

func skillsFixtureObject(t *testing.T, path string) (skillsFixtureIdentity, syscall.ByHandleFileInformation) {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(name, 0x80 /* FILE_READ_ATTRIBUTES */, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(h)
	var info syscall.ByHandleFileInformation
	if err = syscall.GetFileInformationByHandle(h, &info); err != nil || info.NumberOfLinks != 1 || info.FileAttributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT) != 0 {
		t.Fatalf("synthetic regular single-link object required: %v", err)
	}
	return skillsFixtureIdentity{info.VolumeSerialNumber, uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)}, info
}

func TestSkills3080CreatedResourceProvenanceGap(t *testing.T) {
	for _, replace := range []bool{false, true} {
		name := "same_created_object"
		if replace {
			name = "byte_identical_replacement_with_copied_timestamps"
		}
		t.Run(name, func(t *testing.T) {
			root := skillsFixtureRoot(t)
			manifest := skillsParsedFixture(t)
			fs := skillsFS{blocked: func(string) bool { return false }}
			t.Cleanup(fs.close)
			count, reason := skillsCreate(manifest, root, &fs)
			if reason != "" || count != len(manifest.Files) {
				t.Fatalf("real synthetic CreateNew did not complete: %s/%d", reason, count)
			}
			path := filepath.Join(root, filepath.FromSlash(manifest.Files[0].Target))
			createdID, createdInfo := skillsFixtureObject(t, path)
			body := append([]byte(nil), manifest.Files[0].body...)
			fs.close()
			fs.files, fs.dirs = nil, nil
			if replace {
				// Retain the real original. No deletion or ID reuse is needed to
				// show that later exact path/hash/size/time equality is insufficient.
				if err := os.Rename(path, filepath.Join(root, "retained-original.txt")); err != nil {
					t.Fatal(err)
				}
				file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = file.Write(body); err != nil {
					file.Close()
					t.Fatal(err)
				}
				// File timestamps can be preserved by another writer; they are
				// not a missing creation-time file ID or an OS change history.
				err = syscall.SetFileTime(syscall.Handle(file.Fd()), &createdInfo.CreationTime, &createdInfo.LastAccessTime, &createdInfo.LastWriteTime)
				if closeErr := file.Close(); err != nil || closeErr != nil {
					t.Fatalf("timestamp copy/close: %v/%v", err, closeErr)
				}
			}
			currentID, currentInfo := skillsFixtureObject(t, path)
			current, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(current, body) || hashBytes(current) != manifest.Files[0].SHA || currentInfo.FileSizeHigh != createdInfo.FileSizeHigh || currentInfo.FileSizeLow != createdInfo.FileSizeLow || currentInfo.CreationTime != createdInfo.CreationTime || currentInfo.LastWriteTime != createdInfo.LastWriteTime {
				t.Fatalf("counterexample must preserve every declared byte/hash/size/time input: %v", err)
			}
			if (currentID == createdID) == replace {
				t.Fatal("creation-time identity did not distinguish the synthetic replacement")
			}
			// The old return values are identical in both cases: success/count
			// describes the completed operation, not a later object's identity.
			t.Logf("CreateNew completed=%d; exact reviewed bytes=true; same created resource=%t", count, currentID == createdID)
		})
	}
}

func TestSkills3080CreationIdentityIsAvailableBeforeHandlesClose(t *testing.T) {
	root := skillsFixtureRoot(t)
	manifest := skillsParsedFixture(t)
	fs := skillsFS{blocked: func(string) bool { return false }}
	defer fs.close()
	count, reason := skillsCreate(manifest, root, &fs)
	if reason != "" || count != len(manifest.Files) || len(fs.files) != count {
		t.Fatal("synthetic exclusive creation failed")
	}
	for _, file := range fs.files {
		var created syscall.ByHandleFileInformation
		if err := syscall.GetFileInformationByHandle(syscall.Handle(file.Fd()), &created); err != nil {
			t.Fatal(err)
		}
		id, _ := skillsFixtureObject(t, file.Name())
		if id != (skillsFixtureIdentity{created.VolumeSerialNumber, uint64(created.FileIndexHigh)<<32 | uint64(created.FileIndexLow)}) {
			t.Fatal("same still-held creation handle did not identify its current object")
		}
		if err := os.Rename(file.Name(), filepath.Join(root, "substitution.txt")); !errors.Is(err, syscall.Errno(32) /* ERROR_SHARING_VIOLATION */) {
			t.Fatalf("expected the held handle's sharing violation, got %v", err)
		}
	}
}
