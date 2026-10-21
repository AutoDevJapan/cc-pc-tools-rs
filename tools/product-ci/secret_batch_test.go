package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestFiniteBatchOfficialMainDispatch(t *testing.T) {
	const marker = "CC_TEST_3059_BATCH_MAIN"
	if os.Getenv(marker) != "" {
		os.Args = []string{"product-ci", "current-pr-batch-qualify"}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestFiniteBatchOfficialMainDispatch$")
	cmd.Env = append(os.Environ(), marker+"=1")
	hideCurrentTestCommand(cmd)
	out, err := cmd.CombinedOutput()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 || !strings.Contains(string(out), "dependency_locator") || strings.Contains(string(out), "usage:") {
		t.Fatal("official entry point did not reach bounded dependency qualifier")
	}
}

func TestFiniteSecretBatchPlansPreserveBothSourcesAcrossCompletion(t *testing.T) {
	for _, completed939 := range []bool{false, true} {
		p := batchFixture()
		root := "repos/" + p.Repo
		repo := map[string]any{"id": p.RepoID, "full_name": p.Repo, "private": true, "fork": false, "owner": map[string]string{"login": "AutoDevJapan"}, "permissions": map[string]bool{"admin": true}}
		data := map[string]any{fmt.Sprintf("repositories/%d", p.RepoID): repo}
		path, workflow := ".github/workflows/fixture.yml", []byte("synthetic immutable scanner workflow")
		for _, b := range p.Batch {
			branch := fmt.Sprintf("candidate-%d", b.PR)
			status, conclusion := "queued", ""
			if completed939 && b.PR == 939 {
				status, conclusion = "completed", "success"
			}
			data[fmt.Sprintf("%s/pulls/%d", root, b.PR)] = map[string]any{"state": "open", "draft": false, "head": map[string]any{"sha": b.Head, "ref": branch, "repo": repo}, "base": map[string]any{"sha": b.Base, "repo": repo}, "merge_commit_sha": b.Context}
			data[root+"/git/ref/heads/"+branch] = map[string]any{"object": map[string]string{"sha": b.Head}}
			data[root+"/git/commits/"+b.Head] = map[string]any{"tree": map[string]string{"sha": b.Tree}}
			data[fmt.Sprintf("%s/actions/runs/%d", root, b.RunID)] = map[string]any{"id": b.RunID, "run_attempt": b.Attempt, "head_sha": b.Head, "head_branch": branch, "path": path, "event": "pull_request", "status": status, "repository": repo, "head_repository": repo, "pull_requests": []map[string]int{{"number": b.PR}}}
			data[fmt.Sprintf("%s/actions/runs/%d/attempts/%d/jobs?per_page=100", root, b.RunID, b.Attempt)] = map[string]any{"total_count": 1, "jobs": []map[string]int64{{"id": b.JobIDs[0]}}}
			job := map[string]any{"id": b.JobIDs[0], "run_id": b.RunID, "run_attempt": b.Attempt, "head_sha": b.Head, "status": status, "conclusion": conclusion, "runner_id": 0, "labels": []string{p.Label}}
			data[fmt.Sprintf("%s/actions/jobs/%d", root, b.JobIDs[0])] = job
			data[fmt.Sprintf("repositories/%d/actions/jobs/%d", b.RepoID, b.JobIDs[0])] = job
			for _, ref := range []string{b.Head, b.Context} {
				data[root+"/contents/"+path+"?ref="+ref] = map[string]string{"encoding": "base64", "type": "file", "content": base64.StdEncoding.EncodeToString(workflow)}
			}
		}
		read := func(_ context.Context, endpoint string, out any) error {
			v, ok := data[endpoint]
			if !ok {
				return fmt.Errorf("unexpected synthetic endpoint")
			}
			bytes, _ := json.Marshal(v)
			return json.Unmarshal(bytes, out)
		}
		selected, err := readyFiniteBatchMember(context.Background(), p.Batch, read)
		if err != nil || selected.PR != 995 {
			t.Fatalf("remaining repair not selected: %v", err)
		}
		got, err := planFiniteSecretBatchWithPolicy(context.Background(), p.Batch, 995, read, digest([]byte(path)), digest(workflow))
		if err != nil || got != p {
			t.Fatalf("two-source plan rejected: %v", err)
		}
		// A stale or changed peer cannot hide behind the selected repair's proof.
		peer := p.Batch[1]
		data[root+"/git/commits/"+peer.Head] = map[string]any{"tree": map[string]string{"sha": strings.Repeat("f", 40)}}
		if _, err := planFiniteSecretBatchWithPolicy(context.Background(), p.Batch, 995, read, digest([]byte(path)), digest(workflow)); err == nil {
			t.Fatal("changed peer source accepted")
		}
	}
}

func TestFiniteSecretBatchHooksSelectBothActualContexts(t *testing.T) {
	p := batchFixture()
	for _, b := range p.Batch {
		selected, err := selectFiniteBatchContext(p, p.Repo, b.Context, fmt.Sprint(b.RunID), fmt.Sprint(b.Attempt))
		if err != nil || selected.Current != b || !validGuestPlan(selected) || checkCodeQLHook(selected, p.Repo, b.Context, fmt.Sprint(b.RunID), fmt.Sprint(b.Attempt)) != nil {
			t.Fatalf("fixed member context rejected: %v", err)
		}
		for _, change := range []func(*codeQLPlan){func(p *codeQLPlan) { p.Tree = strings.Repeat("f", 40) }, func(p *codeQLPlan) { p.Current.JobIDs[0]++ }, func(p *codeQLPlan) { p.Batch[1].Head = strings.Repeat("f", 40) }, func(p *codeQLPlan) { p.Batch[1].Attempt++ }, func(p *codeQLPlan) { p.Batch[1].Kind = "codeql" }} {
			bad := selected
			change(&bad)
			if validGuestPlan(bad) {
				t.Fatal("altered batch member accepted")
			}
		}
		if _, err = selectFiniteBatchContext(p, p.Repo, b.Head, fmt.Sprint(b.RunID), fmt.Sprint(b.Attempt)); err == nil {
			t.Fatal("head substituted for reviewed merge context")
		}
		if _, err = selectFiniteBatchContext(p, p.Repo, b.Context, fmt.Sprint(b.RunID), fmt.Sprint(b.Attempt+1)); err == nil {
			t.Fatal("changed attempt accepted")
		}
	}
	if validGuestPlan(finiteBatchMember(p, p.Batch[1]).withoutFiniteBatch()) {
		t.Fatal("939 escaped into the legacy single-source guest route")
	}
}

func (p codeQLPlan) withoutFiniteBatch() codeQLPlan { p.Batch = [2]currentPRBinding{}; return p }

func TestFiniteSecretBatchAdmissionRechecksSelectedSource(t *testing.T) {
	p := batchFixture()
	calls := 0
	h := codeQLAdmissionHandlerForPlan(p, "key", func(_ context.Context, selected codeQLPlan) error {
		calls++
		if selected.Current != p.Batch[1] {
			return fmt.Errorf("wrong member selected")
		}
		return nil
	})
	for _, bad := range []bool{false, true} {
		q := codeQLAdmission{p.Batch[1].RunID, p.Batch[1].Attempt, p.Batch[1].Head}
		if bad {
			q.RunID++
		}
		b, _ := json.Marshal(q)
		req := httptest.NewRequest(http.MethodPost, "/admit", bytes.NewReader(b))
		req.Header.Set("X-Product-Admission", "key")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		want := http.StatusNoContent
		if bad {
			want = http.StatusForbidden
		}
		if w.Code != want {
			t.Fatalf("admission status=%d want=%d", w.Code, want)
		}
	}
	if calls != 1 {
		t.Fatalf("unapproved identity reached live source read: %d calls", calls)
	}
}

func batchFixture() codeQLPlan {
	b := currentPRBinding{RepoID: 1285649124, PR: 995, Head: "cbd688871651e46d4821a4535d57eaa408105ff2", Tree: "73a5d39e6694be7e6e604003dcb4e7bdc087c1c5", Base: "41090d19329b81dd1bd076f3336d723823be4b82", RunID: 39384472279, Attempt: 1, JobIDs: currentPRJobIDs{118500893552}, Kind: "secret", Context: "6e7ca682c861024b6f08f31382851dab4bceff35", ReviewSHA256: strings.Repeat("1", 64), AcceptanceSHA256: strings.Repeat("2", 64)}
	d := b
	d.PR, d.Head, d.Tree, d.RunID, d.Attempt, d.JobIDs = 939, "f1c6e235d93d0719532b592b03037edcbe7bffef", "df13e3dd2d796812f922868578df2d0ef8beceb5", 39344575918, 2, currentPRJobIDs{118550301356}
	d.Context = "de71ff5d9a1d5a61328cbcf054347f9686ad7b8e"
	return codeQLPlan{RepoID: b.RepoID, PR: b.PR, Candidate: b.Head, RunID: b.RunID, Attempt: b.Attempt, SHA: b.Head, Tree: b.Tree, Role: "candidate-secret-workflow", Label: secretLabel(b.RepoID), Kind: "secret", ContextSHA: b.Context, Current: b, Batch: [2]currentPRBinding{b, d}, Repo: "AutoDevJapan/opaque-fixture"}
}

func TestFiniteSecretBatchAdmitsOnlyBothFrozenJobs(t *testing.T) {
	p := batchFixture()
	for _, foreign := range []bool{false, true} {
		read := func(_ context.Context, endpoint string, out any) error {
			var v any
			if strings.Contains(endpoint, "/actions/runs?status=queued&") {
				v = map[string]any{"total_count": 2, "workflow_runs": []map[string]int64{{"id": p.Batch[0].RunID}, {"id": p.Batch[1].RunID}}}
			} else if strings.Contains(endpoint, "/actions/runs?status=") {
				v = map[string]any{"total_count": 0}
			} else {
				for _, b := range p.Batch {
					if endpoint == fmt.Sprintf("repos/%s/actions/runs/%d/jobs?per_page=100&page=1", p.Repo, b.RunID) {
						id := b.JobIDs[0]
						if foreign && b.PR == 939 {
							id++
						}
						v = map[string]any{"total_count": 1, "jobs": []map[string]any{{"id": id, "run_id": b.RunID, "status": "queued", "labels": []string{p.Label}}}}
					}
				}
			}
			if v == nil {
				return fmt.Errorf("unexpected fixture endpoint")
			}
			data, _ := json.Marshal(v)
			return json.Unmarshal(data, out)
		}
		err := checkCodeQLQueue(context.Background(), p, read)
		if !foreign && err != nil {
			t.Fatalf("approved two-source queue remained blocked: %v", err)
		}
		if foreign && err == nil {
			t.Fatal("job outside approved pair was admitted")
		}
	}
}
