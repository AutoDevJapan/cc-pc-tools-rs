//go:build !windows

package sourcepacket

import "os/exec"

func configureProcess(*exec.Cmd) {}
