package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func skillsSourcePrefixFixture() ([]byte, object, []byte, time.Time, time.Time) {
	start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	id := fmt.Sprintf("%08x-%04x-7000-8000-000000000001", uint64(start.UnixMilli())>>16, uint64(start.UnixMilli())&0xffff)
	cwd, turn := "D:/CC/Work/synthetic", "22222222-2222-7222-8222-222222222222"
	job := object{"thread_id": id, "started_at": start.Format(time.RFC3339Nano), "cwd": cwd, "sandbox": "read-only", "model": "gpt-6-luna", "effort": "max"}
	project := []byte("PUBLIC project")
	body, _ := skillsAssembleBootstrapSource([]byte("registered policy at launch"), project)
	records := []object{
		{"type": "session_meta", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"id": id, "cwd": cwd, "source": "exec", "originator": "codex_exec", "cli_version": "0.165.0"}},
		{"type": "event_msg", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"type": "task_started", "turn_id": turn}},
		{"type": "response_item", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": skillsBootstrapFixture(cwd, string(body), true)}}}},
		{"type": "world_state", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"full": true, "state": object{"agents_md": object{"directory": cwd, "text": string(body)}}}},
		{"type": "turn_context", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"turn_id": turn, "cwd": cwd, "model": "gpt-6-luna", "effort": "max"}},
	}
	var raw bytes.Buffer
	for _, r := range skillsCompleteRuntimeFixture(records) {
		json.NewEncoder(&raw).Encode(r)
	}
	return raw.Bytes(), job, project, start, start.Add(time.Minute)
}

func TestSkills3080SourcePrefixEndsAtCompleteInitialContext(t *testing.T) {
	for _, newline := range []string{"\n", "\r\n"} {
		t.Run(fmt.Sprintf("newline_%d", len(newline)), func(t *testing.T) {
			raw, job, project, start, end := skillsSourcePrefixFixture()
			raw = []byte(strings.ReplaceAll(string(raw), "\n", newline))
			// The tail must never be included in the recorded source digest.
			stream := append(append([]byte{}, raw...), []byte("not a source or a result to recover\n")...)
			p, ok := skillsInitialSourcePrefixProjection(bytes.NewReader(stream), job, project, start, end)
			if !ok || p.Bytes != int64(len(raw)) || p.SHA != hashBytes(raw) {
				t.Fatal("recorded digest did not end at the exact source newline")
			}
		})
	}
}

func TestSkills3080SourcePrefixRequiresFreshCompleteGeneration(t *testing.T) {
	for _, kind := range []string{"fresh", "resumed", "old_thread", "future_thread", "version4", "wrong_variant", "wrong_sandbox", "unfinished_line", "truncated", "legacy", "wrong_project"} {
		t.Run(kind, func(t *testing.T) {
			raw, job, project, start, end := skillsSourcePrefixFixture()
			switch kind {
			case "resumed":
				job["resume_session"] = job["thread_id"]
			case "old_thread":
				start = start.Add(time.Second)
			case "future_thread":
				end = start.Add(-time.Millisecond)
			case "version4":
				job["thread_id"] = strings.Replace(str(job["thread_id"]), "-7000-", "-4000-", 1)
			case "wrong_variant":
				job["thread_id"] = strings.Replace(str(job["thread_id"]), "-8000-", "-0000-", 1)
			case "wrong_sandbox":
				job["sandbox"] = "workspace-write"
			case "unfinished_line":
				raw = raw[:len(raw)-1]
			case "truncated":
				raw = raw[:len(raw)-8]
			case "legacy":
				raw = bytes.Replace(raw, []byte("0.165.0"), []byte("0.163.0"), 1)
			case "wrong_project":
				project = []byte("different PUBLIC project")
			}
			_, ok := skillsInitialSourcePrefixProjection(bytes.NewReader(raw), job, project, start, end)
			if ok != (kind == "fresh") {
				t.Fatalf("source generation %s accepted=%v", kind, ok)
			}
		})
	}
}
