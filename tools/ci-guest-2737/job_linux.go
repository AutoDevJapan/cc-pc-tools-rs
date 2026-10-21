package main

import (
	"bytes"
	sourcepacket "cc/ci-source-2737"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func strictBlob(input io.Reader, limit uint64, out any) error {
	b, e := readBlob(input, limit)
	if e != nil {
		return e
	}
	defer func() {
		for n := range b {
			b[n] = 0
		}
	}()
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return errors.New("job control frame rejected")
	}
	return nil
}

type liveWire struct {
	mu   sync.Mutex
	out  *json.Encoder
	conn net.Conn
	acks <-chan liveJobAck
	ctx  context.Context
}

// The action may spawn node through PATH even when its entry point uses the
// runner's node24. Keep that bootstrap on the same already extracted SDK.
const liveRunnerPath = "/opt/ci2737-runner/externals/node24/bin:/home/ci2737/.cargo/bin:/usr/local/bin:/usr/bin:/bin"

// Each event owns its write timeout. In particular, an admission timeout must
// not remain on the stream while the runner executes the admitted job.
func writeLiveEvent(conn net.Conn, out *json.Encoder, event liveJobEvent) (err error) {
	if conn != nil {
		if err = conn.SetWriteDeadline(time.Now().Add(15 * time.Second)); err != nil {
			return err
		}
		defer func() { err = errors.Join(err, conn.SetWriteDeadline(time.Time{})) }()
	}
	return out.Encode(event)
}

func (w *liveWire) permit(ctx context.Context, id string, event liveJobEvent) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if e := writeLiveEvent(w.conn, w.out, event); e != nil {
		return errors.New("job event delivery failed")
	}
	select {
	case a, ok := <-w.acks:
		if !ok || a.ID != id || a.Stage != event.Stage || !a.Accepted {
			return errors.New("live job admission rejected")
		}
		return nil
	case <-ctx.Done():
		return errors.New("live job admission cancelled")
	}
}

type localJobOwner struct {
	mu                 sync.Mutex
	plan               linuxJobPlan
	entries            []sourcepacket.TreeEntry
	id                 string
	wire               *liveWire
	started, completed bool
	diagnostic         *hookDiagnostic
}

func (o *localJobOwner) remember(stage, code string) {
	d := &hookDiagnostic{Version: 1, Stage: stage, Code: code}
	if o.diagnostic == nil && validHookDiagnostic(d) {
		o.diagnostic = d
	}
}

func (o *localJobOwner) handle(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(50 * time.Second))
	d := json.NewDecoder(io.LimitReader(c, 32768))
	d.DisallowUnknownFields()
	var q jobHookContext
	if d.Decode(&q) != nil {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	ok := false
	defer func() { _ = json.NewEncoder(c).Encode(struct{ Accepted bool }{ok}) }()
	if q.Diagnostic != nil {
		if q.Diagnostic.Stage == q.Stage && validHookDiagnostic(q.Diagnostic) {
			o.remember(q.Stage, q.Diagnostic.Code)
		}
		return // A diagnostic is never an admission or a source proof.
	}
	if code := jobContextFailure(o.plan, q); code != "" {
		o.remember(q.Stage, code)
		return
	}
	if (q.Stage != "start" && q.Stage != "complete") || (q.Stage == "start" && o.started) || (q.Stage == "complete" && (!o.started || o.completed)) {
		o.remember(q.Stage, "hook_order")
		return
	}
	ctx, cancel := context.WithTimeout(o.wire.ctx, 40*time.Second)
	defer cancel()
	proof, e := sourcepacket.VerifyCheckout(ctx, o.plan.Source, o.entries, liveWorkspaceFor(o.plan.Source))
	if e != nil {
		d := &hookDiagnostic{Version: 1, Stage: q.Stage, Code: sourceFailureCode(e), Metadata: sourcepacket.MetadataFailure(e)}
		if o.diagnostic == nil && validHookDiagnostic(d) {
			o.diagnostic = d
		}
		return
	}
	if e = o.wire.permit(ctx, o.id, liveJobEvent{ID: o.id, Stage: q.Stage, Proof: proof, Context: q}); e != nil {
		o.remember(q.Stage, "host_admission")
		return
	}
	if q.Stage == "start" {
		o.started = true
	} else {
		o.completed = true
	}
	o.diagnostic = nil // An admitted stage supersedes earlier rejected attempts.
	ok = true
}

