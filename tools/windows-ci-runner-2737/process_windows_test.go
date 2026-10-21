//go:build windows && amd64

package windowsrunner

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Child fixture is this test binary, never a runner/service/SDK/Cargo process.
func TestNativeCreationFailureHasMeasuredCodeWithoutInventedExit(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "absent-fixture.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r := runTree(ctx, exe, syscall.EscapeArg(exe), root, os.Environ(), "")
	if r.err == nil || r.phase != "create" || r.nativeError != uint32(syscall.ERROR_FILE_NOT_FOUND) || r.drained || r.logHash != "" {
		t.Fatalf("native failure incorrectly measured: %#v", r)
	}
	ev := configurationFailure(testSeed().Plan.Instance, &configurationError{result: r})
	if ev.NativeError != uint32(syscall.ERROR_FILE_NOT_FOUND) || ev.Exit != nil || ev.Drained {
		t.Fatal("creation failure lost native result or invented process exit")
	}
}

func TestTreeFixture(t *testing.T) {
	mode := os.Getenv("CC2828_TREE_FIXTURE")
	if mode == "" {
		return
	}
	if mode == "leaf" {
		c, e := net.Dial("tcp4", os.Getenv("CC2828_FIXTURE_SOCKET"))
		if e != nil {
			os.Exit(71)
		}
		defer c.Close()
		fmt.Fprintln(c, "leaf", os.Getpid())
		var b [1]byte
		c.Read(b[:])
		os.Exit(0)
	}
	exe, _ := os.Executable()
	child := exec.Command(exe, "-test.run=^TestTreeFixture$")
	child.Env = append(os.Environ(), "CC2828_TREE_FIXTURE=leaf")
	child.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000}
	if child.Start() != nil {
		os.Exit(72)
	}
	c, e := net.Dial("tcp4", os.Getenv("CC2828_FIXTURE_SOCKET"))
	if e != nil {
		os.Exit(73)
	}
	defer c.Close()
	fmt.Fprintln(c, "root", os.Getpid())
	var b [1]byte
	c.Read(b[:])
	os.Exit(0)
}

func TestWindowsJobObjectNaturalAndCancellationDrainRetainedDescendant(t *testing.T) {
	for _, mode := range []string{"natural", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			socket, e := net.Listen("tcp4", "127.0.0.1:0")
			if e != nil {
				t.Fatal(e)
			}
			defer socket.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			exe, _ := os.Executable()
			root := t.TempDir()
			env := append(os.Environ(), "CC2828_TREE_FIXTURE="+mode, "CC2828_FIXTURE_SOCKET="+socket.Addr().String())
			done := make(chan processResult, 1)
			go func() {
				done <- runTree(ctx, exe, syscall.EscapeArg(exe)+" -test.run=^TestTreeFixture$", root, env, filepath.Join(root, "bounded.log"))
			}()
			if tcp, ok := socket.(*net.TCPListener); ok {
				tcp.SetDeadline(time.Now().Add(5 * time.Second))
			}
			var rootConn net.Conn
			var pid int
			for i := 0; i < 2; i++ {
				c, e := socket.Accept()
				if e != nil {
					t.Fatal("fixture not reached", e)
				}
				defer c.Close()
				c.SetDeadline(time.Now().Add(3 * time.Second))
				var kind string
				var id int
				if _, e = fmt.Fscanln(c, &kind, &id); e != nil {
					t.Fatal(e)
				}
				if kind == "root" {
					rootConn = c
				} else if kind == "leaf" {
					pid = id
				}
			}
			if rootConn == nil || pid < 1 {
				t.Fatal("root and descendant barrier absent")
			}
			h, e := syscall.OpenProcess(0x00100000|0x1000, false, uint32(pid))
			if e != nil {
				t.Fatal(e)
			}
			defer syscall.CloseHandle(h)
			if mode == "cancel" {
				cancel()
			} else {
				rootConn.Write([]byte{1})
			}
			r := <-done
			if !r.drained || (mode == "natural" && (r.err != nil || r.exit != 0)) || (mode == "cancel" && r.err == nil) {
				t.Fatalf("tree result: exit=%d drained=%t error=%v", r.exit, r.drained, r.err)
			}
			if w, e := syscall.WaitForSingleObject(h, 0); e != nil || w != 0 {
				t.Fatal("retained descendant still active")
			}
			if !validHex(r.logHash, 64) {
				t.Fatal("private log hash absent")
			}
		})
	}
}

func TestWindowsHookPipeRejectsWrongContextAndStopsIdleClient(t *testing.T) {
	// Exercise the actual authenticated loopback hook transport without touching
	// the fixed guest paths or starting SCM. Listener is scoped to this test.
	o := &windowsOps{plan: testSeed().Plan, hookCalls: make(chan hookCall), closed: make(chan struct{}), capability: fmt.Sprintf("%064d", 1)}
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	o.listener = l
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		c, e := l.Accept()
		if e == nil {
			o.handleHook(c)
		}
	}()
	c, e := net.Dial("tcp4", l.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer c.Close()
	bad := hookContext(o.plan)
	bad.RunID++
	if e = send(c, localHook{o.capability, Start, bad}); e != nil {
		t.Fatal(e)
	}
	c.SetReadDeadline(time.Now().Add(time.Second))
	var reply localAck
	if receive(c, &reply) == nil && reply.Accepted {
		t.Fatal("cross-run local hook admitted")
	}
	if e = o.close(); e != nil {
		t.Fatal(e)
	}
}
