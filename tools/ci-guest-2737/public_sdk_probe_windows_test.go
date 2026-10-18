package main

import (
	"path/filepath"
	"testing"
)

func TestSDKProbeSharesTheNormalCIKernelExclusion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "normal-ci.lock")
	one, e := takeNormalCILock(path)
	if e != nil {
		t.Fatal(e)
	}
	defer one.Close()
	if other, e := takeNormalCILock(path); e == nil {
		_ = other.Close()
		t.Fatal("overlapping public probe and CI admitted")
	}
	if e = one.Close(); e != nil {
		t.Fatal(e)
	}
	other, e := takeNormalCILock(path)
	if e != nil {
		t.Fatal("closed admission remained locked", e)
	}
	_ = other.Close()
}
