//go:build !windows

package main

import "os/exec"

func hideGuard(command *exec.Cmd)      {}
func strictSavedPath(path string) bool { return noLinkAncestors(path) }
