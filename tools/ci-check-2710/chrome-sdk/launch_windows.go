package main

import (
	"os"
	"os/exec"
	"syscall"
)

func launch(c config, args []string) int {
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
