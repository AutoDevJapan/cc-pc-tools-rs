package main

import (
	"os/exec"
	"testing"
	"time"
)

func TestKillGroupDrainsBeforeReturning(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "sleep 30 & wait")
	group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	groupID := cmd.Process.Pid
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	started := time.Now()
	killGroup(cmd)
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("process group drain exceeded its expected bound: %s", elapsed)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("leader was not reaped after the group drained")
	}
	if processGroupExists(groupID) {
		t.Fatal("process group still exists after drain returned")
	}
}
