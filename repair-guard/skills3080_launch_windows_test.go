package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSkills3080CreationRequiresThePendingHiddenLaunchBeforeWriting(t *testing.T) {
	for _, kind := range []string{"hidden_pending", "legacy_hidden_pending", "old_direct", "no_window_false", "wrong_request", "wrong_cwd", "already_returned", "missing_call", "duplicate_call", "outside_window", "unknown_operation"} {
		t.Run(kind, func(t *testing.T) {
			cwd, path, thread := "D:/CC/Work/synthetic", "D:/CC/Work/synthetic/.ci-work/request.json", "11111111-1111-7111-8111-111111111111"
			start := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
			op := "create-skills3080-forward"
			input := skillsForwardInvocation(path, cwd)
			if kind == "legacy_hidden_pending" {
				op = "create-skills3080"
				c := skillsForwardCommand(path, cwd)
				c.Cmd = skillsHiddenProcessScript(skillsRuntimeTarget, "--"+op, path, cwd)
				b, _ := json.Marshal(c)
				input = "text(await tools.exec_command(" + string(b) + "));"
			}
			if kind == "old_direct" {
				b, _ := json.Marshal(skillsLegacyForwardCommand(path, cwd))
				input = "text(await tools.exec_command(" + string(b) + "));"
			}
			if kind == "wrong_request" {
				input = skillsForwardInvocation(path+".copy", cwd)
			}
			if kind == "wrong_cwd" {
				input = skillsForwardInvocation(path, "D:/elsewhere")
			}
			if kind == "no_window_false" {
				c := skillsForwardCommand(path, cwd)
				c.Cmd = string(bytes.Replace([]byte(c.Cmd), []byte("CreateNoWindow = $true"), []byte("CreateNoWindow = $false"), 1))
				b, _ := json.Marshal(c)
				input = "text(await tools.exec_command(" + string(b) + "));"
			}
			if kind == "unknown_operation" {
				op = "other"
			}
			stamp := start.Add(time.Second)
			if kind == "outside_window" {
				stamp = start.Add(-time.Second)
			}
			call := object{"type": "response_item", "timestamp": stamp.Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call", "name": "exec", "call_id": "call_pending_fixture", "input": input}}
			records := []object{{"type": "session_meta", "timestamp": start.Format(time.RFC3339Nano), "payload": object{"id": thread, "cwd": cwd}}}
			if kind != "missing_call" {
				records = append(records, call)
			}
			if kind == "duplicate_call" {
				records = append(records, call)
			}
			if kind == "already_returned" {
				records = append(records, object{"type": "response_item", "timestamp": start.Add(2 * time.Second).Format(time.RFC3339Nano), "payload": object{"type": "custom_tool_call_output", "call_id": "call_pending_fixture", "output": "opaque"}})
			}
			var native bytes.Buffer
			for _, r := range records {
				json.NewEncoder(&native).Encode(r)
			}
			m, root := skillsParsedFixture(t), skillsFixtureRoot(t)
			fs := skillsFS{blocked: func(string) bool { return false }}
			defer fs.close()
			checks := 0
			count, reason := skillsCreateAfterLaunch(m, root, &fs, func() bool {
				checks++
				return skillsForwardPendingLaunch(bytes.NewReader(native.Bytes()), thread, cwd, path, op, start, start.Add(time.Minute))
			})
			want := kind == "hidden_pending" || kind == "legacy_hidden_pending"
			if checks != 1 || ((reason == "") != want) || (want && count != len(m.Files)) || (!want && (count != 0 || reason != "native_launch")) {
				t.Fatalf("premutation launch %s count=%d reason=%s", kind, count, reason)
			}
			if !want {
				for _, target := range skillsTargets(m) {
					if _, e := os.Lstat(filepath.Join(root, target)); !os.IsNotExist(e) {
						t.Fatal("rejected invocation created a target")
					}
				}
			}
		})
	}
}
