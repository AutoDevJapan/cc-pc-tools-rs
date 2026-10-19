//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

const (
	createNoWindow      = 0x08000000
	createNewProcessGrp = 0x00000200
	createBreakawayJob  = 0x01000000
	processQueryLimited = 0x1000
	stillActive         = 259
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

func hide(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// procIdentity reads the creation FILETIME of a live process so that a reused
// PID can never be mistaken for the recorded process.
func procIdentity(pid uint32) (ProcID, error) {
	if pid == 0 {
		return ProcID{}, errors.New("no pid")
	}
	h, _, _ := kernel32.NewProc("OpenProcess").Call(processQueryLimited, 0, uintptr(pid))
	if h == 0 {
		return ProcID{}, errors.New("process unavailable")
	}
	defer kernel32.NewProc("CloseHandle").Call(h)
	var start, exit, kt, ut syscall.Filetime
	ok, _, _ := kernel32.NewProc("GetProcessTimes").Call(h, uintptr(unsafe.Pointer(&start)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kt)), uintptr(unsafe.Pointer(&ut)))
	if ok == 0 {
		return ProcID{}, errors.New("process times unavailable")
	}
	return ProcID{PID: pid, Created: uint64(start.HighDateTime)<<32 | uint64(start.LowDateTime)}, nil
}

// procAlive requires the same PID, the same creation time and a running state.
func procAlive(id ProcID) bool {
	if id.PID == 0 || id.Created == 0 {
		return false
	}
	h, _, _ := kernel32.NewProc("OpenProcess").Call(processQueryLimited, 0, uintptr(id.PID))
	if h == 0 {
		return false
	}
	defer kernel32.NewProc("CloseHandle").Call(h)
	var start, exit, kt, ut syscall.Filetime
	ok, _, _ := kernel32.NewProc("GetProcessTimes").Call(h, uintptr(unsafe.Pointer(&start)), uintptr(unsafe.Pointer(&exit)), uintptr(unsafe.Pointer(&kt)), uintptr(unsafe.Pointer(&ut)))
	if ok == 0 || uint64(start.HighDateTime)<<32|uint64(start.LowDateTime) != id.Created {
		return false
	}
	var code uint32
	ok, _, _ = kernel32.NewProc("GetExitCodeProcess").Call(h, uintptr(unsafe.Pointer(&code)))
	return ok != 0 && code == stillActive
}

// startDetached starts a long-lived fixed child without a console window. It
// first asks to leave the scheduler's job object so the one-pass tick can end
// without killing the supervisor; if that is refused it falls back and records
// that the child stays bound to the tick's job.
func startDetached(c *exec.Cmd) (breakaway bool, err error) {
	for _, flags := range []uint32{createNoWindow | createNewProcessGrp | createBreakawayJob, createNoWindow | createNewProcessGrp} {
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
		if err = c.Start(); err == nil {
			return flags&createBreakawayJob != 0, nil
		}
		c.Process = nil
	}
	return false, err
}

func replaceFile(from, to string) error {
	a, e := syscall.UTF16PtrFromString(from)
	if e != nil {
		return e
	}
	b, e := syscall.UTF16PtrFromString(to)
	if e != nil {
		return e
	}
	ok, _, err := kernel32.NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 0x1|0x8)
	if ok == 0 {
		return err
	}
	return nil
}

var errLockBusy = errors.New("lock busy")

// tryLock takes a non-blocking exclusive OS lock. The OS drops it when the
// holder exits or crashes, so a stale holder can never keep a lock forever.
func tryLock(path string) (func(), error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	var ov syscall.Overlapped
	ok, _, err := kernel32.NewProc("LockFileEx").Call(f.Fd(), 0x3, 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
	if ok == 0 {
		f.Close()
		if errno, is := err.(syscall.Errno); is && (errno == 33 || errno == 32) {
			return nil, errLockBusy
		}
		return nil, err
	}
	return func() {
		kernel32.NewProc("UnlockFileEx").Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
		f.Close()
	}, nil
}
