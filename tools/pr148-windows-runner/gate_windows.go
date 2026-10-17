//go:build windows && amd64

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

type admission struct {
	Token         string
	Jobs, Threads string
	Held          string
	SenderPID     uint32
}

func (a admission) validate() error {
	j, e := strconv.Atoi(a.Jobs)
	t, f := strconv.Atoi(a.Threads)
	if e != nil || f != nil || j < 1 || j > 10 || t < 1 || t > 10 || len(a.Token) == 0 || len(a.Token) > 1024 || a.Held != "1" || a.SenderPID == 0 {
		return errors.New("invalid gate allocation")
	}
	return nil
}
func executeThroughGate(p Plan) error {
	// This branch is NOT used by credential-free fixtures. Parent must have an
	// allocated cargo resource before invoking real execution.
	token, e := boundedRead(os.Stdin, 1024)
	if e != nil {
		return e
	}
	defer clear(token)
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	c := exec.Command(`D:\CC\Tools\codex-jobs\cargo_gate.exe`, "test", "--target-dir", p.target())
	hidden(c)
	for _, k := range []string{"SystemRoot", "WINDIR", "SystemDrive", "PATH", "TEMP", "TMP", "USERPROFILE", "LOCALAPPDATA", "CC_JOB_ID"} {
		if v := os.Getenv(k); v != "" {
			c.Env = append(c.Env, k+"="+v)
		}
	}
	if !samePath(exe, filepath.Join(p.Root, "helper.exe")) {
		return errors.New("execute the prepared immutable helper")
	}
	c.Env = append(c.Env, "CODEX_JOBS_REAL_CARGO="+exe, "PR148_GATE_CHILD=1", "PR148_PLAN_ROOT="+p.Root, `CODEX_JOBS_CARGO_LOCK_DIR=D:/CC/Tools/codex-jobs`)
	c.Stdin = bytes.NewReader(token)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
func gateChild(args []string) error {
	root := os.Getenv("PR148_PLAN_ROOT")
	p, e := readPlan(root)
	if e != nil {
		return e
	}
	if len(args) != 3 || args[0] != "test" || args[1] != "--target-dir" || args[2] != p.target() || p.Fixture != "" {
		return errors.New("gate child invocation rejected")
	}
	if e = verifyGateAllocation(); e != nil {
		return e
	}
	if e = checkControl(root, true); e != nil {
		return e
	}
	return executeAdmitted(p)
}
func verifyGateAllocation() error {
	a := admission{Token: "not-a-credential", Held: os.Getenv("CODEX_JOBS_CARGO_LOCK_HELD"), Jobs: os.Getenv("CARGO_BUILD_JOBS"), Threads: os.Getenv("RUST_TEST_THREADS"), SenderPID: uint32(os.Getpid())}
	if a.validate() != nil {
		return errors.New("shared cargo gate allocation missing or out of bounds")
	}
	parent, e := syscall.OpenProcess(0x1000|0x100000, false, uint32(os.Getppid()))
	if e != nil {
		return e
	}
	defer syscall.CloseHandle(parent)
	image, e := processImage(parent)
	if e != nil || !samePath(image, `D:\CC\Tools\codex-jobs\cargo_gate.exe`) {
		return errors.New("controller is not a direct child of the shared cargo gate")
	}
	w, e := syscall.WaitForSingleObject(parent, 0)
	if e != nil || w != 258 {
		return errors.New("cargo gate parent is not alive")
	}
	return nil
}
func workerEnv(p Plan) []string {
	env := envFor(p)
	for _, k := range []string{"CODEX_JOBS_CARGO_LOCK_HELD", "CARGO_BUILD_JOBS", "RUST_TEST_THREADS", "NEXTEST_TEST_THREADS"} {
		env = append(env, k+"="+os.Getenv(k))
	}
	return env
}
