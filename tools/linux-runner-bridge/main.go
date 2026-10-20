// A finite, repository-bound bridge. Only the host has Docker and the shared
// Windows Cargo gate. No credentials or host directories are mounted in Linux.
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const repo = "AutoDevJapan/cc-pc-tools-rs"
const gate = `D:\CC\Tools\codex-jobs\cargo_gate.exe`
const gateDir = `D:\CC\Tools\codex-jobs`
const gh = `D:\CC\Tools\codex-jobs\gh_gate.exe`
const docker = `C:\Program Files\Docker\Docker\resources\bin\docker.exe`
const container = "todo2511-cc-pc-tools-linux"
const normalLabel = "cc-pc-tools-linux"
const normalBranch = "main"

var normalContainer = compiledNormalProfile.container
var normalImage = compiledNormalProfile.image
var normalPort = compiledNormalProfile.listen
var normalRunner = compiledNormalProfile.runner

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var branchPattern = regexp.MustCompile(`^scheduler/todo-251[01]-[a-z0-9-]+$`)
var relativeManifest = regexp.MustCompile(`^[A-Za-z0-9._/-]+/Cargo\.toml$`)

type request struct {
	Args                                  []string
	Dir, Revision, Ref, Repository, Event string
	HeadSHA, HeadRef, HeadRepository      string
	BaseRepository, BaseRef               string
}
type packet struct {
	Stream string
	Data   []byte
	Exit   *int
}
type host struct {
	key, mode, revision, branch, target string
	mu                                  sync.Mutex
	getHead                             func(string) (string, error)
}

