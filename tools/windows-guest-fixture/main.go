//go:build windows && amd64

// This finite fixture exercises a credential-free Windows Job Object namespace.
// It launches two suspended child processes, observes them live, then checks
// that the namespace and both processes disappear on normal completion and
// cancellation.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

const (
	createSuspended              = 0x00000004
	createNoWindow               = 0x08000000
	jobObjectLimitKillOnJobClose = 0x00002000
	jobObjectExtendedLimitInfo   = 9
	jobObjectQuery               = 0x0004
	processTerminate             = 0x0001
	processSynchronize           = 0x00100000
	processSetQuota              = 0x0100
	threadSuspendResume          = 0x0002
	errorFileNotFound            = syscall.Errno(2)
	th32csSnapThread             = 0x00000004
)

var (
	kernel32                 = syscall.NewLazyDLL("kernel32.dll")
	createJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	setInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	openJobObjectW           = kernel32.NewProc("OpenJobObjectW")
	assignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	openThread               = kernel32.NewProc("OpenThread")
	resumeThread             = kernel32.NewProc("ResumeThread")
	createToolhelp32Snapshot = kernel32.NewProc("CreateToolhelp32Snapshot")
	thread32FirstW           = kernel32.NewProc("Thread32First")
	thread32NextW            = kernel32.NewProc("Thread32Next")
	openProcess              = kernel32.NewProc("OpenProcess")
)

