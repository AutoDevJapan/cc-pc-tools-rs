package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func retainedV3Fixture(t *testing.T) map[string]any {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join("..", "..", "docs", retainedProcedureV3File))
	if e != nil {
		t.Fatal(e)
	}
	v, e := uniqueEvidenceJSON(raw)
	if e != nil {
		t.Fatal(e)
	}
	return v.(map[string]any)
}
func TestRetainedV3ActualSnapshot(t *testing.T) {
	if e := validateRetainedProcedureV3(retainedV3Fixture(t), 91); e != nil {
		t.Fatal(e)
	}
}
func TestRetainedV3ExecutionRejectsRepinnedHistoryAndRenaming(t *testing.T) {
	for _, name := range []string{"valid", "repinned_changed_history", "renamed_artifact"} {
		t.Run(name, func(t *testing.T) {
			v := retainedV3Fixture(t)
			if name == "repinned_changed_history" {
				v["notice_read_exceptions"].([]any)[0].(map[string]any)["legal_permission_confirmed"] = true
			}
			root := t.TempDir()
			write := func(file string, data []byte, n int) ExecutionArtifact {
				if e := os.WriteFile(filepath.Join(root, file), data, 0600); e != nil {
					t.Fatal(e)
				}
				h := sha256.Sum256(data)
				return ExecutionArtifact{File: file, SHA: hex.EncodeToString(h[:]), Rows: n}
			}
			b, e := json.Marshal(v)
			if e != nil {
				t.Fatal(e)
			}
			file := retainedProcedureV3File
			if name == "renamed_artifact" {
				file = "skills-3080-renamed-latest-assessments.json"
			}
			a := write(file, b, 91)
			ledger := write("skills-3080-empty-final-ledger.json", []byte(`{"entries":[],"excluded_entries":[]}`), 0)
			evidence, e := loadExecutionEvidence(root, ExecutionManifest{Artifacts: []ExecutionArtifact{a}, Ledger: ledger})
			if name == "valid" {
				if e != nil || len(evidence.Assessments) != 91 || len(evidence.Decisions) != 0 {
					t.Fatalf("assessment-only execution integration failed: %v", e)
				}
			} else if e == nil {
				t.Fatal("new valid artifact pin bypassed fixed history/filename validation")
			}
		})
	}
}
func TestRetainedV3IsRequiredInPublishedExecutionManifest(t *testing.T) {
	var manifest ExecutionManifest
	loadPublicReceipt(t, "skills-3080-execution-evidence.json", &manifest)
	count := 0
	for _, a := range manifest.Artifacts {
		if a.File == retainedProcedureV3File {
			count++
			if a.Rows != 91 {
				t.Fatal("latest finite assessment range was truncated")
			}
		}
	}
	if count != 1 {
		t.Fatal("all91 latest assessments must occur exactly once in the full occurrence queue")
	}
}
func TestRetainedV3RejectsCoherentHistoryPromotion(t *testing.T) {
	for _, name := range []string{"primary", "support", "gap", "runtime_notice", "masked_diff"} {
		t.Run(name, func(t *testing.T) {
			v := retainedV3Fixture(t)
			rows := v["entries"].([]any)
			counts := v["counts"].(map[string]any)
			switch name {
			case "primary":
				for _, x := range rows {
					r := x.(map[string]any)
					read := r["read"].(map[string]any)
					if read["kind"] != "direct_masked_full" {
						continue
					}
					mask, _ := retainedInteger(read["masked_lines"])
					read["kind"] = "direct_raw_full"
					read["masked_lines"] = json.Number("0")
					read["withheld_physical_lines"] = []any{}
					read["raw_read_complete"] = true
					for _, y := range r["support_read_states"].([]any) {
						s := y.(map[string]any)
						if s["relative_path"] == filepath.Base(r["path"].(string)) {
							s["state"] = "direct_raw_full"
							s["masked_lines"] = json.Number("0")
						}
					}
					changeV3Count(counts, "raw_primary_records", 1)
					changeV3Count(counts, "masked_primary_records", -1)
					changeV3Count(counts, "primary_withheld_lines", -mask)
					states := counts["support_state_counts"].(map[string]any)
					changeV3Count(states, "direct_raw_full", 1)
					changeV3Count(states, "direct_masked_full", -1)
					break
				}
			case "support":
				changed := false
				for _, x := range rows {
					r := x.(map[string]any)
					for _, y := range r["support_read_states"].([]any) {
						s := y.(map[string]any)
						if s["state"] == "saved_unread" {
							s["state"] = "direct_raw_full"
							s["masked_lines"] = json.Number("0")
							changed = true
							break
						}
					}
					if changed {
						break
					}
				}
				if !changed {
					t.Fatal("fixture lacks saved unread support")
				}
				states := counts["support_state_counts"].(map[string]any)
				changeV3Count(states, "saved_unread", -1)
				changeV3Count(states, "direct_raw_full", 1)
			case "gap":
				limit := v["initial_display_limitations"].([]any)[0].(map[string]any)["reported_limit"].(map[string]any)
				limit["initial_unmasked_public_example_name_lines"] = []any{}
				limit["history_not_relabelled"] = false
			case "runtime_notice":
				x := v["notice_read_exceptions"].([]any)[0].(map[string]any)
				x["kind"] = "individual_legal_notice"
				x["legal_permission_confirmed"] = true
			case "masked_diff":
				x := v["notice_read_exceptions"].([]any)[1].(map[string]any)
				x["baseline_read_kind"] = "direct_raw_full"
				x["baseline_masked_lines"] = json.Number("0")
				x["withheld_physical_lines"] = []any{}
			}
			if e := validateRetainedProcedureV3(v, 91); e == nil {
				t.Fatal("coherently rewritten read/history was accepted")
			}
		})
	}
}
func changeV3Count(m map[string]any, key string, delta int64) {
	n, _ := retainedInteger(m[key])
	m[key] = json.Number(fmtV3Integer(n + delta))
}
func fmtV3Integer(n int64) string { b, _ := json.Marshal(n); return string(b) }
