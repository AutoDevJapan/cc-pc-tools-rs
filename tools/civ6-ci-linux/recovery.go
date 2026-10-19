package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type recoveryStatus struct {
	Clean            bool   `json:"clean"`
	Active           bool   `json:"active"`
	NamespaceGone    bool   `json:"namespace_gone"`
	RegistrationGone bool   `json:"registration_gone"`
	LockReleased     bool   `json:"lock_released"`
	RunID            int64  `json:"run_id"`
	JobID            int64  `json:"job_id"`
	Name             string `json:"runner_name,omitempty"`
}

type recoveryOps struct {
	present  func(context.Context, namespace) (bool, error)
	inspect  func(context.Context, *namespace) ([]byte, error)
	dispose  func(*namespace) error
	runners  func(context.Context) ([]runnerEntry, error)
	delete   func(context.Context, string, string, int64) error
	lockPath string
}

func verifyReceiptOwner(ctx context.Context, dir string) error {
	abs, e := filepath.Abs(dir)
	if e != nil || abs != filepath.Clean(dir) {
		return errors.New("absolute receipt root required")
	}
	id := strings.TrimPrefix(filepath.Base(abs), "codex-owned-")
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		return errors.New("safe-delete root required")
	}
	b, e := boundedOutput(cmd(ctx, safeDelete, "plan", "--id", id), 65536)
	var p struct{ ID, Path, Action string }
	if e != nil || json.Unmarshal(b, &p) != nil || p.ID != id || !strings.EqualFold(filepath.Clean(p.Path), abs) || p.Action != "submit_to_exec_tool" {
		return errors.New("receipt ownership verification failed")
	}
	return nil
}

func readRecord(path string, out any) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Size() > 1<<20 {
		return errors.New("recovery record is not a bounded file")
	}
	return json.NewDecoder(f).Decode(out)
}

func deleteOwnedRegistration(ctx context.Context, dir, name string, id int64) error {
	if id <= 0 || !regexp.MustCompile(`^civ6-ci-linux-(rust|codeql)-[0-9]+$`).MatchString(name) {
		return errors.New("owned runner identity required")
	}
	if e := saveNew(filepath.Join(dir, "runner-delete-attempt.private.json"), map[string]any{"name": name, "id": id}); e != nil {
		return errors.New("owned registration delete already attempted; observe only")
	}
	return deleteRunner(ctx, id)
}

// Exact name lookup separates confirmed absence from a failed Docker inspect.
// No shell, daemon config or unknown sibling is consulted.
func namespacePresent(ctx context.Context, n namespace) (bool, error) {
	if !regexp.MustCompile(`^civ6-ci-linux-(rust|codeql)-[0-9]+$`).MatchString(n.Name) {
		return false, errors.New("owned namespace name rejected")
	}
	b, e := boundedOutput(cmd(ctx, dockerExe, "ps", "--all", "--no-trunc", "--filter", "name=^/"+n.Name+"$", "--format", "{{.ID}} {{.Names}}"), 4096)
	if e != nil {
		return false, errors.New("namespace absence unavailable")
	}
	if strings.TrimSpace(string(b)) == "" {
		return false, nil
	}
	parts := strings.Fields(string(b))
	if len(parts) != 2 || parts[1] != n.Name || !isHex(parts[0], 64) || (n.ID != "" && n.ID != parts[0]) {
		return false, errors.New("owned namespace inventory differs")
	}
	return true, nil
}

// recoverJob never verifies a current branch: cleanup is still required after
// source advances or closes. The original kernel flight lease prevents taking
// an active host's resources; durable phase journals prevent mutation retries.
func recoverJob(ctx context.Context, dir string, collect bool) (recoveryStatus, error) {
	var r recoveryStatus
	if e := verifyReceiptOwner(ctx, dir); e != nil {
		return r, e
	}
	return recoverUsing(ctx, dir, collect, recoveryOps{
		present: namespacePresent, inspect: func(c context.Context, n *namespace) ([]byte, error) { return n.inspect(c) }, dispose: func(n *namespace) error { return n.dispose() }, runners: repoRunners, delete: deleteOwnedRegistration,
		lockPath: filepath.Join(os.TempDir(), "civ6-ci-linux.lock"),
	})
}

