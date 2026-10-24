//go:build windows

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSourceProcessProofRequiresTerminationOfExactIdentity(t *testing.T) {
	id, err := procIdentity(uint32(os.Getpid()))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifySourceProcessStopped(id); err == nil || !strings.Contains(err.Error(), "still live") {
		t.Fatal("live current process was not specifically observed alive", err)
	}
	old := id
	old.Created++
	if err := verifySourceProcessStopped(old); err != nil {
		t.Fatal("recorded process replaced by another birth is gone", err)
	}
	for _, bad := range []ProcID{{PID: id.PID}, {Created: id.Created}} {
		if verifySourceProcessStopped(bad) == nil {
			t.Fatal("unknown identity treated as stopped")
		}
	}
	if err := verifySourceProcessStopped(ProcID{}); err != nil {
		t.Fatal(err)
	}
	if err := verifySourceProcessStopped(ProcID{PID: 0xfffffffe, Created: 1}); err != nil {
		t.Fatal("nonexistent PID was not verified absent", err)
	}
}

func TestSourceStoppedOwnedHelper(t *testing.T) {
	if os.Getenv("CIQ_SOURCE_STOPPED_HELPER") == "1" {
		_, _ = io.Copy(io.Discard, os.Stdin)
	}
}

func TestSourceProcessProofAfterOwnedChildExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSourceStoppedOwnedHelper$")
	c.Env = append(scrubbedEnv(), "CIQ_SOURCE_STOPPED_HELPER=1")
	hide(c)
	c.WaitDelay = time.Second
	input, err := c.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	if err = c.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.Process.Kill()
	id, err := procIdentity(uint32(c.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	// Retain this process object after Wait, so the validator observes the actual
	// signaled process rather than relying solely on its PID having disappeared.
	h, _, _ := kernel32.NewProc("OpenProcess").Call(processQueryLimited|0x00100000, 0, uintptr(id.PID))
	if h == 0 {
		t.Fatal("could not retain owned process object")
	}
	defer kernel32.NewProc("CloseHandle").Call(h)
	if err := verifySourceProcessStopped(id); err == nil || !strings.Contains(err.Error(), "still live") {
		t.Fatal("owned helper was not specifically observed alive before EOF", err)
	}
	if err = input.Close(); err != nil {
		t.Fatal(err)
	}
	if err = c.Wait(); err != nil {
		t.Fatal(err)
	}
	if err = verifySourceProcessStopped(id); err != nil {
		t.Fatal("terminated owned helper was not verified", err)
	}
}
