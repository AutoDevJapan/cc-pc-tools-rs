//go:build windows && amd64

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func api(path string, v any) error {
	return apiWithContext(context.Background(), path, v)
}
func apiWithContext(parent context.Context, path string, v any) error {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, `D:\CC\Tools\codex-jobs\gh_gate.exe`, "api", path)
	hidden(c)
	// gh uses its existing OAuth store. Never request auth-token, dump config,
	// pass a credential in argv, or forward the owner's env to the service.
	pipe, e := c.StdoutPipe()
	if e != nil {
		return e
	}
	if e = c.Start(); e != nil {
		return errors.New("GitHub read API unavailable")
	}
	b, readErr := boundedRead(pipe, 4<<20)
	if readErr != nil {
		_ = c.Process.Kill()
	}
	waitErr := c.Wait()
	if readErr != nil || waitErr != nil {
		return errors.New("GitHub read API failed")
	}
	return json.Unmarshal(b, v)
}
func audit(p Plan) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	return auditMode(ctx, p, false)
}
func auditFinished(p Plan) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for {
		e := auditMode(ctx, p, true)
		if !errors.Is(e, errPending) {
			return e
		}
		select {
		case <-ctx.Done():
			return errors.New("official completion/registration deadline")
		case <-time.After(time.Second):
		}
	}
}
func auditMode(ctx context.Context, p Plan, finished bool) error {
	api := func(path string, v any) error { return apiWithContext(ctx, path, v) }
	if p.Fixture != "" {
		return errors.New("fixture is not API or runner identity evidence")
	}
	var repo struct {
		ID            int64
		FullName      string `json:"full_name"`
		Private, Fork bool
		Owner         struct{ Login string }
		Permissions   struct{ Admin bool }
	}
	if e := api("repos/"+repository, &repo); e != nil {
		return e
	}
	if repo.ID != repositoryID || repo.FullName != repository || repo.Owner.Login != "AutoDevJapan" || !repo.Private || repo.Fork || !repo.Permissions.Admin {
		return errors.New("repository ownership/scope rejected")
	}
	var pr struct {
		State      string
		Head, Base struct {
			SHA, Ref string
			Repo     struct {
				ID       int64
				FullName string `json:"full_name"`
			}
		}
	}
	if e := api("repos/"+repository+"/pulls/148", &pr); e != nil {
		return e
	}
	if pr.State != "open" || pr.Head.SHA != p.PRHead || pr.Head.Ref != branch || pr.Base.SHA != baseHead || pr.Base.Ref != "main" || pr.Head.Repo.ID != repositoryID || pr.Base.Repo.ID != repositoryID || pr.Head.Repo.FullName != repository || pr.Base.Repo.FullName != repository {
		return errors.New("actual PR changed")
	}
	if p.PRHead != sourceHead {
		var compare struct {
			Status string
			Total  int `json:"total_commits"`
			Files  []struct {
				Filename, Status string
				Previous         string `json:"previous_filename"`
			}
		}
		if e := api("repos/"+repository+"/compare/"+sourceHead+"..."+p.PRHead, &compare); e != nil {
			return e
		}
		if compare.Status != "ahead" || len(compare.Files) < 1 || len(compare.Files) > 3 {
			return errors.New("routing commit is not a finite workflow-only change")
		}
		for _, f := range compare.Files {
			if (f.Filename != ".github/workflows/rust.yml" && f.Filename != ".github/workflows/native-driver.yml" && f.Filename != ".github/workflows/live-chain.yml") || f.Status != "modified" || f.Previous != "" {
				return errors.New("routing changed product source")
			}
		}
	}
	var commit struct {
		SHA  string
		Tree struct{ SHA string }
	}
	if e := api("repos/"+repository+"/git/commits/"+sourceHead, &commit); e != nil {
		return e
	}
	if commit.SHA != sourceHead || commit.Tree.SHA != sourceTree {
		return errors.New("actual fixed source tree changed")
	}
	var merge struct {
		SHA     string
		Parents []struct{ SHA string }
	}
	if e := api("repos/"+repository+"/git/commits/"+p.WorkflowSHA, &merge); e != nil {
		return e
	}
	if merge.SHA != p.WorkflowSHA || len(merge.Parents) != 2 || merge.Parents[0].SHA != p.Base || merge.Parents[1].SHA != p.PRHead {
		return errors.New("workflow commit is not this exact PR test merge")
	}
	var wf struct{ Content, Encoding string }
	if e := api("repos/"+repository+"/contents/"+p.Workflow+"?ref="+p.WorkflowSHA, &wf); e != nil {
		return e
	}
	wb, e := base64.StdEncoding.DecodeString(strings.ReplaceAll(wf.Content, "\n", ""))
	if e != nil || wf.Encoding != "base64" || digest(wb) != p.WorkflowDigest {
		return errors.New("actual workflow digest mismatch")
	}
	if e := api("repos/"+repository+"/contents/"+p.Workflow+"?ref="+p.PRHead, &wf); e != nil {
		return e
	}
	wb, e = base64.StdEncoding.DecodeString(strings.ReplaceAll(wf.Content, "\n", ""))
	if e != nil || wf.Encoding != "base64" || digest(wb) != p.WorkflowDigest {
		return errors.New("merge and PR workflow bytes differ")
	}
	var run struct {
		ID             int64
		Attempt        int    `json:"run_attempt"`
		HeadSHA        string `json:"head_sha"`
		Event, Path    string
		HeadBranch     string `json:"head_branch"`
		Repository     struct{ ID int64 }
		HeadRepository struct{ ID int64 }     `json:"head_repository"`
		PullRequests   []struct{ Number int } `json:"pull_requests"`
	}
	if e = api(fmt.Sprintf("repos/%s/actions/runs/%d", repository, p.RunID), &run); e != nil {
		return e
	}
	if run.ID != p.RunID || run.Attempt != p.Attempt || run.HeadSHA != p.PRHead || run.Event != "pull_request" || run.Path != p.Workflow || run.HeadBranch != branch || run.Repository.ID != repositoryID || run.HeadRepository.ID != repositoryID {
		return errors.New("actual run/attempt/source rejected")
	}
	found := false
	for _, pr := range run.PullRequests {
		if pr.Number == 148 {
			found = true
		}
	}
	if !found {
		return errors.New("run is not attached to PR148")
	}
	var runners struct {
		Total   int `json:"total_count"`
		Runners []struct {
			ID   int64
			Name string
		}
	}
	if e = api("repos/"+repository+"/actions/runners?per_page=100", &runners); e != nil {
		return e
	}
	if finished && runners.Total == 1 && len(runners.Runners) == 1 && runners.Runners[0].Name == p.name() {
		return errPending
	}
	if runners.Total != 0 || len(runners.Runners) != 0 {
		return errors.New("repository runner registration remains; retain runtime credentials")
	}
	var jobs struct {
		Total int `json:"total_count"`
		Jobs  []apiJob
	}
	if e = api(fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100", repository, p.RunID, p.Attempt), &jobs); e != nil {
		return e
	}
	if jobs.Total > 100 {
		return errors.New("job listing exceeds finite bound")
	}
	found = false
	for _, j := range jobs.Jobs {
		if j.Name == p.JobName {
			if found {
				return errors.New("duplicate target job")
			}
			found = true
			if e = checkJob(p, j, finished); e != nil {
				return e
			}
		}
	}
	if !found {
		return errors.New("actual target job absent")
	}
	return nil
}

