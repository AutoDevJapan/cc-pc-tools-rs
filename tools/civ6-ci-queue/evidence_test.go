package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A root file the controller may not read is listed, not hidden, and the root
// is then quarantined whole instead of deleted.
func TestUnreadableRootEvidenceQuarantinesTheRoot(t *testing.T) {
	h := newHarness(t)
	real := evidenceCopy
	evidenceCopy = func(src, dst string) (string, error) {
		if strings.HasSuffix(src, "runner.log") {
			return "", os.ErrPermission
		}
		return real(src, dst)
	}
	defer func() { evidenceCopy = real }()
	key := h.seed("Windows", 350, 3501, "test (windows-latest)", true)
	if err := h.d.Supervise(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	e := h.ledger().Entries[key]
	if e.State != StateSucceeded || e.Cleanup.State != "quarantined" || len(h.sd.quarantined) != 1 || len(h.sd.finished) != 0 || !strings.Contains(e.Cleanup.Detail, "evidence incomplete") {
		t.Fatalf("%+v", e)
	}
	if _, err := os.Stat(e.Cleanup.Root); err != nil {
		t.Fatal("the root with unreadable bytes must be kept")
	}
	b, _ := os.ReadFile(filepath.Join(e.Cleanup.Evidence, "manifest.json"))
	if !strings.Contains(string(b), "unreadable") {
		t.Fatalf("manifest must list the unreadable entry: %s", b)
	}
}

// A copy that differs from its source is still a hard failure, never a quarantine.
func TestEvidenceHashMismatchIsAHardFailure(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "a.log"), []byte("x"), 0600)
	real := evidenceCopy
	evidenceCopy = func(src, dst string) (string, error) { return "", errCopyDiffers }
	defer func() { evidenceCopy = real }()
	if _, _, err := preserveEvidence(root, t.TempDir(), filepath.Join(t.TempDir(), "ev")); err == nil {
		t.Fatal("hash mismatch accepted")
	}
}
