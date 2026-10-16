package main

import (
	"context"
	"errors"
	"testing"
)

func TestReadRetryIsFiniteAndNeverRetriesRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, stderr string
		want         int
		recover      bool
	}{
		{"transport recovers", "TLS handshake timeout", 2, true},
		{"transport exhausted", "TLS handshake timeout", 3, false},
		{"authorization", "HTTP 403", 1, false},
		{"not found is terminal", "HTTP 404", 1, false},
		{"not found takes precedence over transport text", "HTTP 404 unexpected EOF", 1, false},
		{"policy takes precedence", "gh-gate: refused: TLS handshake timeout", 1, false},
		{"unknown failure", "exit status 1", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			b, e := finiteRead(context.Background(), func() ([]byte, string, error) {
				calls++
				if tc.recover && calls == 2 {
					return []byte("ok"), "", nil
				}
				return nil, tc.stderr, errors.New("failed")
			}, func() error { return nil })
			if calls != tc.want || (e == nil) != tc.recover || (tc.recover && string(b) != "ok") {
				t.Fatalf("calls=%d success=%t", calls, e == nil)
			}
		})
	}
}

func TestGitHubReadFailureClassificationNeverIncludesRawText(t *testing.T) {
	for _, tc := range []struct{ stderr, class string }{
		{"gh: Not Found (HTTP 404) opaque-sensitive-response", "not-found"},
		{"HTTP 403 opaque-sensitive-response", "authorization-or-permission"},
		{"gh-gate: refused HTTP 404 TLS handshake timeout opaque-sensitive-response", "policy-refused"},
		{"HTTP 503 opaque-sensitive-response", "temporary-transport-or-server"},
		{"opaque-sensitive-response", "unclassified"},
	} {
		_, err := finiteRead(context.Background(), func() ([]byte, string, error) {
			return nil, tc.stderr, errors.New("opaque-sensitive-command-error")
		}, func() error { return nil })
		want := "GitHub read failed (" + tc.class + ")"
		if err == nil || err.Error() != want {
			t.Fatalf("unsafe or inaccurate error classification; want %s", tc.class)
		}
	}
}

func TestReadRetryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	_, e := finiteRead(ctx, func() ([]byte, string, error) {
		calls++
		cancel()
		return nil, "TLS handshake timeout", errors.New("failed")
	}, func() error { return ctx.Err() })
	if calls != 1 || !errors.Is(e, context.Canceled) {
		t.Fatalf("calls=%d error=%v", calls, e)
	}
}
