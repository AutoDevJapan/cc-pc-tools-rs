package main

import (
	"os"
	"syscall"
	"testing"
	"unsafe"
)

func TestProcessReceiptRequiresActualCreationIdentity(t *testing.T) {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	h, _, _ := kernel.NewProc("GetCurrentProcess").Call()
	var start, exit, kernelTime, user syscall.Filetime
	ok, _, err := kernel.NewProc("GetProcessTimes").Call(h, uintptr(unsafe.Pointer(&start)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kernelTime)), uintptr(unsafe.Pointer(&user)))
	if ok == 0 {
		t.Fatal(err)
	}
	created := uint64(start.HighDateTime)<<32 | uint64(start.LowDateTime)
	if !processReceiptAlive(uint32(os.Getpid()), created) || processReceiptAlive(uint32(os.Getpid()), created+1) || processReceiptAlive(0, created) {
		t.Fatal("process liveness ignored the saved creation identity")
	}
}
