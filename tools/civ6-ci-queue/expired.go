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
	"time"
)

type expiredJob struct {
	ID          int64             `json:"id"`
	RunID       int64             `json:"run_id"`
	Attempt     int               `json:"run_attempt"`
	Head        string            `json:"head_sha"`
	Name        string            `json:"name"`
	Status      string            `json:"status"`
	Conclusion  string            `json:"conclusion"`
	RunnerID    *int64            `json:"runner_id"`
	RunnerName  *string           `json:"runner_name"`
	Steps       []json.RawMessage `json:"steps"`
	Labels      []string          `json:"labels"`
	StartedAt   string            `json:"started_at"`
	CompletedAt string            `json:"completed_at"`
}

func (j expiredJob) matches(p Plan) bool {
	return j.ID == p.JobID && j.RunID == p.RunID && j.Attempt == p.Attempt && j.Head == p.Head && j.Name == p.JobName &&
		j.RunnerID != nil && *j.RunnerID == 0 && j.RunnerName != nil && *j.RunnerName == "" && j.Steps != nil && len(j.Steps) == 0
}

type expiredProof struct {
	Original    *Entry                `json:"original_entry"`
	Replacement Plan                  `json:"replacement_plan"`
	Cancelled   expiredJob            `json:"cancelled_job"`
	Retired     *CleanupRec           `json:"retired_resource,omitempty"`
	Census      CollectionObservation `json:"process_census"`
	At          string                `json:"verified_at"`
}

