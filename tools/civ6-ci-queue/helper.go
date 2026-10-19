package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"time"
)

var hex32 = regexp.MustCompile(`^[a-f0-9]{32}$`)

// Helper is the fixed adapter to one OS helper. The queue supplies only the
// shared Plan and an owned root; the argv of every step is fixed below and no
// command, executable or callback ever comes from the plan or the GitHub data.
type Helper interface {
	OSName() string
	Pin() Pinned
	Seconds() int
	CleanupSeconds() int
	// RunnerName is the unique instance runner name the contract must see.
	RunnerName(instance string) string
	// WritePlan stores the helper's strict plan document in the owned root.
	WritePlan(root, instance string, p Plan) error
}

// The step verbs are the Windows helper's, which the Linux helper is asked to
// mirror: "<verb> <owned-root>" with the plan in <root>/plan.json and, for
// execute only, the registration credential on stdin.
const (
	StepPrepare          = "prepare"
	StepExecute          = "execute"
	StepStop             = "stop"
	StepCollect          = "collect"
	StepCollectUnstarted = "collect-unstarted"
	StepStatus           = "status"
)

var stepVerbs = map[string]bool{StepPrepare: true, StepExecute: true, StepStop: true, StepCollect: true, StepCollectUnstarted: true, StepStatus: true}

type StepResult struct {
	Exit   int
	Output []byte
}

// StepFunc runs one fixed helper step. onStart receives the helper's process
// identity as soon as it exists.
type StepFunc func(ctx context.Context, h Helper, step, root string, stdin []byte, onStart func(ProcID)) (StepResult, error)

const maxStepOutput = 1 << 20

// runStep starts the pinned helper without a console window. The pin is
// verified immediately before every start.
func runStep(ctx context.Context, h Helper, step, root string, stdin []byte, onStart func(ProcID)) (StepResult, error) {
	if !stepVerbs[step] || !absClean(root) {
		return StepResult{}, errors.New("helper step or owned root rejected")
	}
	if e := h.Pin().Verify(); e != nil {
		return StepResult{}, e
	}
	c := exec.CommandContext(ctx, h.Pin().Path, step, root)
	hide(c)
	c.Env = scrubbedEnv()
	c.Dir = filepath.Dir(root)
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

// writeStrict stores a plan document without overwriting.
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

type fixedHelper struct {
	os  string
	cfg HelperConfig
}

func (h fixedHelper) OSName() string      { return h.os }
func (h fixedHelper) Pin() Pinned         { return h.cfg.Exe }
func (h fixedHelper) Seconds() int        { return h.cfg.Seconds }
func (h fixedHelper) CleanupSeconds() int { return h.cfg.CleanupSeconds }
func (h fixedHelper) cleanupOrDefault() int {
	if h.cfg.CleanupSeconds == 0 {
		return 1800
	}
	return h.cfg.CleanupSeconds
}

func (h fixedHelper) RunnerName(instance string) string {
	if h.os == "Windows" {
		return "CC-CIW-" + instance
	}
	return "CC-CIL-" + instance
}

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
}

func (h fixedHelper) WritePlan(root, instance string, p Plan) error {
	if !hex32.MatchString(instance) || filepath.Base(root) != "codex-owned-"+instance {
		return errors.New("owned root does not match the instance")
	}
	path := filepath.Join(root, "plan.json")
	if h.os == "Windows" {
		return writeStrict(path, windowsPlan{Plan: p, Instance: instance, Root: root, RunnerVersion: h.cfg.RunnerVersion, RunnerDigest: h.cfg.RunnerDigest, ControllerSID: h.cfg.ControllerSID, Seconds: h.cfg.Seconds, CleanupSeconds: h.cfg.CleanupSeconds})
	}
	// The Linux helper's plan decoder is strict and holds exactly the shared Plan.
	return writeStrict(path, p)
}

// probe is the read-only reconciliation question: is every service, runner
// registration, container and process of this owned root gone?
type probe struct {
	Clean bool `json:"clean"`
}

func probeClean(ctx context.Context, run StepFunc, h Helper, root string) (bool, error) {
	r, e := run(ctx, h, StepStatus, root, nil, nil)
	if e != nil || r.Exit != 0 {
		return false, errors.New("helper status unavailable")
	}
	var p probe
	if json.Unmarshal(bytes.TrimSpace(r.Output), &p) != nil {
		return false, errors.New("helper status unreadable")
	}
	return p.Clean, nil
}