func command(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	hidden(c)
	return c
}
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *exec.ExitError
	if errors.As(err, &e) {
		return e.ExitCode()
	}
	return 1
}
func output(c *exec.Cmd) (string, error) { b, e := c.Output(); return strings.TrimSpace(string(b)), e }
func reportStage(name string) {
	root, e := os.Getwd()
	if e != nil {
		return
	}
	record, e := json.Marshal(struct {
		Stage string    `json:"stage"`
		At    time.Time `json:"at"`
	}{Stage: name, At: time.Now().UTC()})
	if e != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(root, ".ci-runtime", "host-status.json"), record, 0600)
}
func reportExit(name string, code int) {
	reportStage(fmt.Sprintf("%s-%d", name, code))
	root, e := os.Getwd()
	if e != nil {
		return
	}
	record, e := json.Marshal(struct {
		Stage string    `json:"stage"`
		Code  int       `json:"code"`
		At    time.Time `json:"at"`
	}{Stage: name, Code: code, At: time.Now().UTC()})
	if e == nil {
		_ = os.WriteFile(filepath.Join(root, ".ci-runtime", "runner-last-failure.json"), record, 0600)
	}
}
func validate(r request) error {
	if r.Dir != "/work/cc-pc-tools-rs/cc-pc-tools-rs" {
		return errors.New("workspace rejected")
	}
	if len(r.Args) == 0 || len(r.Args) > 128 {
		return errors.New("argument count rejected")
	}
	switch r.Args[0] {
	case "test", "check", "clippy", "nextest", "fmt", "metadata", "--version":
	default:
		return errors.New("Cargo command rejected")
	}
	for _, a := range r.Args {
		if len(a) > 2048 || strings.ContainsAny(a, "\x00\r\n") {
			return errors.New("argument rejected")
		}
		for _, p := range []string{"--config", "--target-dir", "-C", "+", "@"} {
			if strings.HasPrefix(a, p) {
				return errors.New("Cargo path/config override rejected")
			}
		}
	}
	for i, a := range r.Args {
		if a == "--manifest-path" {
			if i+1 >= len(r.Args) || !allowedManifest(r.Args[i+1]) {
				return errors.New("Cargo manifest path rejected")
			}
		} else if strings.HasPrefix(a, "--manifest-path=") {
			if !allowedManifest(strings.TrimPrefix(a, "--manifest-path=")) {
				return errors.New("Cargo manifest path rejected")
			}
		} else if strings.HasPrefix(a, "--manifest-path") {
			return errors.New("Cargo manifest path rejected")
		}
	}
	return nil
}
func allowedManifest(p string) bool {
	return p == "cc-bookmark-queue/Cargo.toml" && relativeManifest.MatchString(p) && !strings.Contains(p, "..")
}
func runnerConfigFailureClass(output []byte) int {
	text := strings.ToLower(string(output))
	switch {
	case strings.Contains(text, "401") || strings.Contains(text, "unauthorized"):
		return 1
	case strings.Contains(text, "403") || strings.Contains(text, "forbidden") || strings.Contains(text, "not authorized"):
		return 2
	case strings.Contains(text, "already exists") || strings.Contains(text, "same name"):
		return 3
	case strings.Contains(text, "connect") || strings.Contains(text, "network"):
		return 4
	default:
		return 9
	}
}
func reportRunnerConfigSummary(output []byte) {
	text := strings.ToLower(string(output))
	summary := struct {
		Bytes              int  `json:"bytes"`
		Lines              int  `json:"lines"`
		Has401             bool `json:"has_401"`
		Has403             bool `json:"has_403"`
		HasTokenWord       bool `json:"has_token_word"`
		HasURLWord         bool `json:"has_url_word"`
		HasAuthWord        bool `json:"has_auth_word"`
		HasExistWord       bool `json:"has_exist_word"`
		HasConnect         bool `json:"has_connect_word"`
		HasErrorWord       bool `json:"has_error_word"`
		HasFailWord        bool `json:"has_fail_word"`
		HasRepoWord        bool `json:"has_repo_word"`
		HasEnterWord       bool `json:"has_enter_word"`
		HasConfigWord      bool `json:"has_config_word"`
		HasRetryWord       bool `json:"has_retry_word"`
		HasSessionWord     bool `json:"has_session_word"`
		HasCredentialWord  bool `json:"has_credential_word"`
		HasPermissionWord  bool `json:"has_permission_word"`
		HasInvalidWord     bool `json:"has_invalid_word"`
		HasExpiredWord     bool `json:"has_expired_word"`
		HasAlreadyWord     bool `json:"has_already_word"`
		HasInteractiveWord bool `json:"has_interactive_word"`
	}{
		Bytes: len(output), Lines: strings.Count(string(output), "\n"), Has401: strings.Contains(text, "401"), Has403: strings.Contains(text, "403"),
		HasTokenWord: strings.Contains(text, "token"), HasURLWord: strings.Contains(text, "url"),
		HasAuthWord: strings.Contains(text, "auth"), HasExistWord: strings.Contains(text, "exist"),
		HasConnect: strings.Contains(text, "connect"), HasErrorWord: strings.Contains(text, "error"),
		HasFailWord: strings.Contains(text, "fail"),
		HasRepoWord: strings.Contains(text, "repo"), HasEnterWord: strings.Contains(text, "enter"),
		HasConfigWord: strings.Contains(text, "config"), HasRetryWord: strings.Contains(text, "retry"),
		HasSessionWord: strings.Contains(text, "session"), HasCredentialWord: strings.Contains(text, "credential"),
		HasPermissionWord: strings.Contains(text, "permission"), HasInvalidWord: strings.Contains(text, "invalid"),
		HasExpiredWord: strings.Contains(text, "expired"), HasAlreadyWord: strings.Contains(text, "already"),
		HasInteractiveWord: strings.Contains(text, "what is the url"),
	}
	data, e := json.Marshal(summary)
	if e == nil {
		_ = os.WriteFile(filepath.Join(os.TempDir(), "runner-config-summary.json"), data, 0600)
	}
}
func (h *host) remoteHead(branch string) (string, error) {
	if h.getHead != nil {
		return h.getHead(branch)
	}
	if branch == "" || strings.ContainsAny(branch, "\x00\r\n") || strings.HasPrefix(branch, "-") {
		return "", errors.New("branch rejected")
	}
	ref := "repos/" + repo + "/git/ref/heads/" + url.PathEscape(branch)
	return output(command(gh, "api", ref, "--jq", ".object.sha"))
}
func (h *host) current() error {
	// Query only the specified branch, never credentials or an owner's profile.
	s, e := h.remoteHead(h.branch)
	if e != nil || s != h.revision {
		return errors.New("current remote branch HEAD differs from fixed revision")
	}
	return nil
}
func (h *host) authorize(r request) error {
	if h.mode == "normal" {
		return h.authorizeNormal(r)
	}
	if r.Repository != repo || r.Revision != h.revision || r.Ref != "refs/heads/"+h.branch {
		return errors.New("job identity rejected")
	}
	if r.Event != "push" && r.Event != "workflow_dispatch" {
		return errors.New("job event rejected")
	}
	return h.current()
}
func (h *host) authorizeNormal(r request) error {
	if r.Repository != repo || r.Revision != r.HeadSHA || !revisionPattern.MatchString(r.HeadSHA) || r.HeadRepository != repo || r.BaseRepository != repo {
		return errors.New("job head identity rejected")
	}
	branch := r.HeadRef
	switch r.Event {
	case "push", "schedule":
		if r.Ref != "refs/heads/"+normalBranch || branch != normalBranch {
			return errors.New("push branch rejected")
		}
	case "pull_request":
		if r.HeadRef == "" || r.BaseRef != normalBranch || !strings.HasPrefix(r.Ref, "refs/pull/") || !strings.HasSuffix(r.Ref, "/merge") {
			return errors.New("pull request event rejected")
		}
	default:
		return errors.New("job event rejected")
	}
	current, e := h.remoteHead(branch)
	if e != nil || current != r.HeadSHA {
		return errors.New("remote head differs from job head")
	}
	return nil
}
func (h *host) checkout() error {
	return h.checkoutRevision(h.revision)
}
func (h *host) checkoutRevision(revision string) error {
	s, e := output(command(docker, "exec", normalContainerFor(h.mode), "git", "-C", "/work/cc-pc-tools-rs/cc-pc-tools-rs", "rev-parse", "HEAD"))
	if e != nil || s != revision {
		return errors.New("checked out HEAD differs from fixed revision")
	}
	s, e = output(command(docker, "exec", normalContainerFor(h.mode), "git", "-C", "/work/cc-pc-tools-rs/cc-pc-tools-rs", "remote", "get-url", "origin"))
	if e != nil || s != "https://github.com/"+repo && s != "https://github.com/"+repo+".git" {
		return errors.New("checkout remote rejected")
	}
	if h.mode == "normal" {
		return nil
	}
	return h.current()
}
func normalContainerFor(mode string) string {
	if mode == "normal" {
		return normalContainer
	}
	return container
}

