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
	_ = syscall.Kill(-groupID, syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for processGroupExists(groupID) && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	_ = syscall.Kill(-groupID, syscall.SIGKILL)
	deadline = time.Now().Add(forceWait)
	for {
		if !processGroupExists(groupID) {
			return nil
		}
		if time.Now().After(deadline) {
			// Keep the caller's Cargo gate occupied until every process in the
			// group is gone; never free a shared target lock on a timed-out kill.
			_ = syscall.Kill(-groupID, syscall.SIGKILL)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func processGroupExists(groupID int) bool {
	err := syscall.Kill(-groupID, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
