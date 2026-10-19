package contract

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func originals(t *testing.T) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, w := range workflows {
		b, e := os.ReadFile(filepath.Join("testdata", filepath.Base(w.Path)))
		if e != nil {
			t.Fatal(e)
		}
		out[w.Path] = b
	}
	return out
}
func testPlan() Plan {
	p := Plan{Version: 1, RepoID: RepoID, Head: strings.Repeat("a", 40), Tree: strings.Repeat("b", 40), Event: "pull_request", Branch: OriginalBranch, PR: 149, ContextSHA: strings.Repeat("c", 40), WorkflowSHA: strings.Repeat("c", 40), Workflow: ".github/workflows/rust.yml", WorkflowDigest: strings.Repeat("d", 64), RunID: 7, Attempt: 1, JobID: 71, Job: "test", JobName: "test (ubuntu-latest)"}
	p.WorkflowRef = Repository + "/" + p.Workflow + "@" + p.Ref()
	return p
}
func nativeFixture(t *testing.T) (Plan, map[string]any) {
	t.Helper()
	p := testPlan()
	repo := map[string]any{"id": RepoID, "full_name": Repository, "private": true, "fork": false, "owner": map[string]any{"login": "AutoDevJapan"}, "permissions": map[string]any{"admin": true}}
	pr := map[string]any{"number": 149, "state": "open", "merged": false, "merge_commit_sha": p.ContextSHA, "head": map[string]any{"sha": p.Head, "ref": p.Branch, "repo": repo}, "base": map[string]any{"sha": OriginalBase, "ref": "main", "repo": repo}}
	prefix := "repos/" + Repository + "/"
	fixtures := map[string]any{
		fmt.Sprintf("repositories/%d", RepoID): repo, prefix + "pulls/149": pr,
		prefix + "git/ref/heads/" + p.Branch: map[string]any{"object": map[string]string{"sha": p.Head}},
		prefix + "git/commits/" + p.Head:     map[string]any{"sha": p.Head, "tree": map[string]string{"sha": p.Tree}, "parents": []map[string]string{{"sha": OriginalHead}}},
		prefix + "actions/runs/7":            map[string]any{"id": 7, "run_attempt": 1, "head_sha": p.Head, "head_branch": p.Branch, "event": p.Event, "path": p.Workflow, "status": "queued", "repository": repo, "head_repository": repo, "pull_requests": []map[string]int{{"number": 149}}},
	}
	a := treeReply{SHA: OriginalTree}
	b := treeReply{SHA: p.Tree}
	for _, w := range workflows {
		data := originals(t)[w.Path]
		next, e := Propose(data, w)
		if e != nil {
			t.Fatal(e)
		}
		for sha, body := range map[string][]byte{OriginalHead: data, p.Head: next, p.ContextSHA: next} {
			fixtures[prefix+"contents/"+w.Path+"?ref="+sha] = map[string]string{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(body), "sha": blobID(body)}
		}
		a.Entries = append(a.Entries, treeEntry{w.Path, "100644", "blob", blobID(data)})
		b.Entries = append(b.Entries, treeEntry{w.Path, "100644", "blob", blobID(next)})
		if w.Path == p.Workflow {
			p.WorkflowDigest = Digest(next)
		}
	}
	for _, path := range []string{".github", ".github/workflows"} {
		a.Entries = append(a.Entries, treeEntry{path, "040000", "tree", strings.Repeat("d", 40)})
		b.Entries = append(b.Entries, treeEntry{path, "040000", "tree", strings.Repeat("e", 40)})
	}
	a.Entries = append(a.Entries, treeEntry{"tools/Cargo.toml", "100644", "blob", strings.Repeat("f", 40)})
	b.Entries = append(b.Entries, a.Entries[len(a.Entries)-1])
	fixtures[prefix+"git/trees/"+OriginalTree+"?recursive=1"] = a
	fixtures[prefix+"git/trees/"+p.Tree+"?recursive=1"] = b
	jobs := []Job{{ID: 71, RunID: 7, RunAttempt: 1, Name: p.JobName, Status: "queued", Labels: Labels(p)}}
	q := p
	q.JobName = "test (windows-latest)"
	jobs = append(jobs, Job{ID: 72, RunID: 7, RunAttempt: 1, Name: q.JobName, Status: "queued", Labels: Labels(q)})
	fixtures[prefix+"actions/runs/7/attempts/1/jobs?per_page=100"] = map[string]any{"total_count": 2, "jobs": jobs}
	return p, fixtures
}
func fixtureRead(fixtures map[string]any) ReadAPI {
	return func(_ context.Context, path string, out any) error {
		// URL-escaped branch paths are decoded only in the synthetic router, not by
		// the production API implementation.
		path = strings.ReplaceAll(path, "%2F", "/")
		v, ok := fixtures[path]
		if !ok {
			return fmt.Errorf("fixture endpoint absent")
		}
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		return json.Unmarshal(b, out)
	}
}
func TestProposePreservesCommandsAndParentConcurrency(t *testing.T) {
	for _, w := range workflows {
		t.Run(w.Job, func(t *testing.T) {
			original := originals(t)[w.Path]
			out, e := Propose(original, w)
			if e != nil {
				t.Fatal(e)
			}
			if !strings.Contains(string(out), "concurrency:\n  group: ${{ github.workflow }}-${{ github.event_name }}-${{ github.event.pull_request.number || github.run_id }}\n  cancel-in-progress: ${{ github.event_name == 'pull_request' }}") {
				t.Fatal("parent concurrency text changed")
			}
			for _, line := range strings.Split(string(original), "\n") {
				if strings.Contains(line, "run:") || strings.Contains(line, "cargo ") || strings.Contains(line, "rustup ") || strings.Contains(line, "& $driver") {
					if !strings.Contains(string(out), line) {
						t.Fatalf("original command lost")
					}
				}
			}
			if !strings.Contains(string(out), CheckoutRef) || !strings.Contains(string(out), "persist-credentials: false") {
				t.Fatal("fixed checkout missing")
			}
			modified := append([]byte(nil), original...)
			modified[len(modified)-2] ^= 1
			if _, e = Propose(modified, w); e == nil {
				t.Fatal("changed original accepted")
			}
		})
	}
}
func TestPinnedNativeAdmissionAndRejections(t *testing.T) {
	p, f := nativeFixture(t)
	if e := Verify(context.Background(), p, fixtureRead(f)); e != nil {
		t.Fatalf("valid native job: %v", e)
	}
	cases := []struct {
		name   string
		change func(*Plan, map[string]any)
	}{
		{"wrong owner", func(p *Plan, f map[string]any) {
			f[fmt.Sprintf("repositories/%d", RepoID)].(map[string]any)["owner"] = map[string]string{"login": "other"}
		}},
		{"fork source", func(p *Plan, f map[string]any) {
			f[fmt.Sprintf("repositories/%d", RepoID)].(map[string]any)["fork"] = true
		}},
		{"obsolete source", func(p *Plan, f map[string]any) {
			f["repos/"+Repository+"/git/ref/heads/"+p.Branch] = map[string]any{"object": map[string]string{"sha": strings.Repeat("e", 40)}}
		}},
		{"changed source tree", func(p *Plan, f map[string]any) {
			key := "repos/" + Repository + "/git/trees/" + p.Tree + "?recursive=1"
			tree := f[key].(treeReply)
			tree.Entries[len(tree.Entries)-1].SHA = strings.Repeat("1", 40)
			f[key] = tree
		}},
		{"truncated index", func(p *Plan, f map[string]any) {
			key := "repos/" + Repository + "/git/trees/" + p.Tree + "?recursive=1"
			tree := f[key].(treeReply)
			tree.Truncated = true
			f[key] = tree
		}},
		{"wrong PR merge context", func(p *Plan, f map[string]any) { p.ContextSHA = strings.Repeat("f", 40); p.WorkflowSHA = p.ContextSHA }},
		{"different attempt", func(p *Plan, f map[string]any) { p.Attempt = 2 }},
		{"hosted label spoof", func(p *Plan, f map[string]any) {
			key := "repos/" + Repository + "/actions/runs/7/attempts/1/jobs?per_page=100"
			jobs := f[key].(map[string]any)["jobs"].([]Job)
			jobs[0].Labels = []string{"ubuntu-latest"}
			f[key].(map[string]any)["jobs"] = jobs
		}},
		{"missing Windows matrix", func(p *Plan, f map[string]any) {
			key := "repos/" + Repository + "/actions/runs/7/attempts/1/jobs?per_page=100"
			jobs := f[key].(map[string]any)["jobs"].([]Job)
			f[key] = map[string]any{"total_count": 1, "jobs": jobs[:1]}
		}},
		{"different workflow bytes", func(p *Plan, f map[string]any) {
			key := "repos/" + Repository + "/contents/.github/workflows/rust.yml?ref=" + p.ContextSHA
			v := f[key].(map[string]string)
			body := []byte("unreviewed workflow")
			v["content"] = base64.StdEncoding.EncodeToString(body)
			v["sha"] = blobID(body)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, f := nativeFixture(t)
			tc.change(&p, f)
			if e := Verify(context.Background(), p, fixtureRead(f)); e == nil {
				t.Fatal("unsafe candidate admitted")
			}
		})
	}
}
func TestJobContextSeparatesPRHeadAndMerge(t *testing.T) {
	p := testPlan()
	env := map[string]string{"GITHUB_REPOSITORY": Repository, "GITHUB_REPOSITORY_ID": fmt.Sprint(RepoID), "GITHUB_SHA": p.ContextSHA, "GITHUB_RUN_ID": "7", "GITHUB_RUN_ATTEMPT": "1", "GITHUB_JOB": "test", "RUNNER_OS": "Linux", "GITHUB_EVENT_NAME": "pull_request", "GITHUB_REF": p.Ref(), "GITHUB_WORKFLOW_SHA": p.WorkflowSHA, "GITHUB_WORKFLOW_REF": p.WorkflowRef}
	repo := map[string]any{"id": RepoID, "full_name": Repository, "fork": false}
	raw, _ := json.Marshal(map[string]any{"number": 149, "repository": repo, "pull_request": map[string]any{"head": map[string]any{"sha": p.Head, "ref": p.Branch, "repo": repo}, "base": map[string]any{"ref": "main", "repo": repo}}})
	if e := CheckEvent(p, env, raw); e != nil {
		t.Fatal(e)
	}
	env["GITHUB_SHA"] = p.Head
	if e := CheckEvent(p, env, raw); e == nil {
		t.Fatal("head mistaken for actual merge context")
	}
	env["GITHUB_SHA"] = p.ContextSHA
	env["GITHUB_RUN_ID"] = "8"
	if e := CheckEvent(p, env, raw); e == nil {
		t.Fatal("different assigned job run accepted")
	}
}
func TestUnsafeBranchesAndEvents(t *testing.T) {
	for _, s := range []string{"../main", "a.lock", "a//b", "a@{b", "a\nb", "a\\b", "main~1", "/main", "main/"} {
		if ValidBranch(s) {
			t.Fatalf("branch accepted: %q", s)
		}
	}
	p := testPlan()
	p.Event = "pull_request_target"
	if p.Validate() == nil {
		t.Fatal("privileged PR event accepted")
	}
	p = testPlan()
	p.RepoID = 1
	if p.Validate() == nil {
		t.Fatal("other repository accepted")
	}
}

