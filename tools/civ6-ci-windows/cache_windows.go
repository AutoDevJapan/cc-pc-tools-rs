//go:build windows && amd64

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

func cachePort(p Plan) int {
	return p.cacheListenerPort
}

// The existing owner-account cache server would execute compilers outside
// this Job Object. Use the same installed binary with a new finite foreground
// process. No shared server, cache configuration or ACL is changed.
func startOwnedCache(p *Plan) (func(), error) {
	// Port zero makes the owning process bind an OS-selected available port.
	// No probe-close-rebind race, excluded-port guess, or shared server lookup.
	p.cacheListenerPort = 0
	env, err := runtimeSDKEnv(*p, workerEnv(*p))
	if err != nil {
		return nil, err
	}
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
		port, e := ownedCacheListener(uint32(c.Process.Pid))
		if e != nil {
			stop()
			return nil, e
		}
		if port != 0 {
			if e = saveNew(filepath.Join(p.runtime(), "sdk-sccache-server.json"), map[string]any{"pid": c.Process.Pid, "port": port, "kernel_listener_pid_verified": true, "foreground": true, "inherited_job_object": true, "cache": filepath.Join(p.runtime(), "sccache"), "shared_cache_modified": false}); e != nil {
				stop()
				return nil, e
			}
			p.cacheListenerPort = port
			return stop, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	stop()
	return nil, errors.New("owned foreground cache listener deadline")
}

func ownedCacheListener(pid uint32) (int, error) {
	proc := syscall.NewLazyDLL("iphlpapi.dll").NewProc("GetExtendedTcpTable")
	var n uint32
	r, _, _ := proc.Call(0, uintptr(unsafe.Pointer(&n)), 0, 2, 3, 0)
	if r != 122 || n < 4 || n > 16<<20 {
		return 0, errors.New("kernel listener table size unavailable")
	}
	var b []byte
	for attempt := 0; attempt < 3; attempt++ {
		if n < 4 || n > 16<<20 {
			return 0, errors.New("kernel listener table size bound")
		}
		b = make([]byte, n)
		r, _, _ = proc.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(unsafe.Pointer(&n)), 0, 2, 3, 0)
		if r != 122 {
			break
		}
	}
	if r != 0 {
		return 0, fmt.Errorf("kernel listener table error %d", r)
	}
	if n > uint32(len(b)) {
		return 0, errors.New("kernel listener table grew")
	}
	return cacheListenerFromTable(b[:n], pid)
}

func cacheListenerFromTable(b []byte, pid uint32) (int, error) {
	if len(b) < 4 {
		return 0, errors.New("kernel listener table header truncated")
	}
	count := binary.LittleEndian.Uint32(b[:4])
	if uint64(count)*24+4 > uint64(len(b)) {
		return 0, errors.New("kernel listener table truncated")
	}
	port := 0
	for i := uint32(0); i < count; i++ {
		row := b[4+i*24 : 4+(i+1)*24]
		// MIB_TCP_STATE_LISTEN, IPv4 127.0.0.1, and this exact child PID.
		if binary.LittleEndian.Uint32(row[:4]) == 2 && binary.LittleEndian.Uint32(row[4:8]) == 0x0100007f && binary.LittleEndian.Uint32(row[20:24]) == pid {
			if port != 0 {
				return 0, errors.New("multiple owned cache listeners")
			}
			port = int(binary.BigEndian.Uint16(row[8:10]))
			if port == 0 {
				return 0, errors.New("owned cache listener has zero port")
			}
		}
	}
	return port, nil
}
