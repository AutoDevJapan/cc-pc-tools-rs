package main

import (
	"cc-pc-tools-rs/skills-3080-catalog/internal/primaryflow"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestPrimaryflow47RetainsOriginalMeaningAndPendingMemberRights(t *testing.T) {
	checkPrimaryflowOriginalMeaning(t, "skills-3080-primaryflow47-evaluation175645.json", "7c2d4404e6b10589b037a5dd93955e88f16eb8c78d36d991cfd56c35807fbe31", 47, 10, 57)
}

func TestPrimaryflowNext36RetainsOriginalMeaningAndPendingMemberRights(t *testing.T) {
	checkPrimaryflowOriginalMeaning(t, "skills-3080-primaryflow-next36-evaluation175645.json", "19ee72178b3b7e79feb9dd6521de0ab801ed3aad163c425c5a588a06fd97ccb7", 36, 8, 39)
}

func checkPrimaryflowOriginalMeaning(t *testing.T, file, expectedSHA string, recordsExpected, contextsExpected, membersExpected int) {
	t.Helper()
	var outer map[string]any
	raw := loadPublicReceipt(t, file, &outer)
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != expectedSHA {
		t.Fatal("independently materialized accepted snapshot differs")
	}
	if outer["all_original_packets_losslessly_reconstructed"] != true || outer["all_member_contexts_read"] != false || outer["all_member_rights_confirmed"] != false || outer["whole16_complete"] != false {
		t.Fatal("body assessment gained context, rights, or whole-task completion")
	}
	entries := received27Array(t, outer["entries"])
	records := received27Array(t, outer["records"])
	if len(entries) != recordsExpected || len(records) != recordsExpected {
		t.Fatal("representative record count differs")
	}
	groups := map[string][]any{}
	seen := map[string]bool{}
	for i, value := range records {
		r := received27Object(t, value)
		projection := received27Object(t, entries[i])
		original := received27Object(t, r["original_assessment"])
		id, ok := r["inventory_id"].(string)
		if !ok || seen[id] || id != original["representative_inventory_id"] {
			t.Fatal("representative inferred, repeated, or absent")
		}
		seen[id] = true
		for _, key := range []string{"inventory_id", "source_repo", "source_commit", "path", "source_sha256", "git_blob", "bytes"} {
			if !reflect.DeepEqual(r[key], projection[key]) {
				t.Fatal("execution projection differs from accepted source")
			}
		}
		if r["representative_body_meaning_only"] != true || r["all_member_contexts_and_individual_rights_pending"] != true || r["new_final_disposition"] != false || original["each_member_context_and_rights_pending"] != true || original["status"] != "visible_primary_evaluated" {
			t.Fatal("unread member context or rights promoted")
		}
		if r["source_sha256"] != original["body_sha256"] || r["bytes"] != original["body_bytes"] || r["git_blob"] != original["source_git_blob"] {
			t.Fatal("representative tuple differs")
		}
		read := received27Object(t, original["read"])
		if read["raw_full"] != false || read["unseen_meanings_remain_unknown"] != true {
			t.Fatal("original conservative read profile changed")
		}
		sha, ok := r["original_packet_sha256"].(string)
		if !ok {
			t.Fatal("original packet pointer missing")
		}
		groups[sha] = append(groups[sha], original)
	}
	contexts := received27Object(t, outer["shared_original_contexts"])
	if len(contexts) != contextsExpected || len(groups) != contextsExpected {
		t.Fatal("whole accepted packet membership differs")
	}
	members := 0
	for sha, originals := range groups {
		context := received27Object(t, contexts[sha])
		common := received27Object(t, context["common"])
		ack := received27Object(t, context["independent_parent_ack"])
		whole := map[string]any{}
		for key, value := range common {
			whole[key] = value
		}
		whole["entries"] = originals
		if received27Digest(t, whole) != sha || ack["packet_sha256"] != sha || ack["packet_id"] != common["packet_id"] || ack["reader_job"] != common["reader_job"] || ack["whole16_complete"] != false {
			t.Fatal("complete original packet/actor/ACK changed")
		}
		boundary := received27Object(t, common["boundaries"])
		if boundary["source_executed"] != false {
			t.Fatal("source was promoted to execution")
		}
		for _, key := range []string{"copy_grants", "final_dispositions", "central_operations", "new_global_raw_credit", "support_body_reads"} {
			if boundary[key] != float64(0) {
				t.Fatal("transport gained side effects or reading credit")
			}
		}
		bound := received27Object(t, context["member_metadata"])
		for _, value := range originals {
			original := received27Object(t, value)
			ordinal := fmt.Sprint(original["ordinal"])
			list := received27Array(t, bound[ordinal])
			// The fixed plan hashes its exported ordered Member wire type, not
			// sorted generic-object JSON. Reuse that format without re-reading bodies.
			encoded, err := json.Marshal(list)
			if err != nil {
				t.Fatal(err)
			}
			var typed []primaryflow.Member
			if err = json.Unmarshal(encoded, &typed); err != nil {
				t.Fatal(err)
			}
			encoded, err = json.Marshal(typed)
			if err != nil {
				t.Fatal(err)
			}
			if primaryflow.Hash(encoded) != original["member_metadata_sha256"] || float64(len(list)) != original["member_occurrences"] {
				t.Fatal("fixed member metadata differs")
			}
			found := 0
			for _, value := range list {
				member := received27Object(t, value)
				if len(member) != 8 {
					t.Fatal("fixed member fields lost or expanded")
				}
				if member["sha256"] != original["body_sha256"] || member["bytes"] != original["body_bytes"] {
					t.Fatal("same-body membership inferred from name")
				}
				if member["inventory_id"] == original["representative_inventory_id"] {
					found++
				}
			}
			if found != 1 {
				t.Fatal("representative is not a unique fixed member")
			}
			members += len(list)
		}
	}
	if members != membersExpected {
		t.Fatal("metadata aliases became individual context reads")
	}
	correction := received27Object(t, outer["historical_parent_arithmetic_correction"])
	if correction["correct_aggregate_physical"] != float64(1143) || correction["correct_aggregate_visible"] != float64(1039) || correction["correct_aggregate_withheld"] != float64(104) || correction["original_ACK_not_rewritten"] != true {
		t.Fatal("historical arithmetic correction or original ACK lost")
	}
}
