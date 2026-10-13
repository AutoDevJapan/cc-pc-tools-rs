//go:build windows

package main

import (
	"os"
	"testing"
)

func TestNativeWindowsProcessCreationIdentity(t *testing.T) {
	pid := uint32(os.Getpid())
	p := nativeProbe(pid)
	if !p.known || !p.alive || p.created == nil || *p.created == 0 {
		t.Fatal("real Windows process could not be inspected")
	}
	if presence(pid, p.created, nativeProbe) != "verified_alive" || presence(pid, nil, nativeProbe) != "alive_identity_unknown" {
		t.Fatal("known and unknown creation identities were conflated")
	}
	wrong := *p.created + 1
	if presence(pid, &wrong, nativeProbe) != "identity_mismatch" {
		t.Fatal("PID reuse must not count as the registered worker")
	}
}
