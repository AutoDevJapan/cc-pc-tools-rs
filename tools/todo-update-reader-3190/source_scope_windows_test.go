package main

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// objectOnlyGit lists the Git commands that read only refs, configuration and
// objects. Others, such as diff, status, ls-files, hash-object or add, can open
// working files that the guard has not classified one by one.
var objectOnlyGit = map[string]bool{"rev-parse": true, "remote": true, "merge-base": true, "ls-tree": true}

func gitSubcommand(args string) string {
	fields := strings.Fields(args)
	for i := 0; i < len(fields); i++ {
		if fields[i] == "-c" {
			i++
			continue
		}
		return fields[i]
	}
	return ""
}

// expectGitObjectOnly fails on any recorded Git call that can read working
// files and returns how many Git calls the reader made.
func expectGitObjectOnly(t *testing.T, f *fixture) int {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	calls := 0
	for _, event := range f.events {
		if args, ok := strings.CutPrefix(event, "git:"); ok {
			calls++
			if !objectOnlyGit[gitSubcommand(args)] {
				t.Errorf("git %s can read working files the guard has not classified", args)
			}
		}
	}
	return calls
}

// denyReads holds name open without sharing, so every other open fails until
// the returned function closes it.
func denyReads(t *testing.T, name string) func() {
	t.Helper()
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(name); err == nil {
		syscall.CloseHandle(h)
		t.Fatalf("%s could still be read while held", name)
	}
	return func() { syscall.CloseHandle(h) }
}

// rebuildReceipt records the working inputs as they are now, as a receipt
// written after the change would; the blobs stay those of the fixed tree.
func (f *fixture) rebuildReceipt() {
	f.t.Helper()
	for i, in := range f.inputs {
		f.inputs[i] = f.input(in.Path)
	}
	f.writeReceipt()
}

// The guard classifies files one by one, so Git may read only refs,
// configuration and objects. A protected tracked file outside the inputs is
// never opened: it cannot decide the result even when it changes and nobody
// may read it.
func TestGitNeverReadsWorkingFiles(t *testing.T) {
	cases := map[string]func(t *testing.T, name string){
		"timestamp changed": func(t *testing.T, name string) {
			later := time.Now().Add(time.Hour)
			if err := os.Chtimes(name, later, later); err != nil {
				t.Fatal(err)
			}
		},
		"content changed": func(t *testing.T, name string) { writeFile(t, name, "private change\r\n") },
	}
	for caseName, change := range cases {
		t.Run(caseName, func(t *testing.T) {
			f := newFixture(t, nil)
			protected := f.path("README.md")
			f.protected[key(protected)] = clientVerdict
			change(t, protected)
			release := denyReads(t, protected)
			defer release()
			got, reason := f.verify()
			if !got.Qualified || reason != "" {
				t.Fatalf("verify = qualified %v reason %q: a protected file outside the inputs decided the result", got.Qualified, reason)
			}
			if f.index("read:"+key(protected)) >= 0 || f.index("guard-path:"+key(protected)) >= 0 {
				t.Fatalf("a file outside the inputs was classified or read: %v", f.events)
			}
			if expectGitObjectOnly(t, f) == 0 {
				t.Fatalf("the source was verified without Git")
			}
		})
	}
}

// With Git kept off the working files, the reader itself ties each pinned
// input to the fixed tree's blob, in the form Git writes it out on this PC.
func TestInputsMustBeTheCommittedBytes(t *testing.T) {
	t.Run("LF working file is the blob itself", func(t *testing.T) {
		f := newFixture(t, nil)
		f.write("lib-a/src/lib.rs", fixtureTracked["lib-a/src/lib.rs"])
		f.rebuildReceipt()
		if got, reason := f.verify(); !got.Qualified || reason != "" {
			t.Fatalf("verify = qualified %v reason %q for the blob written without conversion", got.Qualified, reason)
		}
	})
	cases := map[string]func(*fixture){
		"edited input with a receipt that agrees": func(f *fixture) {
			f.write("lib-a/src/lib.rs", crlf("pub fn a() { panic!() }\n"))
		},
		"mixed line endings": func(f *fixture) {
			f.write("Cargo.toml", strings.Replace(crlf(fixtureTracked["Cargo.toml"]), "\r\n", "\n", 1))
		},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			edit(f)
			f.rebuildReceipt()
			expectReason(t, f, "input")
		})
	}
}

func TestCheckedOutForms(t *testing.T) {
	// Object names printed by git hash-object.
	if got := gitBlobID(nil); got != "e69de29bb2d1d6434b8b29ae775ad8c2e48c5391" {
		t.Fatalf("empty blob = %q", got)
	}
	if got := gitBlobID([]byte("hello\n")); got != "ce013625030ba8dba906f756967f9e9ca394464a" {
		t.Fatalf("hello blob = %q", got)
	}
	cases := []struct {
		name, working, blob string
		want                bool
	}{
		{"the blob itself", "a\nb\n", "a\nb\n", true},
		{"every LF written as CRLF", "a\r\nb\r\n", "a\nb\n", true},
		{"a blob that already has CRLF", "a\r\nb\r\n", "a\r\nb\r\n", true},
		{"one LF left as LF", "a\r\nb\n", "a\nb\n", false},
		{"an extra CR", "a\r\r\nb\r\n", "a\r\nb\n", false},
		{"binary is never converted", "a\x00\r\nb\r\n", "a\x00\nb\n", false},
		{"other bytes", "a\r\nc\r\n", "a\nb\n", false},
	}
	for _, tc := range cases {
		if got := checkedOut([]byte(tc.working), gitBlobID([]byte(tc.blob))); got != tc.want {
			t.Errorf("%s: checkedOut = %v, want %v", tc.name, got, tc.want)
		}
	}
}
