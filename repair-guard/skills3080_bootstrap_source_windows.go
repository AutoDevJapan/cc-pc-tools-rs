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

// The runtime-owned launch world_state captures the loaded agents_md version
// independently of its user-message rendering. Its first-turn relationship and
// PUBLIC project blob are proven from the same frozen native snapshot. No
// current instruction file, alternate source path or caller digest is read.
func skillsRuntimeBootstrapProjection(input io.Reader, job object, project []byte, start, end time.Time) ([]byte, bool, bool) {
	scan := bufio.NewScanner(io.LimitReader(input, 4<<20))
	scan.Buffer(make([]byte, 64<<10), 16<<20)
	session, started, boot, state := false, false, false, false
	turn, body := "", ""
	var source []byte
	last := start
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
		if err != nil || at.Before(start) || at.After(end) || at.Before(last) {
			return nil, modern, false
		}
		last = at
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
			if m.Version != "0.165.0" {
				return nil, false, true
			}
			modern = true
			if m.Source != "exec" || m.Originator != "codex_exec" {
				return nil, true, false
			}
			continue
		}
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
			if v.Role != "user" {
				continue
			}
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if v.Type != "message" || !started || boot || json.Unmarshal(v.Content, &blocks) != nil || len(blocks) < 1 || len(blocks) > 2 {
				return nil, true, false
			}
			var text strings.Builder
			for _, b := range blocks {
				if b.Type != "input_text" {
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
