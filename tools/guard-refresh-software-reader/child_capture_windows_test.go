//go:build windows

package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAskFiniteCaptureChild(t *testing.T) {
	mode := os.Getenv("ASK_FINITE_CAPTURE_CHILD")
	if mode == "" {
		return
	}
	if !askWorkerParent() {
		os.Exit(6)
	}
	if mode == "not-reading" {
		if os.WriteFile(filepath.Join(os.Getenv("ASK_CAPTURE_ROOT"), "ready"), []byte("owned"), 0600) != nil {
			os.Exit(7)
		}
		time.Sleep(10 * time.Second)
		os.Exit(0)
	}
	if mode == "overflow" {
		var block [65536]byte
		for i := range block {
			block[i] = 'x'
		}
		for i := 0; i < 129; i++ {
			if _, err := os.Stdout.Write(block[:]); err != nil {
				os.Exit(8)
			}
		}
		os.Exit(0)
	}
	var input [32]byte
	n, err := io.ReadFull(os.Stdin, input[:])
	defer clear(input[:])
	if mode == "nil" {
		if n != 0 || err != io.EOF {
			os.Exit(9)
		}
		_, _ = os.Stdout.Write([]byte("owned-eof"))
		os.Exit(0)
	}
	if err != io.ErrUnexpectedEOF || string(input[:n]) != "owned-fixed-request\n" {
		os.Exit(10)
	}
	_, _ = os.Stdout.Write(input[:n])
	if mode == "failure" {
		os.Exit(3)
	}
	os.Exit(0)
}

func TestAskFiniteCaptureOwnsBuffersAndCollects(t *testing.T) {
	for _, mode := range []string{"normal", "nil", "failure", "overflow", "already-canceled", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("ASK_FINITE_CAPTURE_CHILD", mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			input := []byte("owned-fixed-request\n")
			if mode == "nil" {
				input = nil
			}
			if mode == "oversize" {
				input = make([]byte, (1<<20)+1)
			}
			if mode == "already-canceled" {
				cancel()
			}
			ctx = context.WithValue(ctx, askChildCaptureKey{}, askChildCapture{Stdin: input})
			r := askOwnedChild(ctx, os.Args[0], "-test.run=^TestAskFiniteCaptureChild$")
			defer clear(r.Raw)
			if r.Capture == nil || len(r.Raw) != 0 {
				t.Fatal("capture result missing or exposed legacy raw")
			}
			defer clear(r.Capture.Stdout)
			if mode == "already-canceled" || mode == "oversize" {
				if r.Started || r.Capture.Complete || len(r.Capture.Stdout) != 0 || r.Capture.NativeProcess != nil {
					t.Fatal("invalid input started native process")
				}
				return
			}
			if !r.Started || !r.Assigned || !r.WaitVerified || !r.Reaped || r.TimedOut || r.Capture.NativeProcess == nil || r.Capture.NativeProcess.PID == 0 || r.Capture.NativeProcess.Creation == 0 {
				t.Fatal("native identity or collection incomplete")
			}
			if mode == "overflow" {
				if !r.Capture.Overflow || r.Capture.Complete || len(r.Capture.Stdout) != 8<<20 || r.Exit != 0 {
					t.Fatal("overflow not bounded and drained")
				}
				return
			}
			want := []byte("owned-fixed-request\n")
			if mode == "nil" {
				want = []byte("owned-eof")
			}
			wantExit := 0
			if mode == "failure" {
				wantExit = 3
			}
			if !r.Capture.Complete || r.Capture.Overflow || r.Exit != wantExit || !bytes.Equal(r.Capture.Stdout, want) {
				t.Fatal("finite capture bytes or exit mismatch")
			}
		})
	}
}

func TestAskFiniteCaptureCancelsChildThatDoesNotReadInput(t *testing.T) {
	root := physicalSoftwareFixture(t)
	t.Setenv("ASK_FINITE_CAPTURE_CHILD", "not-reading")
	t.Setenv("ASK_CAPTURE_ROOT", root)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	input := make([]byte, 1<<20)
	defer clear(input)
	ctx = context.WithValue(ctx, askChildCaptureKey{}, askChildCapture{Stdin: input})
	done := make(chan askChildResult, 1)
	go func() { done <- askOwnedChild(ctx, os.Args[0], "-test.run=^TestAskFiniteCaptureChild$") }()
	ready := false
	deadline := time.NewTimer(3 * time.Second)
	ticker := time.NewTicker(5 * time.Millisecond)
	defer deadline.Stop()
	defer ticker.Stop()
wait:
	for {
		select {
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
				ready = true
				break wait
			}
		case <-deadline.C:
			break wait
		}
	}
	cancel()
	select {
	case r := <-done:
		defer clear(r.Raw)
		if r.Capture != nil {
			defer clear(r.Capture.Stdout)
		}
		if !ready || !r.Started || !r.Reaped || !r.WaitVerified || !r.TimedOut || r.Capture == nil || r.Capture.Complete {
			t.Fatal("blocked native stdin not canceled and joined")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("finite input copy remained blocked")
	}
}
