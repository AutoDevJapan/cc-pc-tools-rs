package main

import (
	"context"
	"errors"
	"time"
)

func prestartBudget(p Plan, now time.Time) error {
	if p.ExecuteBefore > 0 && time.Unix(0, p.ExecuteBefore).Sub(now) < 60*time.Second {
		return errors.New("execution budget expired or insufficient before service startup")
	}
	return nil
}

func prestartContext(p Plan, max time.Duration) (context.Context, context.CancelFunc) {
	d := time.Now().Add(max)
	if p.ExecuteBefore > 0 && time.Unix(0, p.ExecuteBefore).Before(d) {
		d = time.Unix(0, p.ExecuteBefore)
	}
	return context.WithDeadline(context.Background(), d)
}

// The queue's absolute bound includes time spent in prepare and waiting for
// Cargo. The gate child reads the same immutable plan after acquiring its slot.
func executionDeadline(p Plan, start time.Time) time.Time {
	d := start.Add(time.Duration(p.Seconds+60) * time.Second)
	if p.ExecuteBefore != 0 {
		outer := time.Unix(0, p.ExecuteBefore)
		if outer.Before(d) {
			return outer
		}
	}
	return d
}
