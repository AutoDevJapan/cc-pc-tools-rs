package main

import (
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/normalci"
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
	return len(p.HeadSHA) == 40 && strings.Trim(p.HeadSHA, "0123456789abcdef") == "" && len(p.TreeSHA) == 40 && strings.Trim(p.TreeSHA, "0123456789abcdef") == "" && validHash(p.WorkflowSHA256)
}
func validLinuxJob(p linuxJobPlan, expected sourcepacket.Pin, instance string) bool {
	if !validSourcePin(expected) || p.Source != expected || p.RunID < 1 || p.JobID < 1 || p.RunnerID != 0 || p.Attempt < 1 || p.Jobs < 1 || p.Jobs > 10 || p.Threads < 1 || p.Threads > 10 || p.Name != "todo2737-"+instance || p.Label != normalci.RuntimeLabel(p.Role, p.RunID) || (p.Role != "verify" && p.Role != "ci-gate") || len(p.Inputs) != 0 {
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
}
type liveJobEvent struct {
	ID, Stage, Failure string
	Proof              sourcepacket.Receipt
	Context            jobHookContext
	Configured         bool
	Exit               *int
}
type liveJobAck struct {
	ID, Stage string
	Accepted  bool
	RunnerID  int64
}

func validateJobContext(p linuxJobPlan, c jobHookContext) error {
	if c.Repository != "AutoDevJapan/GoDD-MCP-Alpha" || c.SHA != p.Source.HeadSHA || c.WorkflowSHA != p.Source.HeadSHA || c.Ref != "refs/heads/"+sourcepacket.HeadRef || c.Event != "workflow_dispatch" || c.RunID != strconv.FormatInt(p.RunID, 10) || c.Attempt != strconv.Itoa(p.Attempt) || c.Job != p.Role || c.Name != p.Name {
		return errors.New("official job context rejected")
	}
	if len(c.Inputs) != len(p.Inputs) {
		return errors.New("dispatch input binding rejected")
	}
	for key, want := range p.Inputs {
		if got, ok := c.Inputs[key]; !ok || got != want {
			return errors.New("dispatch input binding rejected")
		}
	}
	return nil
}