func hook(p Plan, mode string) error {
	if p.Fixture != "" {
		return errors.New("official job hook rejects fixtures")
	}
	// Avoid scanning the checkout or opening runner credentials. Only the event
	// and exact required identity keys enter the context comparison.
	env := map[string]string{}
	for _, k := range []string{"GITHUB_REPOSITORY", "GITHUB_REPOSITORY_ID", "GITHUB_EVENT_NAME", "GITHUB_HEAD_REF", "GITHUB_BASE_REF", "GITHUB_REF", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_JOB", "GITHUB_WORKFLOW_SHA", "GITHUB_WORKFLOW_REF", "RUNNER_NAME"} {
		env[k] = os.Getenv(k)
	}
	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if e := beneath(p.runtime(), eventPath); e != nil {
		return e
	}
	event, e := os.ReadFile(eventPath)
	if e != nil {
		return errors.New("job event unavailable")
	}
	if len(event) > 4<<20 {
		return errors.New("job event too large")
	}
	if e = checkEvent(p, env, event); e != nil {
		return e
	}
	// Hook refuses all jobs without the original shared gate admission. No
	// adapter or added gate-file ACE is silently installed by this helper.
	jobs, jerr := strconv.Atoi(os.Getenv("CARGO_BUILD_JOBS"))
	threads, terr := strconv.Atoi(os.Getenv("RUST_TEST_THREADS"))
	if os.Getenv("CODEX_JOBS_CARGO_LOCK_HELD") != "1" || jerr != nil || terr != nil || jobs < 1 || jobs > 10 || threads < 1 || threads > 10 || os.Getenv("CARGO_TARGET_DIR") != filepath.Join(p.runtime(), "target") || os.Getenv("RUSTC_WRAPPER") != `D:\CC\Tools\sccache\sccache.exe` {
		return errors.New("original shared cargo gate admission missing")
	}
	if e = verifySource(p, os.Getenv("GITHUB_WORKSPACE"), mode != "hook-completed"); e != nil {
		return e
	}
	return saveNew(filepath.Join(p.runtime(), mode+".json"), makeHookReceipt(p, mode, event))
}
func validateHookFiles(p Plan) error {
	var event string
	for _, stage := range []string{"hook-start", "verify-source", "hook-completed"} {
		b, e := os.ReadFile(filepath.Join(p.runtime(), stage+".json"))
		if e != nil {
			return errors.New("required hook/source-guard evidence missing")
		}
		r, e := validateHookData(p, stage, b)
		if e != nil {
			return e
		}
		if event != "" && r.EventDigest != event {
			return errors.New("hook event evidence changed")
		}
		event = r.EventDigest
	}
	return nil
}

