//go:build !windows

package main

import (
	"os"
	"os/exec"
)

func hidden(*exec.Cmd) {}

func contain(process *os.Process) (func(), error) {
	return func() { _ = process.Kill() }, nil
}
