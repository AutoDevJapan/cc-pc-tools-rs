package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const next4Manifest = "39ea776e81250f702902f53aa0e08df98df30d71a964edd9d4295317dcc1a209"
const next4Request = "79f7fbc0694e435a597d9b4a24bbf7ca827bc6f872c8e568ce2f0bf39507ab77"
const next4Call = "call_64138b14df4e483885c35a84ad2a2741"

func next4Object(v any) map[string]any { m, _ := v.(map[string]any); return m }
func next4Fields(m map[string]any, want map[string]any) error {
	for key, value := range want {
		if !reflect.DeepEqual(m[key], value) {
			return fmt.Errorf("next4 required field differs: %s", key)
		}
	}
	return nil
}

// This creation has two genuine runtime issuers, a nonliteral invocation and
// a later normal readback. Preserve these fields instead of projecting it
// through the older same-runtime receipt shape.
func validateNext4Contract(h, n, r map[string]any) error {
	checks := []struct{ got, want map[string]any }{
		{h, map[string]any{"publication_review_coordination_origin_job": "20261026-214717-todo-3080", "result_publisher_job": next3Actor}},
		{next4Object(h["qualified_full_review"]), map[string]any{"immutable_origin_job": next3Actor, "actual_author": next3Actor, "model_origin": "owner_request", "effort_origin": "owner_request"}},
		{h, map[string]any{"creator_job": next3Actor, "new_targets_created": float64(4), "successful_CreateNew_count": float64(1), "current_files_readback_verified": float64(28), "bytes": float64(80712), "creation_manifest_sha256": next4Manifest, "creation_request_sha256": next4Request, "implicit_skill_selection_proven": false, "full_discovery_or_context_budget_proven": false, "third_party_assets_services_or_unknown_central_rights_accepted": false, "whole16_complete": false}},
		{next4Object(h["frozen_full_content_review"]), map[string]any{"task": float64(4193), "job": "20261029-000437-todo-4193", "spec": "74728b7e206521ae607c518b438854976f09b9a93751e9ff467f73ff495315c8", "original_sha": "bd8a09b0f3f6dbcb629494a424ed90da5a6770841360d7b47dc57e9bd6d6b2fa", "manifest": next4Manifest, "request": next4Request, "created_once": float64(28), "existing_overwrites": float64(0), "M": float64(0), "S": float64(0), "N": float64(0)}},
		{next4Object(h["qualified_full_review"]), map[string]any{"producer_job": "20261029-000437-todo-4193", "registered_spec_sha256": "74728b7e206521ae607c518b438854976f09b9a93751e9ff467f73ff495315c8", "source_proof_sha256": "9cb3b4bac029328138392fce8746cae0d801c5f061289286158ada04352a14bc", "raw_sha256": "bd8a09b0f3f6dbcb629494a424ed90da5a6770841360d7b47dc57e9bd6d6b2fa", "normal_typed_qualified": true, "full_no_comparison": true, "selected_PUBLIC_inputs": float64(14), "payload_files": float64(28), "M": float64(0), "S": float64(0), "N": float64(0), "original_equals_job_last_message": true, "thread_matches": true, "cwd_matches": true}},
		{n, map[string]any{"job": next3Actor, "created_targets": float64(4), "created_files": float64(28), "successful_CreateNew_count": float64(1), "native_call_id": next4Call, "request_sha256": next4Request, "creator_runtime_sha256": "eab69e4fc8e52f2ad6858ef4e8498e8cc6030ac8ce7f788251a1aae476dc5d55", "creator_runtime_revision": "034520d84d52f76dcd7dc7ef371910d6acd8ae9f", "actual_unmodified_invocation_sha256": "d8439880cc85a444f34f695a968de73761310b014782a2c819d1e72e6c436ce0", "rendered_preflight_invocation_sha256": "14c78472bc6642bedc5d04ebdc3c475879c14371d9a9ead089ddad2f02084663", "literal_invocations_equal": false, "parsed_exec_options_and_quoted_request_paths_equivalent": true, "original_CLI_stdout_sha256": "88c7c342d9239c0f8b37511b9c7d213400623a5c250e2e2b303ba9ee87ed3c95", "original_tool_blocks_sha256": "caf16e923490249f8b37e138fb5cb050f88385af968b5c07d7d926c6c932809a", "central_file_reads_by_capture": float64(0), "new_adoptions": float64(0), "other_native_bodies_returned": false}},
		{r, map[string]any{"job": next3Actor, "original_creator_job": next3Actor, "manifest_sha256": next4Manifest, "request_sha256": next4Request, "native_call_id": next4Call, "original_creator_runtime_sha256": "eab69e4fc8e52f2ad6858ef4e8498e8cc6030ac8ce7f788251a1aae476dc5d55", "original_creator_runtime_revision": "034520d84d52f76dcd7dc7ef371910d6acd8ae9f", "current_runtime_sha256": "7fb45b8a411ca2e1dd64a034b94c51ce5ca51324ad7c0c4b778eecb9f4538d36", "current_runtime_revision": "b78007695a7c5d73eb1f11118757e93918aa3234", "qualified_current_files": float64(28), "bytes": float64(80712), "bounded_normal_batches": float64(4), "current_IDs_match_creation": true, "all_same_read_buffers_match_manifest": true, "new_creation_calls_by_reader": float64(0), "direct_central_opens_by_parent_verifier": float64(0), "new_global_raw_source_read_credit": float64(0), "atomic_snapshot_across_files_or_historical_continuity_proven": false, "current_OS_writer": "unproved", "general_access_granted": false, "original_registry_recovered": false, "whole16_complete": false}},
	}
	for _, c := range checks {
		if err := next4Fields(c.got, c.want); err != nil {
			return err
		}
	}
	if err := validateSubscriptionObservation(next4Object(h["subscription_execution"]), []string{"20261029-000437-todo-4193", "20261029-033138-todo-4203", "20261029-035105-todo-4204"}); err != nil {
		return err
	}
	if err := next4Fields(h, map[string]any{"creator_runtime_sha256": "eab69e4fc8e52f2ad6858ef4e8498e8cc6030ac8ce7f788251a1aae476dc5d55", "creator_runtime_revision": "034520d84d52f76dcd7dc7ef371910d6acd8ae9f", "current_runtime_sha256": "7fb45b8a411ca2e1dd64a034b94c51ce5ca51324ad7c0c4b778eecb9f4538d36", "current_runtime_revision": "b78007695a7c5d73eb1f11118757e93918aa3234", "software_fix_reviewed_head": "f36bd74e131e29fcc2787ba5ea0a900f91912bd8", "software_fix_merge": "b78007695a7c5d73eb1f11118757e93918aa3234", "normal_current_ID_hash_buffers_verified": true}); err != nil {
		return err
	}
	pair, ok := h["original_software_reviews"].([]any)
	if !ok || len(pair) != 2 {
		return fmt.Errorf("next4 final software pair absent")
	}
	code3, code4 := next4Object(pair[0]), next4Object(pair[1])
	if err := next4Fields(code3, map[string]any{"producer_task": float64(4203), "producer_job": "20261029-033138-todo-4203", "head": "f36bd74e131e29fcc2787ba5ea0a900f91912bd8", "spec_sha": "c5b705499352d98f15fa82e1761251eeaa48e38e053a63f99c4bffab6cde3919", "raw_sha": "5c4a05aa9627c5bf89b2f8b42d17fe523ee118bfb951760b058162fd5c5b419c", "normal_qualified": true, "original_equals_job_last_message": true, "accepted": true, "M": float64(0), "S": float64(0), "N": float64(0), "thread_matches": true, "cwd_matches": true}); err != nil {
		return err
	}
	if err := next4Fields(code4, map[string]any{"producer_task": float64(4204), "producer_job": "20261029-035105-todo-4204", "fixed_head": "f36bd74e131e29fcc2787ba5ea0a900f91912bd8", "registered_spec_sha256": "e91ed81f9abd08e1f6c469c73dc042797773a2cc78b8105f33d0413cbc8fb74e", "raw_sha256": "266a418692c300e35268945064385bbe0a41bde5cea3113fc0935592201c543f", "normal_result_qualified": true, "original_equals_job_last_message": true, "M": float64(0), "S": float64(0), "N": float64(0), "thread_matches": true, "cwd_matches": true}); err != nil {
		return err
	}
	if err := next4Fields(next4Object(code4["code_acceptance"]), map[string]any{"head": "f36bd74e131e29fcc2787ba5ea0a900f91912bd8", "accepted": true, "must": float64(0), "suggestion": float64(0), "nit": float64(0)}); err != nil {
		return err
	}
	for _, value := range []struct {
		record map[string]any
		key    string
	}{{code3, "native"}, {code4, "actual_native"}} {
		if err := next4Fields(value.record, map[string]any{"model_origin": "owner_request", "effort_origin": "owner_request"}); err != nil {
			return err
		}
		contexts, ok := value.record[value.key].([]any)
		if !ok || len(contexts) == 0 {
			return fmt.Errorf("next4 software actual native absent")
		}
		for _, ctx := range contexts {
			if err := next4Fields(next4Object(ctx), map[string]any{"model": "gpt-6-luna", "effort": "max"}); err != nil {
				return err
			}
		}
	}
	contexts, ok := next4Object(h["qualified_full_review"])["native"].([]any)
	if !ok || len(contexts) == 0 {
		return fmt.Errorf("next4 actual full-review native context absent")
	}
	for _, ctx := range contexts {
		if err := next4Fields(next4Object(ctx), map[string]any{"model": "gpt-6-luna", "effort": "max"}); err != nil {
			return err
		}
	}
	return nil
}

