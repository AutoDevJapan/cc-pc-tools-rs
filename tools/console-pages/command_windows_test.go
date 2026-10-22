package consolepages

import (
	"os/exec"
	"syscall"
)

func windowless(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000} }
