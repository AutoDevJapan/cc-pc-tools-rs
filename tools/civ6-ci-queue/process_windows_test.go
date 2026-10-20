//go:build windows && amd64

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestOwnedTreeFixture(t *testing.T) {
	index := -1
	for i, a := range os.Args {
		if a == "--owned-tree-fixture" {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	mode, root := os.Args[index+1], os.Args[index+2]
	if mode == "leaf" {
		time.Sleep(time.Minute)
		os.Exit(0)
	}
	exe, _ := os.Executable()
	c := exec.Command(exe, "-test.run=^TestOwnedTreeFixture$", "--", "--owned-tree-fixture", "leaf", root)
	hide(c)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	if err := c.Start(); err != nil {
		os.Exit(2)
	}
	b, _ := json.Marshal(map[string]int{"root": os.Getpid(), "child": c.Process.Pid})
	if os.WriteFile(filepath.Join(root, "ready.json"), b, 0600) != nil {
		os.Exit(3)
	}
	for {
		if _, e := os.Stat(filepath.Join(root, "release")); e == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	fmt.Print("owned fixture complete\n")
	os.Exit(0) // The intentionally remaining leaf must be collected by the Job.
}

func TestOwnedTreeNormalAndCancelledKeepOriginalHandles(t *testing.T) {
	if unsafe.Sizeof(queueJobLimits{}) != 144 || unsafe.Sizeof(queueStartupEx{}) != 112 {
		t.Fatal("Win64 Job/STARTUPINFOEX layout mismatch")
	}
	for _, cancelRun := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelRun), func(t *testing.T) {
			root := t.TempDir()
			exe, _ := os.Executable()
			ctx, cancel := context.WithCancel(context.Background())
			type answer struct {
				result StepResult
				err    error
			}
			ch := make(chan answer, 1)
			done := make(chan struct{})
			go func() {
				r, e := runOwnedExe(ctx, exe, []string{"-test.run=^TestOwnedTreeFixture$", "--", "--owned-tree-fixture", "root", root}, nil, nil)
				ch <- answer{r, e}
				close(done)
			}()
			t.Cleanup(func() {
				cancel()
				select {
				case <-done:
				case <-time.After(35 * time.Second):
					t.Error("owned tree did not drain")
				}
			})
			var ids struct{ Root, Child uint32 }
			until := time.Now().Add(10 * time.Second)
			for ids.Child == 0 && time.Now().Before(until) {
				if b, e := os.ReadFile(filepath.Join(root, "ready.json")); e == nil {
					json.Unmarshal(b, &ids)
				}
				select {
				case <-done:
					a := <-ch
					t.Fatalf("fixture exited before ready: %v %s", a.err, a.result.Output)
				default:
				}
				if ids.Child == 0 {
					time.Sleep(10 * time.Millisecond)
				}
			}
			if ids.Child == 0 {
				t.Fatal("fixture readiness unavailable")
			}
			var retained []syscall.Handle
			for _, pid := range []uint32{ids.Root, ids.Child} {
				h, e := syscall.OpenProcess(0x100000|0x1000, false, pid)
				if e != nil {
					t.Fatal(e)
				}
				retained = append(retained, h)
				defer syscall.CloseHandle(h)
				s, e := syscall.WaitForSingleObject(h, 0)
				if e != nil || s != 258 {
					t.Fatal("process was not alive when retained")
				}
			}
			if cancelRun {
				cancel()
			} else {
				if e := os.WriteFile(filepath.Join(root, "release"), nil, 0600); e != nil {
					t.Fatal(e)
				}
			}
			a := <-ch
			if !a.result.TreeGone || (cancelRun && a.err == nil) || (!cancelRun && (a.err != nil || a.result.Exit != 0 || !strings.Contains(string(a.result.Output), "owned fixture complete"))) {
				t.Fatalf("result %+v error %v", a.result, a.err)
			}
			for _, h := range retained {
				s, e := syscall.WaitForSingleObject(h, 0)
				if e != nil || s != 0 {
					t.Fatal("same retained process handle not signalled")
				}
			}
		})
	}
}
