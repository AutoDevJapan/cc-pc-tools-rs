package main

import (
	"context"
	"time"
)

const normalTickBudget = 50 * time.Second

// The existing task starts once per minute and ignores overlapping starts.
// Keep a whole periodic pass below that interval, reserving time for parent
// results and the pinned queue instead of skipping the next minute's poll.
// Standalone CLI operations retain their existing fifty-second allowance.
const periodicTickBudget = 55 * time.Second
const periodicNormalBudget = 32 * time.Second
const periodicAgentResultBudget = 8 * time.Second

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
	resultCtx, cancelResults := context.WithTimeout(ctx, periodicAgentResultBudget)
	r.agentResultPhase(resultCtx, &resultHealth)
	cancelResults()
	h := runPeriodicPhases(ctx, periodicNormalBudget, r.tick, r.civ6CIQueue)
	h.Errors = append(h.Errors, resultHealth.Errors...)
	h.Finished = r.Now().UTC().Format(time.RFC3339Nano)
	return h
}
