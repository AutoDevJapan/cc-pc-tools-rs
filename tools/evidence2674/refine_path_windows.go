package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func hideGuard(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
}

func strictSavedPath(path string) bool {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, e := os.Lstat(current)
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		ptr, e := syscall.UTF16PtrFromString(current)
		if e != nil {
			return false
		}
		attributes, e := syscall.GetFileAttributes(ptr)
		if e != nil || attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return false
		}
		if filepath.Dir(current) == current {
			return true
		}
	}
}