type normalContainerInspection struct {
	Config struct {
		Image string `json:"Image"`
		User  string `json:"User"`
	} `json:"Config"`
	HostConfig struct {
		Init           bool              `json:"Init"`
		Privileged     bool              `json:"Privileged"`
		CapDrop        []string          `json:"CapDrop"`
		SecurityOpt    []string          `json:"SecurityOpt"`
		Devices        []json.RawMessage `json:"Devices"`
		DeviceRequests []json.RawMessage `json:"DeviceRequests"`
		Binds          []string          `json:"Binds"`
		Tmpfs          map[string]string `json:"Tmpfs"`
	} `json:"HostConfig"`
	Mounts []struct {
		Type string `json:"Type"`
	} `json:"Mounts"`
}

type dockerOutput func(...string) (string, error)

func verifyContainerReaper(name string, run dockerOutput) error {
	value, err := run("inspect", "--format", "{{json .HostConfig.Init}}", name)
	var enabled bool
	if err != nil || json.Unmarshal([]byte(value), &enabled) != nil || !enabled {
		return errors.New("runner container requires an init reaper")
	}
	return nil
}

func inspectNormalContainer(data string) (normalContainerInspection, error) {
	var inspected []normalContainerInspection
	if json.Unmarshal([]byte(data), &inspected) != nil || len(inspected) != 1 {
		return normalContainerInspection{}, errors.New("normal runner container inspection is invalid")
	}
	return inspected[0], nil
}

// Validate immutable Docker configuration before starting the container.
// A stopped container cannot answer docker exec checks until after startup.
func validateNormalContainerConfiguration(c normalContainerInspection) error {
	allCapabilitiesDropped := false
	for _, cap := range c.HostConfig.CapDrop {
		if cap == "ALL" {
			allCapabilitiesDropped = true
		}
	}
	noNewPrivileges := false
	for _, opt := range c.HostConfig.SecurityOpt {
		if opt == "no-new-privileges:true" {
			noNewPrivileges = true
		}
	}
	if c.Config.Image != normalImage || c.Config.User != "runner" || !c.HostConfig.Init || c.HostConfig.Privileged || !allCapabilitiesDropped || !noNewPrivileges || len(c.HostConfig.Devices) != 0 || len(c.HostConfig.DeviceRequests) != 0 || len(c.HostConfig.Binds) != 0 || c.HostConfig.Tmpfs["/tmp"] == "" || c.HostConfig.Tmpfs["/runner"] == "" {
		return errors.New("normal runner container isolation rejected")
	}
	runnerTmpfs := strings.Split(c.HostConfig.Tmpfs["/runner"], ",")
	canExecuteRunner := false
	for _, option := range runnerTmpfs {
		if option == "exec" {
			canExecuteRunner = true
		}
		if option == "noexec" {
			canExecuteRunner = false
			break
		}
	}
	if !canExecuteRunner {
		return errors.New("runner tmpfs must allow official runner executables")
	}
	for _, option := range strings.Split(c.HostConfig.Tmpfs["/tmp"], ",") {
		if option == "exec" {
			return errors.New("credential tmpfs must remain noexec")
		}
	}
	for _, mount := range c.Mounts {
		if mount.Type != "tmpfs" {
			return errors.New("normal runner mount rejected")
		}
	}
	return nil
}

func validateNormalContainerRuntime(name string, run dockerOutput, pause func(time.Duration)) error {
	uid, e := run("exec", name, "id", "-u")
	if e != nil || uid != "1001" {
		return errors.New("normal runner effective user rejected")
	}
	for attempt := 0; attempt < 60; attempt++ {
		preflight, preflightErr := run("exec", name, "/bin/sh", "-c", "test -x /runner/config.sh && test -x /runner/run.sh && test -x /opt/bridge/bridge && test -w /runner && test -w /tmp && test ! -e /var/run/docker.sock && mount | grep -F ' on /runner type tmpfs ' >/dev/null && ! mount | grep -F ' on /runner type tmpfs ' | grep -F noexec >/dev/null && mount | grep -F ' on /tmp type tmpfs ' | grep -F noexec >/dev/null")
		if preflightErr == nil && preflight == "" {
			return nil
		}
		pause(time.Second)
	}
	return errors.New("normal runner runtime preflight did not become ready")
}

func verifyNormalContainer() error {
	return verifyNormalContainerWith(normalContainer, func(args ...string) (string, error) {
		return output(command(docker, args...))
	})
}

