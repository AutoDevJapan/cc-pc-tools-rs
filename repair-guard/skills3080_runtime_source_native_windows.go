package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const skillsRuntimeSourceDomain = "skills-3080-runtime-source-v1"

type skillsRuntimeSourcePreparation struct {
	Domain        string         `json:"domain"`
	Writer        skillsWriter   `json:"writer"`
	WriterSpec    string         `json:"writer_spec"`
	Task          int            `json:"task"`
	PreviousJob   string         `json:"previous_job"`
	TemplateSHA   string         `json:"template_sha256"`
	SpecSHA       string         `json:"spec_sha256"`
	PreparedSpec  string         `json:"prepared_spec"`
	Nonce         string         `json:"nonce"`
	OwnershipID   string         `json:"ownership_id"`
	Root          string         `json:"root"`
	RootID        skillsObjectID `json:"root_object"`
	TraceRoot     string         `json:"trace_root"`
	TraceID       skillsObjectID `json:"trace_object"`
	PreparedAt    string         `json:"prepared_at"`
	RequestPath   string         `json:"request_path"`
	RequestSHA    string         `json:"request_sha256"`
	CallID        string         `json:"native_call_id"`
	Producer      skillsProducer `json:"producer"`
	SourceMeaning string         `json:"source_meaning"`
}

func skillsSourceObjectShape(id skillsObjectID) bool {
	_, err := strconv.ParseUint(id.Volume, 10, 64)
	b, err2 := hex.DecodeString(id.File)
	return err == nil && err2 == nil && len(b) == 16 && id.File != strings.Repeat("0", 32)
}

func skillsRuntimeSourcePreparationShape(p skillsRuntimeSourcePreparation) bool {
	_, err := time.Parse(time.RFC3339Nano, p.PreparedAt)
	return p.Domain == skillsRuntimeSourceDomain && skillsWriterValid(p.Writer) && p.Task > 0 && p.Task != 3080 && p.Task != 3686 && (p.PreviousJob == "" || skillsResultID(skillsResultRequest{Task: p.Task, Job: p.PreviousJob})) &&
		skillsDigest(p.TemplateSHA) && skillsDigest(p.SpecSHA) && skillsRuntimeNonce(p.Nonce) && skillsRuntimeNonce(p.OwnershipID) && skillsDigest(p.RequestSHA) && skillsCallID.MatchString(p.CallID) &&
		skillsConsumerSpecPath(p.WriterSpec) && same(p.Root, filepath.Join(p.Writer.Workspace, "codex-owned-"+p.OwnershipID)) &&
		same(p.TraceRoot, filepath.Join(p.Root, "trace")) && same(p.PreparedSpec, filepath.Join(p.Root, "scope.json")) &&
		filepath.IsAbs(p.RequestPath) && within(p.Writer.Workspace, p.RequestPath) && filepath.Ext(p.RequestPath) == ".json" && !strings.ContainsAny(p.RequestPath, "'\r\n") &&
		skillsSourceObjectShape(p.RootID) && skillsSourceObjectShape(p.TraceID) && skillsDigest(p.Producer.SHA) && skillsGitObjectID(p.Producer.Revision) && err == nil && p.SourceMeaning == "first_recorded_model_visible_request_object"
}

func skillsRuntimeSourceCommand(path, cwd, flag string) skillsCreationCommand {
	if !skillsRuntimeSourceInvocationPaths(path, cwd, flag) {
		return skillsCreationCommand{}
	}
	c := skillsForwardCommand(path, cwd)
	c.Cmd = skillsHiddenProcessScript(skillsRuntimeTarget, flag, path, cwd)
	return c
}

func skillsRuntimeSourceInvocationPaths(path, cwd, flag string) bool {
	return (flag == "--prepare-skills3080-runtime-source" || flag == "--start-skills3080-runtime-source") &&
		filepath.IsAbs(path) && within(cwd, path) && skillsCreationPathEqual(path, path) && skillsCreationPathEqual(cwd, cwd)
}

