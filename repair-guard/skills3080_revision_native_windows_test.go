package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func skillsRevisionNativeFixture(e skillsRevisionEvidence, c skillsConsumeRequest, op string) []object {
	result, _ := json.Marshal(object{"qualified": true, "operation": op, "request_sha256": e.RequestSHA, "manifest_sha256": e.ManifestSHA, "general_access_granted": false, "original_registry_recovered": false, "revision_evidence": e})
	wrapper, _ := json.Marshal(object{"exit_code": 0, "output": string(result) + "\n"})
	return []object{
		{"type": "session_meta", "payload": object{"id": "synthetic-thread", "cwd": c.Creator.Workspace}},
		{"type": "response_item", "timestamp": "2026-10-26T05:00:01Z", "payload": object{"type": "custom_tool_call", "call_id": c.CallID, "name": "exec", "input": skillsRevisionInvocation(op, c.RequestPath, c.Creator.Workspace)}},
		{"type": "response_item", "timestamp": "2026-10-26T05:00:02Z", "payload": object{"type": "custom_tool_call_output", "call_id": c.CallID, "output": []object{{"type": "input_text", "text": "Script completed\nWall time 1.0 seconds\nOutput:\n"}, {"type": "input_text", "text": string(wrapper)}}}},
	}
}

func TestSkills3080RevisionNativeAuthority(t *testing.T) {
	w := skillsTestManifest().Writer
	c := skillsConsumeRequest{Creator: w, RequestPath: w.Workspace + "/.ci-work/revision.json", RequestSHA: strings.Repeat("a", 64), CallID: "call_revision_test"}
	e := skillsRevisionEvidence{Writer: w, RequestSHA: c.RequestSHA, ManifestSHA: skillsRevisionManifestSHA}
	start, _ := time.Parse(time.RFC3339, "2026-10-26T05:00:00Z")
	end := start.Add(time.Minute)
	for _, op := range []string{"prepare-skills3080-revision", "apply-skills3080-revision"} {
		for _, kind := range []string{"valid", "other_operation", "other_path", "other_thread", "other_cwd", "wrong_call", "early", "late", "duplicate", "duplicate_result", "echo", "copy", "other_writer", "other_request", "session_yield", "exit1", "truncated", "result_before_call", "missing_call", "missing_result", "replay_request"} {
			t.Run(op+"/"+kind, func(t *testing.T) {
				records := skillsRevisionNativeFixture(e, c, op)
				call := obj(records[1]["payload"])
				response := obj(records[2]["payload"])
				switch kind {
				case "other_operation":
					call["input"] = skillsRevisionInvocation("prepare-skills3080-revision", c.RequestPath, c.Creator.Workspace)
					if op == "prepare-skills3080-revision" {
						call["input"] = skillsRevisionInvocation("apply-skills3080-revision", c.RequestPath, c.Creator.Workspace)
					}
				case "other_path":
					call["input"] = skillsRevisionInvocation(op, c.RequestPath+".copy", c.Creator.Workspace)
				case "other_thread":
					obj(records[0]["payload"])["id"] = "other"
				case "other_cwd":
					obj(records[0]["payload"])["cwd"] = "D:/other"
				case "wrong_call":
					call["call_id"] = "call_wrong"
				case "early":
					records[1]["timestamp"] = start.Add(-time.Second).Format(time.RFC3339)
				case "late":
					records[2]["timestamp"] = end.Add(time.Second).Format(time.RFC3339)
				case "duplicate":
					records = append(records, records[1])
				case "duplicate_result":
					records = append(records, records[2])
				case "echo":
					call["input"] = "text({qualified:true});"
				case "copy":
					response["type"] = "message"
				case "other_writer":
					x := e
					x.Writer.Job += "x"
					records = skillsRevisionNativeFixture(x, c, op)
				case "other_request", "replay_request":
					x := e
					x.RequestSHA = strings.Repeat("b", 64)
					records = skillsRevisionNativeFixture(x, c, op)
				case "session_yield", "exit1", "truncated":
					blocks := response["output"].([]object)
					wrapper, _ := conversationJSON([]byte(str(blocks[1]["text"])))
					if kind == "session_yield" {
						wrapper["session_id"] = 17
					}
					if kind == "exit1" {
						wrapper["exit_code"] = 1
					}
					if kind == "truncated" {
						wrapper["output"] = `{"qualified":true`
					}
					b, _ := json.Marshal(wrapper)
					blocks[1]["text"] = string(b)
				case "result_before_call":
					records[1], records[2] = records[2], records[1]
				case "missing_call":
					records = append(records[:1], records[2])
				case "missing_result":
					records = records[:2]
				}
				var stream bytes.Buffer
				for _, record := range records {
					json.NewEncoder(&stream).Encode(record)
				}
				got, ok := skillsRevisionNativeProjection(&stream, "synthetic-thread", w.Workspace, start, end, c, op)
				if ok != (kind == "valid") || (ok && skillsProjectionSHA(got) != skillsProjectionSHA(e)) {
					t.Fatalf("%s accepted=%v", kind, ok)
				}
			})
		}
	}
}