func recoverUsing(ctx context.Context, dir string, collect bool, ops recoveryOps) (recoveryStatus, error) {
	var r recoveryStatus
	entries, e := os.ReadDir(dir)
	if e != nil {
		return r, e
	}
	if len(entries) == 0 {
		r.Clean, r.NamespaceGone, r.RegistrationGone, r.LockReleased = true, true, true, true
		return r, nil
	}
	lease, e := flightLease(filepath.Join(dir, "flight.lock"), false)
	if e != nil {
		r.Active = true
		return r, errors.New("original flight is active or its lease proof is absent")
	}
	defer lease.Close()
	var rec receipt
	e = readRecord(filepath.Join(dir, "intent.private.json"), &rec)
	if e != nil {
		// The only operation before intent publication creates this kernel lease.
		// No Docker/create/registration/global-lock operation can precede intent.
		if errors.Is(e, os.ErrNotExist) && len(entries) == 1 && entries[0].Name() == "flight.lock" {
			r.Clean, r.NamespaceGone, r.RegistrationGone, r.LockReleased = true, true, true, true
			return r, nil
		}
		return r, errors.New("original recovery intent unavailable")
	}
	if rec.RepoID != repoID || rec.Plan.localCheck() != nil || rec.Kind != KindRust && rec.Kind != KindCodeQL {
		return r, errors.New("original recovery plan rejected")
	}
	r.RunID, r.JobID = rec.Plan.RunID, rec.Plan.JobID
	var n namespace
	ne := readRecord(filepath.Join(dir, "namespace-intent.private.json"), &n)
	if ne != nil && !errors.Is(ne, os.ErrNotExist) {
		return r, ne
	}
	if ne == nil {
		if n.Journal != dir || n.Kind != rec.Kind {
			return r, errors.New("original namespace journal differs")
		}
		r.Name = n.Name
		exists, e := ops.present(ctx, n)
		if e != nil {
			return r, e
		}
		if exists {
			// inspect validates full isolation/ownership before any stop/removal.
			b, e := ops.inspect(ctx, &n)
			if e != nil {
				return r, e
			}
			observed, e := parseInspect(b)
			if e != nil || observed.Config.Labels["cc.run"] != strconv.FormatInt(r.RunID, 10) || observed.Config.Labels["cc.job_id"] != strconv.FormatInt(r.JobID, 10) {
				return r, errors.New("original namespace run/job differs")
			}
			if collect {
				if e = ops.dispose(&n); e != nil {
					return r, e
				}
				exists, e = ops.present(ctx, n)
				if e != nil {
					return r, e
				}
			}
		}
		r.NamespaceGone = !exists
	} else {
		r.NamespaceGone = true
	}
	var registration struct {
		Name  string
		RunID int64 `json:"run_id"`
		JobID int64 `json:"job_id"`
	}
	re := readRecord(filepath.Join(dir, "registration-intent.private.json"), &registration)
	if re != nil && !errors.Is(re, os.ErrNotExist) {
		return r, re
	}
	if re == nil {
		if n.Name == "" || registration.Name != n.Name || registration.RunID != r.RunID || registration.JobID != r.JobID {
			return r, errors.New("original registration differs")
		}
		runners, e := ops.runners(ctx)
		if e != nil {
			return r, e
		}
		found := false
		for _, runner := range runners {
			if runner.Name == n.Name {
				if found {
					return r, errors.New("unique owned runner name required")
				}
				found = true
				if collect && r.NamespaceGone {
					if e = ops.delete(ctx, dir, n.Name, runner.ID); e != nil {
						return r, e
					}
				}
			}
		}
		if collect && found && r.NamespaceGone {
			runners, e = ops.runners(ctx)
			if e != nil {
				return r, e
			}
			found = false
			for _, runner := range runners {
				if runner.Name == n.Name {
					found = true
				}
			}
		}
		r.RegistrationGone = !found
	} else {
		r.RegistrationGone = true
	}
	lockPath := ops.lockPath
	b, e := os.ReadFile(lockPath)
	if e != nil && !errors.Is(e, os.ErrNotExist) {
		return r, e
	}
	want := fmt.Sprintf("%d\n%d\n%s\n", r.RunID, r.JobID, dir)
	ours := string(b) == want
	if e == nil && !ours {
		// Another root's lock belongs to another operation and is never removed.
		r.LockReleased = true
	} else if errors.Is(e, os.ErrNotExist) {
		r.LockReleased = true
	} else if collect && r.NamespaceGone && r.RegistrationGone {
		if e = os.Remove(lockPath); e != nil {
			return r, e
		}
		r.LockReleased = true
	}
	r.Clean = r.NamespaceGone && r.RegistrationGone && r.LockReleased
	return r, nil
}
