package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"
)

func fileVersion(path string) (string, bool) {
	u, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return "", false
	}
	dll := syscall.NewLazyDLL("version.dll")
	var ignored uint32
	n, _, _ := dll.NewProc("GetFileVersionInfoSizeW").Call(uintptr(unsafe.Pointer(u)), uintptr(unsafe.Pointer(&ignored)))
	if n == 0 || n > 1<<20 {
		return "", false
	}
	buf := make([]byte, n)
	ok, _, _ := dll.NewProc("GetFileVersionInfoW").Call(uintptr(unsafe.Pointer(u)), 0, n, uintptr(unsafe.Pointer(&buf[0])))
	if ok == 0 {
		return "", false
	}
	query, _ := syscall.UTF16PtrFromString(`\`)
	var p unsafe.Pointer
	var size uint32
	ok, _, _ = dll.NewProc("VerQueryValueW").Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(query)), uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&size)))
	if ok == 0 || p == nil || size < 24 {
		return "", false
	}
	v := unsafe.Slice((*uint32)(p), 6)
	if v[0] != 0xfeef04bd {
		return "", false
	}
	return fmt.Sprintf("Google Chrome %d.%d.%d.%d", v[2]>>16, v[2]&0xffff, v[3]>>16, v[3]&0xffff), true
}

func launch(c config, args []string) int {
	for _, arg := range args[1:] {
		if arg == "--version" {
			v, ok := fileVersion(c.Browser)
			if !ok {
				return 126
			}
			fmt.Println(v)
			return 0
		}
	}
	x := exec.Command(c.Browser, args[1:]...)
	x.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	x.Env = os.Environ()
	x.Stdin = os.Stdin
	x.Stdout = os.Stdout
	x.Stderr = os.Stderr
	if e := x.Run(); e != nil {
		if ex, ok := e.(*exec.ExitError); ok {
			return ex.ExitCode()
		}
		return 125
	}
	return 0
}
