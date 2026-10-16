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

func openAdmissionForRecovery(path string) (*os.File, error) {
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(h), path), nil
}
