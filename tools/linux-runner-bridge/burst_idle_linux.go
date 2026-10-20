package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func burstWorkerIdle() int {
	if os.Getuid() != 1001 || !compiledNormalProfile.ephemeral {
		return 1
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 1
	}
	for _, e := range entries {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		name, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm"))
		if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			return 1
		}
		if strings.TrimSpace(string(name)) == "Runner.Worker" {
			return 1
		}
	}
	return 0
}
