//go:build windows && amd64

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

func cachePort(p Plan) int {
	n, _ := strconv.ParseUint(p.Instance[:4], 16, 32)
	return 50000 + int(n%10000)
}

// The existing owner-account cache server would execute compilers outside
// this Job Object. Use the same installed binary with a new finite foreground
// process. No shared server, cache configuration or ACL is changed.
func startOwnedCache(p Plan) (func(), error) {
	env := runtimeSDKEnv(p, workerEnv(p))
	env = append(env, "SCCACHE_START_SERVER=1", "SCCACHE_NO_DAEMON=1")
	c := exec.Command(`D:\CC\Tools\sccache\sccache.exe`)
	hidden(c)
	c.Env = env
	c.Dir = p.runtime()
	f, e := os.OpenFile(filepath.Join(p.runtime(), "sdk-sccache-server.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return nil, e
	}
	c.Stdout = f
	c.Stderr = f
	if e = c.Start(); e != nil {
		f.Close()
		return nil, e
	}
	h, err := syscall.OpenProcess(0x1000, false, uint32(c.Process.Pid))
	var member uint32
	var observed uintptr
	if err == nil {
		observed, _, err = inJob.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&member)))
		syscall.CloseHandle(h)
		if observed != 0 {
			err = nil
		}
	}
	if err != nil || observed == 0 || member == 0 {
		_ = c.Process.Kill()
		_ = c.Wait()
		f.Close()
		return nil, errors.New("cache process Job Object membership not observed")
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait(); f.Close() }()
	stop := func() {
		_ = c.Process.Kill()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}
	until := time.Now().Add(15 * time.Second)
	for time.Now().Before(until) {
		select {
		case <-done:
			return nil, errors.New("owned foreground cache exited before binding")
		default:
		}
		ok, e := ownedTCPListener(uint32(c.Process.Pid), cachePort(p))
		if e != nil {
			stop()
			return nil, e
		}
		if ok {
			if e = saveNew(filepath.Join(p.runtime(), "sdk-sccache-server.json"), map[string]any{"pid": c.Process.Pid, "port": cachePort(p), "kernel_listener_pid_verified": true, "foreground": true, "inherited_job_object": true, "cache": filepath.Join(p.runtime(), "sccache"), "shared_cache_modified": false}); e != nil {
				stop()
				return nil, e
			}
			return stop, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	stop()
	return nil, errors.New("owned foreground cache listener deadline")
}

func ownedTCPListener(pid uint32, port int) (bool, error) {
	proc := syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")
	var n uint32
	r, _, _ := proc.Call(0, uintptr(unsafe.Pointer(&n)), 0, 2, 3, 0)
	if r != 122 || n < 4 || n > 16<<20 {
		return false, errors.New("kernel listener table size unavailable")
	}
	b := make([]byte, n)
	r, _, _ = proc.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&n)), 0, 2, 3, 0)
	if r != 0 {
		return false, fmt.Errorf("kernel listener table error %d", r)
	}
	count := binary.LittleEndian.Uint32(b[:4])
	if uint64(count)*24+4 > uint64(n) {
		return false, errors.New("kernel listener table truncated")
	}
	for i := uint32(0); i < count; i++ {
		row := b[4+i*24 : 4+(i+1)*24]
		if binary.BigEndian.Uint16(row[8:10]) == uint16(port) && binary.LittleEndian.Uint32(row[20:24]) == pid {
			return true, nil
		}
	}
	return false, nil
}
