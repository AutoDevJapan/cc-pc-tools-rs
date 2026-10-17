package normalci

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Explicit immutable expectations, selected by the controller. This resolver
// cannot advance a source, mutate a workflow, register a runner or start a job.
type Pin struct {
	Source            Candidate
	Tree, WorkflowSHA string
	RunID             int64
	Attempt           int
	Role              string
	RunnerID          int64
	RunnerName        string
}
type Binding struct {
	RepoID, RunID, JobID                             int64
	PR, Attempt, TimeoutMinutes                      int
	SHA, Tree, WorkflowSHA, Role, Label, OS, JobName string
}
type OfficialJob struct {
	ID, RunID, RunnerID      int64
	Name, Status, RunnerName string
	Labels                   []string
}

// Use API JSON names rather than letting underscores silently produce zeros.
func ReadJobs(ctx context.Context, endpoint string, read API) ([]OfficialJob, error) {
	var response struct {
		Total int `json:"total_count"`
		Jobs  []struct {
			ID           int64 `json:"id"`
			RunID        int64 `json:"run_id"`
			RunnerID     int64 `json:"runner_id"`
			Name, Status string
			RunnerName   string `json:"runner_name"`
			Labels       []string
		}
	}
	if e := read(ctx, endpoint, &response); e != nil {
		return nil, e
	}
	if response.Total < 1 || response.Total > 4 || response.Total != len(response.Jobs) {
		return nil, errors.New("normal CI finite job inventory rejected")
	}
	var out []OfficialJob
	for _, j := range response.Jobs {
		out = append(out, OfficialJob{j.ID, j.RunID, j.RunnerID, j.Name, j.Status, j.RunnerName, j.Labels})
	}
	return out, nil
}
func Revision(s string) bool {
	return len(s) == 40 && strings.Trim(s, "0123456789abcdef") == ""
}
func Plan(ctx context.Context, p Pin, read API) (Binding, error) {
	bad := func() (Binding, error) {
		return Binding{}, errors.New("normal CI job binding rejected")
	}
	if p.Source.ID != RepoID || p.Source.PR != 3613 || !Revision(p.Source.SHA) || !Revision(p.Tree) || len(p.WorkflowSHA) != 64 || strings.Trim(p.WorkflowSHA, "0123456789abcdef") != "" || p.RunID < 1 || p.Attempt < 1 || p.RunnerID < 0 || (p.RunnerID == 0 && p.RunnerName != "") {
		return bad()
	}
	r, e := Verify(ctx, p.Source, read)
	if e != nil {
		return Binding{}, e
	}
	if r.Owner.Login != "AutoDevJapan" || r.FullName != "AutoDevJapan/GoDD-MCP-Alpha" {
		return bad()
	}
	var routeSwitch struct{ Name, Value string }
	if read(ctx, "repos/"+r.FullName+"/actions/variables/"+RouteVariable, &routeSwitch) != nil || routeSwitch.Name != RouteVariable || routeSwitch.Value != "enabled" {
		return bad()
	}
	var pr Pull
	if read(ctx, fmt.Sprintf("repos/%s/pulls/3613", r.FullName), &pr) != nil || pr.Head.Ref != "codex/todo2641-http-response-pseudo-20261015" {
		return bad()
	}
	var run workflowRun
	if read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d", r.FullName, p.RunID), &run) != nil {
		return bad()
	}
	if run.ID != p.RunID || run.RunAttempt != p.Attempt || run.Path != ".github/workflows/ci.yml" || run.Event != "workflow_dispatch" || run.HeadSHA != p.Source.SHA || run.HeadBranch != pr.Head.Ref || run.Repository.ID != p.Source.ID || run.HeadRepo.ID != p.Source.ID || run.HeadRepo.Fork || run.Repository.FullName != r.FullName || run.HeadRepo.FullName != r.FullName || (run.Status != "queued" && run.Status != "in_progress" && run.Status != "waiting") {
		return bad()
	}
	var commit struct{ Tree struct{ SHA string } }
	if read(ctx, "repos/"+r.FullName+"/git/commits/"+p.Source.SHA, &commit) != nil || commit.Tree.SHA != p.Tree {
		return bad()
	}
	var content struct{ Encoding, Content string }
	if read(ctx, "repos/"+r.FullName+"/contents/.github/workflows/ci.yml?ref="+url.QueryEscape(p.Source.SHA), &content) != nil || content.Encoding != "base64" {
		return bad()
	}
	data, e := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(content.Content), ""))
	if e != nil || digest(data) != p.WorkflowSHA {
		return bad()
	}
	root, e := ParseYAML(data)
	if e != nil {
		return bad()
	}
	jobs := Field(root, "jobs")
	if jobs == nil || len(jobs.Content) != 8 {
		return bad()
	}
	allowedNames := map[string]string{}
	var b Binding
	for _, route := range Routes() {
		job := Field(jobs, route.Job)
		runner := Field(job, "runs-on")
		timeout := Field(job, "timeout-minutes")
		if runner == nil || runner.Value != RunnerExpression(route) || timeout == nil {
			return bad()
		}
		minutes, e := strconv.Atoi(timeout.Value)
		want := 5
		if route.Job == "verify" {
			want = 30
		}
		if route.Job == "windows-native-check" {
			want = 20
		}
		if e != nil || minutes != want {
			return bad()
		}
		name := route.Job
		if n := Field(job, "name"); n != nil {
			name = n.Value
		}
		if name == "" || strings.Contains(name, "${{") || allowedNames[name] != "" || Field(job, "strategy") != nil {
			return bad()
		}
		allowedNames[name] = route.Job
		if route.Job == p.Role {
			b = Binding{RepoID: p.Source.ID, PR: p.Source.PR, RunID: p.RunID, Attempt: p.Attempt, SHA: p.Source.SHA, Tree: p.Tree, WorkflowSHA: p.WorkflowSHA, Role: p.Role, Label: route.Proposed, OS: route.OS, JobName: name, TimeoutMinutes: minutes}
		}
	}
	if b.Role == "" {
		return bad()
	}
	if _, e = CountWindowsCargo(root); e != nil {
		return bad()
	}
	js, e := ReadJobs(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100", r.FullName, p.RunID, p.Attempt), read)
	if e != nil {
		return bad()
	}
	seen := map[string]bool{}
	seenIDs := map[int64]bool{}
	for _, j := range js {
		if j.ID < 1 || j.RunID != p.RunID || allowedNames[j.Name] == "" || seen[j.Name] || seenIDs[j.ID] {
			return bad()
		}
		seen[j.Name] = true
		seenIDs[j.ID] = true
		if j.Name == b.JobName {
			if (j.Status != "queued" && j.Status != "in_progress") || len(j.Labels) != 1 || j.Labels[0] != b.Label {
				return bad()
			}
			b.JobID = j.ID
			if p.RunnerID == 0 {
				if j.Status != "queued" || j.RunnerID != 0 || j.RunnerName != "" {
					return bad()
				}
			} else if VerifyRunnerClaim(b, j, p.RunnerID, p.RunnerName) != nil {
				return bad()
			}
		}
	}
	if b.JobID < 1 {
		return bad()
	}
	if _, e = Verify(ctx, p.Source, read); e != nil {
		return Binding{}, e
	}
	if read(ctx, fmt.Sprintf("repos/%s/pulls/3613", r.FullName), &pr) != nil || pr.Head.Ref != "codex/todo2641-http-response-pseudo-20261015" || pr.Head.SHA != p.Source.SHA || pr.State != "open" || pr.Head.Repo.ID != p.Source.ID || pr.Base.Repo.ID != p.Source.ID {
		return bad()
	}
	return b, nil
}
func VerifyRunnerClaim(b Binding, j OfficialJob, runnerID int64, runnerName string) error {
	if runnerID < 1 || !strings.HasPrefix(runnerName, "todo2737-") || len(runnerName) != len("todo2737-")+32 || strings.Trim(strings.TrimPrefix(runnerName, "todo2737-"), "0123456789abcdef") != "" || b.JobID != j.ID || b.RunID != j.RunID || b.JobName != j.Name || j.Status != "in_progress" || j.RunnerID != runnerID || j.RunnerName != runnerName || len(j.Labels) != 1 || j.Labels[0] != b.Label {
		return errors.New("normal CI official runner claim rejected")
	}
	return nil
}

type workflowRun struct {
	ID          int64
	RunAttempt  int `json:"run_attempt"`
	Path, Event string
	HeadSHA     string `json:"head_sha"`
	HeadBranch  string `json:"head_branch"`
	Status      string
	Repository  Repository
	HeadRepo    Repository `json:"head_repository"`
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
