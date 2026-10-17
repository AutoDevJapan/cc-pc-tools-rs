package main

import (
	"context"
	"errors"
	"fmt"
)

// This is one audited merge, not permission to follow the default branch.
const mergedSHA = "41090d19329b81dd1bd076f3336d723823be4b82"
const mergedTree = "007b84082008ea9effd5052f63856273aabbfb17"
const mergedPRHead = "20c441df545a88f4a839d7e906b5aeb4f1708ae3"
const mergedBase = "f3d946cb0f7e33b8cb5cbc0858091e583ca61350"
const mergedCodeQLRun int64 = 38941323954
const mergedCodeQLWorkflow int64 = 331939928

func mergedCandidate() candidate { return candidate{1285649124, 1002, mergedSHA} }

// Only execution code calls this. Existing open-PR, bootstrap and route modes
// retain verify and can never use a merged receipt to mutate their scope.
func verifyExecutionSource(ctx context.Context, c candidate, read api) (repository, error) {
	if c == mergedCandidate() {
		return verifyMerged(ctx, c, read)
	}
	return verify(ctx, c, read)
}

func verifyMerged(ctx context.Context, c candidate, read api) (repository, error) {
	if c != mergedCandidate() {
		return repository{}, errors.New("unknown merged source")
	}
	var r struct {
		repository
		DefaultBranch string `json:"default_branch"`
	}
	if e := read(ctx, fmt.Sprintf("repositories/%d", c.ID), &r); e != nil {
		return repository{}, mergedReadFailure("repository metadata", e)
	}
	if r.ID != c.ID || r.Owner.Login != "AutoDevJapan" || !r.Private || r.Fork || !r.Permissions.Admin || r.DefaultBranch != "main" {
		return repository{}, errors.New("merged repository identity rejected")
	}
	// Keep the original open-PR verifier untouched. This receipt has its own
	// stricter closed-state and fixed-history checks.
	var p struct {
		pull
		Merged   bool
		MergeSHA string `json:"merge_commit_sha"`
		Base     struct {
			Ref, SHA string
			Repo     repository
		}
	}
	if e := read(ctx, fmt.Sprintf("repos/%s/pulls/%d", r.FullName, c.PR), &p); e != nil {
		return repository{}, mergedReadFailure("PR receipt", e)
	}
	if !sameMergedRepo(r.repository, r.repository) || p.State != "closed" || !p.Merged || p.MergeSHA != mergedSHA || p.Head.SHA != mergedPRHead || p.Head.Ref == "" || p.Base.Ref != "main" || p.Base.SHA != mergedBase || !sameMergedRepo(p.Head.Repo, r.repository) || !sameMergedRepo(p.Base.Repo, r.repository) {
		return repository{}, errors.New("exact merged PR receipt rejected")
	}
	var ref struct{ Object struct{ SHA string } }
	if e := read(ctx, "repos/"+r.FullName+"/git/ref/heads/main", &ref); e != nil {
		return repository{}, mergedReadFailure("current main ref", e)
	}
	if ref.Object.SHA != mergedSHA {
		return repository{}, errors.New("current main differs from audited merge")
	}
	// A merged PR's temporary branch may be removed by normal repository
	// cleanup. It is not the execution source. Require the original PR head's
	// immutable commit and tree below, and bind that head to the exact ordered
	// merge parents. Never recover by another ref or by ignoring a read error.
	for _, sha := range []string{mergedSHA, mergedPRHead} {
		var commit struct {
			SHA     string
			Tree    struct{ SHA string }
			Parents []struct{ SHA string }
		}
		stage := "merge commit"
		if sha == mergedPRHead {
			stage = "original PR head commit"
		}
		if e := read(ctx, "repos/"+r.FullName+"/git/commits/"+sha, &commit); e != nil {
			return repository{}, mergedReadFailure(stage, e)
		}
		if commit.SHA != sha || commit.Tree.SHA != mergedTree {
			return repository{}, errors.New("merged commit or PR tree rejected")
		}
		if sha == mergedSHA && (len(commit.Parents) != 2 || commit.Parents[0].SHA != mergedBase || commit.Parents[1].SHA != mergedPRHead) {
			return repository{}, errors.New("ordered merge parents rejected")
		}
	}
	return r.repository, nil
}

func mergedReadFailure(stage string, err error) error {
	// Even injected API implementations cannot put raw response text in a
	// receipt. Only the transport's closed error vocabulary is retained.
	var safe *gitHubReadError
	if !errors.As(err, &safe) {
		safe = &gitHubReadError{class: "unclassified"}
	}
	return fmt.Errorf("merged source %s: %w", stage, safe)
}

