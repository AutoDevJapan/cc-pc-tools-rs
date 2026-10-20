//go:build windows && amd64

package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestCacheUsesKernelAssignedPort(t *testing.T) {
	p := fixturePlan()
	// Requesting zero lets the same Windows bind that owns the socket select
	// a port, rather than guessing one in a potentially excluded range.
	if cachePort(p) != 0 {
		t.Fatal("cache guesses a port before binding", cachePort(p))
	}
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	port, err := ownedCacheListener(uint32(os.Getpid()))
	if err != nil || port != l.Addr().(*net.TCPAddr).Port {
		t.Fatal("actual owned listener not found", port, err)
	}
	p.cacheListenerPort = port
	if !slices.Contains(envFor(p), "SCCACHE_SERVER_PORT="+strconv.Itoa(port)) {
		t.Fatal("selected port was not propagated")
	}
}

// Explicit owned empty root only; no Rust, service identity, registration or
// product job. Exercise the installed sccache and its actual client connection.
func TestOwnedCacheInstalledSmoke(t *testing.T) {
	root := os.Getenv("CIW_CACHE_SMOKE_ROOT")
	if root == "" {
		t.Skip("explicit safe-delete root required")
	}
	p := fixturePlan()
	p.Root = root
	for _, name := range []string{"", "tmp", "home", "sccache"} {
		if err := os.Mkdir(filepath.Join(p.runtime(), name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"Local", "Roaming"} {
		if err := os.MkdirAll(filepath.Join(p.runtime(), "home", "AppData", name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := saveNew(filepath.Join(p.runtime(), "sdk-environment.json"), map[string]string{"PATH": `C:\Windows\System32`, "LIB": "unused", "INCLUDE": "unused"}); err != nil {
		t.Fatal(err)
	}
	stop, err := startOwnedCache(&p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if stop != nil {
			stop()
		}
	}()
	var receipt struct {
		PID  uint32 `json:"pid"`
		Port int    `json:"port"`
	}
	b, err := os.ReadFile(filepath.Join(p.runtime(), "sdk-sccache-server.json"))
	if err != nil || json.Unmarshal(b, &receipt) != nil || receipt.Port == 0 || receipt.Port != cachePort(p) {
		t.Fatal("listener receipt", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, `D:\CC\Tools\sccache\sccache.exe`, "--show-stats")
	hidden(c)
	c.Env = workerEnv(p)
	if err = c.Run(); err != nil {
		t.Fatal("actual client could not connect to selected port", err)
	}
	stop()
	stop = nil
	if port, err := ownedCacheListener(receipt.PID); err != nil || port != 0 {
		t.Fatal("owned listener survived shutdown", port, err)
	}
}

func cacheTable(rows ...[4]uint32) []byte {
	b := make([]byte, 4+24*len(rows))
	binary.LittleEndian.PutUint32(b, uint32(len(rows)))
	for i, row := range rows {
		v := b[4+i*24:]
		binary.LittleEndian.PutUint32(v, row[0])     // state
		binary.LittleEndian.PutUint32(v[4:], row[1]) // local IPv4
		binary.BigEndian.PutUint16(v[8:], uint16(row[2]))
		binary.LittleEndian.PutUint32(v[20:], row[3]) // PID
	}
	return b
}

func TestCacheListenerRejectsForeignAndAmbiguousBindings(t *testing.T) {
	const pid = 1234
	row := [4]uint32{2, 0x0100007f, 42001, pid}
	for _, v := range []struct {
		name string
		data []byte
		want int
		fail bool
	}{
		{"owned", cacheTable(row), 42001, false},
		{"foreign PID", cacheTable([4]uint32{2, 0x0100007f, 42001, pid + 1}), 0, false},
		{"not listening", cacheTable([4]uint32{5, 0x0100007f, 42001, pid}), 0, false},
		{"not loopback", cacheTable([4]uint32{2, 0, 42001, pid}), 0, false},
		{"zero port", cacheTable([4]uint32{2, 0x0100007f, 0, pid}), 0, true},
		{"ambiguous", cacheTable(row, [4]uint32{2, 0x0100007f, 42002, pid}), 0, true},
		{"truncated header", []byte{1}, 0, true},
		{"truncated row", cacheTable(row)[:15], 0, true},
	} {
		t.Run(v.name, func(t *testing.T) {
			got, err := cacheListenerFromTable(v.data, pid)
			if got != v.want || (err != nil) != v.fail {
				t.Fatal(got, err)
			}
		})
	}
}
