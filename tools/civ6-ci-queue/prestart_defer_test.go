package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTransientPrestartVerificationKeepsOneReservation(t *testing.T) {
	h := newHarness(t)
	key := h.seed("Windows", 501, 5011, "test (windows-latest)", true)
	h.c.verifyErr = func(Plan) error { return errors.New("GitHub read unavailable") }
	if e := h.d.Supervise(context.Background(), key); e != nil {
		t.Fatal(e)
	}
	e := h.ledger().Entries[key]
	if e.State != StateReserved || e.Started.Attempts != 0 || e.Queued.Deferrals != 1 || h.sd.n != 0 || len(h.ledger().Budget.Initial) != 1 {
		t.Fatalf("prestart consumed an execution: %+v", e)
	}
	if h.tick().Launched != 0 {
		t.Fatal("backoff was ignored")
	}
	h.now = h.now.Add(6 * time.Minute)
	h.c.verifyErr = nil
	if h.tick().Launched != 1 {
		t.Fatal("same reserved job was not replenished")
	}
	if e := h.d.Supervise(context.Background(), key); e != nil {
		t.Fatal(e)
	}
	if h.ledger().Entries[key].State != StateSucceeded || len(h.ledger().Budget.Initial) != 1 {
		t.Fatal("successful resupply duplicated the budget")
	}
}

func TestConfirmedUnstartedRetirementCanResupplyWithoutOverwritingEvidence(t *testing.T) {
	h := newHarness(t)
	key := h.seed("Windows", 502, 5021, "test (windows-latest)", true)
	h.win.out = Outcome{Collected: true, Deferred: true}
	if e := h.d.Supervise(context.Background(), key); e != nil {
		t.Fatal(e)
	}
	first := h.ledger().Entries[key]
	oldEvidence := first.Cleanup.Evidence
	if first.State != StateReserved || first.Cleanup.State != "removed" || h.sd.n != 1 {
		t.Fatal(first)
	}
	h.now = h.now.Add(6 * time.Minute)
	h.win.out = Outcome{Started: true, HelperOK: true, Collected: true}
	h.tick()
	if e := h.d.Supervise(context.Background(), key); e != nil {
		t.Fatal(e)
	}
	next := h.ledger().Entries[key]
	if next.State != StateSucceeded || next.Cleanup.Evidence == oldEvidence || len(h.ledger().Budget.Initial) != 1 {
		t.Fatalf("resupply/evidence failure: %+v", next)
	}
	if len(next.Prestarts) != 1 || next.Prestarts[0].Cleanup == nil || next.Prestarts[0].Cleanup.Evidence != oldEvidence {
		t.Fatal("previous cleanup receipt was lost")
	}
}

func TestPrestartDeferralCountAndAgeAreFinite(t *testing.T) {
	for _, age := range []bool{false, true} {
		h := newHarness(t)
		key := h.seed("Windows", 503, 5031, "test (windows-latest)", true)
		if age {
			h.now = h.now.Add(24 * time.Hour)
		} else {
			for n := 0; n < maxPrestartDeferrals-1; n++ {
				if err := h.d.deferUnstarted(key, "temporary", nil); err != nil {
					t.Fatal(err)
				}
			}
		}
		if err := h.d.deferUnstarted(key, "temporary", nil); err != nil {
			t.Fatal(err)
		}
		e := h.ledger().Entries[key]
		if e.State != StateNeedsRecovery || h.ledger().Fences["Windows"].Key != key || e.Queued.RetryAt != "" || h.tick().Launched != 0 {
			t.Fatal("deferral limit did not fence entry")
		}
	}
}

func TestAgedPrestartDoesNotPrepareAnotherResource(t *testing.T) {
	h := newHarness(t)
	key := h.seed("Windows", 504, 5041, "test (windows-latest)", true)
	h.now = h.now.Add(24 * time.Hour)
	if err := h.d.Supervise(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if h.sd.n != 0 || h.ledger().Entries[key].State != StateNeedsRecovery {
		t.Fatal("aged reservation created a resource")
	}
}
