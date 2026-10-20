package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCurrentPRGuestRetainsFixedSourceAndRole(t *testing.T) {
	p := codeQLPlan{RepoID: 1285649124, PR: 995, Candidate: strings.Repeat("a", 40), SHA: strings.Repeat("a", 40), Tree: strings.Repeat("c", 40), RunID: 7, Attempt: 2, Role: "candidate-secret-workflow", Kind: "secret", Label: secretLabel(1285649124), ContextSHA: strings.Repeat("b", 40)}
	p.Current = currentPRBinding{RepoID: p.RepoID, PR: p.PR, Head: p.SHA, Tree: p.Tree, Base: strings.Repeat("d", 40), RunID: p.RunID, Attempt: p.Attempt, JobIDs: [8]int64{11}, Kind: "secret", Context: p.ContextSHA, ReviewSHA256: strings.Repeat("1", 64), AcceptanceSHA256: strings.Repeat("2", 64)}
	if !validGuestPlan(p) {
		t.Fatal("explicit current PR with fixed source and secret role rejected")
	}
	for _, change := range []func(*codeQLPlan){
		func(p *codeQLPlan) { p.PR = 996 },
		func(p *codeQLPlan) { p.RepoID++ },
		func(p *codeQLPlan) { p.SHA = strings.Repeat("d", 40) },
		func(p *codeQLPlan) { p.Kind = "advanced" },
	} {
		bad := p
		change(&bad)
		if validGuestPlan(bad) {
			t.Fatal("unapproved current PR scope accepted")
		}
	}
	if len(candidates) != 4 || candidates[0].PR != 1002 || candidates[0].SHA != "20c441df545a88f4a839d7e906b5aeb4f1708ae3" {
		t.Fatal("original candidates changed")
	}
}

func currentPRFixture(t *testing.T, kind string, change func(map[string]any)) (currentPRBinding, api, string, string) {
	t.Helper()
	b := currentPRBinding{RepoID: 1285649124, PR: 995, Head: strings.Repeat("a", 40), Tree: strings.Repeat("c", 40), Base: strings.Repeat("d", 40), RunID: 7, Attempt: 2, JobIDs: [8]int64{11}, Kind: kind, ReviewSHA256: strings.Repeat("1", 64), AcceptanceSHA256: strings.Repeat("2", 64)}
	if kind == "secret" {
		b.Context = strings.Repeat("b", 40)
	}
	repo := "AutoDevJapan/opaque-fixture"
	root := "repos/" + repo
	r := map[string]any{"id": b.RepoID, "full_name": repo, "private": true, "fork": false, "owner": map[string]any{"login": "AutoDevJapan"}, "permissions": map[string]bool{"admin": true}}
	workflowPath := ".github/workflows/opaque-secret.yml"
	workflowBytes := []byte("synthetic reviewed workflow")
	event, path, label := "dynamic", "dynamic/github-code-scanning/codeql", codeQLLabel(b.RepoID)
	if kind == "secret" {
		event, path, label = "pull_request", workflowPath, secretLabel(b.RepoID)
	}
	d := map[string]any{
		fmt.Sprintf("repositories/%d", b.RepoID):              r,
		root + "/pulls/995":                                   map[string]any{"state": "open", "draft": false, "head": map[string]any{"sha": b.Head, "ref": "candidate", "repo": r}, "base": map[string]any{"sha": b.Base, "repo": r}, "merge_commit_sha": b.Context},
		root + "/git/ref/heads/candidate":                     map[string]any{"object": map[string]any{"sha": b.Head}},
		root + "/git/commits/" + b.Head:                       map[string]any{"tree": map[string]string{"sha": b.Tree}},
		root + "/code-scanning/default-setup":                 map[string]any{"state": "configured", "runner_type": "labeled", "runner_label": label},
		root + "/actions/runs/7":                              map[string]any{"id": 7, "run_attempt": 2, "head_sha": b.Head, "head_branch": "candidate", "event": event, "path": path, "status": "queued", "repository": r, "head_repository": r, "pull_requests": []map[string]int{{"number": 995}}},
		root + "/actions/runs/7/attempts/2/jobs?per_page=100": map[string]any{"total_count": 1, "jobs": []map[string]int{{"id": 11}}},
		root + "/actions/jobs/11":                             map[string]any{"id": 11, "run_id": 7, "run_attempt": 2, "head_sha": b.Head, "status": "queued", "runner_id": 0, "labels": []string{label}},
	}
	for _, sha := range []string{b.Head, b.Context} {
		d[root+"/contents/"+workflowPath+"?ref="+sha] = map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(workflowBytes)}
	}
	if change != nil {
		change(d)
	}
	read := func(_ context.Context, endpoint string, out any) error {
		v, ok := d[endpoint]
		if !ok {
			return errors.New("unexpected synthetic endpoint")
		}
		data, e := json.Marshal(v)
		if e != nil {
			return e
		}
		return json.Unmarshal(data, out)
	}
	return b, read, digest([]byte(workflowPath)), digest(workflowBytes)
}

