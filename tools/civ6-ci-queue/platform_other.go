//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func hide(c *exec.Cmd) {}

func procIdentity(pid uint32) (ProcID, error) {
	if pid == 0 {
		return ProcID{}, errors.New("no pid")
	}
	if e := syscall.Kill(int(pid), 0); e != nil {
		return ProcID{}, e
	}
	return ProcID{PID: pid, Created: 1}, nil
}

func procAlive(id ProcID) bool {
	return id.PID != 0 && syscall.Kill(int(id.PID), 0) == nil
}

func startDetached(c *exec.Cmd) (bool, error) {
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return true, c.Start()
}

func replaceFile(from, to string) error { return os.Rename(from, to) }

var errLockBusy = errors.New("lock busy")

func tryLock(path string) (func(), error) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		f.Close()
		if e == syscall.EWOULDBLOCK {
			return nil, errLockBusy
		}
		return nil, e
	}
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
