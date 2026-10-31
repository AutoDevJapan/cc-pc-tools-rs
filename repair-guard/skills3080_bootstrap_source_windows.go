package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode/utf8"
)

// The launch world_state and its rendering must agree within this snapshot.
// Typed data operations additionally compare the full initial input against
// an independent transport record; same-log agreement alone is insufficient.
// No current instruction file, alternate result path or caller digest is read.
func skillsRuntimeBootstrapProjection(input io.Reader, job object, project []byte, start, end time.Time) ([]byte, bool, bool) {
	return skillsRuntimeBootstrapProjectionWithOrigin(input, job, project, start, end, nil)
}
func skillsRuntimeBootstrapProjectionWithOrigin(input io.Reader, job object, project []byte, start, end time.Time, origin func() (skillsRuntimeOrigin, bool), envelopes ...map[string]bool) ([]byte, bool, bool) {
	scan := bufio.NewScanner(io.LimitReader(input, 4<<20))
	scan.Buffer(make([]byte, 64<<10), 16<<20)
	session, started, boot, state := false, false, false, false
	turn, body := "", ""
	var source []byte
	var developer [][]byte
	sourceStart, sourceEnd, last := start, end, start
	modern := false
	for scan.Scan() {
		raw := scan.Bytes()
		if !skillsNativeUniqueJSON(raw) || !skillsCreationHeaderKeys(raw, "type", "timestamp", "ordinal", "payload") {
			return nil, modern, false
		}
		var e struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(raw, &e) != nil {
			return nil, modern, false
		}
		at, err := time.Parse(time.RFC3339Nano, e.Timestamp)
		if err != nil || at.After(end) {
			return nil, modern, false
		}
		if e.Type == "session_meta" {
			var m struct {
				ID         string `json:"id"`
				Cwd        string `json:"cwd"`
				Source     string `json:"source"`
				Originator string `json:"originator"`
				Version    string `json:"cli_version"`
			}
			if !skillsCreationHeaderKeys(e.Payload, "id", "cwd", "source", "originator", "cli_version") || json.Unmarshal(e.Payload, &m) != nil || session || m.ID != str(job["thread_id"]) || !same(m.Cwd, str(job["cwd"])) {
				return nil, modern, false
			}
			session = true
			if m.Source != "exec" || m.Originator != "codex_exec" {
				return nil, true, false
			}
			modern = m.Version != "0.163.0"
			if m.Version != "0.165.0" && m.Version != "0.163.0" {
				return nil, true, false
			}
			if at.Before(sourceStart) || str(job["resume_session"]) != "" {
				// A real continuation keeps the first runtime-owned source in
				// its original generation. Prove that generation from the
				// normal same-task history; the current task/final window is
				// independently enforced by skillsNativeProjectionBound.
				if origin == nil {
					return nil, modern, false
				}
				context, valid := origin()
				if !valid || !context.Start.Before(start) || len(context.Windows) == 0 {
					return nil, modern, false
				}
				first := context.Windows[len(context.Windows)-1]
				if !first.Start.Equal(context.Start) || at.Before(first.Start) || at.After(first.End) {
					return nil, modern, false
				}
				sourceStart, sourceEnd, last = first.Start, first.End, first.Start
			}
			if at.Before(last) {
				return nil, true, false
			}
			last = at
			// The old bootstrap digest contract is retained. A resumed old
			// session must nevertheless prove its registered origin above;
			// legacy input cannot bypass the historical-input checks.
			if !modern {
				return nil, false, true
			}
			continue
		}
		if at.Before(sourceStart) || at.Before(last) || at.After(sourceEnd) {
			return nil, modern, false
		}
		last = at
		if !session {
			return nil, modern, false
		}
		switch e.Type {
		case "event_msg":
			var v struct {
				Type string `json:"type"`
				Turn string `json:"turn_id"`
			}
			if !skillsCreationHeaderKeys(e.Payload, "type", "turn_id") || json.Unmarshal(e.Payload, &v) != nil {
				return nil, true, false
			}
			if v.Type == "task_started" {
				if started || !skillsUUID.MatchString(v.Turn) {
					return nil, true, false
				}
				started = true
				turn = v.Turn
			}
			if v.Type == "user_message" {
				return nil, true, false
			}
		case "response_item":
			var v struct {
				Type    string          `json:"type"`
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			}
			if !skillsCreationHeaderKeys(e.Payload, "type", "role", "content") || json.Unmarshal(e.Payload, &v) != nil {
				return nil, true, false
			}
			if v.Type == "message" && v.Role != "user" {
				// Retain the initial rendering only until the independent
				// runtime checkpoint can prove every block's content below.
				if v.Role != "developer" || boot || state || !started {
					return nil, true, false
				}
				developer = append(developer, append([]byte{}, raw...))
				continue
			}
			if v.Role != "user" {
				if v.Role != "developer" || v.Type != "additional_tools" || boot || state || !started {
					return nil, true, false
				}
				developer = append(developer, append([]byte{}, raw...))
				continue
			}
			var blocks []json.RawMessage
			if v.Type != "message" || !started || boot || json.Unmarshal(v.Content, &blocks) != nil || len(blocks) < 1 || len(blocks) > 2 {
				return nil, true, false
			}
			var text strings.Builder
			for _, rawBlock := range blocks {
				var b struct {
					Type string `json:"type"`
					Text string `json:"text"`
				}
				if !skillsStrict(rawBlock, &b) || b.Type != "input_text" {
					return nil, true, false
				}
				text.WriteString(b.Text)
			}
			var valid bool
			body, valid = skillsBootstrapBodyWithSandbox(text.String(), str(job["cwd"]), str(job["sandbox"]))
			if !valid {
				return nil, true, false
			}
			boot = true
		case "world_state":
			var v struct {
				Full  bool            `json:"full"`
				State json.RawMessage `json:"state"`
			}
			var st struct {
				Agents json.RawMessage `json:"agents_md"`
			}
			var a struct {
				Directory string `json:"directory"`
				Text      string `json:"text"`
			}
			if !started || !boot || state || !skillsCreationHeaderKeys(e.Payload, "full", "state") || json.Unmarshal(e.Payload, &v) != nil || !v.Full || !skillsCreationHeaderKeys(v.State, "agents_md") || json.Unmarshal(v.State, &st) != nil || !skillsStrict(st.Agents, &a) || !same(a.Directory, str(job["cwd"])) || a.Text != body || !skillsBootstrapProjectMatches(body, project) {
				return nil, true, false
			}
			source = []byte(a.Text)
			if !skillsRuntimeEnvelopeMatches(developer, v.State, str(job["model"])) {
				return nil, true, false
			}
			if len(envelopes) == 1 {
				for _, raw := range developer {
					envelopes[0][hashBytes(raw)] = true
				}
			}
			state = true
		case "turn_context":
			var v struct {
				Turn   string `json:"turn_id"`
				Cwd    string `json:"cwd"`
				Model  string `json:"model"`
				Effort string `json:"effort"`
			}
			if !state || !boot || !skillsCreationHeaderKeys(e.Payload, "turn_id", "cwd", "model", "effort") || json.Unmarshal(e.Payload, &v) != nil || v.Turn != turn || !same(v.Cwd, str(job["cwd"])) || skillsModel(v.Model) != skillsModel(str(job["model"])) || v.Effort != str(job["effort"]) {
				return nil, true, false
			}
			return source, true, true
		}
	}
	return nil, modern, false
}

func skillsBootstrapProjectMatches(body string, project []byte) bool {
	const separator = "\n\n--- project-doc ---\n\n"
	at := strings.LastIndex(body, separator)
	if at < 1 {
		return false
	}
	actual, ok := skillsBootstrapDocument([]byte(body[at+len(separator):]))
	expected, valid := skillsBootstrapDocument(project)
	return ok && valid && actual != "" && actual == expected
}

func skillsBootstrapDocument(b []byte) (string, bool) {
	b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
	if len(b) == 0 || len(b) > 256<<10 || !utf8.Valid(b) || bytes.ContainsRune(b, 0) {
		return "", false
	}
	// Codex assembles trimmed documents. Windows checkout line endings and
	// the UTF-8 file marker are representation differences, not instructions.
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\r\n", "\n")), true
}

func skillsAssembleBootstrapSource(global, project []byte) ([]byte, bool) {
	g, ok := skillsBootstrapDocument(global)
	p, valid := skillsBootstrapDocument(project)
	if !ok || !valid || g == "" || p == "" {
		return nil, false
	}
	return []byte(g + "\n\n--- project-doc ---\n\n" + p), true
}

func skillsBootstrapSourceMatches(body string, source []byte) bool {
	return len(source) > 0 && body == string(source)
}
