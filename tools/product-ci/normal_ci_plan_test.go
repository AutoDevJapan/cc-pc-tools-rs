package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// This is a synthetic workflow, not product source or an executable fixture.
func normalCIFixture() []byte {
	var b strings.Builder
	b.WriteString("# 日本語の説明はbyte offsetを変えても保持する\r\nname: synthetic\r\non: [workflow_dispatch]\r\npermissions:\r\n  contents: read\r\njobs:\r\n")
	for _, route := range normalCIRoutes() {
		fmt.Fprintf(&b, "  %s:\r\n    runs-on: %s # unchanged comment\r\n    timeout-minutes: 20\r\n    if: always()\r\n    steps:\r\n", route.Job, route.Previous)
		if route.OS == "Windows" {
			for i := 0; i < 13; i++ {
				fmt.Fprintf(&b, "      - run: cargo test --locked -- --test-threads=1 # %d\r\n", i)
			}
		} else {
			b.WriteString("      - run: echo ubuntu-latest # not a route\r\n")
		}
	}
	return []byte(b.String())
}

func TestNormalCIProposalChangesOnlyFourRunnerScalars(t *testing.T) {
	input := normalCIFixture()
	output, e := proposeNormalCIRoute(input, digest(input))
	if e != nil {
		t.Fatal(e)
	}
	// Reconstruct input from the output tokens, independently of YAML offsets.
	restored := append([]byte(nil), output...)
	for _, r := range normalCIRoutes() {
		want := []byte("runs-on: " + normalCIRunnerExpression(r) + " # unchanged comment")
		restored = bytes.ReplaceAll(restored, want, []byte("runs-on: "+r.Previous+" # unchanged comment"))
	}
	if !bytes.Equal(input, restored) || bytes.Count(output, []byte("echo ubuntu-latest")) != 3 {
		t.Fatal("non-route bytes changed")
	}
	root, e := normalCIYAML(output)
	if e != nil {
		t.Fatal(e)
	}
	if count, e := countWindowsCargo(root); e != nil || count != 13 {
		t.Fatal("Windows checks changed")
	}
}

func TestNormalCIRejectsAmbiguousOrChangedWorkflow(t *testing.T) {
	input := normalCIFixture()
	if _, e := proposeNormalCIRoute(append(input, '\n'), digest(input)); e == nil {
		t.Fatal("unreviewed workflow bytes accepted")
	}
	for name, data := range map[string][]byte{
		"duplicate-job":      bytes.Replace(input, []byte("  ci-gate:"), []byte("  verify:"), 1),
		"duplicate-route":    bytes.Replace(input, []byte("    runs-on: ubuntu-latest"), []byte("    runs-on: invalid\n    runs-on: ubuntu-latest"), 1),
		"alias":              bytes.Replace(input, []byte("runs-on: ubuntu-latest"), []byte("runs-on: &runner ubuntu-latest"), 1),
		"multiple-documents": append(append([]byte(nil), input...), []byte("\n---\nname: extra\n")...),
		"foreign-os":         bytes.Replace(input, []byte("windows-2025"), []byte("ubuntu-latest"), 1),
		"extra-job":          append(append([]byte(nil), input...), []byte("  extra:\n    runs-on: ubuntu-latest\n")...),
	} {
		t.Run(name, func(t *testing.T) {
			if _, e := proposeNormalCIRoute(data, digest(data)); e == nil {
				t.Fatal("ambiguous or changed route accepted")
			}
		})
	}
	root, e := normalCIYAML(bytes.Replace(input, []byte("cargo test"), []byte("echo skipped"), 1))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = countWindowsCargo(root); e == nil {
		t.Fatal("dropped Windows command accepted")
	}
}