func TestCurrentPRPlansRejectChangedOriginalEvidence(t *testing.T) {
	root := "repos/AutoDevJapan/opaque-fixture"
	for _, kind := range []string{"secret", "codeql"} {
		t.Run(kind, func(t *testing.T) {
			b, read, path, bytes := currentPRFixture(t, kind, nil)
			p, e := planCurrentPRWithPolicy(context.Background(), b, read, path, bytes)
			if e != nil || !validGuestPlan(p) || p.Current != b {
				t.Fatalf("fixed current PR rejected: %v", e)
			}
			for _, change := range []func(*currentPRBinding){
				func(b *currentPRBinding) { b.PR++ }, func(b *currentPRBinding) { b.RepoID++ },
				func(b *currentPRBinding) { b.Head = strings.Repeat("e", 40) }, func(b *currentPRBinding) { b.Tree = strings.Repeat("e", 40) },
				func(b *currentPRBinding) { b.Base = strings.Repeat("e", 40) }, func(b *currentPRBinding) { b.RunID++ },
				func(b *currentPRBinding) { b.Attempt++ }, func(b *currentPRBinding) { b.JobIDs[0]++ },
				func(b *currentPRBinding) { b.Kind = "advanced" }, func(b *currentPRBinding) { b.ReviewSHA256 = "" },
			} {
				bad := b
				change(&bad)
				if _, e := planCurrentPRWithPolicy(context.Background(), bad, read, path, bytes); e == nil {
					t.Fatal("changed pin accepted")
				}
			}
			for name, change := range map[string]func(map[string]any){
				"fork": func(d map[string]any) { d[fmt.Sprintf("repositories/%d", b.RepoID)].(map[string]any)["fork"] = true },
				"head": func(d map[string]any) {
					d[root+"/pulls/995"].(map[string]any)["head"].(map[string]any)["sha"] = strings.Repeat("e", 40)
				},
				"branch": func(d map[string]any) {
					d[root+"/git/ref/heads/candidate"].(map[string]any)["object"] = map[string]string{"sha": strings.Repeat("e", 40)}
				},
				"draft":       func(d map[string]any) { d[root+"/pulls/995"].(map[string]any)["draft"] = true },
				"attempt":     func(d map[string]any) { d[root+"/actions/runs/7"].(map[string]any)["run_attempt"] = 3 },
				"run":         func(d map[string]any) { d[root+"/actions/runs/7"].(map[string]any)["id"] = 8 },
				"job-run":     func(d map[string]any) { d[root+"/actions/jobs/11"].(map[string]any)["run_id"] = 8 },
				"job-attempt": func(d map[string]any) { d[root+"/actions/jobs/11"].(map[string]any)["run_attempt"] = 3 },
				"job-head": func(d map[string]any) {
					d[root+"/actions/jobs/11"].(map[string]any)["head_sha"] = strings.Repeat("e", 40)
				},
				"job-label":        func(d map[string]any) { d[root+"/actions/jobs/11"].(map[string]any)["labels"] = []string{"unrelated"} },
				"unknown-workflow": func(d map[string]any) { d[root+"/actions/runs/7"].(map[string]any)["path"] = "unreviewed" },
				"changed-workflow": func(d map[string]any) {
					d[root+"/contents/.github/workflows/opaque-secret.yml?ref="+b.Head] = map[string]any{"type": "file", "encoding": "base64", "content": "Y2hhbmdlZA=="}
				},
			} {
				if name == "changed-workflow" && kind != "secret" {
					continue
				}
				t.Run(name, func(t *testing.T) {
					b, read, path, bytes := currentPRFixture(t, kind, change)
					if _, e := planCurrentPRWithPolicy(context.Background(), b, read, path, bytes); e == nil {
						t.Fatal("changed API evidence accepted")
					}
				})
			}
		})
	}
	reads := 0
	_, e := planCurrentPR(context.Background(), currentPRBinding{RepoID: 1285649124, PR: 996}, func(context.Context, string, any) error { reads++; return nil })
	if e == nil || reads != 0 {
		t.Fatal("unknown scope reached API")
	}
}

