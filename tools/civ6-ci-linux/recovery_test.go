package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestFlightLeaseRefusesLiveOwnerAndRecoversAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flight.lock")
	first, e := flightLease(path, true)
	if e != nil {
		t.Fatal(e)
	}
	if second, e := flightLease(path, false); e == nil {
		second.Close()
		first.Close()
		t.Fatal("active flight lease was stolen")
	}
	first.Close()
	next, e := flightLease(path, false)
	if e != nil {
		t.Fatal(e)
	}
	next.Close()
}

func TestReceiptAndWorkerLockNormalizeEquivalentRootSpellings(t *testing.T) {
	dir := t.TempDir()
	alias := dir + string(filepath.Separator) + "."
	if runtime.GOOS == "windows" {
		alias = strings.ToLower(filepath.ToSlash(dir))
	}
	if !sameReceiptPath(dir, alias) {
		t.Fatal("equivalent owned root rejected")
	}
	b := []byte(fmt.Sprintf("7\n71\n%s\n", alias))
	ours, other, e := classifyWorkerLock(b, 7, 71, dir)
	if e != nil || !ours || other {
		t.Fatal("equivalent root was mistaken for another flight")
	}
	for _, data := range []string{"", "7\n", "7\n71\n", "7\n71\nrelative\n", "7\n71\n" + dir, "7\n71\n" + dir + "\nextra\n"} {
		if _, _, e := classifyWorkerLock([]byte(data), 7, 71, dir); e == nil {
			t.Fatal("partial or ambiguous lock accepted")
		}
	}
	otherDir := filepath.Join(dir, "other")
	ours, other, e = classifyWorkerLock([]byte(fmt.Sprintf("8\n72\n%s\n", otherDir)), 7, 71, dir)
	if e != nil || ours || !other {
		t.Fatal("complete different flight not distinguished")
	}
}

func TestCancelledRegistrationDeleteDoesNotConsumeAttempt(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if deleteOwnedRegistration(ctx, dir, "civ6-ci-linux-rust-123", 7) == nil {
		t.Fatal("cancelled delete admitted")
	}
	if _, e := os.Stat(filepath.Join(dir, "runner-delete-attempt.private.json")); !os.IsNotExist(e) {
		t.Fatal("cancelled context consumed delete attempt")
	}
}

