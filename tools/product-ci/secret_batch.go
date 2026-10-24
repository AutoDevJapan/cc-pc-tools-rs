package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"cc/product-ci/inputqual"
)

// The additional source is the original, explicitly answered dependency scope.
// Generic current-PR inputs and the original four candidates are unchanged.
func finiteDependencyBinding(b currentPRBinding) bool {
	if b.OriginTodo != 0 || b.OwnerScopeSHA256 != "" {
		return false
	}
	standard := b
	standard.PR = 995
	return standard.valid() && b.PR == 939 && b.Head == "f1c6e235d93d0719532b592b03037edcbe7bffef" && b.Tree == "df13e3dd2d796812f922868578df2d0ef8beceb5" && b.Base == "41090d19329b81dd1bd076f3336d723823be4b82" && b.RunID == 39344575918 && b.Attempt == 2 && b.JobIDs == (currentPRJobIDs{118550301356}) && b.Kind == "secret" && b.Context == "de71ff5d9a1d5a61328cbcf054347f9686ad7b8e"
}

func validFiniteBindings(batch [2]currentPRBinding) bool {
	b, d := batch[0], batch[1]
	if b.OriginTodo != 0 || b.OwnerScopeSHA256 != "" {
		return false
	}
	return b.valid() && b.PR == 995 && b.Head == "cbd688871651e46d4821a4535d57eaa408105ff2" && b.Tree == "73a5d39e6694be7e6e604003dcb4e7bdc087c1c5" && b.Base == "41090d19329b81dd1bd076f3336d723823be4b82" && b.RunID == 39384472279 && b.Attempt == 1 && b.JobIDs == (currentPRJobIDs{118500893552}) && b.Kind == "secret" && b.Context == "6e7ca682c861024b6f08f31382851dab4bceff35" && finiteDependencyBinding(d)
}

func finiteBatchMember(p codeQLPlan, b currentPRBinding) codeQLPlan {
	p.RepoID, p.PR, p.Candidate, p.SHA, p.Tree = b.RepoID, b.PR, b.Head, b.Head, b.Tree
	p.RunID, p.Attempt, p.ContextSHA, p.Current = b.RunID, b.Attempt, b.Context, b
	return p
}

func structCandidate(p codeQLPlan) candidate { return candidate{p.RepoID, p.PR, p.SHA} }

func validFiniteSecretBatch(p codeQLPlan) bool {
	if !validFiniteBindings(p.Batch) || p.Kind != "secret" || p.Role != "candidate-secret-workflow" || p.Label != secretLabel(1285649124) {
		return false
	}
	for _, b := range p.Batch {
		if p.Current == b && finiteBatchMember(p, b) == p {
			return true
		}
	}
	return false
}

func finiteSecretBatchContains(p codeQLPlan, run, jobRun, job int64) bool {
	if !validFiniteSecretBatch(p) || run != jobRun {
		return false
	}
	for _, b := range p.Batch {
		if run == b.RunID && job == b.JobIDs[0] {
			return true
		}
	}
	return false
}

func planFiniteSecretBatch(ctx context.Context, batch [2]currentPRBinding, selectedPR int, read api) (codeQLPlan, error) {
	return planFiniteSecretBatchWithPolicy(ctx, batch, selectedPR, read, secretWorkflowPathSHA, secretWorkflowSHA)
}

func planFiniteSecretBatchWithPolicy(ctx context.Context, batch [2]currentPRBinding, selectedPR int, read api, pathDigest, workflowDigest string) (codeQLPlan, error) {
	if !validFiniteBindings(batch) {
		return codeQLPlan{}, errors.New("finite secret batch bindings rejected")
	}
	var selected codeQLPlan
	for _, b := range batch {
		p, err := planCurrentPRSnapshot(ctx, b, read, pathDigest, workflowDigest, b.PR != selectedPR)
		if err != nil {
			return codeQLPlan{}, err
		}
		if b.PR == selectedPR {
			selected = p
		}
	}
	selected.Batch = batch
	if !validFiniteSecretBatch(selected) {
		return codeQLPlan{}, errors.New("finite secret batch selection rejected")
	}
	return selected, nil
}

