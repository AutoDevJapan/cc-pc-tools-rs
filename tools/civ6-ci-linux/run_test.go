package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePlan(t *testing.T, p Plan) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "plan.json")
	if e := os.WriteFile(path, mustJSON(p), 0600); e != nil {
		t.Fatal(e)
	}
	return path
}

func TestRunFailsClosedWhenTheParentContractIsNotLinked(t *testing.T) {
	useContract(t, unwiredContract{})
	e := runJob(context.Background(), writePlan(t, rustPlan()), t.TempDir(), "guest", "pwsh")
	if !errors.Is(e, errContractUnwired) {
		t.Fatalf("expected the unwired contract to stop the run first, got %v", e)
	}
}

func TestRunStopsBeforeAnyResourceOnBadLabelsOrOwnership(t *testing.T) {
	f := &fakeContract{labels: []string{"self-hosted", "Linux", "X64", "someone-else"}}
	useContract(t, f)
	if e := runJob(context.Background(), writePlan(t, rustPlan()), t.TempDir(), "guest", "pwsh"); e == nil || f.calls["verify"] != 0 {
		t.Fatalf("labels outside the route were accepted: %v", e)
	}
	f.labels = []string{"self-hosted", "Linux", "X64", linuxLabel, jobLabel(rustPlan())}
	if e := runJob(context.Background(), writePlan(t, rustPlan()), t.TempDir(), "guest", "pwsh"); e == nil || !strings.Contains(e.Error(), "safe-delete") || f.calls["verify"] != 0 {
		t.Fatalf("a root not created by safe-delete was accepted: %v", e)
	}
	f.validate = errors.New("parent rejected the plan")
	if e := runJob(context.Background(), writePlan(t, rustPlan()), t.TempDir(), "guest", "pwsh"); e == nil || !strings.Contains(e.Error(), "parent rejected") {
		t.Fatalf("parent Validate result ignored: %v", e)
	}
	// A plan for a Windows job never reaches the contract.
	w := rustPlan()
	w.JobName = "test (windows-latest)"
	f2 := &fakeContract{}
	useContract(t, f2)
	if e := runJob(context.Background(), writePlan(t, w), t.TempDir(), "guest", "pwsh"); e == nil || len(f2.calls) != 0 {
		t.Fatalf("Windows job accepted: %v", e)
	}
}

func TestOwnedReceiptRootRequiresSafeDeleteShape(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	for _, name := range []string{"plain", "codex-owned-zz", "codex-owned-" + rep("A", 32), "codex-owned-" + rep("a", 31)} {
		dir := filepath.Join(base, name)
		if e := os.Mkdir(dir, 0700); e != nil {
			t.Fatal(e)
		}
		if e := ownedReceiptRoot(ctx, dir); e == nil {
			t.Errorf("accepted %s", name)
		}
	}
	dirty := filepath.Join(base, "codex-owned-"+rep("b", 32))
	if e := os.Mkdir(dirty, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(dirty, "old.private.json"), []byte("{}"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := ownedReceiptRoot(ctx, dirty); e == nil || !strings.Contains(e.Error(), "empty") {
		t.Fatalf("non-empty root accepted: %v", e)
	}
}

func TestAwaitSuccessNeedsTheParentConfirmation(t *testing.T) {
	calls := 0
	ok := func(context.Context, Plan, string, ReadAPI) error {
		calls++
		if calls < 3 {
			return errors.New("still running")
		}
		return nil
	}
	pause := func(context.Context) error { return nil }
	if e := awaitSuccess(context.Background(), rustPlan(), "r", ok, nil, pause, 10); e != nil || calls != 3 {
		t.Fatalf("%v calls=%d", e, calls)
	}
	never := func(context.Context, Plan, string, ReadAPI) error { return errors.New("conclusion=failure") }
	e := awaitSuccess(context.Background(), rustPlan(), "r", never, nil, pause, 4)
	if e == nil || !strings.Contains(e.Error(), "conclusion=failure") {
		t.Fatalf("listener exit or timeout became success: %v", e)
	}
}

func TestCredentialCleanupDeniedIsExact(t *testing.T) {
	msg := []byte("An error occurred: Access to the path '/runner/.credentials' is denied.")
	if !credentialCleanupDenied(2, msg) || credentialCleanupDenied(1, msg) || credentialCleanupDenied(2, []byte("other")) {
		t.Fatal("known post-job credential refusal misclassified")
	}
}

func TestReceiptNeverCarriesATokenField(t *testing.T) {
	b, _ := json.Marshal(receipt{})
	if strings.Contains(strings.ToLower(string(b)), "token") {
		t.Fatal("receipt shape includes a token field")
	}
	b, _ = json.Marshal(guestPacket{})
	if !strings.Contains(string(b), "Token") {
		t.Fatal("packet must carry the RAM-only token until it is cleared")
	}
}
