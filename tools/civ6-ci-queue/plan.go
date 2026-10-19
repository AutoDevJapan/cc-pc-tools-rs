// Package main is the finite queue controller for the one explicitly owned
// repository of ToDo2962. It admits each official run/attempt/job at most once,
// starts at most one fixed supervisor per OS and never reruns, writes statuses
// or accepts a command, executable or callback from its input.
package main

import (
	"context"
	"errors"
	"strconv"
)

// Repository identity is a compile-time constant: no configuration can widen it.
const Repository = "AutoDevJapan/civ6-banseiki"
const RepoID int64 = 1392152644
const OriginalPR = 149
const CodeQLWorkflow = "dynamic/github-code-scanning/codeql"

// Fixed acceptance budget (ToDo2962): PR149 official jobs, then the normal
// push jobs after its merge. Further admission needs the audited continuation.
const InitialBudget = 6
const NormalBudget = 6

// Plan is the parent's shared, secret-free job identity (same JSON tags as
// cc/civ6-ci-contract.Plan). It is only ever built from observed official data
// and is re-verified by the contract before any registration.
type Plan struct {
	Version        int    `json:"version"`
	RepoID         int64  `json:"repo_id"`
	Head           string `json:"head"`
	Tree           string `json:"tree"`
	Event          string `json:"event"`
	Branch         string `json:"branch"`
	PR             int    `json:"pr"`
	ContextSHA     string `json:"context_sha"`
	WorkflowSHA    string `json:"workflow_sha"`
	WorkflowRef    string `json:"workflow_ref"`
	Workflow       string `json:"workflow"`
	WorkflowDigest string `json:"workflow_digest"`
	RunID          int64  `json:"run_id"`
	Attempt        int    `json:"attempt"`
	JobID          int64  `json:"job_id"`
	Job            string `json:"job"`
	JobName        string `json:"job_name"`
}

// ReadAPI is the read-only GitHub reader shape shared with the contract.
type ReadAPI func(context.Context, string, any) error

// Contract is exactly the surface of the parent's tools/civ6-ci-contract used
// here. It carries no callback or command parameter.
type Contract interface {
	Validate(Plan) error
	Labels(Plan) []string
	OS(Plan) string
	// MatchRunPath accepts the REST API's bare path and path@ref run forms.
	MatchRunPath(p Plan, runPath string) bool
	Verify(context.Context, Plan, ReadAPI) error
	RequireSuccess(ctx context.Context, p Plan, runnerName string, read ReadAPI) error
}

var errContractUnwired = errors.New("parent contract is not linked")

// unwiredContract keeps the controller fail-closed when built without the
// parent's reviewed contract package.
type unwiredContract struct{}

func (unwiredContract) Validate(Plan) error                         { return errContractUnwired }
func (unwiredContract) Labels(Plan) []string                        { return nil }
func (unwiredContract) OS(Plan) string                              { return "" }
func (unwiredContract) MatchRunPath(Plan, string) bool              { return false }
func (unwiredContract) Verify(context.Context, Plan, ReadAPI) error { return errContractUnwired }
func (unwiredContract) RequireSuccess(context.Context, Plan, string, ReadAPI) error {
	return errContractUnwired
}

var activeContract Contract = unwiredContract{}

// Key is the permanent identity of one official job execution.
func (p Plan) Key() string {
	return strconv.FormatInt(p.RunID, 10) + "/" + strconv.Itoa(p.Attempt) + "/" + strconv.FormatInt(p.JobID, 10)
}
