package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func cycleFixture(t *testing.T, script string) (string, string, string) {
	t.Helper()
	base := t.TempDir()
	root, tmp, runner := filepath.Join(base, "diag"), filepath.Join(base, "tmp"), filepath.Join(base, "runner")
	if parent := os.Getenv("CC_TEST_CYCLE_TMP_ROOT"); parent != "" {
		var err error
		tmp, err = os.MkdirTemp(parent, "todo3026-cycle-test-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(tmp); err != nil {
				t.Error(err)
			}
		})
	}
	for _, p := range []string{root, tmp, runner} {
		if err := os.Mkdir(p, 0700); err != nil && !os.IsExist(err) {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(runner, "run.sh"), []byte("#!/bin/sh\nset -eu\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	return root, tmp, runner
}

func TestCycleReclaimsOnlyItsCompletedScratch(t *testing.T) {
	for _, code := range []string{"0", "7"} {
		t.Run("exit-"+code, func(t *testing.T) {
			root, tmp, runner := cycleFixture(t, `test "$1" = --once
test "$CC_LINUX_RUNNER_MODE" = normal
test "$ACTIONS_RUNNER_HOOK_JOB_STARTED" = /opt/bridge/job-hook.sh
test "$TMPDIR" = "$TMP" && test "$TMP" = "$TEMP"
dd if=/dev/zero of="$TMPDIR/codeql-output" bs=1M count=2 2>/dev/null
printf diagnostic > _diag/Worker.log
exit `+code+"\n")
			if err := os.WriteFile(filepath.Join(tmp, "bridge-key"), []byte("keep-key"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(filepath.Join(runner, "_diag"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(runner, "_diag", "original.log"), []byte("keep-original"), 0600); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 12; i++ {
				result, err := runNormalCycle(root, tmp, runner, func() error { return nil })
				if err != nil || (result == 0) != (code == "0") {
					t.Fatalf("result=%d err=%v", result, err)
				}
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 8 {
				t.Fatalf("retention cycles=%d err=%v", len(entries), err)
			}
			files, err := os.ReadDir(tmp)
			if err != nil || len(files) != 1 || files[0].Name() != "bridge-key" {
				t.Fatalf("scratch leaked or key removed: %v %v", files, err)
			}
			b, err := os.ReadFile(filepath.Join(tmp, "bridge-key"))
			if err != nil || string(b) != "keep-key" {
				t.Fatal("key changed")
			}
			originals, err := filepath.Glob(filepath.Join(runner, "_diag.before-*", "original.log"))
			if err != nil || len(originals) != 1 {
				t.Fatal("original diagnostic lost")
			}
			b, err = os.ReadFile(originals[0])
			if err != nil || string(b) != "keep-original" {
				t.Fatal("original bytes changed")
			}
		})
	}
}

func TestCyclePreservesDataWhenAnotherWorkerRemains(t *testing.T) {
	root, tmp, runner := cycleFixture(t, `printf keep > "$TMPDIR/codeql-output"
printf diagnostic > _diag/Worker.log
`)
	probes := 0
	_, err := runNormalCycle(root, tmp, runner, func() error {
		probes++
		if probes == 2 {
			return errors.New("other worker")
		}
		return nil
	})
	if err == nil {
		t.Fatal("active worker accepted")
	}
	entries, _ := os.ReadDir(tmp)
	if len(entries) != 1 {
		t.Fatal("unfinished scratch removed")
	}
	cycles, _ := os.ReadDir(root)
	if len(cycles) != 1 {
		t.Fatal("diagnostic missing")
	}
	if _, err := os.Stat(filepath.Join(root, cycles[0].Name(), "complete")); !os.IsNotExist(err) {
		t.Fatal("unverified cycle marked complete")
	}
}

func TestCycleDrainsBackgroundWriterBeforeReclaimingScratch(t *testing.T) {
	root, tmp, runner := cycleFixture(t, `(sleep 30; printf late > "$TMPDIR/late-write") </dev/null >/dev/null 2>&1 &
printf '%s' "$!" > _diag/child-pid
`)
	probes := 0
	_, err := runNormalCycle(root, tmp, runner, func() error {
		probes++
		if probes == 2 {
			data, err := os.ReadFile(filepath.Join(runner, "_diag", "child-pid"))
			if err != nil {
				return err
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				return err
			}
			if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
				return errors.New("background writer still present before cleanup")
			}
			entries, err := os.ReadDir(tmp)
			if err != nil || len(entries) != 1 {
				return errors.New("scratch reclaimed before drain confirmation")
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(tmp)
	if err != nil || len(entries) != 0 {
		t.Fatal("completed scratch leaked")
	}
}

func TestCycleRejectsLiveProcessAndUnknownDiagLink(t *testing.T) {
	proc := t.TempDir()
	for pid, data := range map[string]string{"10": "10 (bridge) S 1 1", "11": "11 (Runner.Worker) S 1 1"} {
		if err := os.Mkdir(filepath.Join(proc, pid), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(proc, pid, "stat"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(proc, pid, "cmdline"), []byte("/runner/bin/Runner.Worker\x00spawnclient\x00"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := cycleQuiescentAt(proc, 10); err == nil {
		t.Fatal("live worker accepted")
	}
	root, tmp, runner := cycleFixture(t, "")
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(runner, "_diag")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := prepareCycle(root, tmp, runner); err == nil {
		t.Fatal("unknown diagnostic link replaced")
	}
	if target, err := os.Readlink(filepath.Join(runner, "_diag")); err != nil || target != external {
		t.Fatal("unknown link changed")
	}
}

func TestRetentionRejectsLinksWithoutTouchingTargets(t *testing.T) {
	root := t.TempDir()
	cycle := diagnosticFixture(t, root, "cycle-001", 1, true)
	external := filepath.Join(t.TempDir(), "original")
	if err := os.WriteFile(external, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(cycle, "external")); err != nil {
		t.Fatal(err)
	}
	if err := retainDiagnostics(root, 0, 0); err == nil {
		t.Fatal("link accepted")
	}
	if data, err := os.ReadFile(external); err != nil || string(data) != "keep" {
		t.Fatal("external target changed")
	}
}
