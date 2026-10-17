package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"reflect"
	"strings"
)

const pr148Repo = "AutoDevJapan/civ6-banseiki"
const pr148LinuxLabel = "cc-product-2736-1392152644-ubuntu-latest"
const pr148Workspace = "/work/civ6-banseiki/civ6-banseiki"

// An explicit NEW source pin is mandatory. Neither the original head nor a
// caller-selected latest ref is a runnable candidate. No mutation lives here.
type pr148LinuxPlan struct {
	Head, Tree, WorkflowSHA string
	RunID, JobID            int64
	Attempt                 int
}

type pr148TreeEntry struct{ Path, Mode, Type, SHA string }
type pr148APITree struct {
	SHA       string
	Truncated bool
	Entries   []pr148TreeEntry `json:"tree"`
}

func pr148SHA(s string) bool { return len(s) == 40 && strings.Trim(s, "0123456789abcdef") == "" }
func (p pr148LinuxPlan) valid() bool {
	return pr148SHA(p.Head) && p.Head != pr148Head && pr148SHA(p.Tree) && p.Tree != pr148Tree &&
		len(p.WorkflowSHA) == 64 && strings.Trim(p.WorkflowSHA, "0123456789abcdef") == "" && p.RunID > 0 && p.JobID > 0 && p.Attempt > 0
}

func pr148Contents(ctx context.Context, read api, path, sha string) ([]byte, error) {
	var v struct{ Type, Encoding, Content, SHA string }
	if e := read(ctx, "repos/"+pr148Repo+"/contents/"+path+"?ref="+sha, &v); e != nil {
		return nil, e
	}
	b, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(v.Content, "\n", ""))
	if e != nil || v.Type != "file" || v.Encoding != "base64" || len(b) > 1<<20 || gitObjectID("blob", b) != v.SHA {
		return nil, errors.New("workflow blob bytes rejected")
	}
	return b, nil
}

// Compare the entire recursive index, including modes, submodules and empty
// directories. Only three blobs and their ancestor tree IDs may differ.
func pr148TreeDelta(old, next pr148APITree, expected map[string]string) error {
	if old.Truncated || next.Truncated || old.SHA != pr148Tree || !pr148SHA(next.SHA) || len(old.Entries) != len(next.Entries) || len(expected) != 3 {
		return errors.New("complete fixed tree comparison unavailable")
	}
	prior := map[string]pr148TreeEntry{}
	for _, x := range old.Entries {
		if x.Path == "" || !pr148SHA(x.SHA) {
			return errors.New("invalid original tree entry")
		}
		if _, ok := prior[x.Path]; ok {
			return errors.New("duplicate original tree entry")
		}
		prior[x.Path] = x
	}
	changed := 0
	for _, y := range next.Entries {
		x, ok := prior[y.Path]
		if !ok || x.Mode != y.Mode || x.Type != y.Type || !pr148SHA(y.SHA) {
			return fmt.Errorf("unexpected tree entry or mode: %s", y.Path)
		}
		delete(prior, y.Path)
		if want, ok := expected[y.Path]; ok {
			if y.Type != "blob" || y.SHA != want || y.SHA == x.SHA {
				return fmt.Errorf("proposal bytes differ: %s", y.Path)
			}
			changed++
		} else if x.SHA != y.SHA && !(y.Type == "tree" && (y.Path == ".github" || y.Path == ".github/workflows")) {
			return fmt.Errorf("unexpected source modification: %s", y.Path)
		}
	}
	if len(prior) != 0 || changed != 3 {
		return errors.New("proposal tree coverage differs")
	}
	return nil
}