// ReplaceExpired is an explicit local admission, never a GitHub rerun. The
// caller separately requests exactly the next official attempt after repairing
// the supply path. A never-assigned, timeout-cancelled initial PR149 job may use
// its existing slot once. The old entry, result and deadlines are archived;
// neither the acceptance budget nor the old prestart limit is reset.
func (d *Deps) ReplaceExpired(ctx context.Context, key, replacementKey string) error {
	if ctx.Err() != nil {
		return ctx.Err()
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
	if ent == nil || ent.Key != key || ent.Plan.Key() != key || ent.OS != "Windows" || ent.Phase != "initial" || ent.Plan.PR != OriginalPR ||
		ent.Plan.Event != "pull_request" || ent.Plan.Attempt != 1 || ent.Replaces != "" ||
		(ent.State != StateNeedsRecovery && ent.State != StateReserved) || ent.Official.Success ||
		d.Alive(ent.Started.Supervisor) || d.Alive(ent.Started.Helper) ||
		!slices.Contains(l.Budget.Initial, key) || len(l.Budget.Initial) > InitialBudget || len(l.Budget.Replacements) >= InitialBudget {
		return errors.New("entry is not an original unassigned initial acceptance slot")
	}
	if f, ok := l.Fences["Windows"]; ok && f.Key != key {
		return errors.New("another Windows entry is fenced")
	}
	for _, r := range l.Budget.Replacements {
		if r.Original == key || r.Replacement == replacementKey {
			return errors.New("original slot already has a replacement")
		}
	}
	if l.Entries[replacementKey] != nil {
		return errors.New("replacement already admitted")
	}
	queued, err := time.Parse(time.RFC3339Nano, ent.Queued.At)
	if err != nil || d.Now().Before(queued.Add(24*time.Hour)) {
		return errors.New("original acceptance has not expired")
	}
	var run, job int64
	var attempt int
	if !keyPattern.MatchString(replacementKey) {
		return errors.New("replacement key invalid")
	}
	if n, _ := fmt.Sscanf(replacementKey, "%d/%d/%d", &run, &attempt, &job); n != 3 || run != ent.Plan.RunID || attempt != 2 || job <= 0 || job == ent.Plan.JobID {
		return errors.New("only the next attempt of the original run can replace it")
	}
	next := ent.Plan
	next.Attempt, next.JobID = attempt, job
	if next.Key() != replacementKey {
		return errors.New("replacement key is not canonical")
	}
	if err = d.C.Validate(next); err != nil {
		return err
	}
	var cancelled expiredJob
	check := func() error {
		r := d.Reader()
		r.MaxCalls = 120
		if err := d.C.Verify(ctx, next, r.API()); err != nil {
			return err
		}
		cancelled = expiredJob{}
		if err := r.Read(ctx, fmt.Sprintf("repos/%s/actions/jobs/%d", Repository, ent.Plan.JobID), &cancelled); err != nil {
			return err
		}
		started, se := time.Parse(time.RFC3339Nano, cancelled.StartedAt)
		ended, ee := time.Parse(time.RFC3339Nano, cancelled.CompletedAt)
		wantLabels, gotLabels := slices.Clone(d.C.Labels(ent.Plan)), slices.Clone(cancelled.Labels)
		slices.Sort(wantLabels)
		slices.Sort(gotLabels)
		if !cancelled.matches(ent.Plan) || cancelled.Status != "completed" || cancelled.Conclusion != "cancelled" ||
			se != nil || ee != nil || ended.Before(started.Add(24*time.Hour)) || ended.After(d.Now()) || !slices.Equal(wantLabels, gotLabels) {
			return errors.New("original official job was not timeout-cancelled without assignment")
		}
		var current expiredJob
		if err := r.Read(ctx, fmt.Sprintf("repos/%s/actions/jobs/%d", Repository, next.JobID), &current); err != nil {
			return err
		}
		wantLabels, gotLabels = slices.Clone(d.C.Labels(next)), slices.Clone(current.Labels)
		slices.Sort(wantLabels)
		slices.Sort(gotLabels)
		if !current.matches(next) || current.Status != "queued" || !slices.Equal(wantLabels, gotLabels) {
			return errors.New("replacement is not the same queued unassigned job")
		}
		return nil
	}
	if err = check(); err != nil {
		return err
	}
	dir := d.home("entries", safeKey(key))
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	proof := expiredProof{Original: ent, Replacement: next, Cancelled: cancelled, At: stamp(d.Now())}
	if ent.Cleanup == (CleanupRec{State: "none"}) && ent.Started == (StartRec{}) {
		// The stored create_intent always precedes creation: this entry created nothing.
	} else {
		cl := ent.Cleanup
		if !hex32.MatchString(cl.SafeDeleteID) || cl.Instance != cl.SafeDeleteID || !absClean(cl.Root) || filepath.Base(cl.Root) != "codex-owned-"+cl.Instance || d.Retired == nil || d.VerifyRetired == nil {
			return errors.New("original resource identity or collection proof unavailable")
		}
		if cl, err = d.Retired(cl, key); err != nil {
			return err
		}
		if proof.Census, err = d.VerifyRetired(ctx, ent, dir); err != nil {
			return err
		}
		if proof.Census.Queryable != 0 || proof.Census.Unreadable < 0 {
			return errors.New("original account has live or unknown queryable processes")
		}
		if _, err = d.Retired(cl, key); err != nil {
			return err
		}
		proof.Retired = &cl
	}
	// Mutable official data are observed again after the local collection proof.
	if err = check(); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	path := filepath.Join(dir, "replacement-2.json")
	body, err := saveExpiredProof(path, proof)
	if err != nil {
		return err
	}
	return d.Store.Update(func(current *Ledger) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		before, _ := json.Marshal(l)
		after, _ := json.Marshal(current)
		if !bytes.Equal(before, after) {
			return errors.New("acceptance ledger changed during proof")
		}
		// Archive first, then commit one relation and a new entry atomically. An
		// interrupted command can reuse only its identical original proof.
		current.Budget.Replacements = append(current.Budget.Replacements, ReplacementRec{Original: key, Replacement: replacementKey, ProofSHA256: digestHexBytes(body)})
		current.Entries[key].State = StateRefused
		current.Entries[key].Reason = "expired without assignment; replacement: " + replacementKey
		current.Entries[replacementKey] = &Entry{Key: replacementKey, Plan: next, OS: "Windows", Phase: "initial", State: StateReserved,
			Queued: QueuedRec{At: stamp(d.Now())}, Cleanup: CleanupRec{State: "none"}, Replaces: key}
		delete(current.Fences, "Windows")
		delete(current.Refusals, replacementKey)
		return nil
	})
}

func saveExpiredProof(path string, p expiredProof) ([]byte, error) {
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		old, err := boundedRegular(path, 256<<10)
		if err != nil {
			return nil, err
		}
		var previous expiredProof
		if json.Unmarshal(old, &previous) != nil {
			return nil, errors.New("existing replacement proof invalid")
		}
		// Keep the first observation time. Every current external proof has just
		// been rechecked; the saved original cannot be changed by a retry.
		p.At = previous.At
		want, _ := json.Marshal(p)
		if !bytes.Equal(old, want) {
			return nil, errors.New("existing replacement proof differs")
		}
		return old, nil
	}
	if err != nil {
		return nil, err
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	if ce := f.Close(); err == nil {
		err = ce
	}
	return b, err
}
