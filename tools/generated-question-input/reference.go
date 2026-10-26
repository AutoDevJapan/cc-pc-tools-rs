package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
)

type inputReference struct {
	ThreadID, TurnID, ItemID, InputSHA256, ImageName, ImageSHA256 string
}

type inputReport struct {
	Domain         string `json:"domain"`
	SourceMatched  bool   `json:"source_matched"`
	Stage          string `json:"stage"`
	Code           string `json:"code"`
	AssetQualified bool   `json:"asset_qualified"`
}

type inputRPC interface {
	Call(context.Context, string, any) (json.RawMessage, error)
	Close() error
}

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var imageNamePattern = regexp.MustCompile(`^exec-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.png$`)
var mentionedImagePattern = regexp.MustCompile(`(?i)\bexec-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.png\b`)
var mentionedDigestPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{64}\b`)
var todoPrefixPattern = regexp.MustCompile(`^(?:担当)?ToDo(?:番号)?\s*[0-9]+(?:\s|[:：]|$)`)

func (r inputReference) valid() bool {
	return uuidPattern.MatchString(r.ThreadID) && uuidPattern.MatchString(r.TurnID) && uuidPattern.MatchString(r.ItemID) &&
		digestPattern.MatchString(r.InputSHA256) && (r.ImageName != "" || r.ImageSHA256 != "") &&
		(r.ImageName == "" || imageNamePattern.MatchString(r.ImageName)) &&
		(r.ImageSHA256 == "" || digestPattern.MatchString(r.ImageSHA256))
}

func failedInput(code string) inputReport {
	return inputReport{Domain: "generated-question-input-reference-v1", Stage: "owner_input", Code: code}
}

type itemEntry struct {
	TurnID string `json:"turnId"`
	Item   struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"item"`
}

func sourceItem(raw json.RawMessage, ref inputReference) (string, string) {
	var page struct {
		Data []itemEntry `json:"data"`
	}
	if json.Unmarshal(raw, &page) != nil || len(page.Data) != 1 {
		return "", "input_shape_rejected"
	}
	row := page.Data[0]
	if row.TurnID != ref.TurnID || row.Item.ID != ref.ItemID || row.Item.Type != "userMessage" {
		return "", "first_input_mismatch"
	}
	if len(row.Item.Content) != 1 || row.Item.Content[0].Type != "text" || len(row.Item.Content[0].Text) == 0 || len(row.Item.Content[0].Text) > 256<<10 {
		return "", "input_shape_rejected"
	}
	return row.Item.Content[0].Text, ""
}

func automatedInput(text string) bool {
	text = strings.TrimSpace(text)
	for _, prefix := range []string{"この PC の共有スケジューラから割り当てられた作業です。", "# AGENTS.md instructions for ", "<environment_context>", "<subagent_notification>"} {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return todoPrefixPattern.MatchString(text)
}

func uniqueMentions(pattern *regexp.Regexp, text string) map[string]bool {
	values := map[string]bool{}
	for _, value := range pattern.FindAllString(text, -1) {
		values[strings.ToLower(value)] = true
	}
	return values
}

func imageMentionCode(text string, ref inputReference) string {
	names := uniqueMentions(mentionedImagePattern, text)
	hashes := uniqueMentions(mentionedDigestPattern, text)
	if len(names) > 1 || len(hashes) > 1 {
		return "ambiguous_image_reference"
	}
	if len(names) != 0 && (ref.ImageName == "" || !names[ref.ImageName]) || len(hashes) != 0 && (ref.ImageSHA256 == "" || !hashes[ref.ImageSHA256]) {
		return "image_reference_mismatch"
	}
	if len(names) == 0 && len(hashes) == 0 {
		return "image_reference_missing"
	}
	return ""
}

func inspectInputReference(ctx context.Context, rpc inputRPC, ref inputReference, now int64) inputReport {
	if !ref.valid() {
		return failedInput("invalid_reference")
	}
	raw, err := rpc.Call(ctx, "thread/read", map[string]any{"threadId": ref.ThreadID, "includeTurns": false})
	if err != nil {
		return failedInput("source_unavailable")
	}
	var thread struct {
		Thread struct {
			ID     string            `json:"id"`
			Source json.RawMessage   `json:"source"`
			Turns  []json.RawMessage `json:"turns"`
		} `json:"thread"`
	}
	if json.Unmarshal(raw, &thread) != nil || thread.Thread.ID != ref.ThreadID || len(thread.Thread.Turns) != 0 {
		return failedInput("thread_metadata_mismatch")
	}
	var kind string
	if json.Unmarshal(thread.Thread.Source, &kind) != nil || (kind != "cli" && kind != "vscode") {
		return failedInput("source_thread_kind_rejected")
	}
	found, cursor := false, ""
	seenCursors := map[string]bool{}
	for pageNumber := 0; pageNumber < 8; pageNumber++ {
		params := map[string]any{"threadId": ref.ThreadID, "itemsView": "notLoaded", "limit": 20, "sortDirection": "desc"}
		if cursor != "" {
			params["cursor"] = cursor
		}
		raw, err = rpc.Call(ctx, "thread/turns/list", params)
		if err != nil {
			return failedInput("source_unavailable")
		}
		var page struct {
			Data []struct {
				ID         string            `json:"id"`
				RootTurnID string            `json:"rootTurnId"`
				ItemsView  string            `json:"itemsView"`
				StartedAt  *int64            `json:"startedAt"`
				Items      []json.RawMessage `json:"items"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if json.Unmarshal(raw, &page) != nil || len(page.Data) > 20 {
			return failedInput("turn_metadata_mismatch")
		}
		for _, turn := range page.Data {
			if turn.ItemsView != "notLoaded" || len(turn.Items) != 0 {
				return failedInput("turn_metadata_mismatch")
			}
			if turn.ID != ref.TurnID {
				continue
			}
			if found || turn.RootTurnID != ref.TurnID || turn.StartedAt == nil || *turn.StartedAt <= 0 || *turn.StartedAt > now {
				return failedInput("root_turn_mismatch")
			}
			found = true
		}
		if found || page.NextCursor == nil || *page.NextCursor == "" {
			break
		}
		cursor = *page.NextCursor
		if seenCursors[cursor] {
			return failedInput("turn_metadata_mismatch")
		}
		seenCursors[cursor] = true
	}
	if !found {
		return failedInput("root_turn_not_found")
	}
	raw, err = rpc.Call(ctx, "thread/items/list", map[string]any{"threadId": ref.ThreadID, "turnId": ref.TurnID, "limit": 1, "sortDirection": "asc"})
	if err != nil {
		return failedInput("source_unavailable")
	}
	first, code := sourceItem(raw, ref)
	if code != "" {
		return failedInput(code)
	}
	if automatedInput(first) {
		return failedInput("automation_input_rejected")
	}
	raw, err = rpc.Call(ctx, "thread/items/read", map[string]any{"threadId": ref.ThreadID, "turnId": ref.TurnID, "itemIds": []string{ref.ItemID}})
	if err != nil {
		return failedInput("source_unavailable")
	}
	second, code := sourceItem(raw, ref)
	if code != "" {
		return failedInput(code)
	}
	if first != second {
		return failedInput("input_version_changed")
	}
	digest := sha256.Sum256([]byte(second))
	if hex.EncodeToString(digest[:]) != ref.InputSHA256 {
		return failedInput("input_version_mismatch")
	}
	if code := imageMentionCode(second, ref); code != "" {
		return failedInput(code)
	}
	return inputReport{Domain: "generated-question-input-reference-v1", Stage: "complete", Code: "source_reference_matched", SourceMatched: true}
}
