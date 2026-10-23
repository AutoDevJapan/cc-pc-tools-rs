package main

import (
	"context"
	"time"
)

const normalTickBudget = 50 * time.Second

// The existing task allows two minutes. Preserve its normal fifty seconds,
// then allow fifteen for the pinned CI queue and five for bounded pipe drain.
// Parent-result delivery has a separate twenty-second allowance, still below
// the existing task's two-minute ceiling.
const periodicTickBudget = normalTickBudget + civ6QueueBudget + agentResultBudget + 5*time.Second

func runPeriodicPhases(ctx context.Context, normalBudget time.Duration, normal func(context.Context) Health, queue func(context.Context, *Health)) Health {
	work, cancel := context.WithTimeout(ctx, normalBudget)
	defer cancel()
	h := normal(work)
	cancel()
	// Same overall parent, separate normal-stage deadline. An exhausted intake,
	// report or retry stage cannot starve the CI queue on every periodic event.
	queue(ctx, &h)
	return h
}

func (r *Runner) periodicTick(ctx context.Context) Health {
	resultHealth := Health{}
	r.agentResultPhase(ctx, &resultHealth)
	h := runPeriodicPhases(ctx, normalTickBudget, r.tick, r.civ6CIQueue)
	h.Errors = append(h.Errors, resultHealth.Errors...)
	h.Finished = r.Now().UTC().Format(time.RFC3339Nano)
	return h
}
