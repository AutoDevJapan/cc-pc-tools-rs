package main

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
)

type hookReceipt struct {
	Stage, Instance                                                                   string
	RunID                                                                             int64
	Attempt                                                                           int
	Job, PRHead, Head, Tree, WorkflowSHA, WorkflowDigest, EventDigest, Source, Target string
	SourceVerified                                                                    bool
}

func blockedGitKey(key string) bool {
	return strings.Contains(key, "extraheader") || strings.HasPrefix(key, "credential.") || strings.Contains(key, "insteadof") || strings.HasPrefix(key, "include") || key == "core.fsmonitor" || key == "core.hookspath" || key == "core.sshcommand" || strings.HasSuffix(key, ".pushurl") || strings.HasPrefix(key, "filter.")
}
func (p Plan) source() string {
	return filepath.Join(p.runner(), "_work", "civ6-banseiki", "civ6-banseiki")
}
func makeHookReceipt(p Plan, stage string, event []byte) hookReceipt {
	return hookReceipt{Stage: stage, Instance: p.Instance, RunID: p.RunID, Attempt: p.Attempt, Job: p.Job, PRHead: p.PRHead, Head: p.Head, Tree: p.Tree, WorkflowSHA: p.WorkflowSHA, WorkflowDigest: p.WorkflowDigest, EventDigest: digest(event), Source: p.source(), Target: filepath.Join(p.runtime(), "target"), SourceVerified: true}
}
func validateHookData(p Plan, stage string, b []byte) (hookReceipt, error) {
	var r hookReceipt
	if len(b) > 65536 || json.Unmarshal(b, &r) != nil {
		return r, errors.New("hook evidence JSON rejected")
	}
	if r.Stage != stage || r.Instance != p.Instance || r.RunID != p.RunID || r.Attempt != p.Attempt || r.Job != p.Job || r.PRHead != p.PRHead || r.Head != p.Head || r.Tree != p.Tree || r.WorkflowSHA != p.WorkflowSHA || r.WorkflowDigest != p.WorkflowDigest || r.Source != p.source() || r.Target != filepath.Join(p.runtime(), "target") || !r.SourceVerified || !hex64.MatchString(r.EventDigest) {
		return r, errors.New("hook evidence context mismatch")
	}
	return r, nil
}

type apiJob struct {
	Name               string
	Labels             []string
	Runner             string `json:"runner_name"`
	Status, Conclusion string
}

var errPending = errors.New("official job or ephemeral registration is not terminal yet")

func checkJob(p Plan, j apiJob, finished bool) error {
	if j.Name != p.JobName || len(j.Labels) != 1 || j.Labels[0] != p.Label || (j.Runner != "" && j.Runner != p.name()) {
		return errors.New("actual job route/runner rejected")
	}
	if finished {
		if j.Status == "queued" || j.Status == "in_progress" || j.Status == "waiting" {
			return errPending
		}
		if j.Runner != p.name() || j.Status != "completed" || j.Conclusion != "success" {
			return errors.New("official target job did not complete successfully on this runner")
		}
	}
	return nil
}
