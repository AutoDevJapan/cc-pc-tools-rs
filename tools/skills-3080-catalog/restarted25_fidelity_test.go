package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"
)

const resumedPrimary13File = "skills-3080-primaryflow-next13-plus-held1-after216-evaluation095653.json"
const resumedPrimary13SHA = "fcf47aad13ee0d1fd342411f16070e449c7e04b3884502b9405a24002211a673"

func TestResumedOriginalTwelvePreservesThreeActualConsumersAndAllSupportStates(t *testing.T) {
	checkOriginal3719PendingSnapshotWithActors(t, "skills-3080-original3719-next12-311322-evaluation095653.json", "da04c115066784a75785b724f72a2a070cc4ec311f4f53b26ce0f6387e78e13b", 311, 322, 3, "20261028-171959-todo-3719", 371, 244, 127, "20261030-095653-todo-3080", map[string]string{
		"c322818d0ee26a6a5590fe2de98a839773d47cdb586180f1669629ce2d3579bf": "20261029-170905-todo-3080",
		"4f0f51d9b7c1e24bc06421bc1f3d310be679d036452afd3abc6442f90927c927": "20261029-170905-todo-3080",
		"1f41e65464aef5f4d3004705913829fcc1c4d5a89c2190ccff263b8ac616c85b": "20261030-095653-todo-3080",
	})
}

// Preserve the original plan member struct's JSON order when checking its
// digest; generic map reserialization has a different field order.
type resumedPlanMember struct {
	ID      string `json:"inventory_id"`
	Repo    string `json:"repo"`
	Commit  string `json:"commit"`
	Path    string `json:"path"`
	SHA     string `json:"sha256"`
	Blob    string `json:"git_blob"`
	Bytes   int64  `json:"bytes"`
	Content string `json:"content_path"`
}

func validateResumedThirteenAndHeld(v map[string]any) error {
	return validateResumedVisibleSnapshot(v, 13, 1, 14, 3,
		map[string]any{"records": float64(216), "sha256": "abc54e47f783c03ecb1e235a2b556e191b0d215f285148c698d7373e393d14a6", "all_records_and64_original_contexts_exactly_retained": true},
		map[string]any{"records": float64(229), "held_groups": float64(1), "acknowledged_groups": float64(230), "original_packets": float64(67), "sha256": "9b34f3cec8b9895826d64cc289a5c41736f75c36e16f87d58834afa38303be0e"},
		map[string][5]string{
			"77584462e0028ffd4941707f2341d0ac9efce930717ae20ee805aac6327f69aa": {"20261029-174543-todo-4133", "20261029-170905-todo-3080", "p5a4c66/gpt-6.1-sol", "9336a02f41b267d04f4f406815ff048938055a11ea4582f8514d79708881c9ff", "20261029-170905-todo-3080"},
			"c5866fcb34261c82de2d1c0825eddc6d78a21bc05110e39c93a4592ac84c4489": {"20261029-174632-todo-4134", "20261029-170905-todo-3080", "gpt-6.1-sol", "b0bfc631a8d355a1c406003da7c417b668dabd49dccf48b1d11ffa18a3132b72", "20261029-170905-todo-3080"},
			"19665e6f7aab6488fa8c1e224cc2f4999c4a18a5830b8c402b28a379fdc2c3f3": {"20261030-033405-todo-4134", "20261030-095653-todo-3080", "gpt-6.1-sol", "b0bfc631a8d355a1c406003da7c417b668dabd49dccf48b1d11ffa18a3132b72", ""},
		})
}

