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

func hidden(c *exec.Cmd)    {}
func group(c *exec.Cmd)     { c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func killGroup(c *exec.Cmd) { _ = terminateAndDrainGroup(c, 3*time.Second, 15*time.Second) }

func terminateAndDrainGroup(c *exec.Cmd, grace, forceWait time.Duration) error {
	if c == nil || c.Process == nil {
		return errors.New("process group is unavailable")
	}
	groupID := c.Process.Pid
	_ = syscall.Kill(-groupID, syscall.SIGTERM)
	deadline := time.Now().Add(grace)
	for processGroupExists(groupID) && time.Now().Before(deadline) {
		time.Sleep(25 * time.Millisecond)
	}
	if !processGroupExists(groupID) {
		return nil
	}
	_ = syscall.Kill(-groupID, syscall.SIGKILL)
	deadline = time.Now().Add(forceWait)
	for {
		if !processGroupExists(groupID) {
			return nil
		}
		if time.Now().After(deadline) {
			// Keep the gate occupied while any member can still execute. An
			// unreaped zombie cannot write to the target and must not hold it.
			_ = syscall.Kill(-groupID, syscall.SIGKILL)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func processGroupExists(groupID int) bool {
	if groupID <= 0 {
		return true
	}
	err := syscall.Kill(-groupID, 0)
	if errors.Is(err, syscall.ESRCH) {
		return false
	}
	if err != nil && !errors.Is(err, syscall.EPERM) {
		return true
	}
	live, err := groupHasLiveMembers("/proc", groupID)
	return err != nil || live
}

func procMember(stat []byte) (string, int, error) {
	// comm can itself contain spaces and parentheses; fields begin after its
	// final closing parenthesis. State, ppid, pgrp are the next three fields.
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return "", 0, errors.New("process state unavailable")
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 3 || len(fields[0]) != 1 {
		return "", 0, errors.New("process state unavailable")
	}
	groupID, err := strconv.Atoi(fields[2])
	if err != nil || groupID < 0 {
		return "", 0, errors.New("process group unavailable")
	}
	return fields[0], groupID, nil
}

func groupHasLiveMembers(root string, groupID int) (bool, error) {
	processes, err := os.ReadDir(root)
	if err != nil {
		return true, err
	}
	for _, process := range processes {
		if _, err := strconv.Atoi(process.Name()); err != nil {
			continue
		}
		path := filepath.Join(root, process.Name())
		stat, err := os.ReadFile(filepath.Join(path, "stat"))
		if errors.Is(err, os.ErrNotExist) {
			continue // The process was reaped during this scan.
		}
		if err != nil {
			return true, err
		}
		state, memberGroup, err := procMember(stat)
		if err != nil {
			return true, err
		}
		if memberGroup != groupID {
			continue
		}
		if state != "Z" && state != "X" {
			return true, nil
		}
		// A zombie thread-group leader can still have executing threads.
		// Check every task before treating the process as unable to write.
		tasks, err := os.ReadDir(filepath.Join(path, "task"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return true, err
		}
		for _, task := range tasks {
			stat, err := os.ReadFile(filepath.Join(path, "task", task.Name(), "stat"))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return true, err
			}
			state, _, err := procMember(stat)
			if err != nil {
				return true, err
			}
			if state != "Z" && state != "X" {
				return true, nil
			}
		}
	}
	return false, nil
}
