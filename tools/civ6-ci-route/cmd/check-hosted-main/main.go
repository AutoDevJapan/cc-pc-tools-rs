// check-hosted-main verifies the fixed PR149 source merge without publishing,
// executing a workflow, issuing tokens, or changing a local runner ledger.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"cc/civ6-ci-contract/hostedsource"
)

type boundedOutput struct{ data bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.data.Len()+len(p) > 16<<20 {
		return 0, errors.New("source observation exceeds bound")
	}
	return b.data.Write(p)
}

func command(ctx context.Context, dir, exe string, args ...string) ([]byte, error) {
	c := exec.CommandContext(ctx, exe, args...)
	c.Dir = dir
	hide(c)
	c.WaitDelay = 2 * time.Second
	var out boundedOutput
	c.Stdout = &out
	e := c.Run()
	if e != nil {
		return nil, fmt.Errorf("%s read failed: %w", filepath.Base(exe), e)
	}
	return out.data.Bytes(), nil
}
func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	return command(ctx, root, "git", args...)
}
func commit(ctx context.Context, root, ref string) (hostedsource.Commit, error) {
	return readCommit(ctx, root, ref, git)
}

type sourceRead func(context.Context, string, ...string) ([]byte, error)

func readCommit(ctx context.Context, root, ref string, read sourceRead) (hostedsource.Commit, error) {
	b, e := read(ctx, root, "show", "-s", "--format=%H%n%T%n%P", ref)
	if e != nil {
		return hostedsource.Commit{}, e
	}
	rows := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(rows) < 2 || len(rows) > 3 {
		return hostedsource.Commit{}, errors.New("commit metadata differs")
	}
	c := hostedsource.Commit{SHA: strings.TrimSpace(rows[0]), Tree: strings.TrimSpace(rows[1])}
	if len(rows) == 3 {
		c.Parents = strings.Fields(rows[2])
	}
	return c, nil
}
func tree(ctx context.Context, root, ref string) ([]hostedsource.Entry, error) {
	return readTree(ctx, root, ref, git)
}

func readTree(ctx context.Context, root, ref string, read sourceRead) ([]hostedsource.Entry, error) {
	b, e := read(ctx, root, "ls-tree", "-r", "-t", "-z", ref)
	if e != nil {
		return nil, e
	}
	rows := strings.Split(string(b), "\x00")
	out := make([]hostedsource.Entry, 0, len(rows))
	for _, row := range rows {
		if row == "" {
			continue
		}
		meta, p, ok := strings.Cut(row, "\t")
		fields := strings.Fields(meta)
		if !ok || len(fields) != 3 {
			return nil, errors.New("tree row invalid")
		}
		out = append(out, hostedsource.Entry{Path: p, Mode: fields[0], Type: fields[1], SHA: fields[2]})
	}
	return out, nil
}
func observe(ctx context.Context, root string) (hostedsource.Snapshot, error) {
	return observeWith(ctx, root, git)
}

func observeWith(ctx context.Context, root string, read sourceRead) (hostedsource.Snapshot, error) {
	var s hostedsource.Snapshot
	var e error
	if s.Old, e = readCommit(ctx, root, hostedsource.OldHead, read); e != nil {
		return s, e
	}
	if s.Main, e = readCommit(ctx, root, hostedsource.Main, read); e != nil {
		return s, e
	}
	if s.InitialHosted, e = readCommit(ctx, root, hostedsource.InitialHostedHead, read); e != nil {
		return s, e
	}
	if s.Candidate, e = readCommit(ctx, root, "HEAD", read); e != nil {
		return s, e
	}
	if s.MainTree, e = readTree(ctx, root, hostedsource.Main, read); e != nil {
		return s, e
	}
	if s.CandidateTree, e = readTree(ctx, root, s.Candidate.SHA, read); e != nil {
		return s, e
	}
	s.MainFiles = map[string][]byte{}
	s.CandidateFiles = map[string][]byte{}
	for _, w := range hostedsource.Workflows() {
		if s.MainFiles[w.Path], e = read(ctx, root, "show", hostedsource.Main+":"+w.Path); e != nil {
			return s, e
		}
		if s.CandidateFiles[w.Path], e = read(ctx, root, "show", s.Candidate.SHA+":"+w.Path); e != nil {
			return s, e
		}
	}
	s.CandidateFiles[hostedsource.AssetTestPath], e = read(ctx, root, "show", s.Candidate.SHA+":"+hostedsource.AssetTestPath)
	return s, e
}
func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: check-hosted-main <local-reviewed-checkout>")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, e := observe(ctx, os.Args[1])
	if e == nil {
		e = hostedsource.Verify(s)
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	result := struct {
		Generation, Head, Tree                                                           string
		LocalSourceVerified, RemoteFreshnessVerified, RunnerAdmission, OfficialCISuccess bool
	}{
		hostedsource.Generation, s.Candidate.SHA, s.Candidate.Tree, true, false, false, false}
	if json.NewEncoder(os.Stdout).Encode(result) != nil {
		os.Exit(1)
	}
}
