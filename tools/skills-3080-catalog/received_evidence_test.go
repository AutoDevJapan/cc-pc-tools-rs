package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func TestReceivedCohortBindsOriginalBytesAndRejectsCoupledClaimChanges(t *testing.T) {
	for _, name := range []string{"valid", "reader", "masked_as_raw", "partial_as_full", "reuse_as_new_raw", "unsaved_as_empty", "grant", "execute", "same_source_path", "source_namespace", "counter", "missing_counter", "recomputed_anchor", "duplicate_json_key"} {
		t.Run(name, func(t *testing.T) {
			var a ReceivedCohort
			loadPublicReceipt(t, "skills-3080-sixmile-received50-evaluation.json", &a)
			var entries []map[string]any
			if err := json.Unmarshal(a.Entries, &entries); err != nil {
				t.Fatal(err)
			}
			mutateSupport := func(code string, change func(map[string]any)) {
				t.Helper()
				for _, e := range entries {
					for _, v := range e["support_read_states"].([]any) {
						s := v.(map[string]any)
						if s["state"] == code {
							change(s)
							return
						}
					}
				}
				t.Fatal("mutation support absent")
			}
			switch name {
			case "reader":
				entries[0]["reader_job"] = "forged"
			case "masked_as_raw":
				for _, e := range entries {
					if e["body_read_complete"] == false {
						e["body_read_complete"] = true
						e["body_mask_count"] = float64(0)
						a.Counts["primary_raw"]++
						a.Counts["primary_masked"]--
						break
					}
				}
			case "partial_as_full":
				mutateSupport("PT", func(s map[string]any) {
					s["state"] = "FR"
					s["partial_read"] = false
					s["raw_read_complete"] = true
					s["workflow_read_complete"] = true
				})
				a.Counts["partial"]--
				a.Counts["direct_raw_support"]++
				a.Counts["full_workflow_support"]++
			case "reuse_as_new_raw":
				mutateSupport("RU", func(s map[string]any) { s["state"] = "FR"; s["raw_read_complete"] = true; delete(s, "representative") })
				a.Counts["same_byte_notice_reuse"]--
				a.Counts["direct_raw_support"]++
			case "unsaved_as_empty":
				mutateSupport("U", func(s map[string]any) { s["bytes"] = float64(0) })
			case "grant":
				entries[0]["individual_notices"].([]any)[0].(map[string]any)["copy_license_accepted"] = true
			case "execute":
				entries[0]["source_code_executed"] = true
			case "same_source_path":
				entries[1]["path"] = entries[0]["path"]
			case "source_namespace":
				entries[0]["source"] = "other/skills"
			case "counter":
				a.Counts["primary_raw"]++
			case "missing_counter":
				delete(a.Counts, "scan")
			case "recomputed_anchor":
				entries[0]["reader_job"] = "forged"
				a.EntrySHA = strings.Repeat("0", 64)
			case "duplicate_json_key":
				a.Entries = json.RawMessage(`[{"inventory_id":"first","inventory_id":"last"}]`)
			}
			if name != "valid" && name != "duplicate_json_key" {
				b, err := json.Marshal(entries)
				if err != nil {
					t.Fatal(err)
				}
				a.Entries = b
			}
			_, err := validateReceivedCohort(a, received50Spec)
			if name == "valid" && err != nil {
				t.Fatal(err)
			}
			if name != "valid" && err == nil {
				t.Fatalf("accepted %s", name)
			}
		})
	}
}

func TestReceivedCohortStateRulesStillApplyToNewIndependentlyAnchoredInputs(t *testing.T) {
	for _, name := range []string{"masked_zero", "raw_not_workflow", "unsaved_has_bytes", "unsaved_partial", "unsaved_scan", "reuse_unknown_actor", "notice_state_mismatch", "missing_boolean"} {
		t.Run(name, func(t *testing.T) {
			var a ReceivedCohort
			loadPublicReceipt(t, "skills-3080-sixmile-received50-evaluation.json", &a)
			var entries []map[string]any
			if err := json.Unmarshal(a.Entries, &entries); err != nil {
				t.Fatal(err)
			}
			changed := false
			for _, e := range entries {
				for _, item := range e["support_read_states"].([]any) {
					s := item.(map[string]any)
					if name == "masked_zero" && s["state"] == "FW" {
						s["masked_value_count"] = float64(0)
						changed = true
					}
					if name == "raw_not_workflow" && s["state"] == "FR" {
						s["workflow_read_complete"] = false
						changed = true
					}
					if name == "unsaved_has_bytes" && s["state"] == "U" {
						s["bytes"] = float64(0)
						changed = true
					}
					if (name == "unsaved_partial" || name == "unsaved_scan") && s["state"] == "U" {
						field := "partial_read"
						if name == "unsaved_scan" {
							field = "scan_only"
						}
						s[field] = true
						changed = true
					}
					if name == "reuse_unknown_actor" && s["state"] == "I" {
						s["reader_job"] = "other"
						changed = true
					}
					if name == "missing_boolean" && s["state"] == "FR" {
						delete(s, "source_executed")
						changed = true
					}
					if changed {
						break
					}
				}
				if name == "notice_state_mismatch" {
					n := e["individual_notices"].([]any)[0].(map[string]any)
					n["source_state"].(map[string]any)["raw_read_complete"] = false
					changed = true
				}
				if changed {
					break
				}
			}
			if !changed {
				t.Fatal("negative fixture absent")
			}
			b, err := json.Marshal(entries)
			if err != nil {
				t.Fatal(err)
			}
			a.Entries = b
			canonical, err := uniqueEvidenceJSON(b)
			if err != nil {
				t.Fatal(err)
			}
			b, err = json.Marshal(canonical)
			if err != nil {
				t.Fatal(err)
			}
			h := sha256.Sum256(b)
			spec := received50Spec
			spec.EntrySHA = hex.EncodeToString(h[:])
			a.EntrySHA = spec.EntrySHA
			if _, err = validateReceivedCohort(a, spec); err == nil {
				t.Fatal("new input bypassed structured state checks")
			}
		})
	}
}
