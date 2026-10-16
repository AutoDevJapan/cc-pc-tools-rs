//go:build windows && amd64

package main

import (
	"fmt"
	"os"

	"syscall"
	"time"
	"unsafe"
)

// Disable the broad Authenticated Users allow ACEs, without restricted SIDs:
// Windows Schannel rejects the latter even for an anonymous TLS client.
// The official listener and all its children retain the dedicated service user
// and its exact CI-folder/lock-file grants. No owner token is impersonated.
var advapi = syscall.NewLazyDLL("advapi32.dll")
var kernel = syscall.NewLazyDLL("kernel32.dll")
var openToken = advapi.NewProc("OpenProcessToken")
var convertSID = advapi.NewProc("ConvertStringSidToSidW")
var restrictToken = advapi.NewProc("CreateRestrictedToken")
var processAsUser = advapi.NewProc("CreateProcessAsUserW")
var processWithToken = advapi.NewProc("CreateProcessWithTokenW")
var startDispatcher = advapi.NewProc("StartServiceCtrlDispatcherW")
var registerHandler = advapi.NewProc("RegisterServiceCtrlHandlerExW")
var setStatus = advapi.NewProc("SetServiceStatus")
var createJob = kernel.NewProc("CreateJobObjectW")
var setJob = kernel.NewProc("SetInformationJobObject")
var assignJob = kernel.NewProc("AssignProcessToJobObject")
var terminateJob = kernel.NewProc("TerminateJobObject")
var queryJob = kernel.NewProc("QueryInformationJobObject")
var inJob = kernel.NewProc("IsProcessInJob")
var resumeThread = kernel.NewProc("ResumeThread")
var convertDescriptor = advapi.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW")
var setObjectSecurity = advapi.NewProc("SetKernelObjectSecurity")
var getDescriptorDACL = advapi.NewProc("GetSecurityDescriptorDacl")
var setTokenInformation = advapi.NewProc("SetTokenInformation")
var getTokenInformation = advapi.NewProc("GetTokenInformation")
var sidToString = advapi.NewProc("ConvertSidToStringSidW")
var snapshotThreads = kernel.NewProc("CreateToolhelp32Snapshot")
var threadFirst = kernel.NewProc("Thread32First")
var threadNext = kernel.NewProc("Thread32Next")
var openThread = kernel.NewProc("OpenThread")

var launcherToken syscall.Handle