func skillsRuntimeSourceInvocation(input, path, cwd, flag string) bool {
	if !skillsRuntimeSourceInvocationPaths(path, cwd, flag) {
		return false
	}
	const prefix, suffix = "text(await tools.exec_command(", "));"
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, prefix) || !strings.HasSuffix(input, suffix) {
		return false
	}
	var command skillsCreationCommand
	if !skillsStrict([]byte(strings.TrimSuffix(strings.TrimPrefix(input, prefix), suffix)), &command) {
		return false
	}
	return command == skillsRuntimeSourceCommand(path, cwd, flag)
}

func skillsRuntimeSourceInvocationText(path, cwd, flag string) string {
	if !skillsRuntimeSourceInvocationPaths(path, cwd, flag) {
		return ""
	}
	b, _ := json.Marshal(skillsRuntimeSourceCommand(path, cwd, flag))
	return "text(await tools.exec_command(" + string(b) + "));"
}

func skillsRuntimeSourcePendingCall(input io.Reader, thread, cwd, path, flag string, start, end time.Time) (string, bool) {
	scan := bufio.NewScanner(input)
	scan.Buffer(make([]byte, 64<<10), 16<<20)
	session := false
	pending, seen := map[string]bool{}, map[string]bool{}
	for scan.Scan() {
		var e struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if !skillsNativeUniqueJSON(scan.Bytes()) || !skillsCreationHeaderKeys(scan.Bytes(), "type", "timestamp", "payload") || json.Unmarshal(scan.Bytes(), &e) != nil {
			return "", false
		}
		if e.Type != "session_meta" && e.Type != "response_item" {
			continue
		}
		var p struct {
			Type string `json:"type"`
			ID   string `json:"id"`
			Cwd  string `json:"cwd"`
			Name string `json:"name"`
			Call string `json:"call_id"`
			Code string `json:"input"`
		}
		if !skillsCreationHeaderKeys(e.Payload, "type", "id", "cwd", "name", "call_id", "input") || json.Unmarshal(e.Payload, &p) != nil {
			return "", false
		}
		if e.Type == "session_meta" {
			if session || p.ID != thread || !same(p.Cwd, cwd) {
				return "", false
			}
			session = true
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, e.Timestamp)
		if err != nil {
			return "", false
		}
		if at.Before(start) || at.After(end) {
			continue
		}
		if p.Type == "custom_tool_call" && p.Name == "exec" && skillsRuntimeSourceInvocation(p.Code, path, cwd, flag) {
			if !session || !skillsCallID.MatchString(p.Call) || seen[p.Call] {
				return "", false
			}
			pending[p.Call], seen[p.Call] = true, true
		}
		if p.Type == "custom_tool_call_output" {
			delete(pending, p.Call)
		}
	}
	if scan.Err() != nil || !session || len(pending) != 1 {
		return "", false
	}
	// Repeating the same preparation must not allocate another owned root.
	// Starting an already prepared task may still retry normal admission.
	if flag == "--prepare-skills3080-runtime-source" && len(seen) != 1 {
		return "", false
	}
	for id := range pending {
		return id, true
	}
	return "", false
}

