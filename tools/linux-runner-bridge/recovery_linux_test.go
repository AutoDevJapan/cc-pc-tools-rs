package main

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestQuiescentRetryIsFiniteAndDoesNotAuthorizePersistentWorkers(t *testing.T) {
	clock := time.Unix(100, 0)
	probes := 0
	err := awaitCycleQuiescent(func() error { probes++; return errors.New("worker remains") }, func(d time.Duration) { clock = clock.Add(d) }, func() time.Time { return clock }, time.Second)
	if err == nil || probes != 5 {
		t.Fatalf("probes=%d err=%v", probes, err)
	}
}

func TestCycleRecoversInterruptedScratchWithoutClaimingJobSuccess(t *testing.T) {
	root, tmp, runner := cycleFixture(t, "exit 0\n")
	old, scratch, err := prepareCycle(root, tmp, runner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, "Worker.log"), []byte("interrupted evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scratch, "codeql-output"), make([]byte, 2<<20), 0600); err != nil {
		t.Fatal(err)
	}
	_, err = runNormalCycle(root, tmp, runner, func() error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(scratch); !os.IsNotExist(err) {
		t.Fatal("interrupted scratch leaked")
	}
	if _, err := os.Stat(filepath.Join(old, "complete")); !os.IsNotExist(err) {
		t.Fatal("interrupted job was marked complete")
	}
	if b, err := os.ReadFile(filepath.Join(old, "interrupted")); err != nil || string(b) != cycleOwner {
		t.Fatal("interruption not recorded")
	}
	if b, err := os.ReadFile(filepath.Join(old, "Worker.log")); err != nil || string(b) != "interrupted evidence" {
		t.Fatal("interrupted evidence changed")
	}
	if _, err := activeCycleScratch(root, tmp, runner); err == nil {
		t.Fatal("closed cycle admitted Cargo")
	}
}

func TestRecoveryLeavesUnknownScratchAndOutsideLinksUntouched(t *testing.T) {
	root, tmp, runner := cycleFixture(t, "")
	_, scratch, err := prepareCycle(root, tmp, runner)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Remove(scratch); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, scratch); err != nil {
		t.Fatal(err)
	}
	if err := recoverInterruptedCycles(root, tmp); err == nil {
		t.Fatal("substituted scratch accepted")
	}
	if target, err := os.Readlink(scratch); err != nil || target != outside {
		t.Fatal("unknown scratch changed")
	}
}

func TestInterruptedRecoveryWaitsForIdleAndStaysBoundedAcrossRestarts(t *testing.T) {
	root, tmp, runner := cycleFixture(t, "")
	for i := 0; i < 12; i++ {
		_, scratch, err := prepareCycle(root, tmp, runner)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(scratch, "temporary"), make([]byte, 2<<20), 0600); err != nil {
			t.Fatal(err)
		}
		_, err = runNormalCycle(root, tmp, runner, func() error { return errors.New("worker remains") })
		if err == nil {
			t.Fatal("live worker accepted")
		}
		if _, err := os.Stat(scratch); err != nil {
			t.Fatal("recovery touched active scratch")
		}
		if err := recoverInterruptedCycles(root, tmp); err != nil {
			t.Fatal(err)
		}
		if err := retainDiagnostics(root, diagnosticBudget, diagnosticCount); err != nil {
			t.Fatal(err)
		}
		if files, err := os.ReadDir(tmp); err != nil || len(files) != 0 {
			t.Fatal("interrupted scratch accumulated")
		}
	}
	if files, err := os.ReadDir(root); err != nil || len(files) != 8 {
		t.Fatal("interrupted evidence was not bounded")
	}
}

func TestScratchRemovalRestoresOwnedDirectoryAccessOnly(t *testing.T) {
	root, tmp, runner := cycleFixture(t, "")
	_, scratch, err := prepareCycle(root, tmp, runner)
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	file := filepath.Join(outside, "original")
	if err := os.WriteFile(file, []byte("keep"), 0400); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(scratch, "read-only")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(nested, "outside")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "generated"), []byte("temporary"), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(nested, 0000); err != nil {
		t.Fatal(err)
	}
	if err := removeOwnedScratch(scratch); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(file); err != nil || info.Mode().Perm() != 0400 {
		t.Fatal("outside permissions changed")
	}
	if b, err := os.ReadFile(file); err != nil || string(b) != "keep" {
		t.Fatal("outside data changed")
	}
}

