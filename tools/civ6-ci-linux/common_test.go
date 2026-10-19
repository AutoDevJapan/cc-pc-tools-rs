package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("CC_CIL_GATE_CHILD") == "1" {
		os.Exit(gatedChild(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func rep(s string, n int) string { return strings.Repeat(s, n) }

func rustPlan() Plan {
	return Plan{Version: 1, RepoID: repoID, Head: rep("1", 40), Tree: rep("2", 40), Event: "pull_request", Branch: "scheduler/fixture", PR: 5,
		ContextSHA: rep("3", 40), WorkflowSHA: rep("3", 40), WorkflowRef: repoFull + "/" + rustWorkflow + "@refs/pull/5/merge", Workflow: rustWorkflow,
		WorkflowDigest: rep("4", 64), RunID: 101, Attempt: 1, JobID: 102, Job: "test", JobName: "test (ubuntu-latest)"}
}

func codeQLPlan(name string) Plan {
	return Plan{Version: 1, RepoID: repoID, Head: rep("1", 40), Tree: rep("2", 40), Event: "dynamic", Branch: "main", ContextSHA: rep("1", 40),
		WorkflowSHA: rep("1", 40), Workflow: codeQLWorkflow, RunID: 201, Attempt: 1, JobID: 202, Job: "analyze", JobName: name}
}

// fakeContract records calls and lets each test choose the parent's answers.
type fakeContract struct {
	validate, verify, assigned, success, event error
	labels                                     []string
	calls                                      map[string]int
}

func (f *fakeContract) count(k string) {
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[k]++
}
func (f *fakeContract) Validate(Plan) error  { f.count("validate"); return f.validate }
func (f *fakeContract) Labels(Plan) []string { return f.labels }
func (f *fakeContract) Verify(context.Context, Plan, ReadAPI) error {
	f.count("verify")
	return f.verify
}
func (f *fakeContract) VerifyAssigned(_ context.Context, _ Plan, runner string, _ ReadAPI) error {
	f.count("assigned:" + runner)
	return f.assigned
}
func (f *fakeContract) RequireSuccess(_ context.Context, _ Plan, runner string, _ ReadAPI) error {
	f.count("success:" + runner)
	return f.success
}
func (f *fakeContract) CheckEvent(Plan, map[string]string, []byte) error {
	f.count("event")
	return f.event
}

func useContract(t *testing.T, c Contract) {
	t.Helper()
	old := activeContract
	activeContract = c
	t.Cleanup(func() { activeContract = old })
}
