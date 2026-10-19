//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func hide(c *exec.Cmd) {}

// procStat reads the start time (clock ticks since boot) and the state of a
// process from /proc. Without /proc the identity is unknown, which the callers
// treat as "not the recorded process".
func procStat(pid uint32) (start uint64, state byte, err error) {
	b, e := os.ReadFile("/proc/" + strconv.FormatUint(uint64(pid), 10) + "/stat")
	if e != nil {
		return 0, 0, e
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 || i+2 >= len(s) {
		return 0, 0, errors.New("unreadable process stat")
	}
	f := strings.Fields(s[i+2:])
	// After the command name: state is field 3, starttime is field 22 overall.
	if len(f) < 20 || len(f[0]) != 1 {
		return 0, 0, errors.New("unreadable process stat")
	}
	start, e = strconv.ParseUint(f[19], 10, 64)
	return start, f[0][0], e
}

func procIdentity(pid uint32) (ProcID, error) {
	if pid == 0 {
		return ProcID{}, errors.New("no pid")
	}
	start, _, e := procStat(pid)
	if e != nil {
		return ProcID{}, e
	}
	return ProcID{PID: pid, Created: start + 1}, nil
}

func procAlive(id ProcID) bool {
	if id.PID == 0 || id.Created == 0 {
		return false
	}
	start, state, e := procStat(id.PID)
	return e == nil && start+1 == id.Created && state != 'Z' && state != 'X'
}

func startDetached(mk func() *exec.Cmd) (*exec.Cmd, bool, error) {
	c := mk()
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if e := c.Start(); e != nil {
		return nil, false, e
	}
	return c, true, nil
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