func validateResumedVisibleSnapshot(v map[string]any, semanticCount, heldCount, expectedMembers, packetCount int, prior, current map[string]any, actors map[string][5]string) error {
	if err := next4Fields(v, map[string]any{
		"domain": "skills3080-primaryflow-parent-shared-snapshot-v1", "publisher_job": "20261030-095653-todo-3080",
		"representative_body_groups": float64(semanticCount), "acknowledged_body_groups": float64(semanticCount + heldCount), "held_body_groups": float64(heldCount),
		"bound_member_metadata": float64(expectedMembers), "complete_original_packets": float64(packetCount),
		"all_original_packets_losslessly_reconstructed": true, "all_member_contexts_read": false, "all_member_rights_confirmed": false, "whole16_complete": false,
	}); err != nil {
		return err
	}
	if err := next4Fields(next4Object(v["prior_snapshot"]), prior); err != nil {
		return err
	}
	if err := next4Fields(next4Object(v["current_snapshot"]), current); err != nil {
		return err
	}
	prep := next4Object(v["publication_preparation"])
	for _, key := range []string{"new_source_body_reads", "new_global_raw_credit", "new_permissions", "new_final_dispositions", "central_operations"} {
		if prep[key] != float64(0) {
			return fmt.Errorf("pending materialization gained an action: %s", key)
		}
	}
	records, rok := v["records"].([]any)
	entries, eok := v["entries"].([]any)
	held, hok := v["held_records"].([]any)
	contexts := next4Object(v["shared_original_contexts"])
	if !rok || !eok || !hok || len(records) != semanticCount || len(entries) != semanticCount || len(held) != heldCount || len(contexts) != packetCount || len(actors) != packetCount {
		return fmt.Errorf("fixed semantic entries, separate held groups and original packets required")
	}
	groups := map[string][]any{}
	rowsByOrdinal := map[string]map[string]any{}
	seen := map[string]bool{}
	all := append(append([]any{}, records...), held...)
	for i, value := range all {
		r := next4Object(value)
		a := next4Object(r["original_assessment"])
		read := next4Object(a["read"])
		id, ok := r["inventory_id"].(string)
		if !ok || id == "" || seen[id] || r["new_final_disposition"] != false || r["representative_body_meaning_only"] != true || r["all_member_contexts_and_individual_rights_pending"] != true || a["each_member_context_and_rights_pending"] != true || read["raw_full"] != false || a["member_occurrences"] != float64(1) {
			return fmt.Errorf("original pending identity, read scope or rights changed")
		}
		if r["ordinal"] != a["ordinal"] || a["representative_inventory_id"] != id || a["body_sha256"] != r["source_sha256"] || a["source_git_blob"] != r["git_blob"] || a["body_bytes"] != r["bytes"] {
			return fmt.Errorf("derived record no longer binds the original representative and body tuple")
		}
		seen[id] = true
		if i < len(records) {
			entry := next4Object(entries[i])
			for _, key := range []string{"inventory_id", "source_repo", "source_commit", "path", "source_sha256", "git_blob", "bytes"} {
				if entry[key] != r[key] {
					return fmt.Errorf("semantic source tuple changed")
				}
			}
			if a["status"] != "visible_primary_evaluated" {
				return fmt.Errorf("unread group credited as semantic")
			}
			for _, key := range []string{"use", "value", "positive", "negative"} {
				if text, ok := a[key].(string); !ok || text == "" {
					return fmt.Errorf("visible meaning missing: %s", key)
				}
			}
		} else {
			if a["status"] != "all_withheld" || r["body_meaning_unread"] != true || r["ordinal"] != float64(107) || read["total_physical_lines"] != float64(6) || read["unseen_meanings_remain_unknown"] != true {
				return fmt.Errorf("held group gained meaning or credit")
			}
			for _, key := range []string{"use", "value", "positive", "negative"} {
				if a[key] != "" {
					return fmt.Errorf("held meaning was invented")
				}
			}
			ranges, ok := read["ranges"].([]any)
			if !ok || len(ranges) != 1 {
				return fmt.Errorf("held full range missing")
			}
			span := next4Object(ranges[0])
			withheld, ok := span["withheld_physical_lines"].([]any)
			if span["first"] != float64(1) || span["through"] != float64(6) || span["visible_lines"] != float64(0) || !ok || len(withheld) != 6 {
				return fmt.Errorf("held range gained visible text")
			}
			for j, line := range withheld {
				if line != float64(j+1) {
					return fmt.Errorf("held range coverage changed")
				}
			}
		}
		sha, ok := r["original_packet_sha256"].(string)
		if !ok || !validHex(sha, 64) {
			return fmt.Errorf("original packet anchor missing")
		}
		groups[sha] = append(groups[sha], a)
		rowsByOrdinal[fmt.Sprintf("%s/%v", sha, a["ordinal"])] = r
	}
	if !original3719PacketKeySetsMatch(groups, contexts) {
		return fmt.Errorf("extra or missing original context")
	}
	memberCount := 0
	for sha, originals := range groups {
		c := next4Object(contexts[sha])
		common := next4Object(c["common"])
		sort.Slice(originals, func(i, j int) bool {
			return next4Object(originals[i])["ordinal"].(float64) < next4Object(originals[j])["ordinal"].(float64)
		})
		whole := map[string]any{}
		for key, value := range common {
			whole[key] = value
		}
		whole["entries"] = originals
		b, err := json.Marshal(whole)
		if err != nil || travel3Digest(b) != sha || common["parent_origin_job"] != "20261027-175645-todo-3080" || c["all_original_fields_reconstructed"] != true {
			return fmt.Errorf("complete original packet changed")
		}
		actor, ok := actors[sha]
		if !ok {
			return fmt.Errorf("unexpected original packet")
		}
		reader, consumer, model, spec, parent := actor[0], actor[1], actor[2], actor[3], actor[4]
		ack := next4Object(c["independent_parent_ack"])
		meaning := next4Object(c["independent_parent_meaning"])
		native := next4Object(c["independent_native_metadata"])
		first, through := next4Object(originals[0])["ordinal"], next4Object(originals[len(originals)-1])["ordinal"]
		if err := next4Fields(ack, map[string]any{"domain": "skills3080-primaryflow-parent-ack-v1", "packet_id": common["packet_id"], "packet_sha256": sha, "acknowledged_through": through, "cohort": common["cohort"], "plan_sha256": common["plan_sha256"], "parent_origin_job": common["parent_origin_job"]}); err != nil {
			return err
		}
		if err := next4Fields(meaning, map[string]any{"packet_id": common["packet_id"], "packet_sha256": sha, "first": first, "through": through}); err != nil {
			return err
		}
		if common["reader_job"] != reader || c["actual_assessment_reader"] != reader || ack["reader_job"] != reader || ack["consumer_job"] != consumer || meaning["reader_job"] != reader || meaning["consumer_job"] != consumer || meaning["all_entry_meanings_and_limits_read"] != true {
			return fmt.Errorf("actual reader or resumed consumer changed")
		}
		if err := next4Fields(native, map[string]any{"reader_job": reader, "actual_consumer_job": consumer, "registered_spec_sha256": spec, "actual_model": model, "actual_effort": "max", "thread_matches": true, "cwd_matches": true, "immutable_origin_job": "20261027-175645-todo-3080"}); err != nil {
			return err
		}
		turns, ok := native["native_turn_contexts"].([]any)
		if !ok || len(turns) == 0 {
			return fmt.Errorf("actual native turn contexts missing")
		}
		startText, _ := native["strict_window_start"].(string)
		endText, _ := native["strict_window_observed_end"].(string)
		start, startErr := time.Parse(time.RFC3339Nano, startText)
		end, endErr := time.Parse(time.RFC3339Nano, endText)
		if startErr != nil || endErr != nil || end.Before(start) {
			return fmt.Errorf("actual native time window invalid")
		}
		for _, value := range turns {
			turn := next4Object(value)
			if turn["model"] != model || turn["effort"] != "max" {
				return fmt.Errorf("actual native turn model or effort differs")
			}
			stamp, _ := turn["timestamp"].(string)
			at, err := time.Parse(time.RFC3339Nano, stamp)
			if err != nil || at.Before(start) || at.After(end) {
				return fmt.Errorf("native turn outside the same reader observation window")
			}
		}
		observed, present := native["actual_launch_parent"]
		if !present || (parent == "" && observed != nil) || (parent != "" && observed != parent) {
			return fmt.Errorf("actual parent null/missing/empty/retained origin conflated")
		}
		members := next4Object(c["member_metadata"])
		if len(members) != len(originals) {
			return fmt.Errorf("original member groups omitted")
		}
		for _, value := range originals {
			a := next4Object(value)
			ordinal := fmt.Sprint(a["ordinal"])
			r := rowsByOrdinal[sha+"/"+ordinal]
			if r["cohort"] != common["cohort"] || r["actual_assessment_reader"] != reader {
				return fmt.Errorf("derived cohort or actual reader changed")
			}
			raw, err := json.Marshal(members[ordinal])
			if err != nil {
				return err
			}
			var bound []resumedPlanMember
			if json.Unmarshal(raw, &bound) != nil || len(bound) != 1 {
				return fmt.Errorf("one pending member per new group required")
			}
			encoded, err := json.Marshal(bound)
			if err != nil || travel3Digest(encoded) != a["member_metadata_sha256"] {
				return fmt.Errorf("member metadata no longer matches original digest")
			}
			m := bound[0]
			if m.ID != r["inventory_id"] || m.Repo != r["source_repo"] || m.Commit != r["source_commit"] || m.Path != r["path"] || m.SHA != r["source_sha256"] || m.Blob != r["git_blob"] || float64(m.Bytes) != r["bytes"] {
				return fmt.Errorf("pending member source tuple changed")
			}
			memberCount++
		}
		boundary := next4Object(common["boundaries"])
		if boundary["source_executed"] != false {
			return fmt.Errorf("source executed")
		}
		for _, key := range []string{"copy_grants", "final_dispositions", "central_operations", "new_global_raw_credit", "support_body_reads"} {
			if boundary[key] != float64(0) {
				return fmt.Errorf("assessment gained authority")
			}
		}
	}
	if memberCount != expectedMembers {
		return fmt.Errorf("all individual member contexts must remain pending")
	}
	return nil
}

