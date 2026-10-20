package main

import (
	"context"
	"testing"
	"time"
)

func TestPeriodicPhasesPreserveQueueBudgetAfterNormalDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	r, _ := fixture(t)
	r.Config.Civ6Queue = civ6Config()
	r.Civ6Verify = func(string, string) error { return nil }
	called := false
	r.Run = func(c context.Context, path string, args ...string) (string, error) {
		if c.Err() != nil {
			t.Fatal("queue inherited exhausted normal deadline")
		}
		deadline, ok := c.Deadline()
		if !ok || time.Until(deadline) < civ6QueueMinRemaining {
			t.Fatal("queue has no usable budget")
		}
		called = true
		return `{"status":"ok"}`, nil
	}
	h := runPeriodicPhases(ctx, -time.Second, func(c context.Context) Health {
		if c.Err() != context.DeadlineExceeded {
			t.Fatal("normal deadline not exercised")
		}
		return Health{Errors: []string{"existing_normal_failure"}}
	}, r.civ6CIQueue)
	if !called || h.Civ6Queue == nil || len(h.Errors) != 1 || h.Errors[0] != "existing_normal_failure" {
		t.Fatal("queue skipped or normal result lost")
	}
}

func TestPeriodicPhasesRespectParentCancellationAndReleaseNormalContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var normal context.Context
	called := false
	h := runPeriodicPhases(ctx, time.Minute, func(c context.Context) Health { normal = c; cancel(); return Health{} }, func(c context.Context, h *Health) {
		called = true
		if c.Err() != context.Canceled {
			t.Fatal("parent cancellation ignored")
		}
	})
	_ = h
	if !called || normal.Err() != context.Canceled {
		t.Fatal("normal context not released")
	}
}
