//go:build !windows

package primaryflow

import "os/exec"

func hideWindow(c *exec.Cmd) {}
