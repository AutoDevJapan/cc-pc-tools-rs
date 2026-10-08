//go:build !windows

package main

import "os/exec"

func hidden(cmd *exec.Cmd) {}
func collect(r *report)    { r.Problems = append(r.Problems, "windows_required") }
