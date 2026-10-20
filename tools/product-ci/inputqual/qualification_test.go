package inputqual

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fixture struct {
	t     *testing.T
	p     policy
	prior map[string]any
}

func (f *fixture) put(path string, value any) anchor {
	f.t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		f.t.Fatal(err)
	}
	if os.WriteFile(path, data, 0600) != nil {
		f.t.Fatal("synthetic fixture write")
	}
	return anchor{path, hash(data), 8 << 20}
}
func (f *fixture) updatePrior() { f.p.predecessor = f.put(f.p.predecessor.path, f.prior) }
func newFixture(t *testing.T) *fixture {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{t: t}
	f.p = policy{root: base, pin: filepath.Join(base, "pin.json"), review: filepath.Join(base, "review.json"), acceptance: filepath.Join(base, "acceptance.json"), db: filepath.Join(base, "tasks.json"), origin: func(string) bool { return true }}
	f.p.scope = f.put(filepath.Join(base, "scope.md"), "synthetic independently retained current task scope")
	f.p.parentSpec = f.put(filepath.Join(base, "parent.md"), "synthetic independently retained original scope")
	f.p.originSpec = f.put(filepath.Join(base, "origin.md"), "synthetic independently retained initial owner scope")
	reviewOutput := []byte("private synthetic independently retained original review output")
	f.p.reviewSHA = hash(reviewOutput)
	f.p.original = f.put(filepath.Join(base, "original.json"), map[string]any{"todo_id": 3068, "original_todo": 3056, "repo_id": 1285649124, "pr": 995, "new_head": fixedHead, "new_tree": fixedTree, "finite_acceptance_complete": true, "final_ref_updated": true, "final_review": map[string]any{"candidate": fixedHead, "status": "succeeded", "must": 0, "runtime_context_verified": true, "response_sha256": f.p.reviewSHA, "job": "20261020-160356-todo3068-csv-final-review"}})
	ownedID := strings.Repeat("a", 32)
	f.p.owned = f.put(filepath.Join(base, "owned.json"), map[string]any{"id": ownedID, "path": base, "project": "todo-3059", "content": "current-cbd-evidence-and-ci", "state": "owned"})
	proofSHA := map[string]string{}
	for kind, path := range map[string]string{"review": f.p.review, "acceptance": f.p.acceptance} {
		original := reviewOutput
		if kind == "acceptance" {
			original, err = os.ReadFile(f.p.original.path)
			if err != nil {
				t.Fatal(err)
			}
		}
		a := f.put(path, map[string]any{"origin_todo_id": 3056, "repo_id": 1285649124, "pr": 995, "head_sha": fixedHead, "head_tree": fixedTree, "kind": kind, "success": true, "must": 0, "original": original, "original_sha256": hash(original)})
		proofSHA[kind] = a.sha
	}
	pin := f.put(f.p.pin, map[string]any{"repo_id": 1285649124, "pr": 995, "head_sha": fixedHead, "head_tree": fixedTree, "base_sha": "41090d19329b81dd1bd076f3336d723823be4b82", "run_id": 39384472279, "attempt": 1, "job_ids": []int64{118500893552}, "kind": "secret", "context_sha": "6e7ca682c861024b6f08f31382851dab4bceff35", "review_sha256": proofSHA["review"], "acceptance_sha256": proofSHA["acceptance"], "review_file": f.p.review, "acceptance_file": f.p.acceptance})
	f.prior = map[string]any{"todo_id": 3059, "origin_todo_id": 3056, "additional_todo_id": 3068, "owned_id": ownedID, "owned_path": base, "head": fixedHead, "tree": fixedTree, "review_sha256": proofSHA["review"], "acceptance_sha256": proofSHA["acceptance"], "original_receipt_sha256": f.p.original.sha, "original_artifacts_verified": 36, "current_secret_pin": map[string]any{"head": fixedHead, "run": 39384472279, "attempt": 1, "job_ids": []int64{118500893552}, "secret_pin": f.p.pin, "secret_pin_sha256": pin.sha}}
	f.p.predecessor = anchor{path: filepath.Join(base, "predecessor.json")}
	f.updatePrior()
	f.put(f.p.db, map[string]any{"items": []any{map[string]any{"id": 3059, "status": "wait", "dispatchable": true, "spec": f.p.parentSpec.path, "wait_on": []int{3089}}, map[string]any{"id": 3056, "status": "wait", "spec": f.p.originSpec.path, "wait_on": []int{3059}}, map[string]any{"id": 3068, "status": "done"}, map[string]any{"id": 3089, "status": "doing", "dispatchable": true, "spec": f.p.scope.path}}})
	f.p.stop = f.put(filepath.Join(base, "stop.json"), map[string]any{"origin_todo_id": 3059, "input_owned_id": ownedID, "guard_client_resource": true, "guard_classification_unverified": true, "input_files": []string{filepath.Base(f.p.pin), filepath.Base(f.p.review), filepath.Base(f.p.acceptance)}})
	f.p.final = f.put(filepath.Join(base, "final.json"), map[string]any{"origin_todo_id": 3059, "current_product_candidate": map[string]any{"repo_id": 1285649124, "pr": 995, "head": fixedHead, "tree": fixedTree, "base": fixedBase, "source_evidence_sha256": f.p.original.sha, "current_secret_pin": f.p.pin, "current_secret_pin_sha256": pin.sha, "secret_run": int64(39384472279), "secret_job": int64(118500893552), "secret_attempt": 1}})
	f.p.outcome = f.put(filepath.Join(base, "outcome.json"), map[string]any{"origin_todo_id": 3059, "final_candidate_receipt": f.p.final.path, "final_candidate_receipt_sha256": f.p.final.sha, "guard_stop_file": f.p.stop.path, "target995_run": int64(39384472279), "target995_job": int64(118500893552), "target995_inspect_exit": 0})
	return f
}

