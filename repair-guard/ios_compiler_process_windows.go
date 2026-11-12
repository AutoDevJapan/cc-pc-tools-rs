package main

import (
	"context"
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

type iosCompilerJobBasic struct {
	User, Kernel         int64
	Flags                uint32
	Min, Max             uintptr
	Active               uint32
	Affinity             uintptr
	Priority, Scheduling uint32
}
type iosCompilerJobLimits struct {
	Basic                                                      iosCompilerJobBasic
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}
type iosCompilerJobAccounting struct {
	User, Kernel, PeriodUser, PeriodKernel int64
	Faults, Total, Active, Terminated      uint32
}
type iosCompilerStartupEx struct {
	syscall.StartupInfo
	Attributes uintptr
}

var iosCompilerKernel = syscall.NewLazyDLL("kernel32.dll")

// JOB_LIST attaches the process and future gate/gh children atomically before
// execution. Killing only CommandContext's immediate gate is insufficient.
// This is limited to fixed metadata GETs; it starts no artifact or GUI.
func iosCompilerCommand(ctx context.Context, path string, args ...string) ([]byte, error) {
	return iosCompilerCommandObserved(ctx, path, args, nil)
}

func iosCompilerCommandObserved(ctx context.Context, path string, args []string, observe func(uint32, uint32)) ([]byte, error) {
	if ctx.Err() != nil || !filepath.IsAbs(path) {
		return nil, errIOSCompiler
	}
	j, _, _ := iosCompilerKernel.NewProc("CreateJobObjectW").Call(0, 0)
	if j == 0 {
		return nil, errIOSCompiler
	}
	defer syscall.CloseHandle(syscall.Handle(j))
	limits := iosCompilerJobLimits{}
	limits.Basic.Flags = 0x2000
	ok, _, _ := iosCompilerKernel.NewProc("SetInformationJobObject").Call(j, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		return nil, errIOSCompiler
	}
	r, w, err := os.Pipe()
	if err != nil {
		return nil, errIOSCompiler
	}
	defer r.Close()
	defer w.Close()
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, errIOSCompiler
	}
	defer null.Close()
	handles := []syscall.Handle{syscall.Handle(null.Fd()), syscall.Handle(w.Fd())}
	for _, h := range handles {
		if syscall.SetHandleInformation(h, 1, 1) != nil {
			return nil, errIOSCompiler
		}
	}
	init := iosCompilerKernel.NewProc("InitializeProcThreadAttributeList")
	var size uintptr
	init.Call(0, 2, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 || size > 64<<10 {
		return nil, errIOSCompiler
	}
	attributes := make([]byte, int(size))
	attr := uintptr(unsafe.Pointer(&attributes[0]))
	ok, _, _ = init.Call(attr, 2, 0, uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return nil, errIOSCompiler
	}
	defer func() {
		iosCompilerKernel.NewProc("DeleteProcThreadAttributeList").Call(attr)
		runtime.KeepAlive(attributes)
	}()
	jobs := []syscall.Handle{syscall.Handle(j)}
	var pins runtime.Pinner
	pins.Pin(&handles[0])
	pins.Pin(&jobs[0])
	defer pins.Unpin()
	update := iosCompilerKernel.NewProc("UpdateProcThreadAttribute")
	ok, _, _ = update.Call(attr, 0, 0x20002, uintptr(unsafe.Pointer(&handles[0])), uintptr(len(handles))*unsafe.Sizeof(handles[0]), 0, 0)
	if ok == 0 {
		return nil, errIOSCompiler
	}
	ok, _, _ = update.Call(attr, 0, 0x2000D, uintptr(unsafe.Pointer(&jobs[0])), unsafe.Sizeof(jobs[0]), 0, 0)
	if ok == 0 {
		return nil, errIOSCompiler
	}
	application, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, errIOSCompiler
	}
	argv := append([]string{path}, args...)
	for i := range argv {
		argv[i] = syscall.EscapeArg(argv[i])
	}
	line, err := syscall.UTF16FromString(strings.Join(argv, " "))
	if err != nil {
		return nil, errIOSCompiler
	}
	dir, err := syscall.UTF16PtrFromString(filepath.Dir(path))
	if err != nil {
		return nil, errIOSCompiler
	}
	env := official3894Env()
	sort.Slice(env, func(i, j int) bool { return strings.ToUpper(env[i]) < strings.ToUpper(env[j]) })
	for _, v := range env {
		if strings.ContainsRune(v, 0) {
			return nil, errIOSCompiler
		}
	}
	block := utf16.Encode([]rune(strings.Join(env, "\x00") + "\x00\x00"))
	defer clear(block)
	si := iosCompilerStartupEx{StartupInfo: syscall.StartupInfo{Cb: uint32(unsafe.Sizeof(iosCompilerStartupEx{})), Flags: 0x100, StdInput: handles[0], StdOutput: handles[1], StdErr: handles[0]}, Attributes: attr}
	var pi syscall.ProcessInformation
	ok, _, _ = iosCompilerKernel.NewProc("CreateProcessW").Call(uintptr(unsafe.Pointer(application)), uintptr(unsafe.Pointer(&line[0])), 0, 0, 1, 0x08000000|0x80000|0x400, uintptr(unsafe.Pointer(&block[0])), uintptr(unsafe.Pointer(dir)), uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	runtime.KeepAlive(handles)
	runtime.KeepAlive(jobs)
	runtime.KeepAlive(block)
	runtime.KeepAlive(attributes)
	if ok == 0 {
		return nil, errIOSCompiler
	}
	defer syscall.CloseHandle(pi.Process)
	defer syscall.CloseHandle(pi.Thread)
	w.Close()
	type captured struct {
		raw []byte
		err error
	}
	done := make(chan captured, 1)
	go func() {
		raw := make([]byte, 64<<10+1)
		n := 0
		var e error
		for n < len(raw) {
			var got int
			got, e = r.Read(raw[n:])
			n += got
			if e != nil {
				break
			}
			if got == 0 {
				e = errIOSCompiler
				break
			}
		}
		if n > 64<<10 {
			e = errIOSCompiler
		}
		done <- captured{raw[:n], e}
	}()
	failed := false
	for {
		state, e := syscall.WaitForSingleObject(pi.Process, 10)
		if e != nil {
			failed = true
			break
		}
		if state == 0 {
			break
		}
		if ctx.Err() != nil {
			failed = true
			break
		}
	}
	var code uint32
	if syscall.GetExitCodeProcess(pi.Process, &code) != nil || code != 0 {
		failed = true
	}
	var before iosCompilerJobAccounting
	if observe != nil {
		ok, _, _ = iosCompilerKernel.NewProc("QueryInformationJobObject").Call(j, 1, uintptr(unsafe.Pointer(&before)), unsafe.Sizeof(before), 0)
		if ok == 0 {
			failed = true
		}
	}
	// Successful root exit does not prove its descendants ended. Terminate the
	// exact owned job, then require the kernel's active count to reach zero.
	ok, _, _ = iosCompilerKernel.NewProc("TerminateJobObject").Call(j, 1)
	if ok == 0 {
		failed = true
	}
	deadline := time.Now().Add(3 * time.Second)
	gone := false
	for time.Now().Before(deadline) {
		var a iosCompilerJobAccounting
		ok, _, _ = iosCompilerKernel.NewProc("QueryInformationJobObject").Call(j, 1, uintptr(unsafe.Pointer(&a)), unsafe.Sizeof(a), 0)
		if ok == 0 {
			failed = true
			break
		}
		if a.Active == 0 {
			gone = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !gone {
		failed = true
	}
	if observe != nil {
		after := uint32(1)
		if gone {
			after = 0
		}
		observe(before.Active, after)
	}
	var output captured
	select {
	case output = <-done:
	case <-time.After(time.Second):
		r.Close()
		output = <-done
		failed = true
	}
	if failed || output.err != nil && output.err != io.EOF || ctx.Err() != nil {
		clear(output.raw)
		return nil, errIOSCompiler
	}
	return output.raw, nil
}
