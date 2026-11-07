package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type skillsRecordedTransport struct {
	Input            skillsTransportInput
	TraceID          string
	InferenceID      string
	TurnID           string
	PayloadSHA       string
	PayloadID        skillsObjectID
	RecordedAt       time.Time
	EventSequence    uint64
	EventPrefixBytes int64
}

func skillsSourceDirectoryID(s *skillsFS, path string) (skillsObjectID, bool) {
	h, ok := s.directory(path)
	if !ok {
		return skillsObjectID{}, false
	}
	return skillsHandleID(h)
}

// Only the controller-selected, initially empty trace root reaches this
// reader. Public callers cannot choose a bundle or a payload pathname.
func skillsReadTransportTraceAt(s *skillsFS, root string, expected skillsObjectID, job object, prompt []byte, start, end time.Time) (skillsRecordedTransport, string) {
	var empty skillsRecordedTransport
	if !skillsFreshRuntimeThread(job, start, end) || job["sandbox"] != "read-only" {
		return empty, "source_generation"
	}
	id, ok := skillsSourceDirectoryID(s, root)
	if !ok || id != expected || expected == (skillsObjectID{}) {
		return empty, "source_directory"
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || !entries[0].IsDir() || entries[0].Type()&os.ModeSymlink != 0 {
		return empty, "source_bundle"
	}
	bundle := filepath.Join(root, entries[0].Name())
	if _, ok := s.directory(bundle); !ok {
		return empty, "source_bundle"
	}
	var manifest struct {
		Schema   uint64 `json:"schema_version"`
		Trace    string `json:"trace_id"`
		Rollout  string `json:"rollout_id"`
		Thread   string `json:"root_thread_id"`
		Started  int64  `json:"started_at_unix_ms"`
		Events   string `json:"raw_event_log"`
		Payloads string `json:"payloads_dir"`
	}
	raw, ok := s.read(filepath.Join(bundle, "manifest.json"), 64<<10)
	if !ok || !skillsStrict(raw, &manifest) || manifest.Schema != 2 || !skillsUUID.MatchString(manifest.Trace) || manifest.Rollout == "" || manifest.Thread != str(job["thread_id"]) || manifest.Started < start.UnixMilli() || manifest.Started > end.UnixMilli() || manifest.Events != "trace.jsonl" || manifest.Payloads != "payloads" || entries[0].Name() != "trace-"+manifest.Trace+"-"+manifest.Thread {
		return empty, "source_manifest"
	}
	stream, ok := skillsSourceEventSnapshot(s, filepath.Join(bundle, manifest.Events), job["status"] == "running")
	if !ok {
		return empty, "source_events"
	}
	reference, trace, stage := skillsFirstRecordedRequest(stream, manifest.Trace, manifest.Rollout, manifest.Thread, str(job["model"]), start, end)
	if stage != "" {
		return empty, stage
	}
	// Select the kind and the complete bounded relative name before opening.
	// Response, tool, reducer and agent-result payloads are never candidates.
	if reference.Kind.Type != "inference_request" || !strings.HasPrefix(reference.Path, "payloads/") || filepath.ToSlash(filepath.Dir(reference.Path)) != "payloads" || !strings.HasSuffix(reference.Path, ".json") {
		return empty, "source_payload_reference"
	}
	n := strings.TrimSuffix(strings.TrimPrefix(reference.Path, "payloads/"), ".json")
	ordinal, err := strconv.ParseUint(n, 10, 32)
	if err != nil || ordinal == 0 || strconv.FormatUint(ordinal, 10) != n || reference.ID != "raw_payload:"+n {
		return empty, "source_payload_reference"
	}
	payload, ok := s.open(filepath.Join(bundle, "payloads", n+".json"), 8<<20)
	if !ok {
		return empty, "source_payload"
	}
	trace.PayloadID, ok = skillsHandleID(syscall.Handle(payload.Fd()))
	if !ok {
		return empty, "source_payload"
	}
	body, err := io.ReadAll(io.LimitReader(payload, (8<<20)+1))
	if err != nil || len(body) > 8<<20 {
		return empty, "source_payload"
	}
	trace.Input, ok = skillsTransportInputProjection(body, prompt, str(job["model"]), str(job["effort"]))
	if !ok {
		return empty, "source_input"
	}
	trace.PayloadSHA = hashBytes(body)
	if !stream.unchanged(trace.EventPrefixBytes) {
		return empty, "source_events"
	}
	return trace, ""
}

type skillsTracePayloadReference struct {
	ID   string `json:"raw_payload_id"`
	Kind struct {
		Type string `json:"type"`
	} `json:"kind"`
	Path string `json:"path"`
}

func skillsFirstRecordedRequest(input io.Reader, traceID, rollout, thread, model string, start, end time.Time) (skillsTracePayloadReference, skillsRecordedTransport, string) {
	var empty skillsTracePayloadReference
	out := skillsRecordedTransport{TraceID: traceID}
	scan := bufio.NewScanner(input)
	scan.Buffer(make([]byte, 64<<10), 1<<20)
	scan.Split(skillsTraceCompleteLine)
	var prefixBytes int64
	seq := uint64(1)
	root, child, configured, started, turn := false, false, false, false, ""
	last := start.UnixMilli()
	for scan.Scan() {
		prefixBytes += int64(len(scan.Bytes()))
		var e struct {
			Schema  uint64          `json:"schema_version"`
			Seq     uint64          `json:"seq"`
			At      int64           `json:"wall_time_unix_ms"`
			Rollout string          `json:"rollout_id"`
			Thread  *string         `json:"thread_id"`
			Turn    *string         `json:"codex_turn_id"`
			Payload json.RawMessage `json:"payload"`
		}
		if !skillsNativeUniqueJSON(scan.Bytes()) || !skillsStrict(scan.Bytes(), &e) || e.Schema != 2 || e.Seq != seq || e.Rollout != rollout || e.At < last || e.At > end.UnixMilli() || (e.Thread != nil && *e.Thread != thread) || (e.Turn != nil && turn != "" && *e.Turn != turn) {
			return empty, out, "source_event_binding"
		}
		seq++
		last = e.At
		var kind struct {
			Type string `json:"type"`
		}
		if !skillsCreationHeaderKeys(e.Payload, "type") || json.Unmarshal(e.Payload, &kind) != nil {
			return empty, out, "source_event_binding"
		}
		switch kind.Type {
		case "rollout_started":
			var p struct {
				Type   string `json:"type"`
				Trace  string `json:"trace_id"`
				Thread string `json:"root_thread_id"`
			}
			if root || seq != 2 || !skillsStrict(e.Payload, &p) || p.Trace != traceID || p.Thread != thread {
				return empty, out, "source_event_binding"
			}
			root = true
		case "thread_started":
			var p struct {
				Type     string          `json:"type"`
				Thread   string          `json:"thread_id"`
				Agent    string          `json:"agent_path"`
				Metadata json.RawMessage `json:"metadata_payload"`
			}
			if !root || child || !skillsStrict(e.Payload, &p) || p.Thread != thread {
				return empty, out, "source_event_binding"
			}
			child = true
		case "codex_turn_started":
			var p struct {
				Type   string `json:"type"`
				Thread string `json:"thread_id"`
				Turn   string `json:"codex_turn_id"`
			}
			if !child || !configured || turn != "" || !skillsStrict(e.Payload, &p) || p.Thread != thread || !skillsUUID.MatchString(p.Turn) || (e.Turn != nil && *e.Turn != p.Turn) {
				return empty, out, "source_event_binding"
			}
			turn = p.Turn
		case "protocol_event_observed":
			var p struct {
				Type    string          `json:"type"`
				Event   string          `json:"event_type"`
				Payload json.RawMessage `json:"event_payload"`
			}
			if !child || !skillsStrict(e.Payload, &p) || (p.Event != "session_configured" && p.Event != "turn_started") {
				return empty, out, "source_event_binding"
			}
			if p.Event == "session_configured" {
				if configured || turn != "" {
					return empty, out, "source_event_binding"
				}
				configured = true
			} else {
				if !configured || turn == "" || started {
					return empty, out, "source_event_binding"
				}
				started = true
			}
		case "history_item_accepted":
			var p struct {
				Type string `json:"type"`
				ID   string `json:"item_id"`
			}
			if !started || !skillsStrict(e.Payload, &p) || p.ID == "" {
				return empty, out, "source_event_binding"
			}
		case "inference_started":
			var p struct {
				Type      string                      `json:"type"`
				Inference string                      `json:"inference_call_id"`
				Thread    string                      `json:"thread_id"`
				Turn      string                      `json:"codex_turn_id"`
				Model     string                      `json:"model"`
				Provider  string                      `json:"provider_name"`
				Request   skillsTracePayloadReference `json:"request_payload"`
			}
			if !started || e.Thread == nil || *e.Thread != thread || e.Turn == nil || *e.Turn != turn || !skillsStrict(e.Payload, &p) || p.Thread != thread || p.Turn != turn || !skillsTransportModelMatches(p.Model, model) || p.Inference == "" || p.Provider != "OpenAI" || p.Request.Kind.Type != "inference_request" {
				return empty, out, "source_event_binding"
			}
			out.InferenceID, out.TurnID, out.RecordedAt, out.EventSequence = p.Inference, p.Turn, time.UnixMilli(e.At), e.Seq
			out.EventPrefixBytes = prefixBytes
			return p.Request, out, ""
		default:
			return empty, out, "source_event_binding"
		}
	}
	if scan.Err() != nil {
		return empty, out, "source_events"
	}
	return empty, out, "source_pending"
}

func skillsTraceCompleteLine(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i+1], nil
	}
	if atEOF && len(data) != 0 {
		return 0, nil, io.ErrUnexpectedEOF
	}
	return 0, nil, nil
}

