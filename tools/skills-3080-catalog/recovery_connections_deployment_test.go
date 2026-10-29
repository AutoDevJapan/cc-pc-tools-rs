package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

const recoveryManifest = "03612ee51f6249602bd4afd59c13d79430ff58cd464d0872e1cb1bb9d37eb27f"
const recoveryRequest = "0e6861bb732d3c110fd1768fcee2565e46854c1f6f18b2e39073a76c81fd3efd"
const recoveryCall = "call_fbe8e9db91674be687522b1ba1ace09b"
const recoveryProducer = "20261029-092744-todo-4215"

// This invocation differs in both whitespace and path separator spelling.
// Preserve that observation rather than borrowing the earlier rental receipt.
func recoveryRuntimeContract(h, n, r map[string]any) error {
	checks := []struct{ got, want map[string]any }{
		{h, map[string]any{"publisher_job": next3Actor, "creator_job": next3Actor, "new_targets_created": float64(3), "successful_CreateNew_count": float64(1), "current_files_readback_verified": float64(21), "bytes": float64(107677), "manifest_sha256": recoveryManifest, "request_sha256": recoveryRequest, "creation_input_head": "11aaab41f9e804fae36eabcfae11f79505e45751", "native_invocation_literal_equal": false, "native_invocation_trim_equal": false, "actual_invocation_characters": float64(446), "rendered_invocation_characters": float64(425), "exact_executable_operation_and_parsed_owned_paths_match": true, "current_instruction_files": float64(6), "current_instruction_bytes": float64(14197), "actual_current_workflow_decisions": float64(3), "synthetic_decisions": float64(10), "new_real_restore_customer_gateway_operations": float64(0), "implicit_selection_full_discovery_context_budget_proven": false, "new_global_raw_source_credit": float64(0), "unknown_material_or_third_party_rights_accepted": false, "publication_central_writes": float64(0), "whole16_complete": false}},
		{next4Object(h["qualified_full_review"]), map[string]any{"producer_task": float64(4215), "producer_job": recoveryProducer, "immutable_origin_job": next3Actor, "actual_author": next3Actor, "registered_spec_sha256": "5a1d14382742d0c91d541a746e1d7a67e556578587431e9c9e7edd52e55f6974", "source_proof_sha256": "0e11892ba15850d2611d7abdbfd115dfaaacc0de7c7f0a1f29ad3354afc1b39f", "fixed_head": "11aaab41f9e804fae36eabcfae11f79505e45751", "full_no_comparison": true, "selected_PUBLIC_inputs": float64(13), "payload_files": float64(21), "manifest_sha256": recoveryManifest, "normal_typed_qualified": true, "model_origin": "owner_request", "effort_origin": "owner_request", "thread_matches": true, "cwd_matches": true, "raw_sha256": "4d7c399b1635e890652dafec66c4644864ceb0a70f739ef76b9fa6e228236b91", "original_equals_job_last_message": true, "M": float64(0), "S": float64(0), "N": float64(0), "acceptance_marker_present": true}},
		{next4Object(h["frozen_Creator"]), map[string]any{"Creator_task": float64(4215), "Creator_job": recoveryProducer, "manifest_sha256": recoveryManifest, "request_sha256": recoveryRequest, "actual_CreateNew_success_observed": true, "created_files": float64(21), "existing_overwrites": float64(0), "successful_CreateNew_count": float64(1), "never_edit_restart_or_recreate": true}},
		{n, map[string]any{"job": next3Actor, "native_call_id": recoveryCall, "request_sha256": recoveryRequest, "created_files": float64(21), "created_targets": float64(3), "successful_CreateNew_count": float64(1), "creator_runtime_sha256": "7fb45b8a411ca2e1dd64a034b94c51ce5ca51324ad7c0c4b778eecb9f4538d36", "creator_runtime_revision": "b78007695a7c5d73eb1f11118757e93918aa3234", "exact_unmodified_invocation_sha256": "e2199e878f3fea949523cb830cade2104978be5d393d4301b13dae9c25a3b4d8", "rendered_preflight_invocation_sha256": "2e5531bce6397ea35011e6e29c15cb0794009ccf20245fd2102aed7e8fc25bad", "literal_invocations_equal": false, "original_CLI_stdout_sha256": "5a76333896b4da5acbe7be4572337b1d7bb739b116449f07f7693e4c0ccb9bda", "original_tool_blocks_sha256": "6249f4434552425f990e820de089a5d04c49b131032fe53318ca8667465057af", "central_file_reads_by_capture": float64(0), "other_native_bodies_returned": false}},
		{r, map[string]any{"job": next3Actor, "original_creator_job": next3Actor, "manifest_sha256": recoveryManifest, "request_sha256": recoveryRequest, "native_call_id": recoveryCall, "qualified_current_files": float64(21), "bytes": float64(107677), "normal_batches": float64(3), "all_same_read_buffers_match_manifest": true, "current_IDs_match_creation": true, "direct_central_opens_by_parent_verifier": float64(0), "new_Create_by_reader": float64(0), "atomic_snapshot_or_historical_continuity_proven": false, "current_OS_writer": "unproved", "general_access_granted": false, "original_registry_recovered": false, "whole16_complete": false}},
		{next4Object(h["native_path_spelling_difference"]), map[string]any{"actual_native_call": recoveryCall, "literal_equal": false, "trim_equal": false, "exact_executable_and_create_operation_match": true, "all6_exec_options_match_except_path_separator_spelling": true, "quoted_request_paths_resolve_to_same_explicit_owned_file": true, "workdirs_resolve_to_same_registered_root": true, "canonical_request_sha256": recoveryRequest, "original_native_renderer_and_receipt_not_rewritten": true, "normal_current_consume_3batches_accepted_same_native_request": true, "central_body_reads_by_diagnosis": float64(0), "Create_repeated": float64(0)}},
	}
	for _, c := range checks {
		if e := next4Fields(c.got, c.want); e != nil {
			return e
		}
	}
	for _, key := range []string{"original_creator_runtime_sha256", "current_runtime_sha256"} {
		if r[key] != n["creator_runtime_sha256"] {
			return fmt.Errorf("recovery runtime identity differs")
		}
	}
	if e := validateSubscriptionObservation(next4Object(h["subscription_execution"]), []string{recoveryProducer}); e != nil {
		return e
	}
	contexts := receivedRentalContexts(next4Object(h["qualified_full_review"])["native"])
	if len(contexts) != 1 {
		return fmt.Errorf("exact native review context required")
	}
	return next4Fields(contexts[0], map[string]any{"model": "gpt-6-luna", "effort": "max"})
}

