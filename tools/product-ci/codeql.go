package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// This credential-free SDK is the already verified immutable image, not its
// resident container. No resident runtime, profile or registration is copied.
const codeQLSDK = "sha256:89988fa8449f89b0d8f12fd2369aa5b167bfe3389189df877808a15055869011"

type codeQLSetup struct {
	State       string   `json:"state"`
	Languages   []string `json:"languages"`
	QuerySuite  string   `json:"query_suite"`
	ThreatModel string   `json:"threat_model"`
	RunnerType  string   `json:"runner_type"`
	RunnerLabel string   `json:"runner_label"`
}

type codeQLPlan struct {
	RepoID    int64  `json:"repo_id"`
	PR        int    `json:"pr"`
	Candidate string `json:"candidate_sha"`
	RunID     int64  `json:"run_id"`
	Attempt   int    `json:"attempt"`
	SHA       string `json:"source_sha"`
	Tree      string `json:"source_tree"`
	Role      string `json:"role"`
	Label     string `json:"label"`
	// Repo is only transferred in memory to the guest's private tmpfs. It is
	// intentionally excluded from every host receipt and diagnostic.
	Repo string `json:"-"`
}

type codeQLRun struct {
	ID         int64 `json:"id"`
	RunAttempt int   `json:"run_attempt"`
	Path       string
	Event      string
	HeadSHA    string `json:"head_sha"`
	HeadBranch string `json:"head_branch"`
	Status     string
	Repository repository
	HeadRepo   repository `json:"head_repository"`
}

func codeQLLabel(id int64) string { return fmt.Sprintf("cc-product-2648-%d-codeql", id) }

// Dynamic default-setup runs are the only supported jobs in this mode. An
// advanced workflow is never enabled alongside default setup or rewritten here.
func planCodeQL(ctx context.Context, c candidate, runID int64, read api) (codeQLPlan, error) {
	r, e := verify(ctx, c, read)
	if e != nil {
		return codeQLPlan{}, e
	}
	var s codeQLSetup
	if e = read(ctx, "repos/"+r.FullName+"/code-scanning/default-setup", &s); e != nil {
		return codeQLPlan{}, e
	}
	if s.State != "configured" || s.RunnerType != "labeled" || s.RunnerLabel != codeQLLabel(c.ID) {
		return codeQLPlan{}, errors.New("dedicated default-setup route required")
	}
	var run codeQLRun
	if e = read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d", r.FullName, runID), &run); e != nil {
		return codeQLPlan{}, e
	}
	if run.ID != runID || run.RunAttempt < 1 || run.Repository.ID != c.ID || run.HeadRepo.ID != c.ID || run.HeadRepo.Fork ||
		run.Repository.FullName != r.FullName || run.HeadRepo.FullName != r.FullName ||
		run.Path != "dynamic/github-code-scanning/codeql" || run.Event != "dynamic" ||
		(run.Status != "queued" && run.Status != "in_progress" && run.Status != "waiting") {
		return codeQLPlan{}, errors.New("CodeQL run identity or state rejected")
	}
	p := codeQLPlan{RepoID: c.ID, PR: c.PR, Candidate: c.SHA, RunID: runID, Attempt: run.RunAttempt, SHA: run.HeadSHA, Label: s.RunnerLabel, Repo: r.FullName, Role: "candidate"}
	if run.HeadSHA != c.SHA {
		// Updating default setup first validates the current default branch.
		// This is explicitly separate evidence, never candidate acceptance.
		var meta struct {
			DefaultBranch string `json:"default_branch"`
		}
		if e = read(ctx, fmt.Sprintf("repositories/%d", c.ID), &meta); e != nil {
			return codeQLPlan{}, e
		}
		if meta.DefaultBranch == "" || run.HeadBranch != meta.DefaultBranch {
			return codeQLPlan{}, errors.New("unapproved setup source rejected")
		}
		var ref struct{ Object struct{ SHA string } }
		if e = read(ctx, "repos/"+r.FullName+"/git/ref/heads/"+meta.DefaultBranch, &ref); e != nil || ref.Object.SHA != run.HeadSHA {
			return codeQLPlan{}, errors.New("obsolete setup source rejected")
		}
		p.Role = "setup-validation"
	}
	if len(p.SHA) != 40 || strings.Trim(p.SHA, "0123456789abcdef") != "" {
		return codeQLPlan{}, errors.New("source revision rejected")
	}
	var commit struct{ Tree struct{ SHA string } }
	if e = read(ctx, "repos/"+r.FullName+"/git/commits/"+p.SHA, &commit); e != nil {
		return codeQLPlan{}, e
	}
	if len(commit.Tree.SHA) != 40 || strings.Trim(commit.Tree.SHA, "0123456789abcdef") != "" {
		return codeQLPlan{}, errors.New("source tree unavailable")
	}
	p.Tree = commit.Tree.SHA
	return p, nil
}

