//go:build !windows

package main

import "os/exec"

func hidden(c *exec.Cmd) {}
