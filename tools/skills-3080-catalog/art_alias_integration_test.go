package main

import (
	"fmt"
	"strings"
	"testing"
)

const artFiveAliasEvidenceSHA = "b49bd8ed3571fff98ee961acc9e205f21c4abe9304ce5ef1730d2432ab1e769a"

var artAliasPaths = map[string]string{
	"github:f330d4f9f639ab1bad13dc0b4cfcc5a4235ac8dc6a3b9d21343b75a2e6f0fc70": "skills/darinouyang/algorithmic-art-2/SKILL.md",
	"github:4009f77084478acb9c85713217edbf36ff5a162763380e8337aecfccc0b1b411": "skills/pupuking723/algorithmic-art-anthropic/SKILL.md",
	"github:41e0779f37283f1b4785ea1a6953616b257d0f488306c16f76e605b8b54e429f": "skills/seanphan/algorithmic-art/SKILL.md",
	"github:90b1a9685fdafcbaa8fecf283beb103c9c8955e14a2949e3cf16fc67d130e4a2": "skills/zhao202404/excel-skill/SKILL.md",
	"github:0e27bad8cfdc1ad12b137d1eff7224e2405dde247c53a32755b4996a3862371f": "skills/zhaom0/feifei/SKILL.md",
}

func validateArtAliasObservation(v map[string]any) error {
	if e := next4Fields(v, map[string]any{"todo": float64(3080), "publisher_job": "20261029-170905-todo-3080", "new_integrations": float64(5), "new_Skill_creations": float64(0), "central_new_writes": float64(0), "new_source_raw_credit": float64(0), "whole16_complete": false, "other25_metadata_candidates_not_disposed": true, "nested_current_application_observations_precede_this_ledger_update": true}); e != nil {
		return e
	}
	metadata := next4Object(v["metadata_only_proof"])
	if e := next4Fields(metadata, map[string]any{"source_repo": "6mile/skills", "public": true, "fixed_commit": "920714f17d81e0a95c7a9bc51faf0a7921cd982d", "roster_sha256": "f9c6b2e44c5e3aa8947f758f864eaa0d5edc396d622359826bca848cd96f9901", "source_bodies_or_notice_text_refetched": float64(0), "new_final_dispositions_or_central_writes": float64(0)}); e != nil {
		return e
	}
	aliases, ok := metadata["aliases"].([]any)
	if !ok || len(aliases) != 5 {
		return fmt.Errorf("each individual fixed package and notice required")
	}
	byID := map[string]map[string]any{}
	for _, value := range aliases {
		a := next4Object(value)
		id, _ := a["inventory_id"].(string)
		path := artAliasPaths[id]
		if path == "" || byID[id] != nil {
			return fmt.Errorf("fixed alias repeated or unknown")
		}
		if e := next4Fields(a, map[string]any{"source_repo": "6mile/skills", "source_commit": "920714f17d81e0a95c7a9bc51faf0a7921cd982d", "source_path": path, "source_sha256": "3bc4092c09804853186524c826bc0621b940bb6122c05b84496dff95388e6eef", "source_git_blob": "634f6fa42e4e697fa6afd293acd7fb8246574876", "source_bytes": float64(19769), "individual_notice_path": strings.TrimSuffix(path, "SKILL.md") + "LICENSE.txt", "individual_notice_sha256": "58d1e17ffe5109a7ae296caafcadfdbe6a7d176f0bc4ab01e12a689b0499d8bd", "individual_notice_git_blob": "7a4a3ea2424c09fbe48d455aed1eaa94d9124835", "individual_notice_bytes": float64(11357), "root_license_used_as_blanket_permission": false, "actual_author_independent_copyright_or_third_party_rights_proven": false, "new_source_literal_reads": float64(0)}); e != nil {
			return e
		}
		byID[id] = a
	}
	app := next4Object(v["current_instruction_application"])
	if e := next4Fields(app, map[string]any{"current_consumer_job": "20261029-170905-todo-3080", "original_creator_job": next3Actor, "manifest_sha256": consolidationManifest, "creation_request_sha256": consolidationRequest, "native_creation_call": consolidationCall, "current_instruction_files": float64(2), "current_instruction_bytes": float64(5191), "all_two_literal_current_instructions_read": true, "historical_continuity_proven": false, "current_OS_writer": "unproved", "central_direct_opens": float64(0), "new_creations": float64(0), "real_art_exports_checked": float64(0)}); e != nil {
		return e
	}
	files, ok := app["normal_current_files"].([]any)
	if !ok || len(files) != 2 {
		return fmt.Errorf("two actual current instruction buffers required")
	}
	for i, value := range files {
		f := next4Object(value)
		name, sha, size := "generative-art-reproducibility/SKILL.md", "c6c97201564ba9eebe55b1e3dc71ef55283e0a9c8c103aa8ceaf1780bddbed29", float64(1980)
		if i == 1 {
			name, sha, size = "generative-art-reproducibility/references/guide.md", "43bd0e4a0cd63fb7a6ed491f89f2eab6c42cf233bb546c483b964de185ef61b7", 3211
		}
		if e := next4Fields(f, map[string]any{"index": float64(7 + i), "target": name, "sha256": sha, "bytes": size, "same_current_handle_buffer_read": true, "creation_and_current_ID_equal": true}); e != nil {
			return e
		}
		id, _ := f["creation_and_current_object_identity_sha256"].(string)
		if !validHex(id, 64) {
			return fmt.Errorf("actual identity digest missing")
		}
	}
	consumer := next4Object(v["independent_current_consumer"])
	if e := next4Fields(consumer, map[string]any{"task": float64(4227), "job": "20261029-184734-todo-4227", "spec_sha256": "4b77fed1f4de1afae672ea9f8825de205b9fb9feabe02a9b4703931085cf0f3c", "final_sha256": "9bc6ff14e5a41dd94ba8d7ca55e0b16afb7342b55ea9d1150d89e2aafa1a01ec", "normal_typed_qualified": true, "exact_same_current_buffer_use_confirmed": true, "actual_image_export_or_memory_engine_tested": false}); e != nil {
		return e
	}
	text, ok := consumer["final_response"].(string)
	if !ok || travel3Digest([]byte(text)) != consumer["final_sha256"] {
		return fmt.Errorf("exact normal consumer original changed")
	}
	n := next4Object(consumer["native"])
	if e := next4Fields(n, map[string]any{"actual_job": "20261029-184734-todo-4227", "consumer_job": "20261029-170905-todo-3080", "registered_spec_sha256": "4b77fed1f4de1afae672ea9f8825de205b9fb9feabe02a9b4703931085cf0f3c", "thread_matches": true, "cwd_matches": true, "normal_result_received": true, "result_body_read": true, "Creator_qualification_claimed": false}); e != nil {
		return e
	}
	contexts := receivedRentalContexts(n["actual_native"])
	if len(contexts) != 1 {
		return fmt.Errorf("one actual native consumer context required")
	}
	if e := next4Fields(contexts[0], map[string]any{"model": "gpt-6-luna", "effort": "max"}); e != nil {
		return e
	}
	entries, ok := v["entries"].([]any)
	if !ok || len(entries) != 5 {
		return fmt.Errorf("five integration occurrences required")
	}
	seen := map[string]bool{}
	for _, value := range entries {
		r := next4Object(value)
		id, _ := r["inventory_id"].(string)
		a := byID[id]
		if a == nil || seen[id] {
			return fmt.Errorf("integration occurrence omitted/repeated")
		}
		seen[id] = true
		for _, key := range []string{"source_repo", "source_commit", "source_sha256", "source_git_blob", "source_bytes", "individual_notice_path", "individual_notice_sha256", "individual_notice_git_blob", "individual_notice_bytes"} {
			dest := key
			switch key {
			case "source_git_blob":
				dest = "git_blob"
			case "source_bytes":
				dest = "bytes"
			}
			if r[dest] != a[key] {
				return fmt.Errorf("individual source/notice integration binding differs")
			}
		}
		if r["path"] != a["source_path"] {
			return fmt.Errorf("individual path differs")
		}
		if e := next4Fields(r, map[string]any{"target": "generative-art-reproducibility", "target_entrypoint_sha256": "c6c97201564ba9eebe55b1e3dc71ef55283e0a9c8c103aa8ceaf1780bddbed29", "disposition": "integrated_existing", "same_visible_workflow_only": true, "new_source_literal_reads": float64(0), "new_global_raw_credit": float64(0), "new_Skill_creations": float64(0)}); e != nil {
			return e
		}
		scope, _ := r["rights_scope"].(string)
		if !strings.Contains(scope, "No copying of unread") || !strings.Contains(scope, "individual identical Apache notice") {
			return fmt.Errorf("visible-only individual rights scope lost")
		}
	}
	return nil
}