func TestPurposeUsesIndependentAuthorityAndSameBytes(t *testing.T) {
	f := newFixture(t)
	snapshot, err := qualify(f.p.pin, f.p)
	if err != nil {
		t.Fatal(err)
	}
	r := snapshot.Report()
	if !r.Qualified || r.Purpose != Purpose || r.Task != 3059 || r.OriginTask != 3056 || r.AdditionalTask != 3068 || r.Head != fixedHead || r.Tree != fixedTree || r.Run != 39384472279 || r.Job != 118500893552 || r.OriginalRegistryRecovered || !r.GeneralClassificationUnverified || r.ConfidentialityEstablished || r.IndividualCreationToolEstablished || r.IndividualCreationTimeEstablished || r.OfficialCISuccess {
		t.Fatal("purpose changed source or claimed general/private classification")
	}
	// Replacement after qualification cannot replace what the consumer parses.
	if os.WriteFile(f.p.pin, []byte("changed after qualification"), 0600) != nil || os.Remove(f.p.review) != nil || os.Remove(f.p.acceptance) != nil {
		t.Fatal("fixture replacement")
	}
	called := false
	if err := snapshot.Consume(func(pin, review, acceptance []byte) error {
		called = true
		if hash(pin) != r.PinSHA || hash(review) != r.ReviewSHA || hash(acceptance) != r.AcceptanceSHA {
			t.Fatal("consumer received unverified bytes")
		}
		return nil
	}); err != nil || !called {
		t.Fatal("verified snapshot reopened a replaced resource")
	}
	if _, err := qualify(f.p.pin, f.p); err == nil {
		t.Fatal("a new invocation accepted changed files")
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", snapshot, snapshot, snapshot), "canary") {
		t.Fatal("snapshot diagnostic disclosed original text")
	}
}

func TestIndependentAuthoritiesCannotBeSelfReplaced(t *testing.T) {
	for _, name := range []string{"scope", "parent", "origin", "prior", "original", "owned", "outcome", "final", "stop"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			a := map[string]anchor{"scope": f.p.scope, "parent": f.p.parentSpec, "origin": f.p.originSpec, "prior": f.p.predecessor, "original": f.p.original, "owned": f.p.owned, "outcome": f.p.outcome, "final": f.p.final, "stop": f.p.stop}[name]
			if os.WriteFile(a.path, []byte("self-written replacement"), 0600) != nil {
				t.Fatal("fixture replacement")
			}
			if _, err := qualify(f.p.pin, f.p); err == nil {
				t.Fatal("missing independent authority accepted")
			}
		})
	}
}

