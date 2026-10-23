package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func looseObject(f *fixture, id string) string {
	return filepath.Join(f.common, "objects", id[:2], id[2:])
}

// writable clears the read-only attribute Git gives object files.
func writable(t *testing.T, path string) {
	t.Helper()
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
}

func removeObject(t *testing.T, path string) {
	t.Helper()
	writable(t, path)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

// moveObject moves the loose file of id from one object store to another.
func moveObject(t *testing.T, from, to, id string) {
	t.Helper()
	src := filepath.Join(from, id[:2], id[2:])
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(to, id[:2], id[2:]), string(data))
	removeObject(t, src)
}

// looseCount counts the loose object files of a store.
func looseCount(t *testing.T, objects string) int {
	t.Helper()
	entries, err := os.ReadDir(objects)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() || len(e.Name()) != 2 {
			continue
		}
		files, err := os.ReadDir(filepath.Join(objects, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		n += len(files)
	}
	return n
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

// expectReadsClassified requires every file the reader opened, other than the
// installed leaf that its purpose entry qualifies, to have been classified by
// the guard before.
func expectReadsClassified(t *testing.T, f *fixture) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	classified := map[string]bool{}
	for _, event := range f.events {
		if path, ok := strings.CutPrefix(event, "guard-path:"); ok {
			classified[path] = true
		} else if path, ok := strings.CutPrefix(event, "read:"); ok && !classified[path] && path != key(f.target) {
			t.Errorf("%s was read before its guard classification", path)
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

// gitRaw returns Git's standard output exactly.
func gitRaw(t *testing.T, dir string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	cmd.Env = childEnvironment(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return out
}

type gitObject struct {
	kind string
	data []byte
}

// gitObjects returns every object of the repository at dir as Git reads it.
func gitObjects(t *testing.T, dir string) map[string]gitObject {
	t.Helper()
	out := gitRaw(t, dir, "cat-file", "--batch-all-objects", "--batch")
	objects := map[string]gitObject{}
	for len(out) > 0 {
		line, rest, _ := bytes.Cut(out, []byte("\n"))
		fields := strings.Fields(string(line))
		if len(fields) != 3 {
			t.Fatalf("cat-file printed %q", line)
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || len(rest) < size+1 || rest[size] != '\n' {
			t.Fatalf("cat-file printed %q and %d more bytes", line, len(rest))
		}
		objects[fields[0]] = gitObject{kind: fields[1], data: rest[:size]}
		out = rest[size+1:]
	}
	return objects
}

// parseLsTree reads `git ls-tree -r -z --full-tree` output.
func parseLsTree(t *testing.T, out []byte) map[string]treeEntry {
	t.Helper()
	tree := map[string]treeEntry{}
	for _, record := range strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00") {
		meta, name, ok := strings.Cut(record, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			t.Fatalf("ls-tree printed %q", record)
		}
		tree[name] = treeEntry{mode: fields[0], kind: fields[1], object: fields[2]}
	}
	return tree
}

// historyRepo is a repository whose history gives Git much to store as deltas:
// a long file edited a line at a time and a directory edited a file at a time,
// then an executable, a symbolic link and a submodule entry.
func historyRepo(t *testing.T) (string, func(args ...string) string) {
	t.Helper()
	dir := filepath.Join(testLongPath(t, t.TempDir()), "history")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) string {
		t.Helper()
		out, err := testGit(t, dir, args...)
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return out
	}
	commit := func(message string) {
		git("-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "--allow-empty", "-m", message)
	}
	git("init", "-q", "-b", "main")
	git("config", "core.autocrlf", "false")
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d of a file long enough for Git to store its versions as deltas", i)
	}
	for round := range 8 {
		lines[round*37] = fmt.Sprintf("line edited in round %d", round)
		writeFile(t, filepath.Join(dir, "long.txt"), strings.Join(lines, "\n")+"\n")
		for i := range 40 {
			content := fmt.Sprintf("file %d\n", i)
			if i == round {
				content += fmt.Sprintf("edited in round %d\n", round)
			}
			writeFile(t, filepath.Join(dir, "many", fmt.Sprintf("file-%02d.txt", i)), content)
		}
		git("add", "-A")
		commit(fmt.Sprintf("round %d", round))
	}
	git("update-index", "--chmod=+x", "many/file-00.txt")
	git("update-index", "--add", "--cacheinfo", "120000,"+git("rev-parse", "HEAD:long.txt")+",link")
	git("update-index", "--add", "--cacheinfo", "160000,"+git("rev-parse", "HEAD")+",module")
	commit("modes")
	return dir, git
}

func clearGuard(args ...string) ([]byte, int, bool) {
	return []byte(clearVerdict + "\n"), 0, true
}

// The reader reads every object as Git does, loose, whole in a pack or as a
// delta against an offset or a named base, and reads commits, trees and
// ancestry as Git does.
func TestObjectStoreAgreesWithGit(t *testing.T) {
	repack := []string{"repack", "-a", "-d", "-f", "-q", "--depth=50", "--window=50"}
	cases := map[string]struct {
		prepare         func(git func(args ...string) string)
		ofs, ref, loose bool
	}{
		"loose objects": {prepare: func(func(args ...string) string) {}, loose: true},
		"offset deltas": {prepare: func(git func(args ...string) string) { git(repack...) }, ofs: true},
		"named-base deltas": {prepare: func(git func(args ...string) string) {
			git(append([]string{"-c", "repack.useDeltaBaseOffset=false"}, repack...)...)
		}, ref: true},
		"pack and loose objects": {prepare: func(git func(args ...string) string) {
			git(repack...)
			git("-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "--allow-empty", "-m", "after the repack")
		}, ofs: true, loose: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir, git := historyRepo(t)
			tc.prepare(git)
			objects := filepath.Join(dir, ".git", "objects")
			s := newSession(environment{guard: clearGuard})
			defer s.close()
			store, reason := s.openObjects(sourceObjects{dir: objects}, productionProfile.limits)
			if reason != "" {
				t.Fatalf("openObjects refused the store: %s", reason)
			}
			defer store.close()

			kinds := map[int]int{}
			for _, p := range store.packs {
				if !store.openPack(p) {
					t.Fatalf("%s did not open", p.path)
				}
				for i := range p.count {
					off, ok := p.offset(i)
					h, read := p.header(off)
					if !ok || !read {
						t.Fatalf("entry %d of %s", i, p.path)
					}
					kinds[h.kind]++
				}
			}
			if kinds[objOfsDelta] > 0 != tc.ofs || kinds[objRefDelta] > 0 != tc.ref {
				t.Fatalf("pack entry types %v", kinds)
			}
			if loose := looseCount(t, objects); loose > 0 != tc.loose {
				t.Fatalf("%d loose objects", loose)
			}

			for id, want := range gitObjects(t, dir) {
				kind, data, ok := store.object(id)
				if !ok || kind != want.kind || !bytes.Equal(data, want.data) {
					t.Fatalf("object %s: reader read %q of %d bytes (%v), Git %q of %d bytes", id, kind, len(data), ok, want.kind, len(want.data))
				}
			}

			head := git("rev-parse", "HEAD")
			tree, parents, _, ok := store.commit(head)
			wantParents := strings.Fields(git("rev-list", "--parents", "-n", "1", "HEAD"))[1:]
			if !ok || tree != git("rev-parse", "HEAD^{tree}") || !slices.Equal(parents, wantParents) {
				t.Fatalf("commit %s: tree %s parents %v (%v)", head, tree, parents, ok)
			}
			listed, ok := store.listTree(tree)
			if want := parseLsTree(t, gitRaw(t, dir, "ls-tree", "-r", "-z", "--full-tree", "HEAD")); !ok || !maps.Equal(listed, want) {
				t.Fatalf("tree listing\n got %v\nwant %v", listed, want)
			}
			if root := git("rev-list", "--max-parents=0", "HEAD"); !store.reaches(head, root) || store.reaches(root, head) {
				t.Fatal("the reader's ancestry differs from Git's")
			}
		})
	}
}

// A store whose objects are all in a pack qualifies as a loose one does, and
// every index and pack is classified before it is read.
func TestQualifiesFromPacks(t *testing.T) {
	f := newFixture(t, nil)
	f.gitIn(f.repo, "repack", "-a", "-d", "-q")
	if loose := looseCount(t, filepath.Join(f.common, "objects")); loose != 0 {
		t.Fatalf("%d loose objects are left after the repack", loose)
	}
	if got, reason := f.verify(); !got.Qualified || reason != "" {
		t.Fatalf("verify = qualified %v reason %q from a pack", got.Qualified, reason)
	}
	expectReadsClassified(t, f)
	for _, ext := range []string{"idx", "pack"} {
		matches, err := filepath.Glob(filepath.Join(f.common, "objects", "pack", "pack-*."+ext))
		if err != nil || len(matches) != 1 || f.index("read:"+key(matches[0])) < 0 {
			t.Fatalf("the %s file %v was not read (%v)", ext, matches, err)
		}
	}
}

// The reader lists the pack directory through the handle that it verified. A
// junction put in the directory's place after the pin leads the reader
// nowhere: no name from the junction's target is listed, classified or read.
func TestListsOnlyThePinnedPackDirectory(t *testing.T) {
	f := newFixture(t, nil)
	f.gitIn(f.repo, "repack", "-a", "-d", "-q")
	dir := filepath.Join(f.common, "objects", "pack")
	elsewhere := filepath.Join(f.base, "elsewhere")
	unlisted := "pack-" + strings.Repeat("f", 40)
	writeFile(t, filepath.Join(elsewhere, unlisted+".idx"), "an index that the guard never classified")
	writeFile(t, filepath.Join(elsewhere, unlisted+".pack"), "a pack that the guard never classified")
	swapped := false
	f.onList = func(listed string) {
		if listed != key(dir) || swapped {
			return
		}
		if err := os.Rename(dir, dir+"-pinned"); err != nil {
			t.Fatalf("the pinned pack directory could not be moved: %v", err)
		}
		junction(t, dir, elsewhere)
		swapped = true
	}
	expectReason(t, f, "source_git")
	if !swapped {
		t.Fatal("the reader never listed the pack directory")
	}
	f.mu.Lock()
	for _, event := range f.events {
		if strings.Contains(event, unlisted) {
			t.Errorf("a name from the junction's target reached the reader: %s", event)
		}
	}
	f.mu.Unlock()
	expectUntouchedBelow(t, f, elsewhere)
}

// The reader refuses objects that it cannot read as Git wrote them, and files
// of the store that the guard does not clear, before reading them.
func TestRejectsDamagedObjects(t *testing.T) {
	repack := func(f *fixture) { f.gitIn(f.repo, "repack", "-a", "-d", "-q") }
	packFile := func(f *fixture, ext string) string {
		matches, err := filepath.Glob(filepath.Join(f.common, "objects", "pack", "pack-*."+ext))
		if err != nil || len(matches) != 1 {
			f.t.Fatalf("pack files %v: %v", matches, err)
		}
		return matches[0]
	}
	flip := func(t *testing.T, path string, at int64) {
		t.Helper()
		writable(t, path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if at < 0 {
			at += int64(len(data))
		}
		data[at] ^= 0xff
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// The middle of an entry's compressed data, from git verify-pack -v:
	// "<name> <type> <size> <size in pack> <offset>".
	middleOf := func(f *fixture, id string) int64 {
		for _, line := range strings.Split(f.gitOut("verify-pack", "-v", packFile(f, "idx")), "\n") {
			if fields := strings.Fields(line); len(fields) >= 5 && fields[0] == id {
				size, err1 := strconv.ParseInt(fields[3], 10, 64)
				off, err2 := strconv.ParseInt(fields[4], 10, 64)
				if err1 != nil || err2 != nil {
					f.t.Fatalf("verify-pack printed %q", line)
				}
				return off + size/2
			}
		}
		f.t.Fatalf("%s is not in the pack", id)
		return 0
	}
	cases := map[string]func(f *fixture){
		"loose file holds another object": func(f *fixture) {
			data, err := os.ReadFile(looseObject(f, f.p.tree))
			if err != nil {
				f.t.Fatal(err)
			}
			writable(f.t, looseObject(f, f.p.head))
			writeFile(f.t, looseObject(f, f.p.head), string(data))
		},
		// A commit of the same tree with no parent would pass every other check:
		// only the object's name refuses it.
		"loose file holds another commit of the same tree": func(f *fixture) {
			other := f.gitOut("-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit-tree", f.p.tree, "-m", "another commit")
			data, err := os.ReadFile(looseObject(f, other))
			if err != nil {
				f.t.Fatal(err)
			}
			writable(f.t, looseObject(f, f.p.head))
			writeFile(f.t, looseObject(f, f.p.head), string(data))
		},
		"loose file is not a zlib stream": func(f *fixture) {
			writable(f.t, looseObject(f, f.p.head))
			writeFile(f.t, looseObject(f, f.p.head), "not an object")
		},
		"loose file has a second hard link": func(f *fixture) {
			if err := os.Link(looseObject(f, f.p.head), filepath.Join(f.base, "object-alias")); err != nil {
				f.t.Fatal(err)
			}
		},
		"pack entry damaged": func(f *fixture) {
			repack(f)
			flip(f.t, packFile(f, "pack"), middleOf(f, f.p.head))
		},
		// The first object's CRC, which nothing but the index's own checksum covers.
		"pack index damaged": func(f *fixture) {
			repack(f)
			idx := packFile(f, "idx")
			data, err := os.ReadFile(idx)
			if err != nil {
				f.t.Fatal(err)
			}
			flip(f.t, idx, int64(8+256*4+20*binary.BigEndian.Uint32(data[8+255*4:])))
		},
		"pack is not the one indexed": func(f *fixture) {
			repack(f)
			flip(f.t, packFile(f, "pack"), -1)
		},
		"pack directory is a junction": func(f *fixture) {
			repack(f)
			dir := filepath.Join(f.common, "objects", "pack")
			if err := os.Rename(dir, dir+"-real"); err != nil {
				f.t.Fatal(err)
			}
			junction(f.t, dir, dir+"-real")
		},
	}
	for name, damage := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			damage(f)
			expectReason(t, f, "source_git")
		})
	}
	for name, path := range map[string]func(*fixture) string{
		"loose object":   func(f *fixture) string { return looseObject(f, f.p.head) },
		"pack index":     func(f *fixture) string { repack(f); return packFile(f, "idx") },
		"pack":           func(f *fixture) string { repack(f); return packFile(f, "pack") },
		"pack directory": func(f *fixture) string { return filepath.Join(f.common, "objects", "pack") },
	} {
		t.Run("protected "+name, func(t *testing.T) {
			f := newFixture(t, nil)
			refused := path(f)
			f.protected[key(refused)] = clientVerdict
			expectReason(t, f, "guard_classification")
			if f.index("read:"+key(refused)) >= 0 {
				t.Fatalf("%s was read although the guard refused it", refused)
			}
		})
	}
}

// Deltas the reader must refuse rather than misapply.
func TestApplyDeltaRefusesMalformedDeltas(t *testing.T) {
	base := []byte("0123456789")
	// A delta from the 10-byte base to "345XY": copy 3 bytes at 3, insert "XY".
	good := []byte{10, 5, 0x80 | 0x01 | 0x10, 3, 3, 2, 'X', 'Y'}
	if out, ok := applyDelta(base, good); !ok || string(out) != "345XY" {
		t.Fatalf("applyDelta = %q, %v", out, ok)
	}
	for name, delta := range map[string][]byte{
		"base size differs":        {9, 5, 0x80 | 0x01 | 0x10, 3, 3, 2, 'X', 'Y'},
		"result longer than said":  {10, 4, 0x80 | 0x01 | 0x10, 3, 3, 2, 'X', 'Y'},
		"result shorter than said": {10, 6, 0x80 | 0x01 | 0x10, 3, 3, 2, 'X', 'Y'},
		"copy beyond the base":     {10, 5, 0x80 | 0x01 | 0x10, 9, 3, 2, 'X', 'Y'},
		"reserved opcode":          {10, 5, 0, 0x80 | 0x01 | 0x10, 3, 3, 2, 'X', 'Y'},
		"insert past the end":      {10, 5, 0x80 | 0x01 | 0x10, 3, 3, 4, 'X', 'Y'},
		"copy argument missing":    {10, 5, 0x80 | 0x01 | 0x10, 3},
	} {
		if out, ok := applyDelta(base, delta); ok {
			t.Errorf("%s: applyDelta accepted %v as %q", name, delta, out)
		}
	}
}