func sameMergedRepo(got, want repository) bool {
	return got.ID == want.ID && got.FullName == want.FullName && !got.Fork && got.Private && got.Owner.Login == "AutoDevJapan" && len(got.FullName) > len("AutoDevJapan/") && validMergedName(got.FullName)
}

func validMergedName(s string) bool {
	// Identifiers stay in RAM and cannot introduce extra URL path segments.
	const prefix = "AutoDevJapan/"
	if len(s) <= len(prefix) || s[:len(prefix)] != prefix {
		return false
	}
	for _, ch := range s[len(prefix):] {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_' || ch == '.') {
			return false
		}
	}
	return s[len(prefix):] != "." && s[len(prefix):] != ".."
}

func validMergedPlan(p codeQLPlan) bool {
	return p.Kind == "merged-codeql" && p.RepoID == 1285649124 && p.PR == 1002 && p.Candidate == mergedSHA && p.SHA == mergedSHA && p.Tree == mergedTree && p.RunID == mergedCodeQLRun && p.Attempt == 1 && p.Role == "merged-codeql" && p.Label == codeQLLabel(p.RepoID) && p.ContextSHA == ""
}

func planMergedCodeQL(ctx context.Context, c candidate, runID int64, read api) (codeQLPlan, error) {
	if c != mergedCandidate() || runID != mergedCodeQLRun {
		return codeQLPlan{}, errors.New("unknown post-merge run")
	}
	r, e := verifyMerged(ctx, c, read)
	if e != nil {
		return codeQLPlan{}, e
	}
	var setup codeQLSetup
	if e = read(ctx, "repos/"+r.FullName+"/code-scanning/default-setup", &setup); e != nil {
		return codeQLPlan{}, e
	}
	want := codeQLSetup{State: "configured", Languages: []string{"actions", "javascript-typescript", "python", "rust"}, QuerySuite: "default", ThreatModel: "remote", RunnerType: "labeled", RunnerLabel: codeQLLabel(c.ID)}
	if !sameCodeQLSettings(setup, want) || setup.RunnerType != want.RunnerType || setup.RunnerLabel != want.RunnerLabel {
		return codeQLPlan{}, errors.New("original merged CodeQL settings changed")
	}
	var run struct {
		codeQLRun
		WorkflowID int64 `json:"workflow_id"`
	}
	if e = read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d", r.FullName, runID), &run); e != nil {
		return codeQLPlan{}, e
	}
	if run.ID != mergedCodeQLRun || run.WorkflowID != mergedCodeQLWorkflow || run.RunAttempt != 1 || run.HeadSHA != mergedSHA || run.HeadBranch != "main" || run.Path != "dynamic/github-code-scanning/codeql" || run.Event != "dynamic" || !sameMergedRepo(run.Repository, r) || !sameMergedRepo(run.HeadRepo, r) || (run.Status != "queued" && run.Status != "in_progress" && run.Status != "waiting") {
		return codeQLPlan{}, errors.New("exact merged CodeQL run rejected")
	}
	// Require the original attempt's four exact matrix jobs, including finished
	// jobs on later sessions. No unknown job may share this finite queue.
	var jobs struct {
		Total int `json:"total_count"`
		Jobs  []struct {
			ID     int64
			RunID  int64 `json:"run_id"`
			Labels []string
		}
	}
	if e = read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/attempts/1/jobs?per_page=100", r.FullName, runID), &jobs); e != nil {
		return codeQLPlan{}, e
	}
	expected := map[int64]bool{117109408190: false, 117109408508: false, 117109408625: false, 117109408843: false}
	if jobs.Total != 4 || len(jobs.Jobs) != 4 {
		return codeQLPlan{}, errors.New("original four-job matrix required")
	}
	for _, j := range jobs.Jobs {
		seen, ok := expected[j.ID]
		if !ok || seen || j.RunID != runID || len(j.Labels) != 1 || j.Labels[0] != want.RunnerLabel {
			return codeQLPlan{}, errors.New("merged matrix job or label rejected")
		}
		expected[j.ID] = true
	}
	return codeQLPlan{RepoID: c.ID, PR: c.PR, Candidate: mergedSHA, RunID: runID, Attempt: 1, SHA: mergedSHA, Tree: mergedTree, Role: "merged-codeql", Kind: "merged-codeql", Label: want.RunnerLabel, Repo: r.FullName}, nil
}
