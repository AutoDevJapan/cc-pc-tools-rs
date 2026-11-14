//go:build windows

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// treeStopLimit bounds stopping the processes that a command left behind and
// confirming that each of them has exited.
const treeStopLimit = 10 * time.Second

var (
	kernel32                      = windows.NewLazySystemDLL("kernel32.dll")
	procSetInformationJobObject   = kernel32.NewProc("SetInformationJobObject")
	procQueryInformationJobObject = kernel32.NewProc("QueryInformationJobObject")
	procIsProcessInJob            = kernel32.NewProc("IsProcessInJob")
)

// processIDList is JOBOBJECT_BASIC_PROCESS_ID_LIST with room for 256 processes.
type processIDList struct {
	assigned uint32
	listed   uint32
	ids      [256]uintptr
}

// runOwned runs cmd as the root of its own job object. The root starts
// suspended and joins the job before it runs, so every process it starts
// belongs to the job unless that process breaks away on purpose. Cancelling
// cmd stops the whole job. After the root has ended and its output has been
// drained, runOwned stops the processes left in the job and returns only
// after each of them has exited; it returns an error when it cannot confirm
// that.
func runOwned(cmd *exec.Cmd) (ownedResult, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return ownedResult{}, fmt.Errorf("create a job object: %w", err)
	}
	// Closing the job also stops anything that is still left in it.
	defer windows.CloseHandle(job)
	info := new(windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION)
	// Like the native fixtures' own jobs, allow explicit breakaway: a process
	// started with CREATE_BREAKAWAY_FROM_JOB keeps working outside the tree.
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE | windows.JOB_OBJECT_LIMIT_BREAKAWAY_OK
	if r, _, e := syscall.SyscallN(procSetInformationJobObject.Addr(), uintptr(job), uintptr(windows.JobObjectExtendedLimitInformation), uintptr(unsafe.Pointer(info)), unsafe.Sizeof(*info)); r == 0 {
		return ownedResult{}, fmt.Errorf("configure the job object: %w", e)
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_SUSPENDED
	cmd.Cancel = func() error { return windows.TerminateJobObject(job, 1) }
	if err := cmd.Start(); err != nil {
		return ownedResult{}, err
	}
	if err := adopt(job, cmd); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return ownedResult{}, err
	}
	var res ownedResult
	res.runErr = cmd.Wait()
	res.leftovers, err = drainJob(job, treeStopLimit)
	return res, err
}

// adopt places the suspended root in the job and then resumes it.
func adopt(job windows.Handle, cmd *exec.Cmd) error {
	var assignErr error
	if err := cmd.Process.WithHandle(func(handle uintptr) {
		assignErr = windows.AssignProcessToJobObject(job, windows.Handle(handle))
	}); err != nil {
		return fmt.Errorf("get the started process handle: %w", err)
	}
	if assignErr != nil {
		return fmt.Errorf("place the started process in its job object: %w", assignErr)
	}
	return resumeSuspended(uint32(cmd.Process.Pid))
}

// resumeSuspended resumes the threads of a process created suspended. A
// thread that another program added without suspending it is left as is. It
// fails unless at least one thread, the main thread, was suspended.
func resumeSuspended(pid uint32) error {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		return fmt.Errorf("list threads: %w", err)
	}
	defer windows.CloseHandle(snapshot)
	entry := windows.ThreadEntry32{Size: uint32(unsafe.Sizeof(windows.ThreadEntry32{}))}
	var threads []uint32
	for err = windows.Thread32First(snapshot, &entry); err == nil; err = windows.Thread32Next(snapshot, &entry) {
		if entry.OwnerProcessID == pid {
			threads = append(threads, entry.ThreadID)
		}
	}
	if !errors.Is(err, windows.ERROR_NO_MORE_FILES) {
		return fmt.Errorf("list threads: %w", err)
	}
	resumed := 0
	for _, id := range threads {
		thread, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, id)
		if err != nil {
			return fmt.Errorf("open thread %d: %w", id, err)
		}
		previous, err := windows.ResumeThread(thread)
		windows.CloseHandle(thread)
		if err != nil {
			return fmt.Errorf("resume thread %d: %w", id, err)
		}
		if previous > 0 {
			resumed++
		}
	}
	if resumed == 0 {
		return fmt.Errorf("the started process has %d threads and none was suspended", len(threads))
	}
	return nil
}

// drainJob stops the processes still in the job and waits, within limit,
// until each of them has exited. It holds a handle to every member it finds,
// checked to belong to the job, and waits on that handle, so a reused process
// ID cannot be mistaken for a stopped member. It returns the members' image
// names.
func drainJob(job windows.Handle, limit time.Duration) ([]string, error) {
	deadline := time.Now().Add(limit)
	var names []string
	held := make(map[uint32]windows.Handle)
	defer func() {
		for _, h := range held {
			windows.CloseHandle(h)
		}
	}()
	for {
		ids, err := jobProcessIDs(job)
		if err != nil {
			return names, err
		}
		if len(ids) == 0 {
			break
		}
		for _, id := range ids {
			if _, ok := held[id]; ok {
				continue
			}
			h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, id)
			if err != nil {
				continue // it has exited since the job listed it
			}
			if !inJob(h, job) {
				windows.CloseHandle(h)
				continue
			}
			held[id] = h
			names = append(names, imageName(h))
		}
		if err := windows.TerminateJobObject(job, 1); err != nil {
			return names, fmt.Errorf("stop the processes left in the job: %w", err)
		}
		if !time.Now().Before(deadline) {
			return names, fmt.Errorf("%d processes left in the job still ran %v after the root ended", len(ids), limit)
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, h := range held {
		wait := max(time.Until(deadline), 0)
		if event, err := windows.WaitForSingleObject(h, uint32(wait.Milliseconds())); err != nil || event != windows.WAIT_OBJECT_0 {
			return names, fmt.Errorf("a process stopped in the job did not exit within %v", limit)
		}
	}
	return names, nil
}

// jobProcessIDs lists the processes in the job.
func jobProcessIDs(job windows.Handle) ([]uint32, error) {
	list := new(processIDList)
	r, _, e := syscall.SyscallN(procQueryInformationJobObject.Addr(), uintptr(job), uintptr(windows.JobObjectBasicProcessIdList), uintptr(unsafe.Pointer(list)), unsafe.Sizeof(*list), 0)
	if r == 0 && e != windows.ERROR_MORE_DATA {
		return nil, fmt.Errorf("list the processes in the job: %w", e)
	}
	if list.assigned > list.listed {
		return nil, fmt.Errorf("the job holds %d processes, more than the %d this command follows", list.assigned, len(list.ids))
	}
	ids := make([]uint32, list.listed)
	for i := range ids {
		ids[i] = uint32(list.ids[i])
	}
	return ids, nil
}

// inJob reports whether the process belongs to the job.
func inJob(process, job windows.Handle) bool {
	var result int32
	r, _, _ := syscall.SyscallN(procIsProcessInJob.Addr(), uintptr(process), uintptr(job), uintptr(unsafe.Pointer(&result)))
	return r != 0 && result != 0
}

func imageName(process windows.Handle) string {
	buf := make([]uint16, 1024)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(process, 0, &buf[0], &size); err != nil {
		return "unknown"
	}
	return filepath.Base(windows.UTF16ToString(buf[:size]))
}
