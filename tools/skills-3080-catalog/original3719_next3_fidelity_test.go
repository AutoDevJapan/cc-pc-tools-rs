package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Pending source assessments retain original observations and actor namespaces.
// This test never reopens upstream bodies or promotes them into copy grants.
func TestOriginal3719Next3PreservesRecoveryPacketAndOriginalActors(t *testing.T) {
	var value map[string]any
	raw := loadPublicReceipt(t, "skills-3080-original3719-next3-207209-evaluation175645.json", &value)
	if travel3Digest(raw) != "80f9eab15b84bd88006cde9d5a61dc438be2fb83259f9accac271ac057ddd442" || value["domain"] != "skills3080-original3719-parent-shared-snapshot-v1" || value["publisher_job"] != "20261027-175645-todo-3080" || value["original_journal_parent"] != "20261024-235901-todo-3080" || value["after"] != float64(206) || value["through"] != float64(209) || value["whole16_complete"] != false {
		t.Fatal("original three pending observations or scope changed")
	}
	for _, key := range []string{"new_source_or_support_reads_by_materializer", "new_parent_raw_body_read_credit", "new_copy_permissions", "new_final_dispositions", "central_operations"} {
		if value[key] != float64(0) {
			t.Fatal("transport gained reads, permissions, decisions or central writes")
		}
	}
	records := received27Array(t, value["records"])
	entries := received27Array(t, value["entries"])
	contexts := received27Object(t, value["shared_original_contexts"])
	if len(records) != 3 || len(entries) != 3 || len(contexts) != 1 {
		t.Fatal("exact original three/one chunk membership differs")
	}
	groups := map[string][]any{}
	ids := map[string]bool{}
	supports, saved, unsaved := 0, 0, 0
	for i, item := range records {
		r := received27Object(t, item)
		p := received27Object(t, entries[i])
		a := received27Object(t, r["original_assessment"])
		source := received27Array(t, a["source"])
		id, ok := r["inventory_id"].(string)
		if !ok || ids[id] || len(source) != 5 || source[0] != id || a["row"] != float64(207+i) || r["ordinal"] != a["row"] || r["actual_assessment_reader"] != "20261028-171959-todo-3719" || r["new_final_disposition"] != false || r["current_permission_or_adoption_granted"] != false {
			t.Fatal("original identity, sequence, reader or pending status changed")
		}
		ids[id] = true
		for _, key := range []string{"inventory_id", "source_repo", "source_commit", "path", "source_sha256", "git_blob", "bytes"} {
			if !reflect.DeepEqual(r[key], p[key]) {
				t.Fatal("execution projection differs from original observation")
			}
		}
		if r["source_repo"] != "6mile/skills" || r["source_commit"] != "920714f17d81e0a95c7a9bc51faf0a7921cd982d" || source[1] != r["path"] || source[2] != r["source_sha256"] || source[3] != r["git_blob"] || source[4] != r["bytes"] {
			t.Fatal("fixed source tuple differs")
		}
		sha, ok := r["original_chunk_sha256"].(string)
		if !ok || !validHex(sha, 64) {
			t.Fatal("whole original chunk reference absent")
		}
		groups[sha] = append(groups[sha], a)
		for _, item := range received27Array(t, a["support"]) {
			tuple := received27Array(t, item)
			if len(tuple) != 8 {
				t.Fatal("support observation tuple changed")
			}
			supports++
			if tuple[4] == float64(0) {
				unsaved++
				for _, index := range []int{1, 2, 5, 6} {
					if tuple[index] != nil {
						t.Fatal("unsaved support acquired byte/hash/mask/reader evidence")
					}
				}
			} else {
				saved++
			}
		}
	}
	for sha, originals := range groups {
		context := received27Object(t, contexts[sha])
		common := received27Object(t, context["common"])
		whole := map[string]any{}
		for key, v := range common {
			whole[key] = v
		}
		whole["entries"] = originals
		encoded, err := json.Marshal(whole)
		if err != nil || travel3Digest(encoded) != sha || common["reader_job"] != "20261028-171959-todo-3719" || common["roster_sha256"] != "f9c6b2e44c5e3aa8947f758f864eaa0d5edc396d622359826bca848cd96f9901" || context["original_fields_losslessly_reconstructed"] != true {
			t.Fatal("complete original packet or namespaces changed")
		}
		boundary := received27Object(t, common["boundaries"])
		if boundary["source_executed"] != false || boundary["copy_license_accepted"] != false || boundary["new_final_dispositions"] != float64(0) || boundary["central_operations"] != float64(0) {
			t.Fatal("original assessment gained authority or execution")
		}
		ack := received27Object(t, context["independent_parent_journal_record"])
		meaning := received27Object(t, context["parent_meaning"])
		nativeSHA, nativeOK := meaning["actual_native_sha256"].(string)
		if ack["chunk_sha256"] != sha || ack["reader_job"] != common["reader_job"] || ack["parent_semantic_assessments_read"] != true || meaning["chunk_sha256"] != sha || meaning["producer_job"] != common["reader_job"] || meaning["consumer_job"] != "20261027-175645-todo-3080" || meaning["full_original_meanings_and_all_limits_read"] != true || !nativeOK || !validHex(nativeSHA, 64) || context["independent_native_metadata_sha256"] != nativeSHA {
			t.Fatal("independent parent read no longer binds this exact packet")
		}
	}
	if supports != 38 || saved != 16 || unsaved != 22 {
		t.Fatal("original support states or nulls changed")
	}
}