func verifyNormalContainerWith(name string, run dockerOutput) error {
	state, e := run("inspect", "--format", "{{.State.Running}}", name)
	if e != nil {
		return errors.New("normal runner container is not provisioned")
	}
	data, e := run("inspect", name)
	if e != nil {
		return errors.New("normal runner container inspection failed")
	}
	inspected, e := inspectNormalContainer(data)
	if e != nil {
		return e
	}
	if e = validateNormalContainerConfiguration(inspected); e != nil {
		return e
	}
	if state != "true" {
		if _, e = run("start", name); e != nil {
			return errors.New("normal runner container could not start")
		}
		state, e = run("inspect", "--format", "{{.State.Running}}", name)
		if e != nil || state != "true" {
			return errors.New("normal runner container did not remain running after start")
		}
	}
	// Re-read live mounts after startup. The static check above remains the
	// gate that prevents a disallowed stopped container from being started.
	data, e = run("inspect", name)
	if e != nil {
		return errors.New("normal runner live inspection failed")
	}
	inspected, e = inspectNormalContainer(data)
	if e != nil {
		return e
	}
	if e = validateNormalContainerConfiguration(inspected); e != nil {
		return e
	}
	return validateNormalContainerRuntime(name, run, time.Sleep)
}

func configuredRunnerProcessCount(containerName string, run dockerOutput, pattern string) (int, error) {
	probe := `n=0; for pid in $(pgrep -f '` + pattern + `' || true); do state=$(ps -o stat= -p "$pid" | tr -d ' '); case "$state" in Z*) ;; *) n=$((n+1));; esac; done; printf '%s' "$n"`
	result, err := run("exec", containerName, "/bin/sh", "-c", probe)
	if err != nil {
		return 0, errors.New("configured runner process probe failed")
	}
	count, err := strconv.Atoi(strings.TrimSpace(result))
	if err != nil || count < 0 {
		return 0, errors.New("configured runner process count is invalid")
	}
	return count, nil
}

func configuredRunnerListenerCount(containerName string, run dockerOutput) (int, error) {
	return configuredRunnerProcessCount(containerName, run, `[/]runner/bin/Runner.Listener run`)
}

func configuredRunnerSupervisorCount(containerName string, run dockerOutput) (int, error) {
	// The registered bridge process owns run.sh and waits 30 seconds before
	// restarting it. Count it, run.sh, and run-helper.sh as existing supervisors.
	const pattern = `[/]opt/bridge/bridge (register|supervise)|[/]bin/bash /runner/run[.-]helper.sh|[/]bin/bash /runner/run.sh`
	return configuredRunnerProcessCount(containerName, run, pattern)
}

// Let the registered bridge/run.sh retry loop finish before adding a new
// supervisor. The single host task owns recovery; an existing supervisor is
// never raced with another detached run.sh tree.
func ensureConfiguredRunnerListener(containerName string, run dockerOutput, pause func(time.Duration)) (bool, error) {
	emptyProbes := 0
	for attempt := 0; attempt < 12; attempt++ {
		listeners, err := configuredRunnerListenerCount(containerName, run)
		if err != nil {
			return false, err
		}
		if listeners > 1 {
			return false, errors.New("multiple configured runner listeners are already running")
		}
		if listeners == 1 {
			return false, nil
		}
		supervisors, err := configuredRunnerSupervisorCount(containerName, run)
		if err != nil {
			return false, err
		}
		if supervisors > 0 {
			emptyProbes = 0
		} else {
			emptyProbes++
			if emptyProbes >= 2 {
				break
			}
		}
		pause(5 * time.Second)
	}
	if emptyProbes < 2 {
		return false, errors.New("existing runner supervisor did not recover within 60 seconds")
	}
	if _, err := run("exec", "-d", containerName, "/opt/bridge/bridge", "supervise"); err != nil {
		return false, errors.New("configured runner listener restart failed")
	}
	for attempt := 0; attempt < 15; attempt++ {
		pause(2 * time.Second)
		listeners, err := configuredRunnerListenerCount(containerName, run)
		if err != nil {
			return true, err
		}
		if listeners > 1 {
			return true, errors.New("configured runner listener restart created multiple listeners")
		}
		if listeners == 1 {
			return true, nil
		}
	}
	return true, errors.New("configured runner listener restart was not observed")
}

type streamWriter struct {
	enc    *json.Encoder
	mu     *sync.Mutex
	flush  http.Flusher
	stream string
}

