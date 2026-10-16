//go:build !windows

package main

import (
	"os"
	"syscall"
)

func lockAdmission(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func openAdmissionForRecovery(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDWR, 0600)
}