func mainFixture(t *testing.T) (Plan, map[string]any) {
	p, f := nativeFixture(t)
	pr := f["repos/"+Repository+"/pulls/149"].(map[string]any)
	pr["merged"], pr["state"], pr["merge_commit_sha"] = true, "closed", p.Head
	p.Event, p.Branch, p.PR = "push", "main", 0
	p.ContextSHA, p.WorkflowSHA = p.Head, p.Head
	p.WorkflowRef = Repository + "/" + p.Workflow + "@" + p.Ref()
	f["repos/"+Repository+"/git/ref/heads/main"] = map[string]any{"object": map[string]string{"sha": p.Head}}
	f["repos/"+Repository+"/compare/"+p.Head+"..."+p.Head] = map[string]string{"status": "identical"}
	r := f["repos/"+Repository+"/actions/runs/7"].(map[string]any)
	r["event"], r["head_branch"], r["pull_requests"] = "push", "main", []any{}
	return p, f
}

func TestMainContinuationRequiresActualMergeAndItsLineage(t *testing.T) {
	p, f := mainFixture(t)
	if e := Verify(context.Background(), p, fixtureRead(f)); e != nil {
		t.Fatal(e)
	}
	for _, field := range []string{"merged", "lineage"} {
		t.Run(field, func(t *testing.T) {
			p, f := mainFixture(t)
			if field == "merged" {
				f["repos/"+Repository+"/pulls/149"].(map[string]any)["merged"] = false
			} else {
				f["repos/"+Repository+"/compare/"+p.Head+"..."+p.Head] = map[string]string{"status": "diverged"}
			}
			if Verify(context.Background(), p, fixtureRead(f)) == nil {
				t.Fatal("unmerged/unrelated main admitted")
			}
		})
	}
}

