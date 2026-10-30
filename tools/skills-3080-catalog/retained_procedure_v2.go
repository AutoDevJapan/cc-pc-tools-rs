package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

const retainedProcedureV2File = "skills-3080-retained-procedure-assessments-v2.json"
const retainedProcedureV2Domain = "skills3080-retained-procedure-assessment-journal-v2"

// Validate the additional observations before adapting their common fields to
// the existing read-state validator. The accepted earlier journal stays intact.
func validateRetainedProcedureV2(v map[string]any, expectedRows int) error {
	fail := func() error { return fmt.Errorf("additional retained procedure read or history boundary differs") }
	if !retainedKeys(v, "domain", "catalog_sha256", "assessment_records", "counts", "entries", "initial_display_limitations", "all_source_and_support_rights_confirmed", "new_copy_grants", "new_final_dispositions", "central_operations", "whole16_complete", "scope") || v["domain"] != retainedProcedureV2Domain {
		return fail()
	}
	rows, ok := v["entries"].([]any)
	if !ok || len(rows) == 0 {
		return fail()
	}
	adaptedRows := make([]any, 0, len(rows))
	ids := map[string]bool{}
	for _, x := range rows {
		row, ok := x.(map[string]any)
		if !ok {
			return fail()
		}
		id, _ := row["inventory_id"].(string)
		ids[id] = true
		read, ok := row["read"].(map[string]any)
		if !ok || !retainedKeys(read, "kind", "physical_lines", "masked_lines", "withheld_physical_lines", "recorded_mask_version", "raw_read_complete", "workflow_view_complete", "ordered_view_complete", "visible_range_digest_provided", "encoding_exception", "withheld_meanings_not_treated_as_complete") {
			return fail()
		}
		version, ok := retainedInteger(read["recorded_mask_version"])
		if !ok {
			return fail()
		}
		switch version {
		case 16, 17, 18, 23, 24, 25, 28, 29, 32, 33:
		default:
			return fail()
		}
		if err := validateRetainedEncoding(row, read); err != nil {
			return err
		}
		r := map[string]any{}
		for k, x := range row {
			r[k] = x
		}
		rr := map[string]any{}
		for k, x := range read {
			if k != "encoding_exception" {
				rr[k] = x
			}
		}
		// This does not change the published read claim; only common validation uses
		// an existing profile token after the actual version has been checked above.
		rr["recorded_mask_version"] = json.Number("16")
		rr["initial_withheld_display_gaps"] = []any{}
		rr["final_withheld_unread_except_reported_initial_gaps"] = true
		r["read"] = rr
		adaptedRows = append(adaptedRows, r)
	}
	limits, ok := v["initial_display_limitations"].([]any)
	if !ok || len(limits) != 6 {
		return fail()
	}
	for _, x := range limits {
		m, ok := x.(map[string]any)
		if !ok || !retainedKeys(m, "inventory_ids", "source_field", "reported_limit", "scope_note") || !retainedText(m["scope_note"]) {
			return fail()
		}
		listed, ok := m["inventory_ids"].([]any)
		if !ok || len(listed) == 0 {
			return fail()
		}
		seen := map[string]bool{}
		for _, x := range listed {
			id, ok := x.(string)
			if !ok || !ids[id] || seen[id] {
				return fail()
			}
			seen[id] = true
		}
		// Exact initial-display observations are independently anchored below; do
		// not allow a new matching artifact pin to erase a historical limitation.
		if !retainedPublicLimitValue(m["reported_limit"]) {
			return fail()
		}
	}
	if !retainedInitialLimitAnchor(limits) {
		return fail()
	}
	adapted := map[string]any{}
	for k, x := range v {
		if k != "initial_display_limitations" {
			adapted[k] = x
		}
	}
	adapted["domain"] = retainedProcedureDomain
	adapted["entries"] = adaptedRows
	counts, ok := v["counts"].(map[string]any)
	if !ok {
		return fail()
	}
	cc := map[string]any{}
	for k, x := range counts {
		cc[k] = x
	}
	cc["initial_display_gap_records"] = json.Number("0")
	adapted["counts"] = cc
	if !retainedFullEntryAnchor(rows) {
		return fail()
	}
	if !retainedFontNoticeAnchor(rows) {
		return fail()
	}
	return validateRetainedProcedureCommon(adapted, expectedRows, func(rel string) bool {
		return noticeEvidencePath(rel) || (strings.HasPrefix(rel, "canvas-fonts/") && strings.HasSuffix(rel, "-OFL.txt"))
	})
}

const retainedFontNoticesSHA = "07846b7fb48d02310a20605b60ed89eef45cb15b74cfdbb213554b91f808017e"