func TestRecoveryEquivalentJournalAndLockPathActuallyReleasesOwnLock(t *testing.T) {
	dir, p, n, ops := recoveryFixture(t)
	alias := dir + string(filepath.Separator) + "."
	if runtime.GOOS == "windows" {
		alias = strings.ToLower(filepath.ToSlash(dir))
	}
	n.Journal = alias
	b, _ := json.Marshal(n)
	if e := os.WriteFile(filepath.Join(dir, "namespace-intent.private.json"), b, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(ops.lockPath, []byte(fmt.Sprintf("%d\n%d\n%s\n", p.RunID, p.JobID, alias)), 0600); e != nil {
		t.Fatal(e)
	}
	ops.present = func(context.Context, namespace) (bool, error) { return false, nil }
	ops.runners = func(context.Context) ([]runnerEntry, error) { return nil, nil }
	r, e := recoverUsing(context.Background(), dir, true, ops)
	if e != nil || !r.Clean || !r.LockReleased {
		t.Fatalf("equivalent spelling fenced original resources: %+v %v", r, e)
	}
	if _, e = os.Stat(ops.lockPath); !os.IsNotExist(e) {
		t.Fatal("original worker lock still blocks the next event")
	}
}

func TestRegistrationDeleteIsOneAttemptEvenAfterFailure(t *testing.T) {
	dir := t.TempDir()
	old := mutateOverride
	defer func() { mutateOverride = old }()
	calls := 0
	mutateOverride = func(context.Context, string, string, any) error { calls++; return errors.New("synthetic refusal") }
	for i := 0; i < 2; i++ {
		if deleteOwnedRegistration(context.Background(), dir, "civ6-ci-linux-rust-123", 7) == nil {
			t.Fatal("failed deletion certified success")
		}
	}
	if calls != 1 {
		t.Fatal("refused registration deletion retried")
	}
}

func recoveryFixture(t *testing.T) (string, Plan, namespace, recoveryOps) {
	t.Helper()
	dir := t.TempDir()
	p := rustPlan()
	n := namespace{Name: "civ6-ci-linux-rust-123", Kind: KindRust, Journal: dir, ID: rep("a", 64)}
	f, e := flightLease(filepath.Join(dir, "flight.lock"), true)
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
	if e = saveNew(filepath.Join(dir, "intent.private.json"), receipt{RepoID: repoID, Plan: p, Kind: KindRust}); e != nil {
		t.Fatal(e)
	}
	if e = saveNew(filepath.Join(dir, "namespace-intent.private.json"), n); e != nil {
		t.Fatal(e)
	}
	if e = saveNew(filepath.Join(dir, "registration-intent.private.json"), map[string]any{"name": n.Name, "run_id": p.RunID, "job_id": p.JobID}); e != nil {
		t.Fatal(e)
	}
	ops := recoveryOps{lockPath: filepath.Join(dir, "global.lock")}
	if e = os.WriteFile(ops.lockPath, []byte(fmt.Sprintf("%d\n%d\n%s\n", p.RunID, p.JobID, dir)), 0600); e != nil {
		t.Fatal(e)
	}
	return dir, p, n, ops
}

func TestRecoveryIsReadOnlyUntilCollectAndNeverNeedsCurrentBranch(t *testing.T) {
	dir, p, n, ops := recoveryFixture(t)
	present, registered := true, true
	stops, deletes := 0, 0
	ops.present = func(context.Context, namespace) (bool, error) { return present, nil }
	ops.inspect = func(context.Context, *namespace) ([]byte, error) {
		return json.Marshal([]map[string]any{{"Config": map[string]any{"Labels": map[string]string{"cc.run": strconv.FormatInt(p.RunID, 10), "cc.job_id": strconv.FormatInt(p.JobID, 10)}}}})
	}
	ops.dispose = func(ns *namespace) error {
		if ns.ID != n.ID {
			t.Fatal("wrong namespace")
		}
		stops++
		present = false
		return nil
	}
	ops.runners = func(context.Context) ([]runnerEntry, error) {
		if registered {
			return []runnerEntry{{Name: n.Name, ID: 7}}, nil
		}
		return nil, nil
	}
	ops.delete = func(_ context.Context, root, name string, id int64) error {
		if root != dir || name != n.Name || id != 7 {
			t.Fatal("wrong registration")
		}
		deletes++
		registered = false
		return nil
	}
	status, e := recoverUsing(context.Background(), dir, false, ops)
	if e != nil || status.Clean || stops != 0 || deletes != 0 {
		t.Fatalf("status mutated resources or certified clean: %+v %v", status, e)
	}
	status, e = recoverUsing(context.Background(), dir, true, ops)
	if e != nil || !status.Clean || stops != 1 || deletes != 1 {
		t.Fatalf("owned recovery incomplete: %+v %v", status, e)
	}
	status, e = recoverUsing(context.Background(), dir, true, ops)
	if e != nil || !status.Clean || stops != 1 || deletes != 1 {
		t.Fatal("clean recovery repeated a mutation")
	}
}

func TestRecoveryCannotReleaseLockAfterUnknownOrFailedNamespaceStop(t *testing.T) {
	for _, mode := range []string{"inventory-unavailable", "wrong-job", "stop-refused"} {
		t.Run(mode, func(t *testing.T) {
			dir, p, _, ops := recoveryFixture(t)
			deletes := 0
			ops.present = func(context.Context, namespace) (bool, error) {
				if mode == "inventory-unavailable" {
					return false, errors.New("synthetic Docker unavailable")
				}
				return true, nil
			}
			ops.inspect = func(context.Context, *namespace) ([]byte, error) {
				job := p.JobID
				if mode == "wrong-job" {
					job++
				}
				return json.Marshal([]map[string]any{{"Config": map[string]any{"Labels": map[string]string{"cc.run": strconv.FormatInt(p.RunID, 10), "cc.job_id": strconv.FormatInt(job, 10)}}}})
			}
			ops.dispose = func(*namespace) error { return errors.New("synthetic stop refused") }
			ops.runners = func(context.Context) ([]runnerEntry, error) {
				t.Fatal("registration read before namespace failure returned")
				return nil, nil
			}
			ops.delete = func(context.Context, string, string, int64) error { deletes++; return nil }
			status, e := recoverUsing(context.Background(), dir, true, ops)
			if e == nil || status.Clean || deletes != 0 {
				t.Fatal("unknown/failed drain released resources")
			}
			if _, e = os.Stat(ops.lockPath); e != nil {
				t.Fatal("failure released original lock")
			}
		})
	}
}

// io.Copy's ReaderFrom optimization used to bypass an embedded Buffer.Write.
func TestBoundedCaptureCannotBypassLimitThroughReaderFrom(t *testing.T) {
	b := boundedBuffer{limit: 4}
	if _, e := io.Copy(&b, io.LimitReader(bytes.NewReader([]byte("too-large")), 9)); e == nil || b.Len() > 4 {
		t.Fatal("capture limit bypassed")
	}
}
