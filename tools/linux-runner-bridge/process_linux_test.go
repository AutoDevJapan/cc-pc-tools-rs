package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestZombieGroupRetainsTargetUntilReaped(t *testing.T) {
	cmd := exec.Command("/bin/sh", "-c", "exit 0")
	group(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Wait()
	deadline := time.Now().Add(2 * time.Second)
	for {
		stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", cmd.Process.Pid))
		end := strings.LastIndexByte(string(stat), ')')
		if err == nil && end >= 0 {
			fields := strings.Fields(string(stat[end+1:]))
			if len(fields) > 0 && fields[0] == "Z" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not become an unreaped zombie")
		}
		time.Sleep(time.Millisecond)
	}
	if !processGroupExists(cmd.Process.Pid) {
		t.Fatal("a process snapshot released a group still present in the kernel")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if processGroupExists(cmd.Process.Pid) {
		t.Fatal("reaped group still exists")
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
	done := make(chan error, 1)
	if _, err := bufio.NewReader(stdout).ReadString('\n'); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal(err)
	}
	go func() { done <- cmd.Wait() }()
	if !processGroupExists(cmd.Process.Pid) {
		t.Fatal("live group was treated as drained")
	}
	started := time.Now()
	if err := terminateAndDrainGroup(cmd, 50*time.Millisecond, time.Second); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed < 50*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("force-kill grace period: %s", elapsed)
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("force-killed child was not reaped")
	}
}

func TestSuccessfulLeaderDrainsBackgroundGroup(t *testing.T) {
	file := t.TempDir() + "/late-write"
	cmd := exec.Command("/bin/sh", "-c", `(sleep 30; printf unexpected > "$1") </dev/null >/dev/null 2>&1 & printf '%s\n' "$!"`, "sh", file)
	group(cmd)
	if _, err := cmd.Output(); err != nil {
		t.Fatal(err)
	}
	defer killGroup(cmd)
	if !processGroupExists(cmd.Process.Pid) {
		t.Fatal("fixture did not retain its background writer after a successful leader exit")
	}
	if code := finishGroup(cmd, nil); code != 0 {
		t.Fatalf("successful leader result changed: %d", code)
	}
	if processGroupExists(cmd.Process.Pid) {
		t.Fatal("successful exit released the target while a background group remained")
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("background writer changed the target after leader completion")
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
