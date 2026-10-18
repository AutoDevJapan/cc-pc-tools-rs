package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// This mode changes only the three reviewed workflow blobs on the original
// PR148 branch. It does not dispatch, register a runner, or post CI results.
type pr148RoutingReceipt struct {
	OriginalHead, OriginalTree, Head, Tree, LinuxWorkflowDigest string
	Proposal                                                    pr148Proposal
	BranchUpdateAttempted, BranchVerified                       bool
	RefWriteReturnedError                                       bool
	RefObservedAfterError                                       string
	AllRequiredChecksSuccess                                    bool
}

func routePR148(ctx context.Context, dir string) error {
	if e := pr148OwnedReceiptRoot(ctx, dir); e != nil {
		return e
	}
	return routePR148Using(ctx, dir, get, mutateJSON)
}

func routePR148Using(ctx context.Context, dir string, read api, write githubMutator) error {
	proposal, e := inspectPR148(ctx, read)
	if e != nil {
		return e
	}
	r := pr148RoutingReceipt{OriginalHead: pr148Head, OriginalTree: pr148Tree, Proposal: proposal}
	if e = pr148SaveNew(filepath.Join(dir, "pr148-routing-intent.private.json"), r); e != nil {
		return e
	}
	type treeInput struct {
		Path    string `json:"path"`
		Mode    string `json:"mode"`
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	entries := []treeInput{}
	expected := map[string]string{}
	for _, w := range pr148Workflows {
		original, err := pr148Contents(ctx, read, w.Path, pr148Head)
		if err != nil {
			return err
		}
		b, err := proposePR148(original, w)
		if err != nil {
			return err
		}
		entries = append(entries, treeInput{w.Path, "100644", "blob", string(b)})
		expected[w.Path] = gitObjectID("blob", b)
		if w.Job == "test" {
			r.LinuxWorkflowDigest = digest(b)
		}
	}
	var tree struct{ SHA string }
	if e = write(ctx, "POST", "repos/"+pr148Repo+"/git/trees", map[string]any{"base_tree": pr148Tree, "tree": entries}, &tree); e != nil {
		return e
	}
	if !pr148SHA(tree.SHA) || tree.SHA == pr148Tree {
		return errors.New("routing tree response rejected")
	}
	r.Tree = tree.SHA
	var old, next pr148APITree
	if e = read(ctx, "repos/"+pr148Repo+"/git/trees/"+pr148Tree+"?recursive=1", &old); e != nil {
		return e
	}
	if e = read(ctx, "repos/"+pr148Repo+"/git/trees/"+r.Tree+"?recursive=1", &next); e != nil {
		return e
	}
	if next.SHA != r.Tree {
		return errors.New("routing tree identity changed")
	}
	if e = pr148TreeDelta(old, next, expected); e != nil {
		return e
	}
	var commit struct{ SHA string }
	if e = write(ctx, "POST", "repos/"+pr148Repo+"/git/commits", map[string]any{"message": "ci: route PR148 checks to finite repository runners", "tree": r.Tree, "parents": []string{pr148Head}}, &commit); e != nil {
		return e
	}
	if !pr148SHA(commit.SHA) || commit.SHA == pr148Head {
		return errors.New("routing commit response rejected")
	}
	r.Head = commit.SHA
	var actual originalCommit
	if e = read(ctx, "repos/"+pr148Repo+"/git/commits/"+r.Head, &actual); e != nil {
		return e
	}
	if actual.Tree.SHA != r.Tree || len(actual.Parents) != 1 || actual.Parents[0].SHA != pr148Head {
		return errors.New("routing commit ancestry changed")
	}
	if _, e = originalCommitBytes(actual, r.Head); e != nil {
		return e
	}
	// Save the exact object identities before touching the only allowed ref.
	if e = pr148SaveNew(filepath.Join(dir, "pr148-routing-objects.private.json"), r); e != nil {
		return e
	}
	if _, e = inspectPR148(ctx, read); e != nil {
		return e
	}
	r.BranchUpdateAttempted = true
	if e = pr148SaveNew(filepath.Join(dir, "pr148-routing-ref-attempt.private.json"), r); e != nil {
		return e
	}
	if e = write(ctx, "PATCH", "repos/"+pr148Repo+"/git/refs/heads/"+pr148Branch, map[string]any{"sha": r.Head, "force": false}, nil); e != nil {
		// Observe once after a possibly lost successful response. Never repeat
		// the write, revert the ref, or treat an unverified response as success.
		r.RefWriteReturnedError = true
		var observed struct{ Object struct{ SHA string } }
		readErr := read(ctx, "repos/"+pr148Repo+"/git/ref/heads/"+pr148Branch, &observed)
		r.RefObservedAfterError = "unverified"
		if readErr == nil {
			r.RefObservedAfterError = "other-head"
			if observed.Object.SHA == pr148Head {
				r.RefObservedAfterError = "original-head"
			}
			if observed.Object.SHA == r.Head {
				r.RefObservedAfterError = "intended-head"
			}
		}
		if saveErr := pr148SaveNew(filepath.Join(dir, "pr148-routing-ref-observation.private.json"), r); saveErr != nil {
			return errors.Join(e, readErr, saveErr)
		}
		if readErr != nil || observed.Object.SHA != r.Head {
			return errors.Join(e, readErr)
		}
	}
	if e = verifyPR148RoutingSource(ctx, r.Head, r.Tree, r.LinuxWorkflowDigest, read); e != nil {
		return fmt.Errorf("branch update requires recovery: %w", e)
	}
	r.BranchVerified = true
	return pr148SaveNew(filepath.Join(dir, "pr148-routing-result.private.json"), r)
}
