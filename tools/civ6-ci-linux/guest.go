package main

import (
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

const guestBinary = "/runner/civ6-ci-linux"
const guestPlanFile = "/runner/plan.json"

// guestPacket reaches the guest through stdin once (registration token
// included) and is then stored WITHOUT the token on the private tmpfs.
type guestPacket struct {
	Plan                         Plan
	Kind                         Kind
	Policy                       execPolicy
	Name, Token, Callback, Nonce string
	Custom                       string
	Defaults                     bool
}

func readGuestPacket() (guestPacket, error) {
	var p guestPacket
	b, e := os.ReadFile(guestPlanFile)
	if e != nil || json.Unmarshal(b, &p) != nil || p.Plan.localCheck() != nil || len(p.Nonce) != 64 {
		return p, errors.New("fixed Linux guest plan unavailable")
	}
	if k, e := p.Plan.kind(); e != nil || k != p.Kind {
		return p, errors.New("guest plan kind rejected")
	}
	return p, nil
}

// checkout proves the actual materialized/checked-out workspace: HEAD and tree
// are the Plan's and no checkout authentication remains in the Git config.
func checkout(p Plan) error {
	ctx, stop := context.WithTimeout(context.Background(), 20*time.Second)
	defer stop()
	for expr, want := range map[string]string{"HEAD": p.Head, "HEAD^{tree}": p.Tree} {
		b, e := cmd(ctx, "git", "-C", workspace, "rev-parse", expr).Output()
		if e != nil || strings.TrimSpace(string(b)) != want {
			return errors.New("Linux actual checkout HEAD/tree rejected")
		}
	}
	b, e := cmd(ctx, "git", "-C", workspace, "config", "--local", "--get-regexp", "http\\..*\\.extraheader|credential\\..*").Output()
	if len(b) != 0 || (e != nil && exitStatus(e) != 1) {
		return errors.New("Linux checkout authentication present")
	}
	return nil
}

func jobEnv() map[string]string {
	env := map[string]string{}
	for _, s := range os.Environ() {
		if k, v, ok := strings.Cut(s, "="); ok && (strings.HasPrefix(k, "GITHUB_") || strings.HasPrefix(k, "RUNNER_")) {
			env[k] = v
		}
	}
	return env
}

// guestCargo is /runner/bin/cargo and /runner/bin/rustup. It accepts only the
// fixed commands and forwards them to the host's shared Cargo gate.
func guestCargo(rustup bool) int {
	p, e := readGuestPacket()
	if e != nil || p.Kind != KindRust {
		return 120
	}
	env := jobEnv()
	if env["GITHUB_WORKSPACE"] != workspace || env["GITHUB_SHA"] != p.Plan.ContextSHA || env["GITHUB_RUN_ID"] != strconv.FormatInt(p.Plan.RunID, 10) || env["GITHUB_RUN_ATTEMPT"] != strconv.Itoa(p.Plan.Attempt) {
		return 121
	}
	args := os.Args[1:]
	if p.Policy.index(args, rustup) < 0 {
		return 122
	}
	if checkout(p.Plan) != nil {
		return 123
	}
	if p.Policy.timeout() == 0 {
		return 120
	}
	stage := "cargo"
	if rustup {
		stage = "rustup"
	}
	ctx, stop := context.WithTimeout(context.Background(), p.Policy.timeout()+time.Minute)
	defer stop()
	r, e := requestHost(ctx, p.Callback, stage, p.Nonce, p.Plan, args)
	if e != nil {
		return 124
	}
	_, _ = os.Stdout.Write(r.Log)
	if r.LogTruncated && r.Exit == 0 {
		fmt.Fprintln(os.Stderr, "Cargo output preview truncated; full log retained on host")
		return 126
	}
	return r.Exit
}

// guestHook runs at job start/completion inside the official runner. The
// parent's CheckEvent compares the actual job environment and event payload;
// the host then re-verifies the live job on this unique runner.
func guestHook(complete bool) error {
	p, e := readGuestPacket()
	if e != nil {
		return e
	}
	env := jobEnv()
	var event []byte
	if path := env["GITHUB_EVENT_PATH"]; path != "" {
		f, e := os.Open(path)
		if e == nil {
			event, e = io.ReadAll(io.LimitReader(f, 1<<20+1))
			_ = f.Close()
		}
		if e != nil || len(event) > 1<<20 {
			return errors.New("actual event payload unreadable")
		}
	}
	if e = activeContract.CheckEvent(p.Plan, env, event); e != nil {
		return e
	}
	if env["GITHUB_WORKSPACE"] != workspace {
		return errors.New("Linux job workspace rejected")
	}
	// The Rust workspace is materialized before registration. CodeQL's own
	// checkout runs inside the job, so its HEAD/tree is proven at completion.
	if p.Kind == KindRust || complete {
		if e = checkout(p.Plan); e != nil {
			return e
		}
	}
	stage := "start"
	if complete {
		stage = "complete"
	}
	ctx, stop := context.WithTimeout(context.Background(), 90*time.Second)
	defer stop()
	if _, e = requestHost(ctx, p.Callback, stage, p.Nonce, p.Plan, nil); e != nil {
		return e
	}
	// A local observation only: the host independently validates the live job.
	return saveNew("/home/runner/hook-"+stage+".json", p.Plan)
}

func guestConfigure(p guestPacket) error {
	if e := cmd(context.Background(), "cp", "-a", "/opt/actions-runner/.", "/runner/").Run(); e != nil {
		return e
	}
	if e := os.MkdirAll("/runner/bin", 0700); e != nil {
		return e
	}
	if p.Kind == KindRust {
		for _, name := range []string{"cargo", "rustup"} {
			if e := os.Symlink(guestBinary, "/runner/bin/"+name); e != nil {
				return e
			}
		}
	}
	for _, stage := range []string{"start", "complete"} {
		if e := os.WriteFile("/runner/hook-"+stage+".sh", []byte("#!/bin/sh\nexec "+guestBinary+" guest-hook-"+stage+"\n"), 0700); e != nil {
			return e
		}
	}
	args := []string{"--unattended", "--url", "https://github.com/" + repoFull, "--name", p.Name, "--labels", p.Custom, "--work", "/work", "--ephemeral", "--disableupdate"}
	if !p.Defaults {
		args = append(args, "--no-default-labels")
	}
	config := cmd(context.Background(), "/runner/config.sh", args...)
	config.Dir = "/runner"
	config.Env = []string{"HOME=/home/runner", "PATH=/usr/local/bin:/usr/bin:/bin", "ACTIONS_RUNNER_INPUT_TOKEN=" + p.Token}
	// Configuration output (including failure diagnostics) is never forwarded.
	config.Stdout, config.Stderr = io.Discard, io.Discard
	e := config.Run()
	config.Env = nil
	if e != nil {
		return errors.New("official runner configuration failed")
	}
	return nil
}

// configureRunner is replaceable so tests can prove that no registration step
// is reached for an invalid packet.
var configureRunner = guestConfigure

// validateRegistration is the pure pre-registration check: shape, kind, label
// route and the unique instance name.
func validateRegistration(p guestPacket) error {
	if p.Plan.localCheck() != nil || p.Token == "" || len(p.Nonce) != 64 {
		return errors.New("Linux registration packet rejected")
	}
	k, e := p.Plan.kind()
	wantLabel, wantDefaults := codeQLLabel, false
	if k == KindRust {
		if jobLabel(p.Plan) == "" {
			return errors.New("selecting job label unavailable")
		}
		wantLabel, wantDefaults = linuxLabel+","+jobLabel(p.Plan), true
	}
	if e != nil || k != p.Kind || p.Custom != wantLabel || p.Defaults != wantDefaults || !strings.HasPrefix(p.Name, "civ6-ci-linux-"+string(k)+"-") || filepath.Base(p.Name) != p.Name {
		return errors.New("Linux registration identity rejected")
	}
	return nil
}

func guestRunner() error {
	var p guestPacket
	d := json.NewDecoder(io.LimitReader(os.Stdin, 64<<10))
	d.DisallowUnknownFields()
	if d.Decode(&p) != nil {
		return errors.New("Linux registration packet rejected")
	}
	if e := validateRegistration(p); e != nil {
		return e
	}
	k := p.Kind
	if e := configureRunner(p); e != nil {
		return e
	}
	p.Token = ""
	if e := saveNew(guestPlanFile, p); e != nil {
		return e
	}
	if e := os.Chdir("/runner"); e != nil {
		return e
	}
	hooks := []string{"ACTIONS_RUNNER_HOOK_JOB_STARTED=/runner/hook-start.sh", "ACTIONS_RUNNER_HOOK_JOB_COMPLETED=/runner/hook-complete.sh"}
	if k == KindCodeQL {
		// No Rust, bridge or Cargo on PATH: this mode has no Cargo admission.
		run := cmd(context.Background(), "/runner/run.sh")
		run.Dir = "/runner"
		run.Env = append(os.Environ(), append(hooks, "PATH=/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "HOME=/home/runner", "CODEQL_THREADS=2", "CODEQL_RAM=6144")...)
		run.Stdout, run.Stderr = os.Stdout, os.Stderr
		return run.Run()
	}
	env := append([]string{"HOME=/home/runner", "PATH=/runner/bin:/work/powershell:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin",
		"CARGO_HOME=/toolchain/cargo", "RUSTUP_HOME=/toolchain/rustup", "CARGO_TARGET_DIR=" + workspace + "/tools/target",
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_NO_REPLACE_OBJECTS=1"}, hooks...)
	// run.sh would mask startup failures as exit zero and write into the
	// read-only runner root: start the official listener directly, restricted.
	return runListener(env)
}

func runListener(env []string) error {
	return execRestricted("/runner/bin/Runner.Listener", []string{"run"}, env, true)
}
