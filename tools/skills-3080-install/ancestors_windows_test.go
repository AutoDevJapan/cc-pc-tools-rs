//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestJunctionAncestorRejected(t *testing.T) {
	d := t.TempDir()
	physical := filepath.Join(d, "physical")
	if err := os.MkdirAll(filepath.Join(physical, "skills"), 0700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(d, "linked")
	cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, physical)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create owned junction: %v, %s", err, output)
	}
	t.Cleanup(func() {
		if err := os.Remove(link); err != nil {
			t.Error(err)
		}
	})
	if err := preflight(fixtureBundle(), filepath.Join(link, "skills"), filepath.Join(d, "receipt")); err == nil {
		t.Fatal("junction ancestor accepted")
	}
	entries, err := os.ReadDir(filepath.Join(physical, "skills"))
	if err != nil || len(entries) != 0 {
		t.Fatal("junction rejection wrote a target")
	}
}
