package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

func hidden(c *exec.Cmd) {}
func group(c *exec.Cmd)  { c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// errGroupBindingUnknown means a process group number cannot be tied to a
// direct child that this process still owns, so no group operation is made.
var errGroupBindingUnknown = errors.New("process group is not bound to an unreaped direct child")

// A command started after group(c) leads its own process group, and its
// process ID is the group number. The kernel keeps that number reserved while
// the leader exists, including after it exits and until this process reaps it.
// Once the leader is reaped and no other member remains, the number can be
// given to an unrelated process, so a number seen after the reap must never
// choose a signal target. superviseGroup therefore waits for the leader without
// reaping it (waitid WNOWAIT), signals the group only during that window, and
// reaps the leader last. After the reap the number is used only for signal 0,
// which delivers nothing.
type groupSignaler func(groupID int, sig syscall.Signal) error

type groupCensus struct{ live, zombies int }

type groupSupervisor struct {
	signal  groupSignaler
	members func(leader int) (groupCensus, error)
	// afterExit, when set, runs once the leader has exited on its own and before
	// any member is signalled. It may wait for the members to end by themselves;
	// it never decides what is signalled or released.
	afterExit func(ctx context.Context)
	// afterReap is a test seam that runs between the reap and the kernel
	// absence check, where a reused group number would first be visible.
	afterReap func()
}

func signalGroup(groupID int, sig syscall.Signal) error { return syscall.Kill(-groupID, sig) }

// superviseGroup waits for the started direct child c, drains its whole
// process group, and reaps the child last. waitErr is the child's own result
// (or the context error). cleanupErr reports only a group that could not be
// proven owned, and never replaces waitErr. The kernel confirms that the group
// is absent before this returns.
func superviseGroup(ctx context.Context, c *exec.Cmd, grace, forceWait time.Duration) (waitErr, cleanupErr error) {
	return groupSupervisor{signal: signalGroup, members: groupMembers}.run(ctx, c, grace, forceWait)
}

// runCaptured runs the unstarted c with its standard output copied into out
// and its standard error discarded, and returns the command's primary outcome.
// It keeps the rule that exec.Cmd.WaitDelay applies to a plain Wait: output
// that a descendant still holds open once delay has passed since the command
// exited is distrusted and reported as exec.ErrWaitDelay. The group is drained
// afterwards, as for every supervised command, and that cleanup neither turns
// the outcome into a success nor replaces the command's own result.
func runCaptured(ctx context.Context, c *exec.Cmd, out io.Writer, delay time.Duration) error {
	stdout, err := c.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		return err
	}
	if err := c.Start(); err != nil {
		return err
	}
	var readers sync.WaitGroup
	readers.Add(2)
	go func() { defer readers.Done(); _, _ = io.Copy(out, stdout) }()
	go func() { defer readers.Done(); _, _ = io.Copy(io.Discard, stderr) }()
	copied := make(chan struct{})
	go func() { readers.Wait(); close(copied) }()
	held := false
	s := groupSupervisor{signal: signalGroup, members: groupMembers, afterExit: func(ctx context.Context) {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-copied:
		case <-timer.C:
			held = true
		case <-ctx.Done():
		}
	}}
	waitErr, cleanupErr := s.run(ctx, c, 3*time.Second, 15*time.Second)
	// Closing the read ends releases a reader that something still holds open.
	_ = stdout.Close()
	_ = stderr.Close()
	<-copied
	switch {
	case waitErr != nil:
		return waitErr
	case held:
		return exec.ErrWaitDelay
	}
	return cleanupErr
}

func (s groupSupervisor) run(ctx context.Context, c *exec.Cmd, grace, forceWait time.Duration) (waitErr, cleanupErr error) {
	leader, ok := ownedLeader(c)
	if !ok {
		return refuseGroup(c)
	}
	// Watch for the leader's exit without reaping it: while it stays unreaped
	// the kernel keeps its number reserved for this group.
	exited := make(chan error, 1)
	go func() {
		_, err := childState(leader, true)
		exited <- err
	}()
	joined := false
	select {
	case err := <-exited:
		joined = true
		if err != nil {
			cleanupErr = errGroupBindingUnknown
		}
	case <-ctx.Done():
	}
	if joined && cleanupErr == nil && s.afterExit != nil {
		s.afterExit(ctx)
	}
	if cleanupErr == nil {
		cleanupErr = s.drain(c, grace, forceWait)
	}
	if !joined {
		if cleanupErr != nil {
			_ = c.Process.Kill() // still our unreaped child, so no other process can be hit
		}
		<-exited
	}
	// Reap last. Only after this may the number belong to another process.
	waitErr = c.Wait()
	if s.afterReap != nil {
		s.afterReap()
	}
	if cleanupErr == nil {
		awaitKernelAbsence(leader, forceWait)
	}
	return waitErr, cleanupErr
}

