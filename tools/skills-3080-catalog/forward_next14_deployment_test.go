package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

const next14ManifestSHA = "3dd611655a517ac4734c53cdea296cd0f84070c612f0a88ef376880b262df7b5"

// Keep the actual invocation separate from the preflight spelling. The returned
// bytes and object IDs bind the result, not an invented canonical call history.
func TestNext14ActualInvocationBuffersAndAdoptionIdentity(t *testing.T) {
	var c struct {
		forward11Create
		Invocation string `json:"exact_invocation_sha256"`
		Canonical  string `json:"preflight_canonical_invocation_sha256"`
		Equal      *bool  `json:"preflight_canonical_invocation_equal_actual"`
	}
	loadPublicReceipt(t, "skills-3080-webdesign-next14-create-evidence.json", &c)
	if !recordedTrue(c.Qualified) || c.Count != 84 || c.Files != 84 || len(c.Rows) != 84 || c.Manifest != next14ManifestSHA || c.Request != "fc09686812d53330dbb97a913471293e740985d52032678eab89b251ada7353d" || c.Call != "call_08059cbdd8c6475c99a211d0e2d72c1d" || c.Commit != "5d4a05597944d2db355fa09262051d551be8645a" || !recordedFalse(c.General) || !recordedFalse(c.Registry) {
		t.Fatal("actual14 creation or permission boundary changed")
	}
	if c.Producer.SHA != "03a2e0f115c8435b1a876eda3efba75ba6120a668bfed39539f6ef2503c5f165" || c.Producer.Revision != "3154c8ac9135424e2caf2034dc24d4e7406d6368" || c.OutputSHA != "29ca71a41bbefc15492981bc0c4c623044590811254509633c7e904d7bbd3b8d" || c.BlocksSHA != "0295c97e53e9de99c59cc42426d0d2d0a9ead5d353cee40ff037452cef7b5718" || c.Invocation != "c3e62c7ee19ae03aaf2f897f32038c321644038da5577587658f7c210dd6b528" || c.Canonical != "64f80cc5cec1bd173825a979ee70d85ac7a8bc984e5c8fc3571db635cd226fb4" || !recordedFalse(c.Equal) {
		t.Fatal("actual native output or distinct preflight history changed")
	}
	var op struct {
		Files []struct {
			Target string `json:"target"`
			SHA    string `json:"sha256"`
			Body   string `json:"content_base64"`
		} `json:"files"`
	}
	raw := loadPublicReceipt(t, "skills-3080-operations/create-webdesign-next14-074042.json", &op)
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != next14ManifestSHA || len(op.Files) != 84 {
		t.Fatal("reviewed operation changed")
	}
	hashes := map[string]string{}
	for i, f := range op.Files {
		r := c.Rows[i]
		body, err := base64.StdEncoding.DecodeString(f.Body)
		h := sha256.Sum256(body)
		if err != nil || hex.EncodeToString(h[:]) != f.SHA || len(body) != r.Bytes || r.Index != i || r.Target != f.Target || r.SHA != f.SHA || hashes[f.Target] != "" || !validHex(r.ID, 64) || !recordedTrue(r.Same) || !recordedTrue(r.Regular) || !recordedTrue(r.Buffer) || !recordedTrue(r.Reviewed) {
			t.Fatal("created/current/operation bytes or identity differ", i)
		}
		hashes[f.Target] = f.SHA
	}
	var consumed struct {
		Rows         []forward11File `json:"files"`
		Count        int             `json:"qualified_files"`
		Instructions int             `json:"instruction_live_model_read_files"`
		Audit        int             `json:"audit_files_not_new_model_display"`
		Continuity   *bool           `json:"historical_physical_continuity_proven"`
		General      *bool           `json:"general_access_granted"`
		Registry     *bool           `json:"original_registry_recovered"`
		Writer       string          `json:"current_os_writer"`
		Structure    int             `json:"structure_passed"`
	}
	loadPublicReceipt(t, "skills-3080-webdesign-next14-consume-evidence.json", &consumed)
	if !reflect.DeepEqual(c.Rows, consumed.Rows) || consumed.Count != 84 || consumed.Instructions != 28 || consumed.Audit != 56 || consumed.Structure != 14 || !recordedFalse(consumed.Continuity) || !recordedFalse(consumed.General) || !recordedFalse(consumed.Registry) || consumed.Writer != "unproved" {
		t.Fatal("consume evidence promotes an unproved guarantee")
	}
	var input struct {
		New []struct {
			ID     string `json:"inventory_id"`
			Name   string `json:"source_name"`
			Repo   string `json:"source_repo"`
			Commit string `json:"source_commit"`
			Path   string `json:"source_path"`
			SHA    string `json:"source_sha256"`
		} `json:"new_targets"`
	}
	raw = loadPublicReceipt(t, "skills-3080-webdesign-next14-inputs074042.json", &input)
	h = sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != "a2ea5376344db1a378d9de602b637e4c87a685f25ca33f25740f6ee07bec85dd" || len(input.New) != 14 {
		t.Fatal("independent14 source roster changed")
	}
	want := map[string]purposeAdoptionRow{}
	for _, s := range input.New {
		want[s.ID] = purposeAdoptionRow{cohortLedgerRow: cohortLedgerRow{ID: s.ID, Source: s.Repo, Commit: s.Commit, Path: s.Path, SHA: s.SHA, Disposition: "installed_new_adaptation"}, Target: s.Name, Manifest: "skills-3080-installed-webdesign-next14-manifest.json", EntrySHA: hashes[s.Name+"/SKILL.md"], Evidence: "skills-3080-webdesign-next14-installation-evidence.json"}
	}
	var ledger struct {
		Rows []purposeAdoptionRow `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	if err := validatePurposeAdoptionRows(want, ledger.Rows); err != nil {
		t.Fatal(err)
	}
}

func TestNext14LivePlanningExamplesDoNotClaimRenderingOrImplicitSelection(t *testing.T) {
	var use struct {
		Manifest string `json:"manifest_sha256"`
		Bundle   string `json:"actual_buffer_bundle_sha256"`
		Positive int    `json:"positive_cases"`
		Negative int    `json:"negative_cases"`
		Cases    []struct {
			Index       int    `json:"manifest_instruction_index"`
			Instruction string `json:"instruction_sha256"`
			Guide       string `json:"guide_sha256"`
			Positive    string `json:"positive_case"`
			Decision    string `json:"decision_from_live_instruction_buffer"`
			Negative    string `json:"negative_case"`
			Reason      string `json:"excluded_behavior_reason"`
			Applied     *bool  `json:"actual_model_instruction_use"`
			Product     *bool  `json:"actual_product_UI_or_API_execution"`
		} `json:"cases"`
	}
	useRaw := loadPublicReceipt(t, "skills-3080-webdesign-next14-application-result.json", &use)
	if use.Manifest != next14ManifestSHA || use.Bundle != "817e013f0577aeb059e7fed371ee25996c2413fce8323d756acf61193fbf5ef0" || use.Positive != 14 || use.Negative != 14 || len(use.Cases) != 14 {
		t.Fatal("live planning cases missing")
	}
	var created forward11Create
	loadPublicReceipt(t, "skills-3080-webdesign-next14-create-evidence.json", &created)
	for i, c := range use.Cases {
		if c.Index != i*6 || c.Instruction != created.Rows[i*6].SHA || c.Guide != created.Rows[i*6+1].SHA || c.Positive == "" || c.Decision == "" || c.Negative == "" || c.Reason == "" || !recordedTrue(c.Applied) || !recordedFalse(c.Product) {
			t.Fatal("example borrowed another live buffer or claimed product execution", i)
		}
	}
	var math struct {
		Checks            int    `json:"checks"`
		Passed            *bool  `json:"passed"`
		UI                *bool  `json:"GPU_or_UI_execution"`
		Code              *bool  `json:"upstream_code_executed"`
		Implementation    string `json:"implementation"`
		ImplementationSHA string `json:"implementation_sha256"`
		ApplicationSHA    string `json:"application_result_sha256"`
	}
	loadPublicReceipt(t, "skills-3080-webdesign-next14-math-receipt.json", &math)
	if math.Checks != 40 || !recordedTrue(math.Passed) || !recordedFalse(math.UI) || !recordedFalse(math.Code) {
		t.Fatal("numeric scope changed")
	}
	if math.Implementation != "evidence/skills-3080-next14-independent-math074042.go.txt" {
		t.Fatal("numeric source path differs")
	}
	implementation, err := os.ReadFile("../../docs/" + math.Implementation)
	if err != nil {
		t.Fatal(err)
	}
	implementationSHA := sha256.Sum256(implementation)
	applicationSHAExpected := sha256.Sum256(useRaw)
	if math.ImplementationSHA != hex.EncodeToString(implementationSHA[:]) || math.ApplicationSHA != hex.EncodeToString(applicationSHAExpected[:]) {
		t.Fatal("numeric source or application pin differs")
	}
	var evidence map[string]json.RawMessage
	loadPublicReceipt(t, "skills-3080-webdesign-next14-installation-evidence.json", &evidence)
	h := sha256.Sum256(useRaw)
	var applicationSHA string
	if json.Unmarshal(evidence["application_result_sha256"], &applicationSHA) != nil || applicationSHA != hex.EncodeToString(h[:]) {
		t.Fatal("application evidence not pinned")
	}
	for _, field := range []string{"overall_complete", "general_access_granted", "original_registry_recovered", "separate_fresh_model_turn_executed", "implicit_skill_selection_proven", "renderer_or_GPU_or_SDK_execution_proven", "real_UI_changed", "source_scripts_executed", "full_context_budget_proven", "warning_cause_linked"} {
		var b *bool
		if json.Unmarshal(evidence[field], &b) != nil || !recordedFalse(b) {
			t.Fatal("missing or promoted boundary", field)
		}
	}
	var dis struct {
		Targets    int                `json:"targets"`
		FoundCount int                `json:"found_count"`
		Parse      *int               `json:"parse_error_count"`
		Model      *bool              `json:"model_called"`
		Exited     *bool              `json:"process_tree_exit_confirmed"`
		Drained    *bool              `json:"pending_stdout_read_drained"`
		Found      []newSkillMetadata `json:"found"`
	}
	loadPublicReceipt(t, "skills-3080-webdesign-next14-discovery-receipt.json", &dis)
	if dis.Targets != 14 || dis.FoundCount != 14 || len(dis.Found) != 14 || dis.Parse == nil || *dis.Parse != 0 || !recordedFalse(dis.Model) || !recordedTrue(dis.Exited) || !recordedTrue(dis.Drained) {
		t.Fatal("actual fresh metadata discovery incomplete")
	}
	seen := map[string]bool{}
	for _, r := range dis.Found {
		if hashes := created.Rows; seen[r.Name] || !recordedTrue(r.Central) || !recordedTrue(r.EnabledPresent) || !recordedTrue(r.Enabled) || !recordedTrue(r.Description) {
			t.Fatal("metadata missing or repeated")
		} else {
			found := false
			for _, f := range hashes {
				if f.Target == r.Name+"/SKILL.md" {
					found = true
				}
			}
			if !found {
				t.Fatal("foreign discovered target")
			}
		}
		seen[r.Name] = true
	}
}

func TestForward11FreshQualifiedCallerRetainsOrdinaryHoldAndSeparatePolicyAudit(t *testing.T) {
	var p struct {
		Manifest string `json:"manifest_sha256"`
		Reader   string `json:"reader_job"`
		Spec     string `json:"reader_spec_sha256"`
		Count    int    `json:"files"`
		Bytes    int    `json:"bytes"`
		Rows     []struct {
			Index int    `json:"manifest_index"`
			SHA   string `json:"sha256"`
			Bytes int    `json:"bytes"`
		} `json:"file_tuples"`
		Examples []struct {
			Index    int    `json:"instruction_index"`
			Positive string `json:"positive"`
			Negative string `json:"negative"`
		} `json:"semantic_examples"`
		Native struct {
			Call        string `json:"call_id"`
			Exit        *int   `json:"exit_code"`
			Unforwarded *bool  `json:"exit_code_not_independently_forwarded"`
			SHA         string `json:"CLI_JSON_sha256"`
		} `json:"native"`
		Audit struct {
			Read     *bool `json:"self_reported_read_after_guard_denial"`
			Complete *bool `json:"parent_independent_scope_audit_complete"`
			Clean    *bool `json:"whole_run_unconditional_scope_compliance_claimed"`
		} `json:"extra_policy_read_audit"`
	}
	raw := loadPublicReceipt(t, "skills-3080-webdesign11-fresh-qualified-use074042.json", &p)
	if p.Manifest != forward11ManifestSHA || p.Reader != "20261026-051732-todo-3885" || p.Spec != "acb4b1c9480cb682f7ed3966e90cecdc81807571647ba2ba1bf36dc4206dc845" || p.Count != 22 || p.Bytes != 34474 || len(p.Rows) != 22 || len(p.Examples) != 11 || p.Native.Call != "call_4d76fee2b71d4a31864aecd5be18bfaf" || p.Native.Exit != nil || !recordedTrue(p.Native.Unforwarded) || p.Native.SHA != "7431ad78ee1d98e40186892bc80c296b5eb8e5c4722de9f813e4afcfaa2c68d9" || !recordedTrue(p.Audit.Read) || !recordedFalse(p.Audit.Complete) || !recordedFalse(p.Audit.Clean) {
		t.Fatal("actual fresh caller proof or unresolved audit changed")
	}
	var c forward11Create
	loadPublicReceipt(t, "skills-3080-webdesign11-create-evidence.json", &c)
	total := 0
	for i, r := range p.Rows {
		if r.Index < 0 || r.Index >= len(c.Rows) || r.SHA != c.Rows[r.Index].SHA || r.Bytes != c.Rows[r.Index].Bytes || (i > 0 && r.Index <= p.Rows[i-1].Index) {
			t.Fatal("fresh files not bound to original creation")
		}
		total += r.Bytes
	}
	if total != p.Bytes {
		t.Fatal("fresh byte count differs")
	}
	for i, e := range p.Examples {
		if e.Index != p.Rows[2*i].Index || p.Rows[2*i+1].Index != e.Index+1 || e.Positive == "" || e.Negative == "" {
			t.Fatal("missing fresh positive/negative buffer pair")
		}
	}
	var fields map[string]json.RawMessage
	if err := decodePublicReceipt(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"general_guard_access_granted", "ordinary_all_agents_availability_proven", "implicit_routing_proven", "product_rendering_executed", "historical_physical_continuity_proven", "original_registry_recovered", "previous_ordinary_guard_hold_reversed", "whole16_complete"} {
		var b *bool
		if json.Unmarshal(fields[key], &b) != nil || !recordedFalse(b) {
			t.Fatal("technical success promoted unrelated guarantee", key)
		}
	}
	var evidence struct {
		Fresh struct {
			SHA string `json:"sha256"`
		} `json:"fresh_qualified_use_acceptance"`
	}
	loadPublicReceipt(t, "skills-3080-webdesign11-installation-evidence.json", &evidence)
	h := sha256.Sum256(raw)
	if evidence.Fresh.SHA != hex.EncodeToString(h[:]) {
		t.Fatal("fresh proof pin differs")
	}
}
