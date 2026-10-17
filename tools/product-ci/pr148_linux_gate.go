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
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const pr148CargoGate = `D:\CC\Tools\codex-jobs\cargo_gate.exe`
const pr148GateDir = `D:\CC\Tools\codex-jobs`

var pr148CargoArgs = [][]string{
	{"fmt", "--manifest-path", "tools/Cargo.toml", "--all", "--", "--check"},
	{"test", "--locked", "--manifest-path", "tools/Cargo.toml", "--workspace", "--all-targets"},
	{"test", "--locked", "--manifest-path", "tools/Cargo.toml", "--workspace", "--doc"},
	{"build", "--locked", "--release", "--manifest-path", "tools/Cargo.toml", "--workspace", "--bins"},
}

var pr148RustupArgs = []string{"toolchain", "install", "1.95.0", "--profile", "minimal", "--component", "rustfmt"}

// Only the four original commands can enter. Format argument order is read
// from the hash-pinned workflow, not inferred from a caller's command string.
type pr148CargoPolicy [4]string

func pr148Policy(workflow []byte) (pr148CargoPolicy, error) {
	var policy pr148CargoPolicy
	if digest(workflow) != pr148Workflows[0].OriginalSHA {
		return policy, errors.New("original Cargo workflow digest rejected")
	}
	root, e := pr148YAML(workflow)
	if e != nil {
		return policy, e
	}
	job := pr148Field(pr148Field(root, "jobs"), "test")
	steps := pr148Field(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return policy, errors.New("original Rust steps missing")
	}
	for _, step := range steps.Content {
		run := pr148Field(step, "run")
		if run == nil {
			continue
		}
		for _, line := range strings.Split(run.Value, "\n") {
			fields := strings.Fields(strings.TrimSpace(line))
			if len(fields) < 2 || fields[0] != "cargo" {
				continue
			}
			args := fields[1:]
			for i, want := range pr148CargoArgs {
				ok := reflect.DeepEqual(args, want)
				if i == 0 {
					ok = args[0] == "fmt" && !strings.ContainsAny(strings.Join(args, " "), "'\";$`\\")
				}
				if ok {
					policy[i] = strings.Join(args, " ")
				}
			}
		}
	}
	for _, s := range policy {
		if s == "" {
			return policy, errors.New("original Linux Cargo command set not recognized; do not substitute commands")
		}
	}
	return policy, nil
}

type pr148CargoRequest struct {
	Plan   pr148LinuxPlan
	RepoID int64
	Nonce  string
	Args   []string
}
type pr148GatePacket struct {
	Plan               pr148LinuxPlan
	Namespace          pr148Namespace
	Policy             pr148CargoPolicy
	Nonce, Target, Dir string
	Index              int
	Fixture            bool
	CancelFixture      bool
	LostReceiptFixture bool
}
type pr148CargoResult struct {
	Exit         int
	Log          []byte
	SHA          string
	Drained      bool
	FullLogSHA   string
	FullLogBytes int64
	LogTruncated bool
}
type pr148CargoHost struct {
	plan               pr148LinuxPlan
	policy             pr148CargoPolicy
	key                string
	ns                 *pr148Namespace
	dir, target        string
	mu                 sync.Mutex
	seen               [5]bool
	started, completed bool
	broken             bool
	execution          pr148ExecutionPolicy
	deadline           time.Time
	loseReceiptFixture bool
	recheck            func(context.Context) error
	execute            func(context.Context, int) (pr148CargoResult, error)
}

var errPR148AssignmentPending = errors.New("fixed job assignment not yet visible")

func pr148AwaitAssignment(ctx context.Context, check func(context.Context) error, pause func(context.Context) error) error {
	for attempt := 0; attempt < 5; attempt++ {
		if e := ctx.Err(); e != nil {
			return e
		}
		e := check(ctx)
		if !errors.Is(e, errPR148AssignmentPending) || attempt == 4 {
			return e
		}
		if e = pause(ctx); e != nil {
			return e
		}
	}
	return errPR148AssignmentPending
}

