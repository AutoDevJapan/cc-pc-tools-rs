package main

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
	"time"
)

// Pre-mutation proof of the exact still-pending canonical native tool call.
// A completed/copied receipt cannot authorize a second CreateNew.
func skillsForwardPendingLaunch(input io.Reader, thread, cwd, path, op string, start, end time.Time) bool {
	if op != "create-skills3080-forward" && op != "create-skills3080" {
		return false
	}
	if end.Before(start) {
		return false
	}
	scan := bufio.NewScanner(input)
	scan.Buffer(make([]byte, 65536), 16<<20)
	session := false
	calls := map[string]bool{}
	seen := map[string]bool{}
	for scan.Scan() {
		raw := scan.Bytes()
		if !skillsNativeUniqueJSON(raw) {
			return false
		}
		var e struct {
			Type, Timestamp string
			Payload         json.RawMessage
		}
		if !skillsCreationHeaderKeys(raw, "type", "timestamp", "payload") || json.Unmarshal(raw, &e) != nil {
			return false
		}
		if e.Type != "session_meta" && e.Type != "response_item" {
			continue
		}
		var p struct {
			Type, ID, Cwd, Name, Input string
			Call                       string `json:"call_id"`
		}
		if !skillsCreationHeaderKeys(e.Payload, "type", "id", "cwd", "name", "input", "call_id") || json.Unmarshal(e.Payload, &p) != nil {
			return false
		}
		if e.Type == "session_meta" {
			if session || p.ID != thread || !same(p.Cwd, cwd) {
				return false
			}
			session = true
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, e.Timestamp)
		if err != nil {
			return false
		}
		if at.Before(start) || at.After(end) {
			continue
		}
		if !session {
			return false
		}
		if p.Type == "custom_tool_call" && p.Name == "exec" {
			contract, valid := skillsCreationInvocationContract(p.Input, path, cwd)
			if op == "create-skills3080" {
				const prefix, suffix = "text(await tools.exec_command(", "));"
				text := strings.TrimSpace(p.Input)
				var cmd skillsCreationCommand
				valid = strings.HasPrefix(text, prefix) && strings.HasSuffix(text, suffix) && skillsStrict([]byte(strings.TrimSuffix(strings.TrimPrefix(text, prefix), suffix)), &cmd)
				expected := skillsForwardCommand(path, cwd)
				expected.Cmd = skillsHiddenProcessScript(skillsRuntimeTarget, "--create-skills3080", path, cwd)
				valid = valid && cmd == expected
				contract = skillsHiddenNativeContract
			}
			if valid {
				if !skillsCallID.MatchString(p.Call) || seen[p.Call] || contract != skillsHiddenNativeContract {
					return false
				}
				calls[p.Call] = true
				seen[p.Call] = true
			}
		}
		if p.Type == "custom_tool_call_output" {
			delete(calls, p.Call)
		}
	}
	return scan.Err() == nil && session && len(calls) == 1
}

func skillsForwardLaunchBeforeCreate(s *skillsFS, job object, path, cwd, op string) bool {
	start, e := time.Parse(time.RFC3339Nano, str(job["started_at"]))
	end := time.Now()
	if e != nil || job["status"] != "running" || !same(str(job["cwd"]), cwd) || !skillsUUID.MatchString(str(job["thread_id"])) {
		return false
	}
	input, ok := skillsNativeSnapshot(s, job)
	return ok && skillsForwardPendingLaunch(input, str(job["thread_id"]), cwd, path, op, start, end)
}

func skillsCreateAfterLaunch(m skillsManifest, root string, s *skillsFS, verify func() bool) (int, string) {
	if verify == nil || !verify() {
		return 0, "native_launch"
	}
	return skillsCreate(m, root, s)
}