// All product reads are deferred to the parent's integration execution. Unit
// fixtures supply a credential-free API; this child does not fetch the product.
func verifyPR148LinuxSource(ctx context.Context, p pr148LinuxPlan, read api) error {
	if !p.valid() {
		return errors.New("explicit new source/run/job pin required")
	}
	r, e := verify(ctx, candidate{1392152644, 148, p.Head}, read)
	if e != nil {
		return e
	}
	if r.FullName != pr148Repo || r.Owner.Login != "AutoDevJapan" {
		return errors.New("PR148 repository rejected")
	}
	var pr struct {
		State string
		Draft bool
		Head  struct{ SHA, Ref string }
		Base  struct{ SHA, Ref string }
	}
	if e = read(ctx, "repos/"+pr148Repo+"/pulls/148", &pr); e != nil {
		return e
	}
	if pr.State != "open" || !pr.Draft || pr.Head.SHA != p.Head || pr.Head.Ref != pr148Branch || pr.Base.SHA != pr148Base || pr.Base.Ref != "main" {
		return errors.New("fixed PR148 scope changed")
	}
	var original, next originalCommit
	for sha, out := range map[string]*originalCommit{pr148Head: &original, p.Head: &next} {
		if e = read(ctx, "repos/"+pr148Repo+"/git/commits/"+sha, out); e != nil {
			return e
		}
	}
	if original.Tree.SHA != pr148Tree || next.Tree.SHA != p.Tree || len(next.Parents) != 1 || next.Parents[0].SHA != pr148Head {
		return errors.New("new commit must have only the fixed original parent")
	}
	if _, e = originalCommitBytes(next, p.Head); e != nil {
		return e
	}
	expected := map[string]string{}
	for _, w := range pr148Workflows {
		data, e := pr148Contents(ctx, read, w.Path, pr148Head)
		if e != nil {
			return e
		}
		want, e := proposePR148(data, w)
		if e != nil {
			return e
		}
		got, e := pr148Contents(ctx, read, w.Path, p.Head)
		if e != nil {
			return e
		}
		if !bytes.Equal(want, got) {
			return fmt.Errorf("workflow differs from reviewed proposal: %s", w.Path)
		}
		if w.Job == "test" && digest(got) != p.WorkflowSHA {
			return errors.New("Linux workflow digest differs from explicit pin")
		}
		expected[w.Path] = gitObjectID("blob", got)
	}
	var a, b pr148APITree
	if e = read(ctx, "repos/"+pr148Repo+"/git/trees/"+pr148Tree+"?recursive=1", &a); e != nil {
		return e
	}
	if e = read(ctx, "repos/"+pr148Repo+"/git/trees/"+p.Tree+"?recursive=1", &b); e != nil {
		return e
	}
	if b.SHA != p.Tree {
		return errors.New("explicit new tree pin changed")
	}
	return pr148TreeDelta(a, b, expected)
}

type pr148LinuxJob struct {
	ID                       int64
	RunID                    int64 `json:"run_id"`
	RunAttempt               int   `json:"run_attempt"`
	Name, Status, Conclusion string
	Labels                   []string
	RunnerName               string `json:"runner_name"`
}

func verifyPR148LinuxRun(ctx context.Context, p pr148LinuxPlan, read api, completed bool) error {
	if e := verifyPR148LinuxSource(ctx, p, read); e != nil {
		return e
	}
	var run codeQLRun
	if e := read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d", pr148Repo, p.RunID), &run); e != nil {
		return e
	}
	if run.ID != p.RunID || run.RunAttempt != p.Attempt || run.HeadSHA != p.Head || run.HeadBranch != pr148Branch || run.Event != "workflow_dispatch" || run.Path != pr148Workflows[0].Path || run.Repository.ID != 1392152644 || run.HeadRepo.ID != 1392152644 || run.Repository.FullName != pr148Repo || run.HeadRepo.FullName != pr148Repo || run.HeadRepo.Fork {
		return errors.New("fixed full Linux workflow dispatch rejected")
	}
	if run.Status != "queued" && run.Status != "in_progress" && run.Status != "waiting" && !(completed && run.Status == "completed") {
		return errors.New("Linux run state rejected")
	}
	var jobs struct {
		Total int `json:"total_count"`
		Jobs  []pr148LinuxJob
	}
	if e := read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100", pr148Repo, p.RunID, p.Attempt), &jobs); e != nil {
		return e
	}
	if jobs.Total != 2 || len(jobs.Jobs) != 2 {
		return errors.New("full original Rust OS matrix required")
	}
	seen := map[string]bool{}
	for _, j := range jobs.Jobs {
		if j.RunID != p.RunID || j.RunAttempt != p.Attempt || seen[j.Name] {
			return errors.New("job run/attempt coverage rejected")
		}
		seen[j.Name] = true
		os := "windows-latest"
		if j.Name == "test (ubuntu-latest)" {
			os = "ubuntu-latest"
			if j.ID != p.JobID || (!completed && j.Status == "completed") {
				return errors.New("fixed Linux job identity/state rejected")
			}
		} else if j.Name != "test (windows-latest)" {
			return errors.New("unexpected Rust matrix job")
		}
		if !reflect.DeepEqual(j.Labels, []string{"cc-product-2736-1392152644-" + os}) {
			return errors.New("original OS route label rejected")
		}
	}
	return nil
}
