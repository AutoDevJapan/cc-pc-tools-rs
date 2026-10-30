package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

const retainedProcedureJournalFile = "skills-3080-retained-procedure-assessments.json"
const retainedProcedureDomain = "skills3080-retained-procedure-assessment-journal-v1"

var retainedJobIdentifier = regexp.MustCompile(`\b[0-9]{8}-[0-9]{6}-todo-[0-9]+\b`)
var retainedURLStart = regexp.MustCompile(`(?i)https?://`)
var retainedBareHostStart = regexp.MustCompile(`(?i)(?:^|[^a-z0-9_.%~-])((?:www\.)?(?:chatgpt\.com|chat\.openai\.com|claude\.ai|discord(?:app)?\.com|codex\.openai\.com)\.?(?::[0-9]+)?/)`)

func retainedPercentView(text string) string {
	var decoded strings.Builder
	decoded.Grow(len(text))
	for i := 0; i < len(text); i++ {
		if text[i] == '%' && i+2 < len(text) {
			if value, err := strconv.ParseUint(text[i+1:i+3], 16, 8); err == nil {
				decoded.WriteByte(byte(value))
				i += 2
				continue
			}
		}
		decoded.WriteByte(text[i])
	}
	return decoded.String()
}

func retainedConversationURL(text string) bool {
	boundary := func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune("<>\"'`", r) }
	for _, token := range strings.FieldsFunc(text, boundary) {
		forms := []string{token}
		// A malformed escape in another query value must not hide a valid
		// encoded URL. Decode valid triplets once, retaining other bytes.
		if decoded := retainedPercentView(token); decoded != token {
			forms = append(forms, decoded)
		}
		for _, form := range forms {
			for _, part := range strings.FieldsFunc(form, boundary) {
				starts := []int{}
				for _, match := range retainedURLStart.FindAllStringIndex(part, -1) {
					starts = append(starts, match[0])
				}
				for _, match := range retainedBareHostStart.FindAllStringSubmatchIndex(part, -1) {
					starts = append(starts, match[2])
				}
				sort.Ints(starts)
				unique := starts[:0]
				for _, start := range starts {
					if len(unique) == 0 || unique[len(unique)-1] != start {
						unique = append(unique, start)
					}
				}
				starts = unique
				for index, start := range starts {
					end := len(part)
					if index+1 < len(starts) {
						end = starts[index+1]
					}
					// Inspect every URL start, including one embedded in another
					// URL. Nonoverlapping whole-URL matches hide the inner host.
					candidate := strings.TrimRight(part[start:end], ".,;)]}`")
					lower := strings.ToLower(candidate)
					if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
						candidate = "https://" + candidate
					}
					u, err := url.Parse(candidate)
					if err != nil {
						continue
					}
					host := strings.TrimPrefix(strings.TrimSuffix(strings.ToLower(u.Hostname()), "."), "www.")
					p := strings.ToLower(path.Clean(u.Path))
					switch host {
					case "chatgpt.com", "chat.openai.com":
						if strings.HasPrefix(p, "/c/") || strings.HasPrefix(p, "/share/") || strings.HasPrefix(p, "/codex/tasks/") {
							return true
						}
					case "claude.ai":
						if strings.HasPrefix(p, "/chat/") || strings.HasPrefix(p, "/code/") || strings.HasPrefix(p, "/share/") {
							return true
						}
					case "discord.com", "discordapp.com":
						if strings.HasPrefix(p, "/channels/") {
							return true
						}
					case "codex.openai.com":
						return true
					}
				}
			}
		}
	}
	return false
}

func retainedKeys(v map[string]any, keys ...string) bool {
	if len(v) != len(keys) {
		return false
	}
	for _, key := range keys {
		if _, ok := v[key]; !ok {
			return false
		}
	}
	return true
}

func retainedInteger(v any) (int64, bool) {
	token, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	n, err := token.Int64()
	return n, err == nil && n >= 0 && n <= 1<<53-1
}

func retainedText(v any) bool {
	text, ok := v.(string)
	return ok && strings.TrimSpace(text) != "" && !retainedJobIdentifier.MatchString(text) && !retainedConversationURL(text)
}

func retainedTextList(v any) bool {
	values, ok := v.([]any)
	if !ok || len(values) == 0 {
		return false
	}
	for _, value := range values {
		if !retainedText(value) {
			return false
		}
	}
	return true
}

