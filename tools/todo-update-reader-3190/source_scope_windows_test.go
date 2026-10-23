package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// objectReads are the only Git commands the reader may run, with exactly these
// arguments. Each reads commits or trees from the fixed object store.
func objectReads(f *fixture) []string {
	return []string{
		"rev-parse --verify --quiet " + f.p.head + "^{tree}",
		"merge-base --is-ancestor " + f.p.head + " " + f.main,
		"ls-tree -r -z --full-tree " + f.p.head,
	}
}

func gitCalls(f *fixture) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var calls []string
	for _, event := range f.events {
		if args, ok := strings.CutPrefix(event, "git:"); ok {
			calls = append(calls, args)
		}
	}
	return calls
}

// expectOnlyObjectReads fails on any Git call other than the object reads.
func expectOnlyObjectReads(t *testing.T, f *fixture) {
	t.Helper()
	allowed := objectReads(f)
	for _, call := range gitCalls(f) {
		if !slices.Contains(allowed, call) {
			t.Errorf("git %s is not one of the object reads", call)
		}
	}
}

// expectObjectReads requires exactly the object reads, each once, in order.
func expectObjectReads(t *testing.T, f *fixture) {
	t.Helper()
	if got, want := gitCalls(f), objectReads(f); !slices.Equal(got, want) {
		t.Errorf("git calls\n got %q\nwant %q", got, want)
	}
}

func expectUntouched(t *testing.T, f *fixture, name string) {
	t.Helper()
	if f.index("read:"+key(name)) >= 0 || f.index("guard-path:"+key(name)) >= 0 {
		t.Fatalf("%s was classified or read: %v", name, f.events)
	}
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

func appendFile(t *testing.T, name, content string) {
	t.Helper()
	file, err := os.OpenFile(name, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString(content); err != nil {
		t.Fatal(err)
	}
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

// The guard classifies files one by one, so Git never opens a working file.
// A protected tracked file outside the inputs cannot decide the result even
// when it changes and nobody may read it.
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
			expectUntouched(t, f, protected)
			expectObjectReads(t, f)
		})
	}
}

// Git reads no configuration file. An include that names a protected,
// unreadable file never reaches Git, and the reader refuses a repository
// configuration that Git would have to follow or that rewrites the remote.
func TestGitReadsNoConfiguration(t *testing.T) {
	t.Run("global include", func(t *testing.T) {
		f := newFixture(t, nil)
		protected := f.path("README.md")
		f.protected[key(protected)] = clientVerdict
		home := filepath.Join(f.base, "home")
		writeFile(t, filepath.Join(home, ".gitconfig"), "[include]\n\tpath = "+filepath.ToSlash(protected)+"\n")
		t.Setenv("HOME", home)
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "xdg"))
		release := denyReads(t, protected)
		defer release()
		if got, reason := f.verify(); !got.Qualified || reason != "" {
			t.Fatalf("verify = qualified %v reason %q: the global configuration reached Git", got.Qualified, reason)
		}
		expectUntouched(t, f, protected)
		expectObjectReads(t, f)
	})
	for name, section := range map[string]string{
		"include":             "[include]\n\tpath = PROTECTED\n",
		"conditional include": "[includeIf \"gitdir:C:/\"]\n\tpath = PROTECTED\n",
		"URL rewrite":         "[url \"https://example.invalid/\"]\n\tinsteadOf = https://github.com/\n",
	} {
		t.Run("repository "+name, func(t *testing.T) {
			f := newFixture(t, nil)
			protected := f.path("README.md")
			f.protected[key(protected)] = clientVerdict
			appendFile(t, filepath.Join(f.common, "config"), strings.ReplaceAll(section, "PROTECTED", filepath.ToSlash(protected)))
			release := denyReads(t, protected)
			defer release()
			expectReason(t, f, "source_git")
			if calls := gitCalls(f); len(calls) != 0 {
				t.Fatalf("Git ran %q before the configuration was refused", calls)
			}
			expectUntouched(t, f, protected)
		})
	}
}

