//go:build civ6contract

package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	contract "cc/civ6-ci-contract"
)

// Drives the parent's real Verify against a recording reader and requires every
// endpoint it asks for (including the branch ref of the actual original PR
// branch, which contains slashes) to pass this helper's allowlist.
func TestParentVerifyReadsPassTheAllowlist(t *testing.T) {
	p := rustPlan()
	p.Head, p.Tree, p.Branch, p.PR = contract.OriginalHead, contract.OriginalTree, contract.OriginalBranch, 149
	p.WorkflowRef = repoFull + "/" + rustWorkflow + "@refs/pull/149/merge"
	if e := (bridge{}).Validate(p); e != nil {
		t.Fatal(e)
	}
	repo := map[string]any{"id": repoID, "full_name": repoFull, "private": true, "fork": false, "owner": map[string]any{"login": "AutoDevJapan"}, "permissions": map[string]any{"admin": true}}
	var asked []string
	read := ReadAPI(func(_ context.Context, endpoint string, out any) error {
		asked = append(asked, endpoint)
		var v any
		switch {
		case strings.HasPrefix(endpoint, "repositories/"):
			v = repo
		case strings.HasSuffix(endpoint, "/pulls/149"):
			v = map[string]any{"number": 149, "state": "open", "merge_commit_sha": p.ContextSHA,
				"head": map[string]any{"sha": p.Head, "ref": p.Branch, "repo": repo}, "base": map[string]any{"sha": contract.OriginalBase, "ref": "main", "repo": repo}}
		case strings.Contains(endpoint, "/git/ref/heads/"):
			v = map[string]any{"object": map[string]any{"sha": p.Head}}
		default:
			return errors.New("stop after the branch ref")
		}
		b, _ := json.Marshal(v)
		return json.Unmarshal(b, out)
	})
	guarded := func(ctx context.Context, endpoint string, out any) error {
		if e := allowedRead(endpoint); e != nil {
			return e
		}
		return read(ctx, endpoint, out)
	}
	e := (bridge{}).Verify(context.Background(), p, guarded)
	sawRef := false
	for _, a := range asked {
		if strings.Contains(a, "/git/ref/heads/"+contract.OriginalBranch) {
			sawRef = true
		}
	}
	if !sawRef || (e != nil && strings.Contains(e.Error(), "allowlist")) {
		t.Fatalf("parent's branch-ref read did not pass the allowlist: saw=%v err=%v asked=%v", sawRef, e, asked)
	}
}
