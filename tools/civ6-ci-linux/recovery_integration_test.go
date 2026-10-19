package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// One credential-free namespace: no runner, source, Cargo or GitHub call.
// Simulate the host exiting by releasing its kernel lease, then exercise the
// same owned recovery operations used by the queue after a real interruption.
func TestRecoveryRealOwnedNamespace(t *testing.T) {
	dir := os.Getenv("CC_CIL_RECOVERY_FIXTURE_DIR")
	if dir == "" || runtime.GOOS != "windows" {
		t.Skip("new safe-delete owned root and Windows Docker Desktop required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if e := ownedReceiptRoot(ctx, dir); e != nil {
		t.Fatal(e)
	}
	lease, e := flightLease(filepath.Join(dir, "flight.lock"), true)
	if e != nil {
		t.Fatal(e)
	}
	p := rustPlan()
	if e = saveNew(filepath.Join(dir, "intent.private.json"), receipt{RepoID: repoID, Plan: p, Kind: KindRust}); e != nil {
		t.Fatal(e)
	}
	n, e := newNamespace(ctx, dir, KindRust, p)
	if e != nil {
		lease.Close()
		t.Fatal(e)
	}
	// Cleanup fallback uses the same journal, never a separate delete route.
	defer func() {
		if !n.Removed {
			if e := n.dispose(); e != nil {
				t.Error(e)
			}
		}
	}()
	lease.Close()
	if e = verifyReceiptOwner(ctx, dir); e != nil {
		t.Fatal(e)
	}
	ops := recoveryOps{present: namespacePresent, inspect: func(c context.Context, n *namespace) ([]byte, error) { return n.inspect(c) }, dispose: func(n *namespace) error { e := n.dispose(); return e }, runners: repoRunners, delete: deleteOwnedRegistration, lockPath: filepath.Join(dir, "fixture-global.lock")}
	if e = os.WriteFile(ops.lockPath, []byte(fmt.Sprintf("%d\n%d\n%s\n", p.RunID, p.JobID, dir)), 0600); e != nil {
		t.Fatal(e)
	}
	before, e := recoverUsing(ctx, dir, false, ops)
	if e != nil || before.Clean || before.NamespaceGone {
		t.Fatalf("running namespace mistaken for absent: %+v %v", before, e)
	}
	after, e := recoverUsing(ctx, dir, true, ops)
	if e != nil || !after.Clean {
		t.Fatalf("real owned collection failed: %+v %v", after, e)
	}
	n.Removed, n.TreeGone = true, true
	again, e := recoverUsing(ctx, dir, true, ops)
	if e != nil || !again.Clean {
		t.Fatalf("read-only postcondition failed: %+v %v", again, e)
	}
	if e = saveNew(filepath.Join(dir, "recovery-fixture-result.private.json"), map[string]any{"before": before, "after": after, "repeated_observation": again, "namespace_id": n.ID, "registered_runner": false, "cargo_executed": false}); e != nil {
		t.Fatal(e)
	}
}
