//go:build windows

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRetirementConsumesActualV3SafeDeleteProducer(t *testing.T) {
	base := t.TempDir()
	exe := filepath.Join(base, "safe-delete.exe")
	build := exec.Command("go", "build", "-trimpath", "-o", exe, ".")
	build.Dir = filepath.Join("..", "..", "safe-delete")
	build.Env = append(os.Environ(), "GOTOOLCHAIN=auto")
	hide(build)
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("fixture producer build: %v %s", e, out)
	}
	call := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command(exe, args...)
		hide(cmd)
		for _, env := range os.Environ() {
			if !strings.HasPrefix(env, "CC_JOB_ID=") {
				cmd.Env = append(cmd.Env, env)
			}
		}
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("producer fixture %s: %v %s", args[0], e, out)
		}
		return out
	}
	key := "1/1/1"
	raw := call("create", "--root", base, "--project", "civ6-ci-queue", "--content", "job-"+safeKey(key))
	var r retirementRecord
	if e := json.Unmarshal(raw, &r); e != nil {
		t.Fatal(e)
	}
	if r.Version != 3 || r.Birth == 0 {
		t.Fatal("actual v3 producer expected")
	}
	if e := os.Remove(r.Path); e != nil {
		t.Fatal(e)
	}
	call("finish", "--id", r.ID)
	b, e := os.ReadFile(exe)
	if e != nil {
		t.Fatal(e)
	}
	got, e := retiredReceipt(Pinned{Path: exe, SHA256: digestHexBytes(b)}, CleanupRec{Root: r.Path, SafeDeleteID: r.ID, Instance: r.ID}, key)
	if e != nil || got.State != "removed" {
		t.Fatal("consumer rejected actual producer", got, e)
	}
}
