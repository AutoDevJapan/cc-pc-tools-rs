//go:build windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// The lock serializes normal explicit and automatic fetches for one canonical
// repository/catalogue. It grants no identity and never extends its lifetime.
func ownerLockIdentityRefresh(ctx context.Context, home, repository string) (func(), bool) {
	if ctx.Err() != nil {
		return nil, false
	}
	if _, ok := ownerOriginRepository("https://github.com/" + repository); !ok {
		return nil, false
	}
	if os.MkdirAll(home, 0700) != nil || !currentPlain(home, true) {
		return nil, false
	}
	name := hashBytes([]byte(strings.ToLower(repository)+"\n"+hashBytes(ownerDepartmentCatalog))) + ".refresh.lock"
	path := filepath.Join(home, name)
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, false
	}
	// Exclude deletion of the name while held so another process cannot lock
	// a replacement file. Open the link itself and reject any reparse point.
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_ALWAYS, 0x00200000, 0)
	if err != nil {
		return nil, false
	}
	f := os.NewFile(uintptr(h), path)
	var info syscall.ByHandleFileInformation
	named, namedErr := os.Lstat(path)
	opened, openedErr := f.Stat()
	if syscall.GetFileInformationByHandle(h, &info) != nil || info.NumberOfLinks != 1 || info.FileAttributes&0x410 != 0 || namedErr != nil || openedErr != nil || !plainEntry(opened) || !os.SameFile(opened, named) {
		f.Close()
		return nil, false
	}
	var ov syscall.Overlapped
	lock := syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
	unlock := syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")
	for {
		ok, _, e := lock.Call(uintptr(h), 3, 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
		if ok != 0 {
			return func() { unlock.Call(uintptr(h), 0, 1, 0, uintptr(unsafe.Pointer(&ov))); f.Close() }, true
		}
		if e != syscall.Errno(33) {
			f.Close()
			return nil, false
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, false
		case <-time.After(10 * time.Millisecond):
		}
	}
}
