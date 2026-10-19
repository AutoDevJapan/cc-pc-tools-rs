package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")

const createNoWindow = 0x08000000
const captureLimit = 1 << 20
const outputLimit = 8 << 20

func fileBusy(err error) bool {
	return errors.Is(err, syscall.Errno(32)) || errors.Is(err, syscall.Errno(33))
}

func runBroker(b broker) error {
	// Do not turn an ordinary CLI invocation into a second elevation route.
	admin, _, _ := syscall.NewLazyDLL("shell32.dll").NewProc("IsUserAnAdmin").Call()
	if admin == 0 {
		return errors.New("broker must run through the existing elevated CC-AdminBroker task")
	}
	lock, err := os.OpenFile(b.path("", "broker.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	var ov syscall.Overlapped
	ok, _, err := kernel.NewProc("LockFileEx").Call(lock.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
	if ok == 0 {
		if errors.Is(err, syscall.Errno(33)) { // The authorized broker is already serving.
			return nil
		}
		return err
	}
	defer kernel.NewProc("UnlockFileEx").Call(lock.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
	return b.serve(context.Background())
}

func startTask(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, `C:\Windows\System32\schtasks.exe`, "/Run", "/TN", "CC-AdminBroker")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	cmd.WaitDelay = 2 * time.Second
	return cmd.Run()
}

type jobLimits struct {
	UserTime, JobTime      int64
	Flags                  uint32
	MinWorking, MaxWorking uintptr
	ActiveProcessLimit     uint32
	Affinity               uintptr
	Priority, Scheduling   uint32
	IO                     [6]uint64
	ProcessMemory          uintptr
	JobMemory              uintptr
	PeakProcessMemory      uintptr
	PeakJobMemory          uintptr
}

type jobAccounting struct {
	Times                                 [4]int64
	PageFaults, Total, Active, Terminated uint32
}

type startupInfoEx struct {
	syscall.StartupInfo
	Attributes unsafe.Pointer
}

func winCall(proc string, args ...uintptr) error {
	ok, _, err := kernel.NewProc(proc).Call(args...)
	if ok == 0 {
		return fmt.Errorf("%s: %w", proc, err)
	}
	return nil
}

func drainJob(job, root syscall.Handle) error {
	deadline := time.Now().Add(5 * time.Second)
	var handles []syscall.Handle
	type identity struct {
		pid     uint32
		created uint64
	}
	seen := map[identity]bool{}
	defer func() {
		for _, h := range handles {
			syscall.CloseHandle(h)
		}
	}()
	// Keep verified handles before termination. A zero active-count can precede
	// the final process signal; querying the PID again afterwards is not proof.
	capture := func() error {
		for capacity := 64; capacity <= 65536; capacity *= 2 {
			// BASIC_PROCESS_ID_LIST: two DWORD counts followed by ULONG_PTR IDs.
			headerWords := 8 / int(unsafe.Sizeof(uintptr(0)))
			buffer := make([]uintptr, headerWords+capacity)
			ok, _, err := kernel.NewProc("QueryInformationJobObject").Call(uintptr(job), 3, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer))*unsafe.Sizeof(uintptr(0)), 0)
			if ok == 0 {
				if errors.Is(err, syscall.Errno(234)) {
					continue
				}
				return err
			}
			counts := (*[2]uint32)(unsafe.Pointer(&buffer[0]))
			if counts[1] > uint32(capacity) {
				return errors.New("invalid job process count")
			}
			for _, pid := range buffer[headerWords : headerWords+int(counts[1])] {
				h, err := syscall.OpenProcess(syscall.SYNCHRONIZE|0x1000, false, uint32(pid))
				if errors.Is(err, syscall.Errno(87)) {
					continue
				} // Already gone.
				if err != nil {
					return err
				}
				var belongs int32
				if err := winCall("IsProcessInJob", uintptr(h), uintptr(job), uintptr(unsafe.Pointer(&belongs))); err != nil {
					syscall.CloseHandle(h)
					return err
				}
				if belongs == 0 {
					syscall.CloseHandle(h)
					continue
				}
				var created, exited, kernelTime, userTime syscall.Filetime
				if err := syscall.GetProcessTimes(h, &created, &exited, &kernelTime, &userTime); err != nil {
					handles = append(handles, h)
					return err
				}
				id := identity{uint32(pid), uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)}
				if seen[id] {
					syscall.CloseHandle(h)
				} else {
					seen[id] = true
					handles = append(handles, h)
				}
			}
			return nil
		}
		return errors.New("job process list exceeds capture limit")
	}
	captureErr := capture()
	if err := winCall("TerminateJobObject", uintptr(job), 125); err != nil {
		return err
	}
	for {
		if err := capture(); err != nil {
			captureErr = errors.Join(captureErr, err)
		}
		var accounting jobAccounting
		if err := winCall("QueryInformationJobObject", uintptr(job), 1, uintptr(unsafe.Pointer(&accounting)), unsafe.Sizeof(accounting), 0); err != nil {
			return err
		}
		if accounting.Active == 0 {
			break
		}
		if !time.Now().Before(deadline) {
			return errors.New("request process tree did not finish within cleanup deadline")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, h := range append(handles, root) {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New("process handle cleanup deadline reached")
		}
		state, err := syscall.WaitForSingleObject(h, uint32(remaining.Milliseconds()+1))
		if err != nil {
			return err
		}
		if state != syscall.WAIT_OBJECT_0 {
			return errors.New("request process handle did not signal")
		}
	}
	return captureErr
}

// Use documented process attributes to place the process in its owned Job
// Object at creation, before any request code can spawn descendants. Explicit
// handle inheritance excludes all broker handles, including the queue lock.
func runProcess(ctx context.Context, exe string, args []string, dir string) (r result) {
	r.Exit = 125
	if ctx.Err() != nil {
		return result{Exit: 124, TimedOut: true, Stderr: "request deadline reached"}
	}
	jobValue, _, err := kernel.NewProc("CreateJobObjectW").Call(0, 0)
	if jobValue == 0 {
		r.Stderr = "request job creation failed"
		return r
	}
	job := syscall.Handle(jobValue)
	defer syscall.CloseHandle(job)
	limits := jobLimits{Flags: 0x2000} // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if err := winCall("SetInformationJobObject", uintptr(job), 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits)); err != nil {
		r.Stderr = "request job limits failed"
		return r
	}
	var files []*os.File
	defer func() {
		for _, f := range files {
			f.Close()
			os.Remove(f.Name())
		}
	}()
	for range 2 {
		f, err := os.CreateTemp(dir, ".broker-output-")
		if err != nil {
			r.Stderr = "request output setup failed"
			return r
		}
		files = append(files, f)
	}
	input, err := os.Open(os.DevNull)
	if err != nil {
		r.Stderr = "request input setup failed"
		return r
	}
	defer input.Close()
	current, err := syscall.GetCurrentProcess()
	if err != nil {
		r.Stderr = "current process handle unavailable"
		return r
	}
	var handles [3]syscall.Handle
	defer func() {
		for _, handle := range handles {
			if handle != 0 {
				syscall.CloseHandle(handle)
			}
		}
	}()
	for i, f := range []*os.File{input, files[0], files[1]} {
		if err := syscall.DuplicateHandle(current, syscall.Handle(f.Fd()), current, &handles[i], 0, true, syscall.DUPLICATE_SAME_ACCESS); err != nil {
			r.Stderr = "request handle setup failed"
			return r
		}
	}
	var size uintptr
	initialize := kernel.NewProc("InitializeProcThreadAttributeList")
	initialize.Call(0, 2, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		r.Stderr = "request attributes unavailable"
		return r
	}
	storage := make([]uintptr, (size+unsafe.Sizeof(uintptr(0))-1)/unsafe.Sizeof(uintptr(0)))
	attrs := unsafe.Pointer(&storage[0])
	if ok, _, _ := initialize.Call(uintptr(attrs), 2, 0, uintptr(unsafe.Pointer(&size))); ok == 0 {
		r.Stderr = "request attributes setup failed"
		return r
	}
	jobs := [1]syscall.Handle{job}
	defer func() {
		kernel.NewProc("DeleteProcThreadAttributeList").Call(uintptr(attrs))
		runtime.KeepAlive(storage)
		runtime.KeepAlive(handles)
		runtime.KeepAlive(jobs)
	}()
	if err := winCall("UpdateProcThreadAttribute", uintptr(attrs), 0, 0x20002, uintptr(unsafe.Pointer(&handles[0])), unsafe.Sizeof(handles), 0, 0); err != nil {
		r.Stderr = "request handle attributes failed"
		return r
	}
	if err := winCall("UpdateProcThreadAttribute", uintptr(attrs), 0, 0x2000d, uintptr(unsafe.Pointer(&jobs[0])), unsafe.Sizeof(jobs), 0, 0); err != nil {
		r.Stderr = "request job attributes failed"
		return r
	}
	si := startupInfoEx{Attributes: attrs}
	si.Cb = uint32(unsafe.Sizeof(si))
	si.Flags = syscall.STARTF_USESTDHANDLES | syscall.STARTF_USESHOWWINDOW
	si.StdInput, si.StdOutput, si.StdErr = handles[0], handles[1], handles[2]
	si.ShowWindow = 0
	app, err := syscall.UTF16PtrFromString(exe)
	if err != nil {
		r.Stderr = "invalid executable path"
		return r
	}
	command := []string{syscall.EscapeArg(exe)}
	for _, arg := range args {
		command = append(command, syscall.EscapeArg(arg))
	}
	line, err := syscall.UTF16PtrFromString(strings.Join(command, " "))
	if err != nil {
		r.Stderr = "invalid request command line"
		return r
	}
	cwd, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		r.Stderr = "invalid request directory"
		return r
	}
	var process syscall.ProcessInformation
	err = syscall.CreateProcess(app, line, nil, nil, true, createNoWindow|0x80000, nil, cwd, &si.StartupInfo, &process)
	runtime.KeepAlive(storage)
	runtime.KeepAlive(handles)
	runtime.KeepAlive(jobs)
	if err != nil {
		r.Stderr = fmt.Sprintf("request process creation failed (%v)", err)
		return r
	}
	defer syscall.CloseHandle(process.Process)
	defer syscall.CloseHandle(process.Thread)
	// The parent copies must close immediately so files can be flushed and read.
	for i, h := range handles {
		syscall.CloseHandle(h)
		handles[i] = 0
	}
	var reason string
	for {
		state, err := syscall.WaitForSingleObject(process.Process, 25)
		if err != nil {
			reason = "request process wait failed"
			break
		}
		if state == syscall.WAIT_OBJECT_0 {
			var code uint32
			if err := syscall.GetExitCodeProcess(process.Process, &code); err == nil {
				r.Exit = int(code)
			} else {
				reason = "request exit code unavailable"
			}
			break
		}
		if ctx.Err() != nil {
			r.Exit, r.TimedOut = 124, true
			reason = "request deadline reached"
			break
		}
		var size int64
		for _, f := range files {
			if info, err := f.Stat(); err == nil {
				size += info.Size()
			}
		}
		if size > outputLimit {
			r.Exit, r.Truncated = 125, true
			reason = "request output limit reached"
			break
		}
	}
	// Each request owns this entire tree, never any unrelated live process.
	if err := drainJob(job, process.Process); err != nil {
		r.Exit = 125
		reason = "request process cleanup failed"
	}
	outputs := []*string{&r.Stdout, &r.Stderr}
	for i, f := range files {
		data, err := io.ReadAll(io.NewSectionReader(f, 0, captureLimit+1))
		if err != nil {
			r.Exit = 125
			reason = "request output read failed"
		}
		if len(data) > captureLimit {
			data = data[:captureLimit]
			r.Truncated = true
		}
		*outputs[i] = string(data)
	}
	if reason != "" {
		r.Stderr += "\n" + reason
	}
	return r
}

func runScript(ctx context.Context, script string) result {
	return runProcess(ctx, `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script}, filepath.Dir(script))
}
