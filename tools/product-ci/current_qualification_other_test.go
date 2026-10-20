//go:build !windows

package main

import "os/exec"

func hideCurrentTestCommand(cmd *exec.Cmd) {}