func validatedRecoveryDispositions(t *testing.T) map[string]cohortLedgerRow {
	t.Helper()
	var h map[string]any
	loadPublicReceipt(t, "skills-3080-recovery-connections-installation-evidence175645.json", &h)
	files := map[string]string{"bundle": "skills-3080-recovery-connections-actual-bundle175645.json", "manifest": "skills-3080-installed-recovery-connections-manifest175645.json", "creation": "skills-3080-recovery-connections-create-evidence175645.json", "readback": "skills-3080-recovery-connections-consume-evidence175645.json", "discovery": "skills-3080-recovery-connections-discovery175645.json", "application": "skills-3080-recovery-connections-application175645.json", "design": "skills-3080-recovery-connections-actual-designs175645.json"}
	// Pins originate in the completed normal observations, independently of the
	// header's editable referenced_artifacts map.
	fixed := map[string]string{"bundle": "ddadd11d3c16b32fe601205d1439a26c1e73810e09bb3784dfe75c6b3975362e", "manifest": "dad253423466367fc058c14b3937c1ef5e1fdcae7822eac4b01832305c6dc1b7", "creation": "74a961f9dcaaa9529ffc2fff61f502db06d802bd20b253646609b489ed8fde3d", "readback": "2066e9c15160766613122820016aa07e6d9ed66666330665178cd7aeb3849d23", "discovery": "e9f775bc0e4db3569ff2ebb97799b0b678ca939a6939814f087c1b564a6ec82b", "application": "04d65cb3b0377987a2d6f91948005c348ab0f44b521d8f8aa0a8697802dd1e8d", "design": "1e343dc133cd943a75f2c4835339776a2cc2b9caf8d0b2351ef6e7278b0bf8a3"}
	pins := received27Object(t, h["referenced_artifacts"])
	if len(pins) != len(files) {
		t.Fatal("exact recovery artifact membership required")
	}
	v := map[string]map[string]any{}
	for kind, name := range files {
		var obj map[string]any
		raw := loadPublicReceipt(t, name, &obj)
		pin := received27Object(t, pins[kind])
		if travel3Digest(raw) != fixed[kind] || pin["file"] != name || pin["sha256"] != fixed[kind] {
			t.Fatal("independent recovery pin differs")
		}
		v[kind] = obj
	}
	if e := recoveryRuntimeContract(h, v["creation"], v["readback"]); e != nil {
		t.Fatal(e)
	}
	if e := rentalInstallRoots(v["bundle"], v["manifest"]); e != nil {
		t.Fatal(e)
	}
	var op struct {
		Files []struct {
			Target string `json:"target"`
			SHA    string `json:"sha256"`
			Body   string `json:"content_base64"`
		}
	}
	if travel3Digest(loadPublicReceipt(t, "skills-3080-operations/create-recovery-connections-175645.json", &op)) != recoveryManifest || len(op.Files) != 21 {
		t.Fatal("reviewed original21 changed")
	}
	bodies := map[string][]byte{}
	total := 0
	for _, f := range op.Files {
		b, e := base64.StdEncoding.DecodeString(f.Body)
		if e != nil || bodies[f.Target] != nil || travel3Digest(b) != f.SHA {
			t.Fatal("reviewed output duplicate/hash differs")
		}
		bodies[f.Target] = b
		total += len(b)
	}
	if total != 107677 {
		t.Fatal("reviewed total bytes differ")
	}
	seen := map[int]bool{}
	for _, value := range received27Array(t, v["readback"]["files"]) {
		r := received27Object(t, value)
		i, ok := r["index"].(float64)
		n := int(i)
		if !ok || i != float64(n) || n < 0 || n >= 21 || seen[n] {
			t.Fatal("readback index absent/duplicate")
		}
		seen[n] = true
		f := op.Files[n]
		id, _ := r["creation_and_current_object_identity_sha256"].(string)
		if r["target"] != f.Target || r["sha256"] != f.SHA || r["bytes"] != float64(len(bodies[f.Target])) || !validHex(id, 64) || r["creation_ID_matches_current"] != true || r["same_handle_buffer_returned"] != true || r["reviewed_bytes_equal"] != true {
			t.Fatal("current21 object/buffer differs")
		}
	}
	if len(seen) != 21 {
		t.Fatal("current readback omits outputs")
	}
	names := map[string]string{"dynamic-action-authorization": "github:d2194684c9294b536c20f3bb4cde29940e160ead91942bbc33d6d6d8e7678e55", "backup-restore-reconciliation": "github:55e8e28e664cf2e1352fab67c13c2901dccac7c26ace3fa49c97ce89fef3cd2a", "connection-scope-reconciliation": "github:5934bb2fc7fed07a55396d0c4f003f0d9ff75b7ff7142d6bc5fd7e99c0f78889"}
	for _, kind := range []string{"bundle", "manifest"} {
		seenNames, seenFiles := map[string]bool{}, map[string]bool{}
		for _, value := range received27Array(t, v[kind]["targets"]) {
			r := received27Object(t, value)
			name, _ := r["name"].(string)
			if names[name] == "" || seenNames[name] {
				t.Fatal("actual target omitted/repeated/changed")
			}
			seenNames[name] = true
			rows := received27Array(t, r["files"])
			if len(rows) != 7 {
				t.Fatal("seven actual outputs required")
			}
			for _, value := range rows {
				f := received27Object(t, value)
				path, _ := f["path"].(string)
				key := name + "/" + path
				if seenFiles[key] || bodies[key] == nil || f["sha256"] != travel3Digest(bodies[key]) {
					t.Fatal("actual bundle/metadata differs")
				}
				content, present := f["content"]
				if (kind == "bundle" && (!present || content != string(bodies[key]))) || (kind == "manifest" && present) {
					t.Fatal("actual content or metadata-only boundary differs")
				}
				seenFiles[key] = true
			}
		}
		if len(seenNames) != 3 || len(seenFiles) != 21 {
			t.Fatal("actual target/output membership differs")
		}
	}
	d := v["discovery"]
	if e := next4Fields(d, map[string]any{"found_count": float64(3), "parse_error_count": float64(0), "model_called": false, "process_tree_exit_confirmed": true}); e != nil {
		t.Fatal(e)
	}
	found := map[string]bool{}
	for _, value := range received27Array(t, d["found"]) {
		r := received27Object(t, value)
		name, _ := r["name"].(string)
		if names[name] == "" || found[name] || r["selected_central_path"] != true || r["enabled"] != true || r["enabled_present"] != true || r["description_matches"] != true {
			t.Fatal("fresh discovery mismatch")
		}
		found[name] = true
	}
	if len(found) != 3 {
		t.Fatal("fresh discovery incomplete")
	}
	a := v["application"]
	if e := next4Fields(a, map[string]any{"consumer_job": next3Actor, "manifest_sha256": recoveryManifest, "creation_request_sha256": recoveryRequest, "native_creation_call": recoveryCall, "all_six_literal_current_instruction_contents_read": true, "current_instruction_bytes": float64(14197), "actual_current_task_decision_count": float64(3), "synthetic_decision_count": float64(10), "new_model_job_called_for_this_application": false, "implicit_selection_proven": false, "full_catalog_discovery_or_context_budget_proven": false, "actual_customer_bank_gateway_machine_or_restore_operations": float64(0), "new_central_Create_or_writes_for_application": float64(0), "source_scripts_run": float64(0), "source_unknown_rights_granted": false}); e != nil {
		t.Fatal(e)
	}
	ins := map[string]bool{}
	instructionBytes := 0
	for _, value := range received27Array(t, a["current_instruction_files"]) {
		r := received27Object(t, value)
		target, _ := r["target"].(string)
		file, _ := r["file"].(string)
		key := target + "/" + file
		if names[target] == "" || ins[key] || (file != "SKILL.md" && file != "references/guide.md") || bodies[key] == nil || r["sha256"] != travel3Digest(bodies[key]) || r["bytes"] != float64(len(bodies[key])) || r["literal_current_returned_instruction_read"] != true {
			t.Fatal("actual instruction coverage differs")
		}
		ins[key] = true
		instructionBytes += len(bodies[key])
	}
	if len(ins) != 6 || instructionBytes != 14197 || len(received27Array(t, a["actual_current_task_decisions"])) != 3 || len(received27Array(t, a["synthetic_decisions"])) != 10 {
		t.Fatal("actual/synthetic or instruction counts differ")
	}
	result := map[string]cohortLedgerRow{}
	var ledger struct {
		Rows []cohortLedgerRow `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	designs := received27Array(t, v["design"]["targets"])
	if len(designs) != 3 {
		t.Fatal("three designs required")
	}
	for i, p := range recoveryProposalPins {
		want := cohortLedgerRow{ID: names[p.name], Source: "6mile/skills", Commit: "920714f17d81e0a95c7a9bc51faf0a7921cd982d", Path: p.path, SHA: p.sourceSHA, Disposition: "installed_new_adaptation"}
		design := received27Object(t, designs[i])
		if e := next4Fields(design, map[string]any{"name": p.name, "inventory_id": want.ID, "source_repo": want.Source, "source_commit": want.Commit, "source_path": want.Path, "source_sha256": want.SHA, "individual_notice_sha256": p.licenseSHA, "entrypoint_sha256": travel3Digest(bodies[p.name+"/SKILL.md"]), "included_in_single_CreateNew": true}); e != nil {
			t.Fatal(e)
		}
		if travel3Digest(bodies[p.name+"/references/LICENSE.txt"]) != p.licenseSHA || travel3Digest(bodies[p.name+"/references/original-skill.masked.txt"]) != p.visibleSHA || !strings.Contains(design["rights_scope"].(string), "Withheld meanings") {
			t.Fatal("individual notice or visible-only scope changed")
		}
		matched := 0
		for _, row := range ledger.Rows {
			if row.ID == want.ID {
				if row != want {
					t.Fatal("current recovery disposition differs")
				}
				matched++
			}
		}
		if matched != 1 {
			t.Fatal("one current recovery source required")
		}
		result[want.ID] = want
	}
	return result
}

func TestRecoveryCurrentAcceptance(t *testing.T) { validatedRecoveryDispositions(t) }

func TestRecoveryRejectsReplayedOrOverstatedRuntime(t *testing.T) {
	for _, change := range []string{"valid", "old review", "owner origin", "suggestion", "literal", "trim", "recreate", "general access", "writer", "paths", "freeze"} {
		t.Run(change, func(t *testing.T) {
			var h, n, r map[string]any
			loadPublicReceipt(t, "skills-3080-recovery-connections-installation-evidence175645.json", &h)
			loadPublicReceipt(t, "skills-3080-recovery-connections-create-evidence175645.json", &n)
			loadPublicReceipt(t, "skills-3080-recovery-connections-consume-evidence175645.json", &r)
			switch change {
			case "old review":
				next4Object(h["qualified_full_review"])["producer_job"] = "20261029-090525-todo-4215"
			case "owner origin":
				next4Object(h["qualified_full_review"])["model_origin"] = "policy"
			case "suggestion":
				next4Object(h["qualified_full_review"])["S"] = float64(1)
			case "literal":
				h["native_invocation_literal_equal"] = true
			case "trim":
				h["native_invocation_trim_equal"] = true
			case "recreate":
				r["new_Create_by_reader"] = float64(1)
			case "general access":
				r["general_access_granted"] = true
			case "writer":
				r["current_OS_writer"] = "proved"
			case "paths":
				next4Object(h["native_path_spelling_difference"])["quoted_request_paths_resolve_to_same_explicit_owned_file"] = false
			case "freeze":
				next4Object(h["frozen_Creator"])["never_edit_restart_or_recreate"] = false
			}
			e := recoveryRuntimeContract(h, n, r)
			if (change == "valid" && e != nil) || (change != "valid" && e == nil) {
				t.Fatalf("%s: %v", change, e)
			}
		})
	}
}
