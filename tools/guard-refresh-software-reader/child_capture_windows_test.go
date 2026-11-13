//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAskChildCaptureFixedStdinChild(t *testing.T) {
	if os.Getenv("ASK_CAPTURE_STDIN_CHILD") != "1" {
		return
	}
	if !askWorkerParent() {
		os.Exit(6)
	}
	var raw [32]byte
	n, err := io.ReadFull(os.Stdin, raw[:])
	defer clear(raw[:])
	if err != io.ErrUnexpectedEOF || string(raw[:n]) != "owned-fixed-request\n" {
		os.Exit(7)
	}
	_, _ = os.Stdout.Write(raw[:n])
	os.Exit(0)
}

func TestAskChildCaptureFixedStdinReachesEOFAndCollects(t *testing.T) {
	t.Setenv("ASK_CAPTURE_STDIN_CHILD", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	observed := 0
	var output bytes.Buffer
	ctx = context.WithValue(ctx, askChildCaptureKey{}, askChildCapture{
		BeforeResume: func(pid uint32, created uint64) error { observed++; return nil },
		Stdout:       &output,
		Stdin:        bytes.NewReader([]byte("owned-fixed-request\n")),
	})
	r := askOwnedChild(ctx, os.Args[0], "-test.run=^TestAskChildCaptureFixedStdinChild$")
	defer clear(r.Raw)
	defer clear(output.Bytes())
	if observed != 1 || !r.Assigned || !r.Reaped || !r.WaitVerified || r.TimedOut || r.Exit != 0 || len(r.Raw) != 0 || output.String() != "owned-fixed-request\n" {
		t.Fatal("fixed stdin/EOF child not collected", observed, r)
	}
}

func TestAskChildCaptureBeforeResumeAndOwnedStdout(t *testing.T) {
	for _, mode := range []string{"normal", "reject", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			t.Setenv("ASK_OWNED_SUPERVISOR_CHILD", "1")
			t.Setenv("ASK_OWNED_MODE", "normal")
			t.Setenv("ASK_OWNED_ROOT", root)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			observed := 0
			var captured bytes.Buffer
			ctx = context.WithValue(ctx, askChildCaptureKey{}, askChildCapture{Stdout: &captured, BeforeResume: func(pid uint32, creation uint64) error {
				observed++
				if pid == 0 || creation == 0 {
					t.Error("missing native process identity")
				}
				h, err := syscall.OpenProcess(0x1000, false, pid)
				if err != nil {
					t.Error("observer process unavailable", err)
				} else {
					var c, e, k, u syscall.Filetime
					if syscall.GetProcessTimes(h, &c, &e, &k, &u) != nil || creation != uint64(c.HighDateTime)<<32|uint64(c.LowDateTime) {
						t.Error("observer identity mismatch")
					}
					syscall.CloseHandle(h)
				}
				if mode == "cancel" {
					cancel()
				}
				if mode == "reject" {
					return errors.New("owned_reject")
				}
				return nil
			}})
			r := askOwnedChild(ctx, os.Args[0], "-test.run=^TestAskOwnedSupervisorChild$")
			defer clear(r.Raw)
			if observed != 1 || !r.Started || !r.WaitVerified || !r.Reaped {
				t.Fatal("native child capture or collection missing", observed, r)
			}
			if mode == "normal" {
				if !r.Assigned || r.Exit != 0 || !bytes.Contains(captured.Bytes(), []byte(`{"owned":true}`)) || len(r.Raw) != 0 {
					t.Fatal("stdout not delegated to owned capture", r)
				}
			} else if r.Exit == 0 || captured.Len() != 0 {
				t.Fatal("rejected child ran", r)
			}
			if _, err := os.Stat(filepath.Join(root, "backup.exe")); !os.IsNotExist(err) {
				t.Fatal("unexpected child side effect")
			}
			clear(captured.Bytes())
		})
	}
}
