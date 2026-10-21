package main

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// Metadata-only handle resolution also covers Windows junctions, which recent
// Go EvalSymlinks versions deliberately preserve. No file contents are read.
func ownerPhysicalPath(path string) (string, error) {
	path, err := portable(path)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	h, err := syscall.CreateFile(p, 0x80, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, 0x02000000, 0)
	if err != nil {
		return "", &os.PathError{Op: "metadata", Path: "", Err: err}
	}
	defer syscall.CloseHandle(h)
	return ownerHandlePath(h)
}

func ownerHandlePath(h syscall.Handle) (string, error) {
	buffer := make([]uint16, 32768)
	n, _, callErr := syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW").Call(uintptr(h), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 0)
	if n == 0 || n >= uintptr(len(buffer)) {
		return "", callErr
	}
	resolved, err := portable(syscall.UTF16ToString(buffer[:n]))
	return filepath.Clean(resolved), err
}

func ownerUnicodeKC(value string) string {
	in, err := syscall.UTF16FromString(value)
	if err != nil {
		return value
	}
	proc := syscall.NewLazyDLL("normaliz.dll").NewProc("NormalizeString")
	required, _, _ := proc.Call(5, uintptr(unsafe.Pointer(&in[0])), uintptr(len(in)-1), 0, 0)
	if int32(required) <= 0 || required > 1<<20 {
		return value
	}
	buffer := make([]uint16, int(required))
	n, _, _ := proc.Call(5, uintptr(unsafe.Pointer(&in[0])), uintptr(len(in)-1), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if int32(n) <= 0 || n > uintptr(len(buffer)) {
		return value
	}
	return syscall.UTF16ToString(buffer[:n])
}