func TestUnknownCopiedInputRejectedBeforeAuthorityAccess(t *testing.T) {
	f := newFixture(t)
	calls := 0
	f.p.origin = func(string) bool { calls++; return true }
	data, err := os.ReadFile(f.p.pin)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(filepath.Dir(f.p.pin), "copied.json")
	if os.WriteFile(copyPath, data, 0600) != nil {
		t.Fatal("copy fixture")
	}
	for _, path := range []string{copyPath, "pin.json", filepath.Join(filepath.Dir(f.p.pin), "unknown.json")} {
		if _, err := qualify(path, f.p); err == nil {
			t.Fatal("unknown or reserialized location created authority")
		}
	}
	if calls != 0 {
		t.Fatal("unknown input reached original authority")
	}
}

func TestChangedTaskHeadRunAndOriginalResultRejected(t *testing.T) {
	for _, name := range []string{"task", "origin", "additional", "head", "tree", "run", "job", "attempt", "count", "stopped", "missing-task", "source-origin", "original-result"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			switch name {
			case "task":
				f.prior["todo_id"] = 3060
			case "origin":
				f.prior["origin_todo_id"] = 3057
			case "additional":
				f.prior["additional_todo_id"] = 3069
			case "head":
				f.prior["head"] = strings.Repeat("e", 40)
			case "tree":
				f.prior["tree"] = strings.Repeat("e", 40)
			case "run":
				f.prior["current_secret_pin"].(map[string]any)["run"] = 39384472280
			case "job":
				f.prior["current_secret_pin"].(map[string]any)["job_ids"] = []int64{118500893553}
			case "attempt":
				f.prior["current_secret_pin"].(map[string]any)["attempt"] = 2
			case "count":
				f.prior["original_artifacts_verified"] = 35
			case "stopped":
				data, _ := os.ReadFile(f.p.db)
				data = bytes.Replace(data, []byte(`"status":"doing"`), []byte(`"status":"block"`), 1)
				if os.WriteFile(f.p.db, data, 0600) != nil {
					t.Fatal("fixture state")
				}
			case "missing-task":
				f.put(f.p.db, map[string]any{"items": []any{}})
			case "source-origin":
				f.p.origin = func(string) bool { return false }
			case "original-result":
				data, _ := os.ReadFile(f.p.original.path)
				data = bytes.Replace(data, []byte(`"must":0`), []byte(`"must":1`), 1)
				if os.WriteFile(f.p.original.path, data, 0600) != nil {
					t.Fatal("fixture result")
				}
				f.p.original.sha = hash(data)
				f.prior["original_receipt_sha256"] = hash(data)
			}
			f.updatePrior()
			if _, err := qualify(f.p.pin, f.p); err == nil {
				t.Fatal("changed authority accepted")
			}
		})
	}
}

func TestChangedInputBytesAndRoleSubstitutionRejected(t *testing.T) {
	for _, name := range []string{"pin", "review", "acceptance", "duplicate-key", "role"} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			path := map[string]string{"pin": f.p.pin, "review": f.p.review, "acceptance": f.p.acceptance, "duplicate-key": f.p.pin, "role": f.p.review}[name]
			data, _ := os.ReadFile(path)
			switch name {
			case "duplicate-key":
				data = append([]byte(`{"repo_id":1285649124,`), data[1:]...)
				f.prior["current_secret_pin"].(map[string]any)["secret_pin_sha256"] = hash(data)
				f.updatePrior()
			case "role":
				data, _ = os.ReadFile(f.p.acceptance)
				f.prior["review_sha256"] = hash(data)
				f.updatePrior()
			default:
				data = append(data, ' ')
			}
			if os.WriteFile(path, data, 0600) != nil {
				t.Fatal("fixture input")
			}
			if _, err := qualify(f.p.pin, f.p); err == nil {
				t.Fatal("modified input accepted")
			}
		})
	}
}

func TestLinkAndPostVerificationMutationRejected(t *testing.T) {
	f := newFixture(t)
	snapshot, err := qualify(f.p.pin, f.p)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.review = append(snapshot.review, ' ')
	if snapshot.Consume(func([]byte, []byte, []byte) error { t.Fatal("changed bytes reached consumer"); return nil }) == nil {
		t.Fatal("changed snapshot accepted")
	}
	target := filepath.Join(filepath.Dir(f.p.pin), "real-pin.json")
	if os.Rename(f.p.pin, target) != nil {
		t.Fatal("fixture rename")
	}
	if err := os.Symlink(target, f.p.pin); err != nil {
		t.Skip("platform cannot create synthetic symlink")
	}
	if _, err := qualify(f.p.pin, f.p); err == nil {
		t.Fatal("link replacement accepted")
	}
}
