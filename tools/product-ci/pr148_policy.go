package main

import (
	"context"
	"errors"
	"path/filepath"
)

// Read-only prerequisite evidence for the original hash-pinned Rust workflow.
// Product YAML stays in RAM. No source archive, runner, mutation or Cargo call.
func savePR148Policy(ctx context.Context, dir string) error {
	if e := pr148OwnedReceiptRoot(ctx, dir); e != nil {
		return e
	}
	p, e := inspectPR148(ctx, get)
	if e != nil {
		return e
	}
	b, e := pr148Contents(ctx, get, pr148Workflows[0].Path, pr148Head)
	if e != nil {
		return e
	}
	commands, execution, e := pr148WorkflowExecution(b)
	if e != nil {
		return e
	}
	r, e := verify(ctx, candidate{1392152644, 148, pr148Head}, get)
	if e != nil {
		return e
	}
	if r.FullName != pr148Repo || r.Owner.Login != "AutoDevJapan" {
		return errors.New("policy source repository changed")
	}
	return pr148SaveNew(filepath.Join(dir, "pr148-policy.private.json"), struct {
		Source                                                     pr148Proposal
		WorkflowSHA256                                             string
		Commands                                                   pr148CargoPolicy
		Execution                                                  pr148ExecutionPolicy
		PolicyOnly, ProductTestsExecuted, AllRequiredChecksSuccess bool
	}{Source: p, WorkflowSHA256: digest(b), Commands: commands, Execution: execution, PolicyOnly: true})
}
