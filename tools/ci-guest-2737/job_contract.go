package main

import (
	sourcepacket "cc/ci-source-2737"
	"errors"
	"strconv"
	"strings"
)

const liveWorkspace = "/home/ci2737/work/GoDD-MCP-Alpha/GoDD-MCP-Alpha"
const liveSocket = "/home/ci2737/ci-control.sock"
const livePlanFile = "/opt/ci2737-control/plan.json"

type linuxJobPlan struct {
	Source                 sourcepacket.Pin `json:"source"`
	RunID, JobID, RunnerID int64
	Attempt, Jobs, Threads int
	Role, Name, Label      string
	Inputs                 map[string]string
}

func validSourcePin(p sourcepacket.Pin) bool {
	_, ok := p.Profile()
	return ok && len(p.HeadSHA) == 40 && strings.Trim(p.HeadSHA, "0123456789abcdef") == "" && len(p.TreeSHA) == 40 && strings.Trim(p.TreeSHA, "0123456789abcdef") == "" && validHash(p.WorkflowSHA256)
}
func validLinuxJob(p linuxJobPlan, expected sourcepacket.Pin, instance string) bool {
	policy, ok := jobPolicy(p.Source, p.Role)
	if !ok || policy.OS != "Linux" || !validSourcePin(expected) || p.Source != expected || p.RunID < 1 || p.JobID < 1 || p.RunnerID != 0 || p.Attempt < 1 || p.Jobs < 1 || p.Jobs > 10 || p.Threads < 1 || p.Threads > 10 || p.Name != "todo2737-"+instance || p.Label != jobLabel(p.Source, p.Role, p.RunID, p.Attempt) || len(p.Inputs) != 0 {
		return false
	}
	// The pinned original workflow declares no workflow_dispatch inputs. Its
	// workflow_call release inputs are not admission for this finite manual run.
	return true
}

type jobHookContext struct {
	Stage, Repository, SHA, Ref, Event, Job, Name, WorkflowSHA string
	RunID, Attempt                                             string
	Inputs                                                     map[string]string
	Diagnostic                                                 *hookDiagnostic `json:",omitempty"`
}
type liveJobEvent struct {
	ID, Stage, Failure string
	Proof              sourcepacket.Receipt
	Context            jobHookContext
	Configured         bool
	Exit               *int
	HookDiagnostic     *hookDiagnostic   `json:",omitempty"`
	RustProbe          []rustProbeResult `json:",omitempty"`
}
type liveJobAck struct {
	ID, Stage string
	Accepted  bool
	RunnerID  int64
}

func validateJobContext(p linuxJobPlan, c jobHookContext) error {
	if code := jobContextFailure(p, c); code != "" {
		return errors.New(code)
	}
	return nil
}

func jobContextFailure(p linuxJobPlan, c jobHookContext) string {
	profile, ok := p.Source.Profile()
	policy, roleOK := jobPolicy(p.Source, p.Role)
	if !ok || !roleOK {
		return "context_repository"
	}
	event, ref := jobEventRef(p.Source)
	for _, check := range []struct {
		code  string
		valid bool
	}{
		{"diagnostic_only", c.Diagnostic == nil},
		{"context_repository", c.Repository == profile.Repository},
		{"context_sha", c.SHA == p.Source.HeadSHA},
		{"context_workflow_sha", c.WorkflowSHA == p.Source.HeadSHA},
		{"context_ref", c.Ref == ref},
		{"context_event", c.Event == event},
		{"context_run", c.RunID == strconv.FormatInt(p.RunID, 10)},
		{"context_attempt", c.Attempt == strconv.Itoa(p.Attempt)},
		{"context_job", c.Job == policy.ID},
		{"context_runner", c.Name == p.Name},
	} {
		if !check.valid {
			return check.code
		}
	}
	if len(c.Inputs) != len(p.Inputs) {
		return "context_inputs"
	}
	for key, want := range p.Inputs {
		if got, ok := c.Inputs[key]; !ok || got != want {
			return "context_inputs"
		}
	}
	return ""
}
