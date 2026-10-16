//go:build windows && amd64

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
)

type admission struct {
	Token         string
	Jobs, Threads string
}

func (a admission) validate() error {
	j, e := strconv.Atoi(a.Jobs)
	t, f := strconv.Atoi(a.Threads)
	if e != nil || f != nil || j < 1 || j > 10 || t < 1 || t > 10 || len(a.Token) == 0 || len(a.Token) > 1024 {
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
	c := exec.Command(`D:\CC\Tools\codex-jobs\cargo_gate.exe`, "test", "--target-dir", filepath.Join(p.runtime(), "target"))
	hidden(c)
	for _, k := range []string{"SystemRoot", "WINDIR", "SystemDrive", "PATH", "TEMP", "TMP", "USERPROFILE", "LOCALAPPDATA", "CC_JOB_ID"} {
		if v := os.Getenv(k); v != "" {
			c.Env = append(c.Env, k+"="+v)
		}
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
	if len(args) != 3 || args[0] != "test" || args[1] != "--target-dir" || args[2] != filepath.Join(p.runtime(), "target") || p.Fixture != "" {
		return errors.New("gate child invocation rejected")
	}
	a := admission{Token: "not-a-credential", Jobs: os.Getenv("CARGO_BUILD_JOBS"), Threads: os.Getenv("RUST_TEST_THREADS")}
	if os.Getenv("CODEX_JOBS_CARGO_LOCK_HELD") != "1" || a.validate() != nil {
		return errors.New("shared cargo gate admission unavailable")
	}
	if e = checkPaths(root); e != nil {
		return e
	}
	return execute(p)
}
func workerEnv(p Plan) []string {
	env := envFor(p)
	for _, k := range []string{"CODEX_JOBS_CARGO_LOCK_HELD", "CARGO_BUILD_JOBS", "RUST_TEST_THREADS", "NEXTEST_TEST_THREADS"} {
		env = append(env, k+"="+os.Getenv(k))
	}
	return env
}
