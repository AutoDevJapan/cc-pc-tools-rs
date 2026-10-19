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
)

// callbackRequest is the whole vocabulary a guest can send. There is no field
// for a command, path, environment or URL: Cargo arguments must equal one of the
// fixed commands, and the Plan and nonce must match the host's.
type callbackRequest struct {
	Plan  Plan
	Nonce string
	Args  []string
}

type gatePacket struct {
	Plan               Plan
	Namespace          namespace
	Policy             execPolicy
	Nonce, Target, Dir string
	Index              int
	Fixture            bool
	CancelFixture      bool
	LostReceiptFixture bool
}

type cargoResult struct {
	Exit         int
	Log          []byte
	SHA          string
	Drained      bool
	FullLogSHA   string
	FullLogBytes int64
	LogTruncated bool
}

type host struct {
	plan               Plan
	kind               Kind
	policy             execPolicy
	key                string
	ns                 *namespace
	dir, target        string
	mu                 sync.Mutex
	seen               [5]bool
	started, completed bool
	broken             bool
	deadline           time.Time
	loseReceiptFixture bool
	recheck            func(context.Context) error
	execute            func(context.Context, int) (cargoResult, error)
}

func awaitAssignment(ctx context.Context, check func(context.Context) error, pause func(context.Context) error) error {
	var e error
	for attempt := 0; attempt < 5; attempt++ {
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = check(ctx); e == nil {
			return nil
		}
		if attempt < 4 {
			if pe := pause(ctx); pe != nil {
				return pe
			}
		}
	}
	return e
}

func pauseOneSecond(c context.Context) error {
	t := time.NewTimer(time.Second)
	defer t.Stop()
	select {
	case <-c.Done():
		return c.Err()
	case <-t.C:
		return nil
	}
}

