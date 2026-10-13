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

var revisionPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

type request struct {
	Args                                  []string
	Dir, Revision, Ref, Repository, Event string
}
type packet struct {
	Stream string
	Data   []byte
	Exit   *int
}
type host struct {
	key, revision, branch, target string
	mu                            sync.Mutex
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
		for _, p := range []string{"--config", "--target-dir", "--manifest-path", "-C", "+", "@"} {
			if strings.HasPrefix(a, p) {
				return errors.New("Cargo path/config override rejected")
			}
		}
	}
	return nil
}
func (h *host) current() error {
	// Query only the specified branch, never credentials or an owner's profile.
	s, e := output(command(gh, "api", "repos/"+repo+"/git/ref/heads/"+h.branch, "--jq", ".object.sha"))
	if e != nil || s != h.revision {
		return errors.New("current remote branch HEAD differs from fixed revision")
	}
	return nil
}
func (h *host) authorize(r request) error {
	if r.Repository != repo || r.Revision != h.revision || r.Ref != "refs/heads/"+h.branch {
		return errors.New("job identity rejected")
	}
	if r.Event != "push" && r.Event != "workflow_dispatch" {
		return errors.New("job event rejected")
	}
	return h.current()
}
func (h *host) checkout() error {
	s, e := output(command(docker, "exec", container, "git", "-C", "/work/cc-pc-tools-rs/cc-pc-tools-rs", "rev-parse", "HEAD"))
	if e != nil || s != h.revision {
		return errors.New("checked out HEAD differs from fixed revision")
	}
	s, e = output(command(docker, "exec", container, "git", "-C", "/work/cc-pc-tools-rs/cc-pc-tools-rs", "remote", "get-url", "origin"))
	if e != nil || s != "https://github.com/"+repo && s != "https://github.com/"+repo+".git" {
		return errors.New("checkout remote rejected")
	}
	return h.current()
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
	if e := h.checkout(); e != nil {
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
	c.Env = append(c.Env, "CODEX_JOBS_REAL_CARGO="+executable, "CODEX_JOBS_CARGO_LOCK_DIR="+gateDir, "CARGO_TARGET_DIR="+h.target, "CC_LINUX_GATED_CHILD=1")
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
	a := []string{"exec", "-i", "--workdir", "/work/cc-pc-tools-rs/cc-pc-tools-rs", container, "/opt/bridge/bridge", "execute", strconv.Itoa(jobs), strconv.Itoa(tests)}
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
	return rpc("/authorize", request{Repository: os.Getenv("GITHUB_REPOSITORY"), Revision: os.Getenv("GITHUB_SHA"), Ref: os.Getenv("GITHUB_REF"), Event: os.Getenv("GITHUB_EVENT_NAME")})
}
func hostMain(args []string) int {
	if len(args) != 3 || !revisionPattern.MatchString(args[0]) || !strings.HasPrefix(args[1], "scheduler/todo-2511-") {
		fmt.Fprintln(os.Stderr, "fixed revision, task branch, target required")
		return 1
	}
	key := make([]byte, 32)
	if _, e := rand.Read(key); e != nil {
		return 1
	}
	h := host{key: hex.EncodeToString(key), revision: args[0], branch: args[1], target: args[2]}
	if h.current() != nil {
		return 1
	}
	permission, e := output(command(gh, "repo", "view", repo, "--json", "viewerPermission", "--jq", ".viewerPermission"))
	if e != nil || permission != "ADMIN" {
		return 1
	}
	l, e := net.Listen("tcp", "127.0.0.1:17851")
	if e != nil {
		return 1
	}
	s := http.Server{Handler: http.HandlerFunc(h.serve), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = s.Serve(l) }()
	// The random bridge key and runner token stay in RAM and the official config
	// process. Neither appears in command arguments, Docker config or host files.
	token, e := output(command(gh, "api", "--method", "POST", "repos/"+repo+"/actions/runners/registration-token", "--jq", ".token"))
	if e != nil || token == "" {
		_ = s.Close()
		return 1
	}
	c := command(docker, "exec", "-i", container, "/opt/bridge/bridge", "register")
	c.Stdin = strings.NewReader(h.key + "\n" + token + "\n")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	code := exitCode(c.Run())
	token = ""
	_ = s.Shutdown(context.Background())
	return code
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
	if e = os.WriteFile("/tmp/bridge-key", []byte(strings.TrimSpace(key)), 0600); e != nil {
		return 1
	}
	c := exec.Command("/runner/config.sh", "--unattended", "--url", "https://github.com/"+repo, "--name", "cc-pc-tools-linux-2511", "--labels", "cc-pc-tools-linux", "--work", "/work", "--ephemeral", "--disableupdate")
	c.Dir = "/runner"
	c.Env = append(os.Environ(), "ACTIONS_RUNNER_INPUT_TOKEN="+strings.TrimSpace(token))
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if c.Run() != nil {
		return 1
	}
	token = ""
	c = exec.Command("/runner/run.sh")
	c.Dir = "/runner"
	c.Env = append(os.Environ(), "ACTIONS_RUNNER_HOOK_JOB_STARTED=/opt/bridge/job-hook.sh")
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return exitCode(c.Run())
}
func main() {
	if os.Getenv("CC_LINUX_GATED_CHILD") == "1" {
		os.Exit(gatedChild(os.Args[1:]))
	}
	args := os.Args[1:]
	if filepath.Base(os.Args[0]) == "cargo" {
		dir, _ := os.Getwd()
		os.Exit(rpc("/cargo", request{Args: args, Dir: dir}))
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
