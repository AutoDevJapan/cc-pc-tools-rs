package main

import (
	"context"
	"io"
	"os/exec"
	"syscall"
	"time"
)

func hidden(c *exec.Cmd) { c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} }
func group(c *exec.Cmd)  { hidden(c) }

// superviseGroup waits for the started child. Only the direct child is
// terminated on this platform; the drain deadlines apply to the Linux bridge.
func superviseGroup(ctx context.Context, c *exec.Cmd, _, _ time.Duration) (waitErr, cleanupErr error) {
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		_ = c.Process.Kill()
		waitErr = <-done
	}
	return waitErr, nil
}

// runCaptured runs the unstarted c with its standard output copied into out
// and its standard error discarded. exec.Cmd.WaitDelay bounds the wait for the
// output after the command exited, and its error is the primary outcome. Only
// the direct child exists on this platform, so there is no group to drain.
func runCaptured(_ context.Context, c *exec.Cmd, out io.Writer, delay time.Duration) error {
	c.Stdout, c.Stderr = out, io.Discard
	c.WaitDelay = delay
	return c.Run()
}
