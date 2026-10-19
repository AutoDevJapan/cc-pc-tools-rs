package main

import (
	"context"
	"errors"
)

// ReadAPI is the parent's read-only GitHub reader shape. Every call made through
// it is first checked against a short endpoint allowlist (see api.go).
type ReadAPI func(ctx context.Context, endpoint string, out any) error

// Contract is exactly the surface of the parent's tools/civ6-ci-contract that
// this helper calls. It has no callback or command parameter, so contract code
// cannot make the helper run anything.
type Contract interface {
	Validate(Plan) error
	Labels(Plan) []string
	// Verify re-checks the repository, source, workflows and official run/job.
	Verify(context.Context, Plan, ReadAPI) error
	// VerifyAssigned also requires the official job to run on this unique runner.
	VerifyAssigned(ctx context.Context, p Plan, runnerName string, read ReadAPI) error
	// RequireSuccess requires the official job to be completed/success on it.
	RequireSuccess(ctx context.Context, p Plan, runnerName string, read ReadAPI) error
	// CheckEvent compares the actual SDK job environment and event payload.
	CheckEvent(p Plan, env map[string]string, event []byte) error
}

// activeContract stays fail-closed unless the binary is built with the
// civ6contract tag (contract_bridge.go) or a test installs a fixture.
var activeContract Contract = unwiredContract{}

var errContractUnwired = errors.New("parent contract is not linked: build with -tags civ6contract")

type unwiredContract struct{}

func (unwiredContract) Validate(Plan) error                         { return errContractUnwired }
func (unwiredContract) Labels(Plan) []string                        { return nil }
func (unwiredContract) Verify(context.Context, Plan, ReadAPI) error { return errContractUnwired }
func (unwiredContract) VerifyAssigned(context.Context, Plan, string, ReadAPI) error {
	return errContractUnwired
}
func (unwiredContract) RequireSuccess(context.Context, Plan, string, ReadAPI) error {
	return errContractUnwired
}
func (unwiredContract) CheckEvent(Plan, map[string]string, []byte) error { return errContractUnwired }
