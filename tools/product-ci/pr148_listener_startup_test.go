package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Uses the real pinned SDK and production Landlock/seccomp launch path without
// configuring a runner or providing credentials. The official listener must
// reject missing configuration with its nonzero exit, rather than run.sh
// returning zero after failing to create run-helper.sh in the read-only root.
func TestPR148RestrictedListenerStartup(t *testing.T) {
	dir, elf := os.Getenv("CC_PR148_STARTUP_DIR"), os.Getenv("CC_PR148_FIXTURE_ELF")
	if dir == "" || elf == "" {
		t.Skip("explicit owned startup root and Linux helper required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := pr148OwnedReceiptRoot(ctx, dir); err != nil {
		t.Fatal(err)
	}
	guest, err := os.ReadFile(elf)
	if err != nil {
		t.Fatal(err)
	}
	n, err := newPR148Namespace(ctx, dir)
	if n != nil && n.ID != "" {
		defer func() {
			if err := n.dispose(); err != nil {
				t.Error(err)
			}
			if err := pr148SaveNew(filepath.Join(dir, "startup-namespace-result.private.json"), n); err != nil {
				t.Error(err)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := pr148Transfer(ctx, n, guest); err != nil {
		t.Fatal(err)
	}
	out, err := cmd(ctx, docker, "exec", n.ID, "/runner/product-ci", "pr148-listener-startup-probe").CombinedOutput()
	if writeErr := os.WriteFile(filepath.Join(dir, "startup-listener.private.log"), out, 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	if exitStatus(err) != 1 || !bytes.Contains(bytes.ToLower(out), []byte("not configured")) {
		t.Fatalf("official unconfigured listener failure not observed: exit=%d bytes=%d", exitStatus(err), len(out))
	}
	if bytes.Contains(out, []byte("run-helper.sh")) {
		t.Fatal("mutable shell startup wrapper was invoked")
	}
}
