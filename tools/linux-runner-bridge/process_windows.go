package main

import (
	"os/exec"
	"syscall"
)

func hidden(c *exec.Cmd)    { c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} }
func group(c *exec.Cmd)     { hidden(c) }
func killGroup(c *exec.Cmd) { _ = c.Process.Kill() }
