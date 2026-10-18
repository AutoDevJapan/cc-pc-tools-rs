//go:build windows && amd64

package windowsrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Same suspended/assign/resume protocol and kill-on-close/accounting drain as
// pc-ci/service_windows.go, without its host token or host runner operations.
// We retain process handles, never use a PID as proof that the tree is alive.
var kernel = syscall.NewLazyDLL("kernel32.dll")
var createJob = kernel.NewProc("CreateJobObjectW")
var setJob = kernel.NewProc("SetInformationJobObject")
var assignJob = kernel.NewProc("AssignProcessToJobObject")
var terminateJob = kernel.NewProc("TerminateJobObject")
var queryJob = kernel.NewProc("QueryInformationJobObject")
var resumeThread = kernel.NewProc("ResumeThread")
var inJob = kernel.NewProc("IsProcessInJob")
var initializeAttributes = kernel.NewProc("InitializeProcThreadAttributeList")
var updateAttribute = kernel.NewProc("UpdateProcThreadAttribute")
var deleteAttributes = kernel.NewProc("DeleteProcThreadAttributeList")

type startupInfoEx struct {
	syscall.StartupInfo
	Attributes uintptr
}

type jobBasicLimits struct {
	ProcessTime, JobTime int64
	Flags                uint32
	MinWS, MaxWS         uintptr
	Active               uint32
	Affinity             uintptr
	Priority, Scheduling uint32
}
type jobExtendedLimits struct {
	Basic                                                      jobBasicLimits
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}
type jobAccounting struct {
	User, Kernel, PeriodUser, PeriodKernel int64
	Faults, Total, Active, Terminated      uint32
}

