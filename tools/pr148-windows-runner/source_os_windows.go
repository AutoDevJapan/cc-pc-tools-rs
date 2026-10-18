//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func hideSourceCommand(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
func sourceReparse(fi os.FileInfo) bool {
	return fi.Sys().(*syscall.Win32FileAttributeData).FileAttributes&0x400 != 0
}