type sidAndAttributes struct {
	SID        uintptr
	Attributes uint32
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

func protectLauncher() error {
	// The same-user child must not reopen the unfiltered launcher token or
	// inject into the launcher to regain Authenticated Users' shared writes.
	r, _, e := openToken.Call(^uintptr(0), 0x0002|0x0008|0x0001|0x0080|0x0100, uintptr(unsafe.Pointer(&launcherToken)))
	if r == 0 {
		return fmt.Errorf("open launcher token: %w", e)
	}
	sddl, _ := syscall.UTF16PtrFromString("D:P(D;;GA;;;OW)(A;;GA;;;SY)(A;;GA;;;BA)")
	var descriptor uintptr
	r, _, e = convertDescriptor.Call(uintptr(unsafe.Pointer(sddl)), 1, uintptr(unsafe.Pointer(&descriptor)), 0)
	if r == 0 {
		return fmt.Errorf("launcher descriptor: %w", e)
	}
	defer kernel.NewProc("LocalFree").Call(descriptor)
	// A process handle is required to reopen its primary token. Protect that
	// object and its threads; retain the already-open original token privately.
	r, _, e = setObjectSecurity.Call(^uintptr(0), 4, descriptor)
	if r == 0 {
		return fmt.Errorf("protect unfiltered launcher process: %w", e)
	}
	if err := tokenDefaultDACL(launcherToken, descriptor); err != nil {
		return err
	}
	if err := protectLauncherThreads(descriptor); err != nil {
		return err
	}
	return nil
}
func tokenDefaultDACL(token syscall.Handle, descriptor uintptr) error {
	var present, defaulted uint32
	var acl uintptr
	r, _, e := getDescriptorDACL.Call(descriptor, uintptr(unsafe.Pointer(&present)), uintptr(unsafe.Pointer(&acl)), uintptr(unsafe.Pointer(&defaulted)))
	if r == 0 || present == 0 || acl == 0 {
		return fmt.Errorf("launcher DACL unavailable: %v", e)
	}
	r, _, e = setTokenInformation.Call(uintptr(token), 6, uintptr(unsafe.Pointer(&acl)), unsafe.Sizeof(acl))
	if r == 0 {
		return fmt.Errorf("set process/thread default protection: %w", e)
	}
	return nil
}

type threadEntry struct {
	Size, Usage, ID, Owner      uint32
	BasePriority, DeltaPriority int32
	Flags                       uint32
}

func protectLauncherThreads(descriptor uintptr) error {
	snapshot, _, e := snapshotThreads.Call(4, 0)
	if snapshot == ^uintptr(0) {
		return fmt.Errorf("snapshot own launcher threads: %w", e)
	}
	defer syscall.CloseHandle(syscall.Handle(snapshot))
	entry := threadEntry{Size: uint32(unsafe.Sizeof(threadEntry{}))}
	r, _, _ := threadFirst.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	for r != 0 {
		if entry.Owner == uint32(os.Getpid()) {
			h, _, e := openThread.Call(0x00040000, 0, uintptr(entry.ID))
			if h != 0 {
				v, _, e := setObjectSecurity.Call(h, 4, descriptor)
				syscall.CloseHandle(syscall.Handle(h))
				if v == 0 {
					return fmt.Errorf("protect own launcher thread: %w", e)
				}
			} else if e != syscall.Errno(87) {
				return fmt.Errorf("open own launcher thread for protection: %w", e)
			}
		}
		entry.Size = uint32(unsafe.Sizeof(entry))
		r, _, _ = threadNext.Call(snapshot, uintptr(unsafe.Pointer(&entry)))
	}
	return nil
}
func narrowToken() (syscall.Handle, error) {
	if launcherToken == 0 {
		return 0, fmt.Errorf("protected launcher token unavailable")
	}
	s, _ := syscall.UTF16PtrFromString("S-1-5-11")
	var sid uintptr
	r, _, e := convertSID.Call(uintptr(unsafe.Pointer(s)), uintptr(unsafe.Pointer(&sid)))
	if r == 0 {
		return 0, fmt.Errorf("authenticated-users SID: %w", e)
	}
	defer kernel.NewProc("LocalFree").Call(sid)
	disabled := sidAndAttributes{SID: sid}
	var token syscall.Handle
	r, _, e = restrictToken.Call(uintptr(launcherToken), 1, 1, uintptr(unsafe.Pointer(&disabled)), 0, 0, 0, 0, uintptr(unsafe.Pointer(&token)))
	if r == 0 {
		return 0, fmt.Errorf("disable inherited shared writes: %w", e)
	}
	// Normal descendants need their own process handles. Their filtered token
	// cannot reopen the protected launcher, and its default DACL is user-owned.
	user, err := tokenUserSID(token)
	if err != nil {
		syscall.CloseHandle(token)
		return 0, err
	}
	childSD, _ := syscall.UTF16PtrFromString("D:(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;" + user + ")")
	var descriptor uintptr
	r, _, e = convertDescriptor.Call(uintptr(unsafe.Pointer(childSD)), 1, uintptr(unsafe.Pointer(&descriptor)), 0)
	if r == 0 {
		syscall.CloseHandle(token)
		return 0, fmt.Errorf("child descriptor: %w", e)
	}
	defer kernel.NewProc("LocalFree").Call(descriptor)
	if err := tokenDefaultDACL(token, descriptor); err != nil {
		syscall.CloseHandle(token)
		return 0, err
	}
	return token, nil
}
func tokenUserSID(token syscall.Handle) (string, error) {
	var size uint32
	getTokenInformation.Call(uintptr(token), 1, 0, 0, uintptr(unsafe.Pointer(&size)))
	if size < uint32(unsafe.Sizeof(sidAndAttributes{})) || size > 1024 {
		return "", fmt.Errorf("own-token user buffer size invalid")
	}
	buffer := make([]byte, size)
	r, _, e := getTokenInformation.Call(uintptr(token), 1, uintptr(unsafe.Pointer(&buffer[0])), uintptr(size), uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return "", fmt.Errorf("query own-token user: %w", e)
	}
	sid := (*sidAndAttributes)(unsafe.Pointer(&buffer[0])).SID
	var text *uint16
	r, _, e = sidToString.Call(sid, uintptr(unsafe.Pointer(&text)))
	if r == 0 {
		return "", fmt.Errorf("format own-token SID: %w", e)
	}
	defer kernel.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(text)))
	return syscall.UTF16ToString(unsafe.Slice(text, 256)), nil
}
func drainRunnerJob(job uintptr, process syscall.Handle, budget time.Duration) error {
	handles, err := runnerMembers(job)
	if err != nil {
		return err
	}
	defer func() {
		for _, h := range handles {
			syscall.CloseHandle(h)
		}
	}()
	return waitRunnerJob(job, process, handles, budget)
}
func waitRunnerJob(job uintptr, process syscall.Handle, handles []syscall.Handle, budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		var a jobAccounting
		r, _, e := queryJob.Call(job, 1, uintptr(unsafe.Pointer(&a)), unsafe.Sizeof(a), 0)
		if r == 0 {
			return fmt.Errorf("runner-tree drain query: %w", e)
		}
		w, err := syscall.WaitForSingleObject(process, 0)
		if err != nil {
			return err
		}
		if a.Active == 0 && w == 0 {
			allSignaled := true
			for _, h := range handles {
				signaled, e := syscall.WaitForSingleObject(h, 0)
				if e != nil {
					return e
				}
				if signaled != 0 {
					allSignaled = false
				}
			}
			if allSignaled {
				return nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("runner tree drain deadline exceeded")
}
func runnerMembers(job uintptr) ([]syscall.Handle, error) {
	for slots := 64; slots <= 4096; slots *= 2 {
		buffer := make([]uintptr, slots+1) // two DWORD counts, then pointer-size PIDs
		r, _, e := queryJob.Call(job, 3, uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer))*unsafe.Sizeof(buffer[0]), 0)
		if r == 0 {
			if e == syscall.Errno(234) {
				continue
			}
			return nil, fmt.Errorf("snapshot runner members: %w", e)
		}
		count := *(*uint32)(unsafe.Add(unsafe.Pointer(&buffer[0]), 4))
		if count > uint32(slots) {
			continue
		}
		var handles []syscall.Handle
		for _, pid := range buffer[1 : 1+int(count)] {
			h, err := syscall.OpenProcess(0x00100000|0x1000, false, uint32(pid))
			if err != nil {
				if err == syscall.Errno(87) {
					continue
				}
				for _, p := range handles {
					syscall.CloseHandle(p)
				}
				return nil, fmt.Errorf("retain owned runner member: %w", err)
			}
			var member uint32
			r, _, e = inJob.Call(uintptr(h), job, uintptr(unsafe.Pointer(&member)))
			if r == 0 {
				syscall.CloseHandle(h)
				for _, p := range handles {
					syscall.CloseHandle(p)
				}
				return nil, fmt.Errorf("runner member identity: %w", e)
			}
			if member == 0 {
				syscall.CloseHandle(h)
				continue
			} // exited PID was reused outside this job
			handles = append(handles, h)
		}
		return handles, nil
	}
	return nil, fmt.Errorf("runner member snapshot exceeds bounded capacity")
}
