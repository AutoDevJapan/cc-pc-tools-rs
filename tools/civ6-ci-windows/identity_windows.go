//go:build windows && amd64

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	return apiReadLimit(parent, path, v, 4<<20)
}
func apiReadLimit(parent context.Context, path string, v any, limit int64) error {
	b, err := retryGitHubRead(parent, func() ([]byte, error) {
		return apiReadAttempt(parent, path, limit)
	}, waitGitHubRead)
	if err != nil {
		return &apiReadError{fmt.Errorf("GitHub read request %x: %w", sha256.Sum256([]byte(path)), err)}
	}
	return json.Unmarshal(b, v)
}

// apiReadError marks a failed GitHub read (as opposed to a policy mismatch) so
// the run monitor can tolerate a brief outage without accepting a wrong identity.
type apiReadError struct{ err error }

func (e *apiReadError) Error() string { return e.err.Error() }
func (e *apiReadError) Unwrap() error { return e.err }

type apiErrorPreview struct{ buffer bytes.Buffer }

func (b *apiErrorPreview) String() string { return b.buffer.String() }

func (b *apiErrorPreview) Write(p []byte) (int, error) {
	n := len(p)
	if left := 8192 - b.buffer.Len(); left > 0 {
		if len(p) > left {
			p = p[:left]
		}
		_, _ = b.buffer.Write(p)
	}
	return n, nil
}

func apiReadAttempt(parent context.Context, path string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, `D:\CC\Tools\codex-jobs\gh_gate.exe`, "api", path)
	hidden(c)
	preview := &apiErrorPreview{}
	c.Stderr = preview
	// gh uses its existing OAuth store. Never request auth-token, dump config,
	// pass a credential in argv, or forward the owner's env to the service.
	b, overflow, waitErr := captureGitHubRead(c, limit, cancel)
	if overflow {
		return nil, errors.New("GitHub read response exceeds limit")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if waitErr != nil {
		// No server text, source bytes, endpoint or credential is returned or
		// persisted. Auth/policy/size failures never enter the retry path.
		status := "unclassified"
		text := strings.ToLower(preview.String())
		for _, code := range []string{"401", "403", "404", "429", "500", "502", "503", "504"} {
			if strings.Contains(text, "http "+code) {
				status = code
				break
			}
		}
		exit := -1
		if c.ProcessState != nil {
			exit = c.ProcessState.ExitCode()
		}
		if transientGitHubRead(preview.String()) && !errors.Is(waitErr, exec.ErrWaitDelay) {
			return nil, fmt.Errorf("%w (bytes=%d exit=%d status=%s)", errGitHubReadTransport, len(b), exit, status)
		}
		return nil, fmt.Errorf("GitHub read API failed (bytes=%d exit=%d status=%s pipe_timeout=%t)", len(b), exit, status, errors.Is(waitErr, exec.ErrWaitDelay))
	}
	return b, nil
}

// auditPhase selects which registration state is legitimate for this unique
// ephemeral runner: absent before registration, present or absent while the
// job runs, and absent again once the official job is terminal.
type auditPhase int

const (
	phaseBefore auditPhase = iota
	phaseRunning
	phaseFinished
)

func audit(p Plan) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	return auditMode(ctx, p, phaseBefore)
}

