//go:build !windows

package main

import "os/exec"

func hideCollectorWindow(cmd *exec.Cmd) {}
