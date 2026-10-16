//go:build windows && amd64

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

func samePath(a, b string) bool { return strings.EqualFold(filepath.Clean(a), filepath.Clean(b)) }
func openRegistrationPipe(p Plan) (*os.File, error) {
	// SECURITY_SQOS_PRESENT | SECURITY_ANONYMOUS prevents a pipe server
	// impersonating the controller even before peer validation completes.
	h, e := syscall.CreateFile(wide(pipeName(p)), 0x40000080, 0, nil, 3, 0x100000, 0)
	if e != nil {
		return nil, e
	}
	return os.NewFile(uintptr(h), "registration-channel"), nil
}
func processImage(h syscall.Handle) (string, error) {
	b := make([]uint16, 32768)
	n := uint32(len(b))
	r, _, e := kernel.NewProc("QueryFullProcessImageNameW").Call(uintptr(h), 0, uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return "", e
	}
	return syscall.UTF16ToString(b[:n]), nil
}
func verifyPipeServer(p Plan, f *os.File) error {
	s, e := openService(p, 4)
	if e != nil {
		return e
	}
	defer closeSC(s)
	state, pid, _, e := queryService(s)
	if e != nil || state != 4 || pid == 0 {
		return errors.New("registration service is not running")
	}
	var server uint32
	r, _, e := kernel.NewProc("GetNamedPipeServerProcessId").Call(f.Fd(), uintptr(unsafe.Pointer(&server)))
	if r == 0 {
		return e
	}
	if server != pid {
		return errors.New("registration pipe server is not the SCM service")
	}
	// SYNCHRONIZE only: controller can pin lifetime without opening the launcher's
	// unfiltered process token, address space or thread context.
	h, e := syscall.OpenProcess(0x100000, false, pid)
	if e != nil {
		return e
	}
	defer syscall.CloseHandle(h)
	state, again, _, e := queryService(s)
	if e != nil || state != 4 || again != pid {
		return errors.New("registration service identity changed")
	}
	var check uint32
	r, _, e = kernel.NewProc("GetNamedPipeServerProcessId").Call(f.Fd(), uintptr(unsafe.Pointer(&check)))
	if r == 0 || check != pid {
		return errors.New("registration pipe identity changed")
	}
	w, e := syscall.WaitForSingleObject(h, 0)
	if e != nil || w != 258 {
		return errors.New("registration service process has exited")
	}
	return nil
}