func selectFiniteBatchAdmission(p codeQLPlan, q codeQLAdmission) (codeQLPlan, error) {
	if !validFiniteSecretBatch(p) {
		return codeQLPlan{}, errors.New("finite batch plan rejected")
	}
	for _, b := range p.Batch {
		if q.RunID == b.RunID && q.Attempt == b.Attempt && q.SHA == b.Head {
			return finiteBatchMember(p, b), nil
		}
	}
	return codeQLPlan{}, errors.New("job outside finite batch rejected")
}

func selectFiniteBatchContext(p codeQLPlan, repo, sha, run, attempt string) (codeQLPlan, error) {
	if !validFiniteSecretBatch(p) || repo != p.Repo {
		return codeQLPlan{}, errors.New("finite batch job context rejected")
	}
	for _, b := range p.Batch {
		if sha == b.Context && run == strconv.FormatInt(b.RunID, 10) && attempt == strconv.Itoa(b.Attempt) {
			return finiteBatchMember(p, b), nil
		}
	}
	return codeQLPlan{}, errors.New("job outside finite batch rejected")
}

// This separate entry point preserves the qualified single995 origin route.
func finiteSecretBatchCLI(ctx context.Context, args []string) error {
	inspect := len(args) == 1 && args[0] == "current-pr-batch-inspect"
	job := len(args) == 4 && args[0] == "current-pr-batch-job"
	if !inspect && !job {
		return errors.New("current-pr-batch-inspect | current-pr-batch-job <new-owned-dir> <Linux-helper> <gitleaks-ELF>")
	}
	original, err := inputqual.Construct()
	if err != nil {
		return err
	}
	b, err := consumeCurrentPRSnapshot(original)
	if err != nil {
		return err
	}
	dependency, err := inputqual.ConstructDependency()
	if err != nil {
		return err
	}
	d, err := consumeCurrentPRSnapshot(dependency)
	if err != nil {
		return err
	}
	batch := [2]currentPRBinding{b, d}
	// Prefer the product repair as the initial plan. GitHub may assign either
	// queued member; the guest and authenticated host hook select the real one.
	selected, err := readyFiniteBatchMember(ctx, batch, get)
	if err != nil {
		return err
	}
	p, err := planFiniteSecretBatch(ctx, batch, selected.PR, get)
	if err != nil {
		return err
	}
	if err = checkCurrentPRReady(ctx, p, get); err != nil {
		return err
	}
	if err = checkCodeQLQueue(ctx, p, get); err != nil {
		return err
	}
	if inspect {
		return json.NewEncoder(os.Stdout).Encode(p)
	}
	return executeProductPlan(ctx, structCandidate(p), p, args[1], args[2], args[3])
}

func readyFiniteBatchMember(ctx context.Context, batch [2]currentPRBinding, read api) (currentPRBinding, error) {
	if !validFiniteBindings(batch) {
		return currentPRBinding{}, errors.New("finite batch bindings rejected")
	}
	var selected currentPRBinding
	for _, b := range batch {
		var j struct {
			ID                 int64
			Run                int64  `json:"run_id"`
			Attempt            int    `json:"run_attempt"`
			Head               string `json:"head_sha"`
			Status, Conclusion string
			Runner             int64 `json:"runner_id"`
		}
		if err := read(ctx, fmt.Sprintf("repositories/%d/actions/jobs/%d", b.RepoID, b.JobIDs[0]), &j); err != nil {
			return currentPRBinding{}, err
		}
		if j.ID != b.JobIDs[0] || j.Run != b.RunID || j.Attempt != b.Attempt || j.Head != b.Head {
			return currentPRBinding{}, errors.New("finite batch job identity differs")
		}
		if j.Status == "completed" && j.Conclusion == "success" {
			continue
		}
		if j.Status != "queued" || j.Runner != 0 {
			return currentPRBinding{}, errors.New("finite batch member is active or unsuccessful")
		}
		if selected == (currentPRBinding{}) {
			selected = b
		}
	}
	if selected == (currentPRBinding{}) {
		return selected, errors.New("both finite secret jobs already successful")
	}
	return selected, nil
}
