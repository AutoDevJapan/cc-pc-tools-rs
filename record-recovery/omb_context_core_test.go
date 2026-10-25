package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"
)

func omb3255SyntheticScope() omb3255ContextScope {
	at := time.Date(2026, 10, 24, 13, 30, 0, 0, time.UTC)
	return omb3255ContextScope{"synthetic-native-thread", "synthetic-native-turn", at, at.Add(5 * time.Minute)}
}

func omb3255SyntheticHeader() string {
	return `{"type":"session_meta","payload":{"id":"synthetic-native-thread","session_id":"different-root","model":"gpt-6-luna","effort":"max","body":"synthetic-private-marker"}}` + "\n"
}

func omb3255SyntheticContext(extra string) string {
	return `{"type":"turn_context","timestamp":"2026-10-24T13:31:00Z","payload":{"turn_id":"synthetic-native-turn",` + extra + `}}` + "\n"
}

func TestOMB3255ContextUsesObservedFieldsAndNoGrant(t *testing.T) {
	body := omb3255SyntheticHeader() + omb3255SyntheticContext(`"model":"gpt-6.1-sol","effort":"low","cwd":"synthetic-private-path","body":"synthetic-private-marker"`)
	got := omb3255ProjectContext(omb3255SyntheticScope(), strings.NewReader(body))
	if got.Status != "observed" || got.Model == nil || *got.Model != "gpt-6.1-sol" || got.Effort == nil || *got.Effort != "low" {
		t.Fatalf("matching native context was not extracted without request substitution: %+v", got)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	want := map[string]any{"status": "observed", "model": "gpt-6.1-sol", "model_state": "observed", "effort": "low", "effort_state": "observed", "context_at": "2026-10-24T13:31:00Z", "source": "native_turn_context", "source_qualified": false, "execution_verified": false, "access_granted": false}
	if json.Unmarshal(raw, &fields) != nil || !reflect.DeepEqual(fields, want) {
		t.Fatal("bounded context output key set or values changed")
	}
	for _, marker := range []string{"synthetic-private", "synthetic-native", "different-root", "cwd", "body", "provider", "max", "gpt-6-luna"} {
		if bytes.Contains(raw, []byte(marker)) {
			t.Fatalf("unobserved field reached output: %s", marker)
		}
	}
}

func TestOMB3255ContextDoesNotBorrowOtherTurnsOrWindows(t *testing.T) {
	good := omb3255SyntheticHeader() + omb3255SyntheticContext(`"model":"gpt-6-luna","effort":"max"`)
	for name, body := range map[string]string{
		"root session only": strings.Replace(strings.Replace(good, `"id":"synthetic-native-thread"`, `"id":"other-native-thread"`, 1), `"session_id":"different-root"`, `"session_id":"synthetic-native-thread"`, 1),
		"wrong thread":      strings.Replace(good, `"id":"synthetic-native-thread"`, `"id":"other-native-thread"`, 1),
		"root turn only":    strings.Replace(good, `"turn_id":"synthetic-native-turn"`, `"turn_id":"other-turn","root_turn_id":"synthetic-native-turn"`, 1),
		"missing turn":      strings.Replace(good, `"turn_id":"synthetic-native-turn",`, "", 1),
		"before":            strings.Replace(good, "13:31:00Z", "13:29:59Z", 1),
		"after":             strings.Replace(good, "13:31:00Z", "13:35:00.001Z", 1),
		"bad time":          strings.Replace(good, "2026-10-24T13:31:00Z", "not-a-time", 1),
		"missing header":    omb3255SyntheticContext(`"model":"gpt-6-luna","effort":"max"`),
	} {
		t.Run(name, func(t *testing.T) {
			got := omb3255ProjectContext(omb3255SyntheticScope(), strings.NewReader(body))
			if !reflect.DeepEqual(got, omb3255ContextUnavailable()) {
				t.Fatal("unbound context became an observation")
			}
		})
	}
}

func TestOMB3255ContextMissingFieldsStaySeparate(t *testing.T) {
	for _, tc := range []struct{ fields, modelState, effortState string }{
		{`"model":"gpt-6-luna"`, "observed", "missing"},
		{`"model":"gpt-6-luna","effort":null`, "observed", "null"},
		{`"model":"gpt-6-luna","effort":true`, "observed", "invalid"},
		{`"model":"gpt-6-luna","effort":"opaque-private-effort"`, "observed", "unrecognized"},
		{`"effort":"max","requested_model":"gpt-6-luna"`, "missing", "observed"},
		{`"model":"opaque-private-model","effort":"max"`, "unrecognized", "observed"},
		{`"model":null,"effort":"max"`, "null", "observed"},
		{`"model":false,"effort":"max"`, "invalid", "observed"},
	} {
		t.Run(tc.modelState+"/"+tc.effortState, func(t *testing.T) {
			got := omb3255ProjectContext(omb3255SyntheticScope(), strings.NewReader(omb3255SyntheticHeader()+omb3255SyntheticContext(tc.fields)))
			if got.Status != "partial" || got.ModelState != tc.modelState || got.EffortState != tc.effortState || got.SourceQualified || got.ExecutionVerified || got.AccessGranted {
				t.Fatalf("missing or invalid value was filled from another source: %+v", got)
			}
			raw, _ := json.Marshal(got)
			if bytes.Contains(raw, []byte("opaque-private")) {
				t.Fatal("unrecognized field value leaked")
			}
		})
	}
}

type omb3255BrokenReader struct{ body []byte }

func (r *omb3255BrokenReader) Read(p []byte) (int, error) {
	if len(r.body) == 0 {
		return 0, errors.New("synthetic source failed")
	}
	n := copy(p, r.body)
	r.body = r.body[n:]
	return n, errors.New("synthetic source failed")
}

func TestOMB3255ContextDoesNotReturnPartialReadOrAmbiguity(t *testing.T) {
	line := omb3255SyntheticContext(`"model":"gpt-6-luna","effort":"max"`)
	good := omb3255SyntheticHeader() + line
	for name, input := range map[string]io.Reader{
		"escaped duplicate": strings.NewReader(strings.Replace(good, `"model":"gpt-6-luna","effort":"max"`, `"model":"gpt-6-luna","\u006dodel":"gpt-6.1-sol","effort":"max"`, 1)),
		"depth limit":       strings.NewReader(good + omb3255SyntheticContext(`"model":"gpt-6-luna","effort":"max","nested":`+strings.Repeat("[", 64)+"0"+strings.Repeat("]", 64))),
		"read error":        &omb3255BrokenReader{[]byte(good)},
		"duplicate context": strings.NewReader(good + line),
		"duplicate session": strings.NewReader(good + omb3255SyntheticHeader()),
		"truncated tail":    strings.NewReader(good + `{"type":`),
		"no newline":        strings.NewReader(strings.TrimSuffix(good, "\n")),
		"malformed newline": strings.NewReader(good + "{\n"),
		"duplicate key":     strings.NewReader(strings.Replace(good, `"model":"gpt-6-luna","effort":"max"`, `"model":"gpt-6-luna","model":"gpt-6.1-sol","effort":"max"`, 1)),
		"invalid utf8":      bytes.NewReader(append([]byte(good), 0xff, '\n')),
		"line limit":        strings.NewReader(good + strings.Repeat(" ", (64<<10)+1) + "\n"),
		"source limit":      strings.NewReader(good + strings.Repeat("\n", 1<<20)),
	} {
		t.Run(name, func(t *testing.T) {
			if got := omb3255ProjectContext(omb3255SyntheticScope(), input); !reflect.DeepEqual(got, omb3255ContextUnavailable()) {
				t.Fatalf("partial or ambiguous source became evidence: %+v", got)
			}
		})
	}
}

func TestOMB3255ContextIncludesBothTimeBoundaries(t *testing.T) {
	for _, timestamp := range []string{"2026-10-24T13:30:00Z", "2026-10-24T13:35:00Z", "2026-10-24T22:31:00+09:00"} {
		body := omb3255SyntheticHeader() + strings.Replace(omb3255SyntheticContext(`"model":"gpt-6-luna","effort":"max"`), "2026-10-24T13:31:00Z", timestamp, 1)
		got := omb3255ProjectContext(omb3255SyntheticScope(), strings.NewReader(body))
		if got.Status != "observed" || got.ContextAt == nil {
			t.Fatal("inclusive execution time boundary was lost")
		}
	}
}

func TestOMB3255ContextIgnoresOtherTurnFieldsAndRejectsBadBindings(t *testing.T) {
	other := strings.Replace(omb3255SyntheticContext(`"model":"gpt-6.1-sol","effort":"max"`), "synthetic-native-turn", "other-turn", 1)
	body := omb3255SyntheticHeader() + other + omb3255SyntheticContext(`"model":"gpt-6-luna"`) + other
	got := omb3255ProjectContext(omb3255SyntheticScope(), strings.NewReader(body))
	if got.Status != "partial" || got.Model == nil || *got.Model != "gpt-6-luna" || got.Effort != nil || got.EffortState != "missing" {
		t.Fatal("another turn supplied the selected turn's missing effort or model")
	}
	for _, kind := range []string{"thread missing", "turn missing", "reversed time", "time missing"} {
		scope := omb3255SyntheticScope()
		switch kind {
		case "thread missing":
			scope.nativeThreadID = ""
		case "turn missing":
			scope.nativeTurnID = ""
		case "reversed time":
			scope.finished = scope.started.Add(-time.Second)
		case "time missing":
			scope.started = time.Time{}
		}
		if got := omb3255ProjectContext(scope, strings.NewReader(body)); !reflect.DeepEqual(got, omb3255ContextUnavailable()) {
			t.Fatalf("invalid binding admitted a context: %s", kind)
		}
	}
}

func TestOMB3255ContextAcceptsExactSizeLimits(t *testing.T) {
	line := omb3255SyntheticContext(`"model":"gpt-6-luna","effort":"max"`)
	body := omb3255SyntheticHeader() + strings.TrimSuffix(line, "\n") + strings.Repeat(" ", (64<<10)-(len(line)-1)) + "\n"
	if got := omb3255ProjectContext(omb3255SyntheticScope(), strings.NewReader(body)); got.Status != "observed" {
		t.Fatal("exact 64KiB line was lost")
	}
	body += strings.Repeat("\n", (1<<20)-len(body))
	if got := omb3255ProjectContext(omb3255SyntheticScope(), strings.NewReader(body)); got.Status != "observed" {
		t.Fatal("exact 1MiB source was lost")
	}
}
