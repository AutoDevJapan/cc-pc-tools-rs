package main

import (
	"bufio"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
	boundaries := 0
	_, err := runNormalCycle(root, tmp, runner, func() error {
		boundaries++
		if boundaries == 1 {
			return cycleQuiescent("/proc")
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
			err := cycleQuiescent("/proc")
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

func TestRealProcHealthProbeDoesNotLeakCompletedScratch(t *testing.T) {
	if os.Getenv("CC_CYCLE_LIVE_PROC_HELPER") == "1" {
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestCycleLiveProcHelper$", "-test.timeout=10s")
	cmd.Env = append(os.Environ(), "CC_CYCLE_LIVE_PROC_HELPER=1")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("live proc integration: %v\n%s", err, b)
	}
}
