//go:build windows

package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"
)

type askJobLimits struct {
	User, Kernel                                               int64
	Flags                                                      uint32
	Min, Max                                                   uintptr
	Active                                                     uint32
	Affinity                                                   uintptr
	Priority, Scheduling                                       uint32
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}
type askJobAccounting struct {
	User, Kernel, PeriodUser, PeriodKernel int64
	Faults, Total, Active, Terminated      uint32
}
type askChildResult struct {
	Raw              []byte
	Exit             int
	TimedOut, Reaped bool
}

// Start suspended, assign the exact process to an owned kill-on-close job, then
// resume. This includes future Git children before any worker code executes.
// No goroutine performing native writes is abandoned by the parent.
func askOwnedChild(ctx context.Context, executable string, args ...string) (result askChildResult) {
	result.Exit = -1
	kernel := syscall.NewLazyDLL("kernel32.dll")
	job, _, _ := kernel.NewProc("CreateJobObjectW").Call(0, 0)
	if job == 0 {
		return
	}
	defer syscall.CloseHandle(syscall.Handle(job))
	limits := askJobLimits{Flags: 0x2000}
	ok, _, _ := kernel.NewProc("SetInformationJobObject").Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		return
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000 | 0x00000004}
	cmd.WaitDelay = 100 * time.Millisecond
	out := askGitOutput{limit: 65536}
	errOut := askGitOutput{limit: 65536}
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if cmd.Start() != nil {
		return
	}
	process, err := syscall.OpenProcess(0x0100|0x0001|0x0800, false, uint32(cmd.Process.Pid))
	assigned := false
	if err == nil {
		ok, _, _ = kernel.NewProc("AssignProcessToJobObject").Call(job, uintptr(process))
		assigned = ok != 0
		if assigned {
			code, _, _ := syscall.NewLazyDLL("ntdll.dll").NewProc("NtResumeProcess").Call(uintptr(process))
			if code != 0 {
				assigned = false
			}
		}
		syscall.CloseHandle(process)
	}
	if !assigned {
		_ = cmd.Process.Kill()
	}
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	// Pin members observed while the root is alive. Job accounting can reach
	// active0 shortly before a terminated process's handle becomes signalled.
	members := map[uint32]syscall.Handle{}
	defer func() {
		for _, h := range members {
			syscall.CloseHandle(h)
		}
	}()
	collect := func() {
		var ids [130]uintptr
		ok, _, _ := kernel.NewProc("QueryInformationJobObject").Call(job, 3, uintptr(unsafe.Pointer(&ids[0])), unsafe.Sizeof(ids), 0)
		if ok == 0 {
			return
		}
		n := *(*uint32)(unsafe.Add(unsafe.Pointer(&ids[0]), 4))
		if n > 128 {
			return
		}
		for _, pid := range ids[1 : n+1] {
			id := uint32(pid)
			if _, exists := members[id]; exists {
				continue
			}
			h, e := syscall.OpenProcess(syscall.SYNCHRONIZE|0x1000, false, id)
			if e != nil {
				continue
			}
			var belongs uint32
			ok, _, _ := kernel.NewProc("IsProcessInJob").Call(uintptr(h), job, uintptr(unsafe.Pointer(&belongs)))
			if ok != 0 && belongs != 0 {
				members[id] = h
			} else {
				syscall.CloseHandle(h)
			}
		}
	}
	var waitErr error
loop:
	for {
		collect()
		select {
		case waitErr = <-waited:
			break loop
		case <-time.After(5 * time.Millisecond):
		}
	}
	result.TimedOut = ctx.Err() != nil
	if cmd.ProcessState != nil {
		result.Exit = cmd.ProcessState.ExitCode()
	}
	// Wait is over before reconciliation. A root exit alone does not prove
	// descendants gone; terminate the exact job and require its active count0.
	killed, _, _ := kernel.NewProc("TerminateJobObject").Call(job, 1)
	deadline := time.Now().Add(2 * time.Second)
	for killed != 0 && time.Now().Before(deadline) {
		var a askJobAccounting
		ok, _, _ = kernel.NewProc("QueryInformationJobObject").Call(job, 1, uintptr(unsafe.Pointer(&a)), unsafe.Sizeof(a), 0)
		if ok == 0 {
			break
		}
		if a.Active == 0 {
			result.Reaped = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for _, h := range members {
		left := time.Until(deadline)
		if left < 0 {
			result.Reaped = false
			break
		}
		state, e := syscall.WaitForSingleObject(h, uint32(left/time.Millisecond))
		if e != nil || state != syscall.WAIT_OBJECT_0 {
			result.Reaped = false
		}
	}
	if assigned && result.Reaped && !result.TimedOut && (waitErr == nil || result.Exit == 1) && errOut.Len() == 0 {
		result.Raw = append([]byte(nil), out.Bytes()...)
	}
	clear(out.Bytes())
	clear(errOut.Bytes())
	return
}

// Ten seconds bounds the producer process tree, not each synchronous API.
// A failed/stopped producer is followed by a separate bounded read-only
// reconciliation. Unknown is preserved and can never authorize a switch.
func supervisedAskSoftware(prepare bool) updateResult {
	exe, err := os.Executable()
	if err != nil {
		return updateResult{Reason: "qualification_unavailable"}
	}
	operation := "verify"
	if prepare {
		operation = "prepare"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	child := askOwnedChild(ctx, exe, "--ask-software-worker", operation)
	cancel()
	defer clear(child.Raw)
	var r updateResult
	if child.Reaped && !child.TimedOut && json.Unmarshal(child.Raw, &r) == nil && child.Exit == 0 && r.Qualified && (!prepare || r.Prepared) {
		return r
	}
	if json.Unmarshal(child.Raw, &r) != nil {
		r = updateResult{Reason: "qualification_unavailable"}
	}
	r.Qualified, r.Prepared = false, false
	// A producer whose job could not be collected cannot safely be observed
	// as quiescent. Return explicit unknown without touching its output names.
	unknown := askReconciliation{Domain: "owner-question-fixed-software-reconcile-v1", Target: askReconciledObject{State: "unknown"}, Stage: askReconciledObject{State: "unknown"}, Backup: askReconciledObject{State: "unknown"}}
	r.Reconciliation = &unknown
	if !child.Reaped {
		return r
	}
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	observed := askOwnedChild(ctx, exe, "--ask-software-inspection-worker")
	cancel()
	defer clear(observed.Raw)
	var snapshot askReconciliation
	if observed.Reaped && !observed.TimedOut && observed.Exit == 0 && json.Unmarshal(observed.Raw, &snapshot) == nil && snapshot.Domain == unknown.Domain && !snapshot.SwitchAllowed {
		r.Reconciliation = &snapshot
	}
	return r
}

func runAskWorker(args []string, out io.Writer) int {
	if len(args) == 1 && args[0] == "--ask-software-inspection-worker" {
		return runAskReconciliation([]string{"--inspect-ask-privacy-update"}, out)
	}
	if len(args) != 2 || args[0] != "--ask-software-worker" || args[1] != "verify" && args[1] != "prepare" {
		return 2
	}
	r := performAskSoftware(args[1] == "prepare")
	if json.NewEncoder(out).Encode(r) != nil || !r.Qualified || args[1] == "prepare" && !r.Prepared {
		return 1
	}
	return 0
}
