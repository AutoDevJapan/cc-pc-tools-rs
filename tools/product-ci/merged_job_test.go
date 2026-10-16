package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

const mergedFixtureRepo = "repos/AutoDevJapan/opaque-merged/"

func mergedFixture(change func(map[string]any)) api {
	r := map[string]any{"id": int64(1285649124), "full_name": "AutoDevJapan/opaque-merged", "private": true, "fork": false, "owner": map[string]any{"login": "AutoDevJapan"}, "permissions": map[string]bool{"admin": true}, "default_branch": "main"}
	ref := func(sha string) any { return map[string]any{"object": map[string]string{"sha": sha}} }
	commit := func(sha string, parents []map[string]string) any {
		return map[string]any{"sha": sha, "tree": map[string]string{"sha": mergedTree}, "parents": parents}
	}
	jobs := []map[string]any{}
	for _, id := range []int64{117109408190, 117109408508, 117109408625, 117109408843} {
		jobs = append(jobs, map[string]any{"id": id, "run_id": mergedCodeQLRun, "labels": []string{codeQLLabel(1285649124)}})
	}
	d := map[string]any{
		"repositories/1285649124":                                                                        r,
		mergedFixtureRepo + "pulls/1002":                                                                 map[string]any{"state": "closed", "merged": true, "merge_commit_sha": mergedSHA, "head": map[string]any{"sha": mergedPRHead, "ref": "original", "repo": r}, "base": map[string]any{"ref": "main", "repo": r}},
		mergedFixtureRepo + "git/ref/heads/main":                                                         ref(mergedSHA),
		mergedFixtureRepo + "git/ref/heads/original":                                                     ref(mergedPRHead),
		mergedFixtureRepo + "git/commits/" + mergedSHA:                                                   commit(mergedSHA, []map[string]string{{"sha": mergedBase}, {"sha": mergedPRHead}}),
		mergedFixtureRepo + "git/commits/" + mergedPRHead:                                                commit(mergedPRHead, nil),
		mergedFixtureRepo + "code-scanning/default-setup":                                                map[string]any{"state": "configured", "languages": []string{"actions", "javascript", "javascript-typescript", "python", "rust", "typescript"}, "query_suite": "default", "threat_model": "remote", "runner_type": "labeled", "runner_label": codeQLLabel(1285649124)},
		mergedFixtureRepo + fmt.Sprintf("actions/runs/%d", mergedCodeQLRun):                              map[string]any{"id": mergedCodeQLRun, "workflow_id": mergedCodeQLWorkflow, "run_attempt": 1, "head_sha": mergedSHA, "head_branch": "main", "path": "dynamic/github-code-scanning/codeql", "event": "dynamic", "status": "queued", "repository": r, "head_repository": r},
		mergedFixtureRepo + fmt.Sprintf("actions/runs/%d/attempts/1/jobs?per_page=100", mergedCodeQLRun): map[string]any{"total_count": 4, "jobs": jobs},
	}
	if change != nil {
		change(d)
	}
	return func(_ context.Context, key string, out any) error {
		v, ok := d[key]
		if !ok {
			return errors.New("unexpected endpoint")
		}
		b, e := json.Marshal(v)
		if e != nil {
			return e
		}
		return json.Unmarshal(b, out)
	}
}