// runTree is private. All production callers construct a fixed program/command
// from compiled paths and validated scalar plan values, never from the wire.
func runTree(ctx context.Context, exe, line, dir string, env []string, privateLog string) processResult {
	result := processResult{}
	job, _, _ := createJob.Call(0, 0)
	if job == 0 {
		result.err = errors.New("process job unavailable")
		return result
	}
	defer syscall.CloseHandle(syscall.Handle(job))
	limits := jobExtendedLimits{}
	limits.Basic.Flags = 0x2000
	if ok, _, _ := setJob.Call(job, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits)); ok == 0 {
		result.err = errors.New("process job limits failed")
		return result
	}
	r, w, e := os.Pipe()
	if e != nil {
		result.err = errors.New("process log pipe failed")
		return result
	}
	defer r.Close()
	defer w.Close()
	if syscall.SetHandleInformation(syscall.Handle(w.Fd()), 1, 1) != nil {
		result.err = errors.New("process pipe inheritance failed")
		return result
	}
	defer syscall.SetHandleInformation(syscall.Handle(w.Fd()), 1, 0)
	nul, e := os.Open(os.DevNull)
	if e != nil {
		result.err = e
		return result
	}
	defer nul.Close()
	if syscall.SetHandleInformation(syscall.Handle(nul.Fd()), 1, 1) != nil {
		result.err = errors.New("process input inheritance failed")
		return result
	}
	defer syscall.SetHandleInformation(syscall.Handle(nul.Fd()), 1, 0)
	var log *os.File
	if privateLog != "" {
		log, e = os.OpenFile(privateLog, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			result.err = errors.New("private log creation failed")
			return result
		}
		defer log.Close()
	}
	app, e := syscall.UTF16PtrFromString(exe)
	if e != nil {
		result.err = e
		return result
	}
	command, e := syscall.UTF16PtrFromString(line)
	if e != nil {
		result.err = e
		return result
	}
	cwd, e := syscall.UTF16PtrFromString(dir)
	if e != nil {
		result.err = e
		return result
	}
	env = append([]string(nil), env...)
	sort.Slice(env, func(i, j int) bool { return strings.ToUpper(env[i]) < strings.ToUpper(env[j]) })
	var block []uint16
	for _, v := range env {
		part, err := syscall.UTF16FromString(v)
		if err != nil {
			result.err = errors.New("environment rejected")
			return result
		}
		block = append(block, part...)
	}
	block = append(block, 0)
	if len(block) == 1 {
		block = append(block, 0)
	}
	defer clear(block)
	// Restrict inheritance to stdout/stderr and NUL input. COM, TLS and any
	// simultaneous service handles must never enter the stock runner tree.
	var size uintptr
	initializeAttributes.Call(0, 1, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 || size > 1<<20 {
		result.err = errors.New("process attribute size rejected")
		return result
	}
	attrs := make([]byte, size)
	if ok, _, _ := initializeAttributes.Call(uintptr(unsafe.Pointer(&attrs[0])), 1, 0, uintptr(unsafe.Pointer(&size))); ok == 0 {
		result.err = errors.New("process attributes failed")
		return result
	}
	defer deleteAttributes.Call(uintptr(unsafe.Pointer(&attrs[0])))
	handles := []syscall.Handle{syscall.Handle(nul.Fd()), syscall.Handle(w.Fd())}
	if ok, _, _ := updateAttribute.Call(uintptr(unsafe.Pointer(&attrs[0])), 0, 0x20002, uintptr(unsafe.Pointer(&handles[0])), uintptr(len(handles))*unsafe.Sizeof(handles[0]), 0, 0); ok == 0 {
		result.err = errors.New("process handle allowlist failed")
		return result
	}
	si := startupInfoEx{StartupInfo: syscall.StartupInfo{Cb: uint32(unsafe.Sizeof(startupInfoEx{})), Flags: 0x100, StdInput: handles[0], StdOutput: handles[1], StdErr: handles[1]}, Attributes: uintptr(unsafe.Pointer(&attrs[0]))}
	var pi syscall.ProcessInformation
	// CREATE_NO_WINDOW | CREATE_SUSPENDED | CREATE_UNICODE_ENVIRONMENT
	e = syscall.CreateProcess(app, command, nil, nil, true, 0x08000000|4|0x400|0x80000, &block[0], cwd, &si.StartupInfo, &pi)
	runtime.KeepAlive(attrs)
	runtime.KeepAlive(handles)
	if e != nil {
		result.err = errors.New("fixed process creation failed")
		return result
	}
	defer syscall.CloseHandle(pi.Process)
	defer syscall.CloseHandle(pi.Thread)
	if ok, _, _ := assignJob.Call(job, uintptr(pi.Process)); ok == 0 {
		syscall.TerminateProcess(pi.Process, 125)
		syscall.WaitForSingleObject(pi.Process, 10000)
		result.err = errors.New("suspended process assignment failed")
		return result
	}
	count, _, _ := resumeThread.Call(uintptr(pi.Thread))
	if count != 1 {
		terminateJob.Call(job, 125)
		syscall.WaitForSingleObject(pi.Process, 10000)
		result.err = errors.New("initial resume count rejected")
		return result
	}
	w.Close()
	// The reader always drains. On overflow it requests tree termination and
	// retains only the bounded private prefix; its hash covers that prefix.
	logDone := make(chan error, 1)
	overflow := make(chan struct{}, 1)
	h := sha256.New()
	go func() {
		var used int64
		buf := make([]byte, 32768)
		var failed error
		for {
			n, er := r.Read(buf)
			if n > 0 {
				keep := n
				if int64(keep) > 8<<20-used {
					keep = int(8<<20 - used)
				}
				if keep > 0 {
					h.Write(buf[:keep])
					if log != nil {
						if _, e := log.Write(buf[:keep]); e != nil {
							failed = e
							select {
							case overflow <- struct{}{}:
							default:
							}
						}
					}
					used += int64(keep)
				}
				if keep < n {
					failed = errors.New("private output bound exceeded")
					select {
					case overflow <- struct{}{}:
					default:
					}
				}
			}
			if er != nil {
				if er != io.EOF {
					failed = errors.New("private output read failed")
				}
				logDone <- failed
				return
			}
		}
	}()
	killed := false
	var runErr error
	for {
		select {
		case <-ctx.Done():
			runErr = ctx.Err()
			killed = true
		case <-overflow:
			runErr = errors.New("private output bound exceeded")
			killed = true
		default:
		}
		w, e := syscall.WaitForSingleObject(pi.Process, 25)
		if e != nil {
			runErr = errors.New("process wait failed")
			killed = true
		}
		if w == 0 || killed {
			break
		}
	}
	// Even natural root exit drains surviving descendants by terminating the
	// exclusively owned Job Object. Query zero AND retained root signaled.
	members, snapshotErr := retainMembers(job)
	defer func() {
		for _, h := range members {
			syscall.CloseHandle(h)
		}
	}()
	runErr = errors.Join(runErr, snapshotErr)
	if ok, _, _ := terminateJob.Call(job, 125); ok == 0 {
		runErr = errors.Join(runErr, errors.New("tree termination failed"))
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		var a jobAccounting
		ok, _, _ := queryJob.Call(job, 1, uintptr(unsafe.Pointer(&a)), unsafe.Sizeof(a), 0)
		w, e := syscall.WaitForSingleObject(pi.Process, 0)
		if ok == 0 || e != nil {
			runErr = errors.Join(runErr, errors.New("tree drain query failed"))
			break
		}
		all := true
		for _, h := range members {
			state, e := syscall.WaitForSingleObject(h, 0)
			if e != nil {
				runErr = errors.Join(runErr, errors.New("retained member wait failed"))
				all = false
				break
			}
			if state != 0 {
				all = false
			}
		}
		if a.Active == 0 && w == 0 && all && snapshotErr == nil {
			result.drained = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !result.drained {
		runErr = errors.Join(runErr, errors.New("tree drain unconfirmed"))
		r.Close()
	}
	select {
	case e := <-logDone:
		runErr = errors.Join(runErr, e)
	case <-time.After(5 * time.Second):
		r.Close()
		<-logDone
		runErr = errors.Join(runErr, errors.New("private pipe drain deadline"))
	}
	if syscall.GetExitCodeProcess(pi.Process, &result.exit) != nil {
		runErr = errors.Join(runErr, errors.New("process exit unavailable"))
	}
	result.logHash = hex.EncodeToString(h.Sum(nil))
	if log != nil {
		runErr = errors.Join(runErr, log.Sync())
	}
	result.err = runErr
	return result
}

func retainMembers(job uintptr) ([]syscall.Handle, error) {
	for slots := 64; slots <= 4096; slots *= 2 {
		buffer := make([]uintptr, slots+1)
		ok, _, e := queryJob.Call(job, 3, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer))*unsafe.Sizeof(buffer[0]), 0)
		if ok == 0 {
			if e == syscall.Errno(234) {
				continue
			}
			return nil, errors.New("job member snapshot failed")
		}
		count := *(*uint32)(unsafe.Add(unsafe.Pointer(&buffer[0]), 4))
		if count > uint32(slots) {
			continue
		}
		var members []syscall.Handle
		for _, pid := range buffer[1 : 1+int(count)] {
			h, e := syscall.OpenProcess(0x00100000|0x1000, false, uint32(pid))
			if e != nil {
				if e == syscall.Errno(87) {
					continue
				}
				for _, h := range members {
					syscall.CloseHandle(h)
				}
				return nil, errors.New("job member handle failed")
			}
			var member uint32
			ok, _, _ := inJob.Call(uintptr(h), job, uintptr(unsafe.Pointer(&member)))
			if ok == 0 {
				syscall.CloseHandle(h)
				for _, h := range members {
					syscall.CloseHandle(h)
				}
				return nil, errors.New("job member identity failed")
			}
			if member == 0 {
				syscall.CloseHandle(h)
				continue
			} // PID reused outside owned job
			members = append(members, h)
		}
		return members, nil
	}
	return nil, errors.New("job member snapshot bound exceeded")
}