// Only completed normal tool results may issue a source preparation. User or
// assistant messages, an embedded receipt, and a yielded result cannot do so.
func skillsRuntimeSourceNativeProjection(input io.Reader, thread, cwd, parentJob, nonce string, task int, specSHA string, start, end, generation time.Time) (skillsRuntimeSourcePreparation, bool) {
	var out skillsRuntimeSourcePreparation
	if !skillsUUID.MatchString(thread) || !skillsRuntimeNonce(nonce) || end.Before(start) {
		return out, false
	}
	scan := bufio.NewScanner(input)
	scan.Buffer(make([]byte, 64<<10), 16<<20)
	type call struct {
		Input string
		At    time.Time
	}
	calls := map[string]call{}
	seen := map[string]bool{}
	session, matched := false, false
	for scan.Scan() {
		var e struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if !skillsNativeUniqueJSON(scan.Bytes()) || !skillsCreationHeaderKeys(scan.Bytes(), "type", "timestamp", "payload") || json.Unmarshal(scan.Bytes(), &e) != nil {
			return out, false
		}
		if e.Type != "session_meta" && e.Type != "response_item" {
			continue
		}
		var p struct {
			Type   string          `json:"type"`
			ID     string          `json:"id"`
			Cwd    string          `json:"cwd"`
			Name   string          `json:"name"`
			CallID string          `json:"call_id"`
			Input  string          `json:"input"`
			Output json.RawMessage `json:"output"`
		}
		if !skillsCreationHeaderKeys(e.Payload, "type", "id", "cwd", "name", "call_id", "input", "output") || json.Unmarshal(e.Payload, &p) != nil {
			return out, false
		}
		if e.Type == "session_meta" {
			if session || p.ID != thread || !same(p.Cwd, cwd) {
				return out, false
			}
			session = true
			continue
		}
		if p.Type != "custom_tool_call" && p.Type != "custom_tool_call_output" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, e.Timestamp)
		if err != nil {
			return out, false
		}
		if at.Before(start) || at.After(end) {
			continue
		}
		if !session || !skillsCallID.MatchString(p.CallID) {
			return out, false
		}
		if p.Type == "custom_tool_call" {
			if seen[p.CallID] {
				return out, false
			}
			seen[p.CallID] = true
			if p.Name == "exec" {
				calls[p.CallID] = call{p.Input, at}
			}
			continue
		}
		if matched && p.CallID == out.CallID {
			return out, false
		}
		origin, exists := calls[p.CallID]
		if !exists {
			continue
		}
		delete(calls, p.CallID)
		result, ok := skillsRuntimeSourceToolOutput(p.Output)
		if !ok || result["operation"] != "prepare_runtime_source" {
			continue
		}
		var candidate skillsRuntimeSourcePreparation
		encoded, err := json.Marshal(result["runtime_source_preparation"])
		if err != nil || !skillsStrict(encoded, &candidate) || candidate.Nonce != nonce {
			continue
		}
		prepared, err := time.Parse(time.RFC3339Nano, candidate.PreparedAt)
		if matched || !skillsRuntimeSourcePreparationShape(candidate) || candidate.Task != task || candidate.SpecSHA != specSHA || candidate.Writer.Job != parentJob || !same(candidate.Writer.Workspace, cwd) || candidate.CallID != p.CallID ||
			result["request_sha256"] != candidate.RequestSHA || !skillsRuntimeSourceInvocation(origin.Input, candidate.RequestPath, cwd, "--prepare-skills3080-runtime-source") ||
			err != nil || prepared.Before(origin.At) || at.Before(prepared) || at.After(generation) {
			return out, false
		}
		out, matched = candidate, true
	}
	return out, scan.Err() == nil && session && matched
}

func skillsRuntimeSourceToolOutput(raw json.RawMessage) (object, bool) {
	var encoded []json.RawMessage
	if json.Unmarshal(raw, &encoded) != nil || len(encoded) != 2 {
		return nil, false
	}
	var blocks [2]struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	for i := range blocks {
		if !skillsStrict(encoded[i], &blocks[i]) {
			return nil, false
		}
	}
	if blocks[0].Type != "input_text" || blocks[1].Type != "input_text" || !strings.HasPrefix(blocks[0].Text, "Script completed\nWall time ") || !strings.HasSuffix(blocks[0].Text, "\nOutput:\n") {
		return nil, false
	}
	wrapper, ok := conversationJSON([]byte(blocks[1].Text))
	if !ok || wrapper["exit_code"] != json.Number("0") || wrapper["session_id"] != nil {
		return nil, false
	}
	result, ok := conversationJSON([]byte(str(wrapper["output"])))
	return result, ok && result["qualified"] == true && result["general_access_granted"] == false && result["original_registry_recovered"] == false && result["data_access_granted"] == false
}
