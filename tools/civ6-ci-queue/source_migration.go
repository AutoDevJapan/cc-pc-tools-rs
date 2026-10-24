package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	contract "cc/civ6-ci-contract"
)

// These three original reservations are the entire transition surface.
var originalSourceSlots = map[string]string{
	"39423559387/1/118630300541": ".github/workflows/rust.yml",
	"39423559434/1/118630300375": ".github/workflows/live-chain.yml",
	"39423559440/1/118630301061": ".github/workflows/native-driver.yml",
}

type sourceMigrationProof struct {
	Version        int                   `json:"version"`
	Generation     string                `json:"generation"`
	Original       *Entry                `json:"original_entry"`
	OriginalFence  *Fence                `json:"original_windows_fence,omitempty"`
	Replacement    Plan                  `json:"replacement_plan"`
	Cancelled      expiredJob            `json:"cancelled_job"`
	ReplacementJob expiredJob            `json:"replacement_job"`
	Retired        *CleanupRec           `json:"retired_resource,omitempty"`
	Census         CollectionObservation `json:"process_census_before"`
	CensusAfter    CollectionObservation `json:"process_census_after"`
	OriginalResult []byte                `json:"original_result_bytes,omitempty"`
	NeverCreated   bool                  `json:"never_created"`
	At             string                `json:"verified_at"`
}

