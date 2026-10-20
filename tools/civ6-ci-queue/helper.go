package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var hex32 = regexp.MustCompile(`^[a-f0-9]{32}$`)

// Outcome is what an OS lifecycle reports back. Every field is an observation
// of the helper, never a verdict: official success is checked separately.
type Outcome struct {
	Started   bool   // an official runner may have been registered
	Exit      int    // helper exit code of the job-running step
	HelperOK  bool   // the helper itself reports a verified success
	Runner    string // unique runner name for the contract's RequireSuccess
	Collected bool   // services, registration, namespace and lock are confirmed gone
	Residue   bool   // collection ended "residue only" (profile/hive left behind)
	Failure   string
}

// ExecFunc runs one pinned executable with bounded output. onStart receives the
// process identity as soon as the process exists.
type ExecFunc func(ctx context.Context, pin Pinned, args []string, stdin []byte, onStart func(ProcID)) (StepResult, error)

// AdminFunc runs one generated, fixed-shape PowerShell script through the
// existing CC-AdminBroker window and returns its (untrusted) output.
type AdminFunc func(ctx context.Context, scriptPath string, timeoutSec int) ([]byte, error)

type StepResult struct {
	Exit   int
	Output []byte
}

// LifeCtx gives a lifecycle everything it may touch: the owned root, the
// entry directory (outside the root), and ledger callbacks. It carries no
// command, only fixed effects.
type LifeCtx struct {
	Key      string
	Plan     Plan
	Instance string
	Root     string
	EntryDir string
	Exec     ExecFunc
	Admin    AdminFunc
	Token    TokenSource
	Fence    func(reason string)
	Running  func()
	OnHelper func(ProcID)
	Log      func(name string, b []byte)
}

// Helper is the fixed adapter to one OS helper. The queue supplies the shared
// Plan and a NEW owned root; the argv of every step is fixed in this file and
// no command, executable or callback ever comes from the plan or GitHub data.
type Helper interface {
	OSName() string
	Seconds() int
	CleanupSeconds() int
	// Lifecycle runs the whole helper sequence once and never reruns anything.
	Lifecycle(ctx context.Context, lc *LifeCtx) Outcome
}

const maxStepOutput = 1 << 20

// runPinned starts a pinned executable; the pin is verified immediately before
// every start.
func runPinned(ctx context.Context, pin Pinned, args []string, stdin []byte, onStart func(ProcID)) (StepResult, error) {
	if e := pin.Verify(); e != nil {
		return StepResult{}, e
	}
	return runExe(ctx, pin.Path, args, stdin, onStart)
}

// runExe starts a program without a console window and with a scrubbed
// environment. Only fixed callers in this package reach it.
func runExe(ctx context.Context, path string, args []string, stdin []byte, onStart func(ProcID)) (StepResult, error) {
	c := exec.CommandContext(ctx, path, args...)
	hide(c)
	c.Env = scrubbedEnv()
	c.Dir = filepath.Dir(path)
	if stdin != nil {
		c.Stdin = bytes.NewReader(stdin)
	}
	out := &limitWriter{limit: maxStepOutput}
	c.Stdout, c.Stderr = out, out
	c.WaitDelay = 3 * time.Second
	if e := c.Start(); e != nil {
		return StepResult{}, errors.New("helper could not be started")
	}
	if id, e := procIdentity(uint32(c.Process.Pid)); e == nil && onStart != nil {
		onStart(id)
	}
	e := c.Wait()
	res := StepResult{Output: out.b.Bytes()}
	var xe *exec.ExitError
	switch {
	case e == nil:
	case errors.As(e, &xe):
		res.Exit = xe.ExitCode()
	default:
		return res, errors.New("helper wait failed")
	}
	if ctx.Err() != nil {
		return res, fmt.Errorf("helper step deadline: %w", ctx.Err())
	}
	if res.Exit == 0 && out.overflow {
		res.Exit = -1
	}
	return res, nil
}

const powershellExe = `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`