// The caller must already own a canonical shared Cargo lease. The public seed
// fixes the source expectation; this stream cannot change it or select a shell.
func serveLiveJob(conn net.Conn, input io.Reader, id string, expected sourcepacket.Pin, lifetime *time.Timer) {
	out := json.NewEncoder(conn)
	fail := func(code string) { _ = writeLiveEvent(conn, out, liveJobEvent{ID: id, Stage: "failed", Failure: code}) }
	var plan linuxJobPlan
	if strictBlob(input, 32768, &plan) != nil || !validLinuxJob(plan, expected, id) {
		fail("job_plan_rejected")
		return
	}
	// Extend only after accepting the exact role/source/instance. A short frame
	// deadline must not remain on the ACK reader during the original long job.
	if conn.SetDeadline(time.Now().Add(jobGuestTimeout(plan.Source, plan.Role))) != nil {
		return
	}
	if plan.Source.Product == sourcepacket.PressProduct && lifetime != nil {
		// The boot-time cap cannot shorten a long role admitted later. Alpha's
		// original absolute 45-minute guest lifetime remains unchanged.
		lifetime.Reset(jobGuestTimeout(plan.Source, plan.Role) + 5*time.Minute)
	}
	raw, e := readBlob(input, 256<<20)
	if e != nil {
		fail("job_source_frame_rejected")
		return
	}
	p, e := sourcepacket.Decode(raw, expected)
	raw = nil
	if e != nil {
		fail("job_source_packet_rejected")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), jobGuestTimeout(plan.Source, plan.Role))
	defer cancel()
	var sdkInfo *pressSDKInventory
	if plan.Source.Product == sourcepacket.PressProduct {
		v, err := preparePressSDK(ctx)
		if err != nil {
			_ = writeLiveEvent(conn, out, liveJobEvent{ID: id, Stage: "failed", Failure: "press_sdk_preparation_failed", PressSDK: &v})
			return
		}
		sdkInfo = &v
	}
	proof, e := sourcepacket.Materialize(ctx, p, expected, liveWorkspaceFor(expected))
	if e != nil {
		fail("job_source_materialization_failed")
		return
	}
	if e = filepath.Walk("/home/ci2737/work", func(path string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("job source link rejected")
		}
		return os.Chown(path, 1001, 1001)
	}); e != nil {
		fail("job_source_owner_failed")
		return
	}
	if e = writeLiveEvent(conn, out, liveJobEvent{ID: id, Stage: "source-ready", Proof: proof, RustProbe: probeLiveRust(ctx, plan.Source), PressSDK: sdkInfo}); e != nil {
		return
	}
	var config struct{ Token string }
	if strictBlob(input, 16384, &config) != nil || len(config.Token) < 1 || len(config.Token) > 4096 {
		fail("job_token_frame_rejected")
		return
	}
	if e = configureLiveRunner(ctx, plan, config.Token); e != nil {
		config.Token = ""
		fail("job_configuration_failed")
		return
	}
	config.Token = ""
	if e = writeLiveEvent(conn, out, liveJobEvent{ID: id, Stage: "configured", Configured: true}); e != nil {
		return
	}
	var listen liveJobAck
	if strictBlob(input, 8192, &listen) != nil || listen.ID != id || listen.Stage != "listen" || !listen.Accepted || listen.RunnerID < 1 {
		fail("job_listen_not_authorized")
		return
	}
	plan.RunnerID = listen.RunnerID
	acks := make(chan liveJobAck, 1)
	go func() {
		defer close(acks)
		for {
			var ack liveJobAck
			if strictBlob(input, 8192, &ack) != nil {
				cancel()
				return
			}
			select {
			case acks <- ack:
			case <-ctx.Done():
				return
			}
		}
	}()
	wire := &liveWire{out: out, conn: conn, acks: acks, ctx: ctx}
	owner := &localJobOwner{plan: plan, entries: p.TreeEntries, id: id, wire: wire}
	if e = os.Mkdir("/opt/ci2737-control", 0755); e != nil {
		fail("job_hook_directory_failed")
		return
	}
	b, _ := json.Marshal(plan)
	if e = os.WriteFile(livePlanFile, b, 0644); e != nil {
		fail("job_hook_plan_failed")
		return
	}
	for _, stage := range []string{"start", "complete"} {
		if e = os.WriteFile("/opt/ci2737-control/"+stage+".sh", []byte("#!/bin/sh\nexec /usr/local/bin/ci2737-guest job-hook "+stage+"\n"), 0755); e != nil {
			fail("job_hook_script_failed")
			return
		}
	}
	l, e := net.Listen("unix", liveSocket)
	if e != nil {
		fail("job_hook_listener_failed")
		return
	}
	defer l.Close()
	if os.Chmod(liveSocket, 0600) != nil || os.Chown(liveSocket, 1001, 1001) != nil {
		fail("job_hook_socket_owner_failed")
		return
	}
	go func() {
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			go owner.handle(c)
		}
	}()
	group, e := user.LookupGroup("docker")
	if e != nil {
		fail("job_docker_group_unavailable")
		return
	}
	gid, e := strconv.ParseUint(group.Gid, 10, 32)
	if e != nil {
		fail("job_docker_group_invalid")
		return
	}
	cmd := exec.CommandContext(ctx, "/opt/ci2737-runner/run.sh")
	cmd.Dir = "/opt/ci2737-runner"
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Credential: &syscall.Credential{Uid: 1001, Gid: 1001, Groups: []uint32{uint32(gid)}}}
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.Env = []string{"HOME=/home/ci2737", "LANG=C.UTF-8", "PATH=" + liveRunnerPath, "CARGO_HOME=/home/ci2737/.cargo", "RUSTUP_HOME=/home/ci2737/.rustup", "CARGO_TARGET_DIR=/home/ci2737/ci-target", "CARGO_BUILD_JOBS=" + strconv.Itoa(plan.Jobs), "RUST_TEST_THREADS=" + strconv.Itoa(plan.Threads), "NEXTEST_TEST_THREADS=" + strconv.Itoa(plan.Threads), "RUSTC_WRAPPER=/usr/local/bin/sccache", "SCCACHE_DIR=/home/ci2737/.cache/sccache", "SCCACHE_CACHE_SIZE=30G", "ACTIONS_RUNNER_HOOK_JOB_STARTED=/opt/ci2737-control/start.sh", "ACTIONS_RUNNER_HOOK_JOB_COMPLETED=/opt/ci2737-control/complete.sh", "CUDA_VISIBLE_DEVICES="}
	var log sdkBuffer
	log.Limit = 16 << 20
	cmd.Stdout = &log
	cmd.Stderr = &log
	e = cmd.Run()
	code := 0
	if e != nil {
		code = 1
		if exit, ok := e.(*exec.ExitError); ok {
			code = exit.ExitCode()
		}
	}
	owner.mu.Lock()
	complete := owner.started && owner.completed
	diagnostic := owner.diagnostic
	owner.mu.Unlock()
	terminalProbe := probeLiveRust(ctx, plan.Source)
	if !complete {
		// Wait until run.sh exits so its official logs can finish uploading before
		// the host reacts to this terminal failure and stops the owned VM.
		_ = writeLiveEvent(conn, out, liveJobEvent{ID: id, Stage: "failed", Failure: "job_hooks_not_completed", HookDiagnostic: diagnostic, RustProbe: terminalProbe})
		return
	}
	_ = writeLiveEvent(conn, out, liveJobEvent{ID: id, Stage: "runner-exited", Exit: &code, RustProbe: terminalProbe})
}
func configureLiveRunner(ctx context.Context, p linuxJobPlan, token string) error {
	profile, ok := p.Source.Profile()
	if !ok {
		return errors.New("runner source profile rejected")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, "/opt/ci2737-runner/config.sh", "--unattended", "--url", "https://github.com/"+profile.Repository, "--name", p.Name, "--labels", p.Label, "--no-default-labels", "--work", "/home/ci2737/work", "--ephemeral", "--disableupdate")
	c.Dir = "/opt/ci2737-runner"
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Credential: &syscall.Credential{Uid: 1001, Gid: 1001}}
	c.Env = []string{"HOME=/home/ci2737", "LANG=C.UTF-8", "PATH=/usr/local/bin:/usr/bin:/bin", "ACTIONS_RUNNER_INPUT_TOKEN=" + token}
	c.Stdout = io.Discard
	c.Stderr = io.Discard
	c.WaitDelay = 5 * time.Second
	c.Cancel = func() error {
		if c.Process != nil {
			return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	e := c.Run()
	c.Env = nil
	return e
}

func liveJobHook(stage string) (retErr error) {
	defer func() {
		if retErr == nil {
			return
		}
		code := retErr.Error()
		aliases := map[string]string{"job hook plan unavailable": "hook_plan_unavailable", "job hook plan invalid": "hook_plan_invalid", "job event location rejected": "event_path_outside_temp", "job event unavailable": "event_unavailable", "job event invalid": "event_json_invalid", "job input type rejected": "event_inputs_invalid", "job input size rejected": "event_inputs_invalid", "job hook owner unavailable": "owner_unavailable", "job hook transfer failed": "owner_transfer", "job hook rejected": "owner_rejected"}
		if alias, ok := aliases[code]; ok {
			code = alias
		}
		d := &hookDiagnostic{Version: 1, Stage: stage, Code: code}
		if !validHookDiagnostic(d) {
			return
		}
		c, e := net.DialTimeout("unix", liveSocket, time.Second)
		if e != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		if json.NewEncoder(c).Encode(jobHookContext{Stage: stage, Diagnostic: d}) == nil {
			var reply struct{ Accepted bool }
			_ = json.NewDecoder(io.LimitReader(c, 4096)).Decode(&reply)
		}
	}()
	if stage != "start" && stage != "complete" {
		return errors.New("job hook stage rejected")
	}
	b, e := os.ReadFile(livePlanFile)
	if e != nil {
		return errors.New("job hook plan unavailable")
	}
	var p linuxJobPlan
	if json.Unmarshal(b, &p) != nil {
		return errors.New("job hook plan invalid")
	}
	event := os.Getenv("GITHUB_EVENT_PATH")
	rel, e := filepath.Rel("/home/ci2737/work/_temp", event)
	if e != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") {
		return errors.New("job event location rejected")
	}
	f, e := os.Open(event)
	if e != nil {
		return errors.New("job event unavailable")
	}
	defer f.Close()
	var envelope struct{ Inputs map[string]any }
	d := json.NewDecoder(io.LimitReader(f, 1<<20))
	d.UseNumber()
	if d.Decode(&envelope) != nil {
		return errors.New("job event invalid")
	}
	inputs := map[string]string{}
	for key, value := range envelope.Inputs {
		switch value.(type) {
		case string, bool, json.Number:
			inputs[key] = fmtValue(value)
		default:
			return errors.New("job input type rejected")
		}
		if len(key) > 80 || len(inputs[key]) > 200 {
			return errors.New("job input size rejected")
		}
	}
	q := jobHookContext{Stage: stage, Repository: os.Getenv("GITHUB_REPOSITORY"), SHA: os.Getenv("GITHUB_SHA"), WorkflowSHA: os.Getenv("GITHUB_WORKFLOW_SHA"), Ref: os.Getenv("GITHUB_REF"), Event: os.Getenv("GITHUB_EVENT_NAME"), RunID: os.Getenv("GITHUB_RUN_ID"), Attempt: os.Getenv("GITHUB_RUN_ATTEMPT"), Job: os.Getenv("GITHUB_JOB"), Name: os.Getenv("RUNNER_NAME"), Inputs: inputs}
	if code := jobContextFailure(p, q); code != "" {
		return errors.New(code)
	}
	c, e := net.DialTimeout("unix", liveSocket, 5*time.Second)
	if e != nil {
		return errors.New("job hook owner unavailable")
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(50 * time.Second))
	if json.NewEncoder(c).Encode(q) != nil {
		return errors.New("job hook transfer failed")
	}
	var result struct{ Accepted bool }
	if json.NewDecoder(io.LimitReader(c, 4096)).Decode(&result) != nil || !result.Accepted {
		return errors.New("job hook rejected")
	}
	return nil
}
func fmtValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case json.Number:
		return x.String()
	}
	return ""
}
