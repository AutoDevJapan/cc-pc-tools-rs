package main

import (
	"bytes"
	"compress/zlib"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// writeLooseObject stores body as a loose object of kind, compressed as Git
// stores one, and returns the object's name.
func writeLooseObject(t *testing.T, objects, kind, body string) string {
	t.Helper()
	id := objectName(kind, []byte(body))
	var buf bytes.Buffer
	z := zlib.NewWriter(&buf)
	if _, err := z.Write([]byte(kind + " " + strconv.Itoa(len(body)) + "\x00" + body)); err != nil {
		t.Fatal(err)
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(objects, id[:2], id[2:]), buf.String())
	return id
}

// gitCommitView returns the tree and the parents that Git reads from the
// commit id, or false if Git refuses to read the commit.
func gitCommitView(t *testing.T, dir, id string) (string, []string, bool) {
	t.Helper()
	out, err := testGit(t, dir, "log", "--no-walk", "--format=%T %P", id)
	if err != nil {
		return "", nil, false
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		t.Fatalf("git log printed %q", out)
	}
	return fields[0], fields[1:], true
}

// Git's parse_commit_buffer reads a "tree " line that more bytes follow, then
// takes a line that starts with "parent " as a parent only while more than
// such a line's length follows it, and refuses the commit if that line is not
// "parent <id>\n" with more bytes after it. The reader refuses each commit
// that Git refuses, and also each commit whose "parent " line Git would not
// read, so every commit it accepts has the tree and parents Git reads.
func TestParseCommitAgreesWithGit(t *testing.T) {
	const (
		tree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
		p1   = "1111111111111111111111111111111111111111"
		p2   = "2222222222222222222222222222222222222222"
		who  = "author A <a@example.invalid> 1700000000 +0000\ncommitter C <c@example.invalid> 1700000001 +0000\n"
	)
	refused := []string(nil)
	cases := []struct {
		name        string
		body        string
		git, reader []string // the parents read; nil if the commit is refused
	}{
		{"root commit", "tree " + tree + "\n" + who + "\nmessage\n", []string{}, []string{}},
		{"two parents", "tree " + tree + "\nparent " + p1 + "\nparent " + p2 + "\n" + who + "\nmessage\n", []string{p1, p2}, []string{p1, p2}},
		{"parent line in the message", "tree " + tree + "\n\nparent " + p1 + "\n", []string{}, []string{}},
		{"tree line only", "tree " + tree + "\n", refused, refused},
		{"tree line without its newline", "tree " + tree, refused, refused},
		{"parent line without its newline", "tree " + tree + "\nparent " + p1, []string{}, refused},
		{"commit ends with a parent line", "tree " + tree + "\nparent " + p1 + "\n", refused, refused},
		{"second parent line without its newline", "tree " + tree + "\nparent " + p1 + "\nparent " + p2, []string{p1}, refused},
		{"short parent", "tree " + tree + "\nparent 1111\n" + who, refused, refused},
		{"long parent", "tree " + tree + "\nparent " + p1 + "1\n" + who, refused, refused},
		{"upper-case tree", "tree " + strings.ToUpper(tree) + "\n" + who, []string{}, refused},
	}
	dir := filepath.Join(testLongPath(t, t.TempDir()), "repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := testGit(t, dir, "init", "-q"); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := writeLooseObject(t, filepath.Join(dir, ".git", "objects"), "commit", tc.body)
			gitTree, gitParents, gitRead := gitCommitView(t, dir, id)
			if gitRead != (tc.git != nil) || gitRead && (gitTree != tree || !slices.Equal(gitParents, tc.git)) {
				t.Fatalf("Git read tree %q parents %q (%v), want parents %q", gitTree, gitParents, gitRead, tc.git)
			}
			gotTree, gotParents, ok := parseCommit([]byte(tc.body))
			if ok != (tc.reader != nil) || ok && (gotTree != gitTree || !slices.Equal(gotParents, tc.reader)) {
				t.Fatalf("parseCommit read tree %q parents %q (%v), want parents %q", gotTree, gotParents, ok, tc.reader)
			}
		})
	}
}

// A commit that ends in a "parent " line without its newline has no parent to
// Git. Made the tip of main, it must not let the reader find the fixed commit
// behind it where Git finds no ancestry.
func TestRefusesParentsGitDoesNotRead(t *testing.T) {
	f := newFixture(t, nil)
	id := writeLooseObject(t, filepath.Join(f.common, "objects"), "commit", "tree "+f.p.tree+"\nparent "+f.p.head)
	writeFile(t, filepath.Join(f.common, "refs", "remotes", "origin", "main"), id+"\n")
	_, err := testGit(t, f.root, "merge-base", "--is-ancestor", f.p.head, id)
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("git merge-base --is-ancestor %s %s: %v, want exit status 1", f.p.head, id, err)
	}
	expectReason(t, f, "source_git")
}
