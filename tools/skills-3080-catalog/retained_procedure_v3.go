package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

const retainedProcedureV3File = "skills-3080-retained-procedure-assessments-v3.json"
const retainedProcedureV3Domain = "skills3080-retained-procedure-assessment-journal-v3"
const retainedProcedureV3Scope = "固定保存資料の可視意味評価。本文と支持の実読・再利用・未読、個別権利の未確定を区別し、支持一覧は本文自身を含む。実行用の認証clientを利用許諾条文とせず、伏字を残す通知の順序付き差分と支持資料の初回表示不足を保持する。担当と独立受領の来歴は非公開原記録に保持する。"

const retainedV3EntriesSHA = "c4d1996e7e5fb33e86b9b8166a11f90d8cdb4e5b4c0cfbbf7ba29bda76ec0da5"
const retainedV3HistorySHA = "c6cb667f2be5f999ee9e0f31a5fbb5911f0b6f6913e1827e402e90358c9e9327"

func retainedV3Anchor(value any, want string) bool {
	b, err := json.Marshal(value)
	if err != nil {
		return false
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:]) == want
}

// This new tranche adapts only its validated presentation fields. Earlier
// journals, raw/source claims and permission boundaries are unchanged.
func validateRetainedProcedureV3(v map[string]any, expectedRows int) error {
	fail := func() error { return fmt.Errorf("retained procedure v3 read/history boundary differs") }
	if !retainedKeys(v, "domain", "catalog_sha256", "assessment_records", "counts", "entries", "initial_display_limitations", "notice_read_exceptions", "all_source_and_support_rights_confirmed", "new_copy_grants", "new_final_dispositions", "central_operations", "whole16_complete", "scope") || v["domain"] != retainedProcedureV3Domain || v["scope"] != retainedProcedureV3Scope {
		return fail()
	}
	rows, ok := v["entries"].([]any)
	if !ok || len(rows) != 91 || expectedRows != 91 {
		return fail()
	}
	limits, ok := v["initial_display_limitations"].([]any)
	if !ok || len(limits) != 1 || !retainedPublicLimitValue(limits) {
		return fail()
	}
	exceptions, ok := v["notice_read_exceptions"].([]any)
	if !ok || len(exceptions) != 2 || !retainedPublicLimitValue(exceptions) {
		return fail()
	}
	// Artifact hashes alone cannot protect the accepted read history: a caller
	// could coherently rewrite both records/counters and the mutable artifact pin.
	// These separately fixed snapshots preserve every meaning and read/notice
	// claim, including the runtime-client non-grant and initially unmasked gap.
	if !retainedV3Anchor(rows, retainedV3EntriesSHA) || !retainedV3Anchor(map[string]any{"initial_display_limitations": limits, "notice_read_exceptions": exceptions}, retainedV3HistorySHA) {
		return fail()
	}
	adaptedRows := make([]any, 0, len(rows))
	for _, value := range rows {
		row, ok := value.(map[string]any)
		if !ok {
			return fail()
		}
		read, ok := row["read"].(map[string]any)
		if !ok || !retainedKeys(read, "kind", "physical_lines", "masked_lines", "withheld_physical_lines", "recorded_mask_version", "raw_read_complete", "workflow_view_complete", "ordered_view_complete", "visible_range_digest_provided", "encoding_exception", "withheld_meanings_not_treated_as_complete") || read["encoding_exception"] != nil {
			return fail()
		}
		version, ok := retainedInteger(read["recorded_mask_version"])
		if !ok || version < 33 || version > 37 {
			return fail()
		}
		copyRow, copyRead := map[string]any{}, map[string]any{}
		for k, x := range row {
			copyRow[k] = x
		}
		for k, x := range read {
			if k != "encoding_exception" {
				copyRead[k] = x
			}
		}
		copyRead["recorded_mask_version"] = json.Number("16")
		copyRead["initial_withheld_display_gaps"] = []any{}
		copyRead["final_withheld_unread_except_reported_initial_gaps"] = true
		copyRow["read"] = copyRead
		adaptedRows = append(adaptedRows, copyRow)
	}
	adapted := map[string]any{}
	for k, x := range v {
		if k != "initial_display_limitations" && k != "notice_read_exceptions" {
			adapted[k] = x
		}
	}
	adapted["domain"] = retainedProcedureDomain
	adapted["entries"] = adaptedRows
	counts, ok := v["counts"].(map[string]any)
	if !ok {
		return fail()
	}
	copyCounts := map[string]any{}
	for k, x := range counts {
		copyCounts[k] = x
	}
	copyCounts["initial_display_gap_records"] = json.Number("0")
	adapted["counts"] = copyCounts
	return validateRetainedProcedureCommon(adapted, expectedRows, func(rel string) bool { return noticeEvidencePath(rel) || rel == "license-client.mjs" })
}
