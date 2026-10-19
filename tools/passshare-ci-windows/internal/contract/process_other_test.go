//go:build !windows

package contract

import "os/exec"

func hideTest(c *exec.Cmd) {}