func mutateJSON(ctx context.Context, method, endpoint string, body any, out any) error {
	b, e := json.Marshal(body)
	if e != nil {
		return e
	}
	c := cmd(ctx, gh, "api", "--method", method, endpoint, "--input", "-")
	c.Stdin = bytes.NewReader(b)
	data, e := boundedOutput(c, 1<<20)
	if e != nil {
		return errors.New("GitHub mutation failed; do not retry through another route")
	}
	if out != nil && json.Unmarshal(data, out) != nil {
		return errors.New("GitHub mutation response unverified; preserve recovery record")
	}
	return nil
}

// Only runner fields are sent. In particular, the API's language enumeration
// must never silently remove Rust or change the original query/threat settings.
func routeCodeQL(ctx context.Context, c candidate, dir string) error {
	r, e := verify(ctx, c, get)
	if e != nil {
		return e
	}
	var prior codeQLSetup
	if e = get(ctx, "repos/"+r.FullName+"/code-scanning/default-setup", &prior); e != nil {
		return e
	}
	if prior.State != "configured" || prior.RunnerType != "standard" || prior.RunnerLabel != "" {
		return errors.New("default setup is not on the original standard route")
	}
	path := filepath.Join(dir, fmt.Sprintf("repo-%d-codeql-route.private.json", c.ID))
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("new route receipt required")
	}
	record := struct {
		RepoID int64       `json:"repo_id"`
		SHA    string      `json:"candidate_sha"`
		Prior  codeQLSetup `json:"prior"`
		Label  string      `json:"label"`
		Stage  string      `json:"stage"`
	}{c.ID, c.SHA, prior, codeQLLabel(c.ID), "intent-before-mutation"}
	e = json.NewEncoder(f).Encode(record)
	if e == nil {
		e = f.Sync()
	}
	e = errors.Join(e, f.Close())
	if e != nil {
		return errors.New("route intent not durable")
	}
	var response struct {
		RunID int64 `json:"run_id"`
	}
	if e = mutateJSON(ctx, "PATCH", "repos/"+r.FullName+"/code-scanning/default-setup", map[string]string{"runner_type": "labeled", "runner_label": codeQLLabel(c.ID)}, &response); e != nil {
		return e
	}
	b, _ := json.MarshalIndent(struct {
		RepoID int64 `json:"repo_id"`
		RunID  int64 `json:"run_id"`
	}{c.ID, response.RunID}, "", "  ")
	if e = os.WriteFile(filepath.Join(dir, "codeql-route-response.private.json"), b, 0600); e != nil {
		return e
	}
	var after codeQLSetup
	if e = get(ctx, "repos/"+r.FullName+"/code-scanning/default-setup", &after); e != nil {
		return e
	}
	if after.State != prior.State || after.QuerySuite != prior.QuerySuite || after.ThreatModel != prior.ThreatModel || strings.Join(after.Languages, "\x00") != strings.Join(prior.Languages, "\x00") || after.RunnerType != "labeled" || after.RunnerLabel != codeQLLabel(c.ID) {
		return errors.New("route update not confirmed without acceptance changes")
	}
	return nil
}

type runnerEntry struct {
	ID     int64
	Name   string
	Busy   bool
	Labels []struct{ Name string }
}

func repoRunners(ctx context.Context, repo string) ([]runnerEntry, error) {
	var r struct {
		Total   int `json:"total_count"`
		Runners []runnerEntry
	}
	if e := get(ctx, "repos/"+repo+"/actions/runners?per_page=100", &r); e != nil {
		return nil, e
	}
	if r.Total != len(r.Runners) {
		return nil, errors.New("runner inventory incomplete")
	}
	return r.Runners, nil
}

