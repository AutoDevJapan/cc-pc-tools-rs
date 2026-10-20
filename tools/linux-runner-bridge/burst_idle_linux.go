package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
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
			return 2
		}
	}
	return 0
}

func runRunnerConfiguration(c *exec.Cmd, bounded bool) error {
	if !bounded {
		return c.Run()
	}
	group(c)
	if err := c.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		// No run.sh/job exists until configuration and the synchronous binding
		// finish. Only this setup group is terminated on its own deadline.
		_ = syscall.Kill(-c.Process.Pid, syscall.SIGTERM)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
			select {
			case <-done:
			case <-time.After(15 * time.Second):
				return errConfigurationUnverified
			}
		}
		return errors.New("runner configuration deadline exceeded")
	}
}

// Called by the host only after its bound registration was withdrawn/removed.
// The container has one supervisor; an active Worker always blocks shutdown.
func stopBurstIdleListener() int {
	if burstWorkerIdle() != 0 {
		return 1
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 1
	}
	group := 0
	listeners := 0
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		name, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			return 1
		}
		switch strings.TrimSpace(string(name)) {
		case "Runner.Worker":
			return 1
		case "Runner.Listener":
			listeners++
		case "run.sh":
			data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
			if err != nil {
				return 1
			}
			end := strings.LastIndexByte(string(data), ')')
			if end < 0 {
				return 1
			}
			fields := strings.Fields(string(data[end+1:]))
			if len(fields) < 3 {
				return 1
			}
			pgid, err := strconv.Atoi(fields[2])
			if err != nil || pgid != pid || group != 0 {
				return 1
			}
			group = pgid
		}
	}
	if group == 0 {
		if listeners == 0 {
			return 0
		}
		return 1
	}
	if err := syscall.Kill(-group, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return 1
	}
	return 0
}
