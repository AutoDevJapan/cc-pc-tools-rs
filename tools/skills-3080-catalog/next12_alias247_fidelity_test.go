package main

import (
	"fmt"
	"testing"
)

func TestOriginal3719Next12KeepsAllFourPacketsAndCrossJobNoticeReuse(t *testing.T) {
	checkOriginal3719PendingSnapshot(t, "skills-3080-original3719-next12-256267-evaluation175645.json", "811b6ec446ff3fdd7c6ed4c85250c1c59e6317e51f6775a5f05da46e1e056524", 256, 267, 4, "20261028-171959-todo-3719", 144, 63, 81)
}

func validateWebClawAlias247(evidence, originalCurrent map[string]any) error {
	if e := next4Fields(evidence, map[string]any{"selected_existing_target": "dynamic-action-authorization", "canonical_accepted_main": "a905b4519030b63e72c36a3d31b576d4d78a71f8", "actual_frozen_Creator_job": "20261029-092744-todo-4215", "actual_manifest_sha256": recoveryManifest, "current_normal_consumer_job": next3Actor, "current_instruction_count": float64(2), "current_instruction_bytes": float64(3964), "all_current_IDs_hashes_and_same_buffers_verified": true, "point_in_time_read_not_historical_continuity_or_atomic_cross_file_snapshot": true, "current_OS_writer": "unproved", "catalog_disposition_proposed": "integrated_existing", "working_catalog_integration_recorded": true, "new_Create_or_central_writes": float64(0), "real_service_UI_DB_or_permission_actions": float64(0), "new_rights_grants_or_raw_credit": float64(0), "whole16_complete": false}); e != nil {
		return e
	}
	s := next4Object(evidence["source_identity"])
	if e := next4Fields(s, map[string]any{"source_repo": "6mile/skills", "source_commit": "920714f17d81e0a95c7a9bc51faf0a7921cd982d", "canonical_row": float64(246), "alias_row": float64(247), "canonical_inventory_id": "github:d2194684c9294b536c20f3bb4cde29940e160ead91942bbc33d6d6d8e7678e55", "alias_inventory_id": "github:c0c5815afe581e7819cfd6429bbb519fa9b8638c067148e781228118b839c8c9", "canonical_path": "skills/mailnike/web-claw/SKILL.md", "alias_path": "skills/mailnike/webclaw/SKILL.md", "primary_sha256": "6efecd417740587d4f9634937c3883ea97fb6bbb3bd4013069dbf130c762ef64", "primary_git_blob": "5c62ad5056382591fefae4c6d07b74e9d0e31bf7", "notice_sha256": "6f6bb192fb5854c274ca1bd5f3705ad6d4577099d2db95f46c90d0657ffb830d", "notice_git_blob": "ff21cdae6e9e7e0ed7a376bfe989771b40831982", "fixed_public_alias_blobs_independently_confirmed": true, "each_package_meta_runtime_scripts_and_dependencies_not_equalized": true, "parent246_visible_profile_reused_only_as_same_content_not_new_raw": true}); e != nil {
		return e
	}
	rows, ok := evidence["current_files"].([]any)
	if !ok || len(rows) != 2 {
		return fmt.Errorf("exact current2 target instruction buffers required")
	}
	old, ok := originalCurrent["files"].([]any)
	if !ok || len(old) != 21 {
		return fmt.Errorf("original accepted21 current object metadata required")
	}
	for i, value := range rows {
		r := next4Object(value)
		o := next4Object(old[i])
		target := []string{"dynamic-action-authorization/SKILL.md", "dynamic-action-authorization/references/guide.md"}[i]
		if r["index"] != float64(i) || r["target"] != target || r["sha256"] != o["sha256"] || r["bytes"] != o["bytes"] || r["creation_and_current_object_identity_sha256"] != o["creation_and_current_object_identity_sha256"] || r["normal_same_buffer_actually_read"] != true {
			return fmt.Errorf("alias integration not bound to actual same current instruction object")
		}
	}
	return nil
}

func validatedWebClawAlias247Disposition(t *testing.T) map[string]cohortLedgerRow {
	t.Helper()
	var e, readback map[string]any
	loadPublicReceipt(t, "skills-3080-web-claw-alias247-integration175645.json", &e)
	loadPublicReceipt(t, "skills-3080-recovery-connections-consume-evidence175645.json", &readback)
	if err := validateWebClawAlias247(e, readback); err != nil {
		t.Fatal(err)
	}
	canonical := validatedRecoveryDispositions(t)
	if canonical["github:d2194684c9294b536c20f3bb4cde29940e160ead91942bbc33d6d6d8e7678e55"].Disposition != "installed_new_adaptation" {
		t.Fatal("canonical target has not passed actual current acceptance")
	}
	want := cohortLedgerRow{ID: "github:c0c5815afe581e7819cfd6429bbb519fa9b8638c067148e781228118b839c8c9", Source: "6mile/skills", Commit: "920714f17d81e0a95c7a9bc51faf0a7921cd982d", Path: "skills/mailnike/webclaw/SKILL.md", SHA: "6efecd417740587d4f9634937c3883ea97fb6bbb3bd4013069dbf130c762ef64", Disposition: "integrated_existing"}
	var ledger struct {
		Rows []cohortLedgerRow `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	n := 0
	for _, r := range ledger.Rows {
		if r.ID == want.ID {
			if r != want {
				t.Fatal("actual current alias disposition differs")
			}
			n++
		}
	}
	if n != 1 {
		t.Fatal("one exact current alias disposition required")
	}
	return map[string]cohortLedgerRow{want.ID: want}
}
func TestWebClawAliasUsesAcceptedCurrentTargetWithoutNewCreation(t *testing.T) {
	validatedWebClawAlias247Disposition(t)
}
func TestWebClawAliasCannotTransferRightsFromOnlyEqualBodyBytes(t *testing.T) {
	for _, change := range []string{"valid", "individual notice", "fixed commit", "entire package", "new creation", "invented writer", "different current object"} {
		t.Run(change, func(t *testing.T) {
			var e, r map[string]any
			loadPublicReceipt(t, "skills-3080-web-claw-alias247-integration175645.json", &e)
			loadPublicReceipt(t, "skills-3080-recovery-connections-consume-evidence175645.json", &r)
			switch change {
			case "individual notice":
				next4Object(e["source_identity"])["notice_sha256"] = "other"
			case "fixed commit":
				next4Object(e["source_identity"])["source_commit"] = "other"
			case "entire package":
				next4Object(e["source_identity"])["each_package_meta_runtime_scripts_and_dependencies_not_equalized"] = false
			case "new creation":
				e["new_Create_or_central_writes"] = float64(1)
			case "invented writer":
				e["current_OS_writer"] = "proven"
			case "different current object":
				next4Object(e["current_files"].([]any)[0])["creation_and_current_object_identity_sha256"] = "other"
			}
			err := validateWebClawAlias247(e, r)
			if (change == "valid" && err != nil) || (change != "valid" && err == nil) {
				t.Fatalf("%s: %v", change, err)
			}
		})
	}
}
