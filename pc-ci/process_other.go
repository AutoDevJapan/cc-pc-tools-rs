//go:build !windows

package main

import "os/exec"

func hideConsole(c *exec.Cmd) {}
