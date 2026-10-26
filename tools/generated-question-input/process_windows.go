//go:build windows && amd64

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

var inputKernel = syscall.NewLazyDLL("kernel32.dll")

type inputJobBasic struct {
	User, Kernel         int64
	Flags                uint32
	Min, Max             uintptr
	Active               uint32
	Affinity             uintptr
	Priority, Scheduling uint32
}
type inputJobLimits struct {
	Basic                                                      inputJobBasic
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}
type inputJobAccounting struct {
	User, Kernel, PeriodUser, PeriodKernel int64
	Faults, Total, Active, Terminated      uint32
}
type inputStartupEx struct {
	syscall.StartupInfo
	Attributes uintptr
}

func nativeInputPaths() (inputPaths, error) {
	token, err := syscall.OpenCurrentProcessToken()
	if err != nil {
		return inputPaths{}, errInputSource
	}
	defer token.Close()
	profile, err := token.GetUserProfileDirectory()
	if err != nil || !filepath.IsAbs(profile) {
		return inputPaths{}, errInputSource
	}
	buffer := make([]uint16, 32768)
	n, _, _ := inputKernel.NewProc("GetWindowsDirectoryW").Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n == 0 || n >= uintptr(len(buffer)) {
		return inputPaths{}, errInputSource
	}
	windows := syscall.UTF16ToString(buffer[:n])
	if !filepath.IsAbs(windows) {
		return inputPaths{}, errInputSource
	}
	return inputPaths{Profile: profile, Windows: windows}, nil
}

type nativeInputChild struct {
	input, output *os.File
	process, job  syscall.Handle
	closed        bool
	rootPID       uint32
	members       []syscall.Handle
}

func (p *nativeInputChild) Stdin() io.WriteCloser { return p.input }
func (p *nativeInputChild) Stdout() io.ReadCloser { return p.output }

// Capture handles before closing stdin or terminating the Job. An active-count
// transition alone can precede the last process handle becoming signaled.
func (p *nativeInputChild) PinMembers() error {
	if len(p.members) != 0 {
		return nil
	}
	buffer := make([]uintptr, 1026)
	ok, _, _ := inputKernel.NewProc("QueryInformationJobObject").Call(uintptr(p.job), 3, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer))*unsafe.Sizeof(buffer[0]), 0)
	if ok == 0 {
		return errInputSource
	}
	header := (*[2]uint32)(unsafe.Pointer(&buffer[0]))
	count := header[1]
	if count > 1024 || header[0] > count {
		return errInputSource
	}
	for _, pid := range buffer[1 : 1+count] {
		if uint32(pid) == p.rootPID {
			continue
		}
		h, err := syscall.OpenProcess(0x00100000|0x1000, false, uint32(pid))
		if err != nil {
			return errInputSource
		}
		var belongs int32
		valid, _, _ := inputKernel.NewProc("IsProcessInJob").Call(uintptr(h), uintptr(p.job), uintptr(unsafe.Pointer(&belongs)))
		if valid == 0 || belongs == 0 {
			_ = syscall.CloseHandle(h)
			return errInputSource
		}
		p.members = append(p.members, h)
	}
	return nil
}

func (p *nativeInputChild) membersExited() bool {
	for _, handle := range p.members {
		state, err := syscall.WaitForSingleObject(handle, 0)
		if err != nil || state != syscall.WAIT_OBJECT_0 {
			return false
		}
	}
	return true
}
func (p *nativeInputChild) active() (uint32, error) {
	var info inputJobAccounting
	ok, _, _ := inputKernel.NewProc("QueryInformationJobObject").Call(uintptr(p.job), 1, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), 0)
	if ok == 0 {
		return 0, errInputSource
	}
	return info.Active, nil
}

// Wait on retained handles and the Job's active count, not recycled PIDs.
func (p *nativeInputChild) Wait(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return errInputSource
		}
		state, err := syscall.WaitForSingleObject(p.process, 0)
		if err != nil {
			return errInputSource
		}
		active, err := p.active()
		if err != nil {
			return errInputSource
		}
		if state == syscall.WAIT_OBJECT_0 && active == 0 && p.membersExited() {
			var code uint32
			if syscall.GetExitCodeProcess(p.process, &code) != nil || code != 0 {
				return errInputSource
			}
			return nil
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errInputSource
		case <-timer.C:
		}
	}
}
func (p *nativeInputChild) Stop(ctx context.Context) error {
	ok, _, _ := inputKernel.NewProc("TerminateJobObject").Call(uintptr(p.job), 1)
	if ok == 0 {
		return errInputSource
	}
	for {
		if ctx.Err() != nil {
			return errInputSource
		}
		state, err := syscall.WaitForSingleObject(p.process, 0)
		if err != nil {
			return errInputSource
		}
		active, err := p.active()
		if err != nil {
			return errInputSource
		}
		if state == syscall.WAIT_OBJECT_0 && active == 0 && p.membersExited() {
			return nil
		}
		timer := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return errInputSource
		case <-timer.C:
		}
	}
}
func (p *nativeInputChild) Close() error {
	if p.closed {
		return nil
	}
	p.closed = true
	_ = p.input.Close()
	_ = p.output.Close()
	for _, handle := range p.members {
		_ = syscall.CloseHandle(handle)
	}
	a := syscall.CloseHandle(p.job)
	b := syscall.CloseHandle(p.process)
	if a != nil || b != nil {
		return errInputSource
	}
	return nil
}

