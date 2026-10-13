//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

var errLocked = errors.New("another audit is running")
var kernel = syscall.NewLazyDLL("kernel32.dll")
var diskFree = kernel.NewProc("GetDiskFreeSpaceExW")
var fileInformation = kernel.NewProc("GetFileInformationByHandle")

func hide(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} }
func isReparse(info os.FileInfo) bool {
	if info.Mode()&os.ModeSymlink != 0 {
		return true
	}
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&0x400 != 0
}
func space(path string) (uint64, uint64, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var free, total uint64
	ok, _, callErr := diskFree.Call(uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if ok == 0 {
		return 0, 0, callErr
	}
	return free, total, nil
}
func lock(path string) (func(), error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_ALWAYS, 0x00200000, 0)
	if err == syscall.Errno(32) {
		return nil, errLocked
	}
	if err != nil {
		return nil, err
	}
	var info syscall.ByHandleFileInformation
	if err = syscall.GetFileInformationByHandle(h, &info); err != nil || info.FileAttributes&0x400 != 0 {
		syscall.CloseHandle(h)
		return nil, errors.New("linked lock")
	}
	return func() { syscall.CloseHandle(h) }, nil
}
