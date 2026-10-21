package main

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestRunnerAPIOnlyRetriesReadTransportFailures(t *testing.T) {
	for _, method := range []string{"GET", "POST", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			calls := 0
			invoke := func(context.Context, string, string, []byte) ([]byte, error) {
				calls++
				if calls < 3 {
					return nil, &runnerAPIError{code: "transport"}
				}
				return []byte(`{"ID":7}`), nil
			}
			var out struct{ ID int }
			err := callRunnerAPI(context.Background(), method, "synthetic", nil, &out, invoke)
			if method == "GET" {
				if err != nil || calls != 3 || out.ID != 7 {
					t.Fatal("read did not recover within three attempts")
				}
			} else if err == nil || calls != 1 {
				t.Fatal("mutation repeated after ambiguous failure")
			}
		})
	}
}

func TestRunnerAPIDenialsInvalidResponsesAndCancellationDoNotRetry(t *testing.T) {
	for _, mode := range []string{"denied", "operation", "response", "cancelled", "exhausted"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			calls := 0
			invoke := func(context.Context, string, string, []byte) ([]byte, error) {
				calls++
				if mode == "response" {
					return []byte("invalid JSON"), nil
				}
				code := mode
				if mode == "exhausted" {
					code = "transport"
				}
				return nil, &runnerAPIError{code: code}
			}
			var out struct{ ID int }
			err := callRunnerAPI(ctx, "GET", "synthetic", nil, &out, invoke)
			want := 1
			if mode == "cancelled" {
				want = 0
			} else if mode == "exhausted" {
				want = 3
			}
			if err == nil || calls != want {
				t.Fatal("failure category did not retain its finite attempt count")
			}
		})
	}
}

func TestRunnerAPIDiagnosticNeverCarriesRawErrorText(t *testing.T) {
	for _, sample := range []struct{ text, want string }{
		{"dial tcp PRIVATE: i/o timeout", "transport"},
		{"HTTP 503 PRIVATE", "transport"},
		{"policy rejected: PRIVATE i/o timeout", "denied"},
		{"HTTP 403 PRIVATE", "denied"},
		{"PRIVATE unclassified", "operation"},
	} {
		e := classifyRunnerAPIError(sample.text)
		if strings.Contains(e.Error(), "PRIVATE") {
			t.Fatal("raw diagnostic escaped")
		}
		if e.code != sample.want {
			t.Fatal("API failure classification changed")
		}
	}
}

func TestRunnerAPIIncompleteOutputNeverRetries(t *testing.T) {
	for _, channel := range []string{"stdout", "stderr"} {
		for _, failed := range []bool{false, true} {
			t.Run(channel+map[bool]string{false: "/success-exit", true: "/failure-exit"}[failed], func(t *testing.T) {
				out := limitedCapture{limit: 64}
				diagnostic := limitedCapture{limit: 64}
				if _, err := diagnostic.Write([]byte("TLS handshake timeout\n")); err != nil {
					t.Fatal(err)
				}
				target := &out
				if channel == "stderr" {
					target = &diagnostic
				}
				if _, err := target.Write([]byte(strings.Repeat("x", 65) + "policy rejected PRIVATE")); err == nil || !target.truncated || target.data.Len() > target.limit {
					t.Fatal("oversized output was not bounded and remembered")
				}
				if _, err := target.Write([]byte("tail")); err != nil || !target.truncated {
					t.Fatal("later write cleared incomplete capture state")
				}
				var runErr error
				if failed {
					runErr = errors.New("synthetic command failure")
				}
				calls := 0
				invoke := func(context.Context, string, string, []byte) ([]byte, error) {
					calls++
					return runnerAPICaptureResult(&out, &diagnostic, runErr)
				}
				err := callRunnerAPI(context.Background(), "GET", "synthetic", nil, nil, invoke)
				if err == nil || err.Error() != "runner API capture incomplete" || calls != 1 || strings.Contains(err.Error(), "PRIVATE") {
					t.Fatal("incomplete output became a retry or leaked text")
				}
			})
		}
	}
	// The same complete transport diagnostic still permits finite read recovery.
	calls := 0
	invoke := func(context.Context, string, string, []byte) ([]byte, error) {
		calls++
		if calls < 3 {
			out, diagnostic := limitedCapture{limit: 64}, limitedCapture{limit: 64}
			_, _ = diagnostic.Write([]byte("TLS handshake timeout"))
			return runnerAPICaptureResult(&out, &diagnostic, errors.New("synthetic failure"))
		}
		return []byte(`{"ID":7}`), nil
	}
	var result struct{ ID int }
	if err := callRunnerAPI(context.Background(), "GET", "synthetic", nil, &result, invoke); err != nil || calls != 3 || result.ID != 7 {
		t.Fatal("complete read recovery control failed")
	}
}