// The reader reads the worktree link, HEAD, refs, configuration and shallow
// boundary itself, each classified by the guard first, and refuses what it
// cannot tie to the fixed source.
func TestReadsGitMetadataItself(t *testing.T) {
	t.Run("branch only in packed-refs", func(t *testing.T) {
		f := newFixture(t, nil)
		f.gitIn(f.repo, "pack-refs", "--all")
		if _, err := os.Lstat(filepath.Join(f.common, "refs", "heads", "work")); !os.IsNotExist(err) {
			t.Fatalf("pack-refs kept the loose branch: %v", err)
		}
		if got, reason := f.verify(); !got.Qualified || reason != "" {
			t.Fatalf("verify = qualified %v reason %q with packed refs", got.Qualified, reason)
		}
		expectObjectReads(t, f)
	})
	t.Run("shallow boundary", func(t *testing.T) {
		// As in the real shallow clone, HEAD's parent is not in the object
		// store. Only the boundary given to Git keeps the ancestry check from
		// looking for it.
		f := newFixture(t, nil)
		parent := f.p.head
		f.git("-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "--allow-empty", "-m", "boundary")
		head := f.gitOut("rev-parse", "HEAD")
		f.gitIn(f.repo, "update-ref", "refs/remotes/origin/main", head)
		f.p.head, f.main = head, head
		f.receipt["source_head"] = head
		f.writeReceipt()
		if err := os.Remove(filepath.Join(f.common, "objects", parent[:2], parent[2:])); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(f.common, "shallow"), head+"\n")
		if got, reason := f.verify(); !got.Qualified || reason != "" {
			t.Fatalf("verify = qualified %v reason %q with a shallow boundary", got.Qualified, reason)
		}
		expectObjectReads(t, f)
	})
	rejections := map[string]func(f *fixture){
		"worktree file names another worktree": func(f *fixture) {
			f.gitIn(f.repo, "worktree", "add", "-q", "-b", "other", filepath.Join(f.base, "other"))
			writeFile(f.t, f.path(".git"), "gitdir: "+filepath.ToSlash(filepath.Join(f.common, "worktrees", "other"))+"\n")
		},
		"worktree registration names another checkout": func(f *fixture) {
			writeFile(f.t, filepath.Join(f.gitDir, "gitdir"), filepath.ToSlash(filepath.Join(f.base, "elsewhere", ".git"))+"\n")
		},
		"common directory elsewhere": func(f *fixture) {
			writeFile(f.t, filepath.Join(f.gitDir, "commondir"), "../../..\n")
		},
		"HEAD outside refs": func(f *fixture) {
			writeFile(f.t, filepath.Join(f.gitDir, "HEAD"), "ref: refs/heads/../../config\n")
		},
		"symbolic remote-tracking ref": func(f *fixture) {
			writeFile(f.t, filepath.Join(f.common, "refs", "remotes", "origin", "main"), "ref: refs/heads/main\n")
		},
		"per-worktree configuration": func(f *fixture) {
			f.gitIn(f.repo, "config", "extensions.worktreeConfig", "true")
			writeFile(f.t, filepath.Join(f.gitDir, "config.worktree"), "[core]\n\tbare = false\n")
		},
		"alternate object store": func(f *fixture) {
			writeFile(f.t, filepath.Join(f.common, "objects", "info", "alternates"), filepath.ToSlash(filepath.Join(f.base, "elsewhere"))+"\n")
		},
		"reftable refs": func(f *fixture) {
			appendFile(f.t, filepath.Join(f.common, "config"), "[extensions]\n\trefStorage = reftable\n")
		},
		"bare repository": func(f *fixture) {
			appendFile(f.t, filepath.Join(f.common, "config"), "[core]\n\tbare = true\n")
		},
		"work tree set by configuration": func(f *fixture) {
			appendFile(f.t, filepath.Join(f.common, "config"), "[core]\n\tworktree = "+filepath.ToSlash(filepath.Join(f.base, "elsewhere"))+"\n")
		},
		"second origin URL": func(f *fixture) {
			appendFile(f.t, filepath.Join(f.common, "config"), "[remote \"origin\"]\n\turl = https://github.com/AutoDevJapan/cc-pc-tools-rs.git\n")
		},
		"configuration line not understood": func(f *fixture) {
			appendFile(f.t, filepath.Join(f.common, "config"), "[core]\n\tbroken = \"unterminated\n")
		},
		"malformed shallow boundary": func(f *fixture) {
			writeFile(f.t, filepath.Join(f.common, "shallow"), "not a commit\n")
		},
	}
	for name, edit := range rejections {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			edit(f)
			expectReason(t, f, "source_git")
			expectOnlyObjectReads(t, f)
		})
	}
	for name, path := range map[string]func(*fixture) string{
		"worktree file":            func(f *fixture) string { return f.path(".git") },
		"worktree HEAD":            func(f *fixture) string { return filepath.Join(f.gitDir, "HEAD") },
		"repository configuration": func(f *fixture) string { return filepath.Join(f.common, "config") },
		"remote-tracking ref":      func(f *fixture) string { return filepath.Join(f.common, "refs", "remotes", "origin", "main") },
		"object store":             func(f *fixture) string { return filepath.Join(f.common, "objects") },
	} {
		t.Run("protected "+name, func(t *testing.T) {
			f := newFixture(t, nil)
			refused := path(f)
			f.protected[key(refused)] = clientVerdict
			expectReason(t, f, "guard_classification")
			if f.index("read:"+key(refused)) >= 0 {
				t.Fatalf("%s was read although the guard refused it", refused)
			}
			if calls := gitCalls(f); len(calls) != 0 {
				t.Fatalf("Git ran %q although the source was refused", calls)
			}
		})
	}
}

func TestIsolatedGitEnvironment(t *testing.T) {
	parent := []string{`PATH=C:\bin`, `HOME=C:\home`, `GIT_DIR=C:\evil`, `Git_Config_Global=C:\evil.cfg`, "GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=include.path"}
	got := isolatedEnvironment(parent, `C:\tmp\g`, `C:\repo\.git\objects`)
	want := []string{`PATH=C:\bin`, `HOME=C:\home`, `GIT_DIR=C:\tmp\g`, `GIT_OBJECT_DIRECTORY=C:\repo\.git\objects`,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0"}
	if !slices.Equal(got, want) {
		t.Fatalf("environment\n got %q\nwant %q", got, want)
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
