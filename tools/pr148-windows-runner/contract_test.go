package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func fixturePlan() Plan {
	id := strings.Repeat("a", 32)
	return Plan{Instance: id, Root: filepath.Join(os.TempDir(), "codex-owned-"+id), Repository: repository, RepositoryID: repositoryID, PR: 148, Head: strings.Repeat("b", 40), Tree: strings.Repeat("e", 40), Base: baseHead, Branch: branch, RunID: 12, JobID: 13, Attempt: 2, Job: "test", JobName: "test (windows-latest)", Workflow: ".github/workflows/rust.yml", PRHead: strings.Repeat("b", 40), WorkflowSHA: strings.Repeat("b", 40), WorkflowDigest: strings.Repeat("c", 64), Label: routeLabel, RunnerVersion: "2.330.0", RunnerDigest: strings.Repeat("d", 64), ControllerSID: "S-1-5-21-1-2-3-1001", Seconds: 60}
}
func TestContractAndFixedScope(t *testing.T) {
	p := fixturePlan()
	if e := p.validate(); e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*Plan){func(p *Plan) { p.Repository = "another/repo" }, func(p *Plan) { p.RepositoryID++ }, func(p *Plan) { p.PR++ }, func(p *Plan) { p.Head = strings.Repeat("e", 40) }, func(p *Plan) { p.Tree = sourceTree }, func(p *Plan) { p.Base = strings.Repeat("e", 40) }, func(p *Plan) { p.Branch = "main" }, func(p *Plan) { p.Label = "self-hosted" }, func(p *Plan) { p.Attempt = 0 }, func(p *Plan) { p.Seconds = 0 }, func(p *Plan) { p.Seconds = 14401 }, func(p *Plan) { p.Job = "arbitrary" }, func(p *Plan) { p.Workflow = ".github/workflows/custom.yml" }, func(p *Plan) { p.ControllerSID = "S-1-5-18" }, func(p *Plan) { p.RunnerDigest = "" }, func(p *Plan) { p.Root = filepath.Dir(p.Root) }, func(p *Plan) { p.Fixture = "register" }} {
		q := p
		mutate(&q)
		if q.validate() == nil {
			t.Fatalf("accepted invalid plan %+v", q)
		}
	}
	for _, mode := range []string{"normal", "cancel"} {
		q := p
		q.Fixture = mode
		q.Head = sourceHead
		q.Tree = sourceTree
		q.RunID = 0
		q.RunnerDigest = ""
		if e := q.validate(); e != nil {
			t.Fatal(e)
		}
	}
}
func TestRunnerConfigurationCannotExpand(t *testing.T) {
	p := fixturePlan()
	a := configureArgs(p)
	want := []string{"configure", "--unattended", "--url", "https://github.com/" + repository, "--name", p.name(), "--labels", routeLabel, "--ephemeral", "--no-default-labels", "--disableupdate", "--work", "_work"}
	if !reflect.DeepEqual(a, want) {
		t.Fatalf("unexpected registration contract %v", a)
	}
}
func contextFixture(p Plan) (map[string]string, []byte) {
	env := map[string]string{"GITHUB_REPOSITORY": repository, "GITHUB_REPOSITORY_ID": fmt.Sprint(repositoryID), "GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_SHA": p.Head, "RUNNER_OS": "Windows", "GITHUB_HEAD_REF": "", "GITHUB_BASE_REF": "", "GITHUB_REF": "refs/heads/" + branch, "GITHUB_RUN_ID": fmt.Sprint(p.RunID), "GITHUB_RUN_ATTEMPT": fmt.Sprint(p.Attempt), "GITHUB_JOB": p.Job, "GITHUB_WORKFLOW_SHA": p.WorkflowSHA, "GITHUB_WORKFLOW_REF": repository + "/" + p.Workflow + "@refs/heads/" + branch, "RUNNER_NAME": p.name()}
	repo := map[string]any{"id": repositoryID, "full_name": repository}
	ev := map[string]any{"ref": "refs/heads/" + branch, "repository": repo}
	b, _ := json.Marshal(ev)
	return env, b
}
func TestHookAcceptsExactJobAndRejectsEveryContextChange(t *testing.T) {
	p := fixturePlan()
	env, b := contextFixture(p)
	if e := checkEvent(p, env, b); e != nil {
		t.Fatal(e)
	}
	for k, orig := range env {
		env[k] = "different"
		if checkEvent(p, env, b) == nil {
			t.Fatalf("accepted changed %s", k)
		}
		env[k] = orig
		if orig == "" {
			continue
		}
		delete(env, k)
		if checkEvent(p, env, b) == nil {
			t.Fatalf("accepted absent %s", k)
		}
		env[k] = orig
	}
	for _, raw := range []string{`{}`, `{"number":149}`, strings.ReplaceAll(string(b), repository, "someone/fork"), strings.ReplaceAll(string(b), branch, "main"), string(b) + `{}`} {
		if checkEvent(p, env, []byte(raw)) == nil {
			t.Fatal("accepted wrong event")
		}
	}
}
func TestContainmentAndInputBound(t *testing.T) {
	p := fixturePlan()
	for _, path := range []string{p.Root, filepath.Dir(p.Root), p.Root + "-other"} {
		if beneath(p.Root, path) == nil {
			t.Fatal("accepted outside path")
		}
	}
	if e := beneath(p.Root, filepath.Join(p.Root, "runtime", "source")); e != nil {
		t.Fatal(e)
	}
	if _, e := boundedRead(strings.NewReader("12345"), 4); e == nil {
		t.Fatal("unbounded input")
	}
	if b, e := boundedRead(strings.NewReader("1234"), 4); e != nil || string(b) != "1234" {
		t.Fatal("bounded input rejected")
	}
}

func TestCleanupDeadlineContract(t *testing.T) {
	for _, n := range []int{0, 60, 1800, 14400} {
		p := fixturePlan()
		p.CleanupSeconds = n
		if e := p.validate(); e != nil {
			t.Fatalf("valid cleanup budget %d: %v", n, e)
		}
	}
	for _, n := range []int{-1, 1, 59, 14401} {
		p := fixturePlan()
		p.CleanupSeconds = n
		if p.validate() == nil {
			t.Fatalf("unbounded or too short cleanup budget %d accepted", n)
		}
	}
}