// ownedLeader proves that c is a started, unreaped direct child that leads
// its own process group.
func ownedLeader(c *exec.Cmd) (int, bool) {
	if c == nil || c.Process == nil || c.ProcessState != nil {
		return 0, false
	}
	pid := c.Process.Pid
	if pid <= 1 || c.SysProcAttr == nil || !c.SysProcAttr.Setpgid || c.SysProcAttr.Pgid != 0 {
		return 0, false
	}
	if _, err := childState(pid, false); err != nil {
		return 0, false
	}
	return pid, true
}

// refuseGroup makes no group operation. A direct child that is still ours is
// terminated and reaped on its own so it is not left behind.
func refuseGroup(c *exec.Cmd) (waitErr, cleanupErr error) {
	if c != nil && c.Process != nil && c.ProcessState == nil && c.Process.Pid > 1 {
		if _, err := childState(c.Process.Pid, false); err == nil {
			_ = c.Process.Kill()
			waitErr = c.Wait()
		}
	}
	return waitErr, errGroupBindingUnknown
}

// childState reports through waitid(2) whether pid is a direct child of this
// process that has exited. WNOWAIT leaves an exited child unreaped. An error
// (ECHILD) means pid is not an unreaped child of this process.
func childState(pid int, block bool) (exited bool, err error) {
	// siginfo_t begins with si_signo. The kernel stores SIGCHLD there when it
	// reports a child and zero when WNOHANG found none ready, on every ABI.
	var info [128]byte
	flags := uintptr(syscall.WEXITED | syscall.WNOWAIT)
	if !block {
		flags |= syscall.WNOHANG
	}
	for {
		_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, 1 /* P_PID */, uintptr(pid), uintptr(unsafe.Pointer(&info[0])), flags, 0, 0)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return false, errno
		}
		return binary.NativeEndian.Uint32(info[:4]) == uint32(syscall.SIGCHLD), nil
	}
}

// drain terminates every member of the group while the unreaped leader still
// reserves its number. The /proc census only schedules the reap; it is never
// authority to release the target, which stays with the kernel absence check.
func (s groupSupervisor) drain(c *exec.Cmd, grace, forceWait time.Duration) error {
	leader := c.Process.Pid
	send := func(sig syscall.Signal) error {
		if c.ProcessState != nil {
			return errGroupBindingUnknown
		}
		if _, err := childState(leader, false); err != nil {
			return errGroupBindingUnknown
		}
		_ = s.signal(leader, sig)
		return nil
	}
	poll := func() (live, zombies int, known bool) {
		counts, err := s.members(leader)
		if err != nil {
			return 0, 0, false
		}
		live, zombies = counts.live, counts.zombies
		if exited, err := childState(leader, false); err == nil && !exited {
			live++
		}
		return live, zombies, true
	}
	if live, zombies, known := poll(); known && live == 0 && zombies == 0 {
		return nil
	}
	if err := send(syscall.SIGTERM); err != nil {
		return err
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
		if live, zombies, known := poll(); known && live == 0 && zombies == 0 {
			return nil
		}
	}
	if err := send(syscall.SIGKILL); err != nil {
		return err
	}
	deadline = time.Now().Add(forceWait)
	reported := false
	for {
		live, _, known := poll()
		if known && live == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			if !known {
				// The census is unavailable. SIGKILL was delivered while the
				// leader reserved the group; the kernel check decides release.
				return nil
			}
			if !reported {
				fmt.Fprintf(os.Stderr, "linux process group %d remains; retaining target until kernel absence\n", leader)
				reported = true
			}
			if err := send(syscall.SIGKILL); err != nil {
				return err
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// groupMembers counts the members of the leader's group other than the leader.
// Entries that vanish while being read have exited and are skipped.
func groupMembers(leader int) (groupCensus, error) {
	var census groupCensus
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return groupCensus{}, err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid == leader {
			continue
		}
		data, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			return groupCensus{}, err
		}
		end := strings.LastIndexByte(string(data), ')')
		if end < 0 {
			return groupCensus{}, errors.New("process status is unreadable")
		}
		fields := strings.Fields(string(data[end+1:]))
		if len(fields) < 3 {
			return groupCensus{}, errors.New("process status is unreadable")
		}
		groupID, err := strconv.Atoi(fields[2])
		if err != nil {
			return groupCensus{}, errors.New("process status is unreadable")
		}
		if groupID != leader {
			continue
		}
		if fields[0] == "Z" || fields[0] == "X" {
			census.zombies++
		} else {
			census.live++
		}
	}
	return census, nil
}

// awaitKernelAbsence keeps the target occupied until the kernel reports no
// such group. It only observes with signal 0, so a reused number cannot be hurt.
// Unreaped zombies and unknown states keep the target occupied past forceWait.
func awaitKernelAbsence(groupID int, forceWait time.Duration) {
	deadline := time.Now().Add(forceWait)
	reported := false
	for processGroupExists(groupID) {
		if !reported && time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "linux process group %d remains; retaining target until kernel absence\n", groupID)
			reported = true
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func processGroupExists(groupID int) bool {
	if groupID <= 0 {
		return true
	}
	err := syscall.Kill(-groupID, 0)
	return !errors.Is(err, syscall.ESRCH)
}
