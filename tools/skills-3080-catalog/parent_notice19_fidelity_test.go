package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

func TestParentNotice19RetainsVisibleRangesAndIndividualUnknowns(t *testing.T) {
	var outer map[string]any
	raw := loadPublicReceipt(t, "skills-3080-parent-notice19-evaluation175645.json", &outer)
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != "240674e96c94edd15e5a9bc9d468c5c6c33a7e2728ff6f801d8852a2bd8e56d0" {
		t.Fatal("independently prepared original19 fields or range proofs changed")
	}
	if outer["original_parent_snapshot_raw_sha256"] != "a9b03f0ce2ac4695cb57447eec6638236d339f5de984981286e36cb324fd29cf" || outer["all_original_parent_fields_preserved"] != true || outer["whole16_complete"] != false || outer["public_copy_created"] != false {
		t.Fatal("original snapshot or private-preparation boundary changed")
	}
	for _, key := range []string{"new_workflow_raw_credit", "new_permissions", "new_final_dispositions", "central_operations"} {
		if outer[key] != float64(0) {
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
	groupMetadata := 0.0
	physical, visible, masked, views, bytes := 0.0, 0.0, 0.0, 0, 0.0
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
		if a["reader_job"] != snapshot["assessor_job"] || a["all_visible_meanings_actually_read"] != true || a["body_SHA_bytes_blob_matched_to_selected_member"] != true || a["all_member_individual_context_rights_pending"] != true || a["individual_copy_permission_accepted"] != false || a["final_disposition"] != nil || a["source_or_support_scripts_executed"] != false || a["central_operations"] != float64(0) {
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
		groupMetadata += a["displayed_group_member_metadata_occurrences"].(float64)
		bytes += m["bytes"].(float64)
		r := received27Object(t, a["read"])
		if r["raw_full"] != false || r["unseen_meanings_remain_unknown"] != true {
			t.Fatal("masked original became full raw read")
		}
		first := 1.0
		vsum, msum := 0.0, 0.0
		for _, rv := range received27Array(t, r["ranges"]) {
			rangeValue := received27Object(t, rv)
			end := rangeValue["through"].(float64)
			if rangeValue["first"] != first || end < first {
				t.Fatal("primary visible ranges are not continuous")
			}
			withheld := received27Array(t, rangeValue["withheld_physical_lines"])
			positions := map[float64]bool{}
			for _, p := range withheld {
				n := p.(float64)
				if n < first || n > end || positions[n] {
					t.Fatal("original withheld physical position differs")
				}
				positions[n] = true
			}
			v := rangeValue["visible_lines"].(float64)
			if v+float64(len(withheld)) != end-first+1 {
				t.Fatal("visible and withheld positions do not conserve physical range")
			}
			vsum += v
			msum += float64(len(withheld))
			first = end + 1
			views++
		}
		if first-1 != r["total_physical_lines"] || vsum != r["visible_lines"] || msum != r["withheld_lines"] {
			t.Fatal("complete original visible-body coverage differs")
		}
		physical += first - 1
		visible += vsum
		masked += msum
		if a["use_value_and_findings"] == "" || a["positive"] == "" || a["negative"] == "" || a["required_changes_and_unmet"] == "" {
			t.Fatal("original semantic explanation missing")
		}
	}
	if nonRepresentative != 1 || groupMetadata != 22 || bytes != 91588 || physical != 2745 || visible != 2137 || masked != 608 || views != 27 {
		t.Fatal("19 selected bodies and22 metadata members were conflated")
	}
	notices := received27Array(t, snapshot["original_notice_contexts"])
	if len(notices) != 7 {
		t.Fatal("original7 notice contexts absent")
	}
	noticeBytes, noticePhysical, noticeWithheld := 0.0, 0.0, 0.0
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
		noticeBytes += s["bytes"].(float64)
		noticePhysical += n["physical_lines"].(float64)
		noticeWithheld += n["withheld_lines"].(float64)
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
	ranges, ok := n["ranges"].([]any)
	if !ok || len(ranges) == 0 {
		return errors.New("notice ranges absent")
	}
	first, totalWithheld := 1.0, 0.0
	for _, value := range ranges {
		r, ok := value.(map[string]any)
		if !ok {
			return errors.New("notice range not an object")
		}
		end, endOK := r["through"].(float64)
		visible, visibleOK := r["visible_lines"].(float64)
		withheld, withheldOK := r["withheld_physical_lines"].([]any)
		if !endOK || !visibleOK || !withheldOK || r["first"] != first || end < first {
			return errors.New("notice ranges not continuous")
		}
		positions := map[float64]bool{}
		for _, p := range withheld {
			line, ok := p.(float64)
			if !ok || line < first || line > end || positions[line] {
				return errors.New("notice withheld position invalid or repeated")
			}
			positions[line] = true
		}
		if visible < 0 || visible+float64(len(withheld)) != end-first+1 {
			return errors.New("notice visible/withheld range does not conserve lines")
		}
		totalWithheld += float64(len(withheld))
		first = end + 1
	}
	if n["physical_lines"] != first-1 || n["withheld_lines"] != totalWithheld {
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

func TestParent19NoticeAndBodyChecksRejectCounterexamples(t *testing.T) {
	member := func() map[string]any {
		return map[string]any{"inventory_id": "selected", "sha256": "same-source", "bytes": float64(7), "git_blob": "same-blob"}
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
		return map[string]any{"unseen_meanings_remain_unknown": true, "raw_full": false, "physical_lines": float64(3), "withheld_lines": float64(1), "ranges": []any{map[string]any{"first": float64(1), "through": float64(3), "visible_lines": float64(2), "withheld_physical_lines": []any{float64(2)}}}}
	}
	mutations := map[string]func(map[string]any){
		"missing": func(n map[string]any) { delete(n, "ranges") },
		"gap":     func(n map[string]any) { n["ranges"].([]any)[0].(map[string]any)["first"] = float64(2) },
		"out-of-range": func(n map[string]any) {
			n["ranges"].([]any)[0].(map[string]any)["withheld_physical_lines"] = []any{float64(4)}
		},
		"duplicate": func(n map[string]any) {
			n["ranges"].([]any)[0].(map[string]any)["withheld_physical_lines"] = []any{float64(2), float64(2)}
		},
		"visible": func(n map[string]any) { n["ranges"].([]any)[0].(map[string]any)["visible_lines"] = float64(3) },
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