func TestCurrentPRInputReadsExactOriginalProofBytes(t *testing.T) {
	b, _, _, _ := currentPRFixture(t, "secret", nil)
	dir := t.TempDir()
	input := currentPRInput{currentPRBinding: b, ReviewFile: filepath.Join(dir, "review.json"), AcceptanceFile: filepath.Join(dir, "acceptance.json")}
	for i, file := range []string{input.ReviewFile, input.AcceptanceFile} {
		kind := "review"
		if i == 1 {
			kind = "acceptance"
		}
		original := []byte("opaque original receipt")
		v := currentPREvidence{OriginTodo: 3056, RepoID: b.RepoID, PR: b.PR, Head: b.Head, Tree: b.Tree, Kind: kind, Success: true, Must: 0, Original: original, OriginalSHA256: digest(original)}
		data, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(file, data, 0600); e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			input.ReviewSHA256 = digest(data)
		} else {
			input.AcceptanceSHA256 = digest(data)
		}
		wrong := b
		wrong.Head = strings.Repeat("e", 40)
		if verifyCurrentPREvidence(data, wrong, kind) == nil {
			t.Fatal("old proof accepted for new head")
		}
	}
	data, _ := json.Marshal(input)
	file := filepath.Join(dir, "pin.json")
	if e := os.WriteFile(file, data, 0600); e != nil {
		t.Fatal(e)
	}
	if got, e := readCurrentPRInput(file); e != nil || got != input.currentPRBinding {
		t.Fatalf("valid proof rejected: %v", e)
	}
	same := input
	same.ReviewFile = input.AcceptanceFile
	same.ReviewSHA256 = input.AcceptanceSHA256
	sameData, _ := json.Marshal(same)
	if e := os.WriteFile(file, sameData, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := readCurrentPRInput(file); e == nil {
		t.Fatal("acceptance used for both evidence kinds")
	}
	if e := os.WriteFile(file, data, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(input.ReviewFile, []byte("changed original receipt"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := readCurrentPRInput(file); e == nil {
		t.Fatal("changed proof accepted")
	}
	if e := os.Remove(input.ReviewFile); e != nil {
		t.Fatal(e)
	}
	if _, e := readCurrentPRInput(file); e == nil {
		t.Fatal("missing proof accepted")
	}
}

func TestCurrentPRJobSetInputBounds(t *testing.T) {
	for _, input := range []string{`[11]`, `[11,12]`, `[]`, `[11,12,13,14,15,16,17,18,19]`, `[11,11]`, `[0]`, `[-1]`, `[12,11]`} {
		var ids currentPRJobIDs
		e := json.Unmarshal([]byte(input), &ids)
		want := input == `[11]` || input == `[11,12]`
		if (e == nil) != want {
			t.Fatalf("job set %s admitted=%v", input, e == nil)
		}
	}
	b, _, _, _ := currentPRFixture(t, "secret", nil)
	b.JobIDs[1] = 12
	if b.valid() {
		t.Fatal("secret accepted multiple jobs")
	}
}

func TestCurrentPRAdmissionUsesLiveSourceAndRunner(t *testing.T) {
	b, read, path, bytes := currentPRFixture(t, "codeql", nil)
	p, e := planCurrentPRWithPolicy(context.Background(), b, read, path, bytes)
	if e != nil {
		t.Fatal(e)
	}
	c := candidate{b.RepoID, b.PR, b.Head}
	assigned := func(ctx context.Context, endpoint string, out any) error {
		if e := read(ctx, endpoint, out); e != nil {
			return e
		}
		if strings.Contains(endpoint, "/actions/jobs/") {
			data, _ := json.Marshal(out)
			var job map[string]any
			_ = json.Unmarshal(data, &job)
			job["runner_name"] = "owned-runner"
			job["runner_id"] = 44
			job["status"] = "in_progress"
			data, _ = json.Marshal(job)
			return json.Unmarshal(data, out)
		}
		return nil
	}
	if e := verifyCodeQLAdmissionPlan(context.Background(), c, p, "owned-runner", assigned); e != nil {
		t.Fatal(e)
	}
	stale := func(ctx context.Context, endpoint string, out any) error {
		if strings.Contains(endpoint, "/pulls/995") {
			return errors.New("source changed")
		}
		return assigned(ctx, endpoint, out)
	}
	if verifyCodeQLAdmissionPlan(context.Background(), c, p, "owned-runner", stale) == nil {
		t.Fatal("live changed source accepted")
	}
	reads, pauses := 0, 0
	delayed := func(ctx context.Context, endpoint string, out any) error {
		reads++
		if reads == 1 {
			return read(ctx, endpoint, out)
		}
		return assigned(ctx, endpoint, out)
	}
	if e := awaitCurrentPRRunner(context.Background(), p, "owned-runner", delayed, func(context.Context) error { pauses++; return nil }); e != nil || pauses != 1 {
		t.Fatalf("bounded delayed assignment: %v, pauses=%d", e, pauses)
	}
	reads, pauses = 0, 0
	if e := awaitCurrentPRRunner(context.Background(), p, "owned-runner", read, func(context.Context) error { pauses++; return nil }); e == nil || pauses != 2 {
		t.Fatal("unassigned runner did not stop after three reads")
	}
	if e := checkCurrentPRReady(context.Background(), p, read); e != nil {
		t.Fatal(e)
	}
	if checkCurrentPRReady(context.Background(), p, assigned) == nil {
		t.Fatal("already assigned matrix reached registration")
	}
}

func TestCurrentPRMatrixCompletionAndFreshInvocation(t *testing.T) {
	root := "repos/AutoDevJapan/opaque-fixture"
	for _, status := range []string{"queued", "success", "failure"} {
		b, read, path, bytes := currentPRFixture(t, "codeql", func(d map[string]any) {
			d[root+"/actions/runs/7/attempts/2/jobs?per_page=100"] = map[string]any{"total_count": 2, "jobs": []map[string]int{{"id": 11}, {"id": 12}}}
			job := d[root+"/actions/jobs/11"].(map[string]any)
			second := map[string]any{}
			for k, v := range job {
				second[k] = v
			}
			second["id"] = 12
			d[root+"/actions/jobs/12"] = second
			if status != "queued" {
				job["status"] = "completed"
				job["conclusion"] = status
			}
		})
		b.JobIDs[1] = 12
		if _, e := planCurrentPRWithPolicy(context.Background(), b, read, path, bytes); (e == nil) != (status != "failure") {
			t.Fatalf("partial matrix %s: %v", status, e)
		}
	}
	newHead := strings.Repeat("e", 40)
	b, read, path, bytes := currentPRFixture(t, "codeql", func(d map[string]any) {
		oldHead := strings.Repeat("a", 40)
		d[root+"/pulls/995"].(map[string]any)["head"].(map[string]any)["sha"] = newHead
		d[root+"/git/ref/heads/candidate"] = map[string]any{"object": map[string]string{"sha": newHead}}
		d[root+"/git/commits/"+newHead] = d[root+"/git/commits/"+oldHead]
		d[root+"/actions/runs/7"].(map[string]any)["head_sha"] = newHead
		d[root+"/actions/jobs/11"].(map[string]any)["head_sha"] = newHead
	})
	if _, e := planCurrentPRWithPolicy(context.Background(), b, read, path, bytes); e == nil {
		t.Fatal("old invocation followed advancing source")
	}
	b.Head = newHead
	if p, e := planCurrentPRWithPolicy(context.Background(), b, read, path, bytes); e != nil || p.SHA != newHead {
		t.Fatalf("fresh fixed invocation requires no compiled SHA: %v", e)
	}
}

func TestCurrentPRGeneratedCodeQLRefBindsExactPRAndSHA(t *testing.T) {
	root := "repos/AutoDevJapan/opaque-fixture"
	for _, fault := range []string{"", "different-pr", "changed-ref", "unrelated-branch"} {
		b, read, path, bytes := currentPRFixture(t, "codeql", func(d map[string]any) {
			run := d[root+"/actions/runs/7"].(map[string]any)
			run["head_branch"] = "refs/pull/995/head"
			sha := strings.Repeat("a", 40)
			if fault == "different-pr" {
				run["head_branch"] = "refs/pull/996/head"
			}
			if fault == "changed-ref" {
				sha = strings.Repeat("e", 40)
			}
			if fault == "unrelated-branch" {
				run["head_branch"] = "unrelated"
			}
			d[root+"/git/ref/pull/995/head"] = map[string]any{"object": map[string]string{"sha": sha}}
		})
		if _, e := planCurrentPRWithPolicy(context.Background(), b, read, path, bytes); (e == nil) != (fault == "") {
			t.Fatalf("generated PR ref %q: %v", fault, e)
		}
	}
}

func TestCurrentPRHookRejectsOtherAssignedJob(t *testing.T) {
	b, read, path, bytes := currentPRFixture(t, "codeql", nil)
	p, e := planCurrentPRWithPolicy(context.Background(), b, read, path, bytes)
	if e != nil {
		t.Fatal(e)
	}
	for _, fault := range []string{"", "other-runner", "other-job", "other-head", "other-attempt", "queued"} {
		read := func(_ context.Context, _ string, out any) error {
			j := map[string]any{"id": 11, "run_id": 7, "run_attempt": 2, "head_sha": b.Head, "runner_id": 44, "runner_name": "owned-runner", "status": "in_progress"}
			switch fault {
			case "other-runner":
				j["runner_name"] = "foreign-runner"
			case "other-job":
				j["id"] = 12
			case "other-head":
				j["head_sha"] = strings.Repeat("e", 40)
			case "other-attempt":
				j["run_attempt"] = 3
			case "queued":
				j["status"] = "queued"
			}
			data, _ := json.Marshal(j)
			return json.Unmarshal(data, out)
		}
		if got := checkCurrentPRRunner(context.Background(), p, "owned-runner", read) == nil; got != (fault == "") {
			t.Fatalf("runner assignment %q accepted=%v", fault, got)
		}
	}
}

func TestCurrentPRQueueRejectsUnfrozenJob(t *testing.T) {
	b, read, path, bytes := currentPRFixture(t, "codeql", nil)
	p, e := planCurrentPRWithPolicy(context.Background(), b, read, path, bytes)
	if e != nil {
		t.Fatal(e)
	}
	for _, jobID := range []int64{11, 12} {
		read := func(_ context.Context, path string, out any) error {
			var v any
			switch {
			case strings.Contains(path, "/actions/runs?status=queued&"):
				v = map[string]any{"total_count": 1, "workflow_runs": []map[string]int{{"id": 7}}}
			case strings.Contains(path, "/actions/runs?status="):
				v = map[string]any{"total_count": 0, "workflow_runs": []any{}}
			case strings.Contains(path, "/actions/runs/7/jobs?"):
				v = map[string]any{"total_count": 1, "jobs": []map[string]any{{"id": jobID, "run_id": 7, "status": "queued", "labels": []string{p.Label}}}}
			default:
				return errors.New("unexpected queue fixture endpoint")
			}
			data, _ := json.Marshal(v)
			return json.Unmarshal(data, out)
		}
		if got := checkCodeQLQueue(context.Background(), p, read) == nil; got != (jobID == 11) {
			t.Fatalf("queue job %d accepted=%v", jobID, got)
		}
	}
}
