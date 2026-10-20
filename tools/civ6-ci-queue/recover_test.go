package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func recoveryHarness(t *testing.T) (*harness, string, obj) {
	h := newHarness(t)
	key := h.seed("Windows", 801, 8011, "test (windows-latest)", true)
	h.win.out = Outcome{Started: true, Collected: false, Exit: 1}
	if err := h.d.Supervise(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	j := obj{"id": int64(8011), "run_id": int64(801), "run_attempt": 1, "head_sha": headSHA, "status": "queued", "runner_id": 0, "runner_name": "", "steps": []obj{}}
	h.gh.data["repos/"+Repository+"/actions/jobs/8011"] = j
	h.d.Retired = func(cl CleanupRec, key string) (CleanupRec, error) { cl.State = "quarantined"; return cl, nil }
	h.d.VerifyRetired = func(context.Context, *Entry, string) error { return nil }
	return h, key, j
}

func TestRecoveredUnassignedJobReusesExactlyOneReservation(t *testing.T) {
	h, key, _ := recoveryHarness(t)
	// Baseline: even after an external collection, periodic tick cannot infer
	// recovery or turn a process failure into an official successful job.
	if h.tick().Launched != 0 || h.ledger().Entries[key].State != StateNeedsRecovery {
		t.Fatal("fence did not hold")
	}
	calls := h.win.names()
	if err := h.d.ResumeUnassigned(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	l := h.ledger()
	e := l.Entries[key]
	if e.State != StateReserved || e.Official.Success || e.Started.Attempts != 0 || e.Queued.Deferrals != 1 || len(e.Prestarts) != 1 ||
		len(l.Budget.Initial) != 1 || len(l.Fences) != 0 || h.win.names() != calls || h.sd.n != 1 {
		t.Fatal("recovery created an execution or false verdict", e)
	}
	if _, err := os.Stat(h.d.resultPath(key)); !os.IsNotExist(err) {
		t.Fatal("old result would undo recovery")
	}
	if err := h.d.ResumeUnassigned(context.Background(), key); err == nil {
		t.Fatal("same recovery admitted twice")
	}
	if h.tick().Launched != 1 || len(h.ledger().Budget.Initial) != 1 {
		t.Fatal("normal tick did not resupply same job")
	}
}

func TestRecoveryRetainsFenceWhenProofFails(t *testing.T) {
	for _, name := range []string{"assigned", "started", "completed", "other-head", "other-run", "other-attempt", "named-runner", "retirement", "collection", "live", "aged", "count", "context", "changed-during-proof"} {
		t.Run(name, func(t *testing.T) {
			h, key, j := recoveryHarness(t)
			ctx := context.Background()
			switch name {
			case "assigned":
				j["runner_id"] = 55
			case "started":
				j["steps"] = []obj{{"name": "test", "status": "in_progress"}}
			case "completed":
				j["status"] = "completed"
			case "other-head":
				j["head_sha"] = mergeSHA
			case "other-run":
				j["run_id"] = 999
			case "other-attempt":
				j["run_attempt"] = 2
			case "named-runner":
				j["runner_name"] = "foreign"
			case "retirement":
				h.d.Retired = func(cl CleanupRec, key string) (CleanupRec, error) { return cl, errors.New("not retired") }
			case "collection":
				h.d.VerifyRetired = func(context.Context, *Entry, string) error { return errors.New("marker differs") }
			case "live":
				h.d.Alive = func(ProcID) bool { return true }
			case "aged":
				h.now = h.now.Add(24 * time.Hour)
			case "count":
				_ = h.d.mutate(key, func(_ *Ledger, e *Entry) { e.Queued.Deferrals = maxPrestartDeferrals - 1 })
			case "context":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "changed-during-proof":
				h.d.VerifyRetired = func(context.Context, *Entry, string) error { j["runner_id"] = 44; return nil }
			}
			if err := h.d.ResumeUnassigned(ctx, key); err == nil {
				t.Fatal("invalid recovery accepted")
			}
			l := h.ledger()
			if l.Entries[key].State != StateNeedsRecovery || l.Fences["Windows"].Key != key || len(l.Budget.Initial) != 1 {
				t.Fatal("failed proof changed admission")
			}
		})
	}
}

func TestRecoveryResumesAfterResultArchiveAndBeforeLedgerCommit(t *testing.T) {
	h, key, _ := recoveryHarness(t)
	dir := h.d.home("entries", safeKey(key), "recovery-0")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "original-result.json")
	if err := os.Rename(h.d.resultPath(key), archive); err != nil {
		t.Fatal(err)
	}
	if err := h.d.ResumeUnassigned(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if h.ledger().Entries[key].State != StateReserved || len(h.ledger().Entries[key].Prestarts) != 1 {
		t.Fatal("interrupted recovery not resumed")
	}
}

func TestRetirementRequiresOriginalClosedOwnedRecord(t *testing.T) {
	root := filepath.Join(t.TempDir(), "codex-owned-"+"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	cl := CleanupRec{Root: root, Instance: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SafeDeleteID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	r := retirementRecord{retirementIdentity: retirementIdentity{Version: 2, ID: cl.Instance, Root: filepath.Dir(root), Path: root, Volume: 1, FileID: 2, Project: "civ6-ci-queue"}, State: "source_absent"}
	sign := func() { b, _ := json.Marshal(r.retirementIdentity); r.Integrity = digestHexBytes(b) }
	sign()
	if _, err := checkRetirementRecord(r, cl); err != nil {
		t.Fatal(err)
	}
	r.State = "owned"
	if _, err := checkRetirementRecord(r, cl); err == nil {
		t.Fatal("open record accepted")
	}
	r.State = "source_absent"
	r.ID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	sign()
	if _, err := checkRetirementRecord(r, cl); err == nil {
		t.Fatal("different original identity accepted")
	}
	r.ID = cl.Instance
	sign()
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := checkRetirementRecord(r, cl); err == nil {
		t.Fatal("existing old root accepted")
	}
}