type skillsSourceEventReader struct {
	file     *os.File
	snapshot skillsNativeSnapshotReader
	observed bytes.Buffer
}

func (r *skillsSourceEventReader) Read(p []byte) (int, error) {
	n, err := r.snapshot.Read(p)
	r.observed.Write(p[:n])
	return n, err
}

func (r *skillsSourceEventReader) unchanged(prefix int64) bool {
	info, err := r.file.Stat()
	if err != nil || info.Size() < r.snapshot.length || prefix < 1 || prefix > int64(r.observed.Len()) {
		return false
	}
	before := r.observed.Bytes()[:prefix]
	after := make([]byte, prefix)
	n, err := r.file.ReadAt(after, 0)
	return err == nil && n == len(after) && bytes.Equal(before, after)
}

var skillsTraceLockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")

func skillsSourceEventSnapshot(s *skillsFS, path string, live bool) (*skillsSourceEventReader, bool) {
	ph, ok := s.directory(filepath.Dir(path))
	if !ok || s.blocked(path) {
		return nil, false
	}
	// Completed records deny write sharing. A live runtime may keep its append
	// handle open, so lock the observed byte range against in-place writes while
	// allowing appends after that range. Keep the handle/lock until fs.close.
	h, ok := skillsOpenAtSharing(ph, filepath.Base(path), false, false, live)
	if !ok {
		return nil, false
	}
	f := os.NewFile(uintptr(h), path)
	s.files = append(s.files, f)
	info, err := f.Stat()
	actual, err2 := ownerHandlePath(h)
	if err != nil || err2 != nil || !same(path, actual) || info.Size() < 1 || info.Size() > 16<<20 {
		return nil, false
	}
	if live {
		var overlap syscall.Overlapped
		locked, _, _ := skillsTraceLockFileEx.Call(uintptr(h), 1, 0, uintptr(info.Size()), 0, uintptr(unsafe.Pointer(&overlap)))
		if locked == 0 {
			return nil, false
		}
	}
	return &skillsSourceEventReader{file: f, snapshot: skillsNativeSnapshotReader{reader: f, remaining: info.Size(), length: info.Size()}}, true
}
