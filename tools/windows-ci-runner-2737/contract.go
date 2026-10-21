// Package windowsrunner implements the finite Windows guest side of original
// PR3613. It never dispatches workflows, chooses a revision or acquires a gate.
package windowsrunner

import (
	sourcepacket "cc/ci-source-2737"
	"cc/product-ci/normalci"
	"cc/product-ci/pressci"
	"errors"
	"strconv"
	"strings"
)

const Role = "windows-native-check"
const Repository = "AutoDevJapan/GoDD-MCP-Alpha"
const labelPrefix = "cc-product-2737-1154066087-windows-windows-native-check-"
const RunnerVersion = "2.338.0"
const RunnerURL = "https://github.com/actions/runner/releases/download/v2.338.0/actions-runner-win-x64-2.338.0.zip"
const RunnerSHA256 = "f48e0750a21812bca5f82de5f7f5aeae71abee647fab5a582f1742d07eba455f"

// Plan's job counts are an admission RESULT. Only the host can prove it owns
// the canonical Cargo gate; no environment marker is evidence of that lease.
type Plan struct {
	Source                 sourcepacket.Pin
	Instance               string
	RunID, JobID, RunnerID int64
	Attempt, Jobs, Threads int
	Role, Name, Label      string
	Inputs                 map[string]string
}

// Seed is public, offline staged on the fresh clone, never on the retained SDK.
type Seed struct {
	Plan         Plan
	ClientSHA256 string
}

func validHex(s string, n int) bool { return len(s) == n && strings.Trim(s, "0123456789abcdef") == "" }
func Label(runID int64) string      { return labelPrefix + strconv.FormatInt(runID, 10) }
func (p Plan) policy() (sourcepacket.Profile, pressci.Job, string, bool) {
	profile, ok := p.Source.Profile()
	if !ok {
		return profile, pressci.Job{}, "", false
	}
	if p.Source.Product == sourcepacket.PressProduct {
		j, ok := pressci.RoleJob(p.Source.Workflow, p.Role)
		if !ok || j.OS != "Windows" || p.Source.PullHeadSHA == "" {
			return profile, j, "", false
		}
		return profile, j, pressci.RuntimeLabel(p.Source.Workflow, p.Role, p.RunID, p.Attempt), true
	}
	return profile, pressci.Job{ID: Role, OS: "Windows", TimeoutMinutes: 20}, Label(p.RunID), p.Role == Role
}
func (p Plan) timeoutMinutes() int {
	_, j, _, ok := p.policy()
	if !ok {
		return 0
	}
	return j.TimeoutMinutes
}
func (p Plan) Validate() error {
	_, _, label, ok := p.policy()
	if !ok || !validHex(p.Instance, 32) || !validHex(p.Source.HeadSHA, 40) || !validHex(p.Source.TreeSHA, 40) || !validHex(p.Source.WorkflowSHA256, 64) || p.RunID < 1 || p.JobID < 1 || p.RunnerID != 0 || p.Attempt < 1 || p.Jobs < 1 || p.Jobs > 10 || p.Threads < 1 || p.Threads > 10 || p.Name != "todo2737-"+p.Instance || p.Label != label || len(p.Inputs) != 0 {
		return errors.New("Windows plan rejected")
	}
	return nil
}
func (s Seed) Validate() error {
	if !validHex(s.ClientSHA256, 64) {
		return errors.New("Windows seed certificate pin rejected")
	}
	return s.Plan.Validate()
}

// NewSeed consumes the host's normalci.Plan result. The baseline legacy-label
// resolver intentionally fails this check until the parent connects run labels.
func NewSeed(pin sourcepacket.Pin, b normalci.Binding, instance, clientHash string, jobs, threads int) (Seed, error) {
	p := Plan{Source: pin, Instance: instance, RunID: b.RunID, JobID: b.JobID, Attempt: b.Attempt, Jobs: jobs, Threads: threads, Role: b.Role, Name: "todo2737-" + instance, Label: b.Label}
	s := Seed{p, clientHash}
	if s.Validate() != nil || ValidateBinding(p, b) != nil {
		return Seed{}, errors.New("official Windows binding rejected")
	}
	return s, nil
}
func ValidateBinding(p Plan, b normalci.Binding) error {
	profile, j, _, ok := p.policy()
	if !ok || p.Validate() != nil || b.RepoID != profile.RepoID || b.PR != profile.PR || b.SHA != p.Source.HeadSHA || b.Tree != p.Source.TreeSHA || b.WorkflowSHA != p.Source.WorkflowSHA256 || b.RunID != p.RunID || b.JobID != p.JobID || b.Attempt != p.Attempt || b.Role != p.Role || b.Label != p.Label || b.OS != "Windows" || b.TimeoutMinutes != j.TimeoutMinutes || b.JobName == "" {
		return errors.New("official Windows binding changed")
	}
	return nil
}

type HookContext struct {
	Repository, SHA, Ref, Event, Role, Name, WorkflowSHA string
	RunID                                                int64
	Attempt                                              int
	Inputs                                               map[string]string
}

func (c HookContext) Validate(p Plan) error {
	profile, j, _, ok := p.policy()
	event, ref := "workflow_dispatch", "refs/heads/"+profile.HeadRef
	if p.Source.PullHeadSHA != "" {
		event, ref = "pull_request", "refs/pull/175/merge"
	}
	if !ok || c.Repository != profile.Repository || c.SHA != p.Source.HeadSHA || c.WorkflowSHA != p.Source.HeadSHA || c.Ref != ref || c.Event != event || c.Role != j.ID || c.Name != p.Name || c.RunID != p.RunID || c.Attempt != p.Attempt || len(c.Inputs) != 0 {
		return errors.New("official hook context rejected")
	}
	return nil
}
func validProof(p Plan, r sourcepacket.Receipt) bool {
	profile, _, _, ok := p.policy()
	return ok && r.RepoID == profile.RepoID && r.PR == profile.PR && r.HeadSHA == p.Source.HeadSHA && r.TreeSHA == p.Source.TreeSHA && r.BlobCount > 0 && r.PhysicalBlobsVerified
}

type Stage string

const (
	SourceReady  Stage = "source-ready"
	Configured   Stage = "configured"
	Start        Stage = "start"
	Complete     Stage = "complete"
	RunnerExited Stage = "runner-exited"
	Cancelled    Stage = "cancelled"
	Failed       Stage = "failed"
)

type Event struct {
	Instance    string
	Stage       Stage
	RunnerID    int64
	Proof       sourcepacket.Receipt
	Context     HookContext
	Exit        *uint32
	Drained     bool
	LogSHA256   string
	FailureCode string
	NativeError uint32 `json:",omitempty"`
}
type ack struct {
	Instance string
	Stage    Stage
	Accepted bool
	RunnerID int64
}
type configuration struct{ Token []byte }
