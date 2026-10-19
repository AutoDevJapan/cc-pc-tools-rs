package main

import (
	contract "cc/civ6-ci-contract"
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
	head, ctx := strings.Repeat("b", 40), strings.Repeat("9", 40)
	return Plan{
		Plan: contract.Plan{Version: 1, RepoID: repositoryID, Head: head, Tree: strings.Repeat("e", 40), Event: "pull_request", Branch: "feature/windows-ci", PR: 150, ContextSHA: ctx, WorkflowSHA: ctx,
			WorkflowRef: repository + "/.github/workflows/rust.yml@refs/pull/150/merge", Workflow: ".github/workflows/rust.yml", WorkflowDigest: strings.Repeat("c", 64), RunID: 12, Attempt: 2, JobID: 13, Job: "test", JobName: "test (windows-latest)"},
		Instance: id, Root: filepath.Join(os.TempDir(), "codex-owned-"+id), RunnerVersion: "2.330.0", RunnerDigest: strings.Repeat("d", 64), ControllerSID: "S-1-5-21-1-2-3-1001", Seconds: 60}
}

// pushPlan is the normal-continuation shape: a main push with its own workflow.
func pushPlan(workflow, job string) Plan {
	p := fixturePlan()
	p.Event, p.Branch, p.PR, p.ContextSHA = "push", "main", 0, p.Head
	p.WorkflowSHA = p.Head
	p.Workflow, p.Job, p.JobName = workflow, job, job
	if job == "test" {
		p.JobName = "test (windows-latest)"
	}
	p.WorkflowRef = repository + "/" + workflow + "@refs/heads/main"
	return p
}

