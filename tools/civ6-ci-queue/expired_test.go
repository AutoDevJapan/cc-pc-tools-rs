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
	for _, name := range []string{"not-expired", "assigned", "steps", "missing-steps", "missing-runner", "missing-runner-name", "failure", "short-wait", "other-head", "other-name", "wrong-attempt", "new-assigned", "new-runner-name", "new-started", "new-completed", "new-label", "old-label", "contract", "retirement", "collection", "census-live", "census-invalid", "live", "unknown-resource", "foreign-fence", "not-in-budget", "context", "changed-during-proof"} {
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
			case "new-runner-name":
				next["runner_name"] = "assigned"
			case "new-started":
				next["steps"] = []obj{{"name": "test"}}
			case "new-completed":
				next["status"] = "completed"
			case "new-label":
				next["labels"] = []string{"foreign"}
			case "old-label":
				old["labels"] = []string{"foreign"}
			case "contract":
				h.c.verifyErr = func(Plan) error { return errors.New("source changed") }
			case "retirement":
				h.d.Retired = func(CleanupRec, string) (CleanupRec, error) { return CleanupRec{}, errors.New("not retired") }
			case "collection":
				h.d.VerifyRetired = func(context.Context, *Entry, string) (CollectionObservation, error) {
					return CollectionObservation{}, errors.New("not collected")
				}
			case "census-live", "census-invalid":
				h.d.VerifyRetired = func(context.Context, *Entry, string) (CollectionObservation, error) {
					if name == "census-live" {
						return CollectionObservation{Queryable: 1}, nil
					}
					return CollectionObservation{Unreadable: -1}, nil
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

func TestExpiredQueuedReplacementAcceptsNullUnassignedFields(t *testing.T) {
	for _, field := range []string{"runner_id", "runner_name", "steps", "all"} {
		t.Run(field, func(t *testing.T) {
			h, oldKey, newKey, _, next := expiredHarness(t, false)
			if field == "all" {
				next["runner_id"], next["runner_name"], next["steps"] = nil, nil, nil
			} else {
				next[field] = nil
			}
			if err := h.d.ReplaceExpired(context.Background(), oldKey, newKey); err != nil {
				t.Fatal(err)
			}
			if len(h.ledger().Budget.Replacements) != 1 || h.sd.n != 1 || h.win.names() != "Windows:lifecycle" {
				t.Fatal("admission created a resource or lost the original slot")
			}
		})
	}
}

func addExpiredSibling(t *testing.T, h *harness, oldKey string, run, job int64) (string, string) {
	t.Helper()
	original := h.ledger().Entries[oldKey]
	now := h.now
	h.now, _ = time.Parse(time.RFC3339Nano, original.Queued.At)
	key := h.seed("Windows", run, job, "test (windows-latest)", false)
	h.now = now
	started, _ := time.Parse(time.RFC3339Nano, original.Queued.At)
	h.gh.data[fmt.Sprintf("repos/%s/actions/jobs/%d", Repository, job)] = obj{
		"id": job, "run_id": run, "run_attempt": 1, "head_sha": headSHA, "name": "test (windows-latest)",
		"status": "completed", "conclusion": "cancelled", "runner_id": 0, "runner_name": "", "steps": []obj{},
		"started_at": stamp(started), "completed_at": stamp(started.Add(24 * time.Hour)),
	}
	h.gh.data[fmt.Sprintf("repos/%s/actions/jobs/%d", Repository, job+1)] = obj{
		"id": job + 1, "run_id": run, "run_attempt": 2, "head_sha": headSHA, "name": "test (windows-latest)",
		"status": "queued", "runner_id": nil, "runner_name": nil, "steps": nil,
	}
	return key, fmt.Sprintf("%d/2/%d", run, job+1)
}

func TestExpiredUncreatedSiblingCanBeReplacedWithoutClearingForeignFence(t *testing.T) {
	h, oldKey, _, _, _ := expiredHarness(t, true)
	key, next := addExpiredSibling(t, h, oldKey, 803, 8031)
	fence := h.ledger().Fences["Windows"]
	if err := h.d.ReplaceExpired(context.Background(), key, next); err != nil {
		t.Fatal(err)
	}
	if h.ledger().Fences["Windows"] != fence || h.tick().Launched != 0 || h.sd.n != 1 {
		t.Fatal("sibling admission released a foreign fence or executed work")
	}
}

func TestThreeExpiredReplacementsSurviveInterleavedOrdinaryTicks(t *testing.T) {
	h, first, firstNext, _, _ := expiredHarness(t, true)
	second, secondNext := addExpiredSibling(t, h, first, 803, 8031)
	third, thirdNext := addExpiredSibling(t, h, first, 805, 8051)
	h.win.out = Outcome{Started: true, HelperOK: true, Collected: true}
	for i, pair := range [][2]string{{first, firstNext}, {second, secondNext}, {third, thirdNext}} {
		if i > 0 {
			if h.tick().Launched != 1 || h.spawned[len(h.spawned)-1] != pair[0] {
				t.Fatal("fixture did not let the ordinary tick claim the expired sibling")
			}
			creates, calls := h.sd.n, h.win.names()
			if err := h.d.Supervise(context.Background(), pair[0]); err != nil {
				t.Fatal(err)
			}
			e := h.ledger().Entries[pair[0]]
			if e.State != StateNeedsRecovery || e.Started.IntentAt == "" || e.Cleanup.State != "none" || h.sd.n != creates || h.win.names() != calls {
				t.Fatal("expired sibling created resources or launch evidence was discarded")
			}
		}
		if err := h.d.ReplaceExpired(context.Background(), pair[0], pair[1]); err != nil {
			t.Fatal(err)
		}
		if h.tick().Launched != 1 || h.spawned[len(h.spawned)-1] != pair[1] {
			t.Fatal("ordinary tick did not launch the replacement")
		}
		if err := h.d.Supervise(context.Background(), pair[1]); err != nil {
			t.Fatal(err)
		}
		if h.ledger().Entries[pair[1]].State != StateSucceeded {
			t.Fatal("replacement lifecycle did not finish")
		}
	}
	l := h.ledger()
	if len(l.Budget.Initial) != 3 || len(l.Budget.Normal) != 0 || len(l.Budget.Replacements) != 3 || len(l.Fences) != 0 || h.sd.n != 4 {
		t.Fatalf("budget, fence or actual resource count changed: %+v", l)
	}
	before, _ := json.Marshal(l)
	if err := h.d.ReplaceExpired(context.Background(), first, "801/2/8099"); err == nil {
		t.Fatal("fourth replacement admitted")
	}
	after, _ := json.Marshal(h.ledger())
	if string(before) != string(after) {
		t.Fatal("fourth replacement changed the ledger")
	}
}

func TestExpiredReplacementRechecksCensusAndKeepsFirstSavedObservation(t *testing.T) {
	h, oldKey, newKey, _, _ := expiredHarness(t, true)
	before := h.ledger()
	if err := h.d.ReplaceExpired(context.Background(), oldKey, newKey); err != nil {
		t.Fatal(err)
	}
	path := h.d.home("entries", safeKey(oldKey), "replacement-2.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.d.Store.Update(func(l *Ledger) error { *l = *before; return nil }); err != nil {
		t.Fatal(err)
	}
	calls := 0
	h.d.VerifyRetired = func(context.Context, *Entry, string) (CollectionObservation, error) {
		calls++
		return CollectionObservation{Unreadable: 5}, nil
	}
	h.now = h.now.Add(time.Minute)
	if err := h.d.ReplaceExpired(context.Background(), oldKey, newKey); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(original) || calls != 1 {
		t.Fatal("retry skipped current collection or overwrote the original census")
	}
}

func TestReplacementProofPublicationNeverOverwritesExistingBytes(t *testing.T) {
	dir := t.TempDir()
	from, to := filepath.Join(dir, "new.tmp"), filepath.Join(dir, "original.json")
	if err := os.WriteFile(from, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := publishFile(from, to); !os.IsExist(err) {
		t.Fatalf("exclusive publication did not report an existing archive: %v", err)
	}
	got, err := os.ReadFile(to)
	if err != nil || string(got) != "original" {
		t.Fatal("original archive overwritten")
	}
}

func TestReplacementProofIgnoresInterruptedUnpublishedTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".replacement-interrupted.tmp"), []byte(`{"original_entry":`), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "replacement-2.json")
	want := expiredProof{At: stamp(time.Now())}
	b, err := saveExpiredProof(path, want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(b) || !json.Valid(got) {
		t.Fatal("complete original archive was not published")
	}
}
