package main

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"time"
)

type apiOutputBuffer struct {
	buffer   bytes.Buffer
	limit    int64
	overflow bool
	cancel   context.CancelFunc
}

func (b *apiOutputBuffer) Write(p []byte) (int, error) {
	left := b.limit - int64(b.buffer.Len())
	if int64(len(p)) > left {
		if left > 0 {
			_, _ = b.buffer.Write(p[:int(left)])
		}
		b.overflow = true
		b.cancel()
		return int(left), errors.New("GitHub read response exceeds limit")
	}
	return b.buffer.Write(p)
}

func captureGitHubRead(c *exec.Cmd, limit int64, cancel context.CancelFunc) ([]byte, bool, error) {
	out := &apiOutputBuffer{limit: limit, cancel: cancel}
	c.Stdout = out
	// Run owns both copy goroutines. WaitDelay closes inherited pipes even if
	// the direct child exits but a descendant retains their write handles.
	c.WaitDelay = 2 * time.Second
	err := c.Run()
	return out.buffer.Bytes(), out.overflow, err
}
