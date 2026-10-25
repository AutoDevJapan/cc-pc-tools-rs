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

type omb3255RuntimeAcquisition struct {
	records []omb3255RuntimeRecord
	state   string
}

func omb3255InspectRuntime(deps omb3255RuntimeDeps) []omb3255RuntimeRecord {
	return omb3255InspectRuntimeState(deps).records
}

func omb3255InspectRuntimeState(deps omb3255RuntimeDeps) omb3255RuntimeAcquisition {
	if deps.Prepare == nil || deps.Open == nil || deps.Recheck == nil {
		return omb3255RuntimeAcquisition{state: "dependency_unavailable"}
	}
	refs, err := deps.Prepare()
	if err != nil || !omb3255RuntimeRefsValid(refs) {
		return omb3255RuntimeAcquisition{state: "normal_binding_unavailable"}
	}
	unavailable := make([]omb3255RuntimeRecord, len(refs))
	for i, ref := range refs {
		unavailable[i] = omb3255RuntimeRecord{ref.AssignmentID, ref.StartedAt, ref.FinishedAt, omb3255ContextUnavailable()}
	}
	stream, err := deps.Open()
	if stream == nil {
		return omb3255RuntimeAcquisition{unavailable, "source_open_unavailable"}
	}
	if err != nil || !stream.Unchanged() {
		_ = stream.Close()
		return omb3255RuntimeAcquisition{unavailable, "source_identity_unavailable"}
	}
	data, readErr := io.ReadAll(io.LimitReader(stream, (64<<20)+1))
	observations, valid, state := omb3255NativeMetadataState(data, refs)
	stable := stream.Unchanged() && deps.Recheck(refs)
	closeErr := stream.Close()
	if readErr != nil {
		return omb3255RuntimeAcquisition{unavailable, "source_read_unavailable"}
	}
	if !valid {
		return omb3255RuntimeAcquisition{unavailable, state}
	}
	if !stable {
		return omb3255RuntimeAcquisition{unavailable, "source_recheck_unavailable"}
	}
	if closeErr != nil {
		return omb3255RuntimeAcquisition{unavailable, "source_close_unavailable"}
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
	return omb3255RuntimeAcquisition{unavailable, state}
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
	result, valid, _ := omb3255NativeMetadataState(data, refs)
	return result, valid
}

func omb3255NativeMetadataState(data []byte, refs []omb3255RuntimeRef) ([]omb3255ContextObservation, bool, string) {
	fail := func(state string) ([]omb3255ContextObservation, bool, string) { return nil, false, state }
	if !omb3255RuntimeRefsValid(refs) {
		return fail("normal_binding_unavailable")
	}
	if len(data) == 0 || len(data) > 64<<20 {
		return fail("native_size_unavailable")
	}
	if data[len(data)-1] != '\n' || !utf8.Valid(data) {
		return fail("native_encoding_unavailable")
	}
	var header []byte
	selected := make(map[string][]byte)
	selectedFields := make(map[string]map[string]json.RawMessage)
	wanted := make(map[string]bool)
	for _, ref := range refs {
		wanted[ref.TurnID] = true
	}
	for len(data) != 0 {
		end := bytes.IndexByte(data, '\n')
		if end < 0 || end > 4<<20 {
			return fail("native_line_unavailable")
		}
		line := bytes.Trim(data[:end], " \t\r")
		data = data[end+1:]
		if len(line) == 0 {
			continue
		}
		if !omb3255UniqueContextJSON(line) {
			return fail("native_json_unavailable")
		}
		var row map[string]json.RawMessage
		if json.Unmarshal(line, &row) != nil || row == nil {
			return fail("native_json_unavailable")
		}
		var kind string
		if json.Unmarshal(row["type"], &kind) != nil || kind == "" {
			return fail("native_row_unavailable")
		}
		if kind != "session_meta" && kind != "turn_context" {
			continue
		}
		var payload map[string]json.RawMessage
		if json.Unmarshal(row["payload"], &payload) != nil || payload == nil {
			return fail("native_payload_unavailable")
		}
		if kind == "session_meta" {
			var id string
			if json.Unmarshal(payload["id"], &id) != nil || id != refs[0].ThreadID {
				return fail("native_identity_unavailable")
			}
			header, _ = json.Marshal(map[string]any{"type": kind, "payload": map[string]json.RawMessage{"id": payload["id"]}})
			continue
		}
		var turn string
		if raw, exists := payload["turn_id"]; exists && json.Unmarshal(raw, &turn) != nil {
			return fail("native_turn_unavailable")
		}
		if !wanted[turn] {
			continue
		}
		if header == nil {
			return fail("native_header_unavailable")
		}
		fields := map[string]json.RawMessage{"turn_id": payload["turn_id"]}
		for _, key := range []string{"model", "effort"} {
			if raw, exists := payload[key]; exists {
				fields[key] = raw
			}
		}
		if previous := selectedFields[turn]; previous != nil {
			if bytes.Equal(previous["model"], fields["model"]) && bytes.Equal(previous["effort"], fields["effort"]) {
				return fail("native_context_repeated")
			}
			return fail("native_context_conflicting")
		}
		selectedFields[turn] = fields
		selected[turn], _ = json.Marshal(map[string]any{"type": kind, "timestamp": row["timestamp"], "payload": fields})
	}
	if header == nil {
		return fail("native_header_unavailable")
	}
	results := make([]omb3255ContextObservation, len(refs))
	state := "context_unavailable"
	for i, ref := range refs {
		metadata := append(append(append([]byte{}, header...), '\n'), selected[ref.TurnID]...)
		metadata = append(metadata, '\n')
		results[i] = omb3255ProjectContext(omb3255ContextScope{ref.ThreadID, ref.TurnID, time.UnixMilli(ref.StartedAt), time.UnixMilli(ref.FinishedAt)}, bytes.NewReader(metadata))
		if results[i].ContextAt != nil {
			state = "observed"
		}
	}
	return results, true, state
}
