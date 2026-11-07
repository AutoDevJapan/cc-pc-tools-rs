package main

import (
	"bufio"
	"encoding/json"
	"io"
	"time"
)

// Match the entire ordered initial model input, including the normally saved
// task, against the independent transport record. This is not a comparison to
// a digest announced by the child or learned from this native snapshot.
func skillsNativeTransportMatches(input io.Reader, thread, cwd string, start, end time.Time, expected skillsTransportInput, turn string) bool {
	if len(expected.Items) == 0 || end.Before(start) || !skillsUUID.MatchString(turn) {
		return false
	}
	scan := bufio.NewScanner(input)
	scan.Buffer(make([]byte, 64<<10), 16<<20)
	session, index := false, 0
	taskStarted, turnContext := false, false
	for scan.Scan() {
		var e struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if !skillsNativeUniqueJSON(scan.Bytes()) || !skillsCreationHeaderKeys(scan.Bytes(), "type", "timestamp", "payload") || json.Unmarshal(scan.Bytes(), &e) != nil {
			return false
		}
		if e.Type == "session_meta" {
			var m struct {
				ID, Cwd string
				Version string `json:"cli_version"`
			}
			at, err := time.Parse(time.RFC3339Nano, e.Timestamp)
			if session || !skillsCreationHeaderKeys(e.Payload, "id", "cwd", "cli_version") || json.Unmarshal(e.Payload, &m) != nil || m.ID != thread || !same(m.Cwd, cwd) || m.Version != "0.165.0" || err != nil || at.Before(start) || at.After(end) {
				return false
			}
			session = true
			continue
		}
		if e.Type != "response_item" && e.Type != "event_msg" && e.Type != "turn_context" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, e.Timestamp)
		if !session || err != nil || at.Before(start) || at.After(end) {
			return false
		}
		if e.Type == "event_msg" || e.Type == "turn_context" {
			var p struct {
				Type string `json:"type"`
				Turn string `json:"turn_id"`
			}
			if !skillsCreationHeaderKeys(e.Payload, "type", "turn_id") || json.Unmarshal(e.Payload, &p) != nil {
				return false
			}
			if e.Type == "turn_context" {
				if !taskStarted || turnContext || p.Turn != turn {
					return false
				}
				turnContext = true
			} else if p.Type == "task_started" {
				if taskStarted || p.Turn != turn {
					return false
				}
				taskStarted = true
			}
			continue
		}
		sha, _, _, _, ok := skillsSourceInputItem(e.Payload)
		if !ok || index >= len(expected.Items) || sha != expected.Items[index] {
			return false
		}
		index++
		if index == len(expected.Items) {
			return taskStarted && turnContext
		}
	}
	return false
}
