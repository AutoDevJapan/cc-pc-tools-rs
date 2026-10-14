package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestExitedUnreapedGroupDoesNotHoldTarget(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait() // Deliberately leave the leader unreaped until drain returns.
	deadline := time.Now().Add(2 * time.Second)
	for {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", cmd.Process.Pid))
		state, _, parseErr := procMember(stat)
		if err == nil && parseErr == nil && state == "Z" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not become an unreaped zombie")
		}
		time.Sleep(time.Millisecond)
	}
	if err := syscall.Kill(-cmd.Process.Pid, 0); err != nil {
		t.Fatalf("zombie should still have a kernel process group: %v", err)
	}
	drained := make(chan error, 1)
	go func() { drained <- terminateAndDrainGroup(cmd, time.Millisecond, time.Millisecond) }()
	select {
	case err := <-drained:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unreaped zombie held the target drain")
	}
}

func TestLiveTermIgnoringGroupRequiresForceKill(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", "trap '' TERM; printf 'ready\\n'; exec sleep 30")
	group(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	if !processGroupExists(cmd.Process.Pid) {
		t.Fatal("live group was treated as drained")
	}
	started := time.Now()
	if err := terminateAndDrainGroup(cmd, 50*time.Millisecond, time.Second); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 50*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("force-kill did not respect the live group's grace period: %s", elapsed)
	}
}

func TestZombieLeaderWithLiveThreadAndUnreadableStateRetainsTarget(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "10")
	if err := os.MkdirAll(filepath.Join(path, "task", "10"), 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path, value string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(path, "stat"), "10 (name with ) parentheses) Z 1 42 0")
	write(filepath.Join(path, "task", "10", "stat"), "10 (leader) Z 1 42 0")
	if live, err := groupHasLiveMembers(root, 42); err != nil || live {
		t.Fatalf("only zombie tasks should be drained: live=%v err=%v", live, err)
	}
	if err := os.MkdirAll(filepath.Join(path, "task", "11"), 0700); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(path, "task", "11", "stat"), "11 (worker) S 1 42 0")
	if live, err := groupHasLiveMembers(root, 42); err != nil || !live {
		t.Fatalf("live thread under zombie leader must retain target: live=%v err=%v", live, err)
	}
	write(filepath.Join(path, "task", "11", "stat"), "malformed")
	if live, err := groupHasLiveMembers(root, 42); err == nil || !live {
		t.Fatal("unknown task state released the target")
	}
}

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
