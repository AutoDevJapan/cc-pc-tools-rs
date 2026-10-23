package main

import (
	"context"
	"errors"
)

// Called by the real gate child after the start ACK, so queue time has already
// elapsed. The same deadline also reaches the post-boot, pre-token check.
func gatedWorkContext(q gatedHello) (context.Context, context.CancelFunc, error) {
	if q.Deadline.IsZero() {
		return nil, nil, errors.New("gate parent deadline absent")
	}
	ctx, cancel := context.WithDeadline(context.Background(), q.Deadline)
	fail := func(e error) (context.Context, context.CancelFunc, error) {
		cancel()
		return nil, nil, e
	}
	if ctx.Err() != nil {
		return fail(errors.New("gate parent deadline expired"))
	}
	if q.Mode == "linux-job" || q.Mode == "windows-job" {
		request, e := readJobRequest(q.Request)
		if e != nil {
			return fail(e)
		}
		if e = requirePressSessionBudget(ctx, request.Source, request.Role); e != nil {
			return fail(e)
		}
	}
	return ctx, cancel, nil
}
