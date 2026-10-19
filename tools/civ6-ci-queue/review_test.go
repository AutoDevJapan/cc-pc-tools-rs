package main

import (
	"context"
	"os"
	"sync"
	"testing"
)

// The plan may itself demand quarantine (attributes, size...): that is
// followed exactly and the root is never deleted.
func TestPlanQuarantineInstructionIsHonouredAndNeverDeleted(t *testing.T) {
	h := newHarness(t)
	h.sd.planAction, h.sd.planReason = "quarantine", "attributes"
	key := h.seed("Linux", 330, 3301, "test (ubuntu-latest)", true)
	if err := h.d.Supervise(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	e := h.ledger().Entries[key]
	if e.State != StateSucceeded || e.Cleanup.State != "quarantined" || len(h.sd.quarantined) != 1 || len(h.sd.finished) != 0 {
		t.Fatalf("%+v", e)
	}
	if _, err := os.Stat(e.Cleanup.Root); err != nil {
		t.Fatal("a quarantine instruction must leave the root in place for the tool")
	}

	h2 := newHarness(t)
	h2.sd.planAction, h2.sd.planReason = "format-disk", ""
	k2 := h2.seed("Linux", 331, 3311, "test (ubuntu-latest)", true)
	_ = h2.d.Supervise(context.Background(), k2)
	e2 := h2.ledger().Entries[k2]
	if e2.State != StateNeedsRecovery || len(h2.sd.finished) != 0 || len(h2.sd.quarantined) != 0 {
		t.Fatalf("unknown plan action must not delete: %+v", e2)
	}
}

// Two supervisors for one launching entry run the lifecycle once; the loser
// of the lock never rewinds the winner's entry.
func TestDuplicateSupervisorsRunTheLifecycleOnce(t *testing.T) {
	h := newHarness(t)
	h.lin.entered, h.lin.release = make(chan struct{}), make(chan struct{})
	key := h.seed("Linux", 340, 3401, "test (ubuntu-latest)", true)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = h.d.Supervise(context.Background(), key)
	}()
	<-h.lin.entered                              // the first supervisor now holds the OS lock inside its lifecycle
	_ = h.d.Supervise(context.Background(), key) // the duplicate just exits
	e := h.ledger().Entries[key]
	if e.State != StateRunning || !e.Started.Claimed || e.Started.Attempts != 1 {
		t.Fatalf("the winner's entry was rewound: %+v", e)
	}
	h.lin.release <- struct{}{}
	wg.Wait()
	// A third, late supervisor that read the ledger earlier finds it finished.
	if err := h.d.Supervise(context.Background(), key); err == nil {
		t.Fatal("finished entry was claimed again")
	}
	if h.lin.names() != "Linux:lifecycle" || h.ledger().Entries[key].State != StateSucceeded || h.sd.n != 1 {
		t.Fatalf("%s %+v", h.lin.names(), h.ledger().Entries[key])
	}
}

func TestLostLockRaceBeforeTheClaimDoesNothing(t *testing.T) {
	// A claimed entry whose lock is held elsewhere is never rewound by a loser.
	hh := newHarness(t)
	k := hh.seed("Linux", 342, 3421, "test (ubuntu-latest)", true)
	_ = hh.d.mutate(k, func(l *Ledger, e *Entry) { e.Started.Claimed = true })
	unlock, err := hh.d.LockOS("Linux")
	if err != nil {
		t.Fatal(err)
	}
	_ = hh.d.Supervise(context.Background(), k)
	unlock()
	if e := hh.ledger().Entries[k]; e.State != StateLaunching || !e.Started.Claimed || e.Started.Attempts != 1 {
		t.Fatalf("claimed entry was rewound: %+v", e)
	}

	h := newHarness(t)
	key := h.seed("Windows", 341, 3411, "test (windows-latest)", true)
	_ = h.d.mutate(key, func(l *Ledger, e *Entry) { e.Started.Claimed = true })
	if err := h.d.Supervise(context.Background(), key); err == nil || h.win.names() != "" || h.sd.n != 0 {
		t.Fatalf("an already claimed entry must not run again: %v", err)
	}
}
