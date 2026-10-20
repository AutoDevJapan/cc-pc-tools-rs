package main

import (
	"testing"
	"time"
)

func TestGateWaitCannotExtendTheServiceDeadline(t *testing.T) {
	start := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	p := Plan{Seconds: 600, ExecuteBefore: start.Add(900 * time.Second).UnixNano()}
	if got := executionDeadline(p, start); !got.Equal(start.Add(660 * time.Second)) {
		t.Fatal(got)
	}
	// A long gate wait consumes the original absolute budget, rather than
	// starting a fresh full service duration after the slot is acquired.
	late := start.Add(800 * time.Second)
	if got := executionDeadline(p, late); !got.Equal(time.Unix(0, p.ExecuteBefore)) {
		t.Fatal(got)
	}
	if executionDeadline(p, start.Add(901*time.Second)).After(start.Add(901 * time.Second)) {
		t.Fatal("expired gate wait gained runtime")
	}
}

func TestPrestartRequiresMinimumBudgetAndBoundsReads(t *testing.T) {
	now := time.Now()
	for _, seconds := range []int{-1, 0, 59, 60, 61} {
		p := Plan{ExecuteBefore: now.Add(time.Duration(seconds) * time.Second).UnixNano()}
		if (prestartBudget(p, now) == nil) != (seconds >= 60) {
			t.Fatalf("startup margin: %d seconds", seconds)
		}
	}
	p := Plan{ExecuteBefore: now.Add(2 * time.Minute).UnixNano()}
	ctx, cancel := prestartContext(p, 3*time.Minute)
	defer cancel()
	d, ok := ctx.Deadline()
	if !ok || !d.Equal(time.Unix(0, p.ExecuteBefore)) {
		t.Fatal("audit may exceed execution deadline")
	}
}

func TestOfficialPlanRequiresAbsoluteDeadline(t *testing.T) {
	p := fixturePlan()
	p.ExecuteBefore = 0
	if p.validate() == nil {
		t.Fatal("official plan accepted an unbounded wait")
	}
}