type basicLimitInformation struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type ioCounters struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type extendedLimitInformation struct {
	BasicLimitInformation basicLimitInformation
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

type threadEntry32 struct {
	Size           uint32
	Usage          uint32
	ThreadID       uint32
	OwnerProcessID uint32
	BasePriority   int32
	DeltaPriority  int32
	Flags          uint32
}

type fixtureResult struct {
	Case                string `json:"case"`
	NamespaceCreated    bool   `json:"namespace_created"`
	ChildPIDs           []int  `json:"child_pids"`
	BothObservedAlive   bool   `json:"both_observed_alive"`
	ChildrenExited      bool   `json:"children_exited"`
	NamespaceRemoved    bool   `json:"namespace_removed"`
	CancellationFired   bool   `json:"cancellation_fired"`
	CredentialFree      bool   `json:"credential_free"`
	NonceSHA256         string `json:"nonce_sha256"`
	ElapsedMilliseconds int64  `json:"elapsed_milliseconds"`
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--service" {
		if err := runGuestService(); err != nil {
			fmt.Fprintln(os.Stderr, "guest service stopped")
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 4 && os.Args[1] == "vm-smoke" {
		if err := runVMProvision(os.Args[2], os.Args[3]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "vm-smoke-vhd" {
		if err := runVMProvisionFromVHD(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--leaf-normal" {
		time.Sleep(1200 * time.Millisecond)
		return
	}
	if len(os.Args) == 2 && os.Args[1] == "--leaf-live" {
		for {
			time.Sleep(time.Second)
		}
	}
	if len(os.Args) != 2 || (os.Args[1] != "normal" && os.Args[1] != "cancel") {
		fmt.Fprintln(os.Stderr, "usage: windows-guest-fixture normal|cancel | vm-smoke <owned-root> <official-eval-iso> | vm-smoke-vhd <owned-eval-vhd-root>")
		os.Exit(2)
	}
	result, err := run(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(kind string) (fixtureResult, error) {
	started := time.Now()
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fixtureResult{}, err
	}
	nonceDigest := sha256.Sum256(nonce[:])
	name := "Local\\cc2746-" + hex.EncodeToString(nonce[:])
	job, err := createJob(name)
	clear(nonce[:])
	if err != nil {
		return fixtureResult{}, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = syscall.CloseHandle(job)
		}
	}()

	exe, err := os.Executable()
	if err != nil {
		return fixtureResult{}, err
	}
	cmds := make([]*exec.Cmd, 0, 2)
	for _, mode := range []string{"--leaf-normal", "--leaf-live"} {
		cmd := exec.Command(exe, mode)
		cmd.Dir = filepath.Dir(exe)
		cmd.Env = []string{"SystemRoot=" + os.Getenv("SystemRoot"), "PATH=" + os.Getenv("SystemRoot") + `\System32`}
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createSuspended | createNoWindow}
		if err := cmd.Start(); err != nil {
			for _, started := range cmds {
				_ = started.Process.Kill()
				_ = started.Wait()
			}
			return fixtureResult{}, err
		}
		cmds = append(cmds, cmd)
		if err := assignAndResume(job, uint32(cmd.Process.Pid)); err != nil {
			_ = syscall.CloseHandle(job)
			closed = true
			for _, started := range cmds {
				_ = started.Process.Kill()
			}
			for _, started := range cmds {
				_ = started.Wait()
			}
			return fixtureResult{}, err
		}
	}
	result := fixtureResult{Case: kind, NamespaceCreated: true,
		ChildPIDs:      []int{cmds[0].Process.Pid, cmds[1].Process.Pid},
		CredentialFree: true, NonceSHA256: hex.EncodeToString(nonceDigest[:])}
	result.BothObservedAlive = processAlive(uint32(result.ChildPIDs[0])) && processAlive(uint32(result.ChildPIDs[1]))
	if !result.BothObservedAlive {
		_ = syscall.CloseHandle(job)
		closed = true
		for _, cmd := range cmds {
			_ = cmd.Wait()
		}
		return fixtureResult{}, errors.New("fixture processes were not both alive at observation")
	}

	if kind == "normal" {
		if err := cmds[0].Wait(); err != nil {
			_ = syscall.CloseHandle(job)
			closed = true
			_ = cmds[1].Wait()
			return fixtureResult{}, fmt.Errorf("normal child failed: %w", err)
		}
		if processAlive(uint32(result.ChildPIDs[1])) == false {
			return fixtureResult{}, errors.New("live child exited before namespace cleanup")
		}
	} else {
		ctx, cancel := context.WithCancel(context.Background())
		timer := time.AfterFunc(100*time.Millisecond, cancel)
		<-ctx.Done()
		timer.Stop()
		result.CancellationFired = errors.Is(ctx.Err(), context.Canceled)
		cancel()
	}

	if err := syscall.CloseHandle(job); err != nil {
		return fixtureResult{}, err
	}
	closed = true
	for _, cmd := range cmds {
		if cmd.ProcessState == nil {
			_ = cmd.Wait()
		}
	}
	result.ChildrenExited = waitProcessExit(uint32(result.ChildPIDs[0]), 5*time.Second) && waitProcessExit(uint32(result.ChildPIDs[1]), 5*time.Second)
	result.NamespaceRemoved = namespaceAbsent(name)
	result.ElapsedMilliseconds = time.Since(started).Milliseconds()
	if !result.ChildrenExited || !result.NamespaceRemoved {
		return result, errors.New("owned Windows Job Object cleanup was incomplete")
	}
	return result, nil
}

func createJob(name string) (syscall.Handle, error) {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	h, _, callErr := createJobObjectW.Call(0, uintptr(unsafe.Pointer(namePtr)))
	if h == 0 {
		return 0, fmt.Errorf("CreateJobObjectW: %w", callErr)
	}
	info := extendedLimitInformation{BasicLimitInformation: basicLimitInformation{LimitFlags: jobObjectLimitKillOnJobClose}}
	ok, _, callErr := setInformationJobObject.Call(h, jobObjectExtendedLimitInfo, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
	if ok == 0 {
		_ = syscall.CloseHandle(syscall.Handle(h))
		return 0, fmt.Errorf("SetInformationJobObject: %w", callErr)
	}
	return syscall.Handle(h), nil
}

func assignAndResume(job syscall.Handle, pid uint32) error {
	processHandle, _, callErr := openProcess.Call(processSetQuota|processTerminate, 0, uintptr(pid))
	if processHandle == 0 {
		return fmt.Errorf("OpenProcess for job assignment: %w", callErr)
	}
	assigned, _, callErr := assignProcessToJobObject.Call(uintptr(job), processHandle)
	_ = syscall.CloseHandle(syscall.Handle(processHandle))
	if assigned == 0 {
		return fmt.Errorf("AssignProcessToJobObject: %w", callErr)
	}
	return resumeInitialThread(pid)
}

func resumeInitialThread(pid uint32) error {
	snap, _, callErr := createToolhelp32Snapshot.Call(th32csSnapThread, 0)
	if syscall.Handle(snap) == syscall.InvalidHandle {
		return fmt.Errorf("CreateToolhelp32Snapshot: %w", callErr)
	}
	defer syscall.CloseHandle(syscall.Handle(snap))
	entry := threadEntry32{Size: uint32(unsafe.Sizeof(threadEntry32{}))}
	ok, _, callErr := thread32FirstW.Call(snap, uintptr(unsafe.Pointer(&entry)))
	for ok != 0 {
		if entry.OwnerProcessID == pid {
			h, _, e := openThread.Call(threadSuspendResume, 0, uintptr(entry.ThreadID))
			if h == 0 {
				return fmt.Errorf("OpenThread: %w", e)
			}
			defer syscall.CloseHandle(syscall.Handle(h))
			count, _, e := resumeThread.Call(h)
			if count == ^uintptr(0) {
				return fmt.Errorf("ResumeThread: %w", e)
			}
			return nil
		}
		entry.Size = uint32(unsafe.Sizeof(threadEntry32{}))
		ok, _, callErr = thread32NextW.Call(snap, uintptr(unsafe.Pointer(&entry)))
	}
	return fmt.Errorf("initial thread not found: %w", callErr)
}

func processAlive(pid uint32) bool {
	h, _, _ := openProcess.Call(processSynchronize, 0, uintptr(pid))
	if h == 0 {
		return false
	}
	defer syscall.CloseHandle(syscall.Handle(h))
	state, _ := syscall.WaitForSingleObject(syscall.Handle(h), 0)
	return state == syscall.WAIT_TIMEOUT
}

func waitProcessExit(pid uint32, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return !processAlive(pid)
}

func namespaceAbsent(name string) bool {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return false
	}
	h, _, callErr := openJobObjectW.Call(jobObjectQuery, 0, uintptr(unsafe.Pointer(namePtr)))
	if h != 0 {
		_ = syscall.CloseHandle(syscall.Handle(h))
		return false
	}
	return errors.Is(callErr, errorFileNotFound)
}
