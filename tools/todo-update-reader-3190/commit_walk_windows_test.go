package main

import (
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

// Commit times before and after the fixture's commit, which Git makes at the
// current time.
const (
	earlierDates = "author A <a@example.invalid> 1700000000 +0000\ncommitter C <c@example.invalid> 1700000000 +0000\n"
	laterDates   = "author A <a@example.invalid> 4000000000 +0000\ncommitter C <c@example.invalid> 4000000000 +0000\n"
)

// historyCase stores commits with put and returns the one that origin/main
// is to name, and Git's exit status for git merge-base --is-ancestor from the
// fixed commit to it: 0 if Git finds the fixed commit, 128 if it refuses the
// history.
type historyCase struct {
	status int
	main   func(f *fixture, put func(body string) string) string
}

// checkHistories points origin/main at each case's commit and checks that
// Git exits with the case's status there, and that the reader qualifies the
// fixed update where Git finds the fixed commit and refuses the source where
// Git refuses the history.
func checkHistories(t *testing.T, cases map[string]historyCase) {
	t.Helper()
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			objects := filepath.Join(f.common, "objects")
			main := tc.main(f, func(body string) string { return writeLooseObject(t, objects, "commit", body) })
			writeFile(t, filepath.Join(f.common, "refs", "remotes", "origin", "main"), main+"\n")
			out, err := testGit(t, f.root, "merge-base", "--is-ancestor", f.p.head, main)
			status := 0
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				status = exit.ExitCode()
			} else if err != nil {
				t.Fatalf("git merge-base: %v\n%s", err, out)
			}
			if status != tc.status {
				t.Fatalf("git merge-base --is-ancestor %s %s exited %d, want %d\n%s", f.p.head, main, status, tc.status, out)
			}
			if status != 0 {
				expectReason(t, f, "source_git")
				return
			}
			if got, reason := f.verify(); !got.Qualified || reason != "" {
				t.Fatalf("verify = qualified %v reason %q where Git finds the fixed commit", got.Qualified, reason)
			}
		})
	}
}

// Git keeps one type for each object name that it looks up or reads, and
// refuses to use that object as another type (object.c, object_as_type). A
// commit's tree line looks its name up as a tree and each parent line looks
// its name up as a commit, so merge-base refuses a history in which one name
// is both. The reader refuses the source there too.
func TestRefusesNamesOfTwoTypes(t *testing.T) {
	checkHistories(t, map[string]historyCase{
		"tree line names the fixed commit, also the parent": {128, func(f *fixture, put func(string) string) string {
			return put("tree " + f.p.head + "\nparent " + f.p.head + "\n" + laterDates + "\nmain\n")
		}},
		"tree line names the commit of the parent line": {128, func(f *fixture, put func(string) string) string {
			side := put("tree " + f.p.tree + "\nparent " + f.p.head + "\n" + earlierDates + "\nside\n")
			return put("tree " + side + "\nparent " + side + "\n" + laterDates + "\nmain\n")
		}},
		"parent line names the fixed tree": {128, func(f *fixture, put func(string) string) string {
			return put("tree " + f.p.tree + "\nparent " + f.p.head + "\nparent " + f.p.tree + "\n" + laterDates + "\nmain\n")
		}},
	})
}

// Git's merge-base walk does not stop at the fixed commit. Before it answers,
// it reads every commit that main reaches and the fixed commit does not, and
// the parents of each (commit-reach.c, paint_down_to_common), so a commit
// there that Git cannot read, or that gives a name a second type, refuses the
// history. The reader refuses the source there too. Each side branch below is
// older than the fixed commit, and the merge names it before and after the
// fixed commit, so a walk that stopped at the fixed commit would not read it
// whether it took the newest commit or either parent first.
func TestReadsEveryCommitGitReads(t *testing.T) {
	sides := map[string]historyCase{
		"side branch of whole commits": {0, func(f *fixture, put func(string) string) string {
			root := put("tree " + f.p.tree + "\n" + earlierDates + "\nroot\n")
			return put("tree " + f.p.tree + "\nparent " + root + "\n" + earlierDates + "\nside\n")
		}},
		"side branch reaches a missing commit": {128, func(f *fixture, put func(string) string) string {
			return put("tree " + f.p.tree + "\nparent 1111111111111111111111111111111111111111\n" + earlierDates + "\nside\n")
		}},
		"side branch reaches a tree line that names the fixed commit": {128, func(f *fixture, put func(string) string) string {
			root := put("tree " + f.p.head + "\n" + earlierDates + "\nroot\n")
			return put("tree " + f.p.tree + "\nparent " + root + "\n" + earlierDates + "\nside\n")
		}},
	}
	orders := map[string]func(head, side string) string{
		"fixed commit first": func(head, side string) string { return "parent " + head + "\nparent " + side + "\n" },
		"side branch first":  func(head, side string) string { return "parent " + side + "\nparent " + head + "\n" },
	}
	cases := map[string]historyCase{}
	for name, side := range sides {
		for order, parents := range orders {
			cases[name+", "+order] = historyCase{side.status, func(f *fixture, put func(string) string) string {
				return put("tree " + f.p.tree + "\n" + parents(f.p.head, side.main(f, put)) + laterDates + "\nmerge\n")
			}}
		}
	}
	checkHistories(t, cases)
}
