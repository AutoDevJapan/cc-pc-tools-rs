//go:build windows && amd64

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

type queueJobBasic struct {
	User, Kernel         int64
	Flags                uint32
	Min, Max             uintptr
	Active               uint32
	Affinity             uintptr
	Priority, Scheduling uint32
}
type queueJobLimits struct {
	Basic                                                      queueJobBasic
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}
type queueJobAccounting struct {
	User, Kernel, PeriodUser, PeriodKernel int64
	Faults, Total, Active, Terminated      uint32
}
type queueStartupEx struct {
	syscall.StartupInfo
	Attributes uintptr
}

func closeOwnedHandle(h *syscall.Handle) {
	if *h != 0 {
		syscall.CloseHandle(*h)
		*h = 0
	}
}

// The parent retains the Job and process handles. JOB_LIST attaches the helper
// atomically at creation, so even cargo_gate's first child cannot escape. Only
// the stdin/stdout pipe ends are inherited; no token is a file or argument.
func runOwnedExe(ctx context.Context, path string, args []string, input []byte, onStart func(ProcID)) (res StepResult, err error) {
	res.Exit = -1
	res.TreeGone = true // No process exists until CreateProcess succeeds.
	if len(input) > 4096 {
		return res, errors.New("owned helper input exceeds limit")
	}
	if e := ctx.Err(); e != nil {
		return res, e
	}
	j, _, e := kernel32.NewProc("CreateJobObjectW").Call(0, 0)
	if j == 0 {
		return res, e
	}
	job := syscall.Handle(j)
	defer closeOwnedHandle(&job)
	limits := queueJobLimits{}
	limits.Basic.Flags = 0x2000
	ok, _, e := kernel32.NewProc("SetInformationJobObject").Call(j, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		return res, e
	}
	inputReader, w, e := os.Pipe()
	if e != nil {
		return res, e
	}
	defer inputReader.Close()
	defer w.Close()
	r, outputWriter, e := os.Pipe()
	if e != nil {
		return res, e
	}
	defer r.Close()
	defer outputWriter.Close()
	inR, outW := syscall.Handle(inputReader.Fd()), syscall.Handle(outputWriter.Fd())
	for _, h := range []syscall.Handle{inR, outW} {
		if e := syscall.SetHandleInformation(h, 1, 1); e != nil {
			return res, e
		}
	}
	var size uintptr
	initAttr := kernel32.NewProc("InitializeProcThreadAttributeList")
	initAttr.Call(0, 2, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 || size > 64<<10 {
		return res, errors.New("process attribute size rejected")
	}
	attributes := make([]byte, size)
	attr := uintptr(unsafe.Pointer(&attributes[0]))
	ok, _, e = initAttr.Call(attr, 2, 0, uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return res, e
	}
	defer func() {
		if attr != 0 {
			kernel32.NewProc("DeleteProcThreadAttributeList").Call(attr)
		}
		runtime.KeepAlive(attributes)
	}()
	handles := []syscall.Handle{inR, outW}
	jobs := []syscall.Handle{job}
	update := kernel32.NewProc("UpdateProcThreadAttribute")
	ok, _, e = update.Call(attr, 0, 0x20002, uintptr(unsafe.Pointer(&handles[0])), uintptr(len(handles))*unsafe.Sizeof(handles[0]), 0, 0)
	if ok == 0 {
		return res, e
	}
	ok, _, e = update.Call(attr, 0, 0x2000D, uintptr(unsafe.Pointer(&jobs[0])), unsafe.Sizeof(jobs[0]), 0, 0)
	if ok == 0 {
		return res, e
	}
	app, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return res, e
	}
	argv := []string{path}
	argv = append(argv, args...)
	for i := range argv {
		argv[i] = syscall.EscapeArg(argv[i])
	}
	line, e := syscall.UTF16FromString(strings.Join(argv, " "))
	if e != nil {
		return res, e
	}
	dir, e := syscall.UTF16PtrFromString(filepath.Dir(path))
	if e != nil {
		return res, e
	}
	env := scrubbedEnv()
	sort.Slice(env, func(i, k int) bool { return strings.ToUpper(env[i]) < strings.ToUpper(env[k]) })
	for _, v := range env {
		if strings.ContainsRune(v, 0) {
			return res, errors.New("environment rejected")
		}
	}
	block := utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00"))
	si := queueStartupEx{StartupInfo: syscall.StartupInfo{Cb: uint32(unsafe.Sizeof(queueStartupEx{})), Flags: 0x100, StdInput: inR, StdOutput: outW, StdErr: outW}, Attributes: attr}
	var pi syscall.ProcessInformation
	ok, _, e = kernel32.NewProc("CreateProcessW").Call(uintptr(unsafe.Pointer(app)), uintptr(unsafe.Pointer(&line[0])), 0, 0, 1, 0x08000000|0x80000|0x400, uintptr(unsafe.Pointer(&block[0])), uintptr(unsafe.Pointer(dir)), uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	kernel32.NewProc("DeleteProcThreadAttributeList").Call(attr)
	attr = 0
	runtime.KeepAlive(handles)
	runtime.KeepAlive(jobs)
	runtime.KeepAlive(attributes)
	runtime.KeepAlive(block)
	if ok == 0 {
		return res, fmt.Errorf("owned helper creation failed: %w", e)
	}
	res.TreeGone = false
	defer syscall.CloseHandle(pi.Process)
	defer syscall.CloseHandle(pi.Thread)
	inputReader.Close()
	outputWriter.Close()
	if onStart != nil {
		var start, exit, kt, ut syscall.Filetime
		ok, _, _ := kernel32.NewProc("GetProcessTimes").Call(uintptr(pi.Process), uintptr(unsafe.Pointer(&start)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kt)), uintptr(unsafe.Pointer(&ut)))
		if ok != 0 {
			onStart(ProcID{PID: pi.ProcessId, Created: uint64(start.HighDateTime)<<32 | uint64(start.LowDateTime)})
		}
	}
	inputDone := make(chan struct{})
	payload := append([]byte(nil), input...)
	go func() { defer clear(payload); io.Copy(w, bytes.NewReader(payload)); w.Close(); close(inputDone) }()
	out := &limitWriter{limit: maxStepOutput}
	outputDone := make(chan struct{})
	go func() { io.Copy(out, r); close(outputDone) }()
	for {
		s, e := syscall.WaitForSingleObject(pi.Process, 20)
		if e != nil {
			err = e
			break
		}
		if s == 0 {
			break
		}
		if ctx.Err() != nil {
			err = ctx.Err()
			break
		}
	}
	members, snapshotErr := ownedProcessMembers(j)
	defer func() {
		for _, h := range members {
			syscall.CloseHandle(h)
		}
	}()
	ok, _, stopErr := kernel32.NewProc("TerminateJobObject").Call(j, 125)
	if ok != 0 {
		stopErr = nil
	}
	drainErr := waitOwnedProcesses(j, pi.Process, members, 30*time.Second)
	res.TreeGone = snapshotErr == nil && stopErr == nil && drainErr == nil
	var code uint32
	if e := syscall.GetExitCodeProcess(pi.Process, &code); e == nil {
		res.Exit = int(code)
	} else {
		err = errors.Join(err, e)
	}
	select {
	case <-inputDone:
	case <-time.After(2 * time.Second):
		w.Close()
		res.TreeGone = false
	}
	select {
	case <-outputDone:
		res.Output = append([]byte(nil), out.b.Bytes()...)
		if out.overflow && res.Exit == 0 {
			res.Exit = -1
		}
	case <-time.After(2 * time.Second):
		r.Close()
		res.TreeGone = false
	}
	err = errors.Join(err, ctx.Err(), snapshotErr, stopErr, drainErr)
	return res, err
}

