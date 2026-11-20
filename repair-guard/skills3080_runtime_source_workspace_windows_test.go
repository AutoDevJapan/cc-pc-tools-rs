package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A Windows runtime may report an equivalent path with another spelling.
// The controller call must still match the exact independently returned
// declaration, including both its command and workdir fields.
func TestSkills3080SourcePreparationRetainsDeclaredWorkspaceSpelling(t *testing.T) {
	for _, kind := range []string{"same", "runtime_backslash", "runtime_slash", "runtime_case", "other_runtime", "claim_changed", "call_workdir_alias", "call_command_alias"} {
		t.Run(kind, func(t *testing.T) {
			p := skillsSourcePreparationFixture()
			p.Writer.Workspace = filepath.ToSlash(p.Writer.Workspace)
			runtimeCwd := p.Writer.Workspace
			switch kind {
			case "runtime_backslash":
				runtimeCwd = strings.ReplaceAll(runtimeCwd, "/", `\`)
			case "runtime_slash":
				p.Writer.Workspace = strings.ReplaceAll(p.Writer.Workspace, "/", `\`)
			case "runtime_case":
				runtimeCwd = strings.ToUpper(runtimeCwd)
			case "other_runtime":
				runtimeCwd = filepath.Join(runtimeCwd, "different")
			}
			command := skillsRuntimeSourceCommand(p.RequestPath, p.Writer.Workspace, "--prepare-skills3080-runtime-source")
			switch kind {
			case "claim_changed":
				p.Writer.Workspace = strings.ReplaceAll(p.Writer.Workspace, "/", `\`)
			case "call_workdir_alias":
				command.Workdir = strings.ReplaceAll(command.Workdir, "/", `\`)
			case "call_command_alias":
				command.Cmd = strings.ReplaceAll(command.Cmd, p.Writer.Workspace, strings.ToUpper(p.Writer.Workspace))
			}
			encodedCommand, _ := json.Marshal(command)
			result := object{"qualified": true, "operation": "prepare_runtime_source", "request_sha256": p.RequestSHA, "general_access_granted": false, "original_registry_recovered": false, "data_access_granted": false, "runtime_source_preparation": p}
			body, _ := json.Marshal(result)
			wrapper, _ := json.Marshal(object{"exit_code": 0, "output": string(body)})
			start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			thread := "11111111-1111-7111-8111-111111111111"
			records := []object{
				{"type": "session_meta", "payload": object{"id": thread, "cwd": runtimeCwd}},
				{"type": "response_item", "timestamp": start.Add(time.Second).Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call", "name": "exec", "call_id": p.CallID, "input": "text(await tools.exec_command(" + string(encodedCommand) + "));"}},
				{"type": "response_item", "timestamp": start.Add(3 * time.Second).Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call_output", "call_id": p.CallID, "output": []object{{"type": "input_text", "text": "Script completed\nWall time 0.5 seconds\nOutput:\n"}, {"type": "input_text", "text": string(wrapper)}}}},
				{"type": "response_item", "timestamp": start.Add(20 * time.Second).Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "assistant", "content": []object{{"type": "output_text", "text": "independent later work"}}}},
			}
			var input bytes.Buffer
			for _, record := range records {
				if err := json.NewEncoder(&input).Encode(record); err != nil {
					t.Fatal(err)
				}
			}
			got, ok := skillsRuntimeSourceNativeProjection(&input, thread, runtimeCwd, p.Writer.Job, p.Nonce, p.Task, p.SpecSHA, start, start.Add(time.Minute), start.Add(10*time.Second))
			want := kind == "same" || kind == "runtime_backslash" || kind == "runtime_slash" || kind == "runtime_case"
			if ok != want || (ok && got != p) {
				t.Fatalf("declared workspace proof: accepted=%v want=%v", ok, want)
			}
		})
	}
}