func TestMergedReceiptAndOriginalModesStaySeparate(t *testing.T) {
	ctx := context.Background()
	read := mergedFixture(nil)
	p, e := productPlan(ctx, mergedCandidate(), mergedCodeQLRun, "merged-codeql", read)
	if e != nil || !validGuestPlan(p) {
		t.Fatalf("audited merged plan rejected: %v", e)
	}
	if checkCodeQLHook(p, p.Repo, mergedSHA, fmt.Sprint(mergedCodeQLRun), "1") != nil {
		t.Fatal("merged hook rejected")
	}
	if checkCodeQLHook(p, p.Repo, mergedPRHead, fmt.Sprint(mergedCodeQLRun), "1") == nil {
		t.Fatal("old head passed merged hook")
	}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), "opaque-merged") {
		t.Fatal("repo leaked into receipt")
	}
	if _, e = verify(ctx, candidates[0], read); e == nil {
		t.Fatal("closed PR admitted by original verifier")
	}
	if _, e = productPlan(ctx, mergedCandidate(), mergedCodeQLRun, "", read); e == nil {
		t.Fatal("generic mode admitted merged source")
	}
	if _, e = planMergedCodeQL(ctx, candidates[1], mergedCodeQLRun, read); e == nil {
		t.Fatal("other candidate admitted")
	}
	if _, e = planMergedCodeQL(ctx, mergedCandidate(), mergedCodeQLRun+1, read); e == nil {
		t.Fatal("unknown run admitted")
	}
	if _, e = verifyExecutionSource(ctx, mergedCandidate(), read); e != nil {
		t.Fatal(e)
	}
}

func TestMergedRejectsChangedReceipt(t *testing.T) {
	pr := mergedFixtureRepo + "pulls/1002"
	run := mergedFixtureRepo + fmt.Sprintf("actions/runs/%d", mergedCodeQLRun)
	setup := mergedFixtureRepo + "code-scanning/default-setup"
	jobs := mergedFixtureRepo + fmt.Sprintf("actions/runs/%d/attempts/1/jobs?per_page=100", mergedCodeQLRun)
	set := func(key, field string, value any) func(map[string]any) {
		return func(d map[string]any) { d[key].(map[string]any)[field] = value }
	}
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
	}{
		{"fork", set("repositories/1285649124", "fork", true)},
		{"public", set("repositories/1285649124", "private", false)},
		{"not-admin", set("repositories/1285649124", "permissions", map[string]bool{"admin": false})},
		{"other-owner", set("repositories/1285649124", "owner", map[string]string{"login": "unknown"})},
		{"unsafe-name", set("repositories/1285649124", "full_name", "AutoDevJapan/../other")},
		{"open", set(pr, "state", "open")},
		{"not-merged", set(pr, "merged", false)},
		{"different-merge", set(pr, "merge_commit_sha", mergedPRHead)},
		{"head-moved", func(d map[string]any) { d[pr].(map[string]any)["head"].(map[string]any)["sha"] = mergedBase }},
		{"foreign-base", func(d map[string]any) {
			d[pr].(map[string]any)["base"].(map[string]any)["repo"] = map[string]any{"id": 99}
		}},
		{"main-advanced", set(mergedFixtureRepo+"git/ref/heads/main", "object", map[string]string{"sha": mergedBase})},
		{"branch-advanced", set(mergedFixtureRepo+"git/ref/heads/original", "object", map[string]string{"sha": mergedBase})},
		{"tree-changed", set(mergedFixtureRepo+"git/commits/"+mergedSHA, "tree", map[string]string{"sha": mergedBase})},
		{"head-tree-changed", set(mergedFixtureRepo+"git/commits/"+mergedPRHead, "tree", map[string]string{"sha": mergedBase})},
		{"parents-reordered", set(mergedFixtureRepo+"git/commits/"+mergedSHA, "parents", []map[string]string{{"sha": mergedPRHead}, {"sha": mergedBase}})},
		{"extra-parent", set(mergedFixtureRepo+"git/commits/"+mergedSHA, "parents", []map[string]string{{"sha": mergedBase}, {"sha": mergedPRHead}, {"sha": mergedBase}})},
		{"wrong-workflow", set(run, "workflow_id", int64(306106154))},
		{"new-attempt", set(run, "run_attempt", 2)},
		{"wrong-event", set(run, "event", "pull_request_target")},
		{"wrong-source", set(run, "head_sha", mergedPRHead)},
		{"finished", set(run, "status", "completed")},
		{"foreign-run-head", set(run, "head_repository", map[string]any{"id": 99})},
		{"hosted", set(setup, "runner_type", "standard")},
		{"generic-label", set(setup, "runner_label", "self-hosted")},
		{"missing-language", set(setup, "languages", []string{"actions", "python", "rust"})},
		{"query-change", set(setup, "query_suite", "extended")},
		{"extra-job", set(jobs, "total_count", 5)},
		{"unknown-job", func(d map[string]any) { d[jobs].(map[string]any)["jobs"].([]map[string]any)[0]["id"] = int64(999) }},
		{"duplicate-job", func(d map[string]any) {
			d[jobs].(map[string]any)["jobs"].([]map[string]any)[1]["id"] = int64(117109408190)
		}},
		{"job-other-run", func(d map[string]any) { d[jobs].(map[string]any)["jobs"].([]map[string]any)[0]["run_id"] = int64(999) }},
		{"extra-label", func(d map[string]any) {
			d[jobs].(map[string]any)["jobs"].([]map[string]any)[0]["labels"] = []string{codeQLLabel(1285649124), "self-hosted"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := planMergedCodeQL(context.Background(), mergedCandidate(), mergedCodeQLRun, mergedFixture(tc.change)); e == nil {
				t.Fatal("changed receipt admitted")
			}
		})
	}
}

