package main

import (
	"context"
	"errors"
	"os/exec"
	"time"
)

// Safe metadata survives the private subprocess boundary; stdout, stderr,
// arguments, executable paths and raw OS error messages do not.
type commandFailure struct {
	Cause     string
	ElapsedMS int64
	ExitCode  *int
}

func (e *commandFailure) Error() string { return "command unavailable or timed out" }

func commandFailed(parent, child context.Context, err error, started time.Time) error {
	e := &commandFailure{ElapsedMS: time.Since(started).Milliseconds()}
	switch {
	case errors.Is(parent.Err(), context.DeadlineExceeded):
		e.Cause = "caller_deadline"
	case errors.Is(parent.Err(), context.Canceled):
		e.Cause = "caller_cancelled"
	case errors.Is(child.Err(), context.DeadlineExceeded):
		e.Cause = "command_deadline"
	case errors.Is(child.Err(), context.Canceled):
		e.Cause = "command_cancelled"
	case errors.Is(err, exec.ErrWaitDelay):
		e.Cause = "command_pipe_deadline"
	default:
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			e.Cause = "command_exit"
			code := exit.ExitCode()
			e.ExitCode = &code
		} else {
			e.Cause = "command_start_or_io"
		}
	}
	return e
}
