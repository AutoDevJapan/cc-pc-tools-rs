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
const normalContainer = "todo2510-normal-linux"
const normalImage = "todo2510-normal-linux:local"
const normalPort = "127.0.0.1:17852"
const normalRunner = "cc-pc-tools-linux"
const normalBranch = "main"

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
	case "push":
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
func verifyNormalContainer() error {
	state, e := output(command(docker, "inspect", "--format", "{{.State.Running}}", normalContainer))
	if e != nil {
		return errors.New("normal runner container is not provisioned")
	}
	if state != "true" {
		if _, e = output(command(docker, "start", normalContainer)); e != nil {
			return errors.New("normal runner container could not start")
		}
	}
	data, e := output(command(docker, "inspect", normalContainer))
	if e != nil {
		return errors.New("normal runner container inspection failed")
	}
	var inspected []struct {
		Config struct {
			Image string `json:"Image"`
			User  string `json:"User"`
		} `json:"Config"`
		HostConfig struct {
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
	if json.Unmarshal([]byte(data), &inspected) != nil || len(inspected) != 1 {
		return errors.New("normal runner container inspection is invalid")
	}
	c := inspected[0]
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
	if c.Config.Image != normalImage || c.Config.User != "1001" || c.HostConfig.Privileged || !allCapabilitiesDropped || !noNewPrivileges || len(c.HostConfig.Devices) != 0 || len(c.HostConfig.DeviceRequests) != 0 || len(c.HostConfig.Binds) != 0 || c.HostConfig.Tmpfs["/tmp"] == "" || c.HostConfig.Tmpfs["/runner"] == "" {
		return errors.New("normal runner container isolation rejected")
	}
	for _, mount := range c.Mounts {
		if mount.Type != "tmpfs" {
			return errors.New("normal runner mount rejected")
		}
	}
	return nil
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
		c := command(docker, "exec", container, "/opt/rust/bin/cargo", "--version")
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
			c := command(docker, append([]string{"exec", "-i", "--workdir", "/work/cc-pc-tools-rs/cc-pc-tools-rs", normalContainerForMode(), "/opt/bridge/bridge", "execute", "1", "1"}, args...)...)
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
	a := []string{"exec", "-i", "--workdir", "/work/cc-pc-tools-rs/cc-pc-tools-rs", normalContainerForMode(), "/opt/bridge/bridge", "execute", strconv.Itoa(jobs), strconv.Itoa(tests)}
	a = append(a, args...)
	c := command(docker, a...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return exitCode(c.Run())
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
		return exitCode(e)
	case <-disconnected:
		killGroup(c)
		<-done
		return 125
	}
}
func rpc(path string, q request) int {
	key, e := os.ReadFile("/tmp/bridge-key")
	if e != nil {
		return 1
	}
	b, _ := json.Marshal(q)
	req, e := http.NewRequest("POST", "http://host.docker.internal:17851"+path, bytes.NewReader(b))
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
	mode := "finite"
	var revision, branch, target, listen, runnerContainer, runnerName, labels string
	ephemeral := true
	if len(args) == 3 && revisionPattern.MatchString(args[0]) && branchPattern.MatchString(args[1]) {
		// Keep the original finite ToDo2511 invocation and its restrictions.
		revision, branch, target = args[0], args[1], args[2]
		listen, runnerContainer, runnerName, labels = "127.0.0.1:17851", container, "cc-pc-tools-linux-2511", "cc-pc-tools-linux"
	} else if len(args) == 2 && args[0] == "normal" && args[1] != "" {
		mode, target, listen, runnerContainer, runnerName, labels, ephemeral = "normal", args[1], normalPort, normalContainer, normalRunner, normalRunner, false
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
	if mode == "normal" {
		cwd, cwdErr := os.Getwd()
		targetAbs, targetErr := filepath.Abs(target)
		expected, expectedErr := filepath.Abs(filepath.Join(cwd, ".ci-work", "target"))
		if cwdErr != nil || targetErr != nil || expectedErr != nil || !strings.EqualFold(filepath.Clean(targetAbs), filepath.Clean(expected)) {
			fmt.Fprintln(os.Stderr, "normal Cargo target must be this worktree's .ci-work/target")
			return 1
		}
		if _, statErr := os.Lstat(filepath.Join(cwd, ".ci-work")); statErr != nil {
			return 1
		}
		if _, statErr := os.Lstat(targetAbs); statErr != nil {
			return 1
		}
	}
	permission, e := output(command(gh, "repo", "view", repo, "--json", "viewerPermission", "--jq", ".viewerPermission"))
	if e != nil || permission != "ADMIN" {
		return 1
	}
	if mode == "normal" {
		if verifyErr := verifyNormalContainer(); verifyErr != nil {
			fmt.Fprintln(os.Stderr, verifyErr)
			return 1
		}
	}
	l, e := net.Listen("tcp", listen)
	if e != nil {
		return 1
	}
	s := http.Server{Handler: http.HandlerFunc(h.serve), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = s.Serve(l) }()
	// The random bridge key and runner token stay in RAM and the official config
	// process. Neither appears in command arguments, Docker config or host files.
	for {
		token, tokenErr := output(command(gh, "api", "--method", "POST", "repos/"+repo+"/actions/runners/registration-token", "--jq", ".token"))
		if tokenErr != nil || token == "" {
			_ = s.Close()
			return 1
		}
		c := command(docker, "exec", "-i", runnerContainer, "/opt/bridge/bridge", "register")
		c.Stdin = strings.NewReader(h.key + "\n" + token + "\n" + runnerName + "\n" + labels + "\n" + strconv.FormatBool(ephemeral) + "\n")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		code := exitCode(c.Run())
		token = ""
		if ephemeral || code == 0 {
			_ = s.Shutdown(context.Background())
			return code
		}
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
		return 1
	}
	token, e := reader.ReadString('\n')
	if e != nil {
		return 1
	}
	if len(strings.TrimSpace(key)) != 64 {
		return 1
	}
	runnerName, e := reader.ReadString('\n')
	if e != nil {
		return 1
	}
	labels, e := reader.ReadString('\n')
	if e != nil {
		return 1
	}
	ephemeral, e := reader.ReadString('\n')
	if e != nil || (strings.TrimSpace(runnerName) != normalRunner && strings.TrimSpace(runnerName) != "cc-pc-tools-linux-2511") || strings.TrimSpace(labels) != normalRunner || (strings.TrimSpace(ephemeral) != "true" && strings.TrimSpace(ephemeral) != "false") {
		return 1
	}
	if e = os.WriteFile("/tmp/bridge-key", []byte(strings.TrimSpace(key)), 0600); e != nil {
		return 1
	}
	cargs := []string{"--unattended", "--url", "https://github.com/" + repo, "--name", strings.TrimSpace(runnerName), "--labels", strings.TrimSpace(labels), "--work", "/work"}
	if strings.TrimSpace(ephemeral) == "true" {
		cargs = append(cargs, "--ephemeral", "--disableupdate")
	}
	cargs = append(cargs, "--replace")
	c := exec.Command("/runner/config.sh", cargs...)
	c.Dir = "/runner"
	c.Env = append(os.Environ(), "ACTIONS_RUNNER_INPUT_TOKEN="+strings.TrimSpace(token))
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if c.Run() != nil {
		return 1
	}
	token = ""
	mode := "normal"
	if strings.TrimSpace(ephemeral) == "true" {
		mode = "finite"
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
	case "host":
		os.Exit(hostMain(args[1:]))
	case "execute":
		os.Exit(execute(args[1:]))
	case "hook":
		os.Exit(hook())
	case "register":
		os.Exit(register())
	default:
		fmt.Fprintln(os.Stderr, "unknown mode")
		os.Exit(1)
	}
}
