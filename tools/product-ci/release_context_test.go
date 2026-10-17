package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"
)

func contextFixture(t *testing.T) (releaseTreeReply, []byte) {
	t.Helper()
	a, b := []byte("fixture source\n"), []byte("fixture executable\n")
	file := releaseTreeEntry{"opaque-sensitive-file", "100644", "blob", gitBlobHash(a)}
	exe := releaseTreeEntry{"dir/tool", "100755", "blob", gitBlobHash(b)}
	object := func(mode, name, sha string) []byte {
		id, _ := hex.DecodeString(sha)
		return append([]byte(fmt.Sprintf("%s %s%c", mode, name, 0)), id...)
	}
	dir := releaseTreeEntry{"dir", "040000", "tree", gitObjectID("tree", object("100755", "tool", exe.SHA))}
	root := append(object("40000", "dir", dir.SHA), object("100644", file.Path, file.SHA)...)
	tree := releaseTreeReply{SHA: gitObjectID("tree", root), Entries: []releaseTreeEntry{file, dir, exe}}
	archive := contextFixtureArchive(t, []tar.Header{
		{Name: "opaque-root/", Typeflag: tar.TypeDir, Mode: 0755},
		{Name: "opaque-root/" + file.Path, Typeflag: tar.TypeReg, Mode: 0664, Size: int64(len(a))},
		{Name: "opaque-root/dir/", Typeflag: tar.TypeDir, Mode: 0775},
		{Name: "opaque-root/" + exe.Path, Typeflag: tar.TypeReg, Mode: 0775, Size: int64(len(b))},
	}, [][]byte{nil, a, nil, b})
	return tree, archive
}

