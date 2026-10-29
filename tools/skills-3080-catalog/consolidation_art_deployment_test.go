package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"testing"
)

const consolidationManifest = "73aa131a6761ec48b14000d294e7f77b7e961d17c4cbf3dafe64752dcf3bdd25"
const consolidationRequest = "bad8c95b6c9abda64eb7a6d8c61c6935c0b087a8342911cb62e5552ab82f9f95"
const consolidationCall = "call_f3fd7d6488e94a06a409fb63ea3dc104"

func consolidationCurrentRuntime(h, n, r map[string]any) error {
	for _, c := range []struct{ got, want map[string]any }{
		{h, map[string]any{"publisher_job": next3Actor, "creator_job": next3Actor, "new_targets_created": float64(2), "successful_CreateNew_count": float64(1), "current_files_readback_verified": float64(14), "bytes": float64(77902), "manifest_sha256": consolidationManifest, "request_sha256": consolidationRequest, "creation_input_head": "dca4514105560162002e2e6611f210e1c0326f97", "native_invocation_literal_equal": false, "semantic_native_exec_request_identity_verified_by_capture": true, "current_instruction_files": float64(4), "current_instruction_bytes": float64(10454), "actual_current_workflow_decisions": float64(2), "synthetic_decisions": float64(5), "real_art_exports_or_memory_engine_operations": float64(0), "implicit_selection_full_discovery_context_budget_proven": false, "unknown_material_or_third_party_rights_accepted": false, "new_global_raw_source_credit": float64(0), "publication_central_writes": float64(0), "whole16_complete": false}},
		{next4Object(h["qualified_full_review"]), map[string]any{"task": float64(4222), "actual_job": "20261029-152517-todo-4222", "actual_author": next3Actor, "registered_spec_sha256": "d54c53dd2b79fd4cdcc5567242dd299db717fd716f34c66b181af39a3b67e908", "fixed_commit": "dca4514105560162002e2e6611f210e1c0326f97", "full_PUBLIC_inputs": float64(10), "all14_outputs_full_review": true, "comparison_absent": true, "model_origin": "owner_request", "effort_origin": "owner_request", "thread_matches": true, "cwd_matches": true, "normal_typed_qualified": true, "raw_sha256": "f803366450863b5ea3322e58ff9a524ddb500de8abd322168de688a1fec85041", "M": float64(0), "S": float64(0), "N": float64(0), "acceptance_marker_present": true}},
		{next4Object(h["frozen_Creator"]), map[string]any{"Creator_task": float64(4222), "Creator_job": "20261029-152517-todo-4222", "manifest_sha256": consolidationManifest, "request_sha256": consolidationRequest, "actual_CreateNew_success_observed": true, "successful_CreateNew_count": float64(1), "created_files": float64(14), "never_edit_restart_or_recreate": true}},
		{n, map[string]any{"job": next3Actor, "native_call_id": consolidationCall, "request_sha256": consolidationRequest, "created_files": float64(14), "created_targets": float64(2), "successful_CreateNew_count": float64(1), "creator_runtime_sha256": "7fb45b8a411ca2e1dd64a034b94c51ce5ca51324ad7c0c4b778eecb9f4538d36", "creator_runtime_revision": "b78007695a7c5d73eb1f11118757e93918aa3234", "literal_invocations_equal": false, "exact_unmodified_invocation_sha256": "cd0439612793cc95136548dfbaa0cb7d7ecca0945f4e6141362ee7e36a633b4c", "original_CLI_stdout_sha256": "99cf3547c6f2d4f8b9b12c48fdca297114e093a8cf30d205d0928046afd4f57c", "central_file_reads_by_capture": float64(0), "other_native_bodies_returned": false}},
		{r, map[string]any{"job": next3Actor, "original_creator_job": next3Actor, "manifest_sha256": consolidationManifest, "request_sha256": consolidationRequest, "native_call_id": consolidationCall, "qualified_current_files": float64(14), "bytes": float64(77902), "normal_batches": float64(2), "all_same_read_buffers_match_manifest": true, "current_IDs_match_creation": true, "direct_central_opens_by_parent_verifier": float64(0), "new_Create_by_reader": float64(0), "historical_physical_continuity_proven": false, "current_OS_writer": "unproved", "general_access_granted": false, "original_registry_recovered": false, "whole16_complete": false}},
	} {
		if e := next4Fields(c.got, c.want); e != nil {
			return e
		}
	}
	contexts := receivedRentalContexts(next4Object(h["qualified_full_review"])["native"])
	if len(contexts) != 1 {
		return fmt.Errorf("one independently observed review context required")
	}
	return next4Fields(contexts[0], map[string]any{"model": "gpt-6-luna", "effort": "max"})
}

