//go:build windows && amd64

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestGitHubReadCaptureChild(t *testing.T) {
	switch os.Getenv("CC_API_CAPTURE_FIXTURE") {
	case "holder":
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "parent":
		c := exec.Command(os.Args[0], "-test.run=^TestGitHubReadCaptureChild$")
		hidden(c)
		c.Env = append(os.Environ(), "CC_API_CAPTURE_FIXTURE=holder")
		c.Stdout, c.Stderr = os.Stdout, os.Stderr
		if err := c.Start(); err != nil {
			os.Exit(11)
		}
		fmt.Fprintf(os.Stdout, `{"pid":%d}`, c.Process.Pid)
		os.Exit(0)
	case "overflow":
		for i := 0; i < 10000; i++ {
			_, _ = os.Stdout.Write(make([]byte, 4096))
		}
		os.Exit(0)
	case "stderr":
		for i := 0; i < 100; i++ {
			_, _ = os.Stderr.Write(make([]byte, 4096))
		}
		os.Exit(0)
	}
}

func TestGitHubReadCaptureBoundsActualStderr(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGitHubReadCaptureChild$")
	hidden(c)
	c.Env = append(os.Environ(), "CC_API_CAPTURE_FIXTURE=stderr")
	preview := &apiErrorPreview{}
	c.Stderr = preview
	_, overflow, err := captureGitHubRead(c, 1024, cancel)
	if err != nil || overflow || len(preview.String()) != 8192 {
		t.Fatalf("stderr bound bypassed: %v", err)
	}
}

func TestGitHubReadCaptureClosesInheritedPipes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGitHubReadCaptureChild$")
	hidden(c)
	c.Env = append(os.Environ(), "CC_API_CAPTURE_FIXTURE=parent")
	c.Stderr = &apiErrorPreview{}
	start := time.Now()
	b, overflow, err := captureGitHubRead(c, 1024, cancel)
	var child struct{ PID int }
	if json.Unmarshal(b, &child) != nil || child.PID == 0 {
		t.Fatalf("holder identity missing: %v", err)
	}
	p, findErr := os.FindProcess(child.PID)
	if findErr != nil {
		t.Fatal(findErr)
	}
	// Only this synthetic descendant is stopped; wait confirms it has exited.
	defer p.Release()
	if killErr := p.Kill(); killErr != nil {
		t.Fatal(killErr)
	}
	if _, waitErr := p.Wait(); waitErr != nil {
		t.Fatal(waitErr)
	}
	if overflow || !errors.Is(err, exec.ErrWaitDelay) || time.Since(start) >= 7*time.Second {
		t.Fatalf("inherited pipes were not bounded: overflow=%t err=%v", overflow, err)
	}
}

func TestGitHubReadCaptureOverflowCancelsAndDrains(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGitHubReadCaptureChild$")
	hidden(c)
	c.Env = append(os.Environ(), "CC_API_CAPTURE_FIXTURE=overflow")
	c.Stderr = &apiErrorPreview{}
	b, overflow, err := captureGitHubRead(c, 8, cancel)
	if !overflow || len(b) != 8 || err == nil || !errors.Is(ctx.Err(), context.Canceled) || c.ProcessState == nil {
		t.Fatalf("overflow not rejected and drained: bytes=%d overflow=%t err=%v", len(b), overflow, err)
	}
}