func codeQLIsolation(b []byte) error {
	var all []isolation
	if json.Unmarshal(b, &all) != nil || len(all) != 1 {
		return errors.New("CodeQL isolation unavailable")
	}
	i := all[0]
	h := i.HostConfig
	if i.Config.Image != codeQLSDK || i.Config.User != "1001:1001" || i.Config.Labels["cc.todo"] != "2648" || h.Init == nil || !*h.Init || h.Privileged || !h.ReadonlyRootfs || h.NetworkMode != "bridge" || h.PidMode != "" || len(h.Binds) != 0 || len(h.CapAdd) != 0 || len(h.Devices) != 0 || len(h.DeviceRequests) != 0 || h.NanoCPUs != 2000000000 || h.Memory != 8<<30 || h.PidsLimit == nil || *h.PidsLimit != 512 {
		return errors.New("CodeQL isolation rejected")
	}
	if len(h.CapDrop) != 1 || !strings.EqualFold(h.CapDrop[0], "ALL") || len(h.SecurityOpt) != 1 || (h.SecurityOpt[0] != "no-new-privileges" && h.SecurityOpt[0] != "no-new-privileges=true") {
		return errors.New("CodeQL privileges rejected")
	}
	if len(h.Tmpfs) != 4 || !strings.Contains(h.Tmpfs["/tmp"], "noexec") {
		return errors.New("CodeQL scratch rejected")
	}
	for _, p := range []string{"/runner", "/work", "/tmp", "/home/runner"} {
		if !strings.Contains(h.Tmpfs[p], "uid=1001") {
			return errors.New("CodeQL scratch owner rejected")
		}
	}
	for _, m := range i.Mounts {
		if m.Type != "tmpfs" || (m.Destination != "/runner" && m.Destination != "/work" && m.Destination != "/tmp" && m.Destination != "/home/runner") {
			return errors.New("CodeQL host mount rejected")
		}
	}
	return nil
}

