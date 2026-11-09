package main

// Native owner provenance is independent of generated-media provenance. A user
// role alone also occurs on internal messages and is never sufficient here.
import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type taskQuestionTurn struct {
	Start   nativeQuestionEvent `json:"start"`
	Context nativeQuestionEvent `json:"context"`
	Message nativeQuestionEvent `json:"message"`
	Receipt nativeQuestionEvent `json:"receipt"`
}

type taskQuestionNativeRecord struct {
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Payload   struct {
		Type, Role, ID, Source, Originator string
		ThreadSource                       string `json:"thread_source"`
		Creator                            string `json:"creator_user_id"`
		Account                            string `json:"creator_account_id"`
		Cwd                                string `json:"cwd"`
		Thread                             string `json:"thread_id"`
		Turn                               string `json:"turn_id"`
		Root                               string `json:"root_turn_id"`
		Started                            int64  `json:"started_at_ms"`
		Completed                          int64  `json:"completed_at_ms"`
		Attribution                        struct {
			Turn    string          `json:"turn_id"`
			Root    string          `json:"root_turn_id"`
			Trigger string          `json:"turn_trigger"`
			Parent  json.RawMessage `json:"parent_turn_id"`
			Agent   json.RawMessage `json:"initiating_agent_path"`
		} `json:"turn_attribution"`
		Metadata struct {
			Turn  string   `json:"turn_id"`
			Kinds []string `json:"content_item_kinds"`
			Items []struct {
				Provenance struct{ Type string } `json:"provenance"`
			} `json:"content_item_metadata"`
		} `json:"internal_chat_message_metadata_passthrough"`
		Content []struct{ Type, Text string }
		Item    struct {
			Type, ID, Status, Source string
			Client                   string `json:"client_id"`
			Content                  []struct{ Type, Text string }
			Command                  []string
			Cwd                      string
			Output                   string `json:"aggregated_output"`
			Exit                     *int   `json:"exit_code"`
		} `json:"item"`
	} `json:"payload"`
}

func taskQuestionNull(b json.RawMessage) bool { return string(b) == "null" }

func taskQuestionOwnerTurn(records []taskQuestionNativeRecord, session, root string) (string, string, bool) {
	if len(records) != 4 {
		return "", "", false
	}
	s, c, m, r := records[0], records[1], records[2], records[3]
	a := s.Payload.Attribution
	if s.Type != "event_msg" || s.Payload.Type != "task_started" || !generatedUUID.MatchString(s.Payload.Turn) || s.Payload.Root != s.Payload.Turn || a.Trigger != "composer" || !taskQuestionNull(a.Parent) || !taskQuestionNull(a.Agent) || a.Turn != s.Payload.Turn || a.Root != s.Payload.Turn {
		return "", "", false
	}
	turn := s.Payload.Turn
	if c.Type != "turn_context" || c.Payload.Turn != turn || c.Payload.Root != turn || !conversationMediaSamePath(filepath.FromSlash(c.Payload.Cwd), filepath.FromSlash(root)) || m.Type != "response_item" || m.Payload.Type != "message" || m.Payload.Role != "user" || !strings.HasPrefix(m.Payload.ID, "msg_") || m.Payload.Metadata.Turn != turn || len(m.Payload.Content) != 1 || m.Payload.Content[0].Type != "input_text" || len(m.Payload.Metadata.Kinds) != 1 || m.Payload.Metadata.Kinds[0] != "user.text" || len(m.Payload.Metadata.Items) != 1 || m.Payload.Metadata.Items[0].Provenance.Type != "user" {
		return "", "", false
	}
	text := m.Payload.Content[0].Text
	for _, internal := range []string{"<subagent_notification", "<codex_internal_context", "# AGENTS.md instructions", "<environment_context>"} {
		if strings.Contains(text, internal) {
			return "", "", false
		}
	}
	if strings.TrimSpace(text) == "" || r.Type != "event_msg" || r.Payload.Type != "item_completed" || r.Payload.Thread != session || r.Payload.Turn != turn || r.Payload.Item.Type != "UserMessage" || !generatedUUID.MatchString(r.Payload.Item.Client) || len(r.Payload.Item.Content) != 1 || r.Payload.Item.Content[0].Type != "text" || r.Payload.Item.Content[0].Text != text {
		return "", "", false
	}
	return text, turn, true
}

