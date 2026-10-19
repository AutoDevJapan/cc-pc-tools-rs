package contract

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func testPlan() Plan {
	h := strings.Repeat("a", 40)
	return Plan{Version: 1, RepoID: RepoID, Head: h, Tree: strings.Repeat("b", 40), Event: "push", Branch: "main", ContextSHA: h, WorkflowSHA: h, WorkflowRef: Repository + "/" + Workflow + "@refs/heads/main", Workflow: Workflow, WorkflowDigest: Digest(DesktopWorkflow), RunID: 100, Attempt: 1, JobID: 200, Job: "windows", JobName: "Windows build"}
}
func testAPI(p Plan) map[string]any {
	r := map[string]any{"id": RepoID, "full_name": Repository, "owner": map[string]any{"login": "AutoDevJapan"}, "private": false, "visibility": "public", "fork": false, "default_branch": "main", "permissions": map[string]any{"admin": true}}
	j := map[string]any{"id": p.JobID, "run_id": p.RunID, "run_attempt": p.Attempt, "name": p.JobName, "status": "completed", "conclusion": "success", "runner_name": "unique-runner", "labels": Labels(p), "steps": []any{map[string]any{"name": "test", "status": "completed", "conclusion": "success"}}}
	return map[string]any{
		fmt.Sprintf("repositories/%d", RepoID):                                                                r,
		"repos/" + Repository + "/git/ref/heads/main":                                                         map[string]any{"object": map[string]any{"sha": p.Head}},
		"repos/" + Repository + "/git/commits/" + p.Head:                                                      map[string]any{"sha": p.Head, "tree": map[string]any{"sha": p.Tree}},
		"repos/" + Repository + "/contents/" + Workflow + "?ref=" + p.Head:                                    map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(DesktopWorkflow)},
		fmt.Sprintf("repos/%s/actions/runs/%d", Repository, p.RunID):                                          map[string]any{"id": p.RunID, "run_attempt": p.Attempt, "head_sha": p.Head, "head_branch": p.Branch, "path": Workflow, "event": p.Event, "status": "completed", "repository": r, "head_repository": r},
		fmt.Sprintf("repos/%s/actions/jobs/%d", Repository, p.JobID):                                          j,
		fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100", Repository, p.RunID, p.Attempt): map[string]any{"total_count": 2, "jobs": []any{j, map[string]any{"id": 201, "run_id": p.RunID, "run_attempt": p.Attempt, "name": "Desktop admission", "status": "completed", "conclusion": "success", "labels": []string{"ubuntu-latest"}}}},
	}
}
func reader(m map[string]any) ReadAPI {
	return func(_ context.Context, path string, v any) error {
		a, ok := m[path]
		if !ok {
			return fmt.Errorf("unexpected endpoint %s", path)
		}
		b, e := json.Marshal(a)
		if e != nil {
			return e
		}
		return json.Unmarshal(b, v)
	}
}
func TestOfficialSuccessRequiresPinnedAssignment(t *testing.T) {
	p := testPlan()
	m := testAPI(p)
	if e := RequireSuccess(context.Background(), p, "unique-runner", reader(m)); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		name, endpoint, key string
		value               any
	}{
		{"wrong owner", fmt.Sprintf("repositories/%d", RepoID), "owner", map[string]any{"login": "other"}},
		{"private changed", fmt.Sprintf("repositories/%d", RepoID), "private", true},
		{"wrong repo", fmt.Sprintf("repositories/%d", RepoID), "id", 1},
		{"fork", fmt.Sprintf("repositories/%d", RepoID), "fork", true},
		{"obsolete attempt", fmt.Sprintf("repos/%s/actions/runs/%d", Repository, p.RunID), "run_attempt", 2},
		{"foreign source", fmt.Sprintf("repos/%s/actions/runs/%d", Repository, p.RunID), "head_sha", strings.Repeat("c", 40)},
		{"other runner", fmt.Sprintf("repos/%s/actions/jobs/%d", Repository, p.JobID), "runner_name", "other"},
		{"actual failure", fmt.Sprintf("repos/%s/actions/jobs/%d", Repository, p.JobID), "conclusion", "failure"},
		{"empty steps", fmt.Sprintf("repos/%s/actions/jobs/%d", Repository, p.JobID), "steps", []any{}},
		{"incomplete jobs", fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100", Repository, p.RunID, p.Attempt), "total_count", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := testAPI(p)
			m[tc.endpoint].(map[string]any)[tc.key] = tc.value
			if RequireSuccess(context.Background(), p, "unique-runner", reader(m)) == nil {
				t.Fatal("unsafe success accepted")
			}
		})
	}
}
func TestRejectPRAndUnreviewedDispatch(t *testing.T) {
	for _, event := range []string{"pull_request", "pull_request_target", "workflow_run", "schedule", "dynamic"} {
		p := testPlan()
		p.Event = event
		if p.Validate() == nil {
			t.Fatalf("%s admitted", event)
		}
	}
	p := testPlan()
	p.Event = "workflow_dispatch"
	p.Branch = "foreign"
	if p.Validate() == nil {
		t.Fatal("unreviewed branch admitted")
	}
	p.Branch = CandidateBranch
	p.WorkflowRef = Repository + "/" + Workflow + "@" + p.Ref()
	if e := p.Validate(); e != nil {
		t.Fatal(e)
	}
}
func TestActualHookContext(t *testing.T) {
	p := testPlan()
	env := map[string]string{"GITHUB_REPOSITORY": Repository, "GITHUB_REPOSITORY_ID": "1131498455", "GITHUB_SHA": p.Head, "GITHUB_RUN_ID": "100", "GITHUB_RUN_ATTEMPT": "1", "GITHUB_JOB": "windows", "RUNNER_OS": "Windows", "GITHUB_EVENT_NAME": "push", "GITHUB_REF": p.Ref(), "GITHUB_WORKFLOW_SHA": p.Head, "GITHUB_WORKFLOW_REF": p.WorkflowRef}
	raw, _ := json.Marshal(map[string]any{"repository": map[string]any{"id": RepoID, "full_name": Repository, "fork": false}, "ref": p.Ref(), "after": p.Head})
	if e := CheckEvent(p, env, raw); e != nil {
		t.Fatal(e)
	}
	for k, v := range env {
		env[k] = "wrong"
		if CheckEvent(p, env, raw) == nil {
			t.Fatalf("bad %s accepted", k)
		}
		env[k] = v
	}
}
