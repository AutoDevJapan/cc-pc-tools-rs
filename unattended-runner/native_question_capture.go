package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
)

func nativeQuestionImageGuard(ctx context.Context, path string, produced bool) error {
	raw, err := runCommand(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", path)
	if err != nil || len(raw) > 64<<10 {
		return errConversationMedia
	}
	return nativeQuestionGuardVerdict([]byte(raw), produced)
}

func nativeQuestionGuardVerdict(raw []byte, produced bool) error {
	var p struct {
		Allowed    bool     `json:"access_allowed"`
		Client     bool     `json:"client_resource"`
		Unverified bool     `json:"classification_unverified"`
		Basis      string   `json:"classification_basis"`
		Status     string   `json:"protection_status"`
		Categories []string `json:"protection_categories"`
	}
	if historyStrictJSON(raw, &map[string]any{}) != nil || json.Unmarshal(raw, &p) != nil {
		return errConversationMedia
	}
	// Explicit protection and uncertain metadata never qualify. The only
	// attributable-location gap admitted by this purpose has independently
	// verified native bytes/current self producer before this function runs.
	if p.Allowed && !p.Client && !p.Unverified && p.Status == "clear" {
		return nil
	}
	if produced && !p.Allowed && p.Client && !p.Unverified && p.Status == "protected" && p.Basis == "owner_rule_unattributed_location" && len(p.Categories) == 1 && p.Categories[0] == "client_project" {
		return nil
	}
	return errConversationMedia
}

func nativeQuestionCapture(e nativeQuestionEvidence, d nativeQuestionDeps) error {
	if e.Producer.Line <= 1 || e.Producer.Line >= e.Copy.Line || e.Copy.Line >= e.CopyResult.Line || e.CopyResult.Line >= e.Question.Line || !shaPattern.MatchString(e.Producer.SHA) || !shaPattern.MatchString(e.Copy.SHA) || !shaPattern.MatchString(e.CopyResult.SHA) {
		return errConversationMedia
	}
	records, err := d.Records(e.Rollout, []nativeQuestionEvent{e.Producer, e.Copy, e.CopyResult})
	if err != nil || len(records) != 3 {
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
	command := production.Payload.Item.Command
	if len(command) != 3 || command[1] != "-Command" || !strings.HasSuffix(strings.ToLower(command[0]), "pwsh.exe") {
		return errConversationMedia
	}
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(command[2], "\r\n", "\n")), "\n")
	if len(lines) != 4 || !regexp.MustCompile(`^\$env:E2E_PORT='[0-9]{4,5}'$`).MatchString(lines[0]) || lines[1] != "$env:PLAYWRIGHT_BROWSERS_PATH='D:/CC/ToolCache/ms-playwright'" || !regexp.MustCompile(`^node node_modules/@playwright/test/cli\.js test --project=chromium --workers=1 --retries=0 --reporter=line e2e/live-3d\.spec\.ts \*> '[^']+\.log'$`).MatchString(lines[2]) || lines[3] != "exit $LASTEXITCODE" {
		return errConversationMedia
	}
	var copied nativeQuestionRecord
	if json.Unmarshal(records[1], &copied) != nil || copied.Type != "response_item" || copied.Payload.Type != "custom_tool_call" || copied.Payload.Name != "exec" {
		return errConversationMedia
	}
	// Parse only direct native commands; pasted examples inside another string
	// do not constitute the Copy-Item that actually ran.
	commands, err := nativeQuestionTokenCommands(nativeQuestionMetadata(copied.Payload.Input))
	if err != nil {
		return errConversationMedia
	}
	code := strings.Join(commands, "\n")
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
		if !nativeQuestionCopySuffix(commands, copyLine, e) {
			return errConversationMedia
		}
	}
	if sourceCount != 1 {
		return errConversationMedia
	}
	var result nativeQuestionRecord
	if json.Unmarshal(records[2], &result) != nil || result.Type != "response_item" || result.Payload.Type != "custom_tool_call_output" || result.Payload.Call != copied.Payload.Call {
		return errConversationMedia
	}
	var blocks []struct{ Type, Text string }
	if json.Unmarshal(result.Payload.Output, &blocks) != nil {
		return errConversationMedia
	}
	verified := 0
	for _, block := range blocks {
		if block.Type != "input_text" {
			continue
		}
		var commandResult struct {
			Exit   *int   `json:"exit_code"`
			Output string `json:"output"`
		}
		if json.Unmarshal([]byte(block.Text), &commandResult) != nil || commandResult.Exit == nil || *commandResult.Exit != 0 {
			continue
		}
		s := commandResult.Output
		if strings.HasPrefix(s, "Using Node ") {
			if n := strings.Index(s, "\n"); n >= 0 {
				s = s[n+1:]
			}
		}
		var hashes []struct {
			Path string
			Hash string
		}
		if json.Unmarshal([]byte(s), &hashes) != nil {
			continue
		}
		for _, h := range hashes {
			if conversationMediaSamePath(filepath.Clean(filepath.FromSlash(h.Path)), filepath.FromSlash(e.Images[1].Path)) && strings.EqualFold(h.Hash, e.Images[1].SHA) {
				verified++
			}
		}
	}
	if verified != 1 {
		return errConversationMedia
	}
	return nil
}

// After the retained image is copied, only other screenshots to distinct
// destinations and a hash-only readback may follow. Another command or write
// could otherwise replace the very image whose copy lineage is being proved.
func nativeQuestionCopySuffix(commands []string, copyLine string, e nativeQuestionEvidence) bool {
	found := false
	for _, command := range commands {
		if found {
			return false
		}
		lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(command, "\\", "/"), "\r\n", "\n"), "\n")
		for _, line := range lines {
			if !found {
				if line == copyLine {
					found = true
				} else {
					// A command before the selected copy must not already operate
					// on its producer or retained attachment through a literal path.
					// Those exact paths belong only to the selected copy/readback.
					for _, image := range e.Images {
						for _, path := range []string{image.Path, image.Producer} {
							if path == "" {
								continue
							}
							rel, err := filepath.Rel(e.Worktree, filepath.FromSlash(path))
							if err != nil || strings.Contains(strings.ToLower(line), strings.ToLower(filepath.ToSlash(path))) || strings.Contains(strings.ToLower(line), strings.ToLower(filepath.ToSlash(rel))) {
								return false
							}
						}
					}
				}
				continue
			}
			if strings.TrimSpace(line) == "" {
				continue
			}
			m := regexp.MustCompile(`^Copy-Item -LiteralPath '([^']+\.png)' -Destination '([^']+\.png)'$`).FindStringSubmatch(line)
			if len(m) == 3 {
				resolve := func(p string) string {
					p = filepath.FromSlash(p)
					if !filepath.IsAbs(p) {
						p = filepath.Join(e.Worktree, p)
					}
					return filepath.Clean(p)
				}
				if !generatedWithin(filepath.Join(e.Worktree, "test-results"), resolve(m[1])) {
					return false
				}
				for _, im := range e.Images {
					if conversationMediaSamePath(resolve(m[2]), filepath.FromSlash(im.Path)) || (im.Producer != "" && conversationMediaSamePath(resolve(m[2]), filepath.FromSlash(im.Producer))) {
						return false
					}
				}
				continue
			}
			if !regexp.MustCompile(`^Get-FileHash -LiteralPath '[^']+'(?:,'[^']+')* -Algorithm SHA256 \| Select-Object Path,Hash \| ConvertTo-Json$`).MatchString(line) {
				return false
			}
		}
	}
	return found
}
