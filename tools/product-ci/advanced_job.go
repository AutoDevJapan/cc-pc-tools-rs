package main

import (
	"cc/product-ci/normalci"
	"context"
	"errors"
	"fmt"
)

const advanced2737Head = "ff7da61a41974c0ab1630d5ad311201585de8eec"

// Only the advanced command moves to the explicitly delivered candidate.
// Historical proposals, default setup and other products keep their pins.
func advancedCandidate(c candidate) candidate {
	if c.ID == normalci.RepoID && c.PR == 3613 {
		c.SHA = advanced2737Head
	}
	return c
}

func advancedWorkflowDigest(id int64) string {
	switch id {
	case 1154066087:
		return "5793b4e283395a4057234e68e0c9612cb399f8a37ca3bccb1500a4c90eb52ae6"
	case 1386702183:
		return "7962c1872f8bea8866ee4c49f146fd3d31af68c031f4f4008a980f5f5f6366b8"
	}
	return ""
}

func planAdvancedJob(ctx context.Context, c candidate, runID int64, read api) (codeQLPlan, error) {
	expected := advancedWorkflowDigest(c.ID)
	if expected == "" {
		return codeQLPlan{}, errors.New("advanced workflow repository rejected")
	}
	return planAdvancedWithPolicy(ctx, c, runID, read, expected)
}

// The existing workflow_dispatch path requests every original language without
// changing the PR's cost/label policy. Only the same fixed PR branch is served.
func planAdvancedWithPolicy(ctx context.Context, c candidate, runID int64, read api, expected string) (codeQLPlan, error) {
	r, e := verify(ctx, c, read)
	if e != nil {
		return codeQLPlan{}, e
	}
	var setup codeQLSetup
	if e = read(ctx, "repos/"+r.FullName+"/code-scanning/default-setup", &setup); e != nil {
		return codeQLPlan{}, e
	}
	if setup.State != "not-configured" {
		return codeQLPlan{}, errors.New("original advanced setup must remain active without default setup")
	}
	var pr struct {
		State string
		Head  struct{ SHA, Ref string }
	}
	if e = read(ctx, fmt.Sprintf("repos/%s/pulls/%d", r.FullName, c.PR), &pr); e != nil {
		return codeQLPlan{}, e
	}
	if pr.State != "open" || pr.Head.SHA != c.SHA || pr.Head.Ref == "" {
		return codeQLPlan{}, errors.New("advanced original source changed")
	}
	if c.ID == normalci.RepoID {
		if r.FullName != "AutoDevJapan/GoDD-MCP-Alpha" || pr.Head.Ref != "codex/todo2641-http-response-pseudo-20261015" {
			return codeQLPlan{}, errors.New("advanced task route source rejected")
		}
		var route struct{ Name, Value string }
		if e = read(ctx, "repos/"+r.FullName+"/actions/variables/"+normalci.RouteVariable, &route); e != nil || route.Name != normalci.RouteVariable || route.Value != normalci.LeaseValue("0b2414b10418d265c7823a4f87bb5881") {
			return codeQLPlan{}, errors.New("advanced task route lease rejected")
		}
	}
	var run codeQLRun
	if e = read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d", r.FullName, runID), &run); e != nil {
		return codeQLPlan{}, e
	}
	if run.ID != runID || run.RunAttempt < 1 || run.Repository.ID != c.ID || run.HeadRepo.ID != c.ID || run.HeadRepo.Fork || run.Repository.FullName != r.FullName || run.HeadRepo.FullName != r.FullName || run.HeadSHA != c.SHA || run.HeadBranch != pr.Head.Ref || run.Event != "workflow_dispatch" || run.Path != ".github/workflows/codeql.yml" || (run.Status != "queued" && run.Status != "in_progress" && run.Status != "waiting" && run.Status != "pending") {
		return codeQLPlan{}, errors.New("fixed full advanced workflow dispatch rejected")
	}
	if e = verifyWorkflowBytes(ctx, r.FullName, run.Path, c.SHA, expected, read); e != nil {
		return codeQLPlan{}, e
	}
	var commit struct{ Tree struct{ SHA string } }
	if e = read(ctx, "repos/"+r.FullName+"/git/commits/"+c.SHA, &commit); e != nil || len(commit.Tree.SHA) != 40 {
		return codeQLPlan{}, errors.New("advanced original tree unavailable")
	}
	return codeQLPlan{RepoID: c.ID, PR: c.PR, Candidate: c.SHA, RunID: runID, Attempt: run.RunAttempt, SHA: c.SHA, Tree: commit.Tree.SHA, Role: "candidate-advanced-codeql", Label: codeQLLabel(c.ID), Repo: r.FullName, Kind: "advanced"}, nil
}
