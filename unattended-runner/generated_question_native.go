package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The actual user assignment in the generation session binds the source task.
// A caller cannot relabel an image by editing its JSON generation summary.
func validateGeneratedAssignment(s generatedQuestionSpec) error {
	records, err := s.Records(s.Rollout, []generatedEventBound{{Line: 1, SHA: s.SessionSHA}, s.Assignment})
	if err != nil || len(records) != 2 {
		return errConversationMedia
	}
	var meta struct {
		Type    string
		Payload struct{ ID string }
	}
	var assignment struct {
		Type    string
		Payload struct {
			Type, Role string
			Content    []struct{ Type, Text string }
		}
	}
	if json.Unmarshal(records[0], &meta) != nil || meta.Type != "session_meta" || meta.Payload.ID != s.Session || json.Unmarshal(records[1], &assignment) != nil || assignment.Type != "response_item" || assignment.Payload.Type != "message" || assignment.Payload.Role != "user" {
		return errConversationMedia
	}
	pattern := regexp.MustCompile(`^(?:担当)?ToDo([0-9]+)(?:[^0-9]|$)`)
	matched := 0
	for _, content := range assignment.Payload.Content {
		if content.Type != "input_text" {
			continue
		}
		m := pattern.FindStringSubmatch(strings.TrimSpace(content.Text))
		if len(m) != 2 {
			continue
		}
		todo, e := strconv.ParseUint(m[1], 10, 64)
		if e != nil || todo != s.Scope.Todo {
			return errConversationMedia
		}
		matched++
	}
	if matched != 1 {
		return errConversationMedia
	}
	return generatedFirstAssignment(s)
}

// A later user message that happens to mention another task is not a new
// origin for images from this dedicated generation session. Only its first
// substantive user assignment can supply the source ToDo.
func generatedFirstAssignment(s generatedQuestionSpec) error {
	physical, before, err := conversationMediaPhysical(filepath.FromSlash(s.Rollout))
	if err != nil {
		return errConversationMedia
	}
	f, err := os.Open(physical)
	if err != nil {
		return errConversationMedia
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return errConversationMedia
	}
	scanner := bufio.NewScanner(io.LimitReader(f, generatedNativeScanLimit))
	scanner.Buffer(make([]byte, 65536), generatedNativeLineLimit)
	line, found := 0, false
	for scanner.Scan() {
		line++
		if line > s.Assignment.Line {
			break
		}
		var message struct {
			Type    string
			Payload struct {
				Type, Role string
				Content    []struct{ Type, Text string }
			}
		}
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			return errConversationMedia
		}
		if message.Type != "response_item" || message.Payload.Type != "message" || message.Payload.Role != "user" {
			continue
		}
		for _, content := range message.Payload.Content {
			text := strings.TrimSpace(content.Text)
			if content.Type != "input_text" || text == "" || strings.HasPrefix(text, "# AGENTS.md instructions for ") || strings.HasPrefix(text, "<environment_context>") {
				continue
			}
			if line != s.Assignment.Line || bytesHash(scanner.Bytes()) != s.Assignment.SHA || found {
				return errConversationMedia
			}
			found = true
		}
	}
	_, named, pathErr := conversationMediaPhysical(physical)
	if scanner.Err() != nil || !found || line < s.Assignment.Line || pathErr != nil || !os.SameFile(opened, named) {
		return errConversationMedia
	}
	return nil
}

// Native tool results, rather than caller-authored receipts, supply the image
// bytes. Only selected records are decoded; their data URLs are never logged.
func validateGeneratedQuestionNative(records []json.RawMessage, s generatedQuestionSpec) error {
	type native struct {
		Timestamp time.Time `json:"timestamp"`
		Type      string    `json:"type"`
		Payload   struct {
			Type, Name, Input string
			Call              string `json:"call_id"`
			Arguments         string `json:"arguments"`
			Output            json.RawMessage
		} `json:"payload"`
	}
	if len(records) != 2 && len(records) != 4 {
		return errConversationMedia
	}
	r := make([]native, len(records))
	for i, raw := range records {
		if json.Unmarshal(raw, &r[i]) != nil || r[i].Type != "response_item" || r[i].Timestamp.IsZero() || r[i].Timestamp.Before(s.Created) || r[i].Timestamp.After(s.Now()) || (i > 0 && r[i].Timestamp.Before(r[i-1].Timestamp)) {
			return errConversationMedia
		}
	}
	if !r[0].Timestamp.Equal(s.Created) || r[0].Payload.Type != "custom_tool_call" || r[0].Payload.Name != "exec" || r[0].Payload.Call != r[1].Payload.Call || r[1].Payload.Type != "custom_tool_call_output" {
		return errConversationMedia
	}
	code := r[0].Payload.Input
	if validateGeneratedScript(code, len(s.Images)) != nil {
		return errConversationMedia
	}
	if len(records) == 4 {
		var pending string
		var wait struct {
			Cell string `json:"cell_id"`
		}
		if json.Unmarshal(r[1].Payload.Output, &pending) != nil || !strings.HasPrefix(pending, "Script running with cell ID "+s.Cell+"\n") || r[2].Payload.Type != "function_call" || r[2].Payload.Name != "wait" || json.Unmarshal([]byte(r[2].Payload.Arguments), &wait) != nil || wait.Cell != s.Cell || r[3].Payload.Type != "function_call_output" || r[2].Payload.Call != r[3].Payload.Call {
			return errConversationMedia
		}
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
		URL  string `json:"image_url"`
	}
	if json.Unmarshal(r[len(r)-1].Payload.Output, &blocks) != nil {
		return errConversationMedia
	}
	matched := map[string]bool{}
	count := 0
	for i, block := range blocks {
		if block.Type != "input_image" {
			continue
		}
		count++
		if i+1 >= len(blocks) || blocks[i+1].Type != "input_text" || !strings.HasPrefix(block.URL, "data:image/png;base64,") {
			return errConversationMedia
		}
		data, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(block.URL, "data:image/png;base64,"))
		if err != nil || int64(len(data)) > conversationMediaLimit {
			return errConversationMedia
		}
		found := false
		for _, image := range s.Images {
			dir := strings.ReplaceAll(filepath.Dir(filepath.FromSlash(image.Path)), `\`, "/")
			path := strings.ReplaceAll(filepath.FromSlash(image.Path), `\`, "/")
			hint := strings.ReplaceAll(blocks[i+1].Text, `\`, "/")
			prefix := fmt.Sprintf("Generated images are saved to %s as %s by default.\n", dir, path)
			if strings.HasPrefix(hint, prefix) {
				if matched[path] || bytesHash(data) != image.SHA256 || int64(len(data)) != image.Size {
					return errConversationMedia
				}
				matched[path] = true
				found = true
			}
		}
		if !found {
			return errConversationMedia
		}
	}
	if count != len(s.Images) || len(matched) != len(s.Images) {
		return errConversationMedia
	}
	return nil
}
