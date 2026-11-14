//go:build windows

package tree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	queryLimitedInformation = 0x1000
	createBreakawayFromJob  = 0x01000000
)

// TestAChild starts one child process as TREE_MODE asks and records it in
// TREE_PIDS_DIR. Without both variables it does nothing.
func TestAChild(t *testing.T) {
	mode, dir := os.Getenv("TREE_MODE"), os.Getenv("TREE_PIDS_DIR")
	if mode == "" || dir == "" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestChildSleep$")
	child.Env = append(os.Environ(), "TREE_CHILD_SLEEP=1")
	tag := "owned"
	switch mode {
	case "holdoutput":
		child.Stdout, child.Stderr = os.Stdout, os.Stderr
	case "breakaway":
		child.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createBreakawayFromJob}
		tag = "breakaway"
	}
	if err := child.Start(); err != nil {
		t.Fatalf("start the %s child: %v", mode, err)
	}
	created, err := creationTime(uint32(child.Process.Pid))
	if err != nil {
		t.Fatal(err)
	}
	record := fmt.Sprintf("%s %d %d\n", tag, child.Process.Pid, created)
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.pid", tag, child.Process.Pid)), []byte(record), 0o600); err != nil {
		t.Fatal(err)
	}
	if mode == "hang" {
		time.Sleep(time.Hour)
	}
}

func TestBQuick(t *testing.T) {}

// TestChildSleep is the body of the child process.
func TestChildSleep(t *testing.T) {
	if os.Getenv("TREE_CHILD_SLEEP") == "1" {
		time.Sleep(5 * time.Minute)
	}
}

// TestPreviousTreeGone runs in the second partition. The owned child of the
// first partition must have exited by then; a child that broke away from the
// job on purpose must still run, and this test then stops it itself.
func TestPreviousTreeGone(t *testing.T) {
	dir := os.Getenv("TREE_PIDS_DIR")
	if dir == "" {
		return
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.pid"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		tag, pid, created := readRecord(t, file)
		switch alive := running(pid, created); {
		case tag == "owned" && alive:
			t.Errorf("owned child %d of the first partition is still running", pid)
		case tag == "breakaway" && !alive:
			t.Errorf("breakaway child %d of the first partition was stopped", pid)
		case tag == "breakaway":
			if err := stop(pid); err != nil {
				t.Errorf("stop breakaway child %d: %v", pid, err)
			}
		}
	}
}

func TestDQuick(t *testing.T) {}

// stop ends a process that this fixture started outside the job.
func stop(pid uint32) error {
	h, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE|syscall.SYNCHRONIZE, false, pid)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	if err := syscall.TerminateProcess(h, 1); err != nil {
		return err
	}
	if event, err := syscall.WaitForSingleObject(h, 5000); err != nil || event != syscall.WAIT_OBJECT_0 {
		return fmt.Errorf("did not exit: event %d, %v", event, err)
	}
	return nil
}

func creationTime(pid uint32) (uint64, error) {
	h, err := syscall.OpenProcess(queryLimitedInformation, false, pid)
	if err != nil {
		return 0, err
	}
	defer syscall.CloseHandle(h)
	var created, exited, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil {
		return 0, err
	}
	return uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime), nil
}

// running reports whether the recorded process still runs. A process with
// the same ID but another creation time is a different process.
func running(pid uint32, created uint64) bool {
	h, err := syscall.OpenProcess(syscall.SYNCHRONIZE|queryLimitedInformation, false, pid)
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(h)
	var c, exited, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &c, &exited, &kernel, &user); err != nil || uint64(c.HighDateTime)<<32|uint64(c.LowDateTime) != created {
		return false
	}
	event, err := syscall.WaitForSingleObject(h, 0)
	return err == nil && event == syscall.WAIT_TIMEOUT
}

func readRecord(t *testing.T, file string) (string, uint32, uint64) {
	t.Helper()
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 3 {
		t.Fatalf("%s: unexpected record %q", file, data)
	}
	pid, err := strconv.ParseUint(fields[1], 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	created, err := strconv.ParseUint(fields[2], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return fields[0], uint32(pid), created
}
