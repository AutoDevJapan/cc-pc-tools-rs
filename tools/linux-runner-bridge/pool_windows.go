package main

import (
	"errors"
	"syscall"
	"time"
	"unsafe"
)

func burstHostCapacity() error {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	var memory struct {
		Length, Load                                     uint32
		TotalPhys, AvailPhys, TotalPage, AvailPage       uint64
		TotalVirtual, AvailVirtual, AvailExtendedVirtual uint64
	}
	memory.Length = uint32(unsafe.Sizeof(memory))
	ok, _, _ := kernel.NewProc("GlobalMemoryStatusEx").Call(uintptr(unsafe.Pointer(&memory)))
	if ok == 0 || memory.AvailPhys < 24<<30 || memory.TotalPage == 0 || float64(memory.TotalPage-memory.AvailPage)/float64(memory.TotalPage) >= .85 {
		return errors.New("host memory admission unavailable")
	}
	times := kernel.NewProc("GetSystemTimes")
	var before, after [3]uint64
	ok, _, _ = times.Call(uintptr(unsafe.Pointer(&before[0])), uintptr(unsafe.Pointer(&before[1])), uintptr(unsafe.Pointer(&before[2])))
	if ok == 0 {
		return errors.New("host CPU sample unavailable")
	}
	time.Sleep(300 * time.Millisecond)
	ok, _, _ = times.Call(uintptr(unsafe.Pointer(&after[0])), uintptr(unsafe.Pointer(&after[1])), uintptr(unsafe.Pointer(&after[2])))
	total := (after[1] - before[1]) + (after[2] - before[2])
	idle := after[0] - before[0]
	if ok == 0 || total == 0 || idle > total || float64(total-idle)/float64(total) >= .75 {
		return errors.New("host CPU admission unavailable")
	}
	for _, drive := range []string{`C:\`, `D:\`} {
		path, _ := syscall.UTF16PtrFromString(drive)
		var available, total, free uint64
		ok, _, _ := kernel.NewProc("GetDiskFreeSpaceExW").Call(uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(&available)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
		if ok == 0 || available < 30<<30 {
			return errors.New("host disk admission unavailable")
		}
	}
	return nil
}