func TestGeneratedCodeQLMaintainsLanguagesRouteAndThreatModel(t *testing.T) {
	makeFixture := func() (Plan, map[string]any) {
		p, f := mainFixture(t)
		p.Event, p.Workflow, p.WorkflowRef, p.WorkflowDigest, p.Job, p.JobName = "dynamic", CodeQLWorkflow, "", "", "analyze", "Analyze (actions)"
		r := f["repos/"+Repository+"/actions/runs/7"].(map[string]any)
		r["event"], r["path"] = "dynamic", CodeQLWorkflow
		f["repos/"+Repository+"/code-scanning/default-setup"] = map[string]any{"state": "configured", "languages": []string{"actions", "rust"}, "query_suite": "default", "threat_model": "remote", "runner_type": "labeled", "runner_label": CodeQLLabel}
		jobs := []Job{{ID: 71, RunID: 7, RunAttempt: 1, Name: "Analyze (actions)", Status: "queued", Labels: Labels(p)}, {ID: 72, RunID: 7, RunAttempt: 1, Name: "Analyze (rust)", Status: "queued", Labels: Labels(p)}}
		f["repos/"+Repository+"/actions/runs/7/attempts/1/jobs?per_page=100"] = map[string]any{"total_count": 2, "jobs": jobs}
		return p, f
	}
	p, f := makeFixture()
	if e := Verify(context.Background(), p, fixtureRead(f)); e != nil {
		t.Fatal(e)
	}
	for key, value := range map[string]any{"languages": []string{"actions"}, "runner_type": "standard", "runner_label": LinuxLabel, "threat_model": "", "query_suite": "extended"} {
		t.Run(key, func(t *testing.T) {
			p, f := makeFixture()
			f["repos/"+Repository+"/code-scanning/default-setup"].(map[string]any)[key] = value
			if Verify(context.Background(), p, fixtureRead(f)) == nil {
				t.Fatal("CodeQL settings drift admitted")
			}
		})
	}
}