func taskQuestionNative(e taskQuestionEvidence, state taskQuestionState, records func(string, []generatedEventBound) ([]json.RawMessage, error)) error {
	bad := func() error { return generatedFailure("task_source_lineage", "native_task_lineage_unverified") }
	bounds := []generatedEventBound{{Line: 1, SHA: e.SessionSHA}}
	for _, turn := range []taskQuestionTurn{e.RepositoryRequest, e.TaskRequest, e.Continuation} {
		for _, event := range []nativeQuestionEvent{turn.Start, turn.Context, turn.Message, turn.Receipt} {
			bounds = append(bounds, generatedEventBound{Line: event.Line, SHA: event.SHA})
		}
	}
	// Creation belongs between the task's actual owner turn and continuation.
	bounds = append(bounds[:9], append([]generatedEventBound{{Line: e.Creation.Line, SHA: e.Creation.SHA}}, bounds[9:]...)...)
	for i, bound := range bounds {
		if !shaPattern.MatchString(bound.SHA) || bound.Line < 1 || (i > 0 && bound.Line <= bounds[i-1].Line) {
			return bad()
		}
	}
	raw, err := records(e.Rollout, bounds)
	if err != nil || len(raw) != len(bounds) {
		return bad()
	}
	r := make([]taskQuestionNativeRecord, len(raw))
	for i, b := range raw {
		if bytesHash(b) != bounds[i].SHA || json.Unmarshal(b, &r[i]) != nil || r[i].Timestamp.IsZero() || r[i].Timestamp.After(e.Created) || (i > 0 && r[i].Timestamp.Before(r[i-1].Timestamp)) {
			return bad()
		}
		// Native envelopes have many unrelated fields, but duplicate keys are
		// still ambiguous. Validate those without rejecting native extensions.
		var v any
		if historyStrictJSON(b, &v) != nil {
			return bad()
		}
	}
	meta := r[0]
	if meta.Type != "session_meta" || meta.Payload.ID != e.Session || meta.Payload.ThreadSource != "user" || meta.Payload.Source != "vscode" || meta.Payload.Originator != "Codex Desktop" || meta.Payload.Creator == "" || meta.Payload.Account == "" || !conversationMediaSamePath(filepath.FromSlash(meta.Payload.Cwd), filepath.FromSlash(e.Root)) {
		return bad()
	}
	repos, _, ok := taskQuestionOwnerTurn(r[1:5], e.Session, e.Root)
	if !ok || !taskQuestionRepositoryURL(repos, e.Repository.FullName) {
		return bad()
	}
	_, turn, ok := taskQuestionOwnerTurn(r[5:9], e.Session, e.Root)
	if !ok {
		return bad()
	}
	if _, _, ok = taskQuestionOwnerTurn(r[10:14], e.Session, e.Root); !ok {
		return bad()
	}
	c := r[9]
	item := c.Payload.Item
	if c.Type != "event_msg" || c.Payload.Type != "item_completed" || c.Payload.Thread != e.Session || c.Payload.Turn != turn || item.Type != "CommandExecution" || item.Source != "unified_exec_startup" || item.Status != "completed" || item.Exit == nil || *item.Exit != 0 || item.Output != "added "+fmtUint(e.Scope.Todo)+"\n" || len(item.Command) != 3 || item.Command[1] != "-Command" || !strings.EqualFold(filepath.Base(filepath.FromSlash(item.Command[0])), "pwsh.exe") {
		return bad()
	}
	cwd, err := url.Parse(item.Cwd)
	if err != nil || cwd.Scheme != "file" || cwd.Host != "" || !conversationMediaSamePath(filepath.FromSlash(strings.TrimPrefix(cwd.Path, "/")), filepath.FromSlash(e.Root)) {
		return bad()
	}
	// This is a native command receipt, not text scraped out of tool output.
	// Accept the literal add operation and an optional read-only scheduler prefix.
	pattern := regexp.MustCompile(`^(?:& 'D:/CC/Tools/scheduler/scheduler.exe' status; )?& 'D:/CC/Tools/todo/todo.exe' add ([A-Za-z0-9_-]+) '([^'\r\n]+)' --note '[^'\r\n]*'$`)
	m := pattern.FindStringSubmatch(item.Command[2])
	createdMS := state.Created * 1000
	if len(m) != 3 || m[1] != state.Project || m[2] != state.Title || createdMS < float64(c.Payload.Started) || createdMS > float64(c.Payload.Completed) || c.Payload.Started < r[8].Timestamp.UnixMilli() || c.Payload.Completed != c.Timestamp.UnixMilli() || c.Payload.Completed < c.Payload.Started {
		return bad()
	}
	return nil
}

func taskQuestionRepositoryURL(text, fullName string) bool {
	pattern := regexp.MustCompile(`https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+`)
	for _, found := range pattern.FindAllString(text, -1) {
		if found == "https://github.com/"+fullName {
			return true
		}
	}
	return false
}
