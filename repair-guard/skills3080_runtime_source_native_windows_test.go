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

func skillsSourcePreparationFixture() skillsRuntimeSourcePreparation {
	root := filepath.Join(skillsParentRoot, "codex-owned-"+strings.Repeat("a1", 16))
	return skillsRuntimeSourcePreparation{
		Domain: skillsRuntimeSourceDomain, Writer: skillsWriter{Task: 3080, Job: "20300101-000000-todo-3080", SpecSHA: strings.Repeat("b", 64), Workspace: skillsParentRoot},
		WriterSpec: currentTodoSpecsRoot + "/3080-11111111111111111111111111111111.md", Task: 6291, PreviousJob: "20300101-000000-todo-6291",
		TemplateSHA: strings.Repeat("c", 64), SpecSHA: strings.Repeat("d", 64), PreparedSpec: filepath.Join(root, "scope.json"), Nonce: strings.Repeat("e1", 16), OwnershipID: strings.Repeat("a1", 16),
		Root: root, RootID: skillsObjectID{"123", strings.Repeat("1", 32)}, TraceRoot: filepath.Join(root, "trace"), TraceID: skillsObjectID{"123", strings.Repeat("2", 32)},
		PreparedAt: "2030-01-01T00:00:02Z", RequestPath: filepath.Join(skillsParentRoot, ".ci-work", "source-request.json"), RequestID: skillsObjectID{"123", strings.Repeat("3", 32)}, RequestSHA: strings.Repeat("f", 64), CallID: "call_source_preparation",
		Producer: skillsProducer{strings.Repeat("a", 64), strings.Repeat("b", 40)}, SourceMeaning: "first_recorded_model_visible_request_object",
	}
}

func TestSkills3080SourceInvocationRejectsUnsafePathsBeforeRendering(t *testing.T) {

	for _, kind := range []string{"normal", "quote", "newline", "carriage_return", "relative", "outside", "cwd_quote", "cwd_newline", "unknown_flag"} {
		t.Run(kind, func(t *testing.T) {
			p := skillsSourcePreparationFixture()
			path, cwd, flag := p.RequestPath, p.Writer.Workspace, "--prepare-skills3080-runtime-source"
			switch kind {
			case "quote":
				path = filepath.Join(cwd, "a'; Write-Output 'injected.json")
			case "newline":
				path += "\n.json"
			case "carriage_return":
				path += "\r.json"
			case "relative":
				path = "relative.json"
			case "outside":
				path = "D:/different/request.json"
			case "cwd_quote":
				cwd += "'"
			case "cwd_newline":
				cwd += "\n"
			case "unknown_flag":
				flag = "--unknown"
			}
			invocation := skillsRuntimeSourceInvocationText(path, cwd, flag)
			if (invocation != "") != (kind == "normal") || skillsRuntimeSourceInvocation(invocation, path, cwd, flag) != (kind == "normal") {
				t.Fatal("unsafe source path reached a runnable invocation")
			}
		})
	}
}

