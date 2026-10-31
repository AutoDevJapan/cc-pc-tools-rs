package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func skillsTraceFixture(t *testing.T) (string, object, []byte, time.Time, time.Time, object, []object) {
	t.Helper()
	_, job, _, start, end := skillsSourcePrefixFixture()
	request, prompt := skillsTransportRequestFixture()
	root := filepath.Join(skillsFixtureRoot(t), "controller-source")
	trace := "33333333-3333-4333-8333-333333333333"
	thread, turn := str(job["thread_id"]), "44444444-4444-7444-8444-444444444444"
	bundle := filepath.Join(root, "trace-"+trace+"-"+thread)
	if os.MkdirAll(filepath.Join(bundle, "payloads"), 0700) != nil {
		t.Fatal("controlled source fixture")
	}
	manifest := object{"schema_version": 2, "trace_id": trace, "rollout_id": thread, "root_thread_id": thread, "started_at_unix_ms": start.UnixMilli() + 1, "raw_event_log": "trace.jsonl", "payloads_dir": "payloads"}
	ref := func(kind, ordinal string) object {
		return object{"raw_payload_id": "raw_payload:" + ordinal, "kind": object{"type": kind}, "path": "payloads/" + ordinal + ".json"}
	}
	payloads := []object{
		{"type": "rollout_started", "trace_id": trace, "root_thread_id": thread},
		{"type": "thread_started", "thread_id": thread, "agent_path": "/root", "metadata_payload": ref("session_metadata", "99")},
		{"type": "protocol_event_observed", "event_type": "session_configured", "event_payload": ref("protocol_event", "100")},
		{"type": "codex_turn_started", "thread_id": thread, "codex_turn_id": turn},
		{"type": "protocol_event_observed", "event_type": "turn_started", "event_payload": ref("protocol_event", "101")},
		{"type": "history_item_accepted", "item_id": "msg_synthetic"},
		{"type": "inference_started", "inference_call_id": "inference:1", "thread_id": thread, "codex_turn_id": turn, "model": "gpt-6-luna", "provider_name": "synthetic-provider", "request_payload": ref("inference_request", "2")},
	}
	events := []object{}
	for i, p := range payloads {
		e := object{"schema_version": 2, "seq": i + 1, "wall_time_unix_ms": start.UnixMilli() + int64(i+1), "rollout_id": thread, "thread_id": nil, "codex_turn_id": nil, "payload": p}
		if i > 0 {
			e["thread_id"] = thread
		}
		if i >= 3 {
			e["codex_turn_id"] = turn
		}
		events = append(events, e)
	}
	b, _ := json.Marshal(request)
	if os.WriteFile(filepath.Join(bundle, "payloads", "2.json"), b, 0600) != nil {
		t.Fatal("controlled input payload")
	}
	return root, job, prompt, start, end, manifest, events
}