func (h *pr148CargoHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" || (r.URL.Path != "/pr148/cargo" && r.URL.Path != "/pr148/rustup" && r.URL.Path != "/pr148/start" && r.URL.Path != "/pr148/complete") || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-PR148-Nonce")), []byte(h.key)) != 1 {
		http.Error(w, "rejected", 403)
		return
	}
	var q pr148CargoRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(&q) != nil || d.Decode(new(any)) != io.EOF || q.Plan != h.plan || q.RepoID != 1392152644 || q.Nonce != h.key {
		http.Error(w, "identity rejected", 403)
		return
	}
	index := -1
	if r.URL.Path == "/pr148/start" || r.URL.Path == "/pr148/complete" {
		if len(q.Args) != 0 {
			http.Error(w, "hook arguments rejected", 403)
			return
		}
		if !h.mu.TryLock() {
			http.Error(w, "target occupied", 409)
			return
		}
		defer h.mu.Unlock()
		checkCtx, checkStop := context.WithTimeout(r.Context(), 60*time.Second)
		defer checkStop()
		var checkErr error
		if r.URL.Path == "/pr148/start" {
			checkErr = pr148AwaitAssignment(checkCtx, h.recheck, func(c context.Context) error {
				timer := time.NewTimer(time.Second)
				defer timer.Stop()
				select {
				case <-c.Done():
					return c.Err()
				case <-timer.C:
					return nil
				}
			})
		} else {
			checkErr = h.recheck(checkCtx)
		}
		if checkErr != nil {
			http.Error(w, "live job rejected", 403)
			return
		}
		if r.URL.Path == "/pr148/start" {
			if h.started {
				http.Error(w, "duplicate start", 409)
				return
			}
			h.started = true
			h.deadline = time.Now().Add(h.execution.timeout())
		} else {
			if !h.started || h.completed {
				http.Error(w, "job stage rejected", 409)
				return
			}
			h.completed = true
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.URL.Path == "/pr148/cargo" {
		for i, s := range h.policy {
			if reflect.DeepEqual(q.Args, strings.Fields(s)) {
				index = i
			}
		}
	} else if r.URL.Path == "/pr148/rustup" && reflect.DeepEqual(q.Args, pr148RustupArgs) {
		index = 4
	}
	if index < 0 {
		http.Error(w, "command rejected", 403)
		return
	}
	// A nested wrapper cannot wait for the target held by its own ancestor.
	if !h.mu.TryLock() {
		http.Error(w, "target occupied; nested invocation rejected", 409)
		return
	}
	defer h.mu.Unlock()
	if h.broken || !h.started || h.completed || h.seen[index] {
		http.Error(w, "job stage/replay rejected", 409)
		return
	}
	h.seen[index] = true
	deadline := h.deadline
	if deadline.IsZero() {
		deadline = time.Now().Add(h.execution.timeout())
		if h.execution.timeout() == 0 {
			deadline = time.Now().Add(5 * time.Minute)
		}
	}
	ctx, cancel := context.WithDeadline(r.Context(), deadline)
	defer cancel()
	if e := h.recheck(ctx); e != nil {
		http.Error(w, "live source rejected", 403)
		return
	}
	result, e := h.execute(ctx, index)
	if e != nil || !result.Drained {
		http.Error(w, "Cargo drain unverified; see owned receipt", 500)
		return
	}
	if e := h.recheck(ctx); e != nil {
		http.Error(w, "source changed after Cargo", 403)
		return
	}
	_ = json.NewEncoder(w).Encode(result)
}

func pr148HostEnv() []string {
	var out []string
	for _, k := range []string{"SystemRoot", "WINDIR", "SystemDrive", "TEMP", "TMP"} {
		if v := os.Getenv(k); v != "" {
			out = append(out, k+"="+v)
		}
	}
	return out
}

// Gate argv is never copied verbatim into the guest. Verify the complete
// original prefix and only the host-generated target/parallel additions.
func pr148TranslateGate(args []string, packet pr148GatePacket) ([]string, int, int, error) {
	if packet.Index == 4 {
		if !reflect.DeepEqual(args, pr148RustupArgs) {
			return nil, 0, 0, errors.New("rustup argv rejected")
		}
		return args, 1, 1, nil
	}
	if packet.Index < 0 || packet.Index > 3 || packet.Target == "" {
		return nil, 0, 0, errors.New("gate packet rejected")
	}
	base := strings.Fields(packet.Policy[packet.Index])
	jobs, e := strconv.Atoi(os.Getenv("CARGO_BUILD_JOBS"))
	tests, te := strconv.Atoi(os.Getenv("RUST_TEST_THREADS"))
	if packet.Index == 0 {
		if !reflect.DeepEqual(args, base) {
			return nil, 0, 0, errors.New("format argv changed")
		}
		return base, 1, 1, nil // cargo fmt is explicitly unlocked by the machine gate.
	}
	if e != nil || te != nil || jobs < 1 || jobs > 10 || tests < 1 || tests > 10 || os.Getenv("CODEX_JOBS_CARGO_LOCK_HELD") != "1" || os.Getenv("CODEX_JOBS_CARGO_TARGET_HELD") == "" {
		return nil, 0, 0, errors.New("shared Cargo gate permit absent")
	}
	if len(args) < len(base) || !reflect.DeepEqual(args[:len(base)], base) {
		return nil, 0, 0, errors.New("original Cargo argv changed")
	}
	seenTarget, seenJobs := false, false
	for i := len(base); i < len(args); i++ {
		switch args[i] {
		case "--target-dir":
			if seenTarget || i+1 >= len(args) || args[i+1] != packet.Target {
				return nil, 0, 0, errors.New("host target mapping rejected")
			}
			seenTarget = true
			i++
		case "--jobs", "-j":
			if seenJobs || i+1 >= len(args) || args[i+1] != strconv.Itoa(jobs) {
				return nil, 0, 0, errors.New("gate parallel argv rejected")
			}
			seenJobs = true
			i++
		default:
			return nil, 0, 0, errors.New("unknown gate-added argument")
		}
	}
	if !seenTarget {
		return nil, 0, 0, errors.New("host target lock mapping absent")
	}
	return base, jobs, tests, nil
}

func (h *pr148CargoHost) run(ctx context.Context, index int) (result pr148CargoResult, ret error) {
	return h.runWithFixture(ctx, index, false, false)
}
func (h *pr148CargoHost) runWithFixture(ctx context.Context, index int, fixture, cancelFixture bool) (result pr148CargoResult, ret error) {
	self, e := os.Executable()
	if e != nil {
		return result, e
	}
	p := pr148GatePacket{Plan: h.plan, Namespace: *h.ns, Policy: h.policy, Nonce: h.key, Target: h.target, Dir: h.dir, Index: index, Fixture: fixture, CancelFixture: cancelFixture, LostReceiptFixture: h.loseReceiptFixture}
	input, e := json.Marshal(p)
	if e != nil {
		return result, e
	}
	var args []string
	if index == 4 {
		args = pr148RustupArgs
	} else {
		args = strings.Fields(h.policy[index])
		if index != 0 {
			args = append(args, "--target-dir", h.target)
		}
	}
	c := exec.Command(pr148CargoGate, args...)
	if index == 4 {
		c = exec.Command(self, args...)
	}
	hidden(c)
	c.Dir = h.dir
	c.Env = append(pr148HostEnv(), "CODEX_JOBS_REAL_CARGO="+self, "CODEX_JOBS_CARGO_LOCK_DIR="+pr148GateDir, "CC_PR148_GATE_CHILD=1", "CARGO_TARGET_DIR="+h.target)
	stamp := fmt.Sprintf("cargo-%d-%d", index, time.Now().UnixNano())
	log, e := os.OpenFile(filepath.Join(h.dir, stamp+".private.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return result, e
	}
	defer log.Close()
	storageComplete := false
	defer func() {
		failure := ""
		if ret != nil {
			failure = ret.Error()
		}
		e := pr148SaveNew(filepath.Join(h.dir, stamp+".private.json"), struct {
			Exit                                       int
			Bytes                                      int64
			SHA                                        string
			Drained, PreviewTruncated, StorageComplete bool
			Failure                                    string
		}{result.Exit, result.FullLogBytes, result.FullLogSHA, result.Drained, result.LogTruncated, storageComplete, failure})
		ret = errors.Join(ret, e)
	}()
	writer := &pr148LogSink{file: log, limit: 32 << 20}
	c.Stdout = writer
	c.Stderr = writer
	stdin, e := c.StdinPipe()
	if e != nil {
		return result, e
	}
	if e = c.Start(); e != nil {
		_ = stdin.Close()
		return result, e
	}
	// JSON ends with a newline, but stdin stays OPEN for the entire gate flight.
	// Cancellation is EOF, not killing cargo_gate before Linux has drained.
	written := make(chan error, 1)
	go func() { _, e := stdin.Write(append(input, '\n')); written <- e }()
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case ret = <-done:
	case <-ctx.Done():
		_ = stdin.Close()
		ret = <-done
	}
	_ = stdin.Close()
	waitErr := ret
	ret = errors.Join(ret, <-written)
	result.Exit = exitStatus(waitErr)
	result.Log = append([]byte(nil), writer.preview.Bytes()...)
	result.SHA = digest(result.Log)
	result.FullLogSHA = writer.SHA()
	result.FullLogBytes = writer.bytes
	result.LogTruncated = writer.bytes > int64(len(result.Log))
	storageErr := errors.Join(writer.err, log.Sync())
	storageComplete = storageErr == nil
	// A gate-child final record is required even if Cargo failed. A missing
	// record means no admission or an uncertain Docker exec, never success.
	var proof pr148CargoResult
	b, pe := os.ReadFile(filepath.Join(h.dir, "gate-"+h.key+"-"+strconv.Itoa(index)+".private.json"))
	if pe != nil || json.Unmarshal(b, &proof) != nil || !proof.Drained {
		h.broken = true
		stopErr := h.ns.stop()
		result.Drained = h.ns.TreeGone
		return result, errors.Join(errors.New("gate child drain receipt absent; namespace disabled"), stopErr, storageErr)
	}
	result.Drained = true
	return result, storageErr // preserve Cargo exit; log failures are infrastructure failures.
}

func startPR148Cargo(ctx context.Context, h *pr148CargoHost) (endpoint string, closeServer func() error, ret error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", nil, e
	}
	h.key = hex.EncodeToString(b)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return "", nil, e
	}
	budget := h.execution.timeout()
	if budget == 0 {
		budget = 5 * time.Minute
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: budget + 2*time.Minute, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	go func() { _ = s.Serve(l); close(done) }()
	var once sync.Once
	var closeErr error
	return "http://host.docker.internal:" + strconv.Itoa(l.Addr().(*net.TCPAddr).Port) + "/pr148/cargo", func() error {
		once.Do(func() {
			// The parent cancels ctx before shutdown. Every handler retains the gate
			// through guest drain, and Shutdown waits for the complete handler flight.
			c, stop := context.WithTimeout(context.Background(), 60*time.Second)
			defer stop()
			e := s.Shutdown(c)
			if e != nil {
				_ = s.Close()
			}
			<-done
			h.mu.Lock()
			h.mu.Unlock()
			closeErr = e
		})
		return closeErr
	}, nil
}

