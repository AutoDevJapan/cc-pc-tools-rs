package main

import (
	"errors"
	"path/filepath"
	"syscall"
	"unsafe"
)

type memoryStatusEx struct {
	Length                                                                         uint32
	MemoryLoad                                                                     uint32
	TotalPhys, AvailPhys, TotalPageFile, AvailPageFile, TotalVirtual, AvailVirtual uint64
	AvailExtendedVirtual                                                           uint64
}

// hostResources reports available physical memory and free bytes on the
// receipt drive using the plain Win32 counters.
func hostResources(dir string) (phys, disk uint64, err error) {
	k := syscall.NewLazyDLL("kernel32.dll")
	var m memoryStatusEx
	m.Length = uint32(unsafe.Sizeof(m))
	if r, _, _ := k.NewProc("GlobalMemoryStatusEx").Call(uintptr(unsafe.Pointer(&m))); r == 0 {
		return 0, 0, errors.New("host memory observation failed")
	}
	root, e := filepath.Abs(dir)
	if e != nil {
		return 0, 0, e
	}
	p, e := syscall.UTF16PtrFromString(filepath.VolumeName(root) + "\\")
	if e != nil {
		return 0, 0, e
	}
	var free, total, totalFree uint64
	if r, _, _ := k.NewProc("GetDiskFreeSpaceExW").Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&free)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&totalFree))); r == 0 {
		return 0, 0, errors.New("host disk observation failed")
	}
	return m.AvailPhys, free, nil
}
