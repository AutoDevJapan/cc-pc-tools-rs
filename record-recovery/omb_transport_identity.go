package main

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"time"
	"unicode/utf8"
)

// This unexported candidate is neither a source qualification nor a binding to
// an assignment. It has no JSON fields and must not become a context record.
type omb3255NativeCandidate struct {
	threadID, turnID string
}

func omb3255TransportIdentity(input io.Reader, started, finished time.Time) (omb3255NativeCandidate, bool) {
	empty := omb3255NativeCandidate{}
	if input == nil || started.IsZero() || finished.IsZero() || finished.Before(started) {
		return empty, false
	}
	data, err := io.ReadAll(io.LimitReader(input, (1<<20)+1))
	if err != nil || len(data) == 0 || len(data) > 1<<20 || data[len(data)-1] != '\n' || !utf8.Valid(data) {
		return empty, false
	}
	var candidate omb3255NativeCandidate
	var previous time.Time
	var threadRPC, turnRPC int64
	var resumeID string
	stage := 0
	for len(data) != 0 {
		end := bytes.IndexByte(data, '\n')
		if end < 0 || end > 64<<10 {
			return empty, false
		}
		line := bytes.Trim(data[:end], " \t\r")
		data = data[end+1:]
		if len(line) == 0 {
			continue
		}
		if !omb3255UniqueContextJSON(line) {
			return empty, false
		}
		row := omb3255TransportObject(line)
		if row == nil {
			return empty, false
		}
		if omb3255TransportString(row, "source") != "codex.app-server" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, omb3255TransportString(row, "at"))
		if err != nil {
			return empty, false
		}
		if at.Before(started) || at.After(finished) {
			continue
		}
		dir := omb3255TransportString(row, "dir")
		if at.Before(previous) || dir != "in" && dir != "out" {
			return empty, false
		}
		previous = at
		msg := omb3255TransportObject(row["msg"])
		if msg == nil {
			return empty, false
		}
		method := ""
		if raw, present := msg["method"]; present && (json.Unmarshal(raw, &method) != nil || method == "") {
			return empty, false
		}
		switch method {
		case "thread/start", "thread/resume":
			id, ok := omb3255TransportRPC(msg["id"])
			params := omb3255TransportObject(msg["params"])
			if dir != "out" || stage != 0 || !ok || params == nil {
				return empty, false
			}
			if method == "thread/resume" {
				resumeID = omb3255TransportString(params, "threadId")
				if !omb3255ContextID(resumeID) {
					return empty, false
				}
			}
			threadRPC, stage = id, 1
		case "turn/start":
			id, ok := omb3255TransportRPC(msg["id"])
			params := omb3255TransportObject(msg["params"])
			if dir != "out" || stage != 2 || !ok || id == threadRPC ||
				omb3255TransportString(params, "threadId") != candidate.threadID {
				return empty, false
			}
			turnRPC, stage = id, 3
		case "turn/completed":
			params := omb3255TransportObject(msg["params"])
			turn := omb3255TransportObject(params["turn"])
			if dir != "in" || stage != 4 || omb3255TransportString(params, "threadId") != candidate.threadID ||
				omb3255TransportString(turn, "id") != candidate.turnID || omb3255TransportString(turn, "status") != "completed" ||
				len(turn["error"]) != 0 && !bytes.Equal(bytes.TrimSpace(turn["error"]), []byte("null")) {
				return empty, false
			}
			stage = 5
		case "":
			if dir != "in" {
				continue
			}
			id, ok := omb3255TransportRPC(msg["id"])
			if !ok || stage == 0 || id != threadRPC && (stage < 3 || id != turnRPC) {
				continue
			}
			if raw := msg["error"]; len(raw) != 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return empty, false
			}
			if id == threadRPC && stage == 1 {
				result := omb3255TransportObject(msg["result"])
				thread := omb3255TransportObject(result["thread"])
				id := omb3255TransportString(thread, "id")
				if !omb3255ContextID(id) || resumeID != "" && resumeID != id {
					return empty, false
				}
				candidate.threadID, stage = id, 2
			} else if id == turnRPC && stage == 3 {
				result := omb3255TransportObject(msg["result"])
				turn := omb3255TransportObject(result["turn"])
				id := omb3255TransportString(turn, "id")
				if !omb3255ContextID(id) || id == candidate.threadID {
					return empty, false
				}
				candidate.turnID, stage = id, 4
			} else {
				return empty, false
			}
		}
	}
	if stage != 5 {
		return empty, false
	}
	return candidate, true
}

func omb3255TransportObject(raw json.RawMessage) map[string]json.RawMessage {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return nil
	}
	return object
}

func omb3255TransportString(object map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(object[key], &value)
	return value
}

func omb3255TransportRPC(raw json.RawMessage) (int64, bool) {
	// The reviewed producer allocates numeric IDs. Do not equate strings,
	// fractions, exponents, or values beyond JavaScript's exact integer range.
	id, err := strconv.ParseInt(string(raw), 10, 64)
	return id, err == nil && id >= 0 && id <= 1<<53-1
}
