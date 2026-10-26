//go:build !windows

package main

import "os/exec"

func hideInputProcess(_ *exec.Cmd) {}
