package main

import (
	"bytes"
	"encoding/json"
	"io"
	"time"
	"unicode/utf8"
)

type omb3255ContextScope struct {
	nativeThreadID, nativeTurnID string
	started, finished            time.Time
}

type omb3255ContextObservation struct {
	Status            string     `json:"status"`
	Model             *string    `json:"model"`
	ModelState        string     `json:"model_state"`
	Effort            *string    `json:"effort"`
	EffortState       string     `json:"effort_state"`
	ContextAt         *time.Time `json:"context_at"`
	Source            string     `json:"source"`
	SourceQualified   bool       `json:"source_qualified"`
	ExecutionVerified bool       `json:"execution_verified"`
	AccessGranted     bool       `json:"access_granted"`
}

func omb3255ContextUnavailable() omb3255ContextObservation {
	return omb3255ContextObservation{Status: "unavailable", ModelState: "unavailable", EffortState: "unavailable", Source: "native_turn_context"}
}

func omb3255ProjectContext(scope omb3255ContextScope, input io.Reader) omb3255ContextObservation {
	unavailable := omb3255ContextUnavailable()
	if input == nil || !omb3255ContextID(scope.nativeThreadID) || !omb3255ContextID(scope.nativeTurnID) ||
		scope.started.IsZero() || scope.finished.IsZero() || scope.finished.Before(scope.started) {
		return unavailable
	}
	// This core opens nothing. A future qualified producer must supply both the
	// bounded metadata stream and its independently established native binding.
	data, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
	if err != nil || len(data) == 0 || len(data) > 1<<20 || data[len(data)-1] != '\n' || !utf8.Valid(data) {
		return unavailable
	}
	var selected map[string]json.RawMessage
	var observedAt time.Time
	header := false
	for len(data) != 0 {
		end := bytes.IndexByte(data, '\n')
		if end < 0 {
			return unavailable
		}
		raw := data[:end]
		data = data[end+1:]
		if len(raw) > 64<<10 {
			return unavailable
		}
		line := bytes.Trim(raw, " \t\r")
		if len(line) == 0 {
			continue
		}
		if !omb3255UniqueContextJSON(line) {
			return unavailable
		}
		var entry struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(line, &entry) != nil || entry.Type == "" {
			return unavailable
		}
		if entry.Type != "session_meta" && entry.Type != "turn_context" {
			continue
		}
		var payload map[string]json.RawMessage
		if json.Unmarshal(entry.Payload, &payload) != nil || payload == nil {
			return unavailable
		}
		if entry.Type == "session_meta" {
			var id string
			if header || json.Unmarshal(payload["id"], &id) != nil || id != scope.nativeThreadID {
				return unavailable
			}
			header = true
			continue
		}
		var turn string
		if value, ok := payload["turn_id"]; ok && json.Unmarshal(value, &turn) != nil {
			return unavailable
		}
		if turn != scope.nativeTurnID {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, entry.Timestamp)
		if !header || selected != nil || err != nil || at.Before(scope.started) || at.After(scope.finished) {
			return unavailable
		}
		selected, observedAt = payload, at.UTC()
	}
	if !header || selected == nil {
		return unavailable
	}
	result := unavailable
	result.ContextAt = &observedAt
	result.Model, result.ModelState = omb3255ContextField(selected, "model", func(value string) bool {
		return value == "gpt-6.1-sol" || value == "gpt-6-luna"
	})
	result.Effort, result.EffortState = omb3255ContextField(selected, "effort", func(value string) bool {
		switch value {
		case "none", "minimal", "low", "medium", "high", "xhigh", "max":
			return true
		}
		return false
	})
	if result.ModelState == "observed" && result.EffortState == "observed" {
		result.Status = "observed"
	} else if result.ModelState == "observed" || result.EffortState == "observed" {
		result.Status = "partial"
	}
	// These values are input observations, never an access grant or proof that
	// the caller's native IDs belong to the original assignment and response.
	return result
}

func omb3255ContextID(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, c := range []byte(value) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == ':') {
			return false
		}
	}
	return true
}

func omb3255ContextField(payload map[string]json.RawMessage, key string, recognized func(string) bool) (*string, string) {
	raw, ok := payload[key]
	if !ok {
		return nil, "missing"
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, "null"
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return nil, "invalid"
	}
	if !recognized(value) {
		return nil, "unrecognized"
	}
	return &value, "observed"
}

// Reject shadowed identity/model fields before decoding a record. The bounds
// apply to all nested values, including unknown fields that are never emitted.
func omb3255UniqueContextJSON(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value func(int) bool
	value = func(depth int) bool {
		token, err := decoder.Token()
		if err != nil {
			return false
		}
		open, container := token.(json.Delim)
		if !container {
			return true
		}
		if depth >= 64 {
			return false
		}
		switch open {
		case '{':
			seen := make(map[string]bool)
			for decoder.More() {
				token, err := decoder.Token()
				key, ok := token.(string)
				if err != nil || !ok || seen[key] {
					return false
				}
				seen[key] = true
				if !value(depth + 1) {
					return false
				}
			}
			close, err := decoder.Token()
			return err == nil && close == json.Delim('}')
		case '[':
			for decoder.More() {
				if !value(depth + 1) {
					return false
				}
			}
			close, err := decoder.Token()
			return err == nil && close == json.Delim(']')
		}
		return false
	}
	if !value(0) {
		return false
	}
	_, err := decoder.Token()
	return err == io.EOF
}