// Windows-only gate callback. The caller never supplies a host executable,
// directory, Docker option, environment or arbitrary guest command.
func pr148GatedChild(args []string) int {
	dec := json.NewDecoder(io.LimitReader(os.Stdin, 64<<10))
	dec.DisallowUnknownFields()
	var p pr148GatePacket
	if dec.Decode(&p) != nil || !p.Plan.valid() || len(p.Nonce) != 64 || !strings.HasPrefix(p.Namespace.Name, "todo2736-linux-") || filepath.Clean(p.Target) != filepath.Join(filepath.Clean(p.Dir), "cargo-target") {
		return 120
	}
	base, jobs, tests, e := pr148TranslateGate(args, p)
	if e != nil {
		return 121
	}
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	_, e = p.Namespace.inspect(ctx)
	stop()
	if e != nil {
		return 122
	}
	command := []string{"exec", "-i", p.Namespace.ID, "/runner/product-ci", "pr148-execute", strconv.Itoa(jobs), strconv.Itoa(tests), strconv.Itoa(p.Index), p.Nonce, "/runner/pr148-plan.json"}
	if p.Fixture {
		command = append(command, "fixture", strconv.FormatBool(p.CancelFixture))
	}
	_ = base // guest obtains only the fixed policy from its private tmpfs packet.
	c := cmd(context.Background(), docker, command...)
	// Pass the actual pipe handle. A copying goroutine blocked on os.Stdin
	// would delay normal completion until the host sends cancellation EOF.
	c.Stdin = os.Stdin
	out, pe := c.StdoutPipe()
	if pe != nil {
		return 123
	}
	c.Stderr = os.Stderr
	var handshake struct {
		Nonce string
		PGID  int
	}
	var commandErr error
	if e = c.Start(); e == nil {
		reader := bufio.NewReader(out)
		line, re := reader.ReadBytes('\n')
		if re == nil && len(line) < 4096 {
			_ = json.Unmarshal(line, &handshake)
		}
		forwardErr := pr148ForwardAndDrain(os.Stdout, reader)
		commandErr = c.Wait()
		if forwardErr != nil && commandErr == nil {
			commandErr = forwardErr
		}
	} else {
		commandErr = e
	}
	// Docker CLI failure/cancellation is not proof that Linux Cargo exited.
	// Require the nonce-bound kernel-drain record, or stop the entire owned PID
	// namespace while this callback still holds the machine gate.
	probe, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	pe = errors.New("supervisor handshake absent")
	if handshake.Nonce == p.Nonce && handshake.PGID > 1 {
		pe = cmd(probe, docker, "exec", p.Namespace.ID, "/runner/product-ci", "pr148-check-group", strconv.Itoa(handshake.PGID)).Run()
	}
	cancel()
	if pe != nil {
		if se := p.Namespace.stop(); se != nil {
			// Deliberately retain the gate on teardown uncertainty. This is an
			// exceptional recovery hold, not a reusable service or retry runner.
			fmt.Fprintln(os.Stderr, "namespace drain unverified; retaining shared gate for recorded recovery")
			for {
				time.Sleep(time.Second)
				c, stop := context.WithTimeout(context.Background(), 5*time.Second)
				b, e := p.Namespace.inspect(c)
				stop()
				var s []struct {
					State struct {
						Running bool
						Pid     int
					}
				}
				if e == nil && json.Unmarshal(b, &s) == nil && len(s) == 1 && !s[0].State.Running && s[0].State.Pid == 0 {
					break
				}
			}
		}
	}
	code := exitStatus(commandErr)
	if pe != nil && code == 0 {
		code = 124
	}
	if p.Fixture && p.LostReceiptFixture {
		return 123
	} // synthetic metadata-loss AFTER independent kernel drain.
	if e = pr148SaveNew(filepath.Join(p.Dir, "gate-"+p.Nonce+"-"+strconv.Itoa(p.Index)+".private.json"), pr148CargoResult{Exit: code, Drained: true}); e != nil {
		return 123
	}
	return code
}

