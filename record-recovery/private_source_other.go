//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func hideCommand(cmd *exec.Cmd) {}

func verifyPrivateSource(path string) error {
	info, e := os.Lstat(path)
	if e != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return errors.New("acquisition_private_ownership_or_acl_unverified")
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) {
		return errors.New("acquisition_private_ownership_or_acl_unverified")
	}
	return nil
}
