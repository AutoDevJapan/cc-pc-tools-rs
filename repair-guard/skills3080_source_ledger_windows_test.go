package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSkills3080SourceObjectIDsRequireCanonicalEncoding(t *testing.T) {
	good := skillsObjectID{"123", "abcdefabcdefabcdefabcdefabcdefab"}
	for _, kind := range []string{"normal", "leading_zero", "plus", "space", "upper_hex", "short", "zero"} {
		t.Run(kind, func(t *testing.T) {
			id := good
			switch kind {
			case "leading_zero":
				id.Volume = "0123"
			case "plus":
				id.Volume = "+123"
			case "space":
				id.Volume = "123 "
			case "upper_hex":
				id.File = strings.ToUpper(id.File)
			case "short":
				id.File = id.File[:30]
			case "zero":
				id.File = strings.Repeat("0", 32)
			}
			if skillsSourceObjectShape(id) != (kind == "normal") {
				t.Fatal("object identity spelling was not canonical")
			}
		})
	}
}

func TestSkills3080SourceOutcomeRejectsMalformedClaims(t *testing.T) {
	for _, kind := range []string{"before_allocation", "attempted", "string_flag", "null_flag", "false_flag", "missing_id", "partial_without_owner", "standalone_id", "grant", "mixed_success", "unknown", "alias"} {
		t.Run(kind, func(t *testing.T) {
			result := object{"qualified": false, "reason": "source", "failure_stage": "source_spec"}
			id := skillsObjectID{"123", strings.Repeat("1", 32)}
			switch kind {
			case "attempted":
				result["failure_stage"] = "source_owned_root"
				result["source_attempt_recorded"], result["request_object"] = true, id
			case "string_flag":
				result["source_attempt_recorded"] = "true"
			case "null_flag":
				result["source_attempt_recorded"] = nil
			case "false_flag":
				result["source_attempt_recorded"] = false
			case "missing_id":
				result["source_attempt_recorded"] = true
			case "partial_without_owner":
				result["partial_preparation_possible"], result["request_object"] = true, id
			case "standalone_id":
				result["request_object"] = id
			case "grant":
				result["general_access_granted"] = true
			case "mixed_success":
				result["operation"] = "prepare_runtime_source"
			case "unknown":
				result["other_scope"] = true
			case "alias":
				result["Source_Attempt_Recorded"] = true
			}
			if skillsRuntimeSourceOutcomeShape(result) != (kind == "before_allocation" || kind == "attempted") {
				t.Fatal("malformed outcome was treated as a known claim")
			}
		})
	}
}