// Use a subprocess so the real /proc ancestry is isolated from unrelated Go
// package tests. A genuine short-lived sh process models the host health probe.
func TestCycleLiveProcHelper(t *testing.T) {
	if os.Getenv("CC_CYCLE_LIVE_PROC_HELPER") != "1" {
		return
	}
	root, tmp, runner := cycleFixture(t, "exit 0\n")
	// CI runs inside an existing Worker. Ignore only that initial environment,
	// identified by PID plus kernel start time, while probing every new process.
	baseline := liveProcBaseline(t)
	probe := func() error { return cycleQuiescentWith("/proc", os.Getpid(), baseline, os.ReadFile) }
	boundaries := 0
	_, err := runNormalCycle(root, tmp, runner, func() error {
		boundaries++
		if boundaries == 1 {
			return awaitCycleQuiescent(probe, time.Sleep, time.Now, 3*time.Second)
		}
		cmd := exec.Command("/bin/sh", "-c", "printf 'ready\\n'; read stop")
		in, err := cmd.StdinPipe()
		if err != nil {
			return err
		}
		out, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		if _, err := bufio.NewReader(out).ReadString('\n'); err != nil {
			return err
		}
		observed := false
		err = awaitCycleQuiescent(func() error {
			err := probe()
			if err != nil && !observed {
				observed = true
				in.Close()
				_ = cmd.Wait()
			}
			return err
		}, time.Sleep, time.Now, 3*time.Second)
		if !observed {
			in.Close()
			_ = cmd.Wait()
			return errors.New("real probe process was not protected")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if files, err := os.ReadDir(tmp); err != nil || len(files) != 0 {
		t.Fatal("real probe caused scratch leak")
	}
}

func liveProcBaseline(t *testing.T) func(int) bool {
	t.Helper()
	identity := func(pid int) string {
		b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if err != nil {
			return ""
		}
		end := strings.LastIndexByte(string(b), ')')
		if end < 0 {
			return ""
		}
		fields := strings.Fields(string(b[end+1:]))
		if len(fields) <= 19 {
			return ""
		}
		return fields[19]
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	initial := map[int]string{}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err == nil {
			if birth := identity(pid); birth != "" {
				initial[pid] = birth
			}
		}
	}
	return func(pid int) bool { birth, ok := initial[pid]; return ok && birth == identity(pid) }
}

func TestWorkerAncestorRemainsRejectedInProduction(t *testing.T) {
	root := t.TempDir()
	for pid, stat := range map[string]string{"10": "10 (bridge) S 11 1", "11": "11 (Runner.Worker) S 1 1"} {
		if err := os.Mkdir(filepath.Join(root, pid), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, pid, "stat"), []byte(stat), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := cycleQuiescentAt(root, 10); err == nil || !strings.Contains(err.Error(), "runner ancestor") {
		t.Fatal("production accepted Worker ancestor", err)
	}
	if err := cycleQuiescentWith(root, 10, func(pid int) bool { return pid == 11 }, os.ReadFile); err != nil {
		t.Fatal("explicit test environment was not isolated", err)
	}
}

func TestProcZombieWaitsUntilKernelDisappearance(t *testing.T) {
	root := t.TempDir()
	for _, pid := range []string{"10", "11"} {
		if err := os.Mkdir(filepath.Join(root, pid), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "10", "stat"), []byte("10 (bridge) S 1 1"), 0600); err != nil {
		t.Fatal(err)
	}
	clock := time.Unix(100, 0)
	phase := 0
	reads := 0
	read := func(path string) ([]byte, error) {
		if filepath.Base(path) == "cmdline" {
			reads++
			if phase == 0 {
				return []byte{}, nil
			}
			return nil, &os.PathError{Op: "read", Path: path, Err: syscall.ESRCH}
		}
		return os.ReadFile(path)
	}
	probe := func() error { return cycleQuiescentWith(root, 10, nil, read) }
	if err := probe(); err == nil {
		t.Fatal("unreaped zombie was accepted")
	}
	err := awaitCycleQuiescent(probe, func(d time.Duration) { clock = clock.Add(d); phase++ }, func() time.Time { return clock }, time.Second)
	if err != nil || reads < 3 {
		t.Fatal("ESRCH after reaping did not release idle boundary", err)
	}
}

func TestAllocationOwnerRecoveryProtectsNonemptyUnownedData(t *testing.T) {
	root, tmp, _ := cycleFixture(t, "")
	for _, name := range []string{"cycle-empty", "cycle-pending", "cycle-unknown"} {
		if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	pending := filepath.Join(root, "cycle-pending")
	if err := os.WriteFile(filepath.Join(pending, "owner.pending"), []byte(cycleOwner), 0600); err != nil {
		t.Fatal(err)
	}
	if err := recoverInterruptedCycles(root, tmp); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "cycle-empty")); !os.IsNotExist(err) {
		t.Fatal("empty allocation shell retained")
	}
	if _, err := os.Stat(filepath.Join(pending, "interrupted")); err != nil {
		t.Fatal("original pending owner was not recovered")
	}
	// Recreate an unowned directory with actual data. It is never disposable.
	unknown := filepath.Join(root, "cycle-unknown")
	if err := os.Mkdir(unknown, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unknown, "original"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := recoverInterruptedCycles(root, tmp); err == nil {
		t.Fatal("unowned data accepted")
	}
	if b, err := os.ReadFile(filepath.Join(unknown, "original")); err != nil || string(b) != "keep" {
		t.Fatal("unowned original changed")
	}
}

func TestRealProcHealthProbeDoesNotLeakCompletedScratch(t *testing.T) {
	if os.Getenv("CC_CYCLE_LIVE_PROC_HELPER") == "1" {
		return
	}
	// Give the real helper the kernel process name used by the CI Worker.
	// The production nil-baseline guard must still reject it; only this test's
	// explicit startup baseline permits the framework process itself.
	helper := filepath.Join(t.TempDir(), "Runner.Worker")
	if err := os.Link(os.Args[0], helper); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(helper, "-test.run=^TestCycleLiveProcHelper$", "-test.timeout=10s")
	cmd.Env = append(os.Environ(), "CC_CYCLE_LIVE_PROC_HELPER=1")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("live proc integration: %v\n%s", err, b)
	}
}
