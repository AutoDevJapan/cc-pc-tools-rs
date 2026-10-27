package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type skillsCreationCommand struct {
	Cmd       string `json:"cmd"`
	Workdir   string `json:"workdir"`
	Login     bool   `json:"login"`
	Shell     string `json:"shell"`
	Yield     int    `json:"yield_time_ms"`
	MaxTokens int    `json:"max_output_tokens"`
}

func skillsForwardCommand(path, workspace string) skillsCreationCommand {
	return skillsCreationCommand{Cmd: "& '" + skillsRuntimeTarget + "' --create-skills3080-forward '" + path + "'",
		Workdir: workspace, Login: false, Shell: "powershell", Yield: 30000, MaxTokens: 30000}
}

// Returned by preflight. This deliberately small language contains exactly one
// awaited tool call and one unmodified result. No echo, script file, environment
// override, arbitrary JS, alternate executable, or copied receipt is accepted.
func skillsForwardInvocation(path, workspace string) string {
	if !filepath.IsAbs(path) || !within(workspace, path) || strings.ContainsAny(path, "'\r\n") {
		return ""
	}
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if e.Encode(skillsForwardCommand(path, workspace)) != nil {
		return ""
	}
	return "text(await tools.exec_command(" + strings.TrimSpace(b.String()) + "));"
}

func skillsCreationCall(input, path, workspace string) bool {
	const prefix, suffix = "text(await tools.exec_command(", "));"
	input = strings.TrimSpace(input)
	if skillsForwardInvocation(path, workspace) == "" || !strings.HasPrefix(input, prefix) || !strings.HasSuffix(input, suffix) {
		return false
	}
	var cmd skillsCreationCommand
	return skillsStrict([]byte(strings.TrimSuffix(strings.TrimPrefix(input, prefix), suffix)), &cmd) && cmd == skillsForwardCommand(path, workspace)
}

func skillsCreationAuthorBound(creator, consumer skillsWriter, reg skillsRegistry, job object) bool {
	row, ok := reg.Tasks[3080]
	if !ok || !skillsWriterValid(creator) || !skillsWriterValid(consumer) || creator.Job != str(job["id"]) || !skillsJobTask(job, 3080) ||
		!same(str(job["cwd"]), creator.Workspace) || !skillsModelIdentifier(str(job["model"])) {
		return false
	}
	switch str(job["status"]) {
	case "running", "succeeded", "failed", "cancelled", "canceled", "lost":
	default:
		return false
	}
	originAt, currentAt := -1, -1
	for i, id := range row.Jobs {
		if id == creator.Job {
			if originAt >= 0 {
				return false
			}
			originAt = i
		}
		if id == consumer.Job {
			if currentAt >= 0 {
				return false
			}
			currentAt = i
		}
	}
	return originAt >= 0 && currentAt >= originAt && skillsLatest(row, consumer.Job)
}

var skillsCallID = regexp.MustCompile(`^call_[A-Za-z0-9_-]{1,160}$`)

func skillsCreationSessionDays(thread string, start time.Time) []string {
	days := []string{}
	add := func(at time.Time) {
		day := at.Format("2006/01/02")
		for _, old := range days {
			if old == day {
				return
			}
		}
		days = append(days, day)
	}
	add(start)
	add(start.AddDate(0, 0, 1))
	// A normal resumed job retains the original UUIDv7 session, which may
	// predate this actor by days. Its embedded birth time selects only three
	// exact directories (UTC +/- one day for local date layout), never a scan.
	if skillsUUID.MatchString(thread) && thread[14] == '7' {
		ms, err := strconv.ParseUint(thread[:8]+thread[9:13], 16, 48)
		birth := time.UnixMilli(int64(ms)).UTC()
		if err == nil && !birth.After(start.Add(5*time.Minute)) {
			for _, day := range []int{-1, 0, 1} {
				add(birth.AddDate(0, 0, day))
			}
		}
	}
	return days
}

