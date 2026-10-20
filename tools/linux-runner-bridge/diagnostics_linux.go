package main

import (
	"os"
	"syscall"
)

func exportBurstDiagnostics() int {
	if os.Getuid() != 1001 || !compiledNormalProfile.ephemeral {
		return 1
	}
	fd, err := syscall.Open("/tmp/bridge-supervisor.lock", syscall.O_CREAT|syscall.O_RDWR|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return 1
	}
	defer syscall.Close(fd)
	if syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return 1
	}
	defer syscall.Flock(fd, syscall.LOCK_UN)
	if cycleQuiescent("/proc") != nil || ownedDirectory(diagnosticRoot) != nil {
		return 1
	}
	if archiveEndedDiagnostics(diagnosticRoot, os.Stdout) != nil {
		return 1
	}
	return 0
}