func validatedConsolidationDispositions(t *testing.T) map[string]cohortLedgerRow {
	t.Helper()
	var h map[string]any
	loadPublicReceipt(t, "skills-3080-consolidation-art-installation-evidence175645.json", &h)
	files := map[string]string{"bundle": "skills-3080-consolidation-art-actual-bundle175645.json", "manifest": "skills-3080-installed-consolidation-art-manifest175645.json", "creation": "skills-3080-consolidation-art-create-evidence175645.json", "readback": "skills-3080-consolidation-art-consume-evidence175645.json", "discovery": "skills-3080-consolidation-art-discovery175645.json", "application": "skills-3080-consolidation-art-application175645.json", "design": "skills-3080-consolidation-art-actual-designs175645.json"}
	// These independent pins retain the actual post-creation observation buffers.
	fixed := map[string]string{"bundle": "84585be544c87539f0d622cc0e6c31a2d1bb9ad7611410d526c4e9130329158e", "manifest": "690d05dcd12d8d36142a275262874a7e37f75a6167277bcdbfc3da0937e4994d", "creation": "e024b266c032a3e322c8f658eb274e19a421d651bde62feab27b9fcff16e7294", "readback": "5890173378696349a30cfb04e3e162df5d2f65675f5252c06b8d16533e42d925", "discovery": "718e8b6df6b840953576394818579cb4b023c67f2addf267f389038b98413636", "application": "34b97144adf62c77e1a6ab5d0c0750af786e3476fe3b21c4fcc3fd927890a671", "design": "710d96e4076baac2300298f2b93f5c73ee57d067b2db4a5dc324ec8907f1b883"}
	pins := received27Object(t, h["referenced_artifacts"])
	if len(pins) != len(files) {
		t.Fatal("exact current artifact membership required")
	}
	v := map[string]map[string]any{}
	for kind, name := range files {
		var obj map[string]any
		raw := loadPublicReceipt(t, name, &obj)
		pin := received27Object(t, pins[kind])
		if travel3Digest(raw) != fixed[kind] || pin["file"] != name || pin["sha256"] != fixed[kind] {
			t.Fatal("actual current artifact pin differs")
		}
		v[kind] = obj
	}
	if e := consolidationCurrentRuntime(h, v["creation"], v["readback"]); e != nil {
		t.Fatal(e)
	}
	proposal, op, prep, proposalPins := loadConsolidationFixture(t)
	if e := validateConsolidationArtProposal(proposal, op, prep, proposalPins); e != nil {
		t.Fatal(e)
	}
	if proposalPins.ManifestSHA != consolidationManifest {
		t.Fatal("accepted proposal manifest differs")
	}
	if e := rentalInstallRoots(v["bundle"], v["manifest"]); e != nil {
		t.Fatal(e)
	}
	bodies := map[string][]byte{}
	ordered := received27Array(t, op["files"])
	total := 0
	for _, value := range ordered {
		f := received27Object(t, value)
		key, _ := f["target"].(string)
		encoded, _ := f["content_base64"].(string)
		raw, e := base64.StdEncoding.DecodeString(encoded)
		if e != nil || bodies[key] != nil || travel3Digest(raw) != f["sha256"] {
			t.Fatal("reviewed output repeated or changed")
		}
		bodies[key] = raw
		total += len(raw)
	}
	if len(bodies) != 14 || total != 77902 {
		t.Fatal("all14 reviewed bytes required")
	}
	seen := map[int]bool{}
	for _, value := range received27Array(t, v["readback"]["files"]) {
		r := received27Object(t, value)
		f, ok := r["index"].(float64)
		i := int(f)
		if !ok || f != float64(i) || i < 0 || i >= len(ordered) || seen[i] {
			t.Fatal("current readback index repeated or absent")
		}
		seen[i] = true
		want := received27Object(t, ordered[i])
		key := want["target"].(string)
		id, _ := r["creation_and_current_object_identity_sha256"].(string)
		if r["target"] != key || r["sha256"] != want["sha256"] || r["bytes"] != float64(len(bodies[key])) || !validHex(id, 64) || r["creation_ID_matches_current"] != true || r["same_handle_buffer_returned"] != true || r["reviewed_bytes_equal"] != true {
			t.Fatal("actual current buffer and object binding differs")
		}
	}
	if len(seen) != 14 {
		t.Fatal("current readback incomplete")
	}
	names := map[string]string{"memory-consolidation-state": "github:a1b6f6f944d4f177da0d88112cf8fb0e828e71accabe844d9294169008ffe036", "generative-art-reproducibility": "github:db3a296d432ef9beb84fb1c5a128b5297b2f0112b5c61ab1ac248005c2f97a0f"}
	for _, kind := range []string{"bundle", "manifest"} {
		ns, fs := map[string]bool{}, map[string]bool{}
		for _, value := range received27Array(t, v[kind]["targets"]) {
			r := received27Object(t, value)
			name, _ := r["name"].(string)
			if names[name] == "" || ns[name] {
				t.Fatal("current target membership differs")
			}
			ns[name] = true
			rows := received27Array(t, r["files"])
			if len(rows) != 7 {
				t.Fatal("seven outputs per target required")
			}
			for _, value := range rows {
				f := received27Object(t, value)
				path, _ := f["path"].(string)
				key := name + "/" + path
				raw := bodies[key]
				content, present := f["content"]
				if fs[key] || raw == nil || f["sha256"] != travel3Digest(raw) || (kind == "bundle" && (!present || content != string(raw))) || (kind == "manifest" && present) {
					t.Fatal("actual metadata/content differs from reviewed bytes")
				}
				fs[key] = true
			}
		}
		if len(ns) != 2 || len(fs) != 14 {
			t.Fatal("actual outputs incomplete")
		}
	}
	if e := next4Fields(v["discovery"], map[string]any{"targets": float64(2), "found_count": float64(2), "enabled_count": float64(2), "new_description_match_count": float64(2), "parse_error_count": float64(0), "model_called": false, "process_tree_exit_confirmed": true, "pending_stdout_read_drained": true}); e != nil {
		t.Fatal(e)
	}
	a := v["application"]
	if e := next4Fields(a, map[string]any{"actual_job": next3Actor, "manifest_sha256": consolidationManifest, "native_call_id": consolidationCall, "current_instruction_files": float64(4), "current_instruction_bytes": float64(10454), "all_four_literal_current_instruction_contents_read": true, "actual_current_task_decision_count": float64(2), "synthetic_decision_count": float64(5), "real_art_exports_checked": float64(0), "real_memory_service_or_storage_engine_tested": float64(0), "model_turn_implicit_selection_or_context_budget_proven": false, "new_central_writes": float64(0), "whole16_complete": false}); e != nil {
		t.Fatal(e)
	}
	ins := map[string]bool{}
	size := 0
	for _, value := range received27Array(t, a["instructions"]) {
		r := received27Object(t, value)
		name, _ := r["name"].(string)
		path, _ := r["path"].(string)
		key := name + "/" + path
		raw := bodies[key]
		if names[name] == "" || (path != "SKILL.md" && path != "references/guide.md") || ins[key] || raw == nil || r["sha256"] != travel3Digest(raw) || r["bytes"] != float64(len(raw)) || r["literal_current_buffer_read"] != true {
			t.Fatal("current instruction read coverage differs")
		}
		ins[key] = true
		size += len(raw)
	}
	if len(ins) != 4 || size != 10454 || len(received27Array(t, a["actual_current_task_decisions"])) != 2 || len(received27Array(t, a["synthetic_cases"])) != 5 {
		t.Fatal("current decision/reading counts differ")
	}
	var ledger struct {
		Rows []cohortLedgerRow `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	designs := received27Array(t, v["design"]["targets"])
	if len(designs) != 2 {
		t.Fatal("two designs required")
	}
	result := map[string]cohortLedgerRow{}
	for _, value := range designs {
		d := received27Object(t, value)
		name, _ := d["name"].(string)
		var s map[string]any
		if names[name] == "" || json.Unmarshal(bodies[name+"/references/source.json"], &s) != nil {
			t.Fatal("current source unavailable")
		}
		want := cohortLedgerRow{ID: names[name], Source: s["source_repo"].(string), Commit: s["source_commit"].(string), Path: s["source_path"].(string), SHA: s["source_sha256"].(string), Disposition: "installed_new_adaptation"}
		if result[want.ID].ID != "" {
			t.Fatal("repeated accepted source")
		}
		if e := next4Fields(d, map[string]any{"inventory_id": want.ID, "source_repo": want.Source, "source_commit": want.Commit, "source_path": want.Path, "source_sha256": want.SHA, "individual_notice_sha256": s["individual_notice_sha256"], "entrypoint_sha256": travel3Digest(bodies[name+"/SKILL.md"]), "included_in_single_CreateNew": true}); e != nil {
			t.Fatal(e)
		}
		matched := 0
		for _, r := range ledger.Rows {
			if r.ID == want.ID {
				if r != want {
					t.Fatal("actual current disposition differs")
				}
				matched++
			}
		}
		if matched != 1 {
			t.Fatal("one actual current disposition required")
		}
		result[want.ID] = want
	}
	return result
}

func TestConsolidationCurrentAcceptance(t *testing.T) { validatedConsolidationDispositions(t) }

func TestConsolidationRejectsReplayedOrOverstatedRuntime(t *testing.T) {
	for _, change := range []string{"valid", "old review", "missing origin", "suggestion", "different request", "literal", "recreate", "writer", "continuity", "unfrozen", "general access"} {
		t.Run(change, func(t *testing.T) {
			var h, n, r map[string]any
			loadPublicReceipt(t, "skills-3080-consolidation-art-installation-evidence175645.json", &h)
			loadPublicReceipt(t, "skills-3080-consolidation-art-create-evidence175645.json", &n)
			loadPublicReceipt(t, "skills-3080-consolidation-art-consume-evidence175645.json", &r)
			switch change {
			case "old review":
				next4Object(h["qualified_full_review"])["actual_job"] = "20261029-132802-todo-4222"
			case "missing origin":
				delete(next4Object(h["qualified_full_review"]), "model_origin")
			case "suggestion":
				next4Object(h["qualified_full_review"])["S"] = float64(1)
			case "different request":
				r["request_sha256"] = recoveryRequest
			case "literal":
				n["literal_invocations_equal"] = true
			case "recreate":
				r["new_Create_by_reader"] = float64(1)
			case "writer":
				r["current_OS_writer"] = "proved"
			case "continuity":
				r["historical_physical_continuity_proven"] = true
			case "unfrozen":
				next4Object(h["frozen_Creator"])["never_edit_restart_or_recreate"] = false
			case "general access":
				r["general_access_granted"] = true
			}
			err := consolidationCurrentRuntime(h, n, r)
			if (change == "valid" && err != nil) || (change != "valid" && err == nil) {
				t.Fatalf("%s runtime result: %v", change, err)
			}
		})
	}
}

func TestOriginal3719Next6After288PreservesThreeWholePackets(t *testing.T) {
	checkOriginal3719PendingSnapshot(t, "skills-3080-original3719-next6-289294-evaluation175645.json", "5f8c538cbee83004dd540a86da36c6a7977fb9f0a981723cb363a112d0a69df6", 289, 294, 3, "20261028-171959-todo-3719", 73, 48, 25)
}
