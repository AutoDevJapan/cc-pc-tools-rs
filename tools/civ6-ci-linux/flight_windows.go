package main

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

var lockFlightFile = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

// The host owns this byte lock for the entire flight, including all cleanup.
// A crashed host releases it in the kernel; stale PIDs cannot grant ownership.
func flightLease(path string, create bool) (*os.File, error) {
	flags := os.O_RDWR
	if create {
		flags |= os.O_CREATE | os.O_EXCL
	}
	f, e := os.OpenFile(path, flags, 0600)
	if e != nil {
		return nil, e
	}
	var ov syscall.Overlapped
	ok, _, _ := lockFlightFile.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
	if ok == 0 {
		f.Close()
		return nil, errors.New("original Linux flight remains active")
	}
	return f, nil
}