func normalCIAPI(t *testing.T, fault string) api {
	t.Helper()
	c := candidates[2]
	const repoName = "AutoDevJapan/opaque-normal-ci"
	const branch = "codex/todo2641-http-response-pseudo-20261015"
	repo := map[string]any{"id": c.ID, "full_name": repoName, "private": true, "fork": false, "owner": map[string]any{"login": "AutoDevJapan"}, "permissions": map[string]any{"admin": true}}
	pr := map[string]any{"state": "open", "head": map[string]any{"sha": c.SHA, "ref": branch, "repo": repo}, "base": map[string]any{"repo": repo}}
	ref := map[string]any{"object": map[string]any{"sha": c.SHA}}
	run := map[string]any{"id": normalCIFailedRun, "run_attempt": 1, "path": ".github/workflows/ci.yml", "event": "pull_request", "head_sha": c.SHA, "head_branch": branch, "status": "completed", "conclusion": "failure", "repository": repo, "head_repository": repo}
	jobs := []map[string]any{}
	for _, j := range []struct {
		id         int64
		conclusion string
	}{{116948883594, "failure"}, {116948895629, "failure"}, {116948897372, "skipped"}, {116948897826, "skipped"}} {
		jobs = append(jobs, map[string]any{"id": j.id, "run_id": normalCIFailedRun, "status": "completed", "conclusion": j.conclusion, "runner_id": 0, "steps": []any{}})
	}
	annotation := []map[string]any{{"annotation_level": "failure", "message": "The job was not started because an Actions budget is preventing further use."}}
	data := map[string]any{
		fmt.Sprintf("repositories/%d", c.ID):                                                              repo,
		fmt.Sprintf("repos/%s/pulls/%d", repoName, c.PR):                                                  pr,
		"repos/" + repoName + "/git/ref/heads/" + url.PathEscape(branch):                                  ref,
		"repos/" + repoName + "/git/commits/" + c.SHA:                                                     map[string]any{"tree": map[string]any{"sha": "f7eea82c96449b7749f98dd6c4b0344b64f1d651"}},
		"repos/" + repoName + "/contents/.github/workflows/ci.yml?ref=" + c.SHA:                           map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(normalCIFixture())},
		fmt.Sprintf("repos/%s/actions/runs/%d", repoName, normalCIFailedRun):                              run,
		fmt.Sprintf("repos/%s/actions/runs/%d/attempts/1/jobs?per_page=100", repoName, normalCIFailedRun): map[string]any{"total_count": 4, "jobs": jobs},
		"repos/" + repoName + "/check-runs/116948883594/annotations?per_page=100":                         annotation,
		"repos/" + repoName + "/check-runs/116948895629/annotations?per_page=100":                         annotation,
	}
	switch fault {
	case "public":
		repo["private"] = false
	case "foreign-owner":
		repo["owner"] = map[string]any{"login": "Other"}
	case "advanced-head":
		ref["object"] = map[string]any{"sha": strings.Repeat("a", 40)}
	case "different-run-head":
		run["head_sha"] = strings.Repeat("a", 40)
	case "wrong-event":
		run["event"] = "workflow_dispatch"
	case "wrong-run-repo":
		run["head_repository"] = map[string]any{"id": 99, "full_name": "Other/other"}
	case "started-job":
		jobs[0]["runner_id"] = 42
	case "duplicate-job":
		jobs[1]["id"] = jobs[0]["id"]
	case "job-with-steps":
		jobs[0]["steps"] = []any{map[string]any{"number": 1}}
	case "other-failure":
		annotation[0]["message"] = "A test failed"
	}
	repositoryReads := 0
	return func(_ context.Context, key string, out any) error {
		if strings.HasPrefix(key, "repositories/") {
			repositoryReads++
			if fault == "head-advanced-during-inspection" && repositoryReads == 2 {
				ref["object"] = map[string]any{"sha": strings.Repeat("a", 40)}
			}
		}
		value, ok := data[key]
		if !ok {
			return errors.New("unexpected endpoint; fixture admits only fixed reads")
		}
		b, e := json.Marshal(value)
		if e != nil {
			return e
		}
		return json.Unmarshal(b, out)
	}
}

func TestNormalCIInspectionKeepsUnstartedFailureSeparateFromSuccess(t *testing.T) {
	c := candidates[2]
	expected := digest(normalCIFixture())
	p, e := inspectNormalCIWithPolicy(context.Background(), c, normalCIAPI(t, ""), expected)
	if e != nil {
		t.Fatal(e)
	}
	if !p.ProposedOnly || p.AllChecksSuccess || !p.SourceRevalidated || p.WindowsCommands != 13 || len(p.Jobs) != 4 || len(p.BudgetChecks) != 2 || p.ProposedWorkflowSHA == p.WorkflowSHA {
		t.Fatal("proposal misrepresented as executed or verified CI")
	}
	for _, fault := range []string{"public", "foreign-owner", "advanced-head", "different-run-head", "wrong-event", "wrong-run-repo", "started-job", "duplicate-job", "job-with-steps", "other-failure", "head-advanced-during-inspection"} {
		t.Run(fault, func(t *testing.T) {
			if _, e := inspectNormalCIWithPolicy(context.Background(), c, normalCIAPI(t, fault), expected); e == nil {
				t.Fatal("changed fixed identity or failure accepted")
			}
		})
	}
}