func validatedNextFourDispositions(t *testing.T) map[string]cohortLedgerRow {
	t.Helper()
	var h map[string]any
	loadPublicReceipt(t, "skills-3080-next-four-installation-evidence175645.json", &h)
	names := map[string]string{"balance-snapshot-reconciliation": "github:25e4fda6fe6264a4bd4ada96cac534c91ca9bec7c8c6bba333f9b16330eeb483", "browser-extraction-missingness": "github:58702b56afec89eb19801b922c4a44c309d6caf773aa0036533f0e064914719e", "remote-run-result-reconciliation": "github:aa9395d1b8d7707cde0c3bc7c03691e142c478fabe947548504811085309d69a", "codebase-impact-context": "github:192881db536eb8fa09e04f09aa051710b7540b92da63234eddecb424982996cc"}
	files := map[string]string{"bundle": "skills-3080-next-four-actual-bundle175645.json", "manifest": "skills-3080-installed-next-four-manifest175645.json", "creation": "skills-3080-next-four-create-evidence175645.json", "readback": "skills-3080-next-four-consume-evidence175645.json", "structure": "skills-3080-next-four-actual-structure175645.json", "discovery": "skills-3080-next-four-discovery175645.json", "application": "skills-3080-next-four-application175645.json", "design": "skills-3080-next-four-actual-designs175645.json", "precreation": "skills-3080-next-four-precreation-qualification175645.json", "catalog": "skills-3080-next-four-catalog-identity175645.json"}
	fixed := map[string]string{"creation": "48e27f24596f650f91a8620ef54110995a3b52b20b27cad989a23150a4ef4646", "readback": "a4adb02a06d88dd1cd40a7405070472ce1fee489d406e11cf79c68ea6f132076", "application": "dec20303db12c0ea6552c7105b3bfac834d1676c18c3275d3647e3c3a71adb91", "discovery": "ddd3a8f919d9e9a01c279bf330e409da6c0a914574613dfa55cfb85523ea73a9"}
	values := map[string]map[string]any{}
	raws := map[string][]byte{}
	pins := received27Object(t, h["referenced_artifacts"])
	if len(pins) != len(files) {
		t.Fatal("next4 receipt membership differs")
	}
	for kind, file := range files {
		pin := received27Object(t, pins[kind])
		var value map[string]any
		raw := loadPublicReceipt(t, file, &value)
		if pin["file"] != file || pin["sha256"] != travel3Digest(raw) || fixed[kind] != "" && fixed[kind] != travel3Digest(raw) {
			t.Fatal("next4 independent observation pin differs")
		}
		values[kind] = value
		raws[kind] = raw
	}
	if err := validateNext4Contract(h, values["creation"], values["readback"]); err != nil {
		t.Fatal(err)
	}
	var op struct {
		Files []struct {
			Target string `json:"target"`
			SHA    string `json:"sha256"`
			Body   string `json:"content_base64"`
		}
	}
	opRaw := loadPublicReceipt(t, "skills-3080-operations/create-next-four-175645.json", &op)
	if travel3Digest(opRaw) != next4Manifest || len(op.Files) != 28 {
		t.Fatal("next4 original manifest differs")
	}
	bodies := map[string][]byte{}
	total := 0
	for _, f := range op.Files {
		b, err := base64.StdEncoding.DecodeString(f.Body)
		if err != nil || bodies[f.Target] != nil || travel3Digest(b) != f.SHA {
			t.Fatal("next4 original payload differs")
		}
		bodies[f.Target] = b
		total += len(b)
	}
	if total != 80712 {
		t.Fatal("next4 exact original byte count differs")
	}
	seen := map[int]bool{}
	for _, v := range received27Array(t, values["readback"]["files"]) {
		r := received27Object(t, v)
		i := int(r["index"].(float64))
		if i < 0 || i >= 28 || seen[i] {
			t.Fatal("next4 read index differs")
		}
		seen[i] = true
		f := op.Files[i]
		if r["target"] != f.Target || r["sha256"] != f.SHA || r["bytes"] != float64(len(bodies[f.Target])) || !validHex(r["created_and_current_object_identity_sha256"].(string), 64) || r["creation_ID_matches_current"] != true || r["same_handle_buffer_returned"] != true || r["reviewed_bytes_equal"] != true {
			t.Fatal("next4 current object/buffer differs")
		}
	}
	if len(seen) != 28 {
		t.Fatal("next4 current row missing")
	}
	matchedFiles := map[string]bool{}
	targets := map[string]bool{}
	for _, v := range received27Array(t, values["bundle"]["targets"]) {
		target := received27Object(t, v)
		name := target["name"].(string)
		if names[name] == "" || targets[name] {
			t.Fatal("next4 target differs")
		}
		targets[name] = true
		rows := received27Array(t, target["files"])
		if len(rows) != 7 {
			t.Fatal("next4 seven outputs required")
		}
		for _, v := range rows {
			f := received27Object(t, v)
			key := name + "/" + f["path"].(string)
			if matchedFiles[key] || bodies[key] == nil || string(bodies[key]) != f["content"] || travel3Digest(bodies[key]) != f["sha256"] {
				t.Fatal("next4 actual content differs")
			}
			matchedFiles[key] = true
		}
	}
	if len(matchedFiles) != 28 || len(targets) != 4 {
		t.Fatal("next4 exact bundle missing")
	}
	installed := values["manifest"]
	if err := next4Fields(installed, map[string]any{"root": "D:/CC/Skills", "creator_job": next3Actor, "current_consumer_job": next3Actor}); err != nil {
		t.Fatal(err)
	}
	installedNames := map[string]bool{}
	installedFiles := map[string]bool{}
	for _, value := range received27Array(t, installed["targets"]) {
		target := received27Object(t, value)
		name := target["name"].(string)
		if !targets[name] || installedNames[name] {
			t.Fatal("next4 installed target differs")
		}
		installedNames[name] = true
		files := received27Array(t, target["files"])
		if len(files) != 7 {
			t.Fatal("next4 installed seven-file scope differs")
		}
		for _, value := range files {
			file := received27Object(t, value)
			key := name + "/" + file["path"].(string)
			if installedFiles[key] || bodies[key] == nil || file["sha256"] != travel3Digest(bodies[key]) {
				t.Fatal("next4 installed manifest content pin differs")
			}
			if _, exists := file["content"]; exists {
				t.Fatal("next4 installed manifest must remain metadata only")
			}
			installedFiles[key] = true
		}
	}
	if len(installedNames) != 4 || len(installedFiles) != 28 {
		t.Fatal("next4 installed manifest omitted a target/file")
	}
	discovery := values["discovery"]
	if err := next4Fields(discovery, map[string]any{"found_count": float64(4), "parse_error_count": float64(0), "model_called": false, "process_tree_exit_confirmed": true}); err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, v := range received27Array(t, discovery["found"]) {
		r := received27Object(t, v)
		name := r["name"].(string)
		if !targets[name] || found[name] || r["selected_central_path"] != true || r["enabled_present"] != true || r["enabled"] != true || r["description_matches"] != true {
			t.Fatal("next4 discovered row differs")
		}
		found[name] = true
	}
	if len(found) != 4 {
		t.Fatal("next4 discovery missing")
	}
	app := values["application"]
	if err := next4Fields(app, map[string]any{"consumer_job": next3Actor, "all_scenario_meanings_applied": true, "instruction_files": float64(8), "instruction_bytes": float64(13640), "actual_current_workflow_decisions": float64(3), "synthetic_decisions": float64(5), "implicit_skill_selection_proven": false, "new_model_job_spawned": float64(0), "new_service_or_browser_or_payment_operations_for_these_cases": float64(0), "new_keys_fees_permissions": float64(0), "additional_central_writes_or_Create": float64(0)}); err != nil {
		t.Fatal(err)
	}
	if app["readback_proof_sha256"] != values["readback"]["original_private_readback_proof_sha256"] {
		t.Fatal("next4 application not bound to original normal readback")
	}
	ins := map[string]bool{}
	instructionBytes := 0
	for _, v := range received27Array(t, app["instructions"]) {
		r := received27Object(t, v)
		target := r["target"].(string)
		body := bodies[target]
		if ins[target] || body == nil || !(strings.HasSuffix(target, "/SKILL.md") || strings.HasSuffix(target, "/references/guide.md")) || r["sha256"] != travel3Digest(body) || r["bytes"] != float64(len(body)) || r["literal_current_buffer_read_in_actual_parent_turn"] != true {
			t.Fatal("next4 actual instruction differs")
		}
		ins[target] = true
		instructionBytes += len(body)
	}
	if len(ins) != 8 || instructionBytes != 13640 {
		t.Fatal("next4 instruction coverage differs")
	}
	var application struct {
		Decisions []notice4Case `json:"decisions"`
	}
	if json.Unmarshal(raws["application"], &application) != nil {
		t.Fatal("next4 application invalid")
	}
	if err := validateNext4Cases(application.Decisions); err != nil {
		t.Fatal(err)
	}
	structure := values["structure"]
	if structure["exit"] != float64(0) || structure["additional_central_writes"] != float64(0) || !strings.HasPrefix(structure["stdout"].(string), "PASS: 4 explicit skills;") || next4Object(structure["bundle"])["sha256"] != travel3Digest(raws["bundle"]) {
		t.Fatal("next4 structure receipt differs")
	}
	pre := values["precreation"]
	if err := next4Fields(pre, map[string]any{"qualified": true, "request_sha256": next4Request, "manifest_sha256": next4Manifest, "created_files": float64(0), "targets": float64(4), "payload_files": float64(28)}); err != nil {
		t.Fatal(err)
	}
	catalog := values["catalog"]
	if catalog["catalog_records"] != float64(73220) || catalog["catalog_sha256"] != "2d73c9a038a41cc220a9687b935bfa1e0b315b65d9bd6d3e54ddb4111972876c" || catalog["same_read_buffer_catalog_identity_verified"] != true {
		t.Fatal("next4 catalog identity differs")
	}
	byID := map[string]map[string]any{}
	for _, v := range received27Array(t, catalog["source_rows"]) {
		r := received27Object(t, v)
		byID[r["inventory_id"].(string)] = r
	}
	if len(byID) != 4 {
		t.Fatal("next4 exact catalog source count differs")
	}
	result := map[string]cohortLedgerRow{}
	for name, id := range names {
		var source struct {
			ID     string `json:"source_inventory_id"`
			Source struct {
				Repo         string `json:"repository"`
				Commit, Path string
				SHA          string `json:"sha256"`
				License      struct {
					SHA      string `json:"sha256"`
					Scope    string
					Accepted *bool
				}
			}
			Profile struct {
				Raw     *bool `json:"raw_full"`
				Unknown *bool `json:"unseen_meanings_remain_unknown"`
			} `json:"primary_read_profile"`
			Copied        *bool `json:"source_original_body_copied"`
			NonAuthorship *bool `json:"source_published_notice_not_independently_verified_authorship"`
		}
		if json.Unmarshal(bodies[name+"/references/source.json"], &source) != nil || source.ID != id || source.Source.Repo != "6mile/skills" || source.Source.Commit != "920714f17d81e0a95c7a9bc51faf0a7921cd982d" || !recordedTrue(source.Source.License.Accepted) || source.Source.License.Scope == "" || travel3Digest(bodies[name+"/references/LICENSE.txt"]) != source.Source.License.SHA || !recordedFalse(source.Profile.Raw) || !recordedTrue(source.Profile.Unknown) || !recordedFalse(source.Copied) || !recordedTrue(source.NonAuthorship) {
			t.Fatal("next4 limited individual source scope differs")
		}
		row := byID[id]
		if row["path"] != source.Source.Path || row["sha256"] != source.Source.SHA || row["repo"] != source.Source.Repo || row["commit"] != source.Source.Commit {
			t.Fatal("next4 source not tied to fixed catalog")
		}
		result[id] = cohortLedgerRow{ID: id, Source: source.Source.Repo, Commit: source.Source.Commit, Path: source.Source.Path, SHA: source.Source.SHA, Disposition: "installed_new_adaptation"}
	}
	var design struct {
		Targets []struct {
			directSkillDesign
			Rights          string `json:"rights_scope"`
			IncludedInBatch bool   `json:"included_in_single_CreateNew"`
		}
		BatchCalls int `json:"successful_CreateNew_count"`
	}
	if json.Unmarshal(raws["design"], &design) != nil || len(design.Targets) != 4 || design.BatchCalls != 1 {
		t.Fatal("next4 actual design membership differs")
	}
	designIDs := map[string]bool{}
	for _, row := range design.Targets {
		want, ok := result[row.ID]
		if !ok || !row.IncludedInBatch || designIDs[row.ID] || names[row.Name] != row.ID || row.Repo != want.Source || row.Commit != want.Commit || row.Path != want.Path || row.SHA != want.SHA || row.EntrySHA != travel3Digest(bodies[row.Name+"/SKILL.md"]) || row.Rights == "" || row.NoticeSHA != travel3Digest(bodies[row.Name+"/references/LICENSE.txt"]) {
			t.Fatal("next4 design does not match independently bound source/output")
		}
		designIDs[row.ID] = true
	}
	var ledger struct {
		Rows []struct {
			cohortLedgerRow
			Target string `json:"target"`
		} `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	matched := map[string]bool{}
	for _, r := range ledger.Rows {
		if want, ok := result[r.ID]; ok {
			if matched[r.ID] || want != r.cohortLedgerRow || names[r.Target] != r.ID {
				t.Fatal("next4 live ledger differs")
			}
			matched[r.ID] = true
		}
	}
	if len(matched) != 4 {
		t.Fatal("next4 live row missing")
	}
	return result
}

// Pin the actual model decisions, not a claim that arbitrary future advice or
// a synthetic fixture is a real financial/browser/service execution.
func validateNext4Cases(rows []notice4Case) error {
	pins := []string{"b3b45d9e3bd22813870d3a2cbc52cabfe1b182259f502f390cdaa5e27dc2104c", "31d4c1693b6c30f2e2f212ecc163f1a011dde5821cec304038fe6855395792bf", "5413fd68f3829185ade9e7cddef30fc832a6d29ad25124b72080f113862a2a0f", "403f74efe04f8d145ba5b5847990e8b659a5995e03eb4a8ac7f0338da235ec01", "8b4faee7a9f32b3cbf8035ce5214ab29fcc8e9664308c22faee3b6e92e0c814e", "b13a7e09671a8fda104edd7954bf035ce0f83ee62ef7046b4829f262a66c930e", "a0712a6fa4171a09c4fab646c09e2d75d3c468045b57e8ea91e197df24ca024b", "b99a703fecff5e35218537d270c6e22d0983ce2d4b42b0daf64693caaa8d8875"}
	if len(rows) != 8 {
		return fmt.Errorf("next4 case count differs")
	}
	names := []string{"balance-snapshot-reconciliation", "browser-extraction-missingness", "remote-run-result-reconciliation", "codebase-impact-context"}
	for i, r := range rows {
		actual := i == 2 || i == 4 || i == 6
		if r.Skill != names[i/2] || r.Input == "" || r.Decision == "" || actual != strings.HasPrefix(r.Kind, "actual current-workflow") || (!actual) != strings.HasPrefix(r.Kind, "synthetic text-only") || travel3Digest([]byte(strings.Join([]string{r.Skill, r.Kind, r.Input, r.Decision}, "\n"))) != pins[i] {
			return fmt.Errorf("next4 historical input/decision or real/synthetic class differs")
		}
	}
	return nil
}

func TestNextFourCurrentAcceptance(t *testing.T) { validatedNextFourDispositions(t) }

func TestNextFourRejectsRewrittenContractAndApplication(t *testing.T) {
	for _, change := range []string{"valid", "manifest", "creator", "runtime conflated", "new Create", "literal rewritten", "general access", "unknown writer", "review scope", "review result", "coordination missing", "coordination conflated", "creator review origin conflated", "model origin missing", "model origin wrong", "effort origin missing", "effort origin wrong", "OAuth metadata missing", "OAuth metadata non subscription", "additional credits enabled", "historical billing invented"} {
		t.Run(change, func(t *testing.T) {
			var h, n, r map[string]any
			loadPublicReceipt(t, "skills-3080-next-four-installation-evidence175645.json", &h)
			loadPublicReceipt(t, "skills-3080-next-four-create-evidence175645.json", &n)
			loadPublicReceipt(t, "skills-3080-next-four-consume-evidence175645.json", &r)
			switch change {
			case "manifest":
				h["creation_manifest_sha256"] = "changed"
			case "creator":
				n["job"] = "other"
			case "runtime conflated":
				r["current_runtime_sha256"] = n["creator_runtime_sha256"]
			case "new Create":
				r["new_creation_calls_by_reader"] = float64(1)
			case "literal rewritten":
				n["literal_invocations_equal"] = true
			case "general access":
				r["general_access_granted"] = true
			case "unknown writer":
				r["current_OS_writer"] = "proved"
			case "review scope":
				next4Object(h["qualified_full_review"])["selected_PUBLIC_inputs"] = float64(13)
			case "review result":
				next4Object(h["qualified_full_review"])["S"] = float64(1)
			case "coordination missing":
				delete(h, "publication_review_coordination_origin_job")
			case "coordination conflated":
				h["publication_review_coordination_origin_job"] = next3Actor
			case "creator review origin conflated":
				next4Object(h["qualified_full_review"])["immutable_origin_job"] = "20261026-214717-todo-3080"
			case "model origin missing":
				delete(next4Object(h["qualified_full_review"]), "model_origin")
			case "model origin wrong":
				next4Object(h["qualified_full_review"])["model_origin"] = "ai_hint"
			case "effort origin missing":
				delete(next4Object(h["qualified_full_review"]), "effort_origin")
			case "effort origin wrong":
				next4Object(h["qualified_full_review"])["effort_origin"] = "default"
			case "OAuth metadata missing":
				delete(h, "subscription_execution")
			case "OAuth metadata non subscription", "additional credits enabled", "historical billing invented":
				condition := next4Object(h["subscription_execution"])
				if condition == nil {
					condition = map[string]any{}
					h["subscription_execution"] = condition
				}
				if change == "OAuth metadata non subscription" {
					condition["mode"] = "api_key"
				} else if change == "additional credits enabled" {
					condition["all_additional_credits_disabled"] = false
				} else {
					condition["historical_per_request_billing_proven"] = true
				}
			}
			err := validateNext4Contract(h, n, r)
			if change == "valid" && err != nil || change != "valid" && err == nil {
				t.Fatalf("contract %s: %v", change, err)
			}
		})
	}
	var a struct {
		Rows []notice4Case `json:"decisions"`
	}
	loadPublicReceipt(t, "skills-3080-next-four-application175645.json", &a)
	a.Rows[0].Decision = "all balances complete"
	if validateNext4Cases(a.Rows) == nil {
		t.Fatal("changed actual decision accepted")
	}
}
