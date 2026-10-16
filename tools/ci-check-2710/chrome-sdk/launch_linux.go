package main

import (
	"os"
	"syscall"
)

func launch(c config, args []string) int {
	env := os.Environ()
	if c.LibraryPath != "" {
		env = append(env, "LD_LIBRARY_PATH="+c.LibraryPath)
	}
	if syscall.Exec(c.Browser, args, env) != nil {
		return 125
	}
	return 0
}
