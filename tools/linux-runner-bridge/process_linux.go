package main

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

func hidden(c *exec.Cmd)    {}
func group(c *exec.Cmd)     { c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func killGroup(c *exec.Cmd) { _ = terminateAndDrainGroup(c, 3*time.Second, 15*time.Second) }

func terminateAndDrainGroup(c *exec.Cmd, grace, forceWait time.Duration) error {
	if c == nil || c.Process == nil {
		return errors.New("process group is unavailable")
	}
	groupID := c.Process.Pid
	if !processGroupExists(groupID) {
		return nil
	}
	_ = syscall.Kill(-groupID, syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for processGroupExists(groupID) && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if !processGroupExists(groupID) {
		return nil
	}
	_ = syscall.Kill(-groupID, syscall.SIGKILL)
	deadline = time.Now().Add(forceWait)
	for {
		if !processGroupExists(groupID) {
			return nil
		}
		if time.Now().After(deadline) {
			// Every supported container has an init reaper. The direct child is
			// waited concurrently by execute; init reaps orphaned descendants.
			// Keep the target occupied until the kernel confirms group absence.
			// A /proc snapshot can miss a child forked while it is being read.
			_ = syscall.Kill(-groupID, syscall.SIGKILL)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func processGroupExists(groupID int) bool {
	if groupID <= 0 {
		return true
	}
	err := syscall.Kill(-groupID, 0)
	return !errors.Is(err, syscall.ESRCH)
}
