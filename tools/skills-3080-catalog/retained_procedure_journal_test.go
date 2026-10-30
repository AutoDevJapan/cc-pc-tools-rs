package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const retainedProcedureFile = "skills-3080-retained-procedure-assessments.json"

func retainedJournalFixture(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", retainedProcedureFile))
	if err != nil {
		t.Fatal(err)
	}
	v, err := uniqueEvidenceJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}

func loadRetainedJournalFixture(t *testing.T, v map[string]any) (ExecutionEvidence, error) {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, raw []byte, rows int) ExecutionArtifact {
		if err := os.WriteFile(filepath.Join(dir, name), raw, 0600); err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(raw)
		return ExecutionArtifact{File: name, SHA: hex.EncodeToString(h[:]), Rows: rows}
	}
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	artifact := write(retainedProcedureFile, raw, len(v["entries"].([]any)))
	ledger := write("skills-3080-retained-fixture-ledger.json", []byte(`{"entries":[],"excluded_entries":[]}`), 0)
	return loadExecutionEvidence(dir, ExecutionManifest{Artifacts: []ExecutionArtifact{artifact}, Ledger: ledger})
}

func TestRetainedProcedureJournalAcceptedByQueue(t *testing.T) {
	v := retainedJournalFixture(t)
	evidence, err := loadRetainedJournalFixture(t, v)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.Assessments) != 22 || len(evidence.Decisions) != 0 {
		t.Fatal("meaning assessments became decisions or lost identities")
	}
	for _, value := range v["entries"].([]any) {
		r := value.(map[string]any)
		id := r["inventory_id"].(string)
		a := evidence.Assessments[id]
		if a.SourceRepo != r["source_repo"] || a.Commit != r["source_commit"] || a.Path != r["path"] || a.SHA != r["source_sha256"] || len(evidence.References[id]) != 1 {
			t.Fatal("queue source tuple or reference differs")
		}
	}
}

func TestRetainedProcedureJournalRejectsChangedReadAndRightsBeforeQueue(t *testing.T) {
	entry := func(v map[string]any, index int) map[string]any { return v["entries"].([]any)[index].(map[string]any) }
	read := func(v map[string]any) map[string]any { return entry(v, 3)["read"].(map[string]any) }
	support := func(v map[string]any, state string) map[string]any {
		for _, value := range v["entries"].([]any) {
			for _, value := range value.(map[string]any)["support_read_states"].([]any) {
				r := value.(map[string]any)
				if r["state"] == state {
					return r
				}
			}
		}
		t.Fatalf("fixture state %s absent", state)
		return nil
	}
	cases := []struct {
		name string
		edit func(map[string]any)
	}{
		{"masked_primary_claimed_raw", func(v map[string]any) { read(v)["kind"] = "direct_raw_full"; read(v)["raw_read_complete"] = true }},
		{"missing_withheld_position", func(v map[string]any) { read(v)["withheld_physical_lines"] = []any{} }},
		{"unprovided_view_digest", func(v map[string]any) { read(v)["visible_range_digest_provided"] = true }},
		{"notice_gains_copy_permission", func(v map[string]any) {
			entry(v, 0)["individual_notices"].([]any)[0].(map[string]any)["copy_permission_accepted"] = true
		}},
		{"unsaved_gains_bytes", func(v map[string]any) { support(v, "unsaved")["bytes"] = json.Number("1") }},
		{"saved_unread_gains_mask_count", func(v map[string]any) { support(v, "saved_unread")["masked_lines"] = json.Number("0") }},
		{"support_escapes_package", func(v map[string]any) { support(v, "unsaved")["relative_path"] = "../outside.txt" }},
		{"unknown_mask_version_invented", func(v map[string]any) { read(v)["recorded_mask_version"] = "unknown" }},
		{"unknown_numeric_mask_version", func(v map[string]any) { read(v)["recorded_mask_version"] = json.Number("999") }},
		{"private_reader_field", func(v map[string]any) { entry(v, 0)["reader_job"] = "internal-reader" }},
		{"full_completion_claim", func(v map[string]any) { v["whole16_complete"] = true }},
		{"mismatched_source_URL", func(v map[string]any) {
			entry(v, 0)["source_url"] = "https://github.com/other/source/blob/main/SKILL.md"
		}},
		{"false_count", func(v map[string]any) { v["counts"].(map[string]any)["raw_primary_records"] = json.Number("11") }},
		{"initial_gap_omitted", func(v map[string]any) {
			for _, value := range v["entries"].([]any) {
				r := value.(map[string]any)["read"].(map[string]any)
				if len(r["initial_withheld_display_gaps"].([]any)) > 0 {
					r["initial_withheld_display_gaps"] = []any{}
					return
				}
			}
			t.Fatal("initial display-gap fixture absent")
		}},
		{"private_job_value_in_meaning", func(v map[string]any) { entry(v, 0)["value"] = "record 20991231-123456-todo-1" }},
		{"claude_conversation_URL", func(v map[string]any) { entry(v, 0)["value"] = "https://claude.ai/chat/example-session" }},
		{"discord_conversation_URL", func(v map[string]any) { entry(v, 0)["value"] = "https://discord.com/channels/1/2/3" }},
		{"encoded_discord_conversation_URL", func(v map[string]any) { entry(v, 0)["value"] = "https://WWW.DISCORD.COM./%63hannels/1/2/3" }},
		{"legacy_chatgpt_conversation_URL", func(v map[string]any) { entry(v, 0)["value"] = "https://chat.openai.com/c/example-session" }},
		{"shared_conversation_URL", func(v map[string]any) { entry(v, 0)["value"] = "https://chatgpt.com/share/example-share" }},
		{"conversation_URL_inside_another_URL", func(v map[string]any) {
			entry(v, 0)["value"] = "https://example.test/?next=https://claude.ai/chat/example-session"
		}},
		{"query_escaped_conversation_URL", func(v map[string]any) {
			entry(v, 0)["value"] = "https://example.test/?next=https%3A%2F%2Fchatgpt.com%2Fshare%2Fexample-share"
		}},
		{"double_escaped_conversation_URL", func(v map[string]any) {
			entry(v, 0)["value"] = "https://example.test/?next=https%253A%252F%252Fclaude.ai%252Fchat%252Fexample-session"
		}},
		{"deep_escaped_conversation_URL", func(v map[string]any) {
			text := "https://claude.ai/chat/example-session"
			for i := 0; i < 12; i++ {
				text = url.QueryEscape(text)
			}
			entry(v, 0)["value"] = "https://example.test/?next=" + text
		}},
		{"invalid_query_escape_cannot_hide_conversation_URL", func(v map[string]any) {
			entry(v, 0)["value"] = "https://example.test/?bad=100%&next=https%3A%2F%2Fclaude.ai%2Fchat%2Fexample-session"
		}},
		{"bare_conversation_URL", func(v map[string]any) { entry(v, 0)["value"] = "chatgpt.com/c/example-session" }},
		{"protocol_relative_conversation_URL", func(v map[string]any) { entry(v, 0)["value"] = "//chatgpt.com/c/example-session" }},
		{"custom_GPT_conversation_URL", func(v map[string]any) { entry(v, 0)["value"] = "https://chatgpt.com/g/g-example/c/example-session" }},
		{"project_GPT_conversation_URL", func(v map[string]any) { entry(v, 0)["value"] = "https://chatgpt.com/g/g-p-example/c/example-session" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := retainedJournalFixture(t)
			tc.edit(v)
			if _, err := loadRetainedJournalFixture(t, v); err == nil {
				t.Fatal("queue accepted changed read/rights/public scope despite a freshly matching artifact pin")
			}
		})
	}
}

