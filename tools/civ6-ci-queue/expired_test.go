package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func expiredHarness(t *testing.T, resource bool) (*harness, string, string, obj, obj) {
	h, oldKey, oldJob := recoveryHarness(t)
	if !resource {
		if err := h.d.mutate(oldKey, func(l *Ledger, e *Entry) {
			e.State, e.Started, e.Cleanup = StateReserved, StartRec{}, CleanupRec{State: "none"}
			delete(l.Fences, "Windows")
		}); err != nil {
			t.Fatal(err)
		}
	}
	queued := h.now
	h.now = h.now.Add(25 * time.Hour)
	oldJob["name"] = "test (windows-latest)"
	oldJob["status"], oldJob["conclusion"] = "completed", "cancelled"
	oldJob["started_at"], oldJob["completed_at"] = stamp(queued), stamp(queued.Add(24*time.Hour))
	newKey := "801/2/8022"
	newJob := obj{"id": int64(8022), "run_id": int64(801), "run_attempt": 2, "head_sha": headSHA,
		"name": "test (windows-latest)", "status": "queued", "runner_id": 0, "runner_name": "", "steps": []obj{}}
	h.gh.data[fmt.Sprintf("repos/%s/actions/jobs/8022", Repository)] = newJob
	return h, oldKey, newKey, oldJob, newJob
}

func TestExpiredUnassignedReplacementPreservesOriginalSlotAndEvidence(t *testing.T) {
	for _, resource := range []bool{false, true} {
		t.Run(fmt.Sprint(resource), func(t *testing.T) {
			h, oldKey, newKey, _, _ := expiredHarness(t, resource)
			before, _ := json.Marshal(h.ledger().Entries[oldKey])
			calls, creates := h.win.names(), h.sd.n
			if err := h.d.ReplaceExpired(context.Background(), oldKey, newKey); err != nil {
				t.Fatal(err)
			}
			l := h.ledger()
			if len(l.Budget.Initial) != 1 || l.Budget.Initial[0] != oldKey || len(l.Budget.Normal) != 0 || len(l.Budget.Replacements) != 1 {
				t.Fatalf("original budget changed: %+v", l.Budget)
			}
			e := l.Entries[newKey]
			if e == nil || e.Replaces != oldKey || e.State != StateReserved || e.Official.Success || e.Started.Attempts != 0 || len(l.Fences) != 0 || h.sd.n != creates || h.win.names() != calls {
				t.Fatal("replacement executed work, lost its identity or claimed success")
			}
			proofPath := filepath.Join(h.d.home("entries", safeKey(oldKey)), "replacement-2.json")
			proof, err := os.ReadFile(proofPath)
			if err != nil {
				t.Fatal(err)
			}
			var p expiredProof
			if json.Unmarshal(proof, &p) != nil {
				t.Fatal("invalid proof")
			}
			original, _ := json.Marshal(p.Original)
			if string(original) != string(before) || l.Budget.Replacements[0].ProofSHA256 != digestHexBytes(proof) || p.Original.Queued.At != stamp(h.now.Add(-25*time.Hour)) {
				t.Fatal("old entry, deadline or proof hash not preserved")
			}
			if err := h.d.ReplaceExpired(context.Background(), oldKey, newKey); err == nil {
				t.Fatal("replacement accepted twice")
			}
			if h.tick().Launched != 1 || h.spawned[0] != newKey || len(h.ledger().Budget.Initial) != 1 {
				t.Fatal("ordinary tick did not launch only the replacement in its old slot")
			}
		})
	}
}

