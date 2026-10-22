//go:build !windows

package consolepages

import "os/exec"

func windowless(cmd *exec.Cmd) {}
