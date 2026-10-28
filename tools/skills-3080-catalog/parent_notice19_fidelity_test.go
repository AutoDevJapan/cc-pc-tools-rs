package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestParentNotice19RetainsVisibleRangesAndIndividualUnknowns(t *testing.T) {
	var payload json.RawMessage
	raw := loadPublicReceipt(t, "skills-3080-parent-notice19-evaluation175645.json", &payload)
	outer, err := parent19Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "240674e96c94edd15e5a9bc9d468c5c6c33a7e2728ff6f801d8852a2bd8e56d0" {
		t.Fatal("independently prepared original19 fields or range proofs changed")
	}
	if outer["original_parent_snapshot_raw_sha256"] != "a9b03f0ce2ac4695cb57447eec6638236d339f5de984981286e36cb324fd29cf" || outer["all_original_parent_fields_preserved"] != true || outer["whole16_complete"] != false || outer["public_copy_created"] != false {
		t.Fatal("original snapshot or private-preparation boundary changed")
	}
	for _, key := range []string{"new_workflow_raw_credit", "new_permissions", "new_final_dispositions", "central_operations"} {
		if parent19MustInteger(t, outer[key]) != 0 {
			t.Fatal("publication gained a read, permission, disposition or write")
		}
	}
	snapshot := received27Object(t, outer["original_parent_snapshot"])
	if snapshot["assessor_job"] != "20261027-175645-todo-3080" || snapshot["actual_model"] != "gpt-6.1-sol" || snapshot["actual_effort"] != "max" || snapshot["whole16_complete"] != false {
		t.Fatal("original parent actor or partial scope changed")
	}
	entries := received27Array(t, outer["entries"])
	assessments := received27Array(t, snapshot["assessments"])
	if len(entries) != 19 || len(assessments) != 19 {
		t.Fatal("exact19 selected workflow assessments required")
	}
	seen := map[string]bool{}
	nonRepresentative := 0
	var groupMetadata, physical, visible, masked, bodyBytes int64
	views := 0
	for i, value := range assessments {
		a := received27Object(t, value)
		m := received27Object(t, a["selected_notice_member"])
		e := received27Object(t, entries[i])
		id, ok := m["inventory_id"].(string)
		if !ok || seen[id] || e["inventory_id"] != id {
			t.Fatal("selected source identity repeated or changed")
		}
		seen[id] = true
		for _, pair := range [][2]string{{"source_repo", "repo"}, {"source_commit", "commit"}, {"path", "path"}, {"source_sha256", "sha256"}, {"git_blob", "git_blob"}, {"bytes", "bytes"}} {
			if !reflect.DeepEqual(e[pair[0]], m[pair[1]]) {
				t.Fatal("execution projection differs from original source")
			}
		}
		if a["reader_job"] != snapshot["assessor_job"] || a["all_visible_meanings_actually_read"] != true || a["body_SHA_bytes_blob_matched_to_selected_member"] != true || a["all_member_individual_context_rights_pending"] != true || a["individual_copy_permission_accepted"] != false || a["final_disposition"] != nil || a["source_or_support_scripts_executed"] != false || parent19MustInteger(t, a["central_operations"]) != 0 {
			t.Fatal("body meaning promoted to individual rights or execution")
		}
		if !parent19SameDisplayedBody(a) {
			t.Fatal("selected and displayed member body tuples differ")
		}
		if a["selected_member_is_representative"] == false {
			nonRepresentative++
		} else if a["selected_member_is_representative"] != true {
			t.Fatal("original representative association absent")
		}
		groupMetadata += parent19MustInteger(t, a["displayed_group_member_metadata_occurrences"])
		bodyBytes += parent19MustInteger(t, m["bytes"])
		r := received27Object(t, a["read"])
		if r["raw_full"] != false || r["unseen_meanings_remain_unknown"] != true {
			t.Fatal("masked original became full raw read")
		}
		first := int64(1)
		var vsum, msum int64
		for _, rv := range received27Array(t, r["ranges"]) {
			rangeValue := received27Object(t, rv)
			end, endOK := parent19PhysicalNumber(rangeValue["through"])
			start, startOK := parent19PhysicalNumber(rangeValue["first"])
			if !endOK || !startOK || start != first || end < first {
				t.Fatal("primary visible ranges are not continuous")
			}
			withheld := received27Array(t, rangeValue["withheld_physical_lines"])
			positions := map[int64]bool{}
			for _, p := range withheld {
				n, ok := parent19PhysicalNumber(p)
				if !ok || n < first || n > end || positions[n] {
					t.Fatal("original withheld physical position differs")
				}
				positions[n] = true
			}
			v, ok := parent19PhysicalNumber(rangeValue["visible_lines"])
			if !ok || v+int64(len(withheld)) != end-first+1 {
				t.Fatal("visible and withheld positions do not conserve physical range")
			}
			vsum += v
			msum += int64(len(withheld))
			first = end + 1
			views++
		}
		if first-1 != parent19MustInteger(t, r["total_physical_lines"]) || vsum != parent19MustInteger(t, r["visible_lines"]) || msum != parent19MustInteger(t, r["withheld_lines"]) {
			t.Fatal("complete original visible-body coverage differs")
		}
		physical += first - 1
		visible += vsum
		masked += msum
		if a["use_value_and_findings"] == "" || a["positive"] == "" || a["negative"] == "" || a["required_changes_and_unmet"] == "" {
			t.Fatal("original semantic explanation missing")
		}
	}
	if nonRepresentative != 1 || groupMetadata != 22 || bodyBytes != 91588 || physical != 2745 || visible != 2137 || masked != 608 || views != 27 {
		t.Fatal("19 selected bodies and22 metadata members were conflated")
	}
	notices := received27Array(t, snapshot["original_notice_contexts"])
	if len(notices) != 7 {
		t.Fatal("original7 notice contexts absent")
	}
	var noticeBytes, noticePhysical, noticeWithheld int64
	noticeViews := 0
	for _, v := range notices {
		n := received27Object(t, v)
		s := received27Object(t, n["source_tuple"])
		if n["reader_job"] != snapshot["assessor_job"] || n["all_visible_notice_meaning_read"] != true || n["source_or_service_permission_not_added_by_read"] != true {
			t.Fatal("notice reading gained blanket permission")
		}
		if err := parent19NoticeRanges(n); err != nil {
			t.Fatal(err)
		}
		noticeBytes += parent19MustInteger(t, s["bytes"])
		noticePhysical += parent19MustInteger(t, n["physical_lines"])
		noticeWithheld += parent19MustInteger(t, n["withheld_lines"])
		noticeViews += len(received27Array(t, n["ranges"]))
	}
	if noticeBytes != 44745 || noticePhysical != 855 || noticeWithheld != 10 || noticeViews != 11 {
		t.Fatal("original notice read limits changed")
	}
	for _, file := range []string{"skills-3080-primaryflow47-evaluation175645.json", "skills-3080-primaryflow-next36-evaluation175645.json"} {
		var previous map[string]any
		loadPublicReceipt(t, file, &previous)
		for _, v := range received27Array(t, previous["entries"]) {
			if seen[received27Object(t, v)["inventory_id"].(string)] {
				t.Fatal("a prior assessment was counted again as a new selected19")
			}
		}
	}
}

