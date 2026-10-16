package main

import (
	"context"
	"errors"
	"strings"
	"time"
)

// Only idempotent reads with explicit transport/temporary-server failures may
// be retried. Credentials, policy refusals and mutations never use this path.
func transientGitHubRead(stderr string) bool {
	for _, refused := range []string{"gh-gate: refused", "blocked by policy", "HTTP 401", "HTTP 403", "permission denied"} {
		if strings.Contains(stderr, refused) {
			return false
		}
	}
	for _, temporary := range []string{"TLS handshake timeout", "i/o timeout", "connection reset by peer", "unexpected EOF", "HTTP 502", "HTTP 503", "HTTP 504"} {
		if strings.Contains(stderr, temporary) {
			return true
		}
	}
	return false
}

func readGitHub(ctx context.Context, endpoint string) ([]byte, error) {
	return finiteRead(ctx, func() ([]byte, string, error) {
		c := cmd(ctx, gh, "api", endpoint)
		stderr := boundedBuffer{limit: 64 << 10}
		c.Stderr = &stderr
		b, e := boundedOutput(c, 16<<20)
		return b, stderr.String(), e
	}, func() error {
		t := time.NewTimer(time.Second)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			return nil
		}
	})
}

func finiteRead(ctx context.Context, attempt func() ([]byte, string, error), pause func() error) ([]byte, error) {
	for i := 0; i < 3; i++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		b, stderr, e := attempt()
		if e == nil {
			return b, nil
		}
		if i == 2 || !transientGitHubRead(stderr) {
			return nil, errors.New("GitHub read failed")
		}
		if e = pause(); e != nil {
			return nil, e
		}
	}
	return nil, errors.New("GitHub read failed")
}
