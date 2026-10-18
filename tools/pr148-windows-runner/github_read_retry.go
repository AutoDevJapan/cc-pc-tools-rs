package main

import (
	"context"
	"errors"
	"strings"
	"time"
)

var errGitHubReadTransport = errors.New("temporary GitHub read transport failure")

// Only explicitly classified GET transport failures are retried. A denied
// request, an invalid response, a size violation or an expired parent stops.
func retryGitHubRead(ctx context.Context, attempt func() ([]byte, error), wait func(context.Context, time.Duration) error) ([]byte, error) {
	for n := 0; n < 3; n++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b, err := attempt()
		if expired := ctx.Err(); expired != nil {
			return nil, expired
		}
		if !errors.Is(err, errGitHubReadTransport) || n == 2 {
			return b, err
		}
		if err := wait(ctx, time.Duration(n+1)*500*time.Millisecond); err != nil {
			return nil, err
		}
	}
	return nil, errGitHubReadTransport
}

func transientGitHubRead(stderr string) bool {
	s := strings.ToLower(stderr)
	if strings.Contains(s, "http 4") || strings.Contains(s, "forbidden") || strings.Contains(s, "unauthorized") {
		return false
	}
	return strings.Contains(s, "tls handshake timeout") || strings.Contains(s, "unexpected eof") || strings.Contains(s, "connection reset by peer")
}

func waitGitHubRead(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