func (h *host) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	routes := map[string]bool{"/civ6/start": true, "/civ6/complete": true}
	if h.kind == KindRust {
		routes["/civ6/cargo"], routes["/civ6/rustup"] = true, true
	}
	if r.Method != "POST" || !routes[r.URL.Path] || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Civ6-Nonce")), []byte(h.key)) != 1 {
		http.Error(w, "rejected", 403)
		return
	}
	var q callbackRequest
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(&q) != nil || d.Decode(new(any)) != io.EOF || q.Plan != h.plan || q.Nonce != h.key {
		http.Error(w, "identity rejected", 403)
		return
	}
	if r.URL.Path == "/civ6/start" || r.URL.Path == "/civ6/complete" {
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
		if r.URL.Path == "/civ6/start" {
			checkErr = awaitAssignment(checkCtx, h.recheck, pauseOneSecond)
		} else {
			checkErr = h.recheck(checkCtx)
		}
		if checkErr != nil {
			http.Error(w, "live job rejected", 403)
			return
		}
		if r.URL.Path == "/civ6/start" {
			if h.started {
				http.Error(w, "duplicate start", 409)
				return
			}
			h.started = true
			h.deadline = time.Now().Add(h.maxJob())
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
	index := h.policy.index(q.Args, r.URL.Path == "/civ6/rustup")
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
		deadline = time.Now().Add(h.maxJob())
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

func (h *host) maxJob() time.Duration {
	if t := h.policy.timeout(); t > 0 {
		return t
	}
	return 5 * time.Minute
}

func hostEnv() []string {
	// The isolated SDK has two CPUs and 10 GiB while the shared host can admit
	// ten compiler processes: submit lower caps through the ordinary gate. Its
	// actual granted values still drive the guest and can be lower again.
	out := []string{"CARGO_BUILD_JOBS=1", "RUST_TEST_THREADS=1", "NEXTEST_TEST_THREADS=1"}
	for _, k := range []string{"SystemRoot", "WINDIR", "SystemDrive", "TEMP", "TMP"} {
		if v := os.Getenv(k); v != "" {
			out = append(out, k+"="+v)
		}
	}
	return out
}

// translateGate verifies the gate argv: the complete fixed command prefix plus
// only the host-generated target/parallel additions are accepted.
func translateGate(args []string, packet gatePacket) (jobs, tests int, err error) {
	if packet.Index == cmdRustup {
		if !reflect.DeepEqual(args, rustupArgs) {
			return 0, 0, errors.New("rustup argv rejected")
		}
		return 1, 1, nil
	}
	if packet.Index < 0 || packet.Index > cmdBuild || packet.Target == "" || !packet.Policy.Present[packet.Index] {
		return 0, 0, errors.New("gate packet rejected")
	}
	base := cargoTable[packet.Index]
	jobs, e := strconv.Atoi(os.Getenv("CARGO_BUILD_JOBS"))
	tests, te := strconv.Atoi(os.Getenv("RUST_TEST_THREADS"))
	if packet.Index == cmdFmt {
		if !reflect.DeepEqual(args, base) {
			return 0, 0, errors.New("format argv changed")
		}
		return 1, 1, nil // cargo fmt is explicitly unlocked by the machine gate.
	}
	if e != nil || te != nil || jobs != 1 || tests != 1 || os.Getenv("CODEX_JOBS_CARGO_LOCK_HELD") != "1" || os.Getenv("CODEX_JOBS_CARGO_TARGET_HELD") == "" {
		return 0, 0, errors.New("shared Cargo gate permit absent")
	}
	if len(args) < len(base) || !reflect.DeepEqual(args[:len(base)], base) {
		return 0, 0, errors.New("original Cargo argv changed")
	}
	seenTarget, seenJobs := false, false
	for i := len(base); i < len(args); i++ {
		switch args[i] {
		case "--target-dir":
			if seenTarget || i+1 >= len(args) || args[i+1] != packet.Target {
				return 0, 0, errors.New("host target mapping rejected")
			}
			seenTarget = true
			i++
		case "--jobs", "-j":
			if seenJobs || i+1 >= len(args) || args[i+1] != strconv.Itoa(jobs) {
				return 0, 0, errors.New("gate parallel argv rejected")
			}
			seenJobs = true
			i++
		default:
			return 0, 0, errors.New("unknown gate-added argument")
		}
	}
	if !seenTarget {
		return 0, 0, errors.New("host target lock mapping absent")
	}
	return jobs, tests, nil
}

func (h *host) run(ctx context.Context, index int) (cargoResult, error) {
	return h.runWithFixture(ctx, index, false, false)
}

// runWithFixture holds the shared Cargo gate for the whole flight (existing
// cargo_gate.exe, same target exclusion and CPU/RAM admission). Cancellation is
// EOF on its stdin, never a kill before the Linux side has drained.
func (h *host) runWithFixture(ctx context.Context, index int, fixture, cancelFixture bool) (result cargoResult, ret error) {
	self, e := os.Executable()
	if e != nil {
		return result, e
	}
	p := gatePacket{Plan: h.plan, Namespace: *h.ns, Policy: h.policy, Nonce: h.key, Target: h.target, Dir: h.dir, Index: index, Fixture: fixture, CancelFixture: cancelFixture, LostReceiptFixture: h.loseReceiptFixture}
	input, e := json.Marshal(p)
	if e != nil {
		return result, e
	}
	args := append([]string{}, h.policy.args(index)...)
	if index != cmdFmt && index != cmdRustup {
		args = append(args, "--target-dir", h.target)
	}
	c := exec.Command(cargoGate, args...)
	if index == cmdRustup {
		c = exec.Command(self, args...)
	}
	hidden(c)
	c.Dir = h.dir
	c.Env = append(hostEnv(), "CODEX_JOBS_REAL_CARGO="+self, "CODEX_JOBS_CARGO_LOCK_DIR="+gateDir, "CC_CIL_GATE_CHILD=1", "CARGO_TARGET_DIR="+h.target)
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
		e := saveNew(filepath.Join(h.dir, stamp+".private.json"), struct {
			Exit                                       int
			Bytes                                      int64
			SHA                                        string
			Drained, PreviewTruncated, StorageComplete bool
			Failure                                    string
		}{result.Exit, result.FullLogBytes, result.FullLogSHA, result.Drained, result.LogTruncated, storageComplete, failure})
		ret = errors.Join(ret, e)
	}()
	writer := &logSink{file: log, limit: 32 << 20}
	c.Stdout, c.Stderr = writer, writer
	stdin, e := c.StdinPipe()
	if e != nil {
		return result, e
	}
	if e = c.Start(); e != nil {
		_ = stdin.Close()
		return result, e
	}
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
	var proof cargoResult
	b, pe := os.ReadFile(filepath.Join(h.dir, "gate-"+h.key+"-"+strconv.Itoa(index)+".private.json"))
	if pe != nil || json.Unmarshal(b, &proof) != nil || !proof.Drained {
		h.broken = true
		stopErr := h.ns.stop()
		result.Drained = h.ns.TreeGone
		return result, errors.Join(errors.New("gate child drain receipt absent; namespace disabled"), stopErr, storageErr)
	}
	result.Drained = true
	return result, storageErr // Cargo's own exit is preserved; log failures are infrastructure failures.
}

func startHost(ctx context.Context, h *host) (endpoint string, closeServer func() error, ret error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", nil, e
	}
	h.key = hex.EncodeToString(b)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return "", nil, e
	}
	s := &http.Server{Handler: h, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: h.maxJob() + 2*time.Minute, BaseContext: func(net.Listener) context.Context { return ctx }}
	done := make(chan struct{})
	go func() { _ = s.Serve(l); close(done) }()
	var once sync.Once
	var closeErr error
	return "http://host.docker.internal:" + strconv.Itoa(l.Addr().(*net.TCPAddr).Port) + "/civ6/", func() error {
		once.Do(func() {
			// The parent cancels ctx before shutdown. Each handler holds the gate
			// until guest drain, and Shutdown waits for every handler in flight.
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

// gatedChild runs under cargo_gate.exe as its "real Cargo". The caller never
// supplies a host executable, directory, Docker option, environment or guest
// command: only the fixed packet from the host's own stdin.
func gatedChild(args []string) int {
	dec := json.NewDecoder(io.LimitReader(os.Stdin, 64<<10))
	dec.DisallowUnknownFields()
	var p gatePacket
	if dec.Decode(&p) != nil || p.Plan.localCheck() != nil || len(p.Nonce) != 64 || !strings.HasPrefix(p.Namespace.Name, "civ6-ci-linux-") || filepath.Clean(p.Target) != filepath.Join(filepath.Clean(p.Dir), "cargo-target") {
		return 120
	}
	jobs, tests, e := translateGate(args, p)
	if e != nil {
		return 121
	}
	ctx, stop := context.WithTimeout(context.Background(), 30*time.Second)
	_, e = p.Namespace.inspect(ctx)
	stop()
	if e != nil {
		return 122
	}
	command := []string{"exec", "-i", p.Namespace.ID, "/runner/civ6-ci-linux", "guest-execute", strconv.Itoa(jobs), strconv.Itoa(tests), strconv.Itoa(p.Index), p.Nonce, "/runner/plan.json"}
	if p.Fixture {
		command = append(command, "fixture", strconv.FormatBool(p.CancelFixture))
	}
	c := cmd(context.Background(), dockerExe, command...)
	// Pass the actual pipe handle: a copying goroutine blocked on os.Stdin would
	// delay normal completion until the host sends cancellation EOF.
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
		forwardErr := forwardAndDrain(os.Stdout, reader)
		commandErr = c.Wait()
		if forwardErr != nil && commandErr == nil {
			commandErr = forwardErr
		}
	} else {
		commandErr = e
	}
	// Docker CLI failure/cancellation is not proof that Cargo exited. Require
	// the nonce-bound kernel-drain check, or stop the whole owned PID namespace
	// while this process still holds the machine gate.
	probe, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	pe = errors.New("supervisor handshake absent")
	if handshake.Nonce == p.Nonce && handshake.PGID > 1 {
		pe = cmd(probe, dockerExe, "exec", p.Namespace.ID, "/runner/civ6-ci-linux", "guest-check-group", strconv.Itoa(handshake.PGID)).Run()
	}
	cancel()
	if pe != nil {
		if se := p.Namespace.stop(); se != nil {
			// Retain the gate on teardown uncertainty: a recorded recovery hold,
			// not a reusable service or retry runner.
			fmt.Fprintln(os.Stderr, "namespace drain unverified; retaining shared gate for recorded recovery")
			for {
				time.Sleep(time.Second)
				c, stop := context.WithTimeout(context.Background(), 5*time.Second)
				b, e := p.Namespace.inspect(c)
				stop()
				if i, ie := parseInspect(b); e == nil && ie == nil && !i.State.Running && i.State.Pid == 0 {
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
		return 123 // synthetic metadata loss AFTER independent kernel drain
	}
	if e = saveNew(filepath.Join(p.Dir, "gate-"+p.Nonce+"-"+strconv.Itoa(p.Index)+".private.json"), cargoResult{Exit: code, Drained: true}); e != nil {
		return 123
	}
	return code
}

// requestHost is the guest side of the callback. The endpoint must be the host
// alias with the fixed path family; the response is size-bounded and checked.
func requestHost(ctx context.Context, endpoint, stage, key string, p Plan, args []string) (cargoResult, error) {
	var result cargoResult
	u, e := url.Parse(endpoint)
	if e != nil || u.Scheme != "http" || u.Hostname() != "host.docker.internal" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/civ6/" || len(key) != 64 {
		return result, errors.New("host callback endpoint rejected")
	}
	if port, e := strconv.Atoi(u.Port()); e != nil || port < 1 || port > 65535 {
		return result, errors.New("host callback port rejected")
	}
	switch stage {
	case "start", "complete", "cargo", "rustup":
	default:
		return result, errors.New("host callback stage rejected")
	}
	b, _ := json.Marshal(callbackRequest{p, key, args})
	r, e := http.NewRequestWithContext(ctx, "POST", endpoint+stage, bytes.NewReader(b))
	if e != nil {
		return result, e
	}
	r.Header.Set("X-Civ6-Nonce", key)
	t := &http.Transport{Proxy: nil}
	defer t.CloseIdleConnections()
	client := http.Client{Transport: t, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(r)
	if e != nil {
		return result, errors.New("finite host callback unavailable")
	}
	defer resp.Body.Close()
	if stage == "start" || stage == "complete" {
		if resp.StatusCode != http.StatusNoContent {
			return result, errors.New("live hook admission rejected")
		}
		return result, nil
	}
	if resp.StatusCode != 200 {
		return result, errors.New("finite Cargo callback rejected")
	}
	d := json.NewDecoder(io.LimitReader(resp.Body, 48<<20))
	if d.Decode(&result) != nil || !result.Drained || digest(result.Log) != result.SHA {
		return result, errors.New("Cargo response integrity rejected")
	}
	return result, nil
}