func retainedURL(repo, commit, sourcePath string) string {
	parts := strings.Split(sourcePath, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return "https://github.com/" + repo + "/blob/" + commit + "/" + strings.Join(parts, "/")
}

// The journal is an assessment input, never an installer or a permission
// source. Private original receipts remain the separate acceptance anchor.
func validateRetainedProcedureJournal(v map[string]any, expectedRows int) error {
	fail := func() error { return fmt.Errorf("retained procedure journal source/read/rights boundary differs") }
	if !retainedKeys(v, "domain", "catalog_sha256", "assessment_records", "counts", "entries", "all_source_and_support_rights_confirmed", "new_copy_grants", "new_final_dispositions", "central_operations", "whole16_complete", "scope") ||
		v["domain"] != retainedProcedureDomain || v["catalog_sha256"] != "2d73c9a038a41cc220a9687b935bfa1e0b315b65d9bd6d3e54ddb4111972876c" ||
		v["all_source_and_support_rights_confirmed"] != false || v["whole16_complete"] != false || !retainedText(v["scope"]) {
		return fail()
	}
	for _, field := range []string{"new_copy_grants", "new_final_dispositions", "central_operations"} {
		if n, ok := retainedInteger(v[field]); !ok || n != 0 {
			return fail()
		}
	}
	rows, ok := v["entries"].([]any)
	declared, declaredOK := retainedInteger(v["assessment_records"])
	if !ok || !declaredOK || len(rows) == 0 || len(rows) != expectedRows || int64(len(rows)) != declared {
		return fail()
	}
	counts := map[string]int64{"primary_records": int64(len(rows))}
	states := map[string]int64{}
	seen := map[string]bool{}
	for _, value := range rows {
		r, ok := value.(map[string]any)
		if !ok || !retainedKeys(r, "inventory_id", "source_repo", "source_commit", "path", "source_sha256", "git_blob", "bytes", "source_url", "use", "value", "findings", "required_changes", "positive", "negative", "nearby_metadata", "read", "unmet", "support_read_states", "individual_notices", "source_code_executed", "new_final_disposition", "copy_permission_accepted") {
			return fail()
		}
		id, _ := r["inventory_id"].(string)
		repo, _ := r["source_repo"].(string)
		commit, _ := r["source_commit"].(string)
		p, _ := r["path"].(string)
		sha, _ := r["source_sha256"].(string)
		blob, _ := r["git_blob"].(string)
		primaryBytes, sizeOK := retainedInteger(r["bytes"])
		if !strings.HasPrefix(id, "github:") || !evidenceHex(strings.TrimPrefix(id, "github:"), 64) || seen[id] || repo != "6mile/skills" || commit != "920714f17d81e0a95c7a9bc51faf0a7921cd982d" ||
			!evidencePath(p) || !strings.HasPrefix(p, "skills/") || !strings.EqualFold(path.Base(p), "SKILL.md") || !evidenceHex(sha, 64) || !evidenceHex(blob, 40) || !sizeOK || primaryBytes == 0 || r["source_url"] != retainedURL(repo, commit, p) {
			return fail()
		}
		seen[id] = true
		for _, field := range []string{"source_code_executed", "new_final_disposition", "copy_permission_accepted"} {
			if r[field] != false {
				return fail()
			}
		}
		for _, field := range []string{"use", "value", "positive", "negative"} {
			if !retainedText(r[field]) {
				return fail()
			}
		}
		for _, field := range []string{"findings", "required_changes", "nearby_metadata", "unmet"} {
			if !retainedTextList(r[field]) {
				return fail()
			}
		}
		read, ok := r["read"].(map[string]any)
		if !ok || !retainedKeys(read, "kind", "physical_lines", "masked_lines", "withheld_physical_lines", "recorded_mask_version", "raw_read_complete", "workflow_view_complete", "ordered_view_complete", "visible_range_digest_provided", "initial_withheld_display_gaps", "final_withheld_unread_except_reported_initial_gaps", "withheld_meanings_not_treated_as_complete") {
			return fail()
		}
		kind, _ := read["kind"].(string)
		lines, linesOK := retainedInteger(read["physical_lines"])
		masked, maskOK := retainedInteger(read["masked_lines"])
		if !linesOK || !maskOK || lines == 0 || masked > lines || (kind != "direct_raw_full" && kind != "direct_masked_full") ||
			(kind == "direct_raw_full") != (masked == 0) || read["raw_read_complete"] != (kind == "direct_raw_full") ||
			read["workflow_view_complete"] != true || read["ordered_view_complete"] != true || read["visible_range_digest_provided"] != false ||
			read["final_withheld_unread_except_reported_initial_gaps"] != true || read["withheld_meanings_not_treated_as_complete"] != true {
			return fail()
		}
		if read["recorded_mask_version"] != nil {
			if version, ok := retainedInteger(read["recorded_mask_version"]); !ok || (version != 14 && version != 16) {
				return fail()
			}
		}
		withheld, ok := read["withheld_physical_lines"].([]any)
		if !ok || int64(len(withheld)) != masked {
			return fail()
		}
		positions := map[int64]bool{}
		previous := int64(0)
		for _, value := range withheld {
			line, ok := retainedInteger(value)
			if !ok || line <= previous || line > lines {
				return fail()
			}
			positions[line], previous = true, line
		}
		gaps, ok := read["initial_withheld_display_gaps"].([]any)
		if !ok {
			return fail()
		}
		gapLines := map[int64]bool{}
		for _, value := range gaps {
			gap, ok := value.(map[string]any)
			if !ok || !retainedKeys(gap, "line", "kind") || !retainedText(gap["kind"]) {
				return fail()
			}
			line, ok := retainedInteger(gap["line"])
			if !ok || !positions[line] || gapLines[line] {
				return fail()
			}
			gapLines[line] = true
		}
		counts["primary_physical_lines"] += lines
		counts["primary_withheld_lines"] += masked
		counts["initial_display_gap_records"] += int64(len(gaps))
		if kind == "direct_raw_full" {
			counts["raw_primary_records"]++
		} else {
			counts["masked_primary_records"]++
		}
		supports, ok := r["support_read_states"].([]any)
		if !ok || len(supports) == 0 {
			return fail()
		}
		byPath := map[string]map[string]any{}
		for _, value := range supports {
			support, ok := value.(map[string]any)
			if !ok || !retainedKeys(support, "relative_path", "bytes", "sha256_prefix", "git_blob_prefix", "state", "masked_lines", "mode") {
				return fail()
			}
			rel, _ := support["relative_path"].(string)
			state, _ := support["state"].(string)
			prefix, _ := support["sha256_prefix"].(string)
			gitPrefix, _ := support["git_blob_prefix"].(string)
			mode, _ := support["mode"].(string)
			if !evidencePath(rel) || byPath[rel] != nil || !evidenceHex(gitPrefix, 8) || (mode != "100644" && mode != "100755" && mode != "120000" && mode != "160000") {
				return fail()
			}
			byPath[rel] = support
			switch state {
			case "unsaved":
				if support["bytes"] != nil || support["sha256_prefix"] != nil || support["masked_lines"] != nil {
					return fail()
				}
				counts["unsaved_support_metadata"]++
			case "saved_unread", "direct_raw_full", "direct_masked_full", "exact_bytes_reuse", "ordered_full_diff", "partial", "scan":
				size, ok := retainedInteger(support["bytes"])
				if !ok || size == 0 || !evidenceHex(prefix, 8) {
					return fail()
				}
				if state == "saved_unread" {
					if support["masked_lines"] != nil {
						return fail()
					}
				} else {
					mask, ok := retainedInteger(support["masked_lines"])
					if !ok || (state == "direct_raw_full" && mask != 0) || (state == "direct_masked_full" && mask == 0) {
						return fail()
					}
				}
				counts["saved_support_metadata"]++
			default:
				return fail()
			}
			states[state]++
			counts["support_records"]++
		}
		self := byPath[path.Base(p)]
		if self == nil || self["sha256_prefix"] != sha[:8] || self["git_blob_prefix"] != blob[:8] || self["bytes"] != r["bytes"] || self["state"] != kind || self["masked_lines"] != read["masked_lines"] {
			return fail()
		}
		notices, ok := r["individual_notices"].([]any)
		if !ok || len(notices) == 0 {
			return fail()
		}
		seenNotice := map[string]bool{}
		for _, value := range notices {
			notice, ok := value.(map[string]any)
			if !ok || !retainedKeys(notice, "relative_path", "path", "source_url", "sha256", "git_blob", "bytes", "read_kind", "copy_permission_accepted", "scope") {
				return fail()
			}
			rel, _ := notice["relative_path"].(string)
			noticePath, _ := notice["path"].(string)
			noticeSHA, _ := notice["sha256"].(string)
			noticeBlob, _ := notice["git_blob"].(string)
			support := byPath[rel]
			if !evidencePath(rel) || !noticeEvidencePath(rel) || seenNotice[rel] || support == nil || noticePath != path.Join(path.Dir(p), rel) ||
				!evidenceHex(noticeSHA, 64) || !evidenceHex(noticeBlob, 40) || notice["source_url"] != retainedURL(repo, commit, noticePath) ||
				notice["copy_permission_accepted"] != false || !retainedText(notice["scope"]) || notice["bytes"] != support["bytes"] ||
				noticeSHA[:8] != support["sha256_prefix"] || noticeBlob[:8] != support["git_blob_prefix"] || notice["read_kind"] != support["state"] {
				return fail()
			}
			seenNotice[rel] = true
		}
	}
	declaredCounts, ok := v["counts"].(map[string]any)
	if !ok || !retainedKeys(declaredCounts, "primary_records", "raw_primary_records", "masked_primary_records", "primary_physical_lines", "primary_withheld_lines", "initial_display_gap_records", "support_records", "saved_support_metadata", "unsaved_support_metadata", "support_state_counts") {
		return fail()
	}
	for _, key := range []string{"primary_records", "raw_primary_records", "masked_primary_records", "primary_physical_lines", "primary_withheld_lines", "initial_display_gap_records", "support_records", "saved_support_metadata", "unsaved_support_metadata"} {
		if n, ok := retainedInteger(declaredCounts[key]); !ok || n != counts[key] {
			return fail()
		}
	}
	declaredStates, ok := declaredCounts["support_state_counts"].(map[string]any)
	if !ok || len(declaredStates) != len(states) {
		return fail()
	}
	for state, actual := range states {
		if n, ok := retainedInteger(declaredStates[state]); !ok || n != actual {
			return fail()
		}
	}
	return nil
}
