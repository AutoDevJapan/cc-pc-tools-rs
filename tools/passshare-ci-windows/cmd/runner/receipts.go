package main

import (
	contract "cc/passshare-ci-windows/internal/contract"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
)

type hookReceipt struct {
	Stage, Instance                                                                      string
	RunID                                                                                int64
	JobID                                                                                int64
	Attempt                                                                              int
	Event, Branch, Job, Head, Tree, ContextSHA, WorkflowSHA, WorkflowRef, WorkflowDigest string
	PR                                                                                   int
	EventDigest, Source, Target                                                          string
	SourceVerified                                                                       bool
}

func blockedGitKey(key string) bool {
	return strings.Contains(key, "extraheader") || strings.HasPrefix(key, "credential.") || strings.Contains(key, "insteadof") || strings.HasPrefix(key, "include") || key == "extensions.worktreeconfig" || key == "core.fsmonitor" || key == "core.hookspath" || key == "core.sshcommand" || strings.HasSuffix(key, ".pushurl") || strings.HasPrefix(key, "filter.")
}
func (p Plan) source() string {
	return filepath.Join(p.runner(), "_work", "PassShare", "PassShare")
}
func (p Plan) target() string {
	return filepath.Join(p.source(), "tools", "target")
}
func makeHookReceipt(p Plan, stage string, event []byte) hookReceipt {
	return hookReceipt{Stage: stage, Instance: p.Instance, RunID: p.RunID, JobID: p.JobID, Attempt: p.Attempt, Event: p.Event, Branch: p.Branch, PR: p.PR, Job: p.Job, Head: p.Head, Tree: p.Tree, ContextSHA: p.ContextSHA, WorkflowSHA: p.WorkflowSHA, WorkflowRef: p.WorkflowRef, WorkflowDigest: p.WorkflowDigest, EventDigest: digest(event), Source: p.source(), Target: p.target(), SourceVerified: true}
}
func validateHookData(p Plan, stage string, b []byte) (hookReceipt, error) {
	var r hookReceipt
	if len(b) > 65536 || json.Unmarshal(b, &r) != nil {
		return r, errors.New("hook evidence JSON rejected")
	}
	want := makeHookReceipt(p, stage, nil)
	want.EventDigest = r.EventDigest
	if r != want || !hex64.MatchString(r.EventDigest) {
		return r, errors.New("hook evidence context mismatch")
	}
	return r, nil
}

type apiJob struct {
	ID                 int64
	RunID              int64 `json:"run_id"`
	Attempt            int   `json:"run_attempt"`
	Name               string
	Labels             []string
	Runner             string `json:"runner_name"`
	Status, Conclusion string
}

var errPending = errors.New("official job or ephemeral registration is not terminal yet")

// sameLabels compares label sets without trusting their order.
func sameLabels(got, want []string) bool {
	a, b := slices.Clone(got), slices.Clone(want)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func checkJob(p Plan, j apiJob, finished bool) error {
	if j.ID != p.JobID || j.RunID != p.RunID || j.Attempt != p.Attempt || j.Name != p.JobName || !sameLabels(j.Labels, contract.Labels(p.Plan)) || (j.Runner != "" && j.Runner != p.name()) {
		return errors.New("actual job route/runner rejected")
	}
	if finished {
		if j.Status == "queued" || j.Status == "in_progress" || j.Status == "waiting" || j.Status == "pending" {
			return errPending
		}
		if j.Runner != p.name() || j.Status != "completed" || j.Conclusion != "success" {
			return errors.New("official target job did not complete successfully on this runner")
		}
	}
	return nil
}

type apiRunner struct {
	ID     int64
	Name   string
	Labels []struct{ Name string }
}

// ownRunner lists the repository's runners and returns only this plan's unique
// registration. Other runners (the long-lived Linux/Windows workers and the
// Linux ephemeral runner) are neither inspected nor affected.
func ownRunner(p Plan, read readAPI) (*apiRunner, error) {
	var runners struct {
		Total   int `json:"total_count"`
		Runners []apiRunner
	}
	if e := read("repos/"+repository+"/actions/runners?per_page=100", &runners); e != nil {
		return nil, e
	}
	if runners.Total != len(runners.Runners) || runners.Total > 100 {
		return nil, errors.New("complete repository runner inventory required")
	}
	var own *apiRunner
	for i, r := range runners.Runners {
		if r.Name == p.name() {
			if own != nil {
				return nil, errors.New("duplicate unique runner registration")
			}
			own = &runners.Runners[i]
		}
	}
	return own, nil
}

// auditPhase selects which registration state is legitimate for this unique
// ephemeral runner: absent before registration, present or absent while the
// job runs, and absent again once the official job is terminal.
type auditPhase int

const (
	phaseBefore auditPhase = iota
	phaseRunning
	phaseFinished
	// phaseQueued is the periodic check while the job still waits for a runner;
	// the unique registration may or may not exist yet.
	phaseQueued
)
