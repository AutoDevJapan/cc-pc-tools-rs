package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
)

const rentalManifestSHA = "9181080be684764ecc89a6c8a77138f45e79507b470fb76c66ecc0fb6c5a3572"
const rentalRequestSHA = "da7029cef27b8376970475710fbd1f49aedf2384a9f583657fe660b0436c88ba"
const rentalCall = "call_800d6a534c354a5fb1db2a2d455ae852"

func rentalRuntimeContract(h, n, r map[string]any) error {
	for _, pair := range []struct{ got, want map[string]any }{
		{h, map[string]any{"creator_job": next3Actor, "publisher_job": next3Actor, "new_targets_created": float64(1), "successful_CreateNew_count": float64(1), "current_files_readback_verified": float64(7), "bytes": float64(37868), "manifest_sha256": rentalManifestSHA, "request_sha256": rentalRequestSHA, "publication_review_coordination_origin_job": "20261026-214717-todo-3080", "native_invocation_literal_equal": false, "native_invocation_only_outer_whitespace_differs": true, "actual_invocation_characters": float64(434), "rendered_invocation_characters": float64(433), "current_instruction_files": float64(2), "current_instruction_bytes": float64(7895), "actual_current_workflow_decisions": float64(1), "synthetic_decisions": float64(6), "new_real_financial_customer_service_operations": float64(0), "implicit_selection_full_discovery_context_budget_proven": false, "unknown_material_or_third_party_rights_accepted": false, "publication_central_writes": float64(0), "whole16_complete": false}},
		{next4Object(h["qualified_full_review"]), map[string]any{"producer_task": float64(4208), "producer_job": "20261029-061701-todo-4208", "immutable_origin_job": next3Actor, "actual_author": next3Actor, "registered_spec_sha256": "65385471dee9ea8ed2c9f364d430c728e36cb8601c5556d13cdeef541999eb29", "fixed_head": "f7032dff0bbe895960edbbe40a31f192b5c813f0", "full_no_comparison": true, "selected_PUBLIC_inputs": float64(12), "payload_files": float64(7), "manifest_sha256": rentalManifestSHA, "normal_typed_qualified": true, "model_origin": "owner_request", "effort_origin": "owner_request", "thread_matches": true, "cwd_matches": true, "original_equals_job_last_message": true, "raw_sha256": "13e47ee941667e1c9334de75bedd50ef2826a4f7d7160c731f55c7929298e669", "M": float64(0), "S": float64(0), "N": float64(0), "acceptance_marker_present": true}},
		{next4Object(h["frozen_Creator"]), map[string]any{"Creator_task": float64(4208), "Creator_job": "20261029-061701-todo-4208", "manifest_sha256": rentalManifestSHA, "request_sha256": rentalRequestSHA, "created_files": float64(7), "existing_overwrites": float64(0), "successful_CreateNew_count": float64(1), "never_edit_restart_or_recreate": true}},
		{n, map[string]any{"job": next3Actor, "native_call_id": rentalCall, "request_sha256": rentalRequestSHA, "created_files": float64(7), "created_targets": float64(1), "successful_CreateNew_count": float64(1), "creator_runtime_sha256": "7fb45b8a411ca2e1dd64a034b94c51ce5ca51324ad7c0c4b778eecb9f4538d36", "creator_runtime_revision": "b78007695a7c5d73eb1f11118757e93918aa3234", "exact_unmodified_invocation_sha256": "4793642e7213f302efe0793a6d2519e9e33081e3f136e69abcdebcd4fecbfdcd", "rendered_preflight_invocation_sha256": "0c0ddad484a4fad1281996315f19285ff5160620a4f1db8f624fd3c506ec8b42", "literal_invocations_equal": false, "original_CLI_stdout_sha256": "8596757bd2368c294eaccd55a3d0aa2b5a3db6abcbc4e72dd700aec345042b52", "original_tool_blocks_sha256": "0b32e8b9bbbc8a4dbef10dff34543f8d5973a1965f2f61b03fe0c640c126db5d", "central_file_reads_by_capture": float64(0), "other_native_bodies_returned": false}},
		{r, map[string]any{"job": next3Actor, "original_creator_job": next3Actor, "manifest_sha256": rentalManifestSHA, "request_sha256": rentalRequestSHA, "native_call_id": rentalCall, "qualified_current_files": float64(7), "bytes": float64(37868), "normal_batches": float64(1), "all_same_read_buffers_match_manifest": true, "current_IDs_match_creation": true, "direct_central_opens_by_parent_verifier": float64(0), "new_Create_by_reader": float64(0), "atomic_snapshot_or_historical_continuity_proven": false, "current_OS_writer": "unproved", "general_access_granted": false, "original_registry_recovered": false, "whole16_complete": false}},
	} {
		if e := next4Fields(pair.got, pair.want); e != nil {
			return e
		}
	}
	for _, key := range []string{"original_creator_runtime_sha256", "current_runtime_sha256"} {
		if r[key] != "7fb45b8a411ca2e1dd64a034b94c51ce5ca51324ad7c0c4b778eecb9f4538d36" {
			return fmt.Errorf("rental exact runtime differs")
		}
	}
	if err := validateSubscriptionObservation(next4Object(h["subscription_execution"]), []string{"20261029-061701-todo-4208"}); err != nil {
		return err
	}
	for _, v := range receivedRentalContexts(next4Object(h["qualified_full_review"])["native"]) {
		if e := next4Fields(v, map[string]any{"model": "gpt-6-luna", "effort": "max"}); e != nil {
			return e
		}
	}
	if len(receivedRentalContexts(next4Object(h["qualified_full_review"])["native"])) == 0 {
		return fmt.Errorf("rental native proof absent")
	}
	return nil
}
func receivedRentalContexts(value any) []map[string]any {
	a, _ := value.([]any)
	out := []map[string]any{}
	for _, v := range a {
		out = append(out, next4Object(v))
	}
	return out
}