func (w streamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	e := w.enc.Encode(packet{Stream: w.stream, Data: p})
	if w.flush != nil {
		w.flush.Flush()
	}
	return len(p), e
}
func (h *host) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+h.key)) != 1 {
		http.Error(w, "rejected", 403)
		return
	}
	var q request
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 65536))
	d.DisallowUnknownFields()
	if d.Decode(&q) != nil {
		http.Error(w, "invalid request", 400)
		return
	}
	if r.URL.Path == "/authorize" {
		if e := h.authorize(q); e != nil {
			http.Error(w, e.Error(), 403)
			return
		}
		w.WriteHeader(204)
		return
	}
	if r.URL.Path != "/cargo" {
		http.NotFound(w, r)
		return
	}
	if e := validate(q); e != nil {
		http.Error(w, e.Error(), 403)
		return
	}
	// One Linux target maps to one host target lock. The Windows gate performs
	// the CPU/RAM measurement and takes the exact cargo-build.lock.N byte locks.
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.mode == "normal" {
		if e := h.authorizeNormal(q); e != nil {
			http.Error(w, e.Error(), 403)
			return
		}
		if e := h.checkoutRevision(q.HeadSHA); e != nil {
			http.Error(w, e.Error(), 403)
			return
		}
	} else if e := h.checkout(); e != nil {
		http.Error(w, e.Error(), 403)
		return
	}
	enc := json.NewEncoder(w)
	var mu sync.Mutex
	f, _ := w.(http.Flusher)
	executable, e := os.Executable()
	if e != nil {
		http.Error(w, "executable unavailable", 500)
		return
	}
	c := command(gate, q.Args...)
	// A clean environment cannot inherit a parent's LOCK_HELD marker or API key.
	c.Env = hostEnv()
	c.Env = append(c.Env, "CODEX_JOBS_REAL_CARGO="+executable, "CODEX_JOBS_CARGO_LOCK_DIR="+gateDir, "CARGO_TARGET_DIR="+h.target, "CC_LINUX_GATED_CHILD=1", "CC_LINUX_RUNNER_MODE="+h.mode)
	c.Stdout = streamWriter{enc, &mu, f, "stdout"}
	c.Stderr = streamWriter{enc, &mu, f, "stderr"}
	input, e := c.StdinPipe()
	if e != nil {
		http.Error(w, "stdin unavailable", 500)
		return
	}
	if e = c.Start(); e != nil {
		input.Close()
		http.Error(w, "gate unavailable", 500)
		return
	}
	// This pipe remains open until cancellation. EOF reaches the Docker exec
	// supervisor, which kills and reaps Linux Cargo BEFORE the gate releases locks.
	stopped := make(chan struct{})
	go func() {
		select {
		case <-r.Context().Done():
			input.Close()
		case <-stopped:
		}
	}()
	code := exitCode(c.Wait())
	close(stopped)
	input.Close()
	mu.Lock()
	_ = enc.Encode(packet{Exit: &code})
	if f != nil {
		f.Flush()
	}
	mu.Unlock()
	fmt.Printf("gate-result revision=%s exit=%d\n", h.revision, code)
}
func hostEnv() []string {
	var out []string
	for _, k := range []string{"SystemRoot", "WINDIR", "SystemDrive", "TEMP", "TMP", "PATH", "USERPROFILE", "LOCALAPPDATA"} {
		if v := os.Getenv(k); v != "" {
			out = append(out, k+"="+v)
		}
	}
	return out
}
func gatedChild(args []string) int {
	if len(args) == 1 && args[0] == "--version" {
		c := command(docker, "exec", normalContainerForMode(), "/opt/rust/bin/cargo", "--version")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return exitCode(c.Run())
	}
	jobs, e := strconv.Atoi(os.Getenv("CARGO_BUILD_JOBS"))
	// Read-only commands (metadata/fmt/version) intentionally bypass admission.
	// Their compiler cap is irrelevant; use one rather than pretending a slot
	// was admitted. Build-like commands must have the gate's published caps.
	if os.Getenv("CODEX_JOBS_CARGO_LOCK_HELD") != "1" {
		switch args[0] {
		case "metadata", "fmt":
			c := command(docker, dockerExecuteArgs(os.Getenv("CC_LINUX_RUNNER_MODE"), "1", "1", args)...)
			c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
			return exitCode(c.Run())
		default:
			return 1
		}
	}
	if e != nil || jobs < 1 || jobs > 10 {
		return 1
	}
	tests, e := strconv.Atoi(os.Getenv("RUST_TEST_THREADS"))
	if e != nil || tests < 1 || tests > 10 {
		return 1
	}
	// Only the gate's admitted caps cross the host/container boundary. No host
	// environment, cache wrapper, path or credential is forwarded.
	fmt.Fprintf(os.Stderr, "linux-gate-admitted build-jobs=%d test-threads=%d\n", jobs, tests)
	a := dockerExecuteArgs(os.Getenv("CC_LINUX_RUNNER_MODE"), strconv.Itoa(jobs), strconv.Itoa(tests), args)
	c := command(docker, a...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return exitCode(c.Run())
}

func dockerExecuteArgs(mode, jobs, tests string, args []string) []string {
	a := []string{"exec", "-i"}
	if mode == "normal" {
		a = append(a, "-e", "CC_LINUX_RUNNER_MODE=normal")
	}
	a = append(a, "--workdir", "/work/cc-pc-tools-rs/cc-pc-tools-rs", normalContainerFor(mode), "/opt/bridge/bridge", "execute", jobs, tests)
	return append(a, args...)
}

func execute(args []string) int {
	if len(args) < 3 {
		return 1
	}
	jobs, e := strconv.Atoi(args[0])
	if e != nil || jobs < 1 || jobs > 10 {
		return 1
	}
	tests, e := strconv.Atoi(args[1])
	if e != nil || tests < 1 || tests > 10 {
		return 1
	}
	c := exec.Command("/opt/rust/bin/cargo", args[2:]...)
	group(c)
	c.Dir = "/work/cc-pc-tools-rs/cc-pc-tools-rs"
	c.Env = []string{"HOME=/home/runner", "PATH=/opt/rust/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin", "CARGO_HOME=/opt/rust", "RUSTUP_HOME=/opt/rustup", "CARGO_TARGET_DIR=/work/target", "CARGO_BUILD_JOBS=" + args[0], "RUST_TEST_THREADS=" + args[1], "NEXTEST_TEST_THREADS=" + args[1]}
	scratchEnv, err := cargoScratchEnv(os.Getenv("CC_LINUX_RUNNER_MODE"))
	if err != nil {
		return 1
	}
	c.Env = append(c.Env, scratchEnv...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if c.Start() != nil {
		return 1
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	disconnected := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, os.Stdin); close(disconnected) }()
	select {
	case e := <-done:
		return finishGroup(c, e)
	case <-disconnected:
		killGroup(c)
		<-done
		return 125
	}
}

func finishGroup(c *exec.Cmd, result error) int {
	// Successful Cargo can also leave a background descendant. Every exit,
	// not just cancellation, must drain it before releasing the host target.
	killGroup(c)
	return exitCode(result)
}

func rpc(path string, q request) int {
	key, e := os.ReadFile("/tmp/bridge-key")
	if e != nil {
		return 1
	}
	b, _ := json.Marshal(q)
	req, e := http.NewRequest("POST", bridgeURL(os.Getenv("CC_LINUX_RUNNER_MODE"), path), bytes.NewReader(b))
	if e != nil {
		return 1
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(key)))
	client := http.Client{Timeout: 45 * time.Minute}
	resp, e := client.Do(req)
	if e != nil {
		fmt.Fprintln(os.Stderr, "bridge unavailable")
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode == 204 {
		return 0
	}
	if resp.StatusCode != 200 {
		fmt.Fprintf(os.Stderr, "bridge rejected (%d)\n", resp.StatusCode)
		return 1
	}
	d := json.NewDecoder(resp.Body)
	for {
		var p packet
		if d.Decode(&p) != nil {
			return 1
		}
		if p.Exit != nil {
			return *p.Exit
		}
		if p.Stream == "stderr" {
			_, _ = os.Stderr.Write(p.Data)
		} else {
			_, _ = os.Stdout.Write(p.Data)
		}
	}
}
func bridgeURL(mode, path string) string {
	port := "17851"
	if mode == "normal" {
		port = strings.TrimPrefix(normalPort, "127.0.0.1:")
	}
	return "http://host.docker.internal:" + port + path
}
func hook() int {
	r, ok := jobRequestFromEnv()
	if !ok {
		return 1
	}
	return rpc("/authorize", r)
}
func jobRequestFromEnv() (request, bool) {
	r := request{Repository: os.Getenv("GITHUB_REPOSITORY"), Ref: os.Getenv("GITHUB_REF"), Event: os.Getenv("GITHUB_EVENT_NAME")}
	if r.Repository != repo {
		return request{}, false
	}
	if os.Getenv("CC_LINUX_RUNNER_MODE") != "normal" {
		r.Revision = os.Getenv("GITHUB_SHA")
		return r, r.Revision != ""
	}
	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if eventPath == "" {
		return request{}, false
	}
	info, e := os.Stat(eventPath)
	if e != nil || info.Size() < 0 || info.Size() > 1<<20 {
		return request{}, false
	}
	file, e := os.Open(eventPath)
	if e != nil {
		return request{}, false
	}
	defer file.Close()
	data, e := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if e != nil || len(data) > 1<<20 {
		return request{}, false
	}
	var payload struct {
		After      string `json:"after"`
		Ref        string `json:"ref"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest struct {
			Head struct {
				SHA  string `json:"sha"`
				Ref  string `json:"ref"`
				Repo struct {
					FullName string `json:"full_name"`
				} `json:"repo"`
			} `json:"head"`
			Base struct {
				Ref  string `json:"ref"`
				Repo struct {
					FullName string `json:"full_name"`
				} `json:"repo"`
			} `json:"base"`
		} `json:"pull_request"`
	}
	if json.Unmarshal(data, &payload) != nil || payload.Repository.FullName != repo {
		return request{}, false
	}
	switch r.Event {
	case "push":
		if payload.Ref != r.Ref || payload.After != os.Getenv("GITHUB_SHA") {
			return request{}, false
		}
		r.Revision, r.HeadSHA = payload.After, payload.After
		r.HeadRef, r.HeadRepository, r.BaseRepository = normalBranch, repo, repo
	case "schedule":
		// Scheduled payloads have no push `after` field. The host still verifies
		// this SHA against current main before authorizing the scheduled job.
		if r.Ref != "refs/heads/"+normalBranch || !revisionPattern.MatchString(os.Getenv("GITHUB_SHA")) {
			return request{}, false
		}
		r.Revision, r.HeadSHA = os.Getenv("GITHUB_SHA"), os.Getenv("GITHUB_SHA")
		r.HeadRef, r.HeadRepository, r.BaseRepository = normalBranch, repo, repo
	case "pull_request":
		r.Revision, r.HeadSHA = payload.PullRequest.Head.SHA, payload.PullRequest.Head.SHA
		r.HeadRef = payload.PullRequest.Head.Ref
		r.HeadRepository = payload.PullRequest.Head.Repo.FullName
		r.BaseRepository = payload.PullRequest.Base.Repo.FullName
		r.BaseRef = payload.PullRequest.Base.Ref
	default:
		return request{}, false
	}
	return r, true
}
func normalContainerForMode() string {
	return normalContainerFor(os.Getenv("CC_LINUX_RUNNER_MODE"))
}
func hostMain(args []string) int {
	reportStage("host-start")
	mode := "finite"
	var revision, branch, target, listen, runnerContainer, runnerName, labels string
	ephemeral := true
	if len(args) == 3 && revisionPattern.MatchString(args[0]) && branchPattern.MatchString(args[1]) {
		// Keep the original finite ToDo2511 invocation and its restrictions.
		revision, branch, target = args[0], args[1], args[2]
		listen, runnerContainer, runnerName, labels = "127.0.0.1:17851", container, "cc-pc-tools-linux-2511", "cc-pc-tools-linux"
	} else if len(args) == 2 && args[0] == "normal" && args[1] != "" {
		mode, target, listen, runnerContainer, runnerName, labels, ephemeral = "normal", args[1], normalPort, normalContainer, normalRunner, normalLabel, compiledNormalProfile.ephemeral
	} else {
		fmt.Fprintln(os.Stderr, "expected host REVISION scheduler/todo-251x-branch TARGET or host normal TARGET")
		return 1
	}
	key := make([]byte, 32)
	if _, e := rand.Read(key); e != nil {
		return 1
	}
	h := host{key: hex.EncodeToString(key), mode: mode, revision: revision, branch: branch, target: target}
	if mode == "finite" && h.current() != nil {
		return 1
	}
	if mode == "finite" {
		if err := verifyContainerReaper(runnerContainer, func(args ...string) (string, error) {
			return output(command(docker, args...))
		}); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
	}
	if mode == "normal" {
		reportStage("target-validation-started")
		cwd, cwdErr := os.Getwd()
		targetAbs, targetErr := filepath.Abs(target)
		expected, expectedErr := filepath.Abs(filepath.Join(cwd, ".ci-work", "target"))
		if cwdErr != nil || targetErr != nil || expectedErr != nil || !strings.EqualFold(filepath.Clean(targetAbs), filepath.Clean(expected)) {
			fmt.Fprintln(os.Stderr, "normal Cargo target must be this worktree's .ci-work/target")
			return 1
		}
		workInfo, statErr := os.Lstat(filepath.Join(cwd, ".ci-work"))
		if statErr != nil || !workInfo.IsDir() || workInfo.Mode()&os.ModeSymlink != 0 {
			return 1
		}
		targetInfo, statErr := os.Lstat(targetAbs)
		if statErr != nil || !targetInfo.IsDir() || targetInfo.Mode()&os.ModeSymlink != 0 {
			return 1
		}
		reportStage("target-validated")
	}
	permission, e := output(command(gh, "repo", "view", repo, "--json", "viewerPermission", "--jq", ".viewerPermission"))
	if e != nil || permission != "ADMIN" {
		reportStage("oauth-repository-permission-failed")
		return 1
	}
	reportStage("oauth-repository-permission-verified")
	if mode == "normal" {
		if verifyErr := verifyNormalContainer(); verifyErr != nil {
			reportStage("normal-container-verification-failed: " + verifyErr.Error())
			fmt.Fprintln(os.Stderr, verifyErr)
			return 1
		}
		reportStage("normal-container-verified")
	}
	l, e := net.Listen("tcp", listen)
	if e != nil {
		reportStage("bridge-listen-failed")
		return 1
	}
	reportStage("bridge-listening")
	s := http.Server{Handler: http.HandlerFunc(h.serve), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = s.Serve(l) }()
	// The random bridge key and runner token stay in RAM and the official config
	// process. Neither appears in command arguments, Docker config or host files.
	for {
		configured, checkErr := output(command(docker, "exec", runnerContainer, "/bin/sh", "-c", "test -f /runner/.runner && printf configured || printf unconfigured"))
		if checkErr != nil {
			reportStage("runner-container-check-failed")
			_ = s.Close()
			return 1
		}
		token := ""
		if configured != "configured" {
			reportStage("runner-token-request")
			var tokenErr error
			token, tokenErr = output(command(gh, "api", "--method", "POST", "repos/"+repo+"/actions/runners/registration-token", "--jq", ".token"))
			if tokenErr != nil || token == "" {
				reportStage("runner-token-request-failed")
				_ = s.Close()
				return 1
			}
		}
		if token != "" {
			reportStage("runner-token-received")
		}
		c := command(docker, "exec", "-i", runnerContainer, "/opt/bridge/bridge", "register")
		c.Stdin = strings.NewReader(h.key + "\n" + token + "\n" + runnerName + "\n" + labels + "\n" + strconv.FormatBool(ephemeral) + "\n")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		reportStage("runner-registration-started")
		code := exitCode(c.Run())
		token = ""
		if ephemeral {
			_ = s.Shutdown(context.Background())
			return code
		}
		if code == 96 {
			reportStage("existing-runner-bridge-key-refreshed")
			for {
				time.Sleep(15 * time.Second)
				started, restartErr := ensureConfiguredRunnerListener(runnerContainer, func(args ...string) (string, error) {
					return output(command(docker, args...))
				}, time.Sleep)
				if restartErr != nil {
					reportStage("configured-runner-listener-restart-failed: " + restartErr.Error())
					break
				}
				if started {
					reportStage("configured-runner-listener-restart")
				}
			}
			continue
		}
		if code == 0 {
			_ = s.Shutdown(context.Background())
			return code
		}
		if mode == "normal" {
			if summary, summaryErr := output(command(docker, "exec", runnerContainer, "cat", "/tmp/runner-config-summary.json")); summaryErr == nil {
				var check map[string]json.RawMessage
				root, rootErr := os.Getwd()
				if json.Unmarshal([]byte(summary), &check) == nil && len(check) == 22 && rootErr == nil {
					_ = os.WriteFile(filepath.Join(root, ".ci-runtime", "runner-config-summary.json"), []byte(summary), 0600)
				}
			}
		}
		reportExit("runner-registration-exit", code)
		fmt.Printf("linux-runner-exit code=%d; retrying after 30 seconds\n", code)
		time.Sleep(30 * time.Second)
		if _, inspectErr := output(command(docker, "inspect", runnerContainer)); inspectErr != nil {
			_ = s.Close()
			return 1
		}
	}
}
func register() int {
	reader := bufio.NewReader(os.Stdin)
	key, e := reader.ReadString('\n')
	if e != nil {
		return 11
	}
	token, e := reader.ReadString('\n')
	if e != nil {
		return 12
	}
	if len(strings.TrimSpace(key)) != 64 {
		return 13
	}
	runnerName, e := reader.ReadString('\n')
	if e != nil {
		return 14
	}
	labels, e := reader.ReadString('\n')
	if e != nil {
		return 15
	}
	ephemeral, e := reader.ReadString('\n')
	if e != nil || (strings.TrimSpace(runnerName) != normalRunner && strings.TrimSpace(runnerName) != "cc-pc-tools-linux-2511") || strings.TrimSpace(labels) != normalLabel || (strings.TrimSpace(ephemeral) != "true" && strings.TrimSpace(ephemeral) != "false") {
		return 16
	}
	keyPath := filepath.Join(os.TempDir(), "bridge-key")
	keyTmp := keyPath + ".new"
	if e = os.WriteFile(keyTmp, []byte(strings.TrimSpace(key)), 0600); e != nil {
		return 17
	}
	if e = os.Rename(keyTmp, keyPath); e != nil {
		_ = os.Remove(keyTmp)
		return 18
	}
	if _, configuredErr := os.Stat("/runner/.runner"); configuredErr == nil {
		token = ""
		return 96
	}
	cargs := []string{"--unattended", "--url", "https://github.com/" + repo, "--name", strings.TrimSpace(runnerName), "--labels", strings.TrimSpace(labels), "--work", "/work"}
	if strings.TrimSpace(ephemeral) == "true" {
		cargs = append(cargs, "--ephemeral", "--disableupdate")
	}
	if !compiledNormalProfile.ephemeral || strings.TrimSpace(runnerName) == "cc-pc-tools-linux-2511" {
		cargs = append(cargs, "--replace")
	}
	_ = os.Remove(filepath.Join(os.TempDir(), "runner-config-summary.json"))
	c := exec.Command("/runner/config.sh", cargs...)
	c.Dir = "/runner"
	c.Env = append(os.Environ(), "ACTIONS_RUNNER_INPUT_TOKEN="+strings.TrimSpace(token))
	var configOutput bytes.Buffer
	c.Stdout = &configOutput
	c.Stderr = &configOutput
	if configErr := c.Run(); configErr != nil {
		failureClass := runnerConfigFailureClass(configOutput.Bytes())
		reportRunnerConfigSummary(configOutput.Bytes())
		configOutput.Reset()
		return 30 + failureClass
	}
	configOutput.Reset()
	token = ""
	mode := "normal"
	if strings.TrimSpace(ephemeral) == "true" && strings.TrimSpace(runnerName) == "cc-pc-tools-linux-2511" {
		mode = "finite"
	}
	if mode == "normal" {
		return superviseNormal()
	}
	for {
		c = exec.Command("/runner/run.sh")
		c.Dir = "/runner"
		c.Env = append(os.Environ(), "ACTIONS_RUNNER_HOOK_JOB_STARTED=/opt/bridge/job-hook.sh", "CC_LINUX_RUNNER_MODE="+mode)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		code := exitCode(c.Run())
		if mode == "finite" {
			return code
		}
		fmt.Printf("linux-runner-exit code=%d; retrying after 30 seconds\n", code)
		time.Sleep(30 * time.Second)
	}
}
func main() {
	if os.Getenv("CC_LINUX_GATED_CHILD") == "1" {
		os.Exit(gatedChild(os.Args[1:]))
	}
	args := os.Args[1:]
	if filepath.Base(os.Args[0]) == "cargo" {
		dir, _ := os.Getwd()
		r, _ := jobRequestFromEnv()
		r.Args, r.Dir = args, dir
		os.Exit(rpc("/cargo", r))
	}
	if len(args) == 0 {
		os.Exit(1)
	}
	switch args[0] {
	case "queue":
		snapshot, err := currentQueue(runnerAPI, time.Now().UTC())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		_ = json.NewEncoder(os.Stdout).Encode(snapshot)
	case "pool":
		os.Exit(burstPoolMain(args[1:]))
	case "host":
		os.Exit(hostMain(args[1:]))
	case "execute":
		os.Exit(execute(args[1:]))
	case "hook":
		os.Exit(hook())
	case "register":
		os.Exit(register())
	case "supervise":
		os.Exit(superviseNormal())
	default:
		fmt.Fprintln(os.Stderr, "unknown mode")
		os.Exit(1)
	}
}