// auditRunning re-verifies the fixed plan while the official job runs. It is the
// hook-start and hook-complete cross-check made by the controller, which alone
// holds the GitHub OAuth store.
func auditRunning(p Plan) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	return auditMode(ctx, p, phaseRunning)
}
func auditCollection(p Plan) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	return checkCollectionRemote(p, func(path string, v any) error { return apiWithContext(ctx, path, v) })
}
func auditFinished(p Plan) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for {
		e := auditMode(ctx, p, phaseFinished)
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

type apiRunner struct {
	ID     int64
	Name   string
	Labels []struct{ Name string }
}

// ownRunner lists the repository's runners and returns only this plan's unique
// registration. Other runners (the long-lived Linux/Windows workers and the
// Linux ephemeral runner) are neither inspected nor affected.
func ownRunner(p Plan, read readAPI) (*apiRunner, error) {
	var runners struct {
		Total   int `json:"total_count"`
		Runners []apiRunner
	}
	if e := read("repos/"+repository+"/actions/runners?per_page=100", &runners); e != nil {
		return nil, e
	}
	if runners.Total != len(runners.Runners) || runners.Total > 100 {
		return nil, errors.New("complete repository runner inventory required")
	}
	var own *apiRunner
	for i, r := range runners.Runners {
		if r.Name == p.name() {
			if own != nil {
				return nil, errors.New("duplicate unique runner registration")
			}
			own = &runners.Runners[i]
		}
	}
	return own, nil
}

func auditMode(ctx context.Context, p Plan, phase auditPhase) error {
	api := func(path string, v any) error { return apiWithContext(ctx, path, v) }
	return auditWith(ctx, p, phase, api, verifyWith)
}

// auditWith has no hidden network or policy dependency, so every registration
// and job-state decision can be tested against synthetic API replies.
func auditWith(ctx context.Context, p Plan, phase auditPhase, api readAPI, verify func(context.Context, Plan, readAPI) error) error {
	if e := p.validate(); e != nil {
		return e
	}
	if p.Fixture != "" {
		return errors.New("fixture is not API or runner identity evidence")
	}
	if e := verify(ctx, p, api); e != nil {
		return e
	}
	own, e := ownRunner(p, api)
	if e != nil {
		return e
	}
	switch {
	case own != nil && phase == phaseFinished:
		return errPending
	case own != nil && phase == phaseBefore:
		return errors.New("unique runner registration already exists; retain runtime credentials")
	case own != nil:
		var names []string
		for _, l := range own.Labels {
			names = append(names, l.Name)
		}
		if !sameLabels(names, p.requiredLabels()) {
			return errors.New("registered runner labels differ from the reviewed dedicated set")
		}
	}
	var jobs struct {
		Total int `json:"total_count"`
		Jobs  []apiJob
	}
	if e := api(fmt.Sprintf("repos/%s/actions/runs/%d/attempts/%d/jobs?per_page=100", repository, p.RunID, p.Attempt), &jobs); e != nil {
		return e
	}
	if jobs.Total != len(jobs.Jobs) {
		return errors.New("complete job inventory required")
	}
	found := false
	for _, j := range jobs.Jobs {
		if j.ID == p.JobID {
			if found {
				return errors.New("duplicate target job")
			}
			found = true
			if e := checkJob(p, j, phase == phaseFinished); e != nil {
				return e
			}
			if phase == phaseBefore && (j.Status != "queued" || j.Runner != "") {
				return errors.New("target job is no longer waiting for a runner")
			}
		}
	}
	if !found {
		return errors.New("actual target job absent")
	}
	return nil
}

// jobProgress reports how far the one official target job has advanced, for
// the controller's finite monitor. It fails closed on any other runner.
func jobProgress(ctx context.Context, p Plan) (status string, runner string, err error) {
	var j apiJob
	if e := apiWithContext(ctx, fmt.Sprintf("repos/%s/actions/jobs/%d", repository, p.JobID), &j); e != nil {
		return "", "", e
	}
	if j.ID != p.JobID || j.RunID != p.RunID || j.Attempt != p.Attempt || j.Name != p.JobName || (j.Runner != "" && j.Runner != p.name()) {
		return "", "", errors.New("target job identity or runner assignment changed")
	}
	return j.Status, j.Runner, nil
}

func hook(p Plan, mode string) error {
	if p.Fixture != "" {
		return errors.New("official job hook rejects fixtures")
	}
	// Avoid scanning the checkout or opening runner credentials. Only the event
	// and exact required identity keys enter the context comparison.
	env := map[string]string{}
	for _, k := range []string{"GITHUB_REPOSITORY", "GITHUB_REPOSITORY_ID", "GITHUB_EVENT_NAME", "GITHUB_SHA", "RUNNER_OS", "GITHUB_HEAD_REF", "GITHUB_BASE_REF", "GITHUB_REF", "GITHUB_RUN_ID", "GITHUB_RUN_ATTEMPT", "GITHUB_JOB", "GITHUB_WORKFLOW_SHA", "GITHUB_WORKFLOW_REF", "RUNNER_NAME"} {
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
	if os.Getenv("CODEX_JOBS_CARGO_LOCK_HELD") != "1" || jerr != nil || terr != nil || jobs < 1 || jobs > 10 || threads < 1 || threads > 10 || os.Getenv("CARGO_TARGET_DIR") != p.target() || os.Getenv("RUSTC_WRAPPER") != `D:\CC\Tools\sccache\sccache.exe` {
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
	return verifySourceOrigin(p, source, pristine, "https://github.com/"+repository)
}
func verifySourceOrigin(p Plan, source string, pristine bool, origin string) error {
	if !samePath(source, p.source()) {
		return errors.New("checkout must occupy the fixed workspace path")
	}
	if e := sourceMetadata(source); e != nil {
		return e
	}
	run := func(args ...string) ([]byte, error) { return sourceGit(source, args...) }
	keys, e := run("config", "--local", "--no-includes", "--name-only", "--list")
	if e != nil {
		return e
	}
	for _, key := range strings.Fields(strings.ToLower(string(keys))) {
		if blockedGitKey(key) {
			return errors.New("checkout credential/execution config key rejected")
		}
	}
	if e := verifyTrackedBytes(source, p.Tree); e != nil {
		return e
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
	if e != nil || strings.TrimSpace(string(b)) != origin {
		return errors.New("checkout remote is not credential-free same-repository HTTPS")
	}
	global := filepath.Join(p.runtime(), "home", ".gitconfig")
	if _, err := os.Lstat(global); err == nil {
		keys, err := run("config", "--file", global, "--no-includes", "--name-only", "--list")
		if err != nil {
			return err
		}
		for _, key := range strings.Fields(strings.ToLower(string(keys))) {
			if blockedGitKey(key) {
				return errors.New("service Git global credential/config key rejected")
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, name := range []string{".git-credentials", ".netrc", "_netrc"} {
		if _, e = os.Lstat(filepath.Join(source, name)); !os.IsNotExist(e) {
			return errors.New("checkout credential file present or uncertain")
		}
	}
	return nil
}