func ownedProcessMembers(job uintptr) ([]syscall.Handle, error) {
	for slots := 64; slots <= 4096; slots *= 2 {
		b := make([]uintptr, slots+1)
		r, _, e := kernel32.NewProc("QueryInformationJobObject").Call(job, 3, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b))*unsafe.Sizeof(b[0]), 0)
		if r == 0 {
			if e == syscall.Errno(234) {
				continue
			}
			return nil, e
		}
		n := *(*uint32)(unsafe.Add(unsafe.Pointer(&b[0]), 4))
		if n > uint32(slots) {
			continue
		}
		var hs []syscall.Handle
		fail := func(e error) ([]syscall.Handle, error) {
			for _, h := range hs {
				syscall.CloseHandle(h)
			}
			return nil, e
		}
		for _, pid := range b[1 : 1+int(n)] {
			h, e := syscall.OpenProcess(0x100000|0x1000, false, uint32(pid))
			if e != nil {
				if e == syscall.Errno(87) {
					continue
				}
				return fail(e)
			}
			var member uint32
			r, _, e := kernel32.NewProc("IsProcessInJob").Call(uintptr(h), job, uintptr(unsafe.Pointer(&member)))
			if r == 0 {
				syscall.CloseHandle(h)
				return fail(e)
			}
			if member == 0 {
				syscall.CloseHandle(h)
				continue
			}
			hs = append(hs, h)
		}
		return hs, nil
	}
	return nil, errors.New("owned member inventory exceeds limit")
}

func waitOwnedProcesses(job uintptr, root syscall.Handle, members []syscall.Handle, budget time.Duration) error {
	until := time.Now().Add(budget)
	for time.Now().Before(until) {
		var a queueJobAccounting
		r, _, e := kernel32.NewProc("QueryInformationJobObject").Call(job, 1, uintptr(unsafe.Pointer(&a)), unsafe.Sizeof(a), 0)
		if r == 0 {
			return e
		}
		s, e := syscall.WaitForSingleObject(root, 0)
		if e != nil {
			return e
		}
		all := s == 0 && a.Active == 0
		for _, h := range members {
			s, e := syscall.WaitForSingleObject(h, 0)
			if e != nil {
				return e
			}
			all = all && s == 0
		}
		if all {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return errors.New("owned process tree drain deadline exceeded")
}
