package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type runnerAPIError struct{ code string }

func (e *runnerAPIError) Error() string {
	switch e.code {
	case "transport":
		return "runner API transport failed"
	case "denied":
		return "runner API access rejected"
	case "response":
		return "runner API response rejected"
	}
	return "runner API operation failed"
}

func classifyRunnerAPIError(stderr string) *runnerAPIError {
	s := strings.ToLower(stderr)
	// A policy/auth rejection always wins over a transport-looking substring.
	for _, marker := range []string{"denied", "rejected", "forbidden", "policy", "http 401", "http 403"} {
		if strings.Contains(s, marker) {
			return &runnerAPIError{code: "denied"}
		}
	}
	for _, marker := range []string{"tls handshake timeout", "i/o timeout", "connection reset by peer", "temporary failure in name resolution", "no such host", "unexpected eof", "http 502", "http 503", "http 504"} {
		if strings.Contains(s, marker) {
			return &runnerAPIError{code: "transport"}
		}
	}
	return &runnerAPIError{code: "operation"}
}

type runnerAPIInvoke func(context.Context, string, string, []byte) ([]byte, error)

func callRunnerAPI(ctx context.Context, method, endpoint string, payload, out any, invoke runnerAPIInvoke) error {
	var input []byte
	if payload != nil {
		var e error
		input, e = json.Marshal(payload)
		if e != nil {
			return errors.New("runner request rejected")
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return &runnerAPIError{code: "operation"}
		}
		b, e := invoke(ctx, method, endpoint, input)
		if e == nil {
			if out != nil && json.Unmarshal(b, out) != nil {
				return &runnerAPIError{code: "response"}
			}
			return nil
		}
		var apiErr *runnerAPIError
		if method != "GET" || attempt >= 2 || !errors.As(e, &apiErr) || apiErr == nil || apiErr.code != "transport" {
			return e
		}
		// Retry only an explicitly classified transport failure of a read.
		// POST/DELETE, invalid JSON, identity mismatch and policy denial do not retry.
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return &runnerAPIError{code: "operation"}
		case <-timer.C:
		}
	}
}
