//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"
)

var kernel = syscall.NewLazyDLL("kernel32.dll")
var openProcess = kernel.NewProc("OpenProcess")
var waitForSingleObject = kernel.NewProc("WaitForSingleObject")
var getProcessTimes = kernel.NewProc("GetProcessTimes")
var closeHandle = kernel.NewProc("CloseHandle")
var moveFileEx = kernel.NewProc("MoveFileExW")

// Read-only handle inspection, using the raw FILETIME creation identity used by
// cc-jobs/src/platform.rs. A denied inspection never becomes a dead process.
func nativeProbe(pid uint32) processObservation {
	h, _, err := openProcess.Call(0x1000|0x100000, 0, uintptr(pid))
	if h == 0 {
		if err == syscall.Errno(87) { // ERROR_INVALID_PARAMETER: no such PID.
			return processObservation{known: true}
		}
		return processObservation{}
	}
	defer closeHandle.Call(h)
	r, _, _ := waitForSingleObject.Call(h, 0)
	if r == 0 {
		return processObservation{known: true}
	}
	if r != 258 { // WAIT_TIMEOUT
		return processObservation{}
	}
	var created, exited, system, user syscall.Filetime
	ok, _, _ := getProcessTimes.Call(h, uintptr(unsafe.Pointer(&created)), uintptr(unsafe.Pointer(&exited)), uintptr(unsafe.Pointer(&system)), uintptr(unsafe.Pointer(&user)))
	if ok == 0 {
		return processObservation{alive: true, known: true}
	}
	r, _, _ = waitForSingleObject.Call(h, 0)
	if r == 0 {
		return processObservation{known: true}
	}
	if r != 258 {
		return processObservation{}
	}
	n := uint64(created.HighDateTime)<<32 | uint64(created.LowDateTime)
	return processObservation{alive: true, known: true, created: &n}
}

func replaceSnapshot(from, to string) error {
	f, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	t, err := syscall.UTF16PtrFromString(to)
	if err != nil {
		return err
	}
	// Same-directory staging + REPLACE_EXISTING | WRITE_THROUGH. Consumers open
	// each published snapshot anew; no truncation of the previous file occurs.
	var callErr error
	for attempt := 0; attempt < 8; attempt++ {
		var ok uintptr
		ok, _, callErr = moveFileEx.Call(uintptr(unsafe.Pointer(f)), uintptr(unsafe.Pointer(t)), 1|8)
		if ok != 0 {
			return nil
		}
		// Ordinary Go/Windows readers briefly omit FILE_SHARE_DELETE. Retry the
		// rename only; never truncate or delete the previous generation. At
		// most 140ms total delay, even if a reader permanently holds the file.
		if callErr != syscall.Errno(5) && callErr != syscall.Errno(32) && callErr != syscall.Errno(33) {
			return callErr
		}
		if attempt < 7 {
			time.Sleep(time.Duration(attempt+1) * 5 * time.Millisecond)
		}
	}
	return callErr
}
