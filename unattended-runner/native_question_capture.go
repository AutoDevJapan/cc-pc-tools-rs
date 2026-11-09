package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
)

func nativeQuestionImageGuard(ctx context.Context, path string) error {
	raw, err := runCommand(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", path)
	if err != nil || len(raw) > 64<<10 {
		return errConversationMedia
	}
	var p struct {
		Allowed    bool     `json:"access_allowed"`
		Client     bool     `json:"client_resource"`
		Unverified bool     `json:"classification_unverified"`
		Basis      string   `json:"classification_basis"`
		Status     string   `json:"protection_status"`
		Categories []string `json:"protection_categories"`
	}
	if json.Unmarshal([]byte(raw), &p) != nil {
		return errConversationMedia
	}
	// Explicit protection and uncertain metadata never qualify. The only
	// attributable-location gap admitted by this purpose has independently
	// verified native bytes/current self producer before this function runs.
	if p.Allowed && !p.Client && !p.Unverified && p.Status == "clear" {
		return nil
	}
	if !p.Allowed && p.Client && !p.Unverified && p.Status == "protected" && p.Basis == "owner_rule_unattributed_location" && len(p.Categories) == 1 && p.Categories[0] == "client_project" {
		return nil
	}
	return errConversationMedia
}

func nativeQuestionCapture(e nativeQuestionEvidence, d nativeQuestionDeps) error {
	if e.Producer.Line <= 1 || e.Producer.Line >= e.Copy.Line || e.Copy.Line >= e.Question.Line || !shaPattern.MatchString(e.Producer.SHA) || !shaPattern.MatchString(e.Copy.SHA) {
		return errConversationMedia
	}
	records, err := d.Records(e.Rollout, []nativeQuestionEvent{e.Producer, e.Copy})
	if err != nil || len(records) != 2 {
		return errConversationMedia
	}
	var production struct {
		Type    string
		Payload struct {
			Type string
			Item struct {
				Type    string
				Exit    *int     `json:"exit_code"`
				Command []string `json:"command"`
			}
		}
	}
	if json.Unmarshal(records[0], &production) != nil || production.Type != "event_msg" || production.Payload.Type != "item_completed" || production.Payload.Item.Type != "CommandExecution" || production.Payload.Item.Exit == nil || *production.Payload.Item.Exit != 0 {
		return errConversationMedia
	}
	command := strings.Join(production.Payload.Item.Command, "\n")
	if !strings.Contains(command, "node node_modules/@playwright/test/cli.js test ") || !strings.Contains(command, "--project=chromium") || !strings.Contains(command, "--workers=1") || !strings.Contains(command, "--retries=0") || !strings.Contains(command, "e2e/live-3d.spec.ts") {
		return errConversationMedia
	}
	var copied nativeQuestionRecord
	if json.Unmarshal(records[1], &copied) != nil || copied.Type != "response_item" || copied.Payload.Type != "custom_tool_call" || copied.Payload.Name != "exec" {
		return errConversationMedia
	}
	code := strings.ReplaceAll(copied.Payload.Input, "\\\\", "\\")
	if len(e.Images) != 2 || e.Images[0].Producer != "" || e.Images[1].Producer == "" || !generatedWithin(e.Worktree, e.Images[0].Path) {
		return errConversationMedia
	}
	sourceCount := 0
	for _, image := range e.Images {
		if image.Producer == "" {
			continue
		}
		sourceCount++
		p := filepath.FromSlash(image.Producer)
		if !generatedWithin(filepath.Join(e.Worktree, "test-results"), p) || filepath.Base(p) != "live-3d-dark-round-end.png" || !strings.HasPrefix(filepath.Base(filepath.Dir(p)), "live-3d-") {
			return errConversationMedia
		}
		// Bind the original native Copy-Item literal to the exact output path.
		// No new copy is executed and the current producer may have been replaced.
		normalized := strings.ReplaceAll(code, "\\", "/")
		relative, _ := filepath.Rel(e.Worktree, p)
		suffix := strings.ReplaceAll(relative, "\\", "/")
		expected, _ := filepath.Rel(e.Worktree, filepath.FromSlash(image.Path))
		copyLine := "Copy-Item -LiteralPath '" + suffix + "' -Destination '" + strings.ReplaceAll(expected, "\\", "/") + "'"
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(copyLine) + `\r?$`).MatchString(normalized) {
			return errConversationMedia
		}
	}
	if sourceCount != 1 {
		return errConversationMedia
	}
	return nil
}
