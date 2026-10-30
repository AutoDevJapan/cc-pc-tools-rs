package main

import (
	"encoding/json"
	"testing"
)

func TestResumedOriginal3719Next16RetainsMixedActualConsumers(t *testing.T) {
	checkOriginal3719PendingSnapshotWithActors(t, "skills-3080-original3719-next16-295310-evaluation170905.json", "cb88c15f660d360ec53059e5928af48b7ed046d48a834143582d74bb958ac786", 295, 310, 3, "20261028-171959-todo-3719", 233, 166, 67, "20261029-170905-todo-3080", map[string]string{
		"63df86b3477a7d1b4e04259ed07f6ddddfa238ff29d8a23549f6cc559709eb51": "20261027-175645-todo-3080",
		"9fdec5e2b825e647cb2666f3dcf364a5063bd78701c8f01dd208ec11dd2fe075": "20261029-170905-todo-3080",
		"4ded148766851b366d3025836bfa142c03f8cf15ce062634ddb65dfdaf17915e": "20261029-170905-todo-3080",
	})
}

func TestResumedPrimaryFivePreservesCurrentReadersAndNoCopyGrants(t *testing.T) {
	const name = "skills-3080-primaryflow-next5-after211-evaluation170905.json"
	const pin = "7e8bad791a94cf0a5a9db0de108654e6d9b8ce90e3af5ac8d685839cbfc10bcc"
	requireAssessmentInputRegistered(t, name, pin, 5)
	var v map[string]any
	if travel3Digest(loadPublicReceipt(t, name, &v)) != pin {
		t.Fatal("actual pending-five bytes differ")
	}
	if e := next4Fields(v, map[string]any{"domain": "skills3080-primaryflow-parent-shared-snapshot-v1", "publisher_job": "20261029-170905-todo-3080", "representative_body_groups": float64(5), "bound_member_metadata": float64(5), "complete_original_packets": float64(2), "all_original_packets_losslessly_reconstructed": true, "all_member_contexts_read": false, "all_member_rights_confirmed": false, "whole16_complete": false}); e != nil {
		t.Fatal(e)
	}
	prior := received27Object(t, v["prior_snapshot"])
	if e := next4Fields(prior, map[string]any{"records": float64(211), "sha256": "29ab9d5eea8a15843e9ced10b6ce3ce4b56d3bd6768b07e16fc5a8c9694f8568", "all_records_and62_original_contexts_exactly_retained": true, "declared_private_member_header": float64(228), "actual_bound_member_records": float64(237), "old_private_header_not_rewritten": true}); e != nil {
		t.Fatal(e)
	}
	current := received27Object(t, v["current_snapshot"])
	if e := next4Fields(current, map[string]any{"records": float64(216), "original_packets": float64(64), "sha256": "abc54e47f783c03ecb1e235a2b556e191b0d215f285148c698d7373e393d14a6"}); e != nil {
		t.Fatal(e)
	}
	prep := received27Object(t, v["publication_preparation"])
	for _, k := range []string{"new_source_body_reads", "new_global_raw_credit", "new_permissions", "new_final_dispositions", "central_operations"} {
		if prep[k] != float64(0) {
			t.Fatal("pending materialization gained execution or permission")
		}
	}
	contexts := received27Object(t, v["shared_original_contexts"])
	groups := map[string][]any{}
	seen := map[string]bool{}
	records, entries := received27Array(t, v["records"]), received27Array(t, v["entries"])
	if len(records) != 5 || len(entries) != 5 || len(contexts) != 2 {
		t.Fatal("five records and two complete packets required")
	}
	for i, value := range records {
		r := received27Object(t, value)
		id, _ := r["inventory_id"].(string)
		a := received27Object(t, r["original_assessment"])
		read := received27Object(t, a["read"])
		if id == "" || seen[id] || r["new_final_disposition"] != false || r["all_member_contexts_and_individual_rights_pending"] != true || a["each_member_context_and_rights_pending"] != true || read["raw_full"] != false || a["member_occurrences"] != float64(1) {
			t.Fatal("pending source identity or scope changed")
		}
		seen[id] = true
		entry := received27Object(t, entries[i])
		for _, k := range []string{"inventory_id", "source_repo", "source_commit", "path", "source_sha256", "git_blob", "bytes"} {
			if entry[k] != r[k] {
				t.Fatal("assessment projection lost its source tuple")
			}
		}
		sha, _ := r["original_packet_sha256"].(string)
		groups[sha] = append(groups[sha], a)
	}
	if !original3719PacketKeySetsMatch(groups, contexts) {
		t.Fatal("original context set differs")
	}
	for sha, originals := range groups {
		c := received27Object(t, contexts[sha])
		common := received27Object(t, c["common"])
		whole := map[string]any{}
		for k, x := range common {
			whole[k] = x
		}
		whole["entries"] = originals
		b, e := json.Marshal(whole)
		if e != nil || travel3Digest(b) != sha || common["parent_origin_job"] != "20261027-175645-todo-3080" || c["all_original_fields_reconstructed"] != true {
			t.Fatal("whole original packet or origin changed")
		}
		ack := received27Object(t, c["independent_parent_ack"])
		meaning := received27Object(t, c["independent_parent_meaning"])
		native := received27Object(t, c["independent_native_metadata"])
		reader, spec, model := "20261029-174543-todo-4133", "9336a02f41b267d04f4f406815ff048938055a11ea4582f8514d79708881c9ff", "p5a4c66/gpt-6.1-sol"
		if common["cohort"] == float64(1) {
			reader, spec, model = "20261029-174632-todo-4134", "b0bfc631a8d355a1c406003da7c417b668dabd49dccf48b1d11ffa18a3132b72", "gpt-6.1-sol"
		}
		if common["reader_job"] != reader || c["actual_assessment_reader"] != reader || ack["reader_job"] != reader || ack["consumer_job"] != "20261029-170905-todo-3080" || meaning["reader_job"] != reader || meaning["consumer_job"] != ack["consumer_job"] || meaning["all_entry_meanings_and_limits_read"] != true {
			t.Fatal("actual reader and current parent differ")
		}
		if e := next4Fields(native, map[string]any{"reader_job": reader, "registered_spec_sha256": spec, "actual_model": model, "actual_effort": "max", "thread_matches": true, "cwd_matches": true, "immutable_origin_job": "20261027-175645-todo-3080", "actual_launch_parent": "20261029-170905-todo-3080"}); e != nil {
			t.Fatal(e)
		}
		if common["cohort"] == float64(1) && (meaning["normal_render_named_native_calls"] != float64(3) || meaning["returned_expected_range_hashes_with_exit0_and_no_truncation"] != float64(4) || meaning["old_c83_packet_rewritten_or_accepted"] != false) {
			t.Fatal("new C1 readback must not relabel the old wrong-actor packet")
		}
		boundary := received27Object(t, common["boundaries"])
		if boundary["source_executed"] != false {
			t.Fatal("source executed")
		}
		for _, k := range []string{"copy_grants", "final_dispositions", "central_operations", "new_global_raw_credit", "support_body_reads"} {
			if boundary[k] != float64(0) {
				t.Fatal("pending meanings gained authority")
			}
		}
	}
}
