//go:build windows && amd64

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

// Machine-level state of the shared identity. Written only by the administrator
// phases; readable by the non-admin controller.
func stateKeyPath(p Plan) string { return `SOFTWARE\CC\passshare-ci-windows\` + p.svc() }

// collectedRoot returns the root of the last fully collected job, "" if none.
func collectedRoot(p Plan) (string, error) {
	var k syscall.Handle
	e := syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, wide(stateKeyPath(p)), 0, 0x20019, &k)
	if errors.Is(e, syscall.ERROR_FILE_NOT_FOUND) {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	defer syscall.RegCloseKey(k)
	var typ uint32
	var buf [2048]uint16
	size := uint32(len(buf) * 2)
	e = syscall.RegQueryValueEx(k, wide("CollectedRoot"), nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &size)
	if errors.Is(e, syscall.ERROR_FILE_NOT_FOUND) {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	return syscall.UTF16ToString(buf[:]), nil
}

func markCollected(p Plan) error {
	var k uintptr
	var disp uint32
	// KEY_SET_VALUE|KEY_CREATE_SUB_KEY|KEY_QUERY_VALUE = 0x2|0x4|0x1 ... 0x20006 also grants READ_CONTROL.
	if r, _, _ := advapi.NewProc("RegCreateKeyExW").Call(uintptr(syscall.HKEY_LOCAL_MACHINE), ptr(stateKeyPath(p)), 0, 0, 0, 0x20006, 0, uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&disp))); r != 0 {
		return syscall.Errno(r)
	}
	defer syscall.RegCloseKey(syscall.Handle(k))
	v, e := syscall.UTF16FromString(p.Root)
	if e != nil {
		return e
	}
	if r, _, _ := advapi.NewProc("RegSetValueExW").Call(k, ptr("CollectedRoot"), 0, 1, uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2)); r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

// The claim lock is an exclusive byte-range lock on one file under ProgramData,
// shared by the administrator phases and the non-admin controller. It dies with
// the process and has no thread affinity.
func claimLockPath(p Plan) string {
	// The service and the controller must resolve the same machine namespace,
	// including when the gate runs with a deliberately minimal environment.
	return filepath.Join(`C:\ProgramData`, "CC", "passshare-ci-windows", p.svc()+".lock")
}

// prepareClaimLock (administrator) creates the lock file and lets the controller
// open it for read/write. Idempotent.
func prepareClaimLock(p Plan) error {
	path := claimLockPath(p)
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return icacls(path, "/grant:r", "*S-1-5-18:F", "*S-1-5-32-544:F", "*"+p.ControllerSID+":M")
}

func lockClaim(p Plan) (func(), error) {
	h, e := syscall.CreateFile(wide(claimLockPath(p)), 0xC0000000, 3, nil, 3, 0x80, 0)
	if e != nil {
		return nil, e
	}
	until := time.Now().Add(30 * time.Second)
	for {
		ov := syscall.Overlapped{}
		r, _, err := kernel.NewProc("LockFileEx").Call(uintptr(h), 3, 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
		if r != 0 {
			return func() {
				u := syscall.Overlapped{}
				kernel.NewProc("UnlockFileEx").Call(uintptr(h), 0, 1, 0, uintptr(unsafe.Pointer(&u)))
				syscall.CloseHandle(h)
			}, nil
		}
		if err != syscall.Errno(33) || time.Now().After(until) { // ERROR_LOCK_VIOLATION = held elsewhere
			syscall.CloseHandle(h)
			if err == syscall.Errno(33) {
				return nil, errors.New("another operation on the shared service identity is in progress")
			}
			return nil, err
		}
		time.Sleep(100 * time.Millisecond)
	}
}