func TestExpiredReplacementRejectsUnsafeOrUnboundedRecovery(t *testing.T) {
	for _, name := range []string{"not-expired", "assigned", "steps", "missing-steps", "missing-runner", "missing-runner-name", "failure", "short-wait", "other-head", "other-name", "wrong-attempt", "new-assigned", "new-started", "new-completed", "contract", "retirement", "collection", "live", "unknown-resource", "foreign-fence", "not-in-budget", "context", "changed-during-proof"} {
		t.Run(name, func(t *testing.T) {
			h, oldKey, newKey, old, next := expiredHarness(t, true)
			ctx := context.Background()
			switch name {
			case "not-expired":
				h.now = h.now.Add(-24 * time.Hour)
			case "assigned":
				old["runner_id"] = 5
			case "steps":
				old["steps"] = []obj{{"name": "test"}}
			case "missing-steps":
				delete(old, "steps")
			case "missing-runner":
				delete(old, "runner_id")
			case "missing-runner-name":
				delete(old, "runner_name")
			case "failure":
				old["conclusion"] = "failure"
			case "short-wait":
				old["completed_at"] = stamp(h.now.Add(-24 * time.Hour))
			case "other-head":
				next["head_sha"] = mergeSHA
			case "other-name":
				next["name"] = "native-driver"
			case "wrong-attempt":
				newKey = "801/3/8022"
			case "new-assigned":
				next["runner_id"] = 4
			case "new-started":
				next["steps"] = []obj{{"name": "test"}}
			case "new-completed":
				next["status"] = "completed"
			case "contract":
				h.c.verifyErr = func(Plan) error { return errors.New("source changed") }
			case "retirement":
				h.d.Retired = func(CleanupRec, string) (CleanupRec, error) { return CleanupRec{}, errors.New("not retired") }
			case "collection":
				h.d.VerifyRetired = func(context.Context, *Entry, string) (CollectionObservation, error) {
					return CollectionObservation{}, errors.New("not collected")
				}
			case "live":
				h.d.Alive = func(ProcID) bool { return true }
			case "unknown-resource":
				_ = h.d.mutate(oldKey, func(_ *Ledger, e *Entry) { e.Cleanup.State = "create_intent"; e.Cleanup.SafeDeleteID = "" })
			case "foreign-fence":
				_ = h.d.Store.Update(func(l *Ledger) error { l.Fences["Windows"] = Fence{Key: "999/1/1"}; return nil })
			case "not-in-budget":
				_ = h.d.Store.Update(func(l *Ledger) error { l.Budget.Initial = nil; return nil })
			case "context":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "changed-during-proof":
				h.d.VerifyRetired = func(context.Context, *Entry, string) (CollectionObservation, error) {
					next["runner_id"] = 6
					return CollectionObservation{}, nil
				}
			}
			before, _ := json.Marshal(h.ledger())
			if err := h.d.ReplaceExpired(ctx, oldKey, newKey); err == nil {
				t.Fatal("unsafe replacement accepted")
			}
			after, _ := json.Marshal(h.ledger())
			if string(before) != string(after) {
				t.Fatal("failed proof changed the ledger")
			}
		})
	}
}

func TestExpiredReplacementCannotFreeASecondBudgetSlot(t *testing.T) {
	h, oldKey, newKey, _, _ := expiredHarness(t, false)
	if err := h.d.ReplaceExpired(context.Background(), oldKey, newKey); err != nil {
		t.Fatal(err)
	}
	// Another new execution must not reuse this already bound original slot.
	if err := h.d.ReplaceExpired(context.Background(), oldKey, "801/2/8033"); err == nil {
		t.Fatal("original slot replaced twice")
	}
	if err := h.d.ReplaceExpired(context.Background(), newKey, "801/3/8033"); err == nil {
		t.Fatal("replacement chain accepted")
	}
	if len(h.ledger().Budget.Replacements) != 1 {
		t.Fatal("replacement limit changed")
	}
}

func TestExpiredReplacementCanResumeAfterProofSaveBeforeLedgerCommit(t *testing.T) {
	h, oldKey, newKey, _, _ := expiredHarness(t, true)
	before := h.ledger()
	if err := h.d.ReplaceExpired(context.Background(), oldKey, newKey); err != nil {
		t.Fatal(err)
	}
	proofPath := h.d.home("entries", safeKey(oldKey), "replacement-2.json")
	proof, err := os.ReadFile(proofPath)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a crash after the exclusive proof write but before the atomic ledger replace.
	if err := h.d.Store.Update(func(l *Ledger) error { *l = *before; return nil }); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(time.Minute)
	if err := h.d.ReplaceExpired(context.Background(), oldKey, newKey); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(proofPath)
	if err != nil || string(after) != string(proof) {
		t.Fatal("first original proof overwritten")
	}
	if len(h.ledger().Budget.Replacements) != 1 {
		t.Fatal("interruption duplicated a replacement")
	}
}

func TestChangedReplacementProofFailsWithoutChangingTheLedger(t *testing.T) {
	h, oldKey, newKey, _, _ := expiredHarness(t, false)
	dir := h.d.home("entries", safeKey(oldKey))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "replacement-2.json")
	if err := os.WriteFile(path, []byte(`{"original_entry":null}`), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(h.ledger())
	if err := h.d.ReplaceExpired(context.Background(), oldKey, newKey); err == nil {
		t.Fatal("changed proof accepted")
	}
	after, _ := json.Marshal(h.ledger())
	if string(before) != string(after) {
		t.Fatal("changed proof mutated the ledger")
	}
}
