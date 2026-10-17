//go:build windows && amd64

package main

import (
	"testing"
	"unsafe"
)

func TestJobMembershipSuccessfulCallIgnoresLastError(t *testing.T) {
	// IsProcessInJob returns a BOOL result independently of GetLastError. Do
	// not require GetLastError to be a nil Go interface on the success path.
	var member uint32
	r, _, e := inJob.Call(^uintptr(0), 0, uintptr(unsafe.Pointer(&member)))
	if r == 0 {
		t.Fatal(e)
	}
	if member > 1 {
		t.Fatal("invalid membership result")
	}
}

func TestRuntimeFixtureCannotEnterOfficialPlan(t *testing.T) {
	p := fixturePlan()
	p.RuntimeFixture = true
	if p.validate() == nil {
		t.Fatal("runtime fixture authorized an official runner")
	}
}

func TestOwnerCannotUseServiceToolEntry(t *testing.T) {
	if e := checkToolIdentity(fixturePlan()); e == nil {
		t.Fatal("owner token entered the service SDK")
	}
}