// realAdmin submits a script to CC-AdminBroker through its pinned entry point.
// The broker reports 0 even when the script fails, so callers verify the
// helper's own events and receipts instead of trusting this result.
func realAdmin(broker Pinned) AdminFunc {
	return func(ctx context.Context, scriptPath string, timeoutSec int) ([]byte, error) {
		if !absClean(scriptPath) || strings.ContainsAny(scriptPath, "'\"") || timeoutSec < 30 || timeoutSec > 3600 {
			return nil, errors.New("admin script path or timeout rejected")
		}
		if e := broker.Verify(); e != nil {
			return nil, e
		}
		res, e := runExe(ctx, powershellExe, []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", broker.Path, "-Script", scriptPath, "-TimeoutSec", fmt.Sprint(timeoutSec)}, nil, nil)
		if e != nil {
			return res.Output, e
		}
		if res.Exit != 0 {
			return res.Output, errors.New("admin broker reported a failure")
		}
		return res.Output, nil
	}
}

// writeStrict stores a document without overwriting.
func writeStrict(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	_, e = f.Write(append(b, '\n'))
	if c := f.Close(); e == nil {
		e = c
	}
	return e
}

func fileExists(p string) bool {
	st, e := os.Lstat(p)
	return e == nil && st.Mode().IsRegular()
}

// eventLines parses the helpers' one-JSON-object-per-line event stream.
func eventLines(out []byte) []map[string]any {
	var rows []map[string]any
	for _, line := range bytes.Split(out, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var row map[string]any
		if json.Unmarshal(line, &row) == nil {
			rows = append(rows, row)
		}
	}
	return rows
}

func lastEvent(out []byte, name string) map[string]any {
	var last map[string]any
	for _, r := range eventLines(out) {
		if r["event"] == name {
			last = r
		}
	}
	return last
}

func boolField(r map[string]any, k string) bool {
	v, _ := r[k].(bool)
	return v
}

func copyPinned(from Pinned, to string) error {
	if e := from.Verify(); e != nil {
		return e
	}
	in, e := os.Open(from.Path)
	if e != nil {
		return e
	}
	defer in.Close()
	out, e := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	if c := out.Close(); e == nil {
		e = c
	}
	if e != nil {
		return e
	}
	return Pinned{Path: to, SHA256: from.SHA256}.Verify()
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

func timeout(sec int) time.Duration { return time.Duration(sec) * time.Second }

// ---------------------------------------------------------------- Windows

type windowsHelper struct{ cfg HelperConfig }

func (h windowsHelper) OSName() string      { return "Windows" }
func (h windowsHelper) Seconds() int        { return h.cfg.Seconds }
func (h windowsHelper) CleanupSeconds() int { return h.cfg.CleanupSeconds }
func windowsRunner(instance string) string  { return "CC-CIW-" + instance }

// windowsPlan is the Windows helper's strict plan: the shared Plan flattened
// plus its operating parameters.
type windowsPlan struct {
	Plan
	Instance       string `json:"instance"`
	Root           string `json:"root"`
	RunnerVersion  string `json:"runner_version"`
	RunnerDigest   string `json:"runner_digest"`
	ControllerSID  string `json:"controller_sid"`
	Seconds        int    `json:"seconds"`
	CleanupSeconds int    `json:"cleanup_seconds,omitempty"`
	ExecuteBefore  int64  `json:"execute_before,omitempty"`
}

func adminScript(entryDir, verb, root string, extra ...string) (string, error) {
	for _, p := range append([]string{root}, extra...) {
		if !absClean(p) || strings.ContainsAny(p, "'\"`$;&|<>\r\n") {
			return "", errors.New("script argument rejected")
		}
	}
	if verb != "prepare" && verb != "collect" && verb != "collect-unstarted" {
		return "", errors.New("script verb rejected")
	}
	line := "& '" + filepath.Join(root, "bootstrap.exe") + "' " + verb + " '" + root + "'"
	for _, x := range extra {
		line += " '" + x + "'"
	}
	body := "$ErrorActionPreference = 'Stop'\r\n" + line + "\r\nexit $LASTEXITCODE\r\n"
	path := filepath.Join(entryDir, "admin-"+verb+".ps1")
	if e := os.WriteFile(path, []byte(body), 0600); e != nil {
		return "", e
	}
	return path, nil
}

func (h windowsHelper) cleanup() int {
	if h.cfg.CleanupSeconds != 0 {
		return h.cfg.CleanupSeconds
	}
	return 1800
}

func (h windowsHelper) collect(ctx context.Context, lc *LifeCtx, verb string) (done, residue bool) {
	script, e := adminScript(lc.EntryDir, verb, lc.Root)
	if e != nil {
		return false, false
	}
	cctx, cancel := context.WithTimeout(ctx, timeout(h.cleanup()+60))
	defer cancel()
	out, e := lc.Admin(cctx, script, h.cleanup()+30)
	lc.Log(verb+".log", out)
	end := lastEvent(out, "collect-end")
	if e != nil || end == nil {
		return false, false
	}
	// The fixed repository service/profile is a bounded continuing resource,
	// not per-job residue. Only the current helper's complete collection proof
	// permits another prepare. An old residue-only event never releases a job.
	return boolField(end, "complete"), false
}

func (h windowsHelper) Lifecycle(ctx context.Context, lc *LifeCtx) (o Outcome) {
	o.Collected = true // nothing exists outside the owned root yet
	p := windowsPlan{Plan: lc.Plan, Instance: lc.Instance, Root: lc.Root, RunnerVersion: h.cfg.RunnerVersion, RunnerDigest: h.cfg.RunnerDigest, ControllerSID: h.cfg.ControllerSID, Seconds: h.cfg.Seconds, CleanupSeconds: h.cfg.CleanupSeconds}
	// Reserve collection time before starting anything. Gate waits do not move
	// this absolute bound; the helper checks it after acquiring Cargo and the
	// service refuses to run past it. Allow the final drain before cancelling.
	executeEnd := time.Now().Add(timeout(h.cfg.Seconds + 1800))
	if outer, ok := ctx.Deadline(); ok {
		limit := outer.Add(-timeout(h.cleanup() + 300))
		if limit.Before(executeEnd) {
			executeEnd = limit
		}
	}
	if !executeEnd.After(time.Now()) {
		o.Failure = "no execution budget before collection"
		return o
	}
	p.ExecuteBefore = executeEnd.UnixNano()
	if filepath.Base(lc.Root) != "codex-owned-"+lc.Instance || !hex32.MatchString(lc.Instance) {
		o.Failure = "owned root does not match the instance"
		return o
	}
	if e := copyPinned(h.cfg.Exe, filepath.Join(lc.Root, "bootstrap.exe")); e != nil {
		o.Failure = "pinned helper could not be installed"
		return o
	}
	if e := writeStrict(filepath.Join(lc.Root, "plan.json"), p); e != nil {
		o.Failure = "plan could not be written"
		return o
	}
	if e := h.cfg.RunnerArchive.Verify(); e != nil {
		o.Failure = "official runner archive pin rejected"
		return o
	}
	script, e := adminScript(lc.EntryDir, "prepare", lc.Root, h.cfg.RunnerArchive.Path)
	if e != nil {
		o.Failure = "prepare script rejected"
		return o
	}
	// prepare creates the service and ACEs, so any failure from here on needs a
	// collection even if no job ever started.
	lc.Fence("service and ACLs may exist until collect succeeds")
	pctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	out, e := lc.Admin(pctx, script, 1100)
	cancel()
	lc.Log("prepare.log", out)
	if e != nil || !fileExists(filepath.Join(lc.Root, "prepared.json")) {
		o.Failure = "prepare did not complete"
		o.Collected, o.Residue = h.collect(ctx, lc, "collect-unstarted")
		return o
	}
	tok, e := lc.Token.Registration(ctx)
	if e != nil {
		o.Failure = "registration credential unavailable"
		o.Collected, o.Residue = h.collect(ctx, lc, "collect-unstarted")
		return o
	}
	lc.Running()
	helper := Pinned{Path: filepath.Join(lc.Root, "helper.exe"), SHA256: h.cfg.Exe.SHA256}
	o.Started, o.Runner = true, windowsRunner(lc.Instance)
	ectx, ecancel := context.WithDeadline(ctx, executeEnd.Add(120*time.Second))
	res, e := lc.Exec(ectx, helper, []string{"execute", lc.Root}, tok, lc.OnHelper)
	ecancel()
	zero(tok)
	lc.Log("execute.log", res.Output)
	end := lastEvent(res.Output, "execute-end")
	o.Exit = res.Exit
	if e != nil {
		o.Exit = -1
		o.Failure = "execute did not finish: " + short(e)
	} else if res.Exit != 0 {
		o.Failure = fmt.Sprintf("execute exited %d", res.Exit)
	}
	o.HelperOK = e == nil && res.Exit == 0 && end != nil && boolField(end, "ok")
	if end == nil {
		// The helper did not finish its own handling (it makes exactly one stop
		// attempt itself when it does), so one stop is tried here. A reported
		// fence is never stopped again: collect verifies it and fences on failure.
		sctx, scancel := context.WithTimeout(ctx, 5*time.Minute)
		sres, _ := lc.Exec(sctx, helper, []string{"stop", lc.Root}, nil, nil)
		scancel()
		lc.Log("stop.log", sres.Output)
	}
	collectVerb := "collect"
	if started, known := end["service_started"].(bool); known && !started {
		// A rejected/expired gate child did not start SCM or configure a runner.
		// The strict unstarted collector verifies that proof and the queued job.
		// Never try the normal collector first and then bypass its refusal.
		o.Started, o.Runner, o.HelperOK = false, "", false
		collectVerb = "collect-unstarted"
	}
	o.Collected, o.Residue = h.collect(ctx, lc, collectVerb)
	return o
}

// ------------------------------------------------------------------ Linux

type linuxHelper struct{ cfg HelperConfig }

func (h linuxHelper) OSName() string      { return "Linux" }
func (h linuxHelper) Seconds() int        { return h.cfg.Seconds }
func (h linuxHelper) CleanupSeconds() int { return h.cfg.CleanupSeconds }

// linuxReceipt is the part of the helper's result.private.json that the queue
// reads. The helper writes Go field names without tags.
type linuxReceipt struct {
	Namespace *struct {
		Name    string
		Removed bool
	}
	RegistrationMayExist     bool
	RegistrationGone         bool
	LockReleased             bool
	AllRequiredChecksSuccess bool
}

func readLinuxReceipt(root string) (linuxReceipt, bool) {
	var r linuxReceipt
	f, e := os.Open(filepath.Join(root, "result.private.json"))
	if e != nil {
		return r, false
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 1<<20))
	return r, e == nil && json.Unmarshal(b, &r) == nil
}

func (h linuxHelper) Lifecycle(ctx context.Context, lc *LifeCtx) (o Outcome) {
	o.Collected = true
	if filepath.Base(lc.Root) != "codex-owned-"+lc.Instance || !hex32.MatchString(lc.Instance) {
		o.Failure = "owned root does not match the instance"
		return o
	}
	if e := h.cfg.Guest.Verify(); e != nil {
		o.Failure = "Linux guest binary pin rejected"
		return o
	}
	if e := h.cfg.PwshTar.Verify(); e != nil {
		o.Failure = "official PowerShell asset pin rejected"
		return o
	}
	// The plan lives outside the owned root: the helper requires that root to be
	// new and empty, and it creates its own receipts there.
	plan := filepath.Join(lc.EntryDir, "plan.json")
	if e := writeStrict(plan, lc.Plan); e != nil {
		o.Failure = "plan could not be written"
		return o
	}
	lc.Running()
	lc.Fence("Linux namespace, registration or worker lock may exist until the receipt confirms")
	rctx, cancel := context.WithTimeout(ctx, timeout(h.cfg.Seconds+300))
	res, e := lc.Exec(rctx, h.cfg.Exe, []string{"run", plan, lc.Root, h.cfg.Guest.Path, h.cfg.PwshTar.Path}, nil, lc.OnHelper)
	cancel()
	lc.Log("run.log", res.Output)
	o.Exit = res.Exit
	if e != nil {
		o.Exit = -1
		o.Failure = "run did not finish: " + short(e)
	} else if res.Exit != 0 {
		o.Failure = fmt.Sprintf("run exited %d", res.Exit)
	}
	rec, ok := readLinuxReceipt(lc.Root)
	if !ok {
		// Without a receipt nothing proves cleanup, unless the helper refused
		// before it wrote its first intent file.
		o.Collected = !fileExists(filepath.Join(lc.Root, "intent.private.json"))
		if !o.Collected && o.Failure == "" {
			o.Failure = "receipt missing"
		}
		return o
	}
	o.Started = rec.RegistrationMayExist || rec.Namespace != nil
	if rec.Namespace != nil {
		o.Runner = rec.Namespace.Name
	}
	nsGone := rec.Namespace == nil || rec.Namespace.Removed
	regGone := !rec.RegistrationMayExist || rec.RegistrationGone
	o.Collected = nsGone && regGone && rec.LockReleased
	o.HelperOK = e == nil && res.Exit == 0 && rec.AllRequiredChecksSuccess
	return o
}
