package main

import (
	"os/exec"
	"syscall"
)

func hidden(c *exec.Cmd)    {}
func group(c *exec.Cmd)     { c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func killGroup(c *exec.Cmd) { _ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
