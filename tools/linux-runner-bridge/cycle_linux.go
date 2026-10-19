package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const diagnosticRoot = "/work/bridge-diagnostics-todo3026"

func ownedDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || stat.Uid != uint32(os.Getuid()) {
		return errors.New("owned directory rejected")
	}
	return nil
}

// The kernel group drain is the authority for descendants of run.sh. Also
// refuse maintenance if an escaped process remains: it might use scratch later
// even when no file is currently open. Only the known idle container processes
// and this bridge's ancestors may remain. Unknown states retain all evidence.
func cycleQuiescent(procRoot string) error {
	return cycleQuiescentAt(procRoot, os.Getpid())
}

func cycleQuiescentAt(procRoot string, self int) error {
	allowed := map[int]bool{1: true, self: true}
	pid := self
	visited := map[int]bool{}
	for pid > 1 {
		if visited[pid] {
			return errors.New("process ancestry cycle rejected")
		}
		visited[pid] = true
		data, err := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "stat"))
		if err != nil {
			return err
		}
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			return errors.New("process stat rejected")
		}
		start := strings.IndexByte(string(data), '(')
		if start < 0 || start >= end {
			return errors.New("process name rejected")
		}
		name := string(data[start+1 : end])
		if name == "Runner.Worker" || name == "Runner.Listener" {
			return errors.New("runner ancestor is active; retaining data")
		}
		fields := strings.Fields(string(data[end+1:]))
		if len(fields) < 2 {
			return errors.New("process stat incomplete")
		}
		pid, err = strconv.Atoi(fields[1])
		if err != nil {
			return err
		}
		allowed[pid] = true
	}
	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || allowed[pid] {
			continue
		}
		data, err := os.ReadFile(filepath.Join(procRoot, entry.Name(), "cmdline"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		args := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
		// runner-init's original, idle process is sleep infinity. It cannot
		// create files or launch a new worker. No other bridge is exempted.
		if len(args) == 2 && args[0] == "sleep" && args[1] == "infinity" {
			continue
		}
		return errors.New("other process remains; retaining cycle data")
	}
	return nil
}

func prepareCycle(root, tmpRoot, runnerRoot string) (string, string, error) {
	for _, parent := range []string{root, runnerRoot} {
		if err := ownedDirectory(parent); err != nil {
			return "", "", err
		}
	}
	infoTmp, err := os.Lstat(tmpRoot)
	if err != nil || !infoTmp.IsDir() || infoTmp.Mode()&os.ModeSymlink != 0 {
		return "", "", errors.New("scratch parent rejected")
	}
	cycle, err := os.MkdirTemp(root, "cycle-"+time.Now().UTC().Format("20060102T150405.000000000")+"-")
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(filepath.Join(cycle, "owner"), []byte(cycleOwner), 0600); err != nil {
		return "", "", err
	}
	scratch := filepath.Join(tmpRoot, "bridge-"+filepath.Base(cycle))
	if err := os.Mkdir(scratch, 0700); err != nil {
		return "", "", err
	}
	diag := filepath.Join(runnerRoot, "_diag")
	info, err := os.Lstat(diag)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			target, e := os.Readlink(diag)
			if e != nil || filepath.Dir(target) != root || !strings.HasPrefix(filepath.Base(target), "cycle-") {
				return "", "", errors.New("unknown diagnostic link retained")
			}
			if err = os.Remove(diag); err != nil {
				return "", "", err
			}
		} else {
			// Configuration diagnostics predate the first cycle. Keep the
			// directory intact, on the same filesystem, without reading it.
			if !info.IsDir() {
				return "", "", errors.New("unknown diagnostic path retained")
			}
			if err = os.Rename(diag, diag+".before-"+filepath.Base(cycle)); err != nil {
				return "", "", err
			}
		}
	} else if !os.IsNotExist(err) {
		return "", "", err
	}
	if err = os.Symlink(cycle, diag); err != nil {
		return "", "", err
	}
	return cycle, scratch, nil
}

