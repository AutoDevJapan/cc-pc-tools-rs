package main

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func omb3255SyntheticBinding() omb3255ContextBinding {
	return omb3255ContextBinding{
		assignmentID: "4559e952-315a-4053-aee9-125d627cb639",
		botID:        "52137063-f227-4e4f-b0ff-c91661cb8950", taskID: "62780a26-01a6-4464-bb87-c7f3fa09df81",
		responseID: "6c38b9d1-b4a6-4860-a530-71f711bbd8f5", startedAt: 1792848636233, finishedAt: 1792848928582,
		executions: 1, status: "completed", outcome: "done", nativeThreadID: "synthetic-native-thread", nativeTurnID: "synthetic-native-turn",
	}
}

type omb3255SyntheticBoundStream struct {
	io.Reader
	before, after                 omb3255ContextBinding
	boundCalls, closed            int
	beforeErr, afterErr, closeErr error
}

func (s *omb3255SyntheticBoundStream) Binding() (omb3255ContextBinding, error) {
	s.boundCalls++
	if s.boundCalls == 1 {
		return s.before, s.beforeErr
	}
	return s.after, s.afterErr
}

func (s *omb3255SyntheticBoundStream) Close() error { s.closed++; return s.closeErr }

func omb3255SyntheticBoundInput() *omb3255SyntheticBoundStream {
	b := omb3255SyntheticBinding()
	return &omb3255SyntheticBoundStream{Reader: strings.NewReader(omb3255SyntheticHeader() + omb3255SyntheticContext(`"model":"gpt-6-luna","effort":"low"`)), before: b, after: b}
}

func TestOMB3255OriginalBindingAdmitsCompatibleInputWithoutGrant(t *testing.T) {
	s := omb3255SyntheticBoundInput()
	opened := 0
	got := omb3255ProjectOriginal4559(omb3255SyntheticBinding(), func() (omb3255BoundContextStream, error) { opened++; return s, nil })
	if got.Status != "observed" || got.Model == nil || *got.Model != "gpt-6-luna" || got.Effort == nil || *got.Effort != "low" {
		t.Fatal("compatible fixed execution and stream did not deliver the observed context")
	}
	if opened != 1 || s.closed != 1 || s.boundCalls != 2 || got.SourceQualified || got.ExecutionVerified || got.AccessGranted {
		t.Fatal("stream lifecycle or no-grant contract changed")
	}
}

func TestOMB3255OriginalBindingRejectsMismatchBeforeOpening(t *testing.T) {
	// The matching control above establishes that this opener is reachable.
	for _, field := range []string{"assignment", "bot", "task", "response", "start", "end", "count", "status", "outcome", "native missing", "native equal", "native invalid"} {
		t.Run(field, func(t *testing.T) {
			b := omb3255SyntheticBinding()
			switch field {
			case "assignment":
				b.assignmentID = "another-assignment"
			case "bot":
				b.botID = "another-bot"
			case "task":
				b.taskID = "another-task"
			case "response":
				b.responseID = "another-response"
			case "start":
				b.startedAt++
			case "end":
				b.finishedAt++
			case "count":
				b.executions++
			case "status":
				b.status = "running"
			case "outcome":
				b.outcome = "failed"
			case "native missing":
				b.nativeTurnID = ""
			case "native equal":
				b.nativeTurnID = b.nativeThreadID
			case "native invalid":
				b.nativeThreadID = "synthetic/private/path"
			}
			opened := 0
			got := omb3255ProjectOriginal4559(b, func() (omb3255BoundContextStream, error) { opened++; return omb3255SyntheticBoundInput(), nil })
			if !reflect.DeepEqual(got, omb3255ContextUnavailable()) || opened != 0 {
				t.Fatal("mismatched execution opened or returned a stream")
			}
		})
	}
}

func TestOMB3255OriginalBindingDiscardsFailuresAndCloses(t *testing.T) {
	for _, failure := range []string{"before mismatch", "after mismatch", "before error", "after error", "close error", "read error", "open error with stream"} {
		t.Run(failure, func(t *testing.T) {
			s := omb3255SyntheticBoundInput()
			var openErr error
			switch failure {
			case "before mismatch":
				s.before.responseID = "another-response"
			case "after mismatch":
				s.after.nativeTurnID = "another-turn"
			case "before error":
				s.beforeErr = errors.New("synthetic private failure")
			case "after error":
				s.afterErr = errors.New("synthetic private failure")
			case "close error":
				s.closeErr = errors.New("synthetic private failure")
			case "read error":
				s.Reader = &omb3255BrokenReader{[]byte(omb3255SyntheticHeader())}
			case "open error with stream":
				openErr = errors.New("synthetic private failure")
			}
			got := omb3255ProjectOriginal4559(omb3255SyntheticBinding(), func() (omb3255BoundContextStream, error) { return s, openErr })
			if !reflect.DeepEqual(got, omb3255ContextUnavailable()) || s.closed != 1 {
				t.Fatal("failed source retained observations or leaked its stream")
			}
		})
	}
	if got := omb3255ProjectOriginal4559(omb3255SyntheticBinding(), nil); !reflect.DeepEqual(got, omb3255ContextUnavailable()) {
		t.Fatal("missing source returned evidence")
	}
	if got := omb3255ProjectOriginal4559(omb3255SyntheticBinding(), func() (omb3255BoundContextStream, error) { return nil, errors.New("synthetic source unavailable") }); !reflect.DeepEqual(got, omb3255ContextUnavailable()) {
		t.Fatal("unavailable source returned evidence")
	}
}