func TestResumedPrimaryThirteenKeepsAllWithheldSeparateAndNullParentObserved(t *testing.T) {
	requireAssessmentInputRegistered(t, resumedPrimary13File, resumedPrimary13SHA, 13)
	for _, change := range []string{"valid", "held credited", "held meaning", "held visible line", "raw credit", "member source", "orphan context", "wrong consumer", "empty parent", "missing parent", "permission", "record ordinal", "record reader", "record cohort", "record representative scope"} {
		t.Run(change, func(t *testing.T) {
			var v map[string]any
			if travel3Digest(loadPublicReceipt(t, resumedPrimary13File, &v)) != resumedPrimary13SHA {
				t.Fatal("independent exact source bytes changed")
			}
			held := next4Object(v["held_records"].([]any)[0])
			a := next4Object(held["original_assessment"])
			contexts := next4Object(v["shared_original_contexts"])
			c := next4Object(contexts["19665e6f7aab6488fa8c1e224cc2f4999c4a18a5830b8c402b28a379fdc2c3f3"])
			switch change {
			case "held credited":
				v["records"] = append(v["records"].([]any), held)
			case "held meaning":
				a["use"] = "invented from filename"
			case "held visible line":
				next4Object(next4Object(a["read"])["ranges"].([]any)[0])["visible_lines"] = float64(1)
			case "raw credit":
				next4Object(next4Object(v["records"].([]any)[0])["original_assessment"])["read"].(map[string]any)["raw_full"] = true
			case "member source":
				next4Object(next4Object(c["member_metadata"])["107"].([]any)[0])["path"] = "other/SKILL.md"
			case "orphan context":
				contexts["orphan"] = c
			case "wrong consumer":
				next4Object(c["independent_parent_ack"])["consumer_job"] = "20261029-170905-todo-3080"
			case "empty parent":
				next4Object(c["independent_native_metadata"])["actual_launch_parent"] = ""
			case "missing parent":
				delete(next4Object(c["independent_native_metadata"]), "actual_launch_parent")
			case "permission":
				next4Object(next4Object(c["common"])["boundaries"])["copy_grants"] = float64(1)
			case "record ordinal":
				next4Object(v["records"].([]any)[0])["ordinal"] = float64(999)
			case "record reader":
				next4Object(v["records"].([]any)[0])["actual_assessment_reader"] = "20261030-095653-todo-3080"
			case "record cohort":
				next4Object(v["records"].([]any)[0])["cohort"] = float64(99)
			case "record representative scope":
				next4Object(v["records"].([]any)[0])["representative_body_meaning_only"] = false
			}
			err := validateResumedThirteenAndHeld(v)
			if (change == "valid" && err != nil) || (change != "valid" && err == nil) {
				t.Fatalf("%s: %v", change, err)
			}
		})
	}
}
