//go:build windows && amd64

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
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
	var token syscall.Handle
	r, _, e := openToken.Call(^uintptr(0), 8, uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		t.Fatal(e)
	}
	defer syscall.CloseHandle(token)
	got, e := tokenUserSID(token)
	if e != nil {
		t.Fatal(e)
	}
	want := "S-1-5-18"
	if got == want {
		want = "S-1-5-19"
	}
	if e = toolTokenIdentity(token, want); !errors.Is(e, errToolIdentity) {
		t.Fatal("actual mismatched token not rejected", e)
	}
}

func TestSDKEnvironmentMissingAndCorruptFailClosed(t *testing.T) {
	p := fixturePlan()
	p.Root = t.TempDir()
	if _, e := runtimeSDKEnv(p, []string{"SHOULD_NOT_INHERIT=1"}); e == nil {
		t.Fatal("missing SDK environment accepted")
	}
	if e := os.Mkdir(p.runtime(), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(p.runtime(), "sdk-environment.json"), []byte(`{}`), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := runtimeSDKEnv(p, nil); e == nil {
		t.Fatal("incomplete SDK environment accepted")
	}
}

func TestCheckoutStateParsing(t *testing.T) {
	s, e := checkoutStates("isPost<<d\r\ntrue\r\nd\r\nrepositoryPath=x\r\n")
	if e != nil || s["isPost"] != "true" || s["repositoryPath"] != "x" {
		t.Fatal(s, e)
	}
	for _, raw := range []string{"isPost<<d\nx\n", "unexpected=x\n", "broken"} {
		if _, e := checkoutStates(raw); e == nil {
			t.Fatal("invalid action state accepted")
		}
	}
}
