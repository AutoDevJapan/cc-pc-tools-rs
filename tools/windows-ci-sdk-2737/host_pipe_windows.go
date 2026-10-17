//go:build windows && amd64

package sdkprep

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
	"unsafe"
)

const errorPipeBusy = syscall.Errno(231)

var (
	pipeKernel    = syscall.NewLazyDLL("kernel32.dll")
	waitNamedPipe = pipeKernel.NewProc("WaitNamedPipeW")
	openNamedPipe = pipeKernel.NewProc("CreateFileW")
)

// OpenOwnedGuestCOMPipe opens only the deterministic COM pipe for this fresh
// instance; it has no argument for an arbitrary named-pipe path.
func OpenOwnedGuestCOMPipe(ctx context.Context, instance string) (*os.File, error) {
	pipeName, err := COMPipeName(instance)
	if err != nil {
		return nil, err
	}
	wide, err := syscall.UTF16PtrFromString(pipeName)
	if err != nil {
		return nil, errors.New("guest pipe name conversion failed")
	}
	for {
		select {
		case <-ctx.Done():
			return nil, errors.New("guest pipe wait cancelled")
		default:
		}
		ready, _, _ := waitNamedPipe.Call(uintptr(unsafe.Pointer(wide)), 1000)
		if ready != 0 {
			handle, _, callErr := openNamedPipe.Call(uintptr(unsafe.Pointer(wide)), 0x80000000|0x40000000, 0, 0, 3, 0x80, 0)
			if syscall.Handle(handle) != syscall.InvalidHandle {
				f := os.NewFile(handle, pipeName)
				if f != nil {
					return f, nil
				}
				_ = syscall.CloseHandle(syscall.Handle(handle))
				return nil, errors.New("guest pipe handle wrapping failed")
			}
			errno, ok := callErr.(syscall.Errno)
			if !ok || errno != errorPipeBusy && errno != syscall.ERROR_FILE_NOT_FOUND {
				return nil, errors.New("guest pipe open failed")
			}
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, errors.New("guest pipe wait cancelled")
		case <-timer.C:
		}
	}
}
