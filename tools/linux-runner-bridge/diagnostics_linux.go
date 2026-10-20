package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
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

func unconfigureBurstIdle() int {
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
	if cycleQuiescent("/proc") != nil {
		return 1
	}
	token, err := bufio.NewReader(io.LimitReader(os.Stdin, 4096)).ReadString('\n')
	if err != nil || strings.TrimSpace(token) == "" {
		return 1
	}
	c := exec.Command("/runner/config.sh", "remove", "--unattended")
	c.Dir = "/runner"
	c.Env = append(os.Environ(), "ACTIONS_RUNNER_INPUT_TOKEN="+strings.TrimSpace(token))
	token = ""
	var private bytes.Buffer
	c.Stdout = &private
	c.Stderr = &private
	err = c.Run()
	private.Reset()
	if err != nil {
		return 1
	}
	for _, path := range []string{"/runner/.runner", "/runner/.credentials"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			return 1
		}
	}
	return 0
}