func activeCycleScratch(root, tmpRoot, runnerRoot string) (string, error) {
	if err := ownedDirectory(root); err != nil {
		return "", err
	}
	target, err := os.Readlink(filepath.Join(runnerRoot, "_diag"))
	if err != nil || filepath.Dir(target) != root || !strings.HasPrefix(filepath.Base(target), "cycle-") {
		return "", errors.New("active diagnostic cycle rejected")
	}
	if err := ownedDirectory(target); err != nil {
		return "", err
	}
	info, err := os.Lstat(filepath.Join(target, "owner"))
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("active cycle owner rejected")
	}
	owner, err := os.ReadFile(filepath.Join(target, "owner"))
	if err != nil || string(owner) != cycleOwner {
		return "", errors.New("active cycle owner mismatch")
	}
	if _, err := os.Lstat(filepath.Join(target, "complete")); !os.IsNotExist(err) {
		return "", errors.New("cycle is not active")
	}
	scratch := filepath.Join(tmpRoot, "bridge-"+filepath.Base(target))
	if err := ownedDirectory(scratch); err != nil {
		return "", err
	}
	return scratch, nil
}

func cargoScratchEnv(mode string) ([]string, error) {
	if mode != "normal" {
		return nil, nil
	}
	scratch, err := activeCycleScratch(diagnosticRoot, "/tmp", "/runner")
	if err != nil {
		return nil, err
	}
	return []string{"TMPDIR=" + scratch, "TMP=" + scratch, "TEMP=" + scratch}, nil
}

func completeCycle(cycle, scratch string) error {
	if err := ownedDirectory(cycle); err != nil {
		return err
	}
	if err := ownedDirectory(scratch); err != nil {
		return err
	}
	marker, err := os.ReadFile(filepath.Join(cycle, "owner"))
	if err != nil || string(marker) != cycleOwner {
		return errors.New("cycle owner rejected")
	}
	if err := os.RemoveAll(scratch); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cycle, "complete"), []byte(cycleOwner), 0600)
}

func runNormalCycle(root, tmpRoot, runnerRoot string, quiescent func() error) (int, error) {
	if err := quiescent(); err != nil {
		return 1, err
	}
	if err := retainDiagnostics(root, diagnosticBudget, diagnosticCount); err != nil {
		return 1, err
	}
	cycle, scratch, err := prepareCycle(root, tmpRoot, runnerRoot)
	if err != nil {
		return 1, err
	}
	c := command(filepath.Join(runnerRoot, "run.sh"), "--once")
	c.Dir = runnerRoot
	c.Env = append(os.Environ(), "CC_LINUX_RUNNER_MODE=normal", "ACTIONS_RUNNER_HOOK_JOB_STARTED=/opt/bridge/job-hook.sh", "TMPDIR="+scratch, "TMP="+scratch, "TEMP="+scratch)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	group(c)
	if err := c.Start(); err != nil {
		return 1, err
	}
	result := c.Wait()
	if err := terminateAndDrainGroup(c, 3*time.Second, 15*time.Second); err != nil {
		return 1, err
	}
	if err := quiescent(); err != nil {
		return 1, err
	}
	if err := completeCycle(cycle, scratch); err != nil {
		return 1, err
	}
	if err := retainDiagnostics(root, diagnosticBudget, diagnosticCount); err != nil {
		return 1, err
	}
	return exitCode(result), nil
}

func superviseNormal() int {
	if os.Getuid() != 1001 {
		return 1
	}
	// Single nonblocking kernel lock, including between two one-job sessions.
	fd, err := syscall.Open("/tmp/bridge-supervisor.lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return 1
	}
	defer syscall.Close(fd)
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return 1
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	if err := os.Mkdir(diagnosticRoot, 0700); err != nil && !os.IsExist(err) {
		return 1
	}
	if err := ownedDirectory(diagnosticRoot); err != nil {
		return 1
	}
	for {
		code, err := runNormalCycle(diagnosticRoot, "/tmp", "/runner", func() error { return cycleQuiescent("/proc") })
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if code != 0 {
			fmt.Fprintf(os.Stderr, "linux-runner-cycle code=%d; retrying after 30 seconds\n", code)
			time.Sleep(30 * time.Second)
		}
	}
}
