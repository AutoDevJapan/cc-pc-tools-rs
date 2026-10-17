//go:build !windows

package main

import (
	"os"
	"os/exec"
)

func hideSourceCommand(c *exec.Cmd)     {}
func sourceReparse(fi os.FileInfo) bool { return fi.Mode()&os.ModeSymlink != 0 }