func TestMergedGuestRequiresEveryFixedField(t *testing.T) {
	p, e := planMergedCodeQL(context.Background(), mergedCandidate(), mergedCodeQLRun, mergedFixture(nil))
	if e != nil {
		t.Fatal(e)
	}
	for _, change := range []func(*codeQLPlan){
		func(p *codeQLPlan) { p.Kind = "" }, func(p *codeQLPlan) { p.RunID++ }, func(p *codeQLPlan) { p.Attempt++ }, func(p *codeQLPlan) { p.RepoID++ }, func(p *codeQLPlan) { p.PR++ }, func(p *codeQLPlan) { p.Candidate = mergedPRHead }, func(p *codeQLPlan) { p.SHA = mergedPRHead }, func(p *codeQLPlan) { p.Tree = mergedBase }, func(p *codeQLPlan) { p.Label = "self-hosted" }, func(p *codeQLPlan) { p.Role = "setup-validation" }, func(p *codeQLPlan) { p.ContextSHA = mergedSHA },
	} {
		q := p
		change(&q)
		if validGuestPlan(q) {
			t.Fatal("modified merged guest plan admitted")
		}
	}
}

func TestMergedReadFailureIdentifiesStageAndStopsWithoutFallback(t *testing.T) {
	for _, tc := range []struct{ endpoint, stage string }{
		{"repositories/1285649124", "repository metadata"},
		{mergedFixtureRepo + "pulls/1002", "PR receipt"},
		{mergedFixtureRepo + "git/ref/heads/main", "current main ref"},
		{mergedFixtureRepo + "git/ref/heads/original", "original PR branch ref"},
		{mergedFixtureRepo + "git/commits/" + mergedSHA, "fixed commit"},
	} {
		t.Run(tc.stage, func(t *testing.T) {
			fixture := mergedFixture(nil)
			failed, afterFailure := false, 0
			read := func(ctx context.Context, endpoint string, out any) error {
				if failed {
					afterFailure++
				}
				if endpoint == tc.endpoint {
					failed = true
					return &gitHubReadError{class: "not-found"}
				}
				return fixture(ctx, endpoint, out)
			}
			_, err := planMergedCodeQL(context.Background(), mergedCandidate(), mergedCodeQLRun, read)
			want := "merged source " + tc.stage + ": GitHub read failed (not-found)"
			if !failed || err == nil || err.Error() != want || afterFailure != 0 {
				t.Fatal("failure was misattributed, leaked data, or continued after rejection")
			}
		})
	}
	unsafe := mergedReadFailure("original PR branch ref", errors.New("opaque-sensitive-response"))
	if unsafe.Error() != "merged source original PR branch ref: GitHub read failed (unclassified)" {
		t.Fatal("injected transport error leaked")
	}
}