func parent19SameDisplayedBody(a map[string]any) bool {
	selected, ok := a["selected_notice_member"].(map[string]any)
	if !ok {
		return false
	}
	displayed, ok := a["displayed_representative"].(map[string]any)
	if !ok {
		return false
	}
	for _, key := range []string{"sha256", "bytes", "git_blob"} {
		if selected[key] == nil || !reflect.DeepEqual(selected[key], displayed[key]) {
			return false
		}
	}
	flag, ok := a["selected_member_is_representative"].(bool)
	return ok && selected["inventory_id"] != nil && displayed["inventory_id"] != nil && flag == (selected["inventory_id"] == displayed["inventory_id"])
}

func parent19NoticeRanges(n map[string]any) error {
	physical, physicalOK := parent19PhysicalNumber(n["physical_lines"])
	withheldTotal, withheldTotalOK := parent19PhysicalNumber(n["withheld_lines"])
	if !physicalOK || !withheldTotalOK {
		return errors.New("notice whole counts not nonnegative integers")
	}
	ranges, ok := n["ranges"].([]any)
	if !ok || len(ranges) == 0 {
		return errors.New("notice ranges absent")
	}
	first, totalWithheld := int64(1), int64(0)
	for _, value := range ranges {
		r, ok := value.(map[string]any)
		if !ok {
			return errors.New("notice range not an object")
		}
		end, endOK := parent19PhysicalNumber(r["through"])
		visible, visibleOK := parent19PhysicalNumber(r["visible_lines"])
		start, startOK := parent19PhysicalNumber(r["first"])
		withheld, withheldOK := r["withheld_physical_lines"].([]any)
		if !endOK || !visibleOK || !startOK || !withheldOK || start != first || end < first {
			return errors.New("notice ranges not continuous")
		}
		positions := map[int64]bool{}
		for _, p := range withheld {
			line, ok := parent19PhysicalNumber(p)
			if !ok || line < first || line > end || positions[line] {
				return errors.New("notice withheld position invalid or repeated")
			}
			positions[line] = true
		}
		if visible < 0 || visible+int64(len(withheld)) != end-first+1 {
			return errors.New("notice visible/withheld range does not conserve lines")
		}
		totalWithheld += int64(len(withheld))
		first = end + 1
	}
	if physical != first-1 || withheldTotal != totalWithheld {
		return errors.New("notice whole coverage differs")
	}
	unknown, ok := n["unseen_meanings_remain_unknown"].(bool)
	if !ok || totalWithheld > 0 && !unknown {
		return errors.New("unread notice meanings changed to known")
	}
	raw, ok := n["raw_full"].(bool)
	if !ok || raw && totalWithheld != 0 {
		return errors.New("withheld notice became full raw read")
	}
	return nil
}

