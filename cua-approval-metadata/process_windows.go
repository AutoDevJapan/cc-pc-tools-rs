package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")

type basicLimits struct {
	ProcessTime, JobTime   int64
	Flags                  uint32
	MinWorking, MaxWorking uintptr
	ActiveProcesses        uint32
	Affinity               uintptr
	Priority, Scheduling   uint32
}

type extendedLimits struct {
	Basic                                                      basicLimits
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}

func hidden(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func contain(process *os.Process) (func(), error) {
	create := kernel.NewProc("CreateJobObjectW")
	set := kernel.NewProc("SetInformationJobObject")
	assign := kernel.NewProc("AssignProcessToJobObject")
	job, _, err := create.Call(0, 0)
	if job == 0 {
		return nil, err
	}
	closeJob := func() { _ = syscall.CloseHandle(syscall.Handle(job)) }
	limits := extendedLimits{Basic: basicLimits{Flags: 0x2000}} // KILL_ON_JOB_CLOSE
	ok, _, err := set.Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		closeJob()
		return nil, err
	}
	handle, err := syscall.OpenProcess(0x0100|0x0001, false, uint32(process.Pid)) // SET_QUOTA | TERMINATE
	if err != nil {
		closeJob()
		return nil, err
	}
	defer syscall.CloseHandle(handle)
	ok, _, err = assign.Call(job, uintptr(handle))
	if ok == 0 {
		closeJob()
		return nil, fmt.Errorf("assign child: %w", err)
	}
	return closeJob, nil
}
