//go:build civ6contract

package main

import (
	contract "cc/civ6-ci-contract"
	"context"
)

// bridge links the parent's reviewed contract package. Build with
// -tags civ6contract after the parent integrates cc/civ6-ci-contract (the
// go.mod require/replace for ../civ6-ci-contract belongs to that integration).
type bridge struct{}

func cp(p Plan) contract.Plan { return contract.Plan(p) }

func (bridge) Validate(p Plan) error  { return cp(p).Validate() }
func (bridge) Labels(p Plan) []string { return contract.Labels(cp(p)) }
func (bridge) OS(p Plan) string       { return cp(p).OS() }
func (bridge) MatchRunPath(p Plan, runPath string) bool {
	return contract.MatchRunPath(cp(p), runPath)
}
func (bridge) Verify(ctx context.Context, p Plan, read ReadAPI) error {
	return contract.Verify(ctx, cp(p), contract.ReadAPI(read))
}
func (bridge) RequireSuccess(ctx context.Context, p Plan, runner string, read ReadAPI) error {
	return contract.RequireSuccess(ctx, cp(p), runner, contract.ReadAPI(read))
}

func init() { activeContract = bridge{} }
