package main

import (
	"os"
	"strconv"
	"syscall"
	"testing"
)

func TestCIChildCannotInjectIntoLauncher(t *testing.T) {
	value := os.Getenv("PC_CI_LAUNCHER_PID")
	if value == "" {
		t.Skip("requires the actual filtered CI service child")
	}
	pid, err := strconv.ParseUint(value, 10, 32)
	if err != nil || pid == 0 || pid == uint64(os.Getpid()) {
		t.Fatal("invalid launcher identity")
	}
	// These are the rights required to inject a thread and write its payload.
	// Only opening is attempted; no process or token is modified.
	handle, err := syscall.OpenProcess(0x0400|0x0002|0x0020|0x0008, false, uint32(pid))
	if err == nil {
		syscall.CloseHandle(handle)
		t.Fatal("filtered CI child obtained launcher injection rights")
	}
	if err != syscall.ERROR_ACCESS_DENIED {
		t.Fatalf("expected an access denial from the live launcher, got %v", err)
	}
}
