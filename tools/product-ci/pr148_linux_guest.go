package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"
)

type pr148GuestPacket struct {
	Plan                               pr148LinuxPlan
	Policy                             pr148CargoPolicy
	Repo, Name, Token, Callback, Nonce string
	Execution                          pr148ExecutionPolicy
}

func pr148ToolEnv(jobs, tests int) []string {
	return []string{"CI=true", "GITHUB_ACTIONS=true", "TMPDIR=/work/tool-tmp", "HOME=/home/runner", "PATH=/toolchain/rustup/toolchains/1.95.0-x86_64-unknown-linux-gnu/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "CARGO_HOME=/toolchain/cargo", "RUSTUP_HOME=/toolchain/rustup", "CARGO_TARGET_DIR=" + pr148Workspace + "/tools/target", "CARGO_BUILD_JOBS=" + strconv.Itoa(jobs), "RUST_TEST_THREADS=" + strconv.Itoa(tests), "NEXTEST_TEST_THREADS=" + strconv.Itoa(tests), "CARGO=/runner/bin/cargo", "RUSTUP_TOOLCHAIN=1.95.0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_NO_REPLACE_OBJECTS=1"}
}
func pr148ReadGuest() (pr148GuestPacket, error) {
	var p pr148GuestPacket
	b, e := os.ReadFile("/runner/pr148-plan.json")
	if e != nil || json.Unmarshal(b, &p) != nil || !p.Plan.valid() || p.Repo != pr148Repo || len(p.Nonce) != 64 {
		return p, errors.New("fixed Linux guest plan unavailable")
	}
	return p, nil
}

func pr148GuestCargo(rustup bool) int {
	p, e := pr148ReadGuest()
	if e != nil {
		return 120
	}
	if os.Getenv("GITHUB_WORKSPACE") != pr148Workspace || os.Getenv("GITHUB_SHA") != p.Plan.Head || os.Getenv("GITHUB_RUN_ID") != strconv.FormatInt(p.Plan.RunID, 10) || os.Getenv("GITHUB_RUN_ATTEMPT") != strconv.Itoa(p.Plan.Attempt) {
		return 121
	}
	args := os.Args[1:]
	if rustup {
		if !reflect.DeepEqual(args, pr148RustupArgs) {
			return 122
		}
	} else {
		ok := false
		for _, s := range p.Policy {
			if reflect.DeepEqual(args, strings.Fields(s)) {
				ok = true
			}
		}
		if !ok {
			return 122
		}
	}
	if e = pr148Checkout(p.Plan); e != nil {
		return 123
	}
	endpoint := p.Callback
	if rustup {
		endpoint = strings.TrimSuffix(endpoint, "cargo") + "rustup"
	}
	if p.Execution.timeout() == 0 {
		return 120
	}
	ctx, stop := context.WithTimeout(context.Background(), p.Execution.timeout()+time.Minute)
	defer stop()
	r, e := pr148RequestCargo(ctx, endpoint, p.Nonce, p.Plan, args)
	if e != nil {
		return 124
	}
	_, _ = os.Stdout.Write(r.Log)
	if r.LogTruncated && r.Exit == 0 {
		fmt.Fprintln(os.Stderr, "Cargo output preview truncated; full original log retained on host")
		return 126
	}
	return r.Exit
}

func pr148ExecChild(args []string) error {
	var ready [1]byte
	if _, e := io.ReadFull(os.Stdin, ready[:]); e != nil || ready[0] != 'G' {
		return errors.New("supervisor start handshake absent")
	}
	if len(args) != 3 && len(args) != 5 {
		return errors.New("executor argv rejected")
	}
	jobs, e := strconv.Atoi(args[0])
	tests, te := strconv.Atoi(args[1])
	index, ie := strconv.Atoi(args[2])
	if e != nil || te != nil || ie != nil || jobs < 1 || jobs > 10 || tests < 1 || tests > 10 || index < 0 || index > 4 {
		return errors.New("executor caps rejected")
	}
	p, e := pr148ReadGuest()
	if e != nil {
		return e
	}
	path := "/toolchain/rustup/toolchains/1.95.0-x86_64-unknown-linux-gnu/bin/cargo"
	var command []string
	if index == 4 {
		path = "/toolchain/cargo/bin/rustup"
		command = pr148RustupArgs
	} else {
		command = strings.Fields(p.Policy[index])
	}
	if len(args) == 5 {
		if args[3] != "fixture" || (args[4] != "true" && args[4] != "false") {
			return errors.New("fixture argv rejected")
		}
		// Synthetic process-lifecycle fixture, never a product Cargo command.
		path = "/opt/rust/bin/cargo"
		command = []string{"test", "--offline", "--manifest-path", "tools/Cargo.toml", "--workspace", "--all-targets"}
		env := []string{"HOME=/home/runner", "PATH=/opt/rust/bin:/usr/bin:/bin", "CARGO_HOME=/opt/rust", "RUSTUP_HOME=/opt/rustup", "CARGO_TARGET_DIR=" + pr148Workspace + "/tools/target", "CARGO_BUILD_JOBS=" + strconv.Itoa(jobs), "RUST_TEST_THREADS=" + strconv.Itoa(tests), "CC_PR148_CANCEL_FIXTURE=" + args[4]}
		if e = os.Chdir(pr148Workspace); e != nil {
			return e
		}
		return pr148ExecRestricted(path, command, env, false)
	}
	env, dir, e := pr148StepEnvironment(p.Execution, index, jobs, tests)
	if e != nil {
		return e
	}
	if e = os.Chdir(dir); e != nil {
		return e
	}
	return pr148ExecRestricted(path, command, env, false)
}

func pr148Checkout(p pr148LinuxPlan) error {
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	for expr, want := range map[string]string{"HEAD": p.Head, "HEAD^{tree}": p.Tree} {
		b, e := cmd(ctx, "git", "-C", pr148Workspace, "rev-parse", expr).Output()
		if e != nil || strings.TrimSpace(string(b)) != want {
			return errors.New("Linux actual checkout HEAD/tree rejected")
		}
	}
	b, e := cmd(ctx, "git", "-C", pr148Workspace, "config", "--local", "--get-regexp", `http\..*\.extraheader|credential\..*`).Output()
	if len(b) != 0 || (e != nil && exitStatus(e) != 1) {
		return errors.New("Linux checkout authentication present")
	}
	return nil
}

func pr148Hook(complete bool) error {
	p, e := pr148ReadGuest()
	if e != nil {
		return e
	}
	for key, want := range map[string]string{"GITHUB_REPOSITORY": pr148Repo, "GITHUB_REPOSITORY_ID": "1392152644", "GITHUB_SHA": p.Plan.Head, "GITHUB_RUN_ID": strconv.FormatInt(p.Plan.RunID, 10), "GITHUB_RUN_ATTEMPT": strconv.Itoa(p.Plan.Attempt), "GITHUB_REF": "refs/heads/" + pr148Branch, "GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_JOB": "test", "RUNNER_OS": "Linux", "GITHUB_WORKSPACE": pr148Workspace, "GITHUB_WORKFLOW_SHA": p.Plan.Head, "GITHUB_WORKFLOW_REF": pr148Repo + "/.github/workflows/rust.yml@refs/heads/" + pr148Branch} {
		if os.Getenv(key) != want {
			return fmt.Errorf("Linux job context rejected: %s", key)
		}
	}
	if e = pr148Checkout(p.Plan); e != nil {
		return e
	}
	stage := "start"
	if complete {
		stage = "complete"
	}
	ctx, stop := context.WithTimeout(context.Background(), 90*time.Second)
	defer stop()
	body, _ := json.Marshal(pr148CargoRequest{Plan: p.Plan, RepoID: 1392152644, Nonce: p.Nonce})
	request, e := http.NewRequestWithContext(ctx, "POST", strings.TrimSuffix(p.Callback, "cargo")+stage, strings.NewReader(string(body)))
	if e != nil {
		return e
	}
	request.Header.Set("X-PR148-Nonce", p.Nonce)
	transport := &http.Transport{Proxy: nil}
	defer transport.CloseIdleConnections()
	client := http.Client{Transport: transport, Timeout: 90 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, e := client.Do(request)
	if e != nil {
		return errors.New("live Linux hook admission unavailable")
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return errors.New("live Linux hook admission rejected")
	}
	// This is a local observation. The host independently validates live API
	// run/job/workflow pins before and after every Cargo callback and listener.
	return pr148SaveNew("/home/runner/pr148-"+stage+".json", p.Plan)
}

func pr148GuestRunner() error {
	var p pr148GuestPacket
	d := json.NewDecoder(io.LimitReader(os.Stdin, 64<<10))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil || !p.Plan.valid() || p.Repo != pr148Repo || p.Token == "" || len(p.Nonce) != 64 || !strings.HasPrefix(p.Name, "todo2736-linux-") {
		return errors.New("Linux registration packet rejected")
	}
	if e := cmd(context.Background(), "cp", "-a", "/opt/actions-runner/.", "/runner/").Run(); e != nil {
		return e
	}
	if e := os.MkdirAll("/runner/bin", 0700); e != nil {
		return e
	}
	for _, name := range []string{"cargo", "rustup"} {
		if e := os.Symlink("/runner/product-ci", "/runner/bin/"+name); e != nil {
			return e
		}
	}
	for _, stage := range []string{"start", "complete"} {
		if e := os.WriteFile("/runner/pr148-"+stage+".sh", []byte("#!/bin/sh\nexec /runner/product-ci pr148-hook-"+stage+"\n"), 0700); e != nil {
			return e
		}
	}
	config := cmd(context.Background(), "/runner/config.sh", "--unattended", "--url", "https://github.com/"+pr148Repo, "--name", p.Name, "--labels", pr148LinuxLabel, "--no-default-labels", "--work", "/work", "--ephemeral", "--disableupdate")
	config.Dir = "/runner"
	config.Env = []string{"HOME=/home/runner", "PATH=/usr/local/bin:/usr/bin:/bin", "ACTIONS_RUNNER_INPUT_TOKEN=" + p.Token}
	config.Stdout = io.Discard
	config.Stderr = io.Discard
	e := config.Run()
	p.Token = ""
	config.Env = nil
	if e != nil {
		return errors.New("official Linux runner configuration failed")
	}
	if e = pr148SaveNew("/runner/pr148-plan.json", p); e != nil {
		return e
	}
	env := []string{"HOME=/home/runner", "PATH=/runner/bin:/work/powershell:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "CARGO_HOME=/toolchain/cargo", "RUSTUP_HOME=/toolchain/rustup", "CARGO_TARGET_DIR=" + pr148Workspace + "/tools/target", "ACTIONS_RUNNER_HOOK_JOB_STARTED=/runner/pr148-start.sh", "ACTIONS_RUNNER_HOOK_JOB_COMPLETED=/runner/pr148-complete.sh", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_NO_REPLACE_OBJECTS=1"}
	if e = os.Chdir("/runner"); e != nil {
		return e
	}
	// No tools are exposed if the kernel cannot enforce the filesystem boundary.
	return pr148ExecRestricted("/runner/run.sh", nil, env, true)
}

func pr148Entry() (bool, int) {
	if os.Getenv("CC_PR148_GATE_CHILD") == "1" {
		return true, pr148GatedChild(os.Args[1:])
	}
	if filepath.Base(os.Args[0]) == "cargo" {
		return true, pr148GuestCargo(false)
	}
	if filepath.Base(os.Args[0]) == "rustup" {
		return true, pr148GuestCargo(true)
	}
	if len(os.Args) < 2 || !strings.HasPrefix(os.Args[1], "pr148-") {
		return false, 0
	}
	var e error
	switch os.Args[1] {
	case "pr148-execute":
		return true, pr148Supervise(os.Args[2:])
	case "pr148-exec-child":
		e = pr148ExecChild(os.Args[2:])
	case "pr148-check-group":
		if len(os.Args) != 3 {
			return true, 1
		}
		id, err := strconv.Atoi(os.Args[2])
		if err != nil || id <= 1 {
			return true, 1
		}
		if pr148GroupAbsent(id) {
			return true, 0
		}
		return true, 1
	case "pr148-restriction-fixture":
		e = pr148ExecRestricted("/bin/sh", []string{"-c", `set -eu
if /opt/rust/bin/cargo --version >/dev/null 2>&1; then exit 81; fi
if cp /opt/rust/bin/cargo /work/copied-cargo 2>/dev/null; then exit 82; fi
if cat /toolchain/protected >/dev/null 2>&1; then exit 83; fi
if cat /proc/1/root/opt/rust/bin/cargo >/dev/null 2>&1; then exit 84; fi
if (printf 'tamper' > /runner/product-ci) 2>/dev/null; then exit 85; fi
if (printf 'tamper' > /runner/pr148-plan.json) 2>/dev/null; then exit 86; fi
if truncate -s 0 /runner/product-ci 2>/dev/null; then exit 87; fi
mkdir -p /work/refer-from /work/refer-to
printf refer > /work/refer-from/source
mv /work/refer-from/source /work/refer-to/destination
ln /work/refer-to/destination /work/refer-from/hardlink
test "$(cat /work/refer-from/hardlink)" = refer
printf 'landlock-direct-copy-proc-toolchain-denied\n'
`}, []string{"PATH=/usr/bin:/bin", "HOME=/home/runner"}, true)
	case "pr148-fixture-callback":
		p, err := pr148ReadGuest()
		if err != nil || p.Plan.Head != strings.Repeat("1", 40) || p.Plan.Tree != strings.Repeat("2", 40) || p.Plan.RunID != 101 || p.Plan.JobID != 102 {
			return true, 120
		}
		ctx, stop := context.WithTimeout(context.Background(), 3*time.Minute)
		defer stop()
		r, err := pr148RequestCargo(ctx, p.Callback, p.Nonce, p.Plan, strings.Fields(p.Policy[1]))
		if err != nil {
			return true, 124
		}
		_, _ = os.Stdout.Write(r.Log)
		if r.LogTruncated && r.Exit == 0 {
			return true, 126
		}
		return true, r.Exit
	case "pr148-listener-probe":
		if err := cmd(context.Background(), "cp", "-a", "/opt/actions-runner/.", "/runner/").Run(); err != nil {
			return true, 1
		}
		if err := os.MkdirAll("/runner/_diag", 0700); err != nil {
			return true, 1
		}
		e = pr148ExecRestricted("/runner/bin/Runner.Listener", []string{"--version"}, []string{"HOME=/home/runner", "TMPDIR=/tmp", "PATH=/usr/bin:/bin"}, true)
	case "pr148-linux":
		if len(os.Args) != 6 {
			return true, 1
		}
		b, err := os.ReadFile(os.Args[2])
		var p pr148LinuxPlan
		if err != nil || json.Unmarshal(b, &p) != nil {
			return true, 1
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		e = executePR148Linux(ctx, p, os.Args[3], os.Args[4], os.Args[5])
	case "pr148-guest":
		e = pr148GuestRunner()
	case "pr148-hook-start":
		e = pr148Hook(false)
	case "pr148-hook-complete":
		e = pr148Hook(true)
	case "pr148-plan":
		if len(os.Args) != 3 {
			e = errors.New("new owned receipt directory required")
		} else {
			ctx, stop := context.WithTimeout(context.Background(), 5*time.Minute)
			defer stop()
			e = savePR148Proposal(ctx, os.Args[2])
		}
	default:
		e = errors.New("unknown PR148 helper mode")
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		return true, 1
	}
	return true, 0
}