// Only tool response records are examined. Messages/user text, arbitrary
// embedded JSON, and result copies are never receipt issuers. The canonical
// session remains the trust boundary, as in the existing exact-result route.
func skillsCreationNativeProjection(input io.Reader, thread, cwd string, start, end time.Time, c skillsConsumeRequest) (skillsCreatedEvidence, bool) {
	var evidence skillsCreatedEvidence
	if !skillsCallID.MatchString(c.CallID) || end.Before(start) {
		return evidence, false
	}
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64<<10), 16<<20)
	session, called, returned := false, false, false
	var callAt time.Time
	for scanner.Scan() {
		var env struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &env) != nil {
			return evidence, false
		}
		if env.Type != "session_meta" && env.Type != "response_item" {
			continue
		}
		// Projection does not decode or return unrelated message bodies.
		var p struct {
			Type   string          `json:"type"`
			ID     string          `json:"id"`
			Cwd    string          `json:"cwd"`
			Name   string          `json:"name"`
			CallID string          `json:"call_id"`
			Input  string          `json:"input"`
			Output json.RawMessage `json:"output"`
		}
		if json.Unmarshal(env.Payload, &p) != nil {
			return evidence, false
		}
		if env.Type == "session_meta" {
			if session || p.ID != thread || !same(p.Cwd, cwd) {
				return evidence, false
			}
			session = true
			continue
		}
		if p.CallID != c.CallID {
			continue
		}
		if _, ok := conversationJSON(scanner.Bytes()); !ok {
			return evidence, false
		}
		at, err := time.Parse(time.RFC3339Nano, env.Timestamp)
		if !session || err != nil || at.Before(start) || at.After(end) {
			return evidence, false
		}
		switch p.Type {
		case "custom_tool_call":
			if called || returned || p.Name != "exec" || !skillsCreationCall(p.Input, c.RequestPath, cwd) {
				return evidence, false
			}
			called, callAt = true, at
		case "custom_tool_call_output":
			if !called || returned || at.Before(callAt) {
				return evidence, false
			}
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(p.Output, &blocks) != nil || len(blocks) != 2 || blocks[0].Type != "input_text" || blocks[1].Type != "input_text" ||
				!strings.HasPrefix(blocks[0].Text, "Script completed\nWall time ") || !strings.HasSuffix(blocks[0].Text, "\nOutput:\n") {
				return evidence, false
			}
			wrapper, ok := conversationJSON([]byte(blocks[1].Text))
			if !ok || wrapper["exit_code"] != json.Number("0") || wrapper["session_id"] != nil {
				return evidence, false
			}
			// The command must finish in one normal tool response. A yielded or
			// truncated result stays held; never repeat CreateNew to repair it.
			result, ok := conversationJSON([]byte(str(wrapper["output"])))
			if !ok || result["qualified"] != true || result["operation"] != "CreateNew" || result["request_sha256"] != c.RequestSHA ||
				result["general_access_granted"] != false || result["original_registry_recovered"] != false {
				return evidence, false
			}
			b, err := json.Marshal(result["creation_evidence"])
			if err != nil || !skillsStrict(b, &evidence) || evidence.Writer != c.Creator || evidence.RequestSHA != c.RequestSHA ||
				conversationNumber(result["created_files"]) != uint64(len(evidence.Files)) || conversationNumber(result["file_count"]) != uint64(len(evidence.Files)) ||
				result["manifest_sha256"] != evidence.ManifestSHA || result["source_commit"] != evidence.ManifestCommit {
				return evidence, false
			}
			returned = true
		default:
			return evidence, false
		}
	}
	return evidence, scanner.Err() == nil && session && called && returned
}

func skillsReadCreationNative(s *skillsFS, job object, c skillsConsumeRequest) (skillsCreatedEvidence, bool) {
	var empty skillsCreatedEvidence
	id := str(job["thread_id"])
	start, err := time.Parse(time.RFC3339Nano, str(job["started_at"]))
	end := time.Now()
	if err != nil || !skillsUUID.MatchString(id) || start.After(end) {
		return empty, false
	}
	if job["status"] != "running" {
		end, err = time.Parse(time.RFC3339Nano, str(job["finished_at"]))
		if err != nil || end.Before(start) {
			return empty, false
		}
	}
	input, ok := skillsNativeSnapshot(s, job)
	if !ok {
		return empty, false
	}
	return skillsCreationNativeProjection(input, id, c.Creator.Workspace, start, end, c)
}
