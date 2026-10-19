package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

type owned2736Fixture struct {
	access          owned2736Access
	files           map[string][]byte
	pins            []owned2736Receipt
	info            syscall.ByHandleFileInformation
	job, db         object
	reads, metadata int
}

func newOwned2736Fixture(t *testing.T) *owned2736Fixture {
	t.Helper()
	f := &owned2736Fixture{files: map[string][]byte{}}
	f.job = object{"id": owned2736Job, "todo_id": 2736, "status": "succeeded", "cwd": owned2736Tree,
		"work_owner": object{"repository": "autodevjapan/cc-pc-tools-rs", "worktree": owned2736Tree, "branch": "scheduler/todo-2736-pr148-ci"}}
	f.db = object{"items": []any{object{"id": 2736, "project": "cc-pc-tools-rs", "jobs": []any{owned2736Job},
		"status": "wait", "wait_on": []any{2908}, "scheduler": object{"worktree": owned2736Tree}}}}
	f.files[filepath.Join(owned2736Evidence, "fixture.json")] = []byte(`{"fixed_creation_receipt":true}`)
	f.pins = []owned2736Receipt{{"fixture.json", hashBytes(f.files[filepath.Join(owned2736Evidence, "fixture.json")])}}
	created, err := time.Parse(time.RFC3339Nano, "2026-10-19T02:30:34.5379241+09:00")
	if err != nil {
		t.Fatal(err)
	}
	const fileID uint64 = 56857945299087113
	f.info = syscall.ByHandleFileInformation{VolumeSerialNumber: 2229119083, FileIndexHigh: uint32(fileID >> 32),
		FileIndexLow: uint32(fileID & 0xffffffff), CreationTime: syscall.NsecToFiletime(created.UnixNano()), FileAttributes: syscall.FILE_ATTRIBUTE_DIRECTORY}
	f.access = owned2736Access{
		at: created.Add(time.Hour), protected: func(string) bool { return false }, plain: func(string) bool { return true },
		origin: func(p, repo string) bool { return same(p, owned2736Tree) && repo == "autodevjapan/cc-pc-tools-rs" },
		identity: func(p string) (syscall.ByHandleFileInformation, error) {
			f.metadata++
			if !same(p, owned2736Root) {
				t.Fatal("metadata outside fixed root")
			}
			return f.info, nil
		},
		read: func(p string, limit int64) ([]byte, bool) {
			f.reads++
			if within(owned2736Root, p) {
				t.Fatal("resource contents were read")
			}
			var b []byte
			var err error
			switch {
			case same(p, productStore):
				b, err = json.Marshal(f.db)
			case same(p, owned2736JobFile):
				b, err = json.Marshal(f.job)
			default:
				b = f.files[p]
			}
			return b, b != nil && err == nil && int64(len(b)) <= limit
		},
	}
	return f
}

func TestOwned2736FixedResourceAndChild(t *testing.T) {
	for _, p := range []string{owned2736Root, filepath.Join(owned2736Root, "runtime", "runner")} {
		f := newOwned2736Fixture(t)
		if !owned2736AllowedWith(p, f.access, f.pins) || f.metadata != 1 || f.reads != 3 {
			t.Fatal("fixed ownership was not verified")
		}
	}
}

func TestOwned2736UnknownAndExpiredReadNothing(t *testing.T) {
	for _, p := range []string{filepath.Dir(owned2736Root), owned2736Root + "-other", filepath.Join(filepath.Dir(owned2736Root), "codex-owned-other"), "relative"} {
		f := newOwned2736Fixture(t)
		if owned2736AllowedWith(p, f.access, f.pins) || f.reads != 0 || f.metadata != 0 {
			t.Fatal("unknown resource caused inspection")
		}
	}
	for _, at := range []time.Time{time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC)} {
		f := newOwned2736Fixture(t)
		f.access.at = at
		if owned2736AllowedWith(owned2736Root, f.access, f.pins) || f.reads != 0 || f.metadata != 0 {
			t.Fatal("time boundary was ignored")
		}
	}
}

func TestOwned2736OwnershipFailures(t *testing.T) {
	cases := map[string]func(*owned2736Fixture){
		"deny": func(f *owned2736Fixture) { f.access.protected = func(string) bool { return true } },
		"child deny": func(f *owned2736Fixture) {
			f.access.protected = func(p string) bool { return same(p, filepath.Join(owned2736Root, "runtime")) }
		},
		"origin":             func(f *owned2736Fixture) { f.access.origin = func(string, string) bool { return false } },
		"junction or absent": func(f *owned2736Fixture) { f.access.plain = func(string) bool { return false } },
		"changed receipt": func(f *owned2736Fixture) {
			f.files[filepath.Join(owned2736Evidence, "fixture.json")] = []byte("changed")
		},
		"missing receipt":          func(f *owned2736Fixture) { delete(f.files, filepath.Join(owned2736Evidence, "fixture.json")) },
		"no pins":                  func(f *owned2736Fixture) { f.pins = nil },
		"different nonce identity": func(f *owned2736Fixture) { f.info.FileIndexLow++ },
		"different volume":         func(f *owned2736Fixture) { f.info.VolumeSerialNumber++ },
		"reparse":                  func(f *owned2736Fixture) { f.info.FileAttributes |= syscall.FILE_ATTRIBUTE_REPARSE_POINT },
		"not directory":            func(f *owned2736Fixture) { f.info.FileAttributes = 0 },
		"metadata unavailable": func(f *owned2736Fixture) {
			f.access.identity = func(string) (syscall.ByHandleFileInformation, error) { return f.info, os.ErrPermission }
		},
		"wrong job":        func(f *owned2736Fixture) { f.job["todo_id"] = 2908 },
		"wrong job origin": func(f *owned2736Fixture) { obj(f.job["work_owner"])["repository"] = "other/repo" },
		"wrong job tree":   func(f *owned2736Fixture) { f.job["cwd"] = owned2736Tree + "-other" },
		"job unfinished":   func(f *owned2736Fixture) { f.job["status"] = "running" },
		"job detached":     func(f *owned2736Fixture) { obj(f.db["items"].([]any)[0])["jobs"] = []any{} },
		"wrong task tree": func(f *owned2736Fixture) {
			obj(obj(f.db["items"].([]any)[0])["scheduler"])["worktree"] = owned2736Tree + "-other"
		},
		"duplicate task":   func(f *owned2736Fixture) { rows := f.db["items"].([]any); f.db["items"] = append(rows, rows[0]) },
		"unavailable task": func(f *owned2736Fixture) { f.db["items"] = []any{} },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newOwned2736Fixture(t)
			change(f)
			if owned2736AllowedWith(filepath.Join(owned2736Root, "runtime"), f.access, f.pins) {
				t.Fatal("invalid ownership was accepted")
			}
		})
	}
}

func TestOwned2736MetadataAndPhysicalLinks(t *testing.T) {
	// CI may expose the temporary directory through an 8.3 spelling. The
	// production classifier requires canonical physical paths, so normalize
	// this fixture just like the existing conversation fixtures do.
	root, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	info, err := owned2736Identity(root)
	if err != nil || info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 || !conversationPhysical(root) {
		t.Fatalf("real directory metadata: %v", err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skip("symlink creation unavailable")
	}
	if conversationPhysical(link) {
		t.Fatal("linked resource accepted")
	}
	info, err = owned2736Identity(link)
	if err == nil && info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		t.Fatal("metadata followed link")
	}
}
