//go:build windows

package contract

import (
	"os/exec"
	"syscall"
)

func hideTest(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
