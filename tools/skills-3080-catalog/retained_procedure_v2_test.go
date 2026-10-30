package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		{"scope_claim_changed", func(v map[string]any) { v["scope"] = "全候補を許諾済みとして導入完了" }},
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

func TestAdditionalRetainedRejectsCoherentHistoryInflation(t *testing.T) {
	changeCount := func(v map[string]any, key string, delta int64) {
		t.Helper()
		counts := v["counts"].(map[string]any)
		old, ok := retainedInteger(counts[key])
		if !ok {
			t.Fatal("fixture counter absent")
		}
		counts[key] = json.Number(fmt.Sprint(old + delta))
	}
	t.Run("primary_and_support_and_counts_claim_raw", func(t *testing.T) {
		v := retainedV2Fixture(t)
		for _, x := range v["entries"].([]any) {
			r := x.(map[string]any)
			rd := r["read"].(map[string]any)
			if rd["kind"] != "direct_masked_full" || rd["encoding_exception"] != nil {
				continue
			}
			mask, _ := retainedInteger(rd["masked_lines"])
			rd["kind"] = "direct_raw_full"
			rd["raw_read_complete"] = true
			rd["masked_lines"] = json.Number("0")
			rd["withheld_physical_lines"] = []any{}
			for _, s := range r["support_read_states"].([]any) {
				sup := s.(map[string]any)
				if sup["relative_path"] == "SKILL.md" {
					sup["state"] = "direct_raw_full"
					sup["masked_lines"] = json.Number("0")
				}
			}
			changeCount(v, "raw_primary_records", 1)
			changeCount(v, "masked_primary_records", -1)
			changeCount(v, "primary_withheld_lines", -mask)
			states := v["counts"].(map[string]any)["support_state_counts"].(map[string]any)
			for key, d := range map[string]int64{"direct_raw_full": 1, "direct_masked_full": -1} {
				n, _ := retainedInteger(states[key])
				states[key] = json.Number(fmt.Sprint(n + d))
			}
			if _, e := retainedV2Load(t, v); e == nil {
				t.Fatal("coherent masked history promoted to raw")
			}
			return
		}
		t.Fatal("masked fixture absent")
	})
	t.Run("reuse_and_notice_and_counts_claim_fresh", func(t *testing.T) {
		v := retainedV2Fixture(t)
		for _, x := range v["entries"].([]any) {
			r := x.(map[string]any)
			for _, nx := range r["individual_notices"].([]any) {
				n := nx.(map[string]any)
				rel := n["relative_path"].(string)
				if n["read_kind"] != "exact_bytes_reuse" || strings.HasPrefix(rel, "canvas-fonts/") {
					continue
				}
				for _, sx := range r["support_read_states"].([]any) {
					s := sx.(map[string]any)
					if s["relative_path"] == rel {
						s["state"] = "direct_raw_full"
						s["masked_lines"] = json.Number("0")
					}
				}
				n["read_kind"] = "direct_raw_full"
				states := v["counts"].(map[string]any)["support_state_counts"].(map[string]any)
				for key, d := range map[string]int64{"direct_raw_full": 1, "exact_bytes_reuse": -1} {
					count, _ := retainedInteger(states[key])
					states[key] = json.Number(fmt.Sprint(count + d))
				}
				if _, e := retainedV2Load(t, v); e == nil {
					t.Fatal("coherent reused notice promoted to fresh raw")
				}
				return
			}
		}
		t.Fatal("reuse fixture absent")
	})
	for _, kind := range []string{"numeric_byte", "contradictory_method"} {
		t.Run(kind, func(t *testing.T) {
			v := retainedV2Fixture(t)
			ex := retainedV2EncodingRow(t, v)["read"].(map[string]any)["encoding_exception"].(map[string]any)
			if kind == "numeric_byte" {
				ex["invalid_bytes"].([]any)[0].(map[string]any)["invalid_byte_hex"] = json.Number("97")
			} else {
				ex["method"] = "置換文字で復号した"
			}
			if _, e := retainedV2Load(t, v); e == nil {
				t.Fatal("encoding history changed")
			}
		})
	}
}