func TestSkills3080SourcePendingCallLedgerRejectsRebinding(t *testing.T) {
	for _, kind := range []string{"normal", "ordinary", "ordinary_function", "prior_output", "outer_outside_window", "duplicate_custom", "duplicate_function", "duplicate_output", "output_rebound", "source_call_hijacked", "custom_to_function", "function_to_custom", "pending_source_wrong_output", "unknown_output", "unknown_call", "unknown_unrelated_id", "unknown_empty_id", "unknown_null_id", "unknown_outer", "unknown_outer_empty", "unknown_outer_null", "outer_call_id", "outer_case_alias", "outer_bad_time"} {
		t.Run(kind, func(t *testing.T) {
			p := skillsSourcePreparationFixture()
			start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			thread := "11111111-1111-7111-8111-111111111111"
			flag := "--prepare-skills3080-runtime-source"
			var input bytes.Buffer
			enc := json.NewEncoder(&input)
			enc.Encode(object{"type": "session_meta", "payload": object{"id": thread, "cwd": p.Writer.Workspace}})
			emit := func(payload object) {
				enc.Encode(object{"type": "response_item", "timestamp": start.Add(time.Second).Format(time.RFC3339Nano), "payload": payload})
			}
			call := object{"type": "custom_tool_call", "name": "exec", "call_id": "call_ordinary", "input": "text('ordinary');"}
			output := object{"type": "custom_tool_call_output", "call_id": "call_ordinary", "output": "ordinary"}
			switch kind {
			case "ordinary":
				emit(call)
				emit(output)
			case "ordinary_function":
				call["type"], output["type"] = "function_call", "function_call_output"
				emit(call)
				emit(output)
			case "custom_to_function":
				emit(call)
				output["type"] = "function_call_output"
				emit(output)
			case "function_to_custom":
				call["type"] = "function_call"
				emit(call)
				emit(output)
			case "prior_output":
				emit(output)
			case "duplicate_custom":
				emit(call)
				emit(call)
				emit(output)
			case "duplicate_function":
				call["type"], call["name"] = "function_call", "ordinary"
				emit(call)
				emit(call)
			case "duplicate_output":
				emit(call)
				emit(output)
				emit(output)
			case "output_rebound":
				emit(output)
				emit(call)
			case "source_call_hijacked":
				emit(object{"type": "custom_tool_call", "name": "exec", "call_id": "call_ordinary", "input": skillsRuntimeSourceInvocationText(p.RequestPath, p.Writer.Workspace, flag)})
				emit(call)
				body, _ := json.Marshal(object{"qualified": false, "reason": "source", "failure_stage": "source_owned_root", "source_attempt_recorded": true, "request_object": p.RequestID})
				wrapper, _ := json.Marshal(object{"exit_code": 1, "output": string(body)})
				output["output"] = []object{{"type": "input_text", "text": "Script completed\nWall time 0.5 seconds\nOutput:\n"}, {"type": "input_text", "text": string(wrapper)}}
				emit(output)
			}
			path := strings.ToUpper(p.RequestPath)
			if kind == "unknown_call" {
				emit(object{"type": "future_call_type", "name": "exec", "call_id": "call_future", "input": "unknown"})
			}
			if kind == "unknown_unrelated_id" {
				emit(object{"type": "future_result_type", "call_id": "call_future", "output": "unknown"})
			}
			emit(object{"type": "custom_tool_call", "name": "exec", "call_id": "call_current", "input": skillsRuntimeSourceInvocationText(path, p.Writer.Workspace, flag)})
			if kind == "pending_source_wrong_output" {
				emit(object{"type": "function_call_output", "call_id": "call_current", "output": "not a pending custom call"})
			}
			if kind == "unknown_output" {
				emit(object{"type": "future_result_type", "call_id": "call_current", "output": "unknown"})
			}
			if kind == "unknown_empty_id" {
				emit(object{"type": "future_result_type", "call_id": "", "output": "unknown"})
			}
			if kind == "unknown_null_id" {
				emit(object{"type": "future_result_type", "call_id": nil, "output": "unknown"})
			}
			if strings.HasPrefix(kind, "unknown_outer") || strings.HasPrefix(kind, "outer_") {
				payload := object{"type": "custom_tool_call_output", "call_id": "call_current", "output": "unknown envelope"}
				record := object{"type": "future_envelope", "timestamp": start.Add(2 * time.Second).Format(time.RFC3339Nano), "payload": payload}
				switch kind {
				case "unknown_outer_empty":
					payload["call_id"] = ""
				case "unknown_outer_null":
					payload["call_id"] = nil
				case "outer_call_id":
					delete(payload, "call_id")
					record["call_id"] = "call_current"
				case "outer_case_alias":
					delete(payload, "call_id")
					record["Call_Id"] = "call_current"
				case "outer_bad_time":
					record["timestamp"] = "invalid"
				case "outer_outside_window":
					record["timestamp"] = start.Add(-time.Second).Format(time.RFC3339Nano)
				}
				enc.Encode(record)
			}
			id, ok := skillsRuntimeSourcePendingCall(&input, thread, p.Writer.Workspace, path, flag, start, start.Add(time.Minute), p.RequestID)
			want := kind == "normal" || kind == "ordinary" || kind == "ordinary_function" || kind == "prior_output" || kind == "outer_outside_window"
			if ok != want || (ok && id != "call_current") {
				t.Fatalf("ledger %s accepted=%v", kind, ok)
			}
		})
	}
}