func rentalInstallRoots(bundle, installed map[string]any) error {
	if err := next4Fields(bundle, map[string]any{"root": "D:/CC/Skills"}); err != nil {
		return err
	}
	return next4Fields(installed, map[string]any{"root": "D:/CC/Skills", "creator_job": next3Actor, "current_consumer_job": next3Actor})
}

func validatedRentalDispositions(t *testing.T) map[string]cohortLedgerRow {
	t.Helper()
	var h map[string]any
	loadPublicReceipt(t, "skills-3080-rental-ledger-installation-evidence175645.json", &h)
	names := map[string]string{"bundle": "skills-3080-rental-ledger-actual-bundle175645.json", "manifest": "skills-3080-installed-rental-ledger-manifest175645.json", "creation": "skills-3080-rental-ledger-create-evidence175645.json", "readback": "skills-3080-rental-ledger-consume-evidence175645.json", "discovery": "skills-3080-rental-ledger-discovery175645.json", "application": "skills-3080-rental-ledger-application175645.json", "design": "skills-3080-rental-ledger-actual-designs175645.json"}
	fixed := map[string]string{"creation": "34cb9757e549786db4a52bd5fea390fe02718acf965268d7c8408b0c944f17a3", "readback": "4b46f6e31f38b193661566b72b67b679cfb8b506873bfc901c161903e050f42b", "discovery": "760baa9df78cf36f8964a516d67a20a5f3c1543f9ac9999d9e7fd50b1c2ea28c", "application": "c8b4142ec2cd1023e42bf953cf583f4d8120695db01c21fa5360ed0ed68f638c"}
	pins := received27Object(t, h["referenced_artifacts"])
	if len(pins) != 7 {
		t.Fatal("exact rental receipt membership required")
	}
	values := map[string]map[string]any{}
	for kind, file := range names {
		var v map[string]any
		raw := loadPublicReceipt(t, file, &v)
		pin := received27Object(t, pins[kind])
		if pin["file"] != file || pin["sha256"] != travel3Digest(raw) || fixed[kind] != "" && fixed[kind] != travel3Digest(raw) {
			t.Fatal("rental independent artifact pin differs")
		}
		values[kind] = v
	}
	if e := rentalRuntimeContract(h, values["creation"], values["readback"]); e != nil {
		t.Fatal(e)
	}
	if e := rentalInstallRoots(values["bundle"], values["manifest"]); e != nil {
		t.Fatal(e)
	}
	var op struct {
		Files []struct {
			Target string `json:"target"`
			SHA    string `json:"sha256"`
			Body   string `json:"content_base64"`
		} `json:"files"`
	}
	raw := loadPublicReceipt(t, "skills-3080-operations/create-rental-ledger-175645.json", &op)
	if travel3Digest(raw) != rentalManifestSHA || len(op.Files) != 7 {
		t.Fatal("original rental operation differs")
	}
	bodies := map[string][]byte{}
	total := 0
	for _, f := range op.Files {
		b, e := base64.StdEncoding.DecodeString(f.Body)
		if e != nil || bodies[f.Target] != nil || travel3Digest(b) != f.SHA || !strings.HasPrefix(f.Target, "rental-ledger-reconciliation/") {
			t.Fatal("original reviewed rental output differs")
		}
		bodies[f.Target] = b
		total += len(b)
	}
	if total != 37868 {
		t.Fatal("exact rental bytes differ")
	}
	seen := map[int]bool{}
	for _, v := range received27Array(t, values["readback"]["files"]) {
		r := received27Object(t, v)
		i, ok := r["index"].(float64)
		n := int(i)
		if !ok || i != float64(n) || n < 0 || n >= 7 || seen[n] {
			t.Fatal("rental current index absent/repeated")
		}
		seen[n] = true
		f := op.Files[n]
		id, _ := r["creation_and_current_object_identity_sha256"].(string)
		if r["target"] != f.Target || r["sha256"] != f.SHA || r["bytes"] != float64(len(bodies[f.Target])) || !validHex(id, 64) || r["creation_ID_matches_current"] != true || r["same_handle_buffer_returned"] != true || r["reviewed_bytes_equal"] != true {
			t.Fatal("rental current object/hash/buffer differs")
		}
	}
	if len(seen) != 7 {
		t.Fatal("rental current file omitted")
	}
	targets := received27Array(t, values["bundle"]["targets"])
	if len(targets) != 1 {
		t.Fatal("one actual target required")
	}
	target := received27Object(t, targets[0])
	if target["name"] != "rental-ledger-reconciliation" {
		t.Fatal("rental target changed")
	}
	seenFiles := map[string]bool{}
	for _, v := range received27Array(t, target["files"]) {
		f := received27Object(t, v)
		path, _ := f["path"].(string)
		key := "rental-ledger-reconciliation/" + path
		if seenFiles[key] || bodies[key] == nil || string(bodies[key]) != f["content"] || travel3Digest(bodies[key]) != f["sha256"] {
			t.Fatal("actual rental content differs from reviewed operation")
		}
		seenFiles[key] = true
	}
	if len(seenFiles) != 7 {
		t.Fatal("actual bundle omits file")
	}
	installed := values["manifest"]
	it := received27Array(t, installed["targets"])
	if len(it) != 1 {
		t.Fatal("rental installed manifest target omitted")
	}
	if received27Object(t, it[0])["name"] != "rental-ledger-reconciliation" {
		t.Fatal("installed rental manifest changed target name")
	}
	installedFiles := map[string]bool{}
	for _, v := range received27Array(t, received27Object(t, it[0])["files"]) {
		f := received27Object(t, v)
		path, _ := f["path"].(string)
		key := "rental-ledger-reconciliation/" + path
		if installedFiles[key] || bodies[key] == nil || f["sha256"] != travel3Digest(bodies[key]) {
			t.Fatal("installed rental metadata differs")
		}
		if _, present := f["content"]; present {
			t.Fatal("installed manifest must remain metadata only")
		}
		installedFiles[key] = true
	}
	if len(installedFiles) != 7 {
		t.Fatal("installed metadata file omitted")
	}
	if e := next4Fields(values["discovery"], map[string]any{"found_count": float64(1), "parse_error_count": float64(0), "model_called": false, "process_tree_exit_confirmed": true}); e != nil {
		t.Fatal(e)
	}
	found := received27Array(t, values["discovery"]["found"])
	if len(found) != 1 {
		t.Fatal("one discovered rental target required")
	}
	if e := next4Fields(received27Object(t, found[0]), map[string]any{"name": "rental-ledger-reconciliation", "selected_central_path": true, "enabled": true, "enabled_present": true, "description_matches": true}); e != nil {
		t.Fatal(e)
	}
	app := values["application"]
	if e := next4Fields(app, map[string]any{"consumer_job": next3Actor, "current_instruction_buffers_actually_read": true, "instruction_files": float64(2), "instruction_bytes": float64(7895), "actual_current_workflow_decisions": float64(1), "synthetic_decisions": float64(6), "all_case_meanings_applied": true, "implicit_skill_selection_proven": false, "new_model_job_spawned": float64(0), "new_actual_financial_customer_or_bank_operations": float64(0), "new_service_API_UI_or_payment_operations": float64(0), "new_keys_fees_permissions": float64(0), "additional_Create_or_central_writes": float64(0)}); e != nil {
		t.Fatal(e)
	}
	if app["normal_readback_proof_sha256"] != values["readback"]["original_private_readback_proof_sha256"] {
		t.Fatal("rental application not tied to actual current read")
	}
	instructionNames := map[string]bool{}
	ib := 0
	for _, v := range received27Array(t, app["instructions"]) {
		r := received27Object(t, v)
		path, _ := r["path"].(string)
		key := "rental-ledger-reconciliation/" + path
		if instructionNames[path] || (path != "SKILL.md" && path != "references/guide.md") || r["sha256"] != travel3Digest(bodies[key]) || r["bytes"] != float64(len(bodies[key])) || r["normal_readback_buffer_actually_read"] != true {
			t.Fatal("actual instruction changed")
		}
		instructionNames[path] = true
		ib += len(bodies[key])
	}
	if len(instructionNames) != 2 || ib != 7895 {
		t.Fatal("actual2 instruction reading incomplete")
	}
	cases := received27Array(t, app["decisions"])
	casePins := []string{"b1e0d2e0b13c92cea6a877d921e54d9d4efd0e70dc67c8c694b945c874bb60b1", "c50e970c057132c30fb5fcdf6ad5caa3bfcb1f2d214704722e595b8adec2a09f", "312e09cfb6b0f3e6d25711ac873e2736cc4ef971629228213dcffcd80117fca6", "f2eb1c7cf1c1817bee85ffc9f56dd15e575cd541014cbf80dc7e64dab087602e", "99d83e04b90571f897b25502b56f77548d9a3864d87989f5b73247f84de70ff4", "98ad2e19b87f67c05ee6548e5e4eda4b9bec56c845a0f81e5c4c47eb2101b358", "00a8f6f818153d02fd51670189f92402e39feea4b186bda6ad9bf2f73a1913dd"}
	if len(cases) != 7 {
		t.Fatal("actual1 and synthetic6 cases required")
	}
	for i, v := range cases {
		c := received27Object(t, v)
		kind, _ := c["kind"].(string)
		input, _ := c["input"].(string)
		decision, _ := c["decision"].(string)
		if (i == 0) != (kind == "actual current research-workflow") || (i > 0) != (kind == "synthetic text-only") || travel3Digest([]byte(strings.Join([]string{kind, input, decision}, "\n"))) != casePins[i] {
			t.Fatal("actual/synthetic classification or model decision changed")
		}
	}
	id := "github:1834bb0001544b6acd228b9f9fd2eab265895ec9f7cdf256b24ad63e3689033d"
	want := cohortLedgerRow{ID: id, Source: "6mile/skills", Commit: "920714f17d81e0a95c7a9bc51faf0a7921cd982d", Path: "skills/mailnike/propclaw/SKILL.md", SHA: "ffcebd1dd25bf25767d69cc242b597309233c1383c65a53281bcbbd2529b1ac0", Disposition: "installed_new_adaptation"}
	var design struct {
		Targets []struct {
			directSkillDesign
			Rights   string `json:"rights_scope"`
			Included bool   `json:"included_in_single_CreateNew"`
		}
		Calls int `json:"successful_CreateNew_count"`
	}
	loadPublicReceipt(t, "skills-3080-rental-ledger-actual-designs175645.json", &design)
	if len(design.Targets) != 1 || design.Calls != 1 {
		t.Fatal("rental actual design count differs")
	}
	d := design.Targets[0]
	if d.ID != want.ID || d.Name != "rental-ledger-reconciliation" || d.Repo != want.Source || d.Commit != want.Commit || d.Path != want.Path || d.SHA != want.SHA || d.EntrySHA != travel3Digest(bodies["rental-ledger-reconciliation/SKILL.md"]) || d.NoticeSHA != "6f6bb192fb5854c274ca1bd5f3705ad6d4577099d2db95f46c90d0657ffb830d" || d.Rights == "" || !d.Included {
		t.Fatal("rental design not bound to individual source/output")
	}
	var ledger struct {
		Rows []cohortLedgerRow `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	n := 0
	for _, r := range ledger.Rows {
		if r.ID == id {
			if r != want {
				t.Fatal("live rental tuple differs")
			}
			n++
		}
	}
	if n != 1 {
		t.Fatal("one actual rental disposition required")
	}
	return map[string]cohortLedgerRow{id: want}
}

func TestRentalCurrentAcceptance(t *testing.T) { validatedRentalDispositions(t) }

func TestRentalInstallRootsRejectMissingOrChangedPlacement(t *testing.T) {
	for _, mutation := range []string{"valid", "bundle missing", "bundle other root", "manifest missing", "manifest other root", "consumer other job"} {
		t.Run(mutation, func(t *testing.T) {
			var bundle, installed map[string]any
			loadPublicReceipt(t, "skills-3080-rental-ledger-actual-bundle175645.json", &bundle)
			loadPublicReceipt(t, "skills-3080-installed-rental-ledger-manifest175645.json", &installed)
			switch mutation {
			case "bundle missing":
				delete(bundle, "root")
			case "bundle other root":
				bundle["root"] = "E:/Skills"
			case "manifest missing":
				delete(installed, "root")
			case "manifest other root":
				installed["root"] = "C:/Skills"
			case "consumer other job":
				installed["current_consumer_job"] = "other"
			}
			err := rentalInstallRoots(bundle, installed)
			if mutation == "valid" && err != nil || mutation != "valid" && err == nil {
				t.Fatalf("placement mutation %s: %v", mutation, err)
			}
		})
	}
}
func TestRentalRuntimeRejectsUnprovedOrReplayedAcceptance(t *testing.T) {
	for _, mutation := range []string{"valid", "creator origin", "owner model origin", "suggestion", "new Create", "changed runtime", "literal rewritten", "general access", "invented writer"} {
		t.Run(mutation, func(t *testing.T) {
			var h, n, r map[string]any
			loadPublicReceipt(t, "skills-3080-rental-ledger-installation-evidence175645.json", &h)
			loadPublicReceipt(t, "skills-3080-rental-ledger-create-evidence175645.json", &n)
			loadPublicReceipt(t, "skills-3080-rental-ledger-consume-evidence175645.json", &r)
			switch mutation {
			case "creator origin":
				next4Object(h["qualified_full_review"])["immutable_origin_job"] = "20261026-214717-todo-3080"
			case "owner model origin":
				next4Object(h["qualified_full_review"])["model_origin"] = "policy"
			case "suggestion":
				next4Object(h["qualified_full_review"])["S"] = float64(1)
			case "new Create":
				r["new_Create_by_reader"] = float64(1)
			case "changed runtime":
				r["current_runtime_sha256"] = "other"
			case "literal rewritten":
				n["literal_invocations_equal"] = true
			case "general access":
				r["general_access_granted"] = true
			case "invented writer":
				r["current_OS_writer"] = "proven"
			}
			e := rentalRuntimeContract(h, n, r)
			if mutation == "valid" && e != nil || mutation != "valid" && e == nil {
				t.Fatalf("mutation %s: %v", mutation, e)
			}
		})
	}
}
