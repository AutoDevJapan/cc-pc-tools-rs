//go:build windows && amd64

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// Machine-level state of the shared identity. Written only by the administrator
// phases; the key's DACL is set explicitly (SYSTEM/Administrators full, everyone
// else read) every time it is written.
func stateKeyPath(p Plan) string { return `SOFTWARE\CC\civ6-ci-windows\` + p.svc() }

const stateKeySDDL = "D:P(A;;KA;;;SY)(A;;KA;;;BA)(A;;KR;;;AU)"

// collectedRoot returns the root of the last fully collected job, "" if none. The
// value must be a well-formed REG_SZ naming an owned root, otherwise it is an error.
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
	if typ != 1 || size < 4 || size%2 != 0 || buf[size/2-1] != 0 {
		return "", errors.New("collection marker has an unexpected registry type or shape")
	}
	v := syscall.UTF16ToString(buf[:size/2])
	if !ownedRootPattern.MatchString(v) {
		return "", errors.New("collection marker does not name an owned root")
	}
	return v, nil
}

func markCollected(p Plan) error {
	var sd uintptr
	if r, _, e := convertDescriptor.Call(ptr(stateKeySDDL), 1, uintptr(unsafe.Pointer(&sd)), 0); r == 0 {
		return e
	}
	defer kernel.NewProc("LocalFree").Call(sd)
	sa := syscall.SecurityAttributes{Length: uint32(unsafe.Sizeof(syscall.SecurityAttributes{})), SecurityDescriptor: sd}
	var k uintptr
	var disp uint32
	// READ_CONTROL | WRITE_DAC | KEY_SET_VALUE | KEY_CREATE_SUB_KEY
	if r, _, _ := advapi.NewProc("RegCreateKeyExW").Call(uintptr(syscall.HKEY_LOCAL_MACHINE), ptr(stateKeyPath(p)), 0, 0, 0, 0x60006, uintptr(unsafe.Pointer(&sa)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&disp))); r != 0 {
		return syscall.Errno(r)
	}
	defer syscall.RegCloseKey(syscall.Handle(k))
	// A pre-existing key keeps its old DACL through the create call: replace it.
	if r, _, _ := advapi.NewProc("RegSetKeySecurity").Call(k, 4, sd); r != 0 {
		return syscall.Errno(r)
	}
	v, e := syscall.UTF16FromString(p.Root)
	if e != nil {
		return e
	}
	if r, _, _ := advapi.NewProc("RegSetValueExW").Call(k, ptr("CollectedRoot"), 0, 1, uintptr(unsafe.Pointer(&v[0])), uintptr(len(v)*2)); r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

// programData asks the OS for FOLDERID_ProgramData, independent of the caller's
// environment, so every phase locks the same file.
func programData() (string, error) {
	guid := struct {
		D1 uint32
		D2 uint16
		D3 uint16
		D4 [8]byte
	}{0x62AB5D82, 0xFDC1, 0x4DC3, [8]byte{0xA9, 0xDD, 0x07, 0x0D, 0x1D, 0x49, 0x5D, 0x97}}
	var pw *uint16
	hr, _, _ := syscall.NewLazyDLL("shell32.dll").NewProc("SHGetKnownFolderPath").Call(uintptr(unsafe.Pointer(&guid)), 0, 0, uintptr(unsafe.Pointer(&pw)))
	if hr != 0 || pw == nil {
		return "", fmt.Errorf("ProgramData known folder unavailable (hr=%#x)", hr)
	}
	defer syscall.NewLazyDLL("ole32.dll").NewProc("CoTaskMemFree").Call(uintptr(unsafe.Pointer(pw)))
	s := syscall.UTF16ToString((*[1 << 15]uint16)(unsafe.Pointer(pw))[:])
	if !filepath.IsAbs(s) || strings.HasPrefix(s, `\\`) {
		return "", errors.New("ProgramData is not a local absolute path")
	}
	return filepath.Clean(s), nil
}

// The claim lock is an exclusive byte-range lock on one file under the OS
// ProgramData, shared by the administrator phases and the non-admin controller.
// It dies with the process and has no thread affinity.
func claimLockPath(p Plan) (string, error) {
	base, e := programData()
	if e != nil {
		return "", e
	}
	return filepath.Join(base, "CC", "civ6-ci-windows", p.svc()+".lock"), nil
}

func plainEntry(path string, dir bool) error {
	fi, e := os.Lstat(path)
	if e != nil {
		return e
	}
	if fi.IsDir() != dir || fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 || fi.Sys().(*syscall.Win32FileAttributeData).FileAttributes&0x400 != 0 {
		return errors.New("claim lock path is not a plain entry")
	}
	return nil
}

// prepareClaimLock (administrator) creates the lock file and gives the controller
// only what opening and locking it needs (read/write data, no DELETE, no
// WRITE_DAC/WRITE_OWNER); inherited and previous grants are removed. Idempotent.
func prepareClaimLock(p Plan) error {
	path, e := claimLockPath(p)
	if e != nil {
		return e
	}
	dir := filepath.Dir(path)
	if e = os.MkdirAll(dir, 0755); e != nil {
		return e
	}
	if e = plainEntry(dir, true); e != nil {
		return e
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if e = plainEntry(path, false); e != nil {
		return e
	}
	if e = icacls(dir, "/inheritance:r", "/grant:r", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F", "*"+p.ControllerSID+":(RX)"); e != nil {
		return e
	}
	return icacls(path, "/inheritance:r", "/grant:r", "*S-1-5-18:F", "*S-1-5-32-544:F", "*"+p.ControllerSID+":(R,W)")
}

func lockClaim(p Plan) (func(), error) {
	path, e := claimLockPath(p)
	if e != nil {
		return nil, e
	}
	if e = plainEntry(filepath.Dir(path), true); e != nil {
		return nil, e
	}
	if e = plainEntry(path, false); e != nil {
		return nil, e
	}
	h, e := syscall.CreateFile(wide(path), 0xC0000000, 3, nil, 3, 0x80, 0)
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
