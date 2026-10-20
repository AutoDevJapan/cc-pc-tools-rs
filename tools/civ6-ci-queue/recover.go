package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func boundedRegular(path string, n int64) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > n {
		return nil, errors.New("recovery file unavailable or irregular")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, n+1))
	if err != nil || int64(len(b)) > n {
		return nil, errors.New("recovery file size bound")
	}
	return b, nil
}

// ResumeUnassigned is an explicit read-only proof check followed by a local
// queue transition. It does NOT collect, delete, register, rerun or mark success.
// The operator first completes the normal collector and safe-delete lifecycle.
func (d *Deps) ResumeUnassigned(ctx context.Context, key string) error {
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
	if ent == nil || ent.OS != "Windows" || ent.State != StateNeedsRecovery || l.Fences["Windows"].Key != key ||
		ent.Queued.Deferrals >= maxPrestartDeferrals-1 || prestartLimitReached(ent, d.Now()) ||
		d.Alive(ent.Started.Supervisor) || d.Alive(ent.Started.Helper) || ent.Official.Success {
		return errors.New("entry is not an eligible stopped, fenced, finite recovery")
	}
	cl := ent.Cleanup
	if !hex32.MatchString(cl.SafeDeleteID) || cl.Instance != cl.SafeDeleteID || !absClean(cl.Root) || filepath.Base(cl.Root) != "codex-owned-"+cl.Instance {
		return errors.New("recovery root identity differs")
	}
	if d.Retired == nil || d.VerifyRetired == nil {
		return errors.New("recovery proof wiring unavailable")
	}
	if cl, err = d.Retired(cl, key); err != nil {
		return err
	}
	// Each observation gets a fresh reader: mutable GitHub responses cached for
	// the first check must not stand in for the check after local OS verification.
	check := func() error {
		r := d.Reader()
		r.MaxCalls = 120
		if err := d.C.Verify(ctx, ent.Plan, r.API()); err != nil {
			return err
		}
		var j struct {
			ID         int64             `json:"id"`
			RunID      int64             `json:"run_id"`
			Attempt    int               `json:"run_attempt"`
			Head       string            `json:"head_sha"`
			Status     string            `json:"status"`
			RunnerID   int64             `json:"runner_id"`
			RunnerName string            `json:"runner_name"`
			Steps      []json.RawMessage `json:"steps"`
		}
		if err := r.Read(ctx, fmt.Sprintf("repos/%s/actions/jobs/%d", Repository, ent.Plan.JobID), &j); err != nil {
			return err
		}
		if j.ID != ent.Plan.JobID || j.RunID != ent.Plan.RunID || j.Attempt != ent.Plan.Attempt || j.Head != ent.Plan.Head ||
			j.Status != "queued" || j.RunnerID != 0 || j.RunnerName != "" || len(j.Steps) != 0 {
			return errors.New("official job was assigned, started, completed or differs")
		}
		return nil
	}
	if err = check(); err != nil {
		return err
	}
	dir := d.home("entries", safeKey(key), fmt.Sprintf("recovery-%d", ent.Queued.Deferrals))
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err = d.VerifyRetired(ctx, ent, dir); err != nil {
		return err
	}
	if err = check(); err != nil {
		return err
	}
	if _, err = d.Retired(cl, key); err != nil {
		return err
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Preserve the original result atomically. If interrupted after the rename,
	// the same command reads this archive and re-verifies all external proofs.
	archive := filepath.Join(dir, "original-result.json")
	source := d.resultPath(key)
	if _, e := os.Lstat(source); os.IsNotExist(e) {
		source = archive
	} else if e != nil {
		return e
	}
	b, err := boundedRegular(source, 256<<10)
	if err != nil {
		return err
	}
	var old Result
	if json.Unmarshal(b, &old) != nil || old.Key != key || old.State != StateNeedsRecovery || old.Cleanup.Root != cl.Root ||
		old.Cleanup.Instance != cl.Instance || old.Cleanup.SafeDeleteID != cl.SafeDeleteID || old.Official.Success {
		return errors.New("original failure result differs")
	}
	proof := map[string]any{"entry": key, "at": stamp(d.Now()), "original_result_sha256": digestHexBytes(b), "cleanup": cl, "official_success": false, "official_unassigned": true}
	if err = writeAtomic(filepath.Join(dir, "proof.json"), proof); err != nil {
		return err
	}
	proofBytes, err := boundedRegular(filepath.Join(dir, "proof.json"), 256<<10)
	if err != nil {
		return err
	}
	cl.Evidence, cl.ManifestSHA = dir, digestHexBytes(proofBytes)
	if source != archive {
		if _, e := os.Lstat(archive); !os.IsNotExist(e) {
			return errors.New("recovery archive already exists or is uncertain")
		}
		if err = os.Rename(source, archive); err != nil {
			return err
		}
	}
	// Tick and OS locks remain held until this atomic ledger commit. A repeated
	// command cannot add a second deferral, reservation or execution attempt.
	return d.Store.Update(func(current *Ledger) error {
		e := current.Entries[key]
		before, _ := json.Marshal(ent)
		after, _ := json.Marshal(e)
		if !bytes.Equal(before, after) || current.Fences["Windows"].Key != key {
			return errors.New("recovery entry changed")
		}
		e.Queued.Deferrals++
		e.Queued.RetryAt = stamp(d.Now())
		e.Prestarts = append(e.Prestarts, PrestartRec{At: stamp(d.Now()), Reason: "operator verified collection, retirement and still-unassigned official job", Cleanup: &cl})
		e.State, e.Started, e.Cleanup, e.Reason = StateReserved, StartRec{}, cl, "verified unassigned resource recovered"
		e.Official = OfficialRec{CheckedAt: stamp(d.Now()), Reason: "queued and unassigned; no test result claimed"}
		delete(current.Fences, "Windows")
		return nil
	})
}

func digestHexBytes(b []byte) string                 { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func numberField(v map[string]any, key string) int64 { n, _ := v[key].(float64); return int64(n) }

func (d *Deps) verifyRetiredWindows(ctx context.Context, e *Entry, dir string) error {
	h := d.Cfg.Helpers["Windows"]
	p := windowsPlan{Plan: e.Plan, Instance: e.Cleanup.Instance, Root: e.Cleanup.Root, ControllerSID: h.ControllerSID, RunnerVersion: h.RunnerVersion, RunnerDigest: h.RunnerDigest, Seconds: h.Seconds, CleanupSeconds: h.CleanupSeconds, ExecuteBefore: 1}
	path := filepath.Join(dir, "verification-plan.json")
	if err := writeAtomic(path, p); err != nil {
		return err
	}
	out, err := d.Exec(ctx, h.Exe, []string{"verify-collected", path}, nil, nil)
	if logErr := writeAtomic(filepath.Join(dir, "verification-output.json"), out); logErr != nil {
		return logErr
	}
	if err != nil || out.Exit != 0 {
		return errors.New("privileged collection or idle service not verified")
	}
	v := lastEvent(out.Output, "collection-verified")
	if v == nil || !boolField(v, "complete") || !boolField(v, "root_retired") || !boolField(v, "shared_service_idle") ||
		v["instance"] != e.Cleanup.Instance || numberField(v, "run_id") != e.Plan.RunID || numberField(v, "job_id") != e.Plan.JobID || numberField(v, "attempt") != int64(e.Plan.Attempt) {
		return errors.New("collection verification identity differs")
	}
	return nil
}