func contextFixtureArchive(t *testing.T, headers []tar.Header, bodies [][]byte) []byte {
	t.Helper()
	var raw bytes.Buffer
	gz := gzip.NewWriter(&raw)
	tw := tar.NewWriter(gz)
	for i := range headers {
		if err := tw.WriteHeader(&headers[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(bodies[i]); err != nil {
			t.Fatal(err)
		}
	}
	if tw.Close() != nil || gz.Close() != nil {
		t.Fatal("fixture archive failed")
	}
	return raw.Bytes()
}

func TestSealedContextUsesVerifiedSnapshotWithoutHostPaths(t *testing.T) {
	tree, original := contextFixture(t)
	c, err := sealGitContext(tree, original, tree.SHA)
	if err != nil {
		t.Fatal(err)
	}
	original[0] ^= 1
	tree.Entries[0].SHA = strings.Repeat("0", 40)
	r, err := c.verifiedArchive()
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(r)
	if err != nil || digest(b) != c.archiveSHA256 || len(c.files) != 2 {
		t.Fatal("snapshot identity lost")
	}
	tr := tar.NewReader(bytes.NewReader(b))
	count := 0
	for {
		h, e := tr.Next()
		if e == io.EOF {
			break
		}
		if e != nil || (h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir) || strings.HasPrefix(h.Name, "opaque-root") || h.Uid != 1001 || h.Gid != 1001 {
			t.Fatal("canonical archive header rejected")
		}
		if h.Typeflag == tar.TypeDir {
			if !c.directories[strings.TrimSuffix(h.Name, "/")] {
				t.Fatal("unexpected archive directory")
			}
			continue
		}
		actual, e := io.ReadAll(tr)
		if e != nil || gitBlobHash(actual) != c.files[h.Name].blob {
			t.Fatal("archive consumption differs from verified blob")
		}
		count++
	}
	if count != 2 {
		t.Fatal("tracked file omitted")
	}
	b[0] ^= 1 // Caller does not hold the internal archive.
	if _, err := c.verifiedArchive(); err != nil {
		t.Fatal("consumer modified sealed input")
	}
	encoded, err := json.Marshal(c)
	if err != nil || string(encoded) != "{}" {
		t.Fatal("context names or source serialized")
	}
	c.archive[0] ^= 1
	if _, err := c.verifiedArchive(); err == nil || err.Error() != "sealed release archive changed" {
		t.Fatal("damaged archive consumed")
	}
}

func TestReleaseTreeReconstructsObjectsInsteadOfTrustingReply(t *testing.T) {
	tree, raw := contextFixture(t)
	for _, change := range []func(*releaseTreeReply){
		func(v *releaseTreeReply) { v.Truncated = true },
		func(v *releaseTreeReply) { v.SHA = strings.Repeat("a", 40) },
		func(v *releaseTreeReply) { v.Entries[0].SHA = strings.Repeat("b", 40) },
		func(v *releaseTreeReply) { v.Entries[2].SHA = strings.Repeat("c", 40) },
		func(v *releaseTreeReply) { v.Entries = append(v.Entries, v.Entries[0]) },
		func(v *releaseTreeReply) { v.Entries[0].Path = "../opaque-sensitive-path" },
		func(v *releaseTreeReply) { v.Entries[0].Mode = "120000" },
		func(v *releaseTreeReply) { v.Entries[0].Mode, v.Entries[0].Type = "160000", "commit" },
		func(v *releaseTreeReply) { v.Entries[1].Path = "other" },
	} {
		bad := tree
		bad.Entries = append([]releaseTreeEntry(nil), tree.Entries...)
		change(&bad)
		if _, err := sealGitContext(bad, raw, tree.SHA); err == nil || strings.Contains(err.Error(), "opaque-sensitive") {
			t.Fatal("untrusted tree accepted or name leaked")
		}
	}
	if _, err := sealReleaseContext(tree, raw); err == nil || err.Error() != "release tree envelope rejected" {
		t.Fatal("production wrapper accepted a fixture tree")
	}
}

func TestReleaseArchiveRejectsUntrackedChangedAndMissingFiles(t *testing.T) {
	tree, _ := contextFixture(t)
	a := []byte("fixture source\n")
	b := []byte("fixture executable\n")
	base := []tar.Header{
		{Name: "opaque-root/opaque-sensitive-file", Typeflag: tar.TypeReg, Mode: 0644, Size: int64(len(a))},
		{Name: "opaque-root/dir/tool", Typeflag: tar.TypeReg, Mode: 0755, Size: int64(len(b))},
	}
	tests := []struct {
		name, want string
		change     func(*[]tar.Header, *[][]byte)
	}{
		{"changed", "release context file bytes mismatch", func(_ *[]tar.Header, v *[][]byte) { (*v)[0] = bytes.Repeat([]byte{'x'}, len(a)) }},
		{"missing", "release context tracked file missing", func(h *[]tar.Header, b *[][]byte) { *h = (*h)[:1]; *b = (*b)[:1] }},
		{"extra", "untracked release context entry", func(h *[]tar.Header, _ *[][]byte) { (*h)[0].Name = "opaque-root/other" }},
		{"duplicate", "release context root or duplicate rejected", func(h *[]tar.Header, b *[][]byte) { *h = append(*h, (*h)[0]); *b = append(*b, (*b)[0]) }},
		{"second-root", "release context root or duplicate rejected", func(h *[]tar.Header, _ *[][]byte) { (*h)[1].Name = "other-root/dir/tool" }},
		{"mode", "release context file mode mismatch", func(h *[]tar.Header, _ *[][]byte) { (*h)[1].Mode = 0644 }},
		{"special-bits", "release context file mode mismatch", func(h *[]tar.Header, _ *[][]byte) { (*h)[0].Mode = 04644 }},
		{"git", "release context path or mode rejected", func(h *[]tar.Header, _ *[][]byte) { (*h)[0].Name = "opaque-root/.git/config" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, data := append([]tar.Header(nil), base...), [][]byte{a, b}
			tc.change(&h, &data)
			raw := contextFixtureArchive(t, h, data)
			if _, err := sealGitContext(tree, raw, tree.SHA); err == nil || err.Error() != tc.want {
				t.Fatalf("wrong rejection: %v", err)
			}
		})
	}
}

func TestReleaseContextRejectsGzipDamageAndMultipleStreams(t *testing.T) {
	tree, raw := contextFixture(t)
	damaged := bytes.Clone(raw)
	damaged[len(damaged)-8] ^= 1
	for _, bad := range [][]byte{damaged, append(bytes.Clone(raw), raw...), raw[:len(raw)-4]} {
		if _, err := sealGitContext(tree, bad, tree.SHA); err == nil {
			t.Fatal("unverified gzip trailer accepted")
		}
	}
	c, err := sealGitContext(tree, raw, tree.SHA)
	if err != nil {
		t.Fatal(err)
	}
	f := c.files["opaque-sensitive-file"]
	f.data[0] ^= 1
	if _, err := c.makeArchive(); err == nil || err.Error() != "sealed release file changed" {
		t.Fatal("changed source snapshot used for a new archive")
	}
}

func TestReleaseTreeUsesGitDirectoryOrdering(t *testing.T) {
	object := func(mode, name, sha string) []byte {
		id, _ := hex.DecodeString(sha)
		return append([]byte(fmt.Sprintf("%s %s%c", mode, name, 0)), id...)
	}
	blob := gitBlobHash([]byte("fixture"))
	child := gitObjectID("tree", object("100644", "c", blob))
	// Git compares directory a as a/. Thus a-x and a.b precede a/.
	raw := append(object("100644", "a-x", blob), object("100644", "a.b", blob)...)
	raw = append(raw, object("40000", "a", child)...)
	tree := releaseTreeReply{SHA: gitObjectID("tree", raw), Entries: []releaseTreeEntry{
		{"a", "040000", "tree", child}, {"a/c", "100644", "blob", blob},
		{"a.b", "100644", "blob", blob}, {"a-x", "100644", "blob", blob},
	}}
	if _, err := verifyReleaseTree(tree, tree.SHA); err != nil {
		t.Fatal("Git directory ordering lost")
	}
	if releaseSourcePath("dir/.GIT/config") {
		t.Fatal("case-folded Git administration path accepted")
	}
}

func TestReleaseContextRejectsLinksAndDirectoryFileMismatch(t *testing.T) {
	tree, _ := contextFixture(t)
	for _, flag := range []byte{tar.TypeSymlink, tar.TypeLink, tar.TypeDir} {
		h := tar.Header{Name: "root/opaque-sensitive-file", Typeflag: flag, Mode: 0644}
		if flag != tar.TypeDir {
			h.Linkname = "opaque-sensitive-target"
		}
		raw := contextFixtureArchive(t, []tar.Header{h}, [][]byte{nil})
		_, err := sealGitContext(tree, raw, tree.SHA)
		want := "release context path or mode rejected"
		if flag == tar.TypeDir {
			want = "release context directory mismatch"
		}
		if err == nil || err.Error() != want {
			t.Fatalf("wrong special-entry rejection: %v", err)
		}
	}
}
