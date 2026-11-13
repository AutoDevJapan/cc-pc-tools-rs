//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"
)

var mp4DosKernel = syscall.NewLazyDLL("kernel32.dll")
var mp4QueryDosDevice = mp4DosKernel.NewProc("QueryDosDeviceW")
var mp4DefineDosDevice = mp4DosKernel.NewProc("DefineDosDeviceW")

type mp4CargoDirectory struct{}

func acquireCargoDriveLease(letter rune) (func() error, bool) {
	type lease struct {
		release chan chan error
	}
	ready := make(chan *lease)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		name, _ := syscall.UTF16PtrFromString("Local\\ADJ-MP4-CargoRoot-" + string(letter))
		h, _, _ := mp4DosKernel.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(name)))
		if h == 0 {
			ready <- nil
			return
		}
		defer syscall.CloseHandle(syscall.Handle(h))
		wait, _, _ := mp4DosKernel.NewProc("WaitForSingleObject").Call(h, 0)
		if wait != 0 && wait != 0x80 {
			ready <- nil
			return
		}
		l := &lease{release: make(chan chan error)}
		ready <- l
		done := <-l.release
		ok, _, _ := mp4DosKernel.NewProc("ReleaseMutex").Call(h)
		if ok == 0 {
			done <- errors.New("cargo_owned_root_mutex_release_failed")
		} else {
			done <- nil
		}
	}()
	l := <-ready
	if l == nil {
		return nil, false
	}
	return func() error {
		done := make(chan error)
		l.release <- done
		return <-done
	}, true
}

func cargoDriveMapping(name string) (string, error) {
	p, e := syscall.UTF16PtrFromString(name)
	if e != nil {
		return "", e
	}
	buf := make([]uint16, 32768)
	n, _, e := mp4QueryDosDevice.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return "", e
	}
	return syscall.UTF16ToString(buf[:n]), nil
}

func (s *softwareBuildSnapshot) bindCargoDirectory() error {
	path := filepath.Join(s.Root, "cwd")
	if e := snapshotDirectories(path, s.Root, s); e != nil {
		return e
	}
	// An operation-owned drive root has no external Cargo ancestors. This view
	// admits only the new empty cwd; it never aliases protected source or data.
	for letter := 'Z'; letter >= 'P'; letter-- {
		name := string(letter) + ":"
		release, acquired := acquireCargoDriveLease(letter)
		if !acquired {
			continue
		}
		_, e := cargoDriveMapping(name)
		if e != syscall.ERROR_FILE_NOT_FOUND {
			release()
			continue
		}
		target := "\\??\\" + path
		device, _ := syscall.UTF16PtrFromString(name)
		native, _ := syscall.UTF16PtrFromString(target)
		ok, _, _ := mp4DefineDosDevice.Call(1|8, uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(native)))
		if ok == 0 {
			release()
			return errUpdateObject
		}
		s.cargoDrive, s.cargoDriveTarget, s.releaseCargoDrive = name, target, release
		if !s.cargoDirectoryUnchanged() {
			return errUpdateObject
		}
		return nil
	}
	return errors.New("cargo_owned_root_unavailable")
}

func (s *softwareBuildSnapshot) cargoDirectoryUnchanged() bool {
	if s.cargoDrive == "" || s.cargoDriveTarget == "" {
		return false
	}
	mapping, e := cargoDriveMapping(s.cargoDrive)
	if e != nil || mapping != s.cargoDriveTarget {
		return false
	}
	physical, e1 := os.Stat(filepath.Join(s.Root, "cwd"))
	view, e2 := os.Stat(s.cargoDrive + "\\")
	return e1 == nil && e2 == nil && os.SameFile(physical, view)
}

func (s *softwareBuildSnapshot) closeCargoDirectory() error {
	if s.cargoDrive == "" {
		return nil
	}
	var errs []error
	if !s.cargoDirectoryUnchanged() {
		errs = append(errs, errors.New("cargo_owned_root_mapping_unknown"))
	} else {
		device, _ := syscall.UTF16PtrFromString(s.cargoDrive)
		target, _ := syscall.UTF16PtrFromString(s.cargoDriveTarget)
		ok, _, _ := mp4DefineDosDevice.Call(1|2|4|8, uintptr(unsafe.Pointer(device)), uintptr(unsafe.Pointer(target)))
		_, e := cargoDriveMapping(s.cargoDrive)
		if ok == 0 || e != syscall.ERROR_FILE_NOT_FOUND {
			errs = append(errs, errors.New("cargo_owned_root_cleanup_failed"))
		}
	}
	if s.releaseCargoDrive != nil {
		if e := s.releaseCargoDrive(); e != nil {
			errs = append(errs, e)
		}
		s.releaseCargoDrive = nil
	}
	s.cargoDrive, s.cargoDriveTarget = "", ""
	return errors.Join(errs...)
}