func verifySource(p Plan, source string, pristine bool) error {
	if !samePath(source, p.source()) {
		return errors.New("checkout must occupy the fixed workspace path")
	}
	run := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c := exec.CommandContext(ctx, `C:\Program Files\Git\cmd\git.exe`, append([]string{"--no-optional-locks", "-c", "safe.directory=" + source, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=NUL", "-C", source}, args...)...)
		hidden(c)
		c.Env = envFor(p)
		var out bytes.Buffer
		c.Stdout = &out
		if e := c.Run(); e != nil {
			return nil, errors.New("source Git verification failed")
		}
		return out.Bytes(), nil
	}
	keys, e := run("config", "--local", "--no-includes", "--name-only", "--list")
	if e != nil {
		return e
	}
	for _, key := range strings.Fields(strings.ToLower(string(keys))) {
		if blockedGitKey(key) {
			return errors.New("checkout credential/execution config key rejected")
		}
	}
	for _, row := range []struct{ arg, want string }{{"HEAD", p.Head}, {"HEAD^{tree}", p.Tree}} {
		b, e := run("rev-parse", "--verify", row.arg)
		if e != nil || strings.TrimSpace(string(b)) != row.want {
			return errors.New("actual checkout HEAD/tree mismatch")
		}
	}
	b, e := run("write-tree")
	if e != nil || strings.TrimSpace(string(b)) != p.Tree {
		return errors.New("checkout index differs from source")
	}
	b, e = run("diff", "--no-ext-diff", "--no-textconv", "--name-only", "HEAD", "--")
	if e != nil || len(b) != 0 {
		return errors.New("tracked checkout bytes changed")
	}
	if pristine {
		b, e = run("status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored")
		if e != nil || len(b) != 0 {
			return errors.New("pre-execution checkout contains extra or ignored files")
		}
	}
	// Inspect config key names, never their credential values.
	b, e = run("config", "--local", "--name-only", "--list")
	if e != nil {
		return e
	}
	for _, key := range strings.Fields(strings.ToLower(string(b))) {
		if blockedGitKey(key) {
			return errors.New("checkout credential/config key rejected")
		}
	}
	b, e = run("remote", "get-url", "origin")
	if e != nil || strings.TrimSpace(string(b)) != "https://github.com/"+repository {
		return errors.New("checkout remote is not credential-free same-repository HTTPS")
	}
	for _, name := range []string{".git-credentials", ".netrc", "_netrc"} {
		if _, e = os.Lstat(filepath.Join(source, name)); !os.IsNotExist(e) {
			return errors.New("checkout credential file present or uncertain")
		}
	}
	return nil
}
