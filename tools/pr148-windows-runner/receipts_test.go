package main

import (
	"encoding/json"
	"testing"
)

func TestAllHookEvidenceBindsExactExecution(t *testing.T) {
	p := fixturePlan()
	for _, stage := range []string{"hook-start", "verify-source", "hook-completed"} {
		r := makeHookReceipt(p, stage, []byte("credential-free event fixture"))
		b, _ := json.Marshal(r)
		if _, e := validateHookData(p, stage, b); e != nil {
			t.Fatal(e)
		}
		for _, bad := range []func(*hookReceipt){func(r *hookReceipt) { r.RunID++ }, func(r *hookReceipt) { r.JobID++ }, func(r *hookReceipt) { r.Attempt++ }, func(r *hookReceipt) { r.Job = "other" }, func(r *hookReceipt) { r.Stage = "missing" }, func(r *hookReceipt) { r.Instance = "other" }, func(r *hookReceipt) { r.PRHead = "other" }, func(r *hookReceipt) { r.Head = "other" }, func(r *hookReceipt) { r.Tree = "other" }, func(r *hookReceipt) { r.WorkflowDigest = "other" }, func(r *hookReceipt) { r.WorkflowSHA = "other" }, func(r *hookReceipt) { r.Source = "other" }, func(r *hookReceipt) { r.Target = "other" }, func(r *hookReceipt) { r.SourceVerified = false }, func(r *hookReceipt) { r.EventDigest = "" }} {
			q := r
			bad(&q)
			raw, _ := json.Marshal(q)
			if _, e := validateHookData(p, stage, raw); e == nil {
				t.Fatal("changed hook evidence accepted")
			}
		}
		if _, e := validateHookData(p, stage, []byte(`{}`)); e == nil {
			t.Fatal("missing hook accepted")
		}
	}
}
func TestPostAuditRequiresActualSuccessfulRunner(t *testing.T) {
	p := fixturePlan()
	j := apiJob{ID: p.JobID, RunID: p.RunID, Attempt: p.Attempt, Name: p.JobName, Labels: []string{p.Label}, Runner: p.name(), Status: "completed", Conclusion: "success"}
	if e := checkJob(p, j, true); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*apiJob){func(j *apiJob) { j.ID++ }, func(j *apiJob) { j.RunID++ }, func(j *apiJob) { j.Attempt++ }, func(j *apiJob) { j.Runner = "" }, func(j *apiJob) { j.Runner = "other" }, func(j *apiJob) { j.Status = "in_progress" }, func(j *apiJob) { j.Conclusion = "failure" }, func(j *apiJob) { j.Conclusion = "cancelled" }, func(j *apiJob) { j.Labels = []string{"self-hosted"} }, func(j *apiJob) { j.Name = "other" }} {
		q := j
		mutate(&q)
		if checkJob(p, q, true) == nil {
			t.Fatal("incomplete or wrong job accepted")
		}
	}
	j.Runner = ""
	j.Status = "queued"
	j.Conclusion = ""
	if e := checkJob(p, j, false); e != nil {
		t.Fatal("preflight queued job rejected", e)
	}
}
func TestCheckoutExecutionAndCredentialKeysRejected(t *testing.T) {
	for _, key := range []string{"http.https://github.com/.extraheader", "credential.helper", "url.x.insteadof", "core.fsmonitor", "core.hookspath", "core.sshcommand", "include.path", "includeif.gitdir:x.path", "remote.origin.pushurl", "filter.foo.process"} {
		if !blockedGitKey(key) {
			t.Fatal("unsafe key accepted", key)
		}
	}
	for _, key := range []string{"core.repositoryformatversion", "core.autocrlf", "remote.origin.url"} {
		if blockedGitKey(key) {
			t.Fatal("ordinary key rejected", key)
		}
	}
}