func validateArtAliasLedgerProvenance(row, evidence map[string]any) error {
	return next4Fields(row, map[string]any{
		"inventory_id": evidence["inventory_id"], "source_repo": evidence["source_repo"],
		"source_commit": evidence["source_commit"], "source_path": evidence["path"],
		"source_sha256": evidence["source_sha256"], "target": evidence["target"],
		"disposition":              "integrated_existing",
		"evidence":                 "skills-3080-art-five-alias-integrations170905.json",
		"individual_notice_path":   evidence["individual_notice_path"],
		"individual_notice_sha256": evidence["individual_notice_sha256"],
		"manifest":                 evidence["target_manifest"], "entrypoint_sha256": evidence["target_entrypoint_sha256"],
	})
}

func validatedArtAliasDispositions(t *testing.T) map[string]cohortLedgerRow {
	t.Helper()
	var v map[string]any
	if travel3Digest(loadPublicReceipt(t, "skills-3080-art-five-alias-integrations170905.json", &v)) != artFiveAliasEvidenceSHA {
		t.Fatal("independent exact alias observation bytes differ")
	}
	if e := validateArtAliasObservation(v); e != nil {
		t.Fatal(e)
	}
	var ledger struct {
		Entries []map[string]any `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	result := map[string]cohortLedgerRow{}
	for _, value := range received27Array(t, v["entries"]) {
		r := received27Object(t, value)
		id := r["inventory_id"].(string)
		want := cohortLedgerRow{ID: id, Source: r["source_repo"].(string), Commit: r["source_commit"].(string), Path: r["path"].(string), SHA: r["source_sha256"].(string), Disposition: "integrated_existing"}
		matched := 0
		for _, row := range ledger.Entries {
			if row["inventory_id"] == id {
				if err := validateArtAliasLedgerProvenance(row, r); err != nil {
					t.Fatal(err)
				}
				matched++
			}
		}
		if matched != 1 {
			t.Fatal("one final row per occurrence required")
		}
		result[id] = want
	}
	return result
}

func TestArtAliasesIntegrateOnlyIndividuallyBoundVisibleWorkflow(t *testing.T) {
	validatedArtAliasDispositions(t)
}

func TestArtAliasLedgerRejectsLostEvidenceAndIndividualNotice(t *testing.T) {
	var evidence map[string]any
	loadPublicReceipt(t, "skills-3080-art-five-alias-integrations170905.json", &evidence)
	entry := received27Object(t, received27Array(t, evidence["entries"])[0])
	var ledger struct {
		Entries []map[string]any `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	var original map[string]any
	for _, row := range ledger.Entries {
		if row["inventory_id"] == entry["inventory_id"] {
			original = row
		}
	}
	if original == nil {
		t.Fatal("actual alias ledger row missing")
	}
	for _, change := range []string{"valid", "evidence", "individual_notice_path", "individual_notice_sha256", "manifest", "entrypoint_sha256"} {
		t.Run(change, func(t *testing.T) {
			row := map[string]any{}
			for k, v := range original {
				row[k] = v
			}
			if change != "valid" {
				delete(row, change)
			}
			err := validateArtAliasLedgerProvenance(row, entry)
			if (change == "valid" && err != nil) || (change != "valid" && err == nil) {
				t.Fatalf("%s: %v", change, err)
			}
		})
	}
	row := map[string]any{}
	for k, v := range original {
		row[k] = v
	}
	row["evidence"] = "skills-3080-consolidation-art-installation-evidence175645.json"
	row["individual_notice_path"] = "skills/moyu0032/mo-test-1/LICENSE.txt"
	if validateArtAliasLedgerProvenance(row, entry) == nil {
		t.Fatal("reported borrowed-evidence/notice counterexample accepted")
	}
}

func TestArtAliasObservationRejectsWrongSourceNoticeActorOrEffect(t *testing.T) {
	for _, change := range []string{"valid", "other primary", "other notice", "borrowed notice path", "repeated alias", "new installation", "other consumer", "false current buffer", "literal continuity", "new creation", "changed result", "other model"} {
		t.Run(change, func(t *testing.T) {
			var v map[string]any
			loadPublicReceipt(t, "skills-3080-art-five-alias-integrations170905.json", &v)
			a := next4Object(next4Object(v["metadata_only_proof"])["aliases"].([]any)[0])
			app := next4Object(v["current_instruction_application"])
			consumer := next4Object(v["independent_current_consumer"])
			switch change {
			case "other primary":
				a["source_sha256"] = "7ce4e60e2ac08709b60fe5434131bcd81f11bd860beccee8df9bb786ee90c60c"
			case "other notice":
				a["individual_notice_sha256"] = "43a990050404c5fd26abe14ba73fbdea97c43a0366360a16e35ec37e2f245e3c"
			case "borrowed notice path":
				a["individual_notice_path"] = "skills/moyu0032/mo-test-1/LICENSE.txt"
			case "repeated alias":
				v["entries"].([]any)[1] = v["entries"].([]any)[0]
			case "new installation":
				next4Object(v["entries"].([]any)[0])["disposition"] = "installed_new_adaptation"
			case "other consumer":
				app["current_consumer_job"] = next3Actor
			case "false current buffer":
				next4Object(app["normal_current_files"].([]any)[0])["same_current_handle_buffer_read"] = false
			case "literal continuity":
				app["historical_continuity_proven"] = true
			case "new creation":
				v["new_Skill_creations"] = float64(1)
			case "changed result":
				consumer["final_response"] = "accepted"
			case "other model":
				next4Object(next4Object(consumer["native"])["actual_native"].([]any)[0])["model"] = "gpt-6.1-sol"
			}
			err := validateArtAliasObservation(v)
			if (change == "valid" && err != nil) || (change != "valid" && err == nil) {
				t.Fatalf("%s: %v", change, err)
			}
		})
	}
}
