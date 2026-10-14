package main

import (
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

func hide(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func replace(from, to string) error {
	a, e := syscall.UTF16PtrFromString(from)
	if e != nil {
		return e
	}
	b, e := syscall.UTF16PtrFromString(to)
	if e != nil {
		return e
	}
	ok, _, err := syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 0x1|0x8)
	if ok == 0 {
		return err
	}
	return nil
}
func acquire(path string) (func(), error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	var ov syscall.Overlapped
	kernel := syscall.NewLazyDLL("kernel32.dll")
	ok, _, err := kernel.NewProc("LockFileEx").Call(f.Fd(), 0x3, 0, 1, 0, uintptr(unsafe.Pointer(&ov)))
	if ok == 0 {
		f.Close()
		return nil, err
	}
	return func() { kernel.NewProc("UnlockFileEx").Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ov))); f.Close() }, nil
}
func discordToken() string {
	if t := os.Getenv("GODD_DISCORD_BOT_TOKEN"); t != "" {
		return t
	}
	key, err := syscall.UTF16PtrFromString("Environment")
	if err != nil {
		return ""
	}
	var handle syscall.Handle
	const read = 0x20019
	adv := syscall.NewLazyDLL("advapi32.dll")
	status, _, _ := adv.NewProc("RegOpenKeyExW").Call(0x80000001, uintptr(unsafe.Pointer(key)), 0, read, uintptr(unsafe.Pointer(&handle)))
	if status != 0 {
		return ""
	}
	defer adv.NewProc("RegCloseKey").Call(uintptr(handle))
	name, _ := syscall.UTF16PtrFromString("GODD_DISCORD_BOT_TOKEN")
	var kind, size uint32
	query := adv.NewProc("RegQueryValueExW")
	status, _, _ = query.Call(uintptr(handle), uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&kind)), 0, uintptr(unsafe.Pointer(&size)))
	if status != 0 || kind != 1 || size == 0 || size > 32768 {
		return ""
	}
	data := make([]uint16, (size+1)/2)
	status, _, _ = query.Call(uintptr(handle), uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&kind)), uintptr(unsafe.Pointer(&data[0])), uintptr(unsafe.Pointer(&size)))
	if status != 0 {
		return ""
	}
	return syscall.UTF16ToString(data)
}
