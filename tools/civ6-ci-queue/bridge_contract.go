package main

import (
	contract "cc/civ6-ci-contract"
	"context"
)

// bridge always links the parent's reviewed contract package.
type bridge struct{}

func cp(p Plan) contract.Plan { return contract.Plan(p) }

func (bridge) Validate(p Plan) error  { return cp(p).Validate() }
func (bridge) Labels(p Plan) []string { return contract.Labels(cp(p)) }
func (bridge) OS(p Plan) string       { return cp(p).OS() }
func (bridge) MatchRunPath(p Plan, runPath string) bool {
	return contract.MatchRunPath(cp(p), runPath)
}
func (bridge) Verify(ctx context.Context, p Plan, read ReadAPI) error {
	if e := contract.Verify(ctx, cp(p), contract.ReadAPI(read)); e != nil {
		return e
	}
	return contract.CheckRegistrationQueue(ctx, cp(p), contract.Labels(cp(p)), contract.ReadAPI(read))
}
func (bridge) RequireSuccess(ctx context.Context, p Plan, runner string, read ReadAPI) error {
	return contract.RequireSuccess(ctx, cp(p), runner, contract.ReadAPI(read))
}

func init() { activeContract = bridge{} }