func TestSkills3080SourcePreparationRequiresOriginalControllerToolResult(t *testing.T) {
	for _, kind := range []string{"normal", "message_copy", "without_call", "wrong_flag", "wrong_path", "no_window_false", "wrong_parent", "wrong_task", "wrong_spec", "wrong_nonce", "wrong_request_hash", "before_call", "after_generation", "duplicate_output", "duplicate_call", "yielded", "extra_block", "block_alias", "receipt_alias", "bad_object", "extra_effect"} {
		t.Run(kind, func(t *testing.T) {
			p := skillsSourcePreparationFixture()
			expected := p
			start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			callAt, returnedAt := start.Add(time.Second), start.Add(3*time.Second)
			generation := start.Add(10 * time.Second)
			thread := "11111111-1111-7111-8111-111111111111"
			command := skillsRuntimeSourceCommand(p.RequestPath, p.Writer.Workspace, "--prepare-skills3080-runtime-source")
			switch kind {
			case "wrong_flag":
				command.Cmd = strings.Replace(command.Cmd, "--prepare-skills3080-runtime-source", "--start-skills3080-runtime-source", 1)
			case "wrong_path":
				command.Cmd = strings.ReplaceAll(command.Cmd, "source-request.json", "different-request.json")
			case "no_window_false":
				command.Cmd = strings.Replace(command.Cmd, "$psi.CreateNoWindow=$true", "$psi.CreateNoWindow=$false", 1)
				if command.Cmd == skillsRuntimeSourceCommand(p.RequestPath, p.Writer.Workspace, "--prepare-skills3080-runtime-source").Cmd {
					command.Cmd = strings.Replace(command.Cmd, "CreateNoWindow = $true", "CreateNoWindow = $false", 1)
				}
			case "wrong_parent":
				p.Writer.Job = "20300101-000001-todo-3080"
			case "wrong_task":
				p.Task = 6292
				p.PreviousJob = "20300101-000000-todo-6292"
			case "wrong_spec":
				p.SpecSHA = strings.Repeat("c", 64)
			case "wrong_nonce":
				p.Nonce = strings.Repeat("f1", 16)
			case "before_call":
				p.PreparedAt = start.Format(time.RFC3339Nano)
			case "after_generation":
				returnedAt = generation.Add(time.Second)
			case "bad_object":
				p.TraceID.File = "invalid"
			case "extra_effect":
				command.Cmd += "; Write-Output 'extra'"
			}
			c, _ := json.Marshal(command)
			invocation := "text(await tools.exec_command(" + string(c) + "));"
			result := object{"qualified": true, "operation": "prepare_runtime_source", "request_sha256": p.RequestSHA, "general_access_granted": false, "original_registry_recovered": false, "data_access_granted": false, "runtime_source_preparation": p}
			if kind == "wrong_request_hash" {
				result["request_sha256"] = strings.Repeat("a", 64)
			}
			if kind == "receipt_alias" {
				b, _ := json.Marshal(p)
				var v object
				json.Unmarshal(b, &v)
				v["Nonce"] = v["nonce"]
				delete(v, "nonce")
				result["runtime_source_preparation"] = v
			}
			body, _ := json.Marshal(result)
			wrapper := object{"exit_code": 0, "output": string(body)}
			if kind == "yielded" {
				wrapper["session_id"] = 123
			}
			w, _ := json.Marshal(wrapper)
			blocks := []object{{"type": "input_text", "text": "Script completed\nWall time 0.5 seconds\nOutput:\n"}, {"type": "input_text", "text": string(w)}}
			if kind == "extra_block" {
				blocks = append(blocks, blocks[1])
			}
			if kind == "block_alias" {
				blocks[1]["Text"] = string(w)
			}
			meta := object{"type": "session_meta", "payload": object{"id": thread, "cwd": p.Writer.Workspace}}
			call := object{"type": "response_item", "timestamp": callAt.Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call", "name": "exec", "call_id": p.CallID, "input": invocation}}
			response := object{"type": "response_item", "timestamp": returnedAt.Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call_output", "call_id": p.CallID, "output": blocks}}
			records := []object{meta, call, response}
			if kind == "message_copy" {
				records[2] = object{"type": "response_item", "timestamp": returnedAt.Format(time.RFC3339Nano), "payload": object{"type": "message", "role": "user", "content": []object{{"type": "input_text", "text": string(body)}}}}
			}
			if kind == "without_call" {
				records = []object{meta, response}
			}
			if kind == "duplicate_output" {
				records = append(records, response)
			}
			if kind == "duplicate_call" {
				records = []object{meta, call, call, response}
			}
			var input bytes.Buffer
			for _, r := range records {
				json.NewEncoder(&input).Encode(r)
			}
			_, ok := skillsRuntimeSourceNativeProjection(&input, thread, expected.Writer.Workspace, expected.Writer.Job, expected.Nonce, expected.Task, expected.SpecSHA, start, start.Add(time.Minute), generation)
			if ok != (kind == "normal") {
				t.Fatalf("controller source %s accepted=%v", kind, ok)
			}
		})
	}
}

func TestSkills3080SourcePrepareRunsOnceButDispatchCanRetry(t *testing.T) {
	for _, kind := range []string{"first_prepare", "repeat_prepare", "retry_start", "other_request"} {
		t.Run(kind, func(t *testing.T) {
			p := skillsSourcePreparationFixture()
			thread := "11111111-1111-7111-8111-111111111111"
			start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			flag := "--prepare-skills3080-runtime-source"
			if kind == "retry_start" {
				flag = "--start-skills3080-runtime-source"
			}
			var input bytes.Buffer
			enc := json.NewEncoder(&input)
			enc.Encode(object{"type": "session_meta", "payload": object{"id": thread, "cwd": p.Writer.Workspace}})
			call := func(id, path string) {
				at := start.Add(time.Second)
				if id == "call_current" {
					at = start.Add(3 * time.Second)
				}
				enc.Encode(object{"type": "response_item", "timestamp": at.Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call", "name": "exec", "call_id": id, "input": skillsRuntimeSourceInvocationText(path, p.Writer.Workspace, flag)}})
			}
			if kind != "first_prepare" {
				path := p.RequestPath
				if kind == "other_request" {
					path = filepath.Join(p.Writer.Workspace, "other.json")
				}
				call("call_old", path)
				// A known failure before allocation lets a different request
				// proceed. Missing output is an uncertain outcome, not success.
				body, _ := json.Marshal(object{"qualified": false, "reason": "source", "failure_stage": "source_spec"})
				wrapper, _ := json.Marshal(object{"exit_code": 1, "output": string(body)})
				enc.Encode(object{"type": "response_item", "timestamp": start.Add(2 * time.Second).Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call_output", "call_id": "call_old", "output": []object{{"type": "input_text", "text": "Script completed\nWall time 0.5 seconds\nOutput:\n"}, {"type": "input_text", "text": string(wrapper)}}}})
			}
			call("call_current", p.RequestPath)
			id, ok := skillsRuntimeSourcePendingCall(&input, thread, p.Writer.Workspace, p.RequestPath, flag, start, start.Add(time.Minute), p.RequestID)
			if ok != (kind != "repeat_prepare") || (ok && id != "call_current") {
				t.Fatal("duplicate preparation allocated another root or a normal start retry was blocked")
			}
		})
	}
}

func TestSkills3080PreparationAliasesBindPhysicalRequestIdentity(t *testing.T) {
	file := filepath.Join(skillsFixtureRoot(t), "request.json")
	if os.WriteFile(file, []byte("synthetic request"), 0600) != nil {
		t.Fatal("physical fixture")
	}
	identity := func(path string) skillsObjectID {
		f, e := os.Open(path)
		if e != nil {
			t.Fatal("physical alias open")
		}
		defer f.Close()
		id, ok := skillsHandleID(syscall.Handle(f.Fd()))
		if !ok {
			t.Fatal("physical request identity")
		}
		return id
	}
	original := identity(file)
	for _, kind := range []string{"case_alias", "slash_alias", "renamed_spelling", "different_file", "different_file_wrong_hash", "different_file_missing_hash", "different_file_wrong_call", "start_retry", "partial_case_alias", "partial_slash_alias", "partial_renamed_spelling", "partial_different_file", "partial_start_retry"} {
		t.Run(kind, func(t *testing.T) {
			variant := strings.TrimPrefix(kind, "partial_")
			currentID := original
			if variant == "case_alias" {
				currentID = identity(strings.ToUpper(file))
			}
			if variant == "slash_alias" {
				currentID = identity(filepath.ToSlash(file))
			}
			if strings.HasPrefix(variant, "different_file") {
				other := filepath.Join(filepath.Dir(file), "other.json")
				if os.WriteFile(other, []byte("same bytes different object"), 0600) != nil {
					t.Fatal("other file")
				}
				currentID = identity(other)
				if currentID == original {
					t.Fatal("identity control")
				}
			}
			p := skillsSourcePreparationFixture()
			p.RequestID = original
			cwd, path, flag := p.Writer.Workspace, p.RequestPath, "--prepare-skills3080-runtime-source"
			if variant == "case_alias" {
				path = strings.ToUpper(path)
			}
			if variant == "slash_alias" {
				path = filepath.ToSlash(path)
			}
			if variant == "renamed_spelling" || strings.HasPrefix(variant, "different_file") {
				path = filepath.Join(cwd, "renamed.json")
			}
			if variant == "start_retry" {
				flag = "--start-skills3080-runtime-source"
			}
			// The command paths are synthetic Root paths. The IDs above are from
			// real NTFS alias opens; the parser must use those IDs, not spelling.
			start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			thread := "11111111-1111-7111-8111-111111111111"
			result := object{"qualified": true, "general_access_granted": false, "original_registry_recovered": false, "data_access_granted": false, "operation": "prepare_runtime_source", "runtime_source_preparation": p, "request_sha256": p.RequestSHA}
			if variant == "different_file_wrong_hash" {
				result["request_sha256"] = hashBytes([]byte("different request"))
			}
			if variant == "different_file_missing_hash" {
				delete(result, "request_sha256")
			}
			if variant == "different_file_wrong_call" {
				other := p
				other.CallID = "call_other"
				result["runtime_source_preparation"] = other
			}
			exit := 0
			if strings.HasPrefix(kind, "partial_") {
				result = object{"qualified": false, "reason": "source", "failure_stage": "source_prepare_write", "partial_preparation_possible": true, "ownership_id": p.OwnershipID, "request_object": original}
				exit = 1
			}
			body, _ := json.Marshal(result)
			wrapper, _ := json.Marshal(object{"exit_code": exit, "output": string(body)})
			var input bytes.Buffer
			enc := json.NewEncoder(&input)
			enc.Encode(object{"type": "session_meta", "payload": object{"id": thread, "cwd": cwd}})
			enc.Encode(object{"type": "response_item", "timestamp": start.Add(time.Second).Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call", "name": "exec", "call_id": p.CallID, "input": skillsRuntimeSourceInvocationText(p.RequestPath, cwd, "--prepare-skills3080-runtime-source")}})
			enc.Encode(object{"type": "response_item", "timestamp": start.Add(3 * time.Second).Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call_output", "call_id": p.CallID, "output": []object{{"type": "input_text", "text": "Script completed\nWall time 0.5 seconds\nOutput:\n"}, {"type": "input_text", "text": string(wrapper)}}}})
			enc.Encode(object{"type": "response_item", "timestamp": start.Add(4 * time.Second).Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call", "name": "exec", "call_id": "call_current", "input": skillsRuntimeSourceInvocationText(path, cwd, flag)}})
			_, ok := skillsRuntimeSourcePendingCall(&input, thread, cwd, path, flag, start, start.Add(time.Minute), currentID)
			if ok != (variant == "different_file" || variant == "start_retry") {
				t.Fatal("physical request identity was bypassed by a spelling change")
			}
		})
	}
}
