package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func retainedV2Fixture(t *testing.T) map[string]any {
	t.Helper()
	b, e := os.ReadFile(filepath.Join("..", "..", "docs", retainedProcedureV2File))
	if e != nil {
		t.Fatal(e)
	}
	v, e := uniqueEvidenceJSON(b)
	if e != nil {
		t.Fatal(e)
	}
	return v.(map[string]any)
}
func retainedV2Load(t *testing.T, v map[string]any) (ExecutionEvidence, error) {
	t.Helper()
	root := t.TempDir()
	write := func(name string, b []byte, n int) ExecutionArtifact {
		if e := os.WriteFile(filepath.Join(root, name), b, 0600); e != nil {
			t.Fatal(e)
		}
		h := sha256.Sum256(b)
		return ExecutionArtifact{File: name, SHA: hex.EncodeToString(h[:]), Rows: n}
	}
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	a := write(retainedProcedureV2File, b, len(v["entries"].([]any)))
	l := write("skills-3080-additional-ledger.json", []byte(`{"entries":[],"excluded_entries":[]}`), 0)
	return loadExecutionEvidence(root, ExecutionManifest{Artifacts: []ExecutionArtifact{a}, Ledger: l})
}
func retainedV2EncodingRow(t *testing.T, v map[string]any) map[string]any {
	t.Helper()
	for _, x := range v["entries"].([]any) {
		r := x.(map[string]any)
		if r["read"].(map[string]any)["encoding_exception"] != nil {
			return r
		}
	}
	t.Fatal("encoding exception absent")
	return nil
}
func TestAdditionalRetainedJournalAcceptedAndSeparate(t *testing.T) {
	v := retainedV2Fixture(t)
	e, err := retainedV2Load(t, v)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Assessments) != 163 || len(e.Decisions) != 0 {
		t.Fatal("assessment credit or final decisions differ")
	}
	if err := validateRetainedProcedureV2(v, 163); err != nil {
		t.Fatal(err)
	}
	if err := validateRetainedProcedureJournal(retainedJournalFixture(t), 22); err != nil {
		t.Fatal("earlier journal changed", err)
	}
}
func TestAdditionalRetainedJournalRejectsChangedReadHistory(t *testing.T) {
	cases := []struct {
		name string
		edit func(map[string]any)
	}{
		{"notice_list_wrong_type", func(v map[string]any) {
			v["entries"].([]any)[0].(map[string]any)["individual_notices"] = "invalid"
		}},
		{"unknown_profile", func(v map[string]any) {
			v["entries"].([]any)[0].(map[string]any)["read"].(map[string]any)["recorded_mask_version"] = json.Number("999")
		}},
		{"font_notice_mapping_changed", func(v map[string]any) {
			for _, x := range v["entries"].([]any) {
				for _, x := range x.(map[string]any)["individual_notices"].([]any) {
					n := x.(map[string]any)
					if len(n["relative_path"].(string)) > 13 && n["relative_path"].(string)[:13] == "canvas-fonts/" {
						n["git_blob"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
						return
					}
				}
			}
			t.Fatal("font notice fixture absent")
		}},
		{"missing_encoding_exception", func(v map[string]any) {
			retainedV2EncodingRow(t, v)["read"].(map[string]any)["encoding_exception"] = nil
		}},
		{"invalid_offset", func(v map[string]any) {
			ex := retainedV2EncodingRow(t, v)["read"].(map[string]any)["encoding_exception"].(map[string]any)
			ex["invalid_bytes"].([]any)[0].(map[string]any)["byte_offset_zero_based"] = json.Number("13925")
		}},
		{"unhidden_invalid_line", func(v map[string]any) {
			ex := retainedV2EncodingRow(t, v)["read"].(map[string]any)["encoding_exception"].(map[string]any)
			ex["invalid_bytes"].([]any)[0].(map[string]any)["line"] = json.Number("19")
		}},
		{"replacement_decoder", func(v map[string]any) {
			retainedV2EncodingRow(t, v)["read"].(map[string]any)["encoding_exception"].(map[string]any)["permissive_decode_not_used"] = false
		}},
		{"false_utf8", func(v map[string]any) {
			retainedV2EncodingRow(t, v)["read"].(map[string]any)["encoding_exception"].(map[string]any)["strict_utf8"] = true
		}},
		{"source_replaced", func(v map[string]any) {
			retainedV2EncodingRow(t, v)["read"].(map[string]any)["encoding_exception"].(map[string]any)["source_refetched"] = true
		}},
		{"initial_limit_omitted", func(v map[string]any) {
			v["initial_display_limitations"] = v["initial_display_limitations"].([]any)[1:]
		}},
		{"initial_limit_changed", func(v map[string]any) {
			v["initial_display_limitations"].([]any)[0].(map[string]any)["scope_note"] = "changed"
		}},
		{"invented_limit_row", func(v map[string]any) {
			v["initial_display_limitations"].([]any)[0].(map[string]any)["reported_limit"].([]any)[1].(map[string]any)["row"] = json.Number("367")
		}},
		{"private_reader", func(v map[string]any) { v["entries"].([]any)[0].(map[string]any)["reader_job"] = "private-reader" }},
		{"notice_reuse_inflated", func(v map[string]any) {
			for _, x := range v["entries"].([]any) {
				for _, n := range x.(map[string]any)["individual_notices"].([]any) {
					m := n.(map[string]any)
					if m["read_kind"] == "exact_bytes_reuse" {
						m["read_kind"] = "direct_raw_full"
						return
					}
				}
			}
			t.Fatal("reuse absent")
		}},
		{"unknown_metadata_read", func(v map[string]any) {
			for _, x := range v["entries"].([]any) {
				for _, s := range x.(map[string]any)["support_read_states"].([]any) {
					m := s.(map[string]any)
					if m["state"] == "saved_unread" {
						m["masked_lines"] = json.Number("0")
						return
					}
				}
			}
			t.Fatal("unread absent")
		}},
		{"new_rights", func(v map[string]any) { v["all_source_and_support_rights_confirmed"] = true }},
		{"unknown_encoding_elsewhere", func(v map[string]any) {
			x := retainedV2EncodingRow(t, v)["read"].(map[string]any)["encoding_exception"]
			v["entries"].([]any)[0].(map[string]any)["read"].(map[string]any)["encoding_exception"] = x
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := retainedV2Fixture(t)
			tc.edit(v)
			if _, e := retainedV2Load(t, v); e == nil {
				t.Fatal("changed history accepted despite fresh matching artifact pin")
			}
		})
	}
}
