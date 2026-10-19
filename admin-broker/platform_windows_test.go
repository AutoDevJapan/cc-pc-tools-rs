package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

type setupExpiry struct {
	context.Context
	calls int
	done  chan struct{}
}

func (c *setupExpiry) Done() <-chan struct{} { return c.done }
func (c *setupExpiry) Err() error {
	c.calls++
	if c.calls == 2 {
		close(c.done)
	}
	if c.calls >= 2 {
		return context.DeadlineExceeded
	}
	return nil
}

func TestDeadlineExpiringDuringSetupDoesNotStartProcess(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "must-not-run")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx := &setupExpiry{Context: context.Background(), done: make(chan struct{})}
	r := runProcess(ctx, exe, []string{"-test.run=TestJobProcess", "--", "--admin-broker-fixture", "child", marker}, dir)
	if r.Exit != 124 || !r.TimedOut || r.CleanupPending {
		t.Fatalf("setup deadline result: %+v", r)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("process started after setup consumed request deadline", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("expired setup leaked output files", err)
	}
}

func TestWindowsJobStructureSizes(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) == 8 && (unsafe.Sizeof(jobLimits{}) != 144 || unsafe.Sizeof(jobAccounting{}) != 48 || unsafe.Sizeof(startupInfoEx{}) != 112) {
		t.Fatal("Win64 structure layout mismatch")
	}
}

func TestWindowsSharingConflictIsRetriedAfterHandleRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "result.json")
	if err := os.WriteFile(path, []byte("complete-result"), 0600); err != nil {
		t.Fatal(err)
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	locked, err := syscall.CreateFile(name, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if locked != 0 {
			syscall.CloseHandle(locked)
		}
	}()
	var data []byte
	var observed []error
	err = retryFile(func() error {
		var readErr error
		data, readErr = os.ReadFile(path)
		observed = append(observed, readErr)
		if fileBusy(readErr) && locked != 0 {
			syscall.CloseHandle(locked)
			locked = 0
		}
		return readErr
	})
	if err != nil || len(observed) != 2 || !fileBusy(observed[0]) || string(data) != "complete-result" {
		t.Fatalf("sharing retry: %v, attempts=%d, data=%q", err, len(observed), data)
	}
}

func TestRunScriptCapturesBothStreamsAndPreservesExit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "space 日本語")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "read only.ps1")
	if err := os.WriteFile(script, []byte("[Console]::Out.WriteLine('real-probe')\n[Console]::Error.WriteLine('real-error')\nexit 7\n"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r := runScript(ctx, script)
	if r.Exit != 7 || !strings.Contains(r.Stdout, "real-probe") || !strings.Contains(r.Stderr, "real-error") || r.TimedOut {
		t.Fatalf("unexpected real process result: %+v", r)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "read only.ps1" {
		t.Fatal("process output files leaked", err)
	}
}

func TestTimeoutDrainsOwnedDescendant(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "child-ready")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan result, 1)
	go func() {
		done <- runProcess(ctx, exe, []string{"-test.run=TestJobProcess", "--", "--admin-broker-fixture", "parent", marker}, dir)
	}()
	// Readiness comes from the real child, rather than a fixed sleep before cancel.
	waitForFile(t, marker)
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.ParseUint(string(data), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(handle)
	state, err := syscall.WaitForSingleObject(handle, 0)
	if err != nil || state != syscall.WAIT_TIMEOUT {
		t.Fatal("fixture child was not alive before cancellation", err)
	}
	cancel()
	select {
	case r := <-done:
		if r.Exit != 124 || !r.TimedOut || strings.Contains(r.Stderr, "cleanup failed") {
			t.Fatalf("unexpected timeout result: %+v", r)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("owned request did not finish after cancellation")
	}
	state, err = syscall.WaitForSingleObject(handle, 0)
	if err != nil || state != syscall.WAIT_OBJECT_0 {
		t.Fatal("owned descendant survived request completion", err)
	}
}

func TestJobProcess(t *testing.T) {
	for i, arg := range os.Args {
		if arg != "--admin-broker-fixture" {
			continue
		}
		mode, marker := os.Args[i+1], os.Args[i+2]
		if mode == "parent" {
			child := exec.Command(os.Args[0], "-test.run=TestJobProcess", "--", "--admin-broker-fixture", "child", marker)
			child.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			if err := child.Start(); err != nil {
				os.Exit(90)
			}
		} else {
			if err := os.WriteFile(marker, []byte(fmt.Sprint(os.Getpid())), 0600); err != nil {
				os.Exit(91)
			}
		}
		time.Sleep(30 * time.Second) // Bounded even if the test's job cleanup regresses.
		os.Exit(0)
	}
}
