package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAdmissionRejectsLiveOwnerAndReleasesOnClose(t *testing.T) {
	p := filepath.Join(t.TempDir(), "admission")
	a, e := os.OpenFile(p, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	if e = lockAdmission(a); e != nil {
		t.Fatal(e)
	}
	b, e := os.OpenFile(p, os.O_RDWR, 0600)
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	if lockAdmission(b) == nil {
		t.Fatal("live owner was displaced")
	}
	if e = a.Close(); e != nil {
		t.Fatal(e)
	}
	if e = lockAdmission(b); e != nil {
		t.Fatalf("dead owner admission did not release: %v", e)
	}
}

func TestRecoveryDeletesOwnedAdmissionWhileKernelLockRemainsHeld(t *testing.T) {
	p := filepath.Join(t.TempDir(), "admission")
	if e := os.WriteFile(p, []byte("nonce"), 0600); e != nil {
		t.Fatal(e)
	}
	f, e := openAdmissionForRecovery(p)
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	if e = lockAdmission(f); e != nil {
		t.Fatal(e)
	}
	if e = os.Remove(p); e != nil {
		t.Fatalf("recovery cannot remove its admission with the kernel lock held: %v", e)
	}
	if _, e = os.Lstat(p); !os.IsNotExist(e) {
		t.Fatal("completed admission path remains")
	}
}
