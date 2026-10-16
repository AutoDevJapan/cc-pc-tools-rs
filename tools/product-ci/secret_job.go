package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

const secretBinarySHA = "09d435057df51b800201bc3bbe0820554b1cac3cd98162e9e02b20c8b441b5bd"
const secretWorkflowSHA = "0f9b993f4d00de86a94ad10432123f473daae1c5dfc9d3412149524f5739e273"
const secretWorkflowPathSHA = "d30b4e6a2bd3c346243a103c773373348c1bf4a80af7bf92f854306c597fca6b"

func secretLabel(id int64) string { return fmt.Sprintf("cc-product-2648-%d-secret", id) }
func productPlan(ctx context.Context, c candidate, runID int64, kind string, read api) (codeQLPlan, error) {
	if kind == "" {
		return planCodeQL(ctx, c, runID, read)
	}
	if kind != "secret" || c.ID != 1285649124 {
		return codeQLPlan{}, errors.New("unsupported product workflow")
	}
	return planSecretJob(ctx, c, runID, read)
}
func planSecretJob(ctx context.Context, c candidate, runID int64, read api) (codeQLPlan, error) {
	return planSecretJobWithPolicy(ctx, c, runID, read, secretWorkflowPathSHA, secretWorkflowSHA)
}

func planSecretJobWithPolicy(ctx context.Context, c candidate, runID int64, read api, pathDigest, workflowDigest string) (codeQLPlan, error) {
	r, e := verify(ctx, c, read)
	if e != nil {
		return codeQLPlan{}, e
	}
	var run struct {
		codeQLRun
		PullRequests []struct{ Number int } `json:"pull_requests"`
	}
	if e = read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d", r.FullName, runID), &run); e != nil {
		return codeQLPlan{}, e
	}
	if run.ID != runID || run.RunAttempt < 1 || run.Repository.ID != c.ID || run.HeadRepo.ID != c.ID || run.HeadRepo.Fork || run.Repository.FullName != r.FullName || run.HeadRepo.FullName != r.FullName || run.HeadSHA != c.SHA || run.Event != "pull_request" || digest([]byte(run.Path)) != pathDigest || (run.Status != "queued" && run.Status != "in_progress" && run.Status != "pending" && run.Status != "waiting") || len(run.PullRequests) != 1 || run.PullRequests[0].Number != c.PR {
		return codeQLPlan{}, errors.New("fixed secret workflow run rejected")
	}
	var source struct {
		Content, Encoding string
		Type              string
	}
	if e = read(ctx, "repos/"+r.FullName+"/contents/"+run.Path+"?ref="+c.SHA, &source); e != nil {
		return codeQLPlan{}, e
	}
	data, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(source.Content, "\n", ""))
	if e != nil || source.Encoding != "base64" || source.Type != "file" || digest(data) != workflowDigest {
		return codeQLPlan{}, errors.New("reviewed native scanner workflow differs")
	}
	var pr struct {
		MergeSHA string `json:"merge_commit_sha"`
	}
	if e = read(ctx, fmt.Sprintf("repos/%s/pulls/%d", r.FullName, c.PR), &pr); e != nil {
		return codeQLPlan{}, e
	}
	if len(pr.MergeSHA) != 40 || strings.Trim(pr.MergeSHA, "0123456789abcdef") != "" {
		return codeQLPlan{}, errors.New("original pull-request job context unavailable")
	}
	var commit struct{ Tree struct{ SHA string } }
	if e = read(ctx, "repos/"+r.FullName+"/git/commits/"+c.SHA, &commit); e != nil || len(commit.Tree.SHA) != 40 {
		return codeQLPlan{}, errors.New("original scanner source tree unavailable")
	}
	return codeQLPlan{RepoID: c.ID, PR: c.PR, Candidate: c.SHA, RunID: runID, Attempt: run.RunAttempt, SHA: c.SHA, Tree: commit.Tree.SHA, Role: "candidate-secret-workflow", Label: secretLabel(c.ID), Repo: r.FullName, Kind: "secret", ContextSHA: pr.MergeSHA}, nil
}

func validGuestPlan(p codeQLPlan) bool {
	known := false
	for _, c := range candidates {
		if c.ID == p.RepoID && c.PR == p.PR && c.SHA == p.Candidate {
			known = true
			break
		}
	}
	if !known {
		return false
	}
	if p.Kind == "" {
		return p.ContextSHA == "" && p.Label == codeQLLabel(p.RepoID)
	}
	return p.Kind == "secret" && p.RepoID == 1285649124 && p.Label == secretLabel(p.RepoID) && len(p.ContextSHA) == 40 && strings.Trim(p.ContextSHA, "0123456789abcdef") == ""
}