func TestSkills3080TraceReaderSelectsOnlyBoundRecordedInput(t *testing.T) {
	for _, kind := range []string{"normal", "response_tail", "wrong_directory", "multiple_bundles", "wrong_schema", "wrong_manifest_thread", "wrong_manifest_layout", "manifest_before_start", "event_sequence_gap", "event_time_outside", "wrong_event_thread", "wrong_turn", "missing_turn", "duplicate_config", "missing_config", "response_kind", "path_escape", "wrong_payload_id", "leading_zero_payload", "wrong_model", "wrong_task", "unknown_event", "short_event", "payload_short", "external_hardlink", "payload_writer"} {
		t.Run(kind, func(t *testing.T) {
			root, job, prompt, start, end, manifest, events := skillsTraceFixture(t)
			bundle := filepath.Join(root, "trace-"+str(manifest["trace_id"])+"-"+str(job["thread_id"]))
			last := obj(events[len(events)-1]["payload"])
			switch kind {
			case "multiple_bundles":
				if os.Mkdir(filepath.Join(root, "unexpected-bundle"), 0700) != nil {
					t.Fatal("fixture directory")
				}
			case "wrong_schema":
				manifest["schema_version"] = 1
			case "wrong_manifest_thread":
				manifest["root_thread_id"] = "different-thread"
			case "wrong_manifest_layout":
				manifest["raw_event_log"] = "../different.jsonl"
			case "manifest_before_start":
				manifest["started_at_unix_ms"] = start.UnixMilli() - 1
			case "event_sequence_gap":
				events[len(events)-1]["seq"] = 100
			case "event_time_outside":
				events[len(events)-1]["wall_time_unix_ms"] = end.UnixMilli() + 1
			case "wrong_event_thread":
				events[len(events)-1]["thread_id"] = "different-thread"
			case "wrong_turn":
				last["codex_turn_id"] = "different-turn"
			case "missing_turn":
				events[len(events)-1]["codex_turn_id"] = nil
			case "duplicate_config":
				obj(events[4]["payload"])["event_type"] = "session_configured"
			case "missing_config":
				obj(events[2]["payload"])["event_type"] = "turn_started"
			case "response_kind":
				obj(obj(last["request_payload"])["kind"])["type"] = "inference_response"
			case "path_escape":
				obj(last["request_payload"])["path"] = "../last_message.md"
			case "wrong_payload_id":
				obj(last["request_payload"])["raw_payload_id"] = "raw_payload:3"
			case "leading_zero_payload":
				obj(last["request_payload"])["path"] = "payloads/02.json"
				obj(last["request_payload"])["raw_payload_id"] = "raw_payload:02"
			case "wrong_model":
				last["model"] = "gpt-6.1-sol"
			case "wrong_task":
				prompt = []byte("different registered task")
			case "unknown_event":
				obj(events[5]["payload"])["type"] = "unknown_source"
			case "payload_short":
				if os.WriteFile(filepath.Join(bundle, "payloads", "2.json"), []byte(`{"model":`), 0600) != nil {
					t.Fatal("short fixture")
				}
			case "external_hardlink":
				from, to := filepath.Join(bundle, "payloads", "2.json"), filepath.Join(filepath.Dir(root), "controlled-hardlink-target")
				if os.Rename(from, to) != nil || os.Link(to, from) != nil {
					t.Fatal("controlled hardlink fixture")
				}
			}
			b, _ := json.Marshal(manifest)
			if os.WriteFile(filepath.Join(bundle, "manifest.json"), b, 0600) != nil {
				t.Fatal("fixture manifest")
			}
			var spine bytes.Buffer
			for _, e := range events {
				json.NewEncoder(&spine).Encode(e)
			}
			if kind == "response_tail" {
				// Deliberately malformed future data cannot become a payload
				// candidate while reading the already complete first source.
				spine.WriteString("not a response or a final to recover\n")
			}
			data := spine.Bytes()
			if kind == "short_event" {
				data = data[:len(data)-10]
			}
			if os.WriteFile(filepath.Join(bundle, "trace.jsonl"), data, 0600) != nil {
				t.Fatal("fixture event spine")
			}
			if kind == "payload_writer" {
				name, _ := syscall.UTF16PtrFromString(filepath.Join(bundle, "payloads", "2.json"))
				h, err := syscall.CreateFile(name, syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0, 0)
				if err != nil {
					t.Fatal("controlled writer")
				}
				defer syscall.CloseHandle(h)
			}
			payloadAttempts, otherPayloads := 0, 0
			fs := skillsFS{blocked: func(path string) bool {
				if strings.EqualFold(filepath.Dir(path), filepath.Join(bundle, "payloads")) && strings.HasSuffix(path, ".json") {
					payloadAttempts++
					if filepath.Base(path) != "2.json" {
						otherPayloads++
					}
				}
				return false
			}}
			defer fs.close()
			expected, ok := skillsSourceDirectoryID(&fs, root)
			if !ok {
				t.Fatal("held fixture source directory")
			}
			if kind == "wrong_directory" {
				expected.File = strings.Repeat("f", 32)
			}
			_, stage := skillsReadTransportTraceAt(&fs, root, expected, job, prompt, start, end)
			want := kind == "normal" || kind == "response_tail"
			if (stage == "") != want || otherPayloads != 0 {
				t.Fatalf("trace %s accepted=%v payload attempts=%d unrelated=%d stage=%s", kind, stage == "", payloadAttempts, otherPayloads, stage)
			}
			if want && payloadAttempts != 1 {
				t.Fatal("positive control did not exercise the payload reader exactly once")
			}
			if (kind == "response_kind" || kind == "path_escape" || kind == "wrong_payload_id" || kind == "leading_zero_payload") && payloadAttempts != 0 {
				t.Fatal("unqualified reference reached a payload")
			}
			if (kind == "external_hardlink" || kind == "payload_writer") && (stage != "source_payload" || payloadAttempts != 1) {
				t.Fatal("controlled leaf denial did not reach the actual payload open")
			}
		})
	}
}
