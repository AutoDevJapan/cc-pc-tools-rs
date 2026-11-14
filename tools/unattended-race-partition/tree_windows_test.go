//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

const (
	treeQueryLimitedInformation = 0x1000
	treeCreateBreakawayFromJob  = 0x01000000
)

// runTreeFixture runs the tree fixture, whose first partition starts a child
// process as mode asks. Any recorded child that still runs when the test ends
// is stopped.
func runTreeFixture(t *testing.T, mode, partitionTimeout string) (int, string, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TREE_MODE", mode)
	t.Setenv("TREE_PIDS_DIR", dir)
	t.Cleanup(func() { stopRecordedChildren(t, dir) })
	start := time.Now()
	code, out := runFixture(t, "tree", partitionTimeout)
	if elapsed := time.Since(start); elapsed > 90*time.Second {
		t.Errorf("the command took %v; it waited for a child instead of stopping it", elapsed)
	}
	return code, out, dir
}

// stoppedChild matches the report of a stopped child test binary. The count
// is not fixed: a system may add helper processes such as a console host.
var stoppedChild = regexp.MustCompile(`partition 1/2 stopped [1-9][0-9]* processes still running after the test binary ended: (\S+ )*package\.test\.exe`)

func requireStoppedChild(t *testing.T, out string) {
	t.Helper()
	if !stoppedChild.MatchString(out) {
		t.Errorf("output does not report the stopped child test binary\n%s", out)
	}
}

func TestRunStopsAChildThatOutlivesTheTestBinary(t *testing.T) {
	code, out, dir := runTreeFixture(t, "detached", "2m")
	if code != exitPass {
		t.Fatalf("exit %d, want %d\n%s", code, exitPass, out)
	}
	requireStoppedChild(t, out)
	requireOutput(t, out, "partition 2/2 passed", "race-partition: PASS")
	requireOwnedChildrenStopped(t, dir)
}

func TestRunFailsAndStopsAChildThatHoldsTheOutput(t *testing.T) {
	code, out, dir := runTreeFixture(t, "holdoutput", "2m")
	if code != exitFail {
		t.Fatalf("exit %d, want %d\n%s", code, exitFail, out)
	}
	requireStoppedChild(t, out)
	requireOutput(t, out, "test output stayed open 5s after the binary exited", "partition 2/2 passed")
	requireOwnedChildrenStopped(t, dir)
}

func TestRunStopsTheTreeAtThePartitionDeadline(t *testing.T) {
	code, out, dir := runTreeFixture(t, "hang", "3s")
	if code != exitFail {
		t.Fatalf("exit %d, want %d\n%s", code, exitFail, out)
	}
	requireStoppedChild(t, out)
	requireOutput(t, out, "panic: test timed out after 3s", "partition 2/2 passed")
	requireOwnedChildrenStopped(t, dir)
}

func TestRunLeavesAChildThatBreaksAwayOnPurpose(t *testing.T) {
	if !breakawayAllowed(t) {
		t.Skip("this environment does not allow CREATE_BREAKAWAY_FROM_JOB, so the fixture cannot detach a child")
	}
	code, out, dir := runTreeFixture(t, "breakaway", "2m")
	if code != exitPass {
		t.Fatalf("exit %d, want %d\n%s", code, exitPass, out)
	}
	requireOutput(t, out, "partition 2/2 passed", "race-partition: PASS")
	if strings.Contains(out, "partition 1/2 stopped") {
		t.Errorf("the command stopped a child that left the job on purpose:\n%s", out)
	}
	if records := treeRecords(t, dir); len(records) != 1 || records[0].tag != "breakaway" {
		t.Fatalf("records = %+v, want one breakaway child", records)
	}
}

// breakawayAllowed reports whether this test process may start a child with
// CREATE_BREAKAWAY_FROM_JOB.
func breakawayAllowed(t *testing.T) bool {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	probe := exec.Command(exe, "-test.run=^$")
	probe.SysProcAttr = &syscall.SysProcAttr{CreationFlags: treeCreateBreakawayFromJob}
	if err := probe.Run(); err != nil {
		t.Logf("breakaway probe: %v", err)
		return false
	}
	return true
}

type treeRecord struct {
	tag     string
	pid     uint32
	created uint64
}

func treeRecords(t *testing.T, dir string) []treeRecord {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "*.pid"))
	if err != nil {
		t.Fatal(err)
	}
	var records []treeRecord
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Fields(string(data))
		if len(fields) != 3 {
			t.Fatalf("%s: unexpected record %q", file, data)
		}
		pid, err1 := strconv.ParseUint(fields[1], 10, 32)
		created, err2 := strconv.ParseUint(fields[2], 10, 64)
		if err1 != nil || err2 != nil {
			t.Fatalf("%s: unexpected record %q", file, data)
		}
		records = append(records, treeRecord{fields[0], uint32(pid), created})
	}
	return records
}

// openRecorded opens the recorded process if it still runs.
func openRecorded(r treeRecord, access uint32) (syscall.Handle, bool) {
	h, err := syscall.OpenProcess(access|syscall.SYNCHRONIZE|treeQueryLimitedInformation, false, r.pid)
	if err != nil {
		return 0, false
	}
	var created, exited, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(h, &created, &exited, &kernel, &user); err != nil || uint64(created.HighDateTime)<<32|uint64(created.LowDateTime) != r.created {
		syscall.CloseHandle(h)
		return 0, false
	}
	if event, err := syscall.WaitForSingleObject(h, 0); err != nil || event != syscall.WAIT_TIMEOUT {
		syscall.CloseHandle(h)
		return 0, false
	}
	return h, true
}

func requireOwnedChildrenStopped(t *testing.T, dir string) {
	t.Helper()
	records := treeRecords(t, dir)
	if len(records) == 0 {
		t.Fatal("the fixture recorded no child")
	}
	for _, r := range records {
		if h, alive := openRecorded(r, 0); alive {
			syscall.CloseHandle(h)
			if r.tag == "owned" {
				t.Errorf("owned child %d still runs after the command returned", r.pid)
			}
		}
	}
}

func stopRecordedChildren(t *testing.T, dir string) {
	for _, r := range treeRecords(t, dir) {
		h, alive := openRecorded(r, syscall.PROCESS_TERMINATE)
		if !alive {
			continue
		}
		t.Logf("stopping recorded %s child %d", r.tag, r.pid)
		_ = syscall.TerminateProcess(h, 1)
		_, _ = syscall.WaitForSingleObject(h, 5000)
		syscall.CloseHandle(h)
	}
}