func retainedFontNoticeAnchor(rows []any) bool {
	notices := []any{}
	for _, x := range rows {
		r, ok := x.(map[string]any)
		if !ok {
			return false
		}
		items, ok := r["individual_notices"].([]any)
		if !ok {
			return false
		}
		for _, x := range items {
			n, ok := x.(map[string]any)
			if !ok {
				return false
			}
			rel, _ := n["relative_path"].(string)
			if strings.HasPrefix(rel, "canvas-fonts/") {
				notices = append(notices, map[string]any{"inventory_id": r["inventory_id"], "notice": n})
			}
		}
	}
	if len(notices) != 54 {
		return false
	}
	b, e := json.Marshal(notices)
	if e != nil {
		return false
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]) == retainedFontNoticesSHA
}

func retainedPublicLimitValue(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, v := range x {
			if k == "row" || k == "reader_job" || k == "reader_namespace" || k == "parent_receipt" {
				return false
			}
			if !retainedPublicLimitValue(v) {
				return false
			}
		}
	case []any:
		for _, v := range x {
			if !retainedPublicLimitValue(v) {
				return false
			}
		}
	case string:
		return retainedText(x)
	case json.Number:
		_, ok := retainedInteger(x)
		return ok
	case bool, nil:
	default:
		return false
	}
	return true
}

// Filled from the separately preserved, reviewed observation snapshot, rather
// than from the artifact's own count or hash declaration.
const retainedInitialLimitSHA = "94a5ceb171fa5dafbf4aeebf44a1996aa6cb114ec7590a84d916ae96fc1ea85f"

func retainedInitialLimitAnchor(limits []any) bool {
	b, e := json.Marshal(limits)
	if e != nil {
		return false
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]) == retainedInitialLimitSHA
}

func validateRetainedEncoding(row, read map[string]any) error {
	fail := func() error {
		return fmt.Errorf("retained encoding exception differs from its source and withheld lines")
	}
	const source = "d130c70c7cb292d82c7d58763338ac686609cb7b62cc635416a9caca5f530694"
	special := row["source_sha256"] == source
	if read["encoding_exception"] == nil {
		if special {
			return fail()
		}
		return nil
	}
	if !special || row["path"] != "skills/theshadowrose/prompt-git/SKILL.md" || row["git_blob"] != "71ef8d26ce5bb4f4bb0c75ded665d20ef50ec730" || row["bytes"] != json.Number("13925") || read["kind"] != "direct_masked_full" || read["raw_read_complete"] != false {
		return fail()
	}
	ex, ok := read["encoding_exception"].(map[string]any)
	const method = "この固定本文だけ、原bufferをLF単位へ分け各行fatal UTF8復号。不正行全体は表示前に伏せ、意味未読。別encoding/置換復号/再取得/原本変更0。"
	if !ok || !retainedKeys(ex, "strict_utf8", "invalid_bytes", "permissive_decode_not_used", "source_refetched", "method") || ex["strict_utf8"] != false || ex["permissive_decode_not_used"] != true || ex["source_refetched"] != false || ex["method"] != method {
		return fail()
	}
	withheld, ok := read["withheld_physical_lines"].([]any)
	if !ok {
		return fail()
	}
	hidden := map[int64]bool{}
	for _, v := range withheld {
		n, ok := retainedInteger(v)
		if !ok {
			return fail()
		}
		hidden[n] = true
	}
	invalid, ok := ex["invalid_bytes"].([]any)
	if !ok || len(invalid) != 9 {
		return fail()
	}
	offsets := []int64{723, 3493, 3515, 3554, 3576, 10769, 10898, 11311, 13020}
	lines := []int64{18, 121, 121, 122, 122, 406, 412, 432, 465}
	for i, x := range invalid {
		m, ok := x.(map[string]any)
		if !ok || !retainedKeys(m, "byte_offset_zero_based", "invalid_byte_hex", "line") {
			return fail()
		}
		off, a := retainedInteger(m["byte_offset_zero_based"])
		line, b := retainedInteger(m["line"])
		byteText, stringOK := m["invalid_byte_hex"].(string)
		if !a || !b || !stringOK || byteText != "97" || off != offsets[i] || line != lines[i] || !hidden[line] {
			return fail()
		}
	}
	return nil
}

const retainedFullEntrySHA = "66933ba23a27a083a2060407a45a8f267255ed34170d72074306e13741376783"

func retainedFullEntryAnchor(rows []any) bool {
	b, e := json.Marshal(rows)
	if e != nil {
		return false
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]) == retainedFullEntrySHA
}
