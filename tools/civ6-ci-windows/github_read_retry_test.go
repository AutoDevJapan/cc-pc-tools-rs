package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGitHubReadRetriesOnlyTransientTransport(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		transient            bool
		recoverAt, wantCalls int
	}{
		{"recovers", true, 3, 3}, {"exhausted", true, 99, 3}, {"denied", false, 99, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, waits := 0, 0
			denied := errors.New("permanent read rejection")
			b, err := retryGitHubRead(context.Background(), func() ([]byte, error) {
				calls++
				if calls == tc.recoverAt {
					return []byte("verified"), nil
				}
				if tc.transient {
					return nil, errGitHubReadTransport
				}
				return nil, denied
			}, func(_ context.Context, d time.Duration) error {
				waits++
				if d != time.Duration(waits)*500*time.Millisecond {
					t.Error("wrong finite backoff")
				}
				return nil
			})
			if calls != tc.wantCalls || waits != calls-1 {
				t.Fatalf("calls=%d waits=%d", calls, waits)
			}
			if tc.recoverAt == 3 {
				if err != nil || string(b) != "verified" {
					t.Fatal("recovery lost")
				}
			} else if err == nil {
				t.Fatal("failure accepted")
			}
			if !tc.transient && !errors.Is(err, denied) {
				t.Fatal("denial changed")
			}
		})
	}
}

func TestGitHubReadCancellationNeverAcceptsLateResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	b, err := retryGitHubRead(ctx, func() ([]byte, error) { cancel(); return []byte("late"), nil }, func(context.Context, time.Duration) error { t.Fatal("wait after cancel"); return nil })
	if !errors.Is(err, context.Canceled) || b != nil {
		t.Fatal("late success accepted")
	}
	calls := 0
	_, err = retryGitHubRead(ctx, func() ([]byte, error) { calls++; return nil, nil }, waitGitHubRead)
	if calls != 0 || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled read started")
	}
}

func TestGitHubReadTransportClassification(t *testing.T) {
	for _, s := range []string{"net/http: TLS handshake timeout", "unexpected EOF", "connection reset by peer", "wsarecv: An existing connection was forcibly closed by the remote host"} {
		if !transientGitHubRead(s) {
			t.Fatal("transport not classified")
		}
	}
	for _, s := range []string{"HTTP 403: forcibly closed by the remote host", "HTTP 403: Forbidden: unexpected EOF", "HTTP 401: Unauthorized: TLS handshake timeout", "HTTP 404: Not Found: unexpected EOF", "HTTP 429: TLS handshake timeout", "Forbidden: connection reset by peer", "input exceeds limit", "HTTP 404: Not Found", "guard refused request", "invalid JSON", ""} {
		if transientGitHubRead(s) {
			t.Fatal("permanent failure retried")
		}
	}
}