func TestPlanJSONIsOneFlatDocumentWithUnknownFieldsRejected(t *testing.T) {
	p := fixturePlan()
	b, e := json.Marshal(p)
	if e != nil {
		t.Fatal(e)
	}
	var flat map[string]any
	if json.Unmarshal(b, &flat) != nil {
		t.Fatal("not an object")
	}
	for _, k := range []string{"version", "repo_id", "head", "tree", "event", "branch", "pr", "context_sha", "workflow_sha", "workflow_ref", "workflow", "workflow_digest", "run_id", "attempt", "job_id", "job", "job_name", "instance", "root", "runner_version", "runner_digest", "controller_sid", "seconds"} {
		if _, ok := flat[k]; !ok {
			t.Fatalf("missing shared/operational key %s", k)
		}
	}
	if _, nested := flat["Plan"]; nested {
		t.Fatal("contract plan must be flattened")
	}
	dir := t.TempDir()
	q := p
	q.Root = filepath.Join(dir, "codex-owned-"+p.Instance)
	if os.Mkdir(q.Root, 0700) != nil {
		t.Fatal("mkdir")
	}
	b, _ = json.Marshal(q)
	if e := os.WriteFile(filepath.Join(q.Root, "plan.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	got, e := readPlan(q.Root)
	if e != nil || !reflect.DeepEqual(got, q) {
		t.Fatalf("round trip failed: %v", e)
	}
	extra := strings.Replace(string(b), "{", `{"surprise":1,`, 1)
	os.Remove(filepath.Join(q.Root, "plan.json"))
	if e := os.WriteFile(filepath.Join(q.Root, "plan.json"), []byte(extra), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := readPlan(q.Root); e == nil {
		t.Fatal("unknown plan field accepted")
	}
}

func TestContractAndFixedScope(t *testing.T) {
	p := fixturePlan()
	if e := p.validate(); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ w, j string }{{".github/workflows/rust.yml", "test"}, {".github/workflows/native-driver.yml", "native-driver"}, {".github/workflows/live-chain.yml", "live-chain"}} {
		if e := pushPlan(tc.w, tc.j).validate(); e != nil {
			t.Fatalf("%s: %v", tc.w, e)
		}
	}
	for name, mutate := range map[string]func(*Plan){
		"repo id":         func(p *Plan) { p.RepoID++ },
		"head short":      func(p *Plan) { p.Head = "abc" },
		"original head":   func(p *Plan) { p.Head = fixtureHead },
		"original tree":   func(p *Plan) { p.Tree = fixtureTree },
		"attempt":         func(p *Plan) { p.Attempt = 0 },
		"seconds low":     func(p *Plan) { p.Seconds = 0 },
		"seconds high":    func(p *Plan) { p.Seconds = 14401 },
		"arbitrary job":   func(p *Plan) { p.Job = "arbitrary" },
		"custom workflow": func(p *Plan) { p.Workflow = ".github/workflows/custom.yml" },
		"ubuntu job":      func(p *Plan) { p.JobName = "test (ubuntu-latest)" },
		"codeql": func(p *Plan) {
			p.Event, p.Workflow, p.Job, p.JobName = "dynamic", contract.CodeQLWorkflow, "analyze", "Analyze (rust)"
		},
		"native job name": func(p *Plan) {
			p.Workflow, p.Job, p.JobName = ".github/workflows/native-driver.yml", "native-driver", "native"
		},
		"fork-like event":  func(p *Plan) { p.Event = "pull_request_target" },
		"pr zero":          func(p *Plan) { p.PR = 0 },
		"push on branch":   func(p *Plan) { p.Event, p.PR, p.ContextSHA, p.WorkflowSHA = "push", 0, p.Head, p.Head },
		"context mismatch": func(p *Plan) { p.WorkflowSHA = strings.Repeat("1", 40) },
		"digest":           func(p *Plan) { p.WorkflowDigest = "" },
		"controller sid":   func(p *Plan) { p.ControllerSID = "S-1-5-18" },
		"runner digest":    func(p *Plan) { p.RunnerDigest = "" },
		"runner version":   func(p *Plan) { p.RunnerVersion = "latest" },
		"root":             func(p *Plan) { p.Root = filepath.Dir(p.Root) },
		"unknown fixture":  func(p *Plan) { p.Fixture = "register" },
	} {
		q := p
		mutate(&q)
		if q.validate() == nil {
			t.Fatalf("accepted invalid plan: %s", name)
		}
	}
	for _, mode := range []string{"normal", "cancel"} {
		q := p
		q.Fixture = mode
		q.Head = fixtureHead
		q.Tree = fixtureTree
		q.RunID = 0
		q.RunnerDigest = ""
		if e := q.validate(); e != nil {
			t.Fatal(e)
		}
		q.RuntimeFixture = false
		q.Head = strings.Repeat("b", 40)
		if q.validate() == nil {
			t.Fatal("fixture must retain the original source pins")
		}
	}
	q := p
	q.RuntimeFixture = true
	if q.validate() == nil {
		t.Fatal("runtime fixture cannot authorize an official runner")
	}
}

func TestRunnerConfigurationCannotExpand(t *testing.T) {
	p := fixturePlan()
	want := []string{"configure", "--unattended", "--url", "https://github.com/" + repository, "--name", p.name(), "--labels", contract.WindowsLabel, "--ephemeral", "--disableupdate", "--work", "_work"}
	if a := configureArgs(p); !reflect.DeepEqual(a, want) {
		t.Fatalf("unexpected registration contract %v", a)
	}
	if !reflect.DeepEqual(p.requiredLabels(), []string{"self-hosted", "Windows", "X64", contract.WindowsLabel}) {
		t.Fatalf("labels %v", p.requiredLabels())
	}
}

func eventFixture(p Plan) (map[string]string, []byte) {
	env := map[string]string{"GITHUB_REPOSITORY": repository, "GITHUB_REPOSITORY_ID": fmt.Sprint(repositoryID), "GITHUB_EVENT_NAME": p.Event, "GITHUB_SHA": p.ContextSHA, "RUNNER_OS": "Windows", "GITHUB_HEAD_REF": "", "GITHUB_BASE_REF": "", "GITHUB_REF": p.Ref(), "GITHUB_RUN_ID": fmt.Sprint(p.RunID), "GITHUB_RUN_ATTEMPT": fmt.Sprint(p.Attempt), "GITHUB_JOB": p.Job, "GITHUB_WORKFLOW_SHA": p.WorkflowSHA, "GITHUB_WORKFLOW_REF": p.WorkflowRef, "RUNNER_NAME": p.name()}
	repo := map[string]any{"id": repositoryID, "full_name": repository, "fork": false}
	ev := map[string]any{"repository": repo}
	switch p.Event {
	case "pull_request":
		env["GITHUB_HEAD_REF"], env["GITHUB_BASE_REF"] = p.Branch, "main"
		ev["number"] = p.PR
		ev["pull_request"] = map[string]any{"head": map[string]any{"sha": p.Head, "ref": p.Branch, "repo": repo}, "base": map[string]any{"ref": "main", "repo": repo}}
	case "push":
		ev["ref"], ev["after"] = p.Ref(), p.Head
	}
	b, _ := json.Marshal(ev)
	return env, b
}

// The pull-request merge context (GITHUB_SHA) is not the tested source (Head);
// the hook must accept that exact split and reject a swap of the two.
func TestHookAcceptsExactJobAndRejectsEveryContextChange(t *testing.T) {
	for _, p := range []Plan{fixturePlan(), pushPlan(".github/workflows/live-chain.yml", "live-chain")} {
		env, b := eventFixture(p)
		if e := checkEvent(p, env, b); e != nil {
			t.Fatalf("%s: %v", p.Event, e)
		}
		for k, orig := range env {
			env[k] = "different"
			if checkEvent(p, env, b) == nil {
				t.Fatalf("%s accepted changed %s", p.Event, k)
			}
			env[k] = orig
			if orig == "" {
				continue
			}
			delete(env, k)
			if checkEvent(p, env, b) == nil {
				t.Fatalf("%s accepted absent %s", p.Event, k)
			}
			env[k] = orig
		}
		for _, raw := range []string{`{}`, `{"number":149}`, strings.ReplaceAll(string(b), repository, "someone/fork"), strings.ReplaceAll(string(b), p.Head, strings.Repeat("7", 40)), string(b) + `{}`} {
			if checkEvent(p, env, []byte(raw)) == nil {
				t.Fatalf("%s accepted wrong event %s", p.Event, raw)
			}
		}
	}
	p := fixturePlan()
	env, b := eventFixture(p)
	env["GITHUB_SHA"] = p.Head
	if checkEvent(p, env, b) == nil {
		t.Fatal("PR head accepted as the merge context")
	}
	f := p
	f.Fixture = "normal"
	if checkEvent(f, env, b) == nil {
		t.Fatal("fixture reached the official hook")
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

func TestOneFixedServiceIdentityPerKindWhileRunnerNamesStayPerJob(t *testing.T) {
	a, b := fixturePlan(), fixturePlan()
	b.Instance = strings.Repeat("b", 32)
	if a.svc() != sharedService || b.svc() != sharedService || a.svc() != b.svc() {
		t.Fatal("jobs must reuse the one repository identity")
	}
	if a.name() == b.name() || a.name() == a.svc() {
		t.Fatal("runner registration must stay unique per job and distinct from the service")
	}
	f := a
	f.Fixture = "normal"
	if f.svc() != fixtureService || f.svc() == sharedService {
		t.Fatal("credential-free fixtures must not touch the real identity")
	}
	for _, id := range []string{a.svc(), f.svc()} {
		if !strings.HasPrefix(id, "CC-CIW-") || len(id) > 40 {
			t.Fatalf("service id %q", id)
		}
	}
}

func TestImageRootParsesWindowsArgumentsStrictly(t *testing.T) {
	id := strings.Repeat("a", 32)
	quote := func(s string) string {
		if strings.Contains(s, " ") {
			return `"` + s + `"`
		}
		return s
	}
	for _, parent := range []string{`C:\Users\x`, `C:\Users\a b`, `C:\ci service zone`, `D:\service \x`} {
		root := parent + `\codex-owned-` + id
		image := quote(root+`\helper.exe`) + " service " + quote(root)
		got, e := imageRoot(image)
		if e != nil || got != root {
			t.Fatalf("parent %q: got %q err %v", parent, got, e)
		}
	}
	root := `C:\x\codex-owned-` + id
	for _, bad := range []string{
		"", "service", `C:\x\other.exe run`,
		root + `\helper.exe service ` + root + " extra",
		`"` + root + `\helper.exe service ` + root,
		root + `\helper.exe run ` + root,
		`C:\elsewhere\helper.exe service ` + root,
		root + `\helper.exe service C:\x\notowned`,
		root + `\helper.exe service codex-owned-` + id,
	} {
		if got, e := imageRoot(bad); e == nil {
			t.Fatalf("accepted %q as %q", bad, got)
		}
	}
}

func TestSharedIdentityMovesOnlyBehindDurableCollectionProof(t *testing.T) {
	prev, cur := `C:\ci\codex-owned-`+strings.Repeat("a", 32), `C:\ci\codex-owned-`+strings.Repeat("b", 32)
	if previousRootSettled("", cur, "") != nil || previousRootSettled(cur, cur, "") != nil {
		t.Fatal("first use or the same root rejected")
	}
	if previousRootSettled(prev, cur, "") == nil {
		t.Fatal("an uncollected predecessor was accepted (no marker)")
	}
	if previousRootSettled(prev, cur, `C:\ci\codex-owned-`+strings.Repeat("c", 32)) == nil {
		t.Fatal("another job's marker was accepted for this predecessor")
	}
	// The proof does not depend on the old path: a deleted, kept or quarantined
	// (moved) root is equally fine once it was collected.
	if e := previousRootSettled(prev, cur, strings.ToUpper(prev)); e != nil {
		t.Fatal(e)
	}
}