func codeQLCreateArgs(name string) []string {
	return []string{"create", "--name", name, "--label", "cc.todo=2648", "--label", "cc.instance=" + name, "--init", "--user", "1001:1001", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--read-only", "--network", "bridge", "--cpus", "2", "--memory", "8g", "--pids-limit", "512", "--tmpfs", "/runner:rw,nosuid,size=4g,uid=1001,gid=1001", "--tmpfs", "/work:rw,nosuid,size=16g,uid=1001,gid=1001", "--tmpfs", "/tmp:rw,nosuid,noexec,size=2g,uid=1001,gid=1001", "--tmpfs", "/home/runner:rw,nosuid,size=1g,uid=1001,gid=1001", codeQLSDK, "sleep", "infinity"}
}

// Host evidence never claims job success from runner exit: the official run,
// logs, uploads and completed-hook checkout must all be assessed separately.
func executeCodeQL(ctx context.Context, c candidate, runID int64, dir, guestBinary string) (ret error) {
	p, e := planCodeQL(ctx, c, runID, get)
	if e != nil {
		return e
	}
	guest, e := os.ReadFile(guestBinary)
	if e != nil || len(guest) < 64 || !bytes.Equal(guest[:4], []byte{127, 'E', 'L', 'F'}) {
		return errors.New("Linux helper binary required")
	}
	rec := receipt{RepoID: c.ID, PR: c.PR, SourceSHA: p.SHA, Stage: "codeql-planned", At: time.Now().UTC(), Scope: p.Role, Image: codeQLSDK}
	rec.SourceTree = p.Tree
	reservation, e := os.OpenFile(filepath.Join(dir, fmt.Sprintf("repo-%d-result.private.json", c.ID)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("new CodeQL receipt directory required")
	}
	if e = reservation.Close(); e != nil {
		return e
	}
	defer func() {
		if ret != nil {
			rec.Failure = ret.Error()
		}
		ret = errors.Join(ret, writeReceipt(dir, rec))
	}()
	planBytes, _ := json.MarshalIndent(p, "", "  ")
	planFile, e := os.OpenFile(filepath.Join(dir, "codeql-plan.private.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("new plan evidence required")
	}
	_, e = planFile.Write(planBytes)
	if e == nil {
		e = planFile.Sync()
	}
	e = errors.Join(e, planFile.Close())
	if e != nil {
		return e
	}
	lockPath := filepath.Join(os.TempDir(), fmt.Sprintf("todo2648-repo-%d.lock", c.ID))
	lock, e := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("candidate already occupied")
	}
	registrationMayExist, registrationGone := false, false
	defer func() {
		ret = errors.Join(ret, lock.Close())
		if (!rec.CreationUncertain || rec.ContainerRemoved) && (!registrationMayExist || registrationGone) {
			if e := os.Remove(lockPath); e != nil {
				ret = errors.Join(ret, e)
			} else {
				rec.LockReleased = true
			}
		}
	}()
	runners, e := repoRunners(ctx, p.Repo)
	if e != nil {
		return e
	}
	for _, r := range runners {
		for _, l := range r.Labels {
			if l.Name == p.Label {
				return errors.New("dedicated label already has a runner")
			}
		}
	}
	name := fmt.Sprintf("todo2648-repo-%d-codeql-%d", c.ID, time.Now().UnixNano())
	rec.ContainerName = name
	rec.CreationUncertain = true
	if e = writeReceipt(dir, rec); e != nil {
		return e
	}
	b, e := cmd(ctx, docker, codeQLCreateArgs(name)...).Output()
	// Even an unsuccessful create can have reached the daemon. Resolve only the
	// recorded nonce, image and ownership; retain the lock if it is uncertain.
	var found []struct {
		ID, Name string
		Config   struct {
			Image  string
			Labels map[string]string
		}
	}
	probe, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	inspect, ie := cmd(probe, docker, "inspect", name).Output()
	if ie != nil || json.Unmarshal(inspect, &found) != nil || len(found) != 1 || found[0].Name != "/"+name || found[0].Config.Image != codeQLSDK || found[0].Config.Labels["cc.instance"] != name || found[0].Config.Labels["cc.todo"] != "2648" {
		return errors.New("CodeQL create uncertain; retain admission")
	}
	rec.ContainerID = found[0].ID
	defer func() {
		if !rec.ContainerRemoved {
			var ce error
			rec.TreeGone, rec.ContainerRemoved, ce = disposeOwned(rec.ContainerID)
			ret = errors.Join(ret, ce)
		}
	}()
	if e != nil || strings.TrimSpace(string(b)) != rec.ContainerID {
		return errors.New("CodeQL create completion uncertain")
	}
	if e = codeQLIsolation(inspect); e != nil {
		return e
	}
	rec.IsolationVerified = true
	if e = cmd(ctx, docker, "start", rec.ContainerID).Run(); e != nil {
		return errors.New("CodeQL container start failed")
	}
	// Stdin transfers only this reviewed helper, never product source or a
	// host directory. The target is fresh tmpfs, not an image or resident runtime.
	transfer := cmd(ctx, docker, "exec", "-i", rec.ContainerID, "sh", "-c", "cat > /runner/product-ci && chmod 755 /runner/product-ci")
	transfer.Stdin = bytes.NewReader(guest)
	if e = transfer.Run(); e != nil {
		return errors.New("guest helper transfer failed")
	}
	p2, e := planCodeQL(ctx, c, runID, get)
	if e != nil || p2 != p {
		return errors.New("CodeQL plan changed before registration")
	}
	callback, key, closeAdmission, e := startCodeQLAdmission(ctx, p, c)
	if e != nil {
		return e
	}
	defer func() { ret = errors.Join(ret, closeAdmission()) }()
	var token struct{ Token string }
	if e = mutateJSON(ctx, "POST", "repos/"+p.Repo+"/actions/runners/registration-token", struct{}{}, &token); e != nil {
		return e
	}
	if token.Token == "" {
		return errors.New("runner registration token absent")
	}
	// The official runner automatically deregisters after one job. On failure,
	// remove only the exact unique name in this same verified repo, after stop.
	defer func() {
		if !rec.TreeGone { // Cleanup defer executes next; stop now before REST removal.
			var ce error
			rec.TreeGone, rec.ContainerRemoved, ce = disposeOwned(rec.ContainerID)
			ret = errors.Join(ret, ce)
		}
		if !rec.TreeGone {
			return
		}
		cc, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		rs, ce := repoRunners(cc, p.Repo)
		if ce != nil {
			ret = errors.Join(ret, ce)
			return
		}
		for _, r := range rs {
			if r.Name == name {
				ce = mutateJSON(cc, "DELETE", fmt.Sprintf("repos/%s/actions/runners/%d", p.Repo, r.ID), nil, nil)
				ret = errors.Join(ret, ce)
			}
		}
		rs, ce = repoRunners(cc, p.Repo)
		if ce != nil {
			ret = errors.Join(ret, ce)
			return
		}
		for _, r := range rs {
			if r.Name == name {
				ret = errors.Join(ret, errors.New("ephemeral registration removal unconfirmed; admission retained"))
				return
			}
		}
		registrationGone = true
	}()
	packet := struct {
		Plan                             codeQLPlan
		Repo, Token, Name, Callback, Key string
	}{p, p.Repo, token.Token, name, callback, key}
	input, e := json.Marshal(packet)
	token.Token = ""
	packet.Token = ""
	if e != nil {
		return e
	}
	log, e := os.OpenFile(filepath.Join(dir, fmt.Sprintf("repo-%d-codeql-runner.private.log", c.ID)), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	run := cmd(ctx, docker, "exec", "-i", rec.ContainerID, "/runner/product-ci", "codeql-guest")
	run.Stdin = bytes.NewReader(input)
	run.Stdout = log
	run.Stderr = log
	registrationMayExist = true
	e = run.Run()
	for i := range input {
		input[i] = 0
	}
	closeErr := log.Close()
	ret = errors.Join(ret, closeErr)
	code := 0
	if e != nil {
		code = 1
	}
	rec.Exit = &code
	logBytes, le := os.ReadFile(log.Name())
	if le != nil {
		return le
	}
	rec.LogSHA = digest(logBytes)
	rec.LogBytes = int64(len(logBytes))
	if e != nil {
		return errors.New("ephemeral runner failed; inspect private original log")
	}
	b, e = cmd(ctx, docker, "exec", rec.ContainerID, "cat", "/runner/checkout-proof.json").Output()
	var proof struct {
		SHA, Tree string
		RunID     int64
		Attempt   int
	}
	if e != nil || json.Unmarshal(b, &proof) != nil || proof.SHA != p.SHA || proof.Tree != p.Tree || proof.RunID != p.RunID || proof.Attempt != p.Attempt {
		return errors.New("actual checkout evidence unavailable or mismatched")
	}
	rec.SourceTreeVerified = true
	if e = os.WriteFile(filepath.Join(dir, "codeql-checkout.private.json"), b, 0600); e != nil {
		return e
	}
	if _, e = verify(ctx, c, get); e != nil {
		return e
	}
	rec.Stage = "ephemeral-job-ended-official-results-still-required"
	return nil
}

func codeQLGuest() error {
	var packet struct {
		Plan                             codeQLPlan
		Repo, Token, Name, Callback, Key string
	}
	if e := json.NewDecoder(io.LimitReader(os.Stdin, 64<<10)).Decode(&packet); e != nil {
		return errors.New("guest input invalid")
	}
	p := packet.Plan
	p.Repo = packet.Repo
	if p.Label != codeQLLabel(p.RepoID) || !strings.HasPrefix(packet.Name, fmt.Sprintf("todo2648-repo-%d-codeql-", p.RepoID)) || strings.Count(p.Repo, "/") != 1 || packet.Token == "" {
		return errors.New("guest registration rejected")
	}
	if e := cmd(context.Background(), "cp", "-a", "/opt/actions-runner/.", "/runner/").Run(); e != nil {
		return e
	}
	private, _ := json.Marshal(struct {
		Plan                codeQLPlan
		Repo, Callback, Key string
	}{p, p.Repo, packet.Callback, packet.Key})
	if e := os.WriteFile("/home/runner/product-plan.json", private, 0600); e != nil {
		return e
	}
	for _, stage := range []string{"start", "complete"} {
		if e := os.WriteFile("/home/runner/product-hook-"+stage+".sh", []byte("#!/bin/sh\nexec /runner/product-ci codeql-hook-"+stage+"\n"), 0700); e != nil {
			return e
		}
	}
	config := cmd(context.Background(), "/runner/config.sh", "--unattended", "--url", "https://github.com/"+p.Repo, "--name", packet.Name, "--labels", p.Label, "--no-default-labels", "--work", "/work", "--ephemeral", "--disableupdate")
	config.Dir = "/runner"
	config.Env = append(os.Environ(), "ACTIONS_RUNNER_INPUT_TOKEN="+packet.Token)
	// Configuration output (including failure diagnostics) is never forwarded.
	config.Stdout = io.Discard
	config.Stderr = io.Discard
	e := config.Run()
	packet.Token = ""
	config.Env = nil
	if e != nil {
		return errors.New("official runner configuration failed")
	}
	run := cmd(context.Background(), "/runner/run.sh")
	run.Dir = "/runner"
	// Exclude /opt/bridge and Rust from PATH: this no-build mode has no Cargo
	// admission. A native build is outside its scope, never an ungated fallback.
	run.Env = append(os.Environ(), "PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "HOME=/home/runner", "CODEQL_THREADS=2", "CODEQL_RAM=6144", "ACTIONS_RUNNER_HOOK_JOB_STARTED=/home/runner/product-hook-start.sh", "ACTIONS_RUNNER_HOOK_JOB_COMPLETED=/home/runner/product-hook-complete.sh")
	run.Stdout = os.Stdout
	run.Stderr = os.Stderr
	return run.Run()
}

func checkCodeQLHook(p codeQLPlan, repo, sha, runID, attempt string) error {
	if repo != p.Repo || sha != p.SHA || runID != strconv.FormatInt(p.RunID, 10) || attempt != strconv.Itoa(p.Attempt) {
		return errors.New("CodeQL job identity rejected")
	}
	return nil
}

func codeQLHook(complete bool) error {
	b, e := os.ReadFile("/home/runner/product-plan.json")
	if e != nil {
		return errors.New("private plan unavailable")
	}
	var packet struct {
		Plan                codeQLPlan
		Repo, Callback, Key string
	}
	if json.Unmarshal(b, &packet) != nil {
		return errors.New("private plan invalid")
	}
	p := packet.Plan
	p.Repo = packet.Repo
	if e = checkCodeQLHook(p, os.Getenv("GITHUB_REPOSITORY"), os.Getenv("GITHUB_SHA"), os.Getenv("GITHUB_RUN_ID"), os.Getenv("GITHUB_RUN_ATTEMPT")); e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	if e = requestCodeQLAdmission(ctx, p, packet.Callback, packet.Key); e != nil {
		return e
	}
	if !complete {
		return nil
	}
	_, name, _ := strings.Cut(p.Repo, "/")
	want := "/work/" + name + "/" + name
	if os.Getenv("GITHUB_WORKSPACE") != want {
		return errors.New("checkout location rejected")
	}
	git := cmd(context.Background(), "git", "-C", want, "rev-parse", "HEAD")
	b, e = git.Output()
	if e != nil || strings.TrimSpace(string(b)) != p.SHA {
		return errors.New("actual checkout head rejected")
	}
	b, e = cmd(context.Background(), "git", "-C", want, "rev-parse", "HEAD^{tree}").Output()
	if e != nil || strings.TrimSpace(string(b)) != p.Tree {
		return errors.New("actual checkout tree rejected")
	}
	// actions/checkout's post-job step must have removed its temporary auth.
	b, e = cmd(context.Background(), "git", "-C", want, "config", "--local", "--get-regexp", `http\..*\.extraheader|credential\..*`).Output()
	if len(b) != 0 || (e != nil && exitStatus(e) != 1) {
		return errors.New("checkout authentication removal unconfirmed")
	}
	proof, _ := json.Marshal(struct {
		SHA, Tree string
		RunID     int64
		Attempt   int
	}{p.SHA, p.Tree, p.RunID, p.Attempt})
	return os.WriteFile("/runner/checkout-proof.json", proof, 0600)
}

func exitStatus(e error) int {
	if e == nil {
		return 0
	}
	type exitCoder interface{ ExitCode() int }
	if x, ok := e.(exitCoder); ok {
		return x.ExitCode()
	}
	return -1
}