func TestOfficialMatrixAssignmentAndSuccessAreSeparate(t *testing.T) {
	p, f := nativeFixture(t)
	key := "repos/" + Repository + "/actions/jobs/71"
	job := Job{ID: 71, RunID: 7, RunAttempt: 1, Name: p.JobName, Status: "in_progress", Labels: Labels(p), RunnerName: "todo2962-unique"}
	f[key] = job
	if e := VerifyAssigned(context.Background(), p, "todo2962-unique", fixtureRead(f)); e != nil {
		t.Fatal(e)
	}
	if RequireSuccess(context.Background(), p, "todo2962-unique", fixtureRead(f)) == nil {
		t.Fatal("running listener certified success")
	}
	if VerifyAssigned(context.Background(), p, "other-instance", fixtureRead(f)) == nil {
		t.Fatal("different runner accepted same-run matrix assignment")
	}
	job.Status, job.Conclusion = "completed", "failure"
	f[key] = job
	if RequireSuccess(context.Background(), p, "todo2962-unique", fixtureRead(f)) == nil {
		t.Fatal("failed formal job certified success")
	}
	job.Conclusion = "success"
	job.Steps = append(job.Steps, struct{ Name, Status, Conclusion string }{"original tests", "completed", "success"})
	f[key] = job
	if e := RequireSuccess(context.Background(), p, "todo2962-unique", fixtureRead(f)); e != nil {
		t.Fatal(e)
	}
}