func startInputChild(path string, args, env []string) (child inputChild, err error) {
	if !filepath.IsAbs(path) {
		return nil, errInputSource
	}
	j, _, _ := inputKernel.NewProc("CreateJobObjectW").Call(0, 0)
	if j == 0 {
		return nil, errInputSource
	}
	job := syscall.Handle(j)
	keepJob := false
	defer func() {
		if !keepJob {
			_ = syscall.CloseHandle(job)
		}
	}()
	limits := inputJobLimits{}
	limits.Basic.Flags = 0x2000
	ok, _, _ := inputKernel.NewProc("SetInformationJobObject").Call(j, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		return nil, errInputSource
	}
	inR, inW, e := os.Pipe()
	if e != nil {
		return nil, errInputSource
	}
	outR, outW, e := os.Pipe()
	if e != nil {
		_ = inR.Close()
		_ = inW.Close()
		return nil, errInputSource
	}
	keepPipes := false
	defer func() {
		_ = inR.Close()
		_ = outW.Close()
		if !keepPipes {
			_ = inW.Close()
			_ = outR.Close()
		}
	}()
	null, e := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if e != nil {
		return nil, errInputSource
	}
	defer null.Close()
	handles := []syscall.Handle{syscall.Handle(inR.Fd()), syscall.Handle(outW.Fd()), syscall.Handle(null.Fd())}
	for _, h := range handles {
		if syscall.SetHandleInformation(h, 1, 1) != nil {
			return nil, errInputSource
		}
	}
	initAttr := inputKernel.NewProc("InitializeProcThreadAttributeList")
	var size uintptr
	initAttr.Call(0, 2, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 || size > 64<<10 {
		return nil, errInputSource
	}
	attributes := make([]byte, size)
	attr := uintptr(unsafe.Pointer(&attributes[0]))
	ok, _, _ = initAttr.Call(attr, 2, 0, uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return nil, errInputSource
	}
	defer func() { inputKernel.NewProc("DeleteProcThreadAttributeList").Call(attr); runtime.KeepAlive(attributes) }()
	jobs := []syscall.Handle{job}
	var pins runtime.Pinner
	pins.Pin(&handles[0])
	pins.Pin(&jobs[0])
	defer pins.Unpin()
	update := inputKernel.NewProc("UpdateProcThreadAttribute")
	ok, _, _ = update.Call(attr, 0, 0x20002, uintptr(unsafe.Pointer(&handles[0])), uintptr(len(handles))*unsafe.Sizeof(handles[0]), 0, 0)
	if ok == 0 {
		return nil, errInputSource
	}
	// Attach the future tree atomically before the launcher can spawn Codex.
	ok, _, _ = update.Call(attr, 0, 0x2000D, uintptr(unsafe.Pointer(&jobs[0])), unsafe.Sizeof(jobs[0]), 0, 0)
	if ok == 0 {
		return nil, errInputSource
	}
	app, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return nil, errInputSource
	}
	argv := append([]string{path}, args...)
	for i := range argv {
		argv[i] = syscall.EscapeArg(argv[i])
	}
	line, e := syscall.UTF16FromString(strings.Join(argv, " "))
	if e != nil {
		return nil, errInputSource
	}
	dir, e := syscall.UTF16PtrFromString(filepath.Dir(path))
	if e != nil {
		return nil, errInputSource
	}
	values := append([]string(nil), env...)
	sort.Slice(values, func(i, j int) bool { return strings.ToUpper(values[i]) < strings.ToUpper(values[j]) })
	for _, value := range values {
		if strings.ContainsRune(value, 0) {
			return nil, errInputSource
		}
	}
	block := utf16.Encode([]rune(strings.Join(values, "\x00") + "\x00\x00"))
	si := inputStartupEx{StartupInfo: syscall.StartupInfo{Cb: uint32(unsafe.Sizeof(inputStartupEx{})), Flags: 0x100, StdInput: handles[0], StdOutput: handles[1], StdErr: handles[2]}, Attributes: attr}
	var pi syscall.ProcessInformation
	ok, _, _ = inputKernel.NewProc("CreateProcessW").Call(uintptr(unsafe.Pointer(app)), uintptr(unsafe.Pointer(&line[0])), 0, 0, 1, 0x08000000|0x80000|0x400, uintptr(unsafe.Pointer(&block[0])), uintptr(unsafe.Pointer(dir)), uintptr(unsafe.Pointer(&si)), uintptr(unsafe.Pointer(&pi)))
	runtime.KeepAlive(handles)
	runtime.KeepAlive(jobs)
	runtime.KeepAlive(block)
	runtime.KeepAlive(attributes)
	if ok == 0 {
		return nil, errInputSource
	}
	_ = syscall.CloseHandle(pi.Thread)
	keepJob = true
	keepPipes = true
	return &nativeInputChild{input: inW, output: outR, process: pi.Process, job: job, rootPID: pi.ProcessId}, nil
}
