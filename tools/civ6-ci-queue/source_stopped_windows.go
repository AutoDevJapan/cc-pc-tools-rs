//go:build windows

package main

import (
	"errors"
	"syscall"
	"unsafe"
)

// An unreadable recorded controller/helper must not be treated as absent.
// Hold one queryable process handle while comparing its birth and wait state.
// OpenProcess's invalid-PID handling follows Microsoft's ProcessManager.Windows;
// access denied, incomplete identity and failed queries remain unknown.
func verifySourceProcessStopped(id ProcID) error {
	if id == (ProcID{}) {
		return nil
	}
	if id.PID == 0 || id.Created == 0 {
		return errors.New("source process identity incomplete")
	}
	h, _, err := kernel32.NewProc("OpenProcess").Call(processQueryLimited|0x00100000, 0, uintptr(id.PID)) // SYNCHRONIZE
	if h == 0 {
		if err == syscall.Errno(87) {
			return nil
		} // fixed flags and nonzero, nonexistent PID
		return errors.New("source process cannot be queried; absence unverified")
	}
	defer kernel32.NewProc("CloseHandle").Call(h)
	var start, exit, kt, ut syscall.Filetime
	ok, _, _ := kernel32.NewProc("GetProcessTimes").Call(h, uintptr(unsafe.Pointer(&start)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kt)), uintptr(unsafe.Pointer(&ut)))
	if ok == 0 {
		return errors.New("source process creation time cannot be queried")
	}
	if uint64(start.HighDateTime)<<32|uint64(start.LowDateTime) != id.Created {
		return nil
	} // PID reused; recorded process is gone.
	state, _, _ := kernel32.NewProc("WaitForSingleObject").Call(h, 0)
	if state == 0 {
		return nil
	}
	if state == 258 {
		return errors.New("recorded source process is still live")
	}
	return errors.New("source process wait state is unknown")
}
