package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestStream90RegisteredOriginalEvidence(t *testing.T) {
	var a ReceivedCohort
	loadPublicReceipt(t, "skills-3080-sixmile-stream90-evaluation.json", &a)
	spec, err := registeredReceivedSpec(a)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := validateReceivedCohort(a, spec)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 90 || a.Counts["primary_raw"] != 2 || a.Counts["primary_workflow"] != 87 || a.Counts["primary_saved_unread"] != 1 || a.Counts["primary_partial"] != 2 || a.Counts["primary_exact_reuse"] != 1 {
		t.Fatal("original observations were promoted or lost")
	}
}

func TestStream90RejectsBodyReaderAndReadStatePromotion(t *testing.T) {
	kinds := []string{"valid", "unread_reader", "missing_reader", "missing_mask", "reader_alias", "unread_as_workflow", "partial_as_workflow", "partial_as_raw", "masked_zero", "reuse_as_literal", "reuse_wrong_predecessor", "reuse_self", "notice_without_reader", "unread_support_with_reader", "unsaved_has_bytes", "copy_grant", "missing_original", "resource_reuse_wrong_actor", "resource_reuse_wrong_predecessor"}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			var a ReceivedCohort
			loadPublicReceipt(t, "skills-3080-sixmile-stream90-evaluation.json", &a)
			spec, err := registeredReceivedSpec(a)
			if err != nil {
				t.Fatal(err)
			}
			var entries []map[string]any
			if err = json.Unmarshal(a.Entries, &entries); err != nil {
				t.Fatal(err)
			}
			row := func(ordinal int) map[string]any { return entries[ordinal-1] }
			support := func(state string, mutate func(map[string]any)) {
				t.Helper()
				for _, e := range entries {
					for _, v := range e["support_read_states"].([]any) {
						s := v.(map[string]any)
						if s["state"] == state {
							mutate(s)
							return
						}
					}
				}
				t.Fatal("required real support state absent")
			}
			switch kind {
			case "unread_reader":
				row(63)["reader_job"] = "20261025-041905-todo-3719"
			case "missing_reader":
				delete(row(63), "reader_job")
			case "missing_mask":
				delete(row(63), "body_mask_count")
			case "reader_alias":
				row(63)["Reader_Job"] = nil
			case "unread_as_workflow":
				row(63)["body_workflow_read_complete"] = true
			case "partial_as_workflow":
				row(78)["body_workflow_read_complete"] = true
			case "partial_as_raw":
				row(81)["body_read_complete"] = true
			case "masked_zero":
				e := row(10)
				e["body_mask_count"] = float64(0)
				e["original_read_tuple"].([]any)[1] = float64(0)
				e["original_reader_assessment"].(map[string]any)["read"].([]any)[1] = float64(0)
			case "reuse_as_literal":
				row(49)["primary_read_method"] = "direct_masked_full"
			case "reuse_wrong_predecessor":
				row(49)["original_reader_assessment"].(map[string]any)["primary_reuse"].(map[string]any)["representative"].(map[string]any)["row"] = float64(9)
			case "reuse_self":
				row(49)["original_reader_assessment"].(map[string]any)["primary_reuse"].(map[string]any)["representative"].(map[string]any)["row"] = float64(49)
			case "notice_without_reader":
				e := row(63)
				n := e["individual_notices"].([]any)[0].(map[string]any)
				p := n["path"]
				n["source_state"].(map[string]any)["reader_job"] = nil
				for _, v := range e["support_read_states"].([]any) {
					s := v.(map[string]any)
					if s["path"] == p {
						s["reader_job"] = nil
					}
				}
			case "unread_support_with_reader":
				support("NR", func(s map[string]any) { s["reader_job"] = "20261025-041905-todo-3719" })
			case "unsaved_has_bytes":
				support("UN", func(s map[string]any) { s["bytes"] = float64(0) })
			case "copy_grant":
				row(1)["copy_license_accepted"] = true
			case "missing_original":
				delete(row(63), "original_reader_assessment")
			case "resource_reuse_wrong_actor":
				for _, v := range row(3)["support_read_states"].([]any) {
					s := v.(map[string]any)
					if s["state"] == "I" && s["path"] == "skills/binance-skills-hub/binance-derivatives-trading-options/CHANGELOG.md" {
						s["representative"].(map[string]any)["reader_job"] = "other"
					}
				}
			case "resource_reuse_wrong_predecessor":
				for _, v := range row(3)["support_read_states"].([]any) {
					s := v.(map[string]any)
					if s["state"] == "I" && s["path"] == "skills/binance-skills-hub/binance-derivatives-trading-options/CHANGELOG.md" {
						s["representative"].(map[string]any)["reader_row"] = float64(1)
					}
				}
			}
			// Recompute only this test's local expected digest so semantic checks,
			// rather than the immutable production pin alone, must reject the claim.
			raw, err := json.Marshal(entries)
			if err != nil {
				t.Fatal(err)
			}
			h := sha256.Sum256(raw)
			a.Entries = raw
			a.EntrySHA = hex.EncodeToString(h[:])
			spec.EntrySHA = a.EntrySHA
			_, err = validateReceivedCohort(a, spec)
			if (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}
