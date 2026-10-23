package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// moveObject moves the loose file of id from one object store to another.
func moveObject(t *testing.T, from, to, id string) {
	t.Helper()
	src := filepath.Join(from, id[:2], id[2:])
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(to, id[:2], id[2:]), string(data))
	if err := os.Chmod(src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
}

// alternateDuringReads names dir in the alternates file of the source's store
// right after the reader has found none and has first read the main ref, and
// removes the file again before the recheck, which therefore cannot see it.
func (f *fixture) alternateDuringReads(dir string) {
	alternates := filepath.Join(f.common, "objects", "info", "alternates")
	mainRef := key(filepath.Join(f.common, "refs", "remotes", "origin", "main"))
	written := false
	f.onRead = func(path string) {
		if path == mainRef && !written {
			written = true
			writeFile(f.t, alternates, filepath.ToSlash(dir)+"\n")
		}
	}
	f.onRecheck = func() {
		if err := os.Remove(alternates); err != nil {
			f.t.Fatal(err)
		}
	}
}

// expectUntouchedBelow fails if anything at or below dir was classified or read.
func expectUntouchedBelow(t *testing.T, f *fixture, dir string) {
	t.Helper()
	prefix := key(dir)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, event := range f.events {
		for _, kind := range []string{"read:", "guard-path:"} {
			if path, ok := strings.CutPrefix(event, kind); ok && (path == prefix || strings.HasPrefix(path, prefix+`\`)) {
				t.Errorf("%s was classified or read: %s", dir, event)
			}
		}
	}
}

// The reader takes objects from the source's own store only. An alternate
// store named only while the reads run is never read: an object that only it
// holds is missing, and a protected store it names is never classified.
func TestObjectsIgnoreAlternates(t *testing.T) {
	t.Run("object only in the alternate store", func(t *testing.T) {
		f := newFixture(t, nil)
		elsewhere := filepath.Join(f.base, "elsewhere", "objects")
		moveObject(t, filepath.Join(f.common, "objects"), elsewhere, f.p.head)
		f.alternateDuringReads(elsewhere)
		expectReason(t, f, "source_git")
		expectUntouchedBelow(t, f, filepath.Join(f.base, "elsewhere"))
	})
	t.Run("protected alternate store", func(t *testing.T) {
		f := newFixture(t, nil)
		elsewhere := filepath.Join(f.base, "elsewhere", "objects")
		if err := os.MkdirAll(elsewhere, 0o755); err != nil {
			t.Fatal(err)
		}
		f.protected[key(elsewhere)] = clientVerdict
		f.alternateDuringReads(elsewhere)
		if got, reason := f.verify(); !got.Qualified || reason != "" {
			t.Fatalf("verify = qualified %v reason %q with every object in the source's store", got.Qualified, reason)
		}
		expectUntouchedBelow(t, f, filepath.Join(f.base, "elsewhere"))
	})
}

// Git trusts the "sorted" trait of packed-refs and finds a name by binary
// search, and it refuses a header without the space after the colon. The
// reader refuses both files instead of reading a ref that Git would not.
func TestPackedRefsAsGitReadsThem(t *testing.T) {
	for name, rewrite := range map[string]func(header string, records []string) string{
		"records out of the declared order": func(header string, records []string) string {
			slices.Reverse(records)
			return header + "\n" + strings.Join(records, "\n") + "\n"
		},
		"header without the space after the colon": func(header string, records []string) string {
			return strings.Replace(header, "with: ", "with:", 1) + "\n" + strings.Join(records, "\n") + "\n"
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			f.gitIn(f.repo, "pack-refs", "--all")
			packed := filepath.Join(f.common, "packed-refs")
			data, err := os.ReadFile(packed)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
			if len(lines) < 3 || lines[0] != "# pack-refs with: peeled fully-peeled sorted " {
				t.Fatalf("pack-refs wrote %q", data)
			}
			writeFile(t, packed, rewrite(lines[0], lines[1:]))
			if out, err := testGit(t, f.root, "rev-parse", "--verify", "--quiet", f.p.mainRef); err == nil {
				t.Fatalf("Git still resolved %s to %s", f.p.mainRef, out)
			}
			expectReason(t, f, "source_git")
		})
	}
}