// MigrateUnexecutedSource observes an already-created official attempt1. It
// never publishes, reruns, registers, executes, collects, or resets a budget.
func (d *Deps) MigrateUnexecutedSource(ctx context.Context, key, replacementKey string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock, err := tryLock(d.home("tick.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	unlockOS, err := d.LockOS("Windows")
	if err != nil {
		return err
	}
	defer unlockOS()
	l, err := d.Store.View()
	if err != nil {
		return err
	}
	ent := l.Entries[key]
	wpath, fixed := originalSourceSlots[key]
	if !fixed || ent == nil || ent.Key != key || ent.Plan.Key() != key || ent.OS != "Windows" || ent.Phase != "initial" ||
		ent.Plan.Workflow != wpath || ent.Plan.PR != OriginalPR || ent.Plan.Event != "pull_request" || ent.Plan.Attempt != 1 ||
		ent.Replaces != "" || ent.SourceReplaces != "" || ent.Official.Success ||
		(ent.State != StateReserved && ent.State != StateNeedsRecovery) ||
		d.Alive(ent.Started.Supervisor) || d.Alive(ent.Started.Helper) ||
		len(l.Budget.Initial) != InitialBudget || len(l.Budget.SourceMigrations) >= InitialBudget ||
		ent.Queued.Deferrals < 0 || ent.Queued.Deferrals >= maxPrestartDeferrals {
		return errors.New("not one of the finite unexecuted original source slots")
	}
	seen := map[string]bool{}
	for _, k := range l.Budget.Initial {
		path, ok := originalSourceSlots[k]
		original := l.Entries[k]
		if !ok || seen[k] || original == nil || original.Key != k || original.Plan.Key() != k || original.Plan.Head != contract.OldRoutedHead || original.Plan.Tree != contract.OldRoutedTree || original.Plan.Workflow != path || original.Phase != "initial" || original.Replaces != "" || original.SourceReplaces != "" {
			return errors.New("original initial budget differs")
		}
		seen[k] = true
	}
	for _, r := range l.Budget.Replacements {
		if r.Original == key || r.Replacement == replacementKey {
			return errors.New("expired replacement already owns the slot")
		}
	}
	for _, r := range l.Budget.SourceMigrations {
		if r.Original == key || r.Replacement == replacementKey {
			return errors.New("source slot already migrated")
		}
	}
	if l.Entries[replacementKey] != nil || !keyPattern.MatchString(replacementKey) {
		return errors.New("new source key invalid or already admitted")
	}
	queued, qe := time.Parse(time.RFC3339Nano, ent.Queued.At)
	if qe != nil || d.Now().Before(queued.Add(24*time.Hour)) {
		return errors.New("original source reservation has not expired")
	}
	if f, ok := l.Fences["Windows"]; ok && f.Key != key && !neverCreatedSource(ent) {
		return errors.New("another Windows resource is fenced")
	}
	var runID, jobID int64
	var attempt int
	if n, _ := fmt.Sscanf(replacementKey, "%d/%d/%d", &runID, &attempt, &jobID); n != 3 || attempt != 1 || runID <= 0 || jobID <= 0 || runID == ent.Plan.RunID || jobID == ent.Plan.JobID {
		return errors.New("new source must be a distinct run at attempt1")
	}
	var next Plan
	var cancelled expiredJob
	var replacementJob expiredJob
	check := func() error {
		r := d.Reader()
		r.MaxCalls = 160
		if err := contract.VerifyHistoricalInitial(ctx, cp(ent.Plan), contract.ReadAPI(r.API())); err != nil {
			return err
		}
		var run apiRun
		if err := r.Read(ctx, fmt.Sprintf("repos/%s/actions/runs/%d", Repository, runID), &run); err != nil {
			return err
		}
		rawPath := run.Path
		run.Path, _, _ = strings.Cut(run.Path, "@")
		if run.ID != runID || run.Attempt != 1 || run.Path != wpath || !contract.MatchRunPath(cp(ent.Plan), rawPath) || run.Event != "pull_request" || run.Status != "queued" || run.HeadBranch != contract.OriginalBranch {
			return errors.New("new source official run differs")
		}
		scanner := Scanner{Read: r.API()}
		p, err := scanner.basePlan(ctx, run, 1)
		if err != nil {
			return err
		}
		p.JobID, p.JobName = jobID, ent.Plan.JobName
		if p.Key() != replacementKey || p.PR != OriginalPR || p.Head == ent.Plan.Head || p.Workflow != wpath || p.Job != ent.Plan.Job || p.JobName != ent.Plan.JobName || cp(p).OS() != "Windows" {
			return errors.New("new source job correspondence differs")
		}
		if err = contract.VerifyMergeSource(ctx, p.Head, p.Tree, contract.ReadAPI(r.API())); err != nil {
			return err
		}
		if err = d.C.Verify(ctx, p, r.API()); err != nil {
			return err
		}
		if next.Key() != "0/0/0" && next != p {
			return errors.New("replacement source changed during proof")
		}
		next = p
		var observedOld expiredJob
		if err = r.Read(ctx, fmt.Sprintf("repos/%s/actions/jobs/%d", Repository, ent.Plan.JobID), &observedOld); err != nil {
			return err
		}
		started, se := time.Parse(time.RFC3339Nano, observedOld.StartedAt)
		ended, ee := time.Parse(time.RFC3339Nano, observedOld.CompletedAt)
		if cancelled.ID != 0 {
			a, _ := json.Marshal(cancelled)
			b, _ := json.Marshal(observedOld)
			if !bytes.Equal(a, b) {
				return errors.New("historical cancellation changed during proof")
			}
		}
		cancelled = observedOld
		if !cancelled.matches(ent.Plan) || !cancelled.cancelledUnassigned() || cancelled.Status != "completed" || cancelled.Conclusion != "cancelled" ||
			se != nil || ee != nil || ended.Before(started.Add(24*time.Hour)) || ended.After(d.Now()) || !migrationLabels(ent.Plan, cancelled.Labels) {
			return errors.New("historical source was not timeout cancelled without assignment")
		}
		var current expiredJob
		if err = r.Read(ctx, fmt.Sprintf("repos/%s/actions/jobs/%d", Repository, jobID), &current); err != nil {
			return err
		}
		if !current.matches(next) || !current.queuedUnassigned() || current.Status != "queued" || current.Conclusion != "" || !migrationLabels(next, current.Labels) {
			return errors.New("new source job assigned, started or differs")
		}
		if replacementJob.ID != 0 {
			a, _ := json.Marshal(replacementJob)
			b, _ := json.Marshal(current)
			if !bytes.Equal(a, b) {
				return errors.New("new source assignment observation changed")
			}
		}
		replacementJob = current
		return nil
	}
	if err = check(); err != nil {
		return err
	}
	dir := d.home("entries", safeKey(key))
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	proof := sourceMigrationProof{Version: 1, Generation: contract.MergeGeneration, Original: ent, Replacement: next, Cancelled: cancelled, ReplacementJob: replacementJob, NeverCreated: neverCreatedSource(ent), At: stamp(d.Now())}
	if f, ok := l.Fences["Windows"]; ok {
		proof.OriginalFence = &f
	}
	// Keep any original result bytes, including a supervisor that stopped before
	// create_intent. A created resource requires its actual failure result.
	resultPath := d.resultPath(key)
	if _, se := os.Lstat(resultPath); se == nil {
		b, re := boundedRegular(resultPath, 256<<10)
		if re != nil {
			return re
		}
		var result Result
		if json.Unmarshal(b, &result) != nil || result.Key != key || result.Official.Success || result.State != StateNeedsRecovery || result.Cleanup != ent.Cleanup {
			return errors.New("original source result differs")
		}
		proof.OriginalResult = b
	} else if !os.IsNotExist(se) || !proof.NeverCreated {
		return errors.New("original source failure result unavailable")
	}
	verifyLocal := func() (CollectionObservation, error) {
		if d.Alive(ent.Started.Supervisor) || d.Alive(ent.Started.Helper) {
			return CollectionObservation{}, errors.New("old source process is live or uncertain")
		}
		if proof.NeverCreated {
			return CollectionObservation{}, nil
		}
		cl := ent.Cleanup
		if !hex32.MatchString(cl.SafeDeleteID) || cl.Instance != cl.SafeDeleteID || !absClean(cl.Root) || filepath.Base(cl.Root) != "codex-owned-"+cl.Instance || d.Retired == nil || d.VerifyRetired == nil {
			return CollectionObservation{}, errors.New("old source resource identity unavailable")
		}
		retired, err := d.Retired(cl, key)
		if err != nil {
			return CollectionObservation{}, err
		}
		obs, err := d.VerifyRetired(ctx, ent, dir)
		if err != nil {
			return obs, err
		}
		if obs.Queryable != 0 || obs.Unreadable < 0 || obs.Unreadable > 1<<20 {
			return obs, errors.New("old source process census invalid")
		}
		again, err := d.Retired(cl, key)
		if err != nil || retired != again {
			return obs, errors.New("old source retirement changed")
		}
		if proof.Retired != nil && *proof.Retired != retired {
			return obs, errors.New("old source retirement proof changed")
		}
		proof.Retired = &retired
		return obs, nil
	}
	if proof.Census, err = verifyLocal(); err != nil {
		return err
	}
	if err = check(); err != nil {
		return err
	}
	if proof.CensusAfter, err = verifyLocal(); err != nil {
		return err
	}
	if err = check(); err != nil {
		return err
	}
	if proof.OriginalResult != nil {
		b, re := boundedRegular(resultPath, 256<<10)
		if re != nil || !bytes.Equal(b, proof.OriginalResult) {
			return errors.New("original result changed during source proof")
		}
	} else if _, re := os.Lstat(resultPath); !os.IsNotExist(re) {
		return errors.New("original result appeared during source proof")
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	body, err := saveSourceMigrationProof(filepath.Join(dir, "source-main392.json"), proof)
	if err != nil {
		return err
	}
	var durable sourceMigrationProof
	if json.Unmarshal(body, &durable) != nil {
		return errors.New("durable source proof cannot be read")
	}
	return d.Store.Update(func(current *Ledger) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		before, _ := json.Marshal(l)
		after, _ := json.Marshal(current)
		if !bytes.Equal(before, after) {
			return errors.New("acceptance ledger changed during source proof")
		}
		current.Budget.SourceMigrations = append(current.Budget.SourceMigrations, SourceMigrationRec{key, replacementKey, contract.MergeGeneration, digestHexBytes(body)})
		current.Entries[key].State = StateRefused
		current.Entries[replacementKey] = &Entry{Key: replacementKey, Plan: next, OS: "Windows", Phase: "initial", State: StateReserved,
			Queued: QueuedRec{At: durable.At, Deferrals: ent.Queued.Deferrals}, Cleanup: CleanupRec{State: "none"}, SourceReplaces: key}
		if f, ok := current.Fences["Windows"]; ok && f.Key == key {
			delete(current.Fences, "Windows")
		}
		delete(current.Refusals, replacementKey)
		return nil
	})
}

func neverCreatedSource(e *Entry) bool {
	if !neverCreated(e) {
		return false
	}
	for _, previous := range e.Prestarts {
		if previous.Cleanup != nil && *previous.Cleanup != (CleanupRec{State: "none"}) {
			return false
		}
	}
	return true
}

func migrationLabels(p Plan, labels []string) bool {
	a, b := contract.Labels(cp(p)), slices.Clone(labels)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}

func saveSourceMigrationProof(path string, p sourceMigrationProof) ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if len(b) > 512<<10 {
		return nil, errors.New("source migration proof exceeds finite limit")
	}
	reuse := func() ([]byte, error) {
		old, err := boundedRegular(path, 512<<10)
		if err != nil {
			return nil, err
		}
		var previous sourceMigrationProof
		if json.Unmarshal(old, &previous) != nil || previous.Version != 1 || previous.Generation != contract.MergeGeneration || previous.Census.Queryable != 0 || previous.CensusAfter.Queryable != 0 || previous.Census.Unreadable < 0 || previous.CensusAfter.Unreadable < 0 || previous.Census.Unreadable > 1<<20 || previous.CensusAfter.Unreadable > 1<<20 {
			return nil, errors.New("original source migration proof invalid")
		}
		then, te := time.Parse(time.RFC3339Nano, previous.At)
		now, ne := time.Parse(time.RFC3339Nano, p.At)
		if te != nil || ne != nil || then.After(now) {
			return nil, errors.New("original source proof time invalid")
		}
		p.At, p.Census, p.CensusAfter = previous.At, previous.Census, previous.CensusAfter
		want, _ := json.Marshal(p)
		if !bytes.Equal(old, want) {
			return nil, errors.New("original source migration proof differs")
		}
		return old, nil
	}
	if _, err := os.Lstat(path); err == nil {
		return reuse()
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".source-migration-*.tmp")
	if err != nil {
		return nil, err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if ce := f.Close(); err == nil {
		err = ce
	}
	if err == nil {
		err = publishFile(f.Name(), path)
		if os.IsExist(err) {
			return reuse()
		}
	}
	return b, err
}