// Keep the JSON token until its integer representation has been checked. A
// float64 has already lost the evidence needed to reject a tiny fraction.
func parent19PhysicalNumber(value any) (int64, bool) {
	token, ok := value.(json.Number)
	if !ok {
		return 0, false
	}
	n, err := token.Int64()
	return n, err == nil && n >= 0 && n <= 1<<53-1
}

func parent19MustInteger(t *testing.T, value any) int64 {
	t.Helper()
	n, ok := parent19PhysicalNumber(value)
	if !ok {
		t.Fatal("original count or position is not an exact nonnegative integer")
	}
	return n
}

func parent19Decode(raw []byte) (map[string]any, error) {
	var result map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&result); err != nil {
		return nil, err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, errors.New("trailing JSON after original parent receipt")
	}
	return result, nil
}

func TestParent19RawJSONFractionalLinesCannotRoundToIntegers(t *testing.T) {
	raw := `{"unseen_meanings_remain_unknown":true,"raw_full":false,"physical_lines":3,"withheld_lines":1,"ranges":[{"first":1,"through":3,"visible_lines":2,"withheld_physical_lines":[2]}]}`
	n, err := parent19Decode([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if err := parent19NoticeRanges(n); err != nil {
		t.Fatal("positive exact JSON integer receipt rejected", err)
	}
	for _, pair := range [][2]string{
		{`"physical_lines":3`, `"physical_lines":3.0000000000000001`},
		{`"first":1`, `"first":1.00000000000000001`},
		{`"through":3`, `"through":3.0000000000000001`},
		{`"visible_lines":2`, `"visible_lines":2.0000000000000001`},
		{`"withheld_lines":1`, `"withheld_lines":1.00000000000000001`},
		{`"withheld_physical_lines":[2]`, `"withheld_physical_lines":[2.0000000000000001]`},
	} {
		mutated, err := parent19Decode([]byte(strings.Replace(raw, pair[0], pair[1], 1)))
		if err != nil || parent19NoticeRanges(mutated) == nil {
			t.Fatal("valid JSON tiny fraction was not rejected by exact range validation", pair[0], err)
		}
	}
}

func TestParent19NoticeAndBodyChecksRejectCounterexamples(t *testing.T) {
	member := func() map[string]any {
		return map[string]any{"inventory_id": "selected", "sha256": "same-source", "bytes": json.Number("7"), "git_blob": "same-blob"}
	}
	for _, key := range []string{"sha256", "bytes", "git_blob", "inventory_id"} {
		t.Run("body-"+key, func(t *testing.T) {
			a := map[string]any{"selected_notice_member": member(), "displayed_representative": member(), "selected_member_is_representative": true}
			if !parent19SameDisplayedBody(a) {
				t.Fatal("positive identical member rejected")
			}
			a["displayed_representative"].(map[string]any)[key] = "changed"
			if parent19SameDisplayedBody(a) {
				t.Fatal("different selected/displayed body or identity accepted")
			}
		})
	}
	fixture := func() map[string]any {
		return map[string]any{"unseen_meanings_remain_unknown": true, "raw_full": false, "physical_lines": json.Number("3"), "withheld_lines": json.Number("1"), "ranges": []any{map[string]any{"first": json.Number("1"), "through": json.Number("3"), "visible_lines": json.Number("2"), "withheld_physical_lines": []any{json.Number("2")}}}}
	}
	mutations := map[string]func(map[string]any){
		"nonfinite":       func(n map[string]any) { n["physical_lines"] = json.Number("Infinity") },
		"nan":             func(n map[string]any) { n["withheld_lines"] = json.Number("NaN") },
		"inexact-integer": func(n map[string]any) { n["physical_lines"] = json.Number("9007199254740992") },
		"fractional-position": func(n map[string]any) {
			n["ranges"].([]any)[0].(map[string]any)["withheld_physical_lines"] = []any{json.Number("2.5")}
		},
		"fractional-balanced-range": func(n map[string]any) {
			r := n["ranges"].([]any)[0].(map[string]any)
			r["through"], r["visible_lines"] = json.Number("3.5"), json.Number("2.5")
			r["withheld_physical_lines"] = []any{json.Number("2.5")}
			n["physical_lines"] = json.Number("3.5")
		},
		"missing": func(n map[string]any) { delete(n, "ranges") },
		"gap":     func(n map[string]any) { n["ranges"].([]any)[0].(map[string]any)["first"] = json.Number("2") },
		"out-of-range": func(n map[string]any) {
			n["ranges"].([]any)[0].(map[string]any)["withheld_physical_lines"] = []any{json.Number("4")}
		},
		"duplicate": func(n map[string]any) {
			n["ranges"].([]any)[0].(map[string]any)["withheld_physical_lines"] = []any{json.Number("2"), json.Number("2")}
		},
		"visible": func(n map[string]any) { n["ranges"].([]any)[0].(map[string]any)["visible_lines"] = json.Number("3") },
		"unknown": func(n map[string]any) { n["unseen_meanings_remain_unknown"] = false },
		"raw":     func(n map[string]any) { n["raw_full"] = true },
	}
	for name, mutate := range mutations {
		t.Run("notice-"+name, func(t *testing.T) {
			n := fixture()
			if err := parent19NoticeRanges(n); err != nil {
				t.Fatal(err)
			}
			mutate(n)
			if parent19NoticeRanges(n) == nil {
				t.Fatal("broken notice preservation accepted")
			}
		})
	}
}