func pr148RequestCargo(ctx context.Context, endpoint, key string, p pr148LinuxPlan, args []string) (pr148CargoResult, error) {
	var result pr148CargoResult
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "http" || u.Hostname() != "host.docker.internal" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "/pr148/cargo" && u.Path != "/pr148/rustup") || len(key) != 64 {
		return result, errors.New("Cargo callback endpoint rejected")
	}
	port, e := strconv.Atoi(u.Port())
	if e != nil || port < 1 || port > 65535 {
		return result, errors.New("Cargo callback port rejected")
	}
	b, _ := json.Marshal(pr148CargoRequest{p, 1392152644, key, args})
	r, e := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(b))
	if e != nil {
		return result, e
	}
	r.Header.Set("X-PR148-Nonce", key)
	t := &http.Transport{Proxy: nil}
	defer t.CloseIdleConnections()
	client := http.Client{Transport: t, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(r)
	if e != nil {
		return result, errors.New("finite Cargo callback unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return result, errors.New("finite Cargo callback rejected")
	}
	d := json.NewDecoder(io.LimitReader(resp.Body, 48<<20))
	if d.Decode(&result) != nil || !result.Drained || digest(result.Log) != result.SHA {
		return result, errors.New("Cargo response integrity rejected")
	}
	return result, nil
}
