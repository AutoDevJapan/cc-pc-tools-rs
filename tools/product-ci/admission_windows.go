package main

import (
	"os"
	"syscall"
	"unsafe"
)

func lockAdmission(f *os.File) error {
	var overlapped syscall.Overlapped
	lock := syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
	ok, _, e := lock.Call(f.Fd(), 3, 0, 1, 0, uintptr(unsafe.Pointer(&overlapped)))
	if ok == 0 {
		return e
	}
	return nil
}
