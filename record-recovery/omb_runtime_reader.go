package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"time"
	"unicode/utf8"
)

type omb3255RuntimeRef struct {
	AssignmentID                string
	StartedAt, FinishedAt       int64
	ThreadID, TurnID, ResultSHA string
}

type omb3255RuntimeRecord struct {
	AssignmentID string                    `json:"assignment_id"`
	StartedAt    int64                     `json:"started_at_ms"`
	FinishedAt   int64                     `json:"finished_at_ms"`
	Observation  omb3255ContextObservation `json:"observation"`
}

type omb3255RuntimeStream interface {
	io.ReadCloser
	Unchanged() bool
}

type omb3255RuntimeDeps struct {
	Prepare func() ([]omb3255RuntimeRef, error)
	Open    func() (omb3255RuntimeStream, error)
	Recheck func([]omb3255RuntimeRef) bool
}

func omb3255InspectRuntime(deps omb3255RuntimeDeps) []omb3255RuntimeRecord {
	if deps.Prepare == nil || deps.Open == nil || deps.Recheck == nil {
		return nil
	}
	refs, err := deps.Prepare()
	if err != nil || !omb3255RuntimeRefsValid(refs) {
		return nil
	}
	unavailable := make([]omb3255RuntimeRecord, len(refs))
	for i, ref := range refs {
		unavailable[i] = omb3255RuntimeRecord{ref.AssignmentID, ref.StartedAt, ref.FinishedAt, omb3255ContextUnavailable()}
	}
	stream, err := deps.Open()
	if stream == nil {
		return unavailable
	}
	if err != nil || !stream.Unchanged() {
		_ = stream.Close()
		return unavailable
	}
	data, readErr := io.ReadAll(io.LimitReader(stream, (64<<20)+1))
	observations, valid := omb3255NativeMetadata(data, refs)
	stable := stream.Unchanged() && deps.Recheck(refs)
	closeErr := stream.Close()
	if readErr != nil || !valid || !stable || closeErr != nil {
		return unavailable
	}
	for i, observation := range observations {
		// Only the production factory's live normal-API admission and the pinned
		// source's recheck can reach here. The pure core itself grants nothing.
		if observation.ContextAt != nil {
			observation.SourceQualified = true
			observation.ExecutionVerified = true
		}
		unavailable[i].Observation = observation
	}
	return unavailable
}

func omb3255RuntimeRefsValid(refs []omb3255RuntimeRef) bool {
	if len(refs) == 0 || len(refs) > 16 {
		return false
	}
	assignments, turns := map[string]bool{}, map[string]bool{}
	for _, ref := range refs {
		if !omb3255ContextID(ref.AssignmentID) || !omb3255ContextID(ref.ThreadID) || !omb3255ContextID(ref.TurnID) ||
			ref.ThreadID != refs[0].ThreadID || ref.StartedAt <= 0 || ref.FinishedAt <= ref.StartedAt ||
			assignments[ref.AssignmentID] || turns[ref.TurnID] || len(ref.ResultSHA) != 64 {
			return false
		}
		if _, err := hex.DecodeString(ref.ResultSHA); err != nil {
			return false
		}
		assignments[ref.AssignmentID], turns[ref.TurnID] = true, true
	}
	return true
}

// This receives one already admitted native handle's bytes, never OMB transport
// or a caller's requested settings. No native body, file hash or path is emitted.
func omb3255NativeMetadata(data []byte, refs []omb3255RuntimeRef) ([]omb3255ContextObservation, bool) {
	if !omb3255RuntimeRefsValid(refs) || len(data) == 0 || len(data) > 64<<20 || data[len(data)-1] != '\n' || !utf8.Valid(data) {
		return nil, false
	}
	var header []byte
	selected := make(map[string][]byte)
	wanted := make(map[string]bool)
	for _, ref := range refs {
		wanted[ref.TurnID] = true
	}
	for len(data) != 0 {
		end := bytes.IndexByte(data, '\n')
		if end < 0 || end > 4<<20 {
			return nil, false
		}
		line := bytes.Trim(data[:end], " \t\r")
		data = data[end+1:]
		if len(line) == 0 {
			continue
		}
		if !omb3255UniqueContextJSON(line) {
			return nil, false
		}
		var row map[string]json.RawMessage
		if json.Unmarshal(line, &row) != nil || row == nil {
			return nil, false
		}
		var kind string
		if json.Unmarshal(row["type"], &kind) != nil || kind == "" {
			return nil, false
		}
		if kind != "session_meta" && kind != "turn_context" {
			continue
		}
		var payload map[string]json.RawMessage
		if json.Unmarshal(row["payload"], &payload) != nil || payload == nil {
			return nil, false
		}
		if kind == "session_meta" {
			var id string
			if json.Unmarshal(payload["id"], &id) != nil || id != refs[0].ThreadID {
				return nil, false
			}
			header, _ = json.Marshal(map[string]any{"type": kind, "payload": map[string]json.RawMessage{"id": payload["id"]}})
			continue
		}
		var turn string
		if raw, exists := payload["turn_id"]; exists && json.Unmarshal(raw, &turn) != nil {
			return nil, false
		}
		if !wanted[turn] {
			continue
		}
		if header == nil || selected[turn] != nil {
			return nil, false
		}
		fields := map[string]json.RawMessage{"turn_id": payload["turn_id"]}
		for _, key := range []string{"model", "effort"} {
			if raw, exists := payload[key]; exists {
				fields[key] = raw
			}
		}
		selected[turn], _ = json.Marshal(map[string]any{"type": kind, "timestamp": row["timestamp"], "payload": fields})
	}
	if header == nil {
		return nil, false
	}
	results := make([]omb3255ContextObservation, len(refs))
	for i, ref := range refs {
		metadata := append(append(append([]byte{}, header...), '\n'), selected[ref.TurnID]...)
		metadata = append(metadata, '\n')
		results[i] = omb3255ProjectContext(omb3255ContextScope{ref.ThreadID, ref.TurnID, time.UnixMilli(ref.StartedAt), time.UnixMilli(ref.FinishedAt)}, bytes.NewReader(metadata))
	}
	return results, true
}