func TestRetainedProcedureJournalKeepsLegitimateNamesAndDocumentationURLs(t *testing.T) {
	v := retainedJournalFixture(t)
	v["entries"].([]any)[0].(map[string]any)["value"] = "Claude-Session is an analyzed header name, gpt-6-luna is a model identifier, and legitimate copyright authors remain. https://platform.openai.com/docs/overview https://discord.com/developers/docs/intro https://chatgpt.com/g/g-example"
	if _, err := loadRetainedJournalFixture(t, v); err != nil {
		t.Fatalf("functional identifiers and documentation links are not an attribution: %v", err)
	}
}

func TestRetainedProcedureJournalPreservesDistinctUnitsAndHistory(t *testing.T) {
	v := retainedJournalFixture(t)
	c := v["counts"].(map[string]any)
	want := map[string]string{"primary_records": "22", "raw_primary_records": "10", "masked_primary_records": "12", "primary_physical_lines": "5995", "primary_withheld_lines": "72", "initial_display_gap_records": "3", "support_records": "320", "saved_support_metadata": "157", "unsaved_support_metadata": "163"}
	for key, value := range want {
		if c[key] != json.Number(value) {
			t.Fatalf("retained observation %s differs", key)
		}
	}
	counts := c["support_state_counts"].(map[string]any)
	for state, value := range map[string]string{"unsaved": "163", "saved_unread": "72", "direct_raw_full": "42", "direct_masked_full": "23", "exact_bytes_reuse": "20"} {
		if counts[state] != json.Number(value) {
			t.Fatalf("support state %s differs", state)
		}
	}
	for _, value := range v["entries"].([]any) {
		r := value.(map[string]any)
		if strings.HasSuffix(r["path"].(string), "/aoment-image-video/SKILL.md") {
			gaps := r["read"].(map[string]any)["initial_withheld_display_gaps"].([]any)
			if len(gaps) != 3 || gaps[0].(map[string]any)["line"] != json.Number("65") || gaps[1].(map[string]any)["line"] != json.Number("96") || gaps[2].(map[string]any)["line"] != json.Number("247") {
				t.Fatal("initial display gaps were silently made historically unread")
			}
		}
	}
}
