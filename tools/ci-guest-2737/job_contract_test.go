package main

import (
	"strings"
	"testing"
)

func TestLiveJobContextRejectsSourceInputAndRoleSubstitution(t *testing.T) {
	instance := strings.Repeat("a", 32)
	p := linuxJobPlan{Source: fixedSourcePin(), RunID: 17, JobID: 29, Attempt: 1, Jobs: 8, Threads: 8, Name: "todo2737-" + instance, Label: "cc-product-2737-1154066087-linux-verify-17", Role: "verify", Inputs: map[string]string{}}
	if !validLinuxJob(p, p.Source, instance) {
		t.Fatal("valid pinned job rejected")
	}
	other := p
	other.RunID++
	if validLinuxJob(other, other.Source, instance) {
		t.Fatal("another run can claim the same runner label")
	}
	other = p
	other.Role = "ci-gate"
	if validLinuxJob(other, other.Source, instance) {
		t.Fatal("another job role can claim the same runner label")
	}
	c := jobHookContext{Stage: "start", Repository: "AutoDevJapan/GoDD-MCP-Alpha", SHA: p.Source.HeadSHA, WorkflowSHA: p.Source.HeadSHA, Ref: "refs/heads/codex/todo2641-http-response-pseudo-20261015", Event: "workflow_dispatch", RunID: "17", Attempt: "1", Job: "verify", Name: p.Name, Inputs: map[string]string{}}
	if validateJobContext(p, c) != nil {
		t.Fatal("valid official context rejected")
	}
	for _, change := range []func(*jobHookContext){func(c *jobHookContext) { c.SHA = strings.Repeat("b", 40) }, func(c *jobHookContext) { c.Job = "ci-gate" }, func(c *jobHookContext) { c.RunID = "18" }, func(c *jobHookContext) { c.Name = "other" }, func(c *jobHookContext) { c.Inputs = map[string]string{"release_source_sha": strings.Repeat("b", 40)} }} {
		x := c
		change(&x)
		if validateJobContext(p, x) == nil {
			t.Fatal("substituted source/job/input accepted")
		}
	}
}
