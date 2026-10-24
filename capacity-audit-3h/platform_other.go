//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
)

func discoverDevices(context.Context) ([]byte, error) { return nil, errors.New("Windows only") }

var errLocked = errors.New("another audit is running")

func hide(cmd *exec.Cmd)                        {}
func isReparse(info os.FileInfo) bool           { return info.Mode()&os.ModeSymlink != 0 }
func space(path string) (uint64, uint64, error) { return 0, 0, errors.New("Windows only") }
func lock(path string) (func(), error)          { return nil, errors.New("Windows only") }
