package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	contract "cc/civ6-ci-contract"
)

func migrationHarness(t *testing.T) (*harness, []string, []string) {
	t.Helper()
	h := newHarness(t)
	h.d.VerifyStopped = func(id ProcID) error {
		if (id.PID == 0) != (id.Created == 0) || h.d.Alive(id) {
			return errors.New("not provably stopped")
		}
		return nil
	}
	oldKeys := []string{
		h.seed("Windows", 39423559387, 118630300541, "test (windows-latest)", true),
		h.seed("Windows", 39423559434, 118630300375, "live-chain", false),
		h.seed("Windows", 39423559440, 118630301061, "native-driver", false),
	}
	newKeys := []string{"5001/1/50011", "5002/1/50021", "5003/1/50031"}
	oldContext := strings.Repeat("4", 40)
	prefix := "repos/" + Repository + "/"
	repo := obj{"id": RepoID, "full_name": Repository, "fork": false, "owner": obj{"login": "AutoDevJapan"}, "permissions": obj{"admin": true}}
	h.gh.data["repositories/1392152644"] = repo
	h.gh.data[prefix+"pulls/149"] = obj{"number": 149, "state": "open", "merged": false, "merge_commit_sha": mergeSHA, "head": obj{"sha": headSHA, "ref": branch, "repo": repo}, "base": obj{"sha": contract.OriginalBase, "ref": "main", "repo": repo}}
	h.gh.data[prefix+"git/ref/heads/"+branch] = obj{"object": obj{"sha": headSHA}}
	h.gh.data[prefix+"git/ref/heads/main"] = obj{"object": obj{"sha": contract.MergeMain}}
	h.gh.data[prefix+"git/commits/"+headSHA] = obj{"sha": headSHA, "tree": obj{"sha": treeSHA}, "parents": []obj{{"sha": contract.OldRoutedHead}, {"sha": contract.MergeMain}}}
	h.gh.data[prefix+"git/commits/"+contract.OldRoutedHead] = obj{"sha": contract.OldRoutedHead, "tree": obj{"sha": contract.OldRoutedTree}, "parents": []obj{{"sha": contract.OriginalHead}}}
	h.gh.data[prefix+"git/commits/"+contract.MergeMain] = obj{"sha": contract.MergeMain, "tree": obj{"sha": contract.MergeMainTree}}
	trees := map[string][]obj{}
	for _, w := range contract.Workflows() {
		original, err := os.ReadFile(filepath.Join("../civ6-ci-contract/testdata", filepath.Base(w.Path)))
		if err != nil {
			t.Fatal(err)
		}
		old, err := contract.Propose(original, w)
		if err != nil {
			t.Fatal(err)
		}
		main, err := os.ReadFile(filepath.Join("../civ6-ci-contract/testdata/main392", filepath.Base(w.Path)))
		if err != nil {
			t.Fatal(err)
		}
		var next []byte
		for _, mw := range contract.MainWorkflows() {
			if mw.Path == w.Path {
				next, err = contract.ProposeMain(main, mw)
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		for sha, body := range map[string][]byte{contract.OriginalHead: original, contract.OldRoutedHead: old, oldContext: old, contract.MergeMain: main, headSHA: next, mergeSHA: next} {
			h.gh.data[prefix+"contents/"+w.Path+"?ref="+sha] = obj{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString(body), "sha": blobSHA(body)}
		}
		for sha, body := range map[string][]byte{contract.OriginalTree: original, contract.OldRoutedTree: old, contract.MergeMainTree: main, treeSHA: next} {
			trees[sha] = append(trees[sha], obj{"path": w.Path, "mode": "100644", "type": "blob", "sha": blobSHA(body)})
		}
	}
	for sha, entries := range trees {
		entries = append(entries, obj{"path": "tools/runner.rs", "mode": "100644", "type": "blob", "sha": strings.Repeat("7", 40)})
		h.gh.data[prefix+"git/trees/"+sha+"?recursive=1"] = obj{"sha": sha, "truncated": false, "tree": entries}
	}
	var active []obj
	if err := h.d.Store.Update(func(l *Ledger) error {
		for i, key := range oldKeys {
			e := l.Entries[key]
			w, _ := contract.WorkflowByPath(originalSourceSlots[key])
			e.Plan.Head, e.Plan.Tree, e.Plan.ContextSHA, e.Plan.WorkflowSHA = contract.OldRoutedHead, contract.OldRoutedTree, oldContext, oldContext
			e.Plan.Workflow, e.Plan.Job = w.Path, w.Job
			e.Plan.WorkflowRef = Repository + "/" + w.Path + "@refs/pull/149/merge"
			v := h.gh.data[prefix+"contents/"+w.Path+"?ref="+oldContext].(obj)
			body, _ := base64.StdEncoding.DecodeString(v["content"].(string))
			e.Plan.WorkflowDigest = digestHex(body)
			h.gh.data[fmt.Sprintf("%sactions/runs/%d/attempts/1", prefix, e.Plan.RunID)] = obj{"id": e.Plan.RunID, "run_attempt": 1, "path": w.Path, "event": "pull_request", "status": "completed", "head_sha": contract.OldRoutedHead, "head_branch": branch, "repository": repo, "head_repository": repo, "pull_requests": []obj{{"number": 149}}}
			h.gh.data[fmt.Sprintf("%sactions/jobs/%d", prefix, e.Plan.JobID)] = obj{"id": e.Plan.JobID, "run_id": e.Plan.RunID, "run_attempt": 1, "head_sha": e.Plan.Head, "name": e.Plan.JobName, "status": "completed", "conclusion": "cancelled", "runner_id": 0, "runner_name": "", "steps": []obj{}, "labels": contract.Labels(cp(e.Plan)), "started_at": stamp(h.now), "completed_at": stamp(h.now.Add(24 * time.Hour))}
			runID, jobID := int64(5001+i), int64(50011+i*10)
			np := e.Plan
			np.Head, np.Tree, np.ContextSHA, np.WorkflowSHA, np.RunID, np.JobID = headSHA, treeSHA, mergeSHA, mergeSHA, runID, jobID
			nextBody := h.gh.data[prefix+"contents/"+w.Path+"?ref="+mergeSHA].(obj)
			b, _ := base64.StdEncoding.DecodeString(nextBody["content"].(string))
			np.WorkflowDigest = digestHex(b)
			r := obj{"id": runID, "run_attempt": 1, "path": w.Path, "event": "pull_request", "status": "queued", "head_sha": headSHA, "head_branch": branch, "repository": repo, "head_repository": repo, "pull_requests": []obj{{"number": 149}}}
			h.gh.data[fmt.Sprintf("%sactions/runs/%d", prefix, runID)] = r
			active = append(active, r)
			j := obj{"id": jobID, "run_id": runID, "run_attempt": 1, "head_sha": headSHA, "name": np.JobName, "status": "queued", "conclusion": "", "runner_id": nil, "runner_name": nil, "steps": nil, "labels": contract.Labels(cp(np))}
			h.gh.data[fmt.Sprintf("%sactions/jobs/%d", prefix, jobID)] = j
			for _, endpoint := range []string{fmt.Sprintf("%sactions/runs/%d/attempts/1/jobs?per_page=100", prefix, runID), fmt.Sprintf("%sactions/runs/%d/jobs?per_page=100&page=1", prefix, runID)} {
				h.gh.data[endpoint] = obj{"total_count": 1, "jobs": []obj{j}}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Only Rust created a resource and failed before any official assignment.
	h.win.out = Outcome{Started: true, Collected: false, Exit: 1}
	if err := h.d.Supervise(context.Background(), oldKeys[0]); err != nil {
		t.Fatal(err)
	}
	h.d.Retired = func(cl CleanupRec, key string) (CleanupRec, error) { cl.State = "quarantined"; return cl, nil }
	h.d.VerifyRetired = func(context.Context, *Entry, string) (CollectionObservation, error) {
		return CollectionObservation{Unreadable: 3}, nil
	}
	for _, status := range []string{"requested", "pending", "waiting", "queued", "in_progress"} {
		runs := []obj{}
		if status == "queued" {
			runs = active
		}
		h.gh.data[prefix+"actions/runs?status="+status+"&per_page=100&page=1"] = obj{"total_count": len(runs), "workflow_runs": runs}
	}
	h.now = h.now.Add(25 * time.Hour)
	h.d.C = bridge{}
	return h, oldKeys, newKeys
}

func TestThreeUnexecutedSourceMigrationsPreserveBudgetAndOriginalEvidence(t *testing.T) {
	h, keys, next := migrationHarness(t)
	before := h.ledger()
	result, err := os.ReadFile(h.d.resultPath(keys[0]))
	if err != nil {
		t.Fatal(err)
	}
	calls, creates := h.win.names(), h.sd.n
	// Never-created siblings can be admitted while Rust's foreign fence stays.
	for _, i := range []int{1, 2, 0} {
		fence := h.ledger().Fences["Windows"]
		if err := h.d.MigrateUnexecutedSource(context.Background(), keys[i], next[i]); err != nil {
			t.Fatal(i, err)
		}
		l := h.ledger()
		if i != 0 && l.Fences["Windows"] != fence {
			t.Fatal("foreign fence removed")
		}
		old := *l.Entries[keys[i]]
		old.State = before.Entries[keys[i]].State
		a, _ := json.Marshal(old)
		b, _ := json.Marshal(before.Entries[keys[i]])
		if !bytes.Equal(a, b) {
			t.Fatal("original history/deadline/cleanup changed")
		}
		e := l.Entries[next[i]]
		if e == nil || e.SourceReplaces != keys[i] || e.Replaces != "" || e.State != StateReserved || e.Started.Attempts != 0 || e.Official.Success || e.Queued.Deferrals != before.Entries[keys[i]].Queued.Deferrals {
			t.Fatal("migration executed or reset history", e)
		}
		proof, err := os.ReadFile(h.d.home("entries", safeKey(keys[i]), "source-main392.json"))
		if err != nil {
			t.Fatal(err)
		}
		var p sourceMigrationProof
		if json.Unmarshal(proof, &p) != nil || p.NeverCreated != (i != 0) || p.Original.Plan.Head != contract.OldRoutedHead || l.Budget.SourceMigrations[len(l.Budget.SourceMigrations)-1].ProofSHA256 != digestHex(proof) {
			t.Fatal("source proof binding missing")
		}
		if i == 0 && (!bytes.Equal(p.OriginalResult, result) || p.Census.Unreadable != 3 || p.CensusAfter.Unreadable != 3) {
			t.Fatal("raw result/census lost")
		}
	}
	l := h.ledger()
	if len(l.Budget.Initial) != 3 || len(l.Budget.Normal) != 0 || len(l.Budget.Continuation) != 0 || len(l.Budget.Replacements) != 0 || len(l.Budget.SourceMigrations) != 3 || len(l.Entries) != 6 || h.sd.n != creates || h.win.names() != calls || len(l.Fences) != 0 {
		t.Fatal("migration changed budget or executed effects", l.Budget)
	}
	if l.Version != 2 {
		t.Fatal("old version1 writer could erase the migration relations")
	}
	var oldWriter struct {
		Version int `json:"version"`
	}
	wire, _ := os.ReadFile(h.d.Store.Path)
	if json.Unmarshal(wire, &oldWriter) != nil || oldWriter.Version == 1 {
		t.Fatal("original writer would accept new schema")
	}
	for i := range keys {
		if h.d.MigrateUnexecutedSource(context.Background(), keys[i], next[i]) == nil || h.d.ReplaceExpired(context.Background(), keys[i], fmt.Sprintf("%d/2/9", before.Entries[keys[i]].Plan.RunID)) == nil {
			t.Fatal("slot migrated twice or reinterpreted as expired")
		}
	}
	after, _ := os.ReadFile(h.d.resultPath(keys[0]))
	if !bytes.Equal(result, after) {
		t.Fatal("original result overwritten")
	}
}

func TestSourceMigrationRejectsUnsafeProofWithoutLedgerChange(t *testing.T) {
	for _, kind := range []string{"other-key", "other-phase", "old-success", "old-started", "old-head", "old-assigned", "old-missing-steps", "old-cancel-change", "not-expired", "new-attempt", "new-assigned", "new-started", "new-name", "new-owner", "new-head", "new-parent", "main-advance", "extra-file", "unknown-cleanup", "live", "retirement", "marker", "census", "foreign-fence", "budget", "result-change", "ledger-cas", "context", "expired-slot"} {
		t.Run(kind, func(t *testing.T) {
			h, keys, next := migrationHarness(t)
			key, newKey := keys[0], next[0]
			prefix := "repos/" + Repository + "/"
			old := h.gh.data[prefix+"actions/jobs/118630300541"].(obj)
			job := h.gh.data[prefix+"actions/jobs/50011"].(obj)
			ctx := context.Background()
			switch kind {
			case "other-key":
				key = "1/1/1"
			case "other-phase":
				_ = h.d.mutate(key, func(_ *Ledger, e *Entry) { e.Phase = "normal" })
			case "old-success":
				_ = h.d.mutate(key, func(_ *Ledger, e *Entry) { e.Official.Success = true })
			case "old-started":
				old["steps"] = []obj{{"name": "test"}}
			case "old-head":
				_ = h.d.mutate(key, func(_ *Ledger, e *Entry) { e.Plan.Head = headSHA })
			case "old-assigned":
				old["runner_id"] = 42
			case "old-missing-steps":
				delete(old, "steps")
			case "old-cancel-change":
				h.d.VerifyRetired = func(context.Context, *Entry, string) (CollectionObservation, error) {
					old["completed_at"] = stamp(h.now)
					return CollectionObservation{}, nil
				}
			case "not-expired":
				h.now = h.now.Add(-24 * time.Hour)
			case "new-attempt":
				newKey = "5001/2/50011"
			case "new-assigned":
				job["runner_id"] = 42
			case "new-started":
				job["steps"] = []obj{{"name": "test"}}
			case "new-name":
				job["name"] = "native-driver"
			case "new-owner":
				h.gh.data["repositories/1392152644"].(obj)["owner"] = obj{"login": "foreign"}
			case "new-head":
				h.gh.data[prefix+"git/ref/heads/"+branch] = obj{"object": obj{"sha": contract.OldRoutedHead}}
			case "new-parent":
				h.gh.data[prefix+"git/commits/"+headSHA].(obj)["parents"] = []obj{{"sha": contract.MergeMain}, {"sha": contract.OldRoutedHead}}
			case "main-advance":
				h.gh.data[prefix+"git/ref/heads/main"] = obj{"object": obj{"sha": mergeSHA}}
			case "extra-file":
				tr := h.gh.data[prefix+"git/trees/"+treeSHA+"?recursive=1"].(obj)
				tr["tree"] = append(tr["tree"].([]obj), obj{"path": "extra.go", "mode": "100644", "type": "blob", "sha": headSHA})
			case "unknown-cleanup":
				_ = h.d.mutate(key, func(_ *Ledger, e *Entry) { e.Cleanup.State = "create_intent"; e.Cleanup.SafeDeleteID = "" })
			case "live":
				h.d.Alive = func(ProcID) bool { return true }
			case "retirement":
				h.d.Retired = func(CleanupRec, string) (CleanupRec, error) { return CleanupRec{}, errors.New("not closed") }
			case "marker":
				h.d.VerifyRetired = func(context.Context, *Entry, string) (CollectionObservation, error) {
					return CollectionObservation{}, errors.New("marker or SCM differs")
				}
			case "census":
				h.d.VerifyRetired = func(context.Context, *Entry, string) (CollectionObservation, error) {
					return CollectionObservation{Queryable: 1, Unreadable: 3}, nil
				}
			case "foreign-fence":
				_ = h.d.Store.Update(func(l *Ledger) error { l.Fences["Windows"] = Fence{Key: "other"}; return nil })
			case "budget":
				_ = h.d.Store.Update(func(l *Ledger) error { l.Budget.Initial[2] = key; return nil })
			case "result-change":
				h.d.VerifyRetired = func(context.Context, *Entry, string) (CollectionObservation, error) {
					_ = os.WriteFile(h.d.resultPath(key), []byte("changed"), 0600)
					return CollectionObservation{}, nil
				}
			case "ledger-cas":
				h.d.VerifyRetired = func(context.Context, *Entry, string) (CollectionObservation, error) {
					_ = h.d.Store.Update(func(l *Ledger) error { l.ResidueCount++; return nil })
					return CollectionObservation{}, nil
				}
			case "context":
				c, cancel := context.WithCancel(ctx)
				cancel()
				ctx = c
			case "expired-slot":
				_ = h.d.Store.Update(func(l *Ledger) error {
					l.Budget.Replacements = append(l.Budget.Replacements, ReplacementRec{Original: key})
					return nil
				})
			}
			before, _ := json.Marshal(h.ledger())
			if err := h.d.MigrateUnexecutedSource(ctx, key, newKey); err == nil {
				t.Fatal("unsafe source migration accepted")
			}
			after, _ := json.Marshal(h.ledger())
			if kind != "ledger-cas" && !bytes.Equal(before, after) {
				t.Fatal("failed proof changed ledger")
			}
			if len(h.ledger().Budget.SourceMigrations) != 0 {
				t.Fatal("failed proof reserved migration")
			}
		})
	}
}

func TestSourceMigrationCrashReusesOriginalSyncedProofAndFreshObservations(t *testing.T) {
	h, keys, next := migrationHarness(t)
	before := h.ledger()
	if err := h.d.MigrateUnexecutedSource(context.Background(), keys[0], next[0]); err != nil {
		t.Fatal(err)
	}
	path := h.d.home("entries", safeKey(keys[0]), "source-main392.json")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.d.Store.Update(func(l *Ledger) error { *l = *before; return nil }); err != nil {
		t.Fatal(err)
	}
	h.now = h.now.Add(time.Minute)
	calls := 0
	h.d.VerifyRetired = func(context.Context, *Entry, string) (CollectionObservation, error) {
		calls++
		return CollectionObservation{Unreadable: 5}, nil
	}
	if err := h.d.MigrateUnexecutedSource(context.Background(), keys[0], next[0]); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(first, after) || calls != 2 {
		t.Fatal("original At/Census overwritten or fresh proof skipped")
	}
	var saved sourceMigrationProof
	if json.Unmarshal(first, &saved) != nil || h.ledger().Entries[next[0]].Queued.At != saved.At {
		t.Fatal("resumed admission reset the source proof's deadline")
	}
	// A partial unpublished temporary file does not replace the committed proof.
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), ".source-migration-interrupted.tmp"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := h.d.Store.Update(func(l *Ledger) error { *l = *before; return nil }); err != nil {
		t.Fatal(err)
	}
	var bad sourceMigrationProof
	if json.Unmarshal(first, &bad) != nil {
		t.Fatal("proof invalid")
	}
	bad.Replacement.Head = contract.OldRoutedHead
	if _, err := saveSourceMigrationProof(path, bad); err == nil {
		t.Fatal("existing proof overwritten")
	}
}

func TestSourceMigrationSerializesTickAndOtherAdmissions(t *testing.T) {
	h, keys, next := migrationHarness(t)
	entered, release := make(chan struct{}), make(chan struct{})
	calls := 0
	h.d.VerifyRetired = func(context.Context, *Entry, string) (CollectionObservation, error) {
		calls++
		if calls == 1 {
			close(entered)
			<-release
		}
		return CollectionObservation{Unreadable: 4}, nil
	}
	done := make(chan error, 1)
	go func() { done <- h.d.MigrateUnexecutedSource(context.Background(), keys[0], next[0]) }()
	<-entered
	if rep := h.tick(); rep.Status != "busy" || rep.Launched != 0 {
		t.Fatal("tick raced source migration", rep)
	}
	if err := h.d.MigrateUnexecutedSource(context.Background(), keys[1], next[1]); err == nil {
		t.Fatal("second source migration bypassed lock")
	}
	if len(h.ledger().Budget.SourceMigrations) != 0 {
		t.Fatal("proof committed before it completed")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(h.ledger().Budget.SourceMigrations) != 1 {
		t.Fatal("local proof not observed before and after")
	}
}

func TestSourceMigrationPreservesNeverCreatedSupervisorAndDeferrals(t *testing.T) {
	h, keys, next := migrationHarness(t)
	// Ordinary supervision reached the old 24h limit before create_intent.
	if err := h.d.mutate(keys[1], func(_ *Ledger, e *Entry) {
		e.State = StateNeedsRecovery
		e.Started = StartRec{Attempts: 1, Claimed: true, IntentAt: stamp(h.now), Supervisor: ProcID{PID: 9000, Created: 7}, ExitCode: intp(1), ExitedAt: stamp(h.now)}
		e.Queued.Deferrals = 2
	}); err != nil {
		t.Fatal(err)
	}
	before := h.ledger().Entries[keys[1]]
	if err := h.d.MigrateUnexecutedSource(context.Background(), keys[1], next[1]); err != nil {
		t.Fatal(err)
	}
	e := h.ledger().Entries[keys[1]]
	a, _ := json.Marshal(e.Started)
	b, _ := json.Marshal(before.Started)
	if !bytes.Equal(a, b) || e.Queued != before.Queued || h.ledger().Entries[next[1]].Queued.Deferrals != 2 || h.ledger().Fences["Windows"].Key != keys[0] {
		t.Fatal("supervisor history/deferrals/fence changed")
	}
	h2, k2, n2 := migrationHarness(t)
	_ = h2.d.mutate(k2[1], func(_ *Ledger, e *Entry) {
		e.Prestarts = []PrestartRec{{Cleanup: &CleanupRec{State: "removed", Root: "unknown"}}}
	})
	if h2.d.MigrateUnexecutedSource(context.Background(), k2[1], n2[1]) == nil {
		t.Fatal("historical created resource misclassified as never created")
	}
}

func TestMigratedReservationUsesOrdinaryTickWithoutRefundOrContinuationBypass(t *testing.T) {
	h, keys, next := migrationHarness(t)
	before := h.ledger()
	p := before.Entries[keys[0]].Plan
	p.RunID, p.JobID, p.Head = 5001, 50011, headSHA
	if err := h.d.Store.Update(func(l *Ledger) error {
		if e, reason := l.reserve(p, "Windows", true, h.now); e != nil || reason != "source_migration_required" {
			t.Fatal("continuation reset a fixed initial slot", e, reason)
		}
		return errNoWrite
	}); err != errNoWrite {
		t.Fatal(err)
	}
	for _, i := range []int{1, 2, 0} {
		if err := h.d.MigrateUnexecutedSource(context.Background(), keys[i], next[i]); err != nil {
			t.Fatal(err)
		}
	}
	if rep := h.tick(); rep.Launched != 1 || len(h.spawned) != 1 || h.spawned[0] != next[0] {
		t.Fatal("ordinary tick failed to launch one migrated slot", rep, h.spawned)
	}
	if len(h.ledger().Budget.Initial) != 3 || len(h.ledger().Budget.Continuation) != 0 || len(h.ledger().Budget.Replacements) != 0 {
		t.Fatal("ordinary tick changed acceptance budget")
	}
}

func TestLegacyExpiredReplacementCannotAdmitAnotherRunOrHead(t *testing.T) {
	for _, otherRun := range []bool{false, true} {
		h, key, next, _, job := expiredHarness(t, false)
		if otherRun {
			next = "802/2/8022"
		} else {
			job["head_sha"] = contract.OldRoutedHead
		}
		before, _ := json.Marshal(h.ledger())
		if err := h.d.ReplaceExpired(context.Background(), key, next); err == nil {
			t.Fatal("legacy attempt2 admitted a different run/head")
		}
		after, _ := json.Marshal(h.ledger())
		if !bytes.Equal(before, after) {
			t.Fatal("legacy rejection changed original budget")
		}
	}
}

func TestSourceProofPublicationSurvivesPartialTemporaryWrite(t *testing.T) {
	for _, published := range []bool{false, true} {
		h, keys, next := migrationHarness(t)
		dir := h.d.home("entries", safeKey(keys[0]))
		name := ".source-migration-interrupted.tmp"
		if published {
			name = "source-main392.json"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{"), 0600); err != nil {
			t.Fatal(err)
		}
		before, _ := json.Marshal(h.ledger())
		err := h.d.MigrateUnexecutedSource(context.Background(), keys[0], next[0])
		if published {
			if err == nil {
				t.Fatal("incomplete published original proof ignored")
			}
			after, _ := json.Marshal(h.ledger())
			if !bytes.Equal(before, after) {
				t.Fatal("invalid original proof changed ledger")
			}
		} else if err != nil {
			t.Fatal("unpublished partial temp blocked atomic publication", err)
		}
		b, _ := os.ReadFile(filepath.Join(dir, name))
		if string(b) != "{" {
			t.Fatal("interrupted evidence overwritten")
		}
	}
}

func TestSourceMigrationRejectsUnverifiableSupervisorIdentity(t *testing.T) {
	h, keys, next := migrationHarness(t)
	_ = h.d.mutate(keys[1], func(_ *Ledger, e *Entry) { e.Started.Supervisor = ProcID{PID: 9000} })
	if err := h.d.MigrateUnexecutedSource(context.Background(), keys[1], next[1]); err == nil {
		t.Fatal("missing process creation time was treated as stopped")
	}
}

func TestSourceMigrationRejectsUnreadableProcessObservationBeforeAndAfterProof(t *testing.T) {
	for _, completedChecks := range []int{0, 1, 2, 3} {
		h, keys, next := migrationHarness(t)
		calls := 0
		h.d.VerifyStopped = func(ProcID) error {
			calls++
			if calls > 2*completedChecks {
				return errors.New("access denied; process state unknown")
			}
			return nil
		}
		before, _ := json.Marshal(h.ledger())
		censusCalls := 0
		h.d.VerifyRetired = func(context.Context, *Entry, string) (CollectionObservation, error) {
			censusCalls++
			return CollectionObservation{}, nil
		}
		if h.d.MigrateUnexecutedSource(context.Background(), keys[0], next[0]) == nil {
			t.Fatal("unreadable process treated as stopped")
		}
		after, _ := json.Marshal(h.ledger())
		if !bytes.Equal(before, after) {
			t.Fatal("unreadable process cleared fence or changed budget")
		}
		expectedCensus := completedChecks - 1
		if expectedCensus < 0 {
			expectedCensus = 0
		}
		if censusCalls != expectedCensus {
			t.Fatal("failure did not reach the requested process checkpoint", completedChecks, censusCalls)
		}
		if _, err := os.Stat(h.d.home("entries", safeKey(keys[0]), "source-main392.json")); !os.IsNotExist(err) {
			t.Fatal("process proof failure published source migration evidence")
		}
	}
}

func TestSourceMigrationResumesUncreatedProofAfterForeignFenceRetires(t *testing.T) {
	h, keys, next := migrationHarness(t)
	before := h.ledger()
	if err := h.d.MigrateUnexecutedSource(context.Background(), keys[1], next[1]); err != nil {
		t.Fatal(err)
	}
	path := h.d.home("entries", safeKey(keys[1]), "source-main392.json")
	original, _ := os.ReadFile(path)
	var p sourceMigrationProof
	if json.Unmarshal(original, &p) != nil || p.OriginalFence != nil {
		t.Fatal("foreign fence became immutable authority for another slot")
	}
	if err := h.d.Store.Update(func(l *Ledger) error { *l = *before; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := h.d.MigrateUnexecutedSource(context.Background(), keys[0], next[0]); err != nil {
		t.Fatal(err)
	}
	if len(h.ledger().Fences) != 0 {
		t.Fatal("Rust's own fence did not retire")
	}
	h.now = h.now.Add(time.Minute)
	if err := h.d.MigrateUnexecutedSource(context.Background(), keys[1], next[1]); err != nil {
		t.Fatal("unrelated retired fence made original proof unusable", err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(original, after) || len(h.ledger().Budget.SourceMigrations) != 2 {
		t.Fatal("reused proof bytes or one-time slot relation changed")
	}
}

func TestSourceMigrationKeepsBothCollectionObservationOutputs(t *testing.T) {
	h, keys, next := migrationHarness(t)
	var paths []string
	h.d.VerifyRetired = func(_ context.Context, _ *Entry, dir string) (CollectionObservation, error) {
		paths = append(paths, dir)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return CollectionObservation{}, err
		}
		if err := os.WriteFile(filepath.Join(dir, "verification-output.json"), []byte(filepath.Base(dir)), 0600); err != nil {
			return CollectionObservation{}, err
		}
		return CollectionObservation{Unreadable: len(paths)}, nil
	}
	if err := h.d.MigrateUnexecutedSource(context.Background(), keys[0], next[0]); err != nil {
		t.Fatal(err)
	}
	if len(paths) != 2 || paths[0] == paths[1] {
		t.Fatal("before and after outputs share one path")
	}
	for _, path := range paths {
		b, err := os.ReadFile(filepath.Join(path, "verification-output.json"))
		if err != nil || string(b) != filepath.Base(path) {
			t.Fatal("collection observation overwritten")
		}
	}
}

func TestSourceMigrationRejectsCrossWorkflowAndExhaustedDeferrals(t *testing.T) {
	for _, kind := range []string{"cross-workflow", "deferrals"} {
		h, keys, next := migrationHarness(t)
		if kind == "cross-workflow" {
			next[0] = next[1]
		} else {
			_ = h.d.mutate(keys[0], func(_ *Ledger, e *Entry) { e.Queued.Deferrals = maxPrestartDeferrals })
		}
		before, _ := json.Marshal(h.ledger())
		if h.d.MigrateUnexecutedSource(context.Background(), keys[0], next[0]) == nil {
			t.Fatal("cross-workflow or exhausted deferral slot migrated")
		}
		after, _ := json.Marshal(h.ledger())
		if !bytes.Equal(before, after) {
			t.Fatal("refused migration changed ledger")
		}
	}
}

func TestOrdinaryTickRecordsSourceMigrationRequirementWithoutConsumingBudget(t *testing.T) {
	h, _, next := migrationHarness(t)
	prefix := "repos/" + Repository + "/"
	runs := h.gh.data[prefix+"actions/runs?status=queued&per_page=100&page=1"].(obj)["workflow_runs"]
	h.gh.data[prefix+"actions/runs?status=queued&per_page=30"] = obj{"workflow_runs": runs}
	h.d.Cfg.Continuation.Enabled = true
	rep := h.tick()
	if rep.BudgetBlocked != 3 || rep.Admitted != 0 || rep.Launched != 0 {
		t.Fatal("tick did not report explicit source migration wait", rep)
	}
	l := h.ledger()
	if len(l.Entries) != 3 || len(l.Budget.SourceMigrations) != 0 || len(l.Budget.Continuation) != 0 {
		t.Fatal("tick admitted migration without proof")
	}
	for _, key := range next {
		if l.Refusals[key].Reason != "source_migration_required" || l.Refusals[key].Next != stamp(h.now.Add(5*time.Minute)) {
			t.Fatal("migration wait or bounded backoff missing")
		}
	}
	if again := h.tick(); again.Admitted != 0 || again.Scan.Skipped != 3 {
		t.Fatal("same-time tick did not honor bounded refusal", again)
	}
	for _, key := range next {
		if h.ledger().Refusals[key].Count != l.Refusals[key].Count {
			t.Fatal("backoff repeated the same refusal")
		}
	}
}

func TestNewDepsWiresSourceProcessAbsenceVerifier(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.json")
	c := Config{Version: 1, Home: root, GH: writePinned(t, "gh", "fixed"), SafeDelete: writePinned(t, "safe-delete", "fixed")}
	if err := writeAtomic(path, c); err != nil {
		t.Fatal(err)
	}
	d, err := NewDeps(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.VerifyStopped == nil {
		t.Fatal("normal CLI omitted source process verifier")
	}
	if err := d.VerifyStopped(ProcID{}); err != nil {
		t.Fatal(err)
	}
}
