package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

const next20ManifestSHA = "df3a781e73580f2fb5e7a00cba71d18d6aec889277d4713ab5d5d61d8d2d0ad7"
const next20RuntimeSHA = "ccb5e797e6b5627d98ade2a562f2f98f3c121a5403e1523071afc07f73af4556"

func next20CreationValid(c forward11Create) error {
	if !recordedTrue(c.Qualified) || c.Domain != "skills-3080-purpose-v1" || c.Operation != "CreateNew" || c.Todo != 3080 || c.Count != 120 || c.Files != 120 || len(c.Rows) != 120 || c.Request != "de7926fc27ce376ee15cd6b62c478c8e4072e5ce7ba54686ae8293a6889cf1b9" || c.Manifest != next20ManifestSHA || c.Call != "call_38df7f39dd1b445b9637eca6c0dee17b" || c.Commit != "5d4a05597944d2db355fa09262051d551be8645a" || !recordedFalse(c.General) || !recordedFalse(c.Registry) {
		return errors.New("next20 original creation boundary differs")
	}
	if c.Producer.SHA != "03a2e0f115c8435b1a876eda3efba75ba6120a668bfed39539f6ef2503c5f165" || c.Producer.Revision != "3154c8ac9135424e2caf2034dc24d4e7406d6368" || c.OutputSHA != "3e91483c6dcbbccd762f4ff76312cff4121200f475bc2c916fc8222c0fe7a59b" || c.BlocksSHA == "" {
		return errors.New("next20 issuing producer or original native output differs")
	}
	seen := map[string]bool{}
	for i, r := range c.Rows {
		if r.Index != i || seen[r.Target] || r.Target == "" || r.Bytes < 1 || !validHex(r.SHA, 64) || !validHex(r.ID, 64) || !recordedTrue(r.Same) || !recordedTrue(r.Regular) || !recordedTrue(r.Buffer) || !recordedTrue(r.Reviewed) {
			return errors.New("next20 current file tuple differs")
		}
		seen[r.Target] = true
	}
	return nil
}

func TestNext20OriginalIssuanceAndPostUpdateCurrentBuffers(t *testing.T) {
	var c forward11Create
	loadPublicReceipt(t, "skills-3080-webdesign-next20-create-evidence.json", &c)
	if e := next20CreationValid(c); e != nil {
		t.Fatal(e)
	}
	var op struct {
		Files []struct {
			Target string `json:"target"`
			SHA    string `json:"sha256"`
			Body   string `json:"content_base64"`
		} `json:"files"`
	}
	raw := loadPublicReceipt(t, "skills-3080-operations/create-webdesign-next20-074042.json", &op)
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != next20ManifestSHA || len(op.Files) != 120 {
		t.Fatal("accepted next20 input changed")
	}
	hashes := map[string]string{}
	total := 0
	for i, f := range op.Files {
		r := c.Rows[i]
		body, e := base64.StdEncoding.DecodeString(f.Body)
		h := sha256.Sum256(body)
		if e != nil || r.Target != f.Target || r.SHA != f.SHA || len(body) != r.Bytes || hex.EncodeToString(h[:]) != f.SHA {
			t.Fatal("next20 returned bytes borrowed another source", i)
		}
		hashes[f.Target] = f.SHA
		total += r.Bytes
	}
	if total != 263518 {
		t.Fatal("next20 byte total differs")
	}
	var consumed struct {
		Files        int             `json:"qualified_files"`
		Instructions int             `json:"instruction_live_model_read_files"`
		Audit        int             `json:"audit_files_not_new_model_display"`
		Rows         []forward11File `json:"files"`
		Producer     struct {
			SHA      string `json:"sha256"`
			Revision string `json:"revision"`
		} `json:"runtime_producer"`
		Revalidated *bool  `json:"original_producer_revalidated"`
		Historical  *bool  `json:"historical_physical_continuity_proven"`
		General     *bool  `json:"general_access_granted"`
		Registry    *bool  `json:"original_registry_recovered"`
		Writer      string `json:"current_os_writer"`
	}
	loadPublicReceipt(t, "skills-3080-webdesign-next20-consume-evidence.json", &consumed)
	if consumed.Files != 120 || consumed.Instructions != 40 || consumed.Audit != 80 || !reflect.DeepEqual(consumed.Rows, c.Rows) || consumed.Producer.SHA != next20RuntimeSHA || consumed.Producer.Revision != "038bc2570014132321919bf6758d68979506b741" || !recordedTrue(consumed.Revalidated) || !recordedFalse(consumed.Historical) || !recordedFalse(consumed.General) || !recordedFalse(consumed.Registry) || consumed.Writer != "unproved" {
		t.Fatal("old issuing and current consuming producer were conflated")
	}
	var input struct {
		Rows []struct {
			ID     string `json:"inventory_id"`
			Name   string `json:"source_name"`
			Repo   string `json:"source_repo"`
			Commit string `json:"source_commit"`
			Path   string `json:"source_path"`
			SHA    string `json:"source_sha256"`
		} `json:"new_targets"`
	}
	raw = loadPublicReceipt(t, "skills-3080-webdesign-next20-inputs074042.json", &input)
	h = sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != "934c9ce5edfe95036c400bc9874de997ddf22cfa4024e196015d8feb3087a512" || len(input.Rows) != 20 {
		t.Fatal("independent next20 source roster differs")
	}
	want := map[string]purposeAdoptionRow{}
	for _, s := range input.Rows {
		want[s.ID] = purposeAdoptionRow{cohortLedgerRow: cohortLedgerRow{ID: s.ID, Source: s.Repo, Commit: s.Commit, Path: s.Path, SHA: s.SHA, Disposition: "installed_new_adaptation"}, Target: s.Name, Manifest: "skills-3080-installed-webdesign-next20-manifest.json", EntrySHA: hashes[s.Name+"/SKILL.md"], Evidence: "skills-3080-webdesign-next20-installation-evidence.json"}
	}
	var ledger struct {
		Rows []purposeAdoptionRow `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	if e := validatePurposeAdoptionRows(want, ledger.Rows); e != nil {
		t.Fatal(e)
	}
}

func TestNext20RejectsBorrowedOrPromotedCreationEvidence(t *testing.T) {
	for _, kind := range []string{"valid", "new_producer_as_issuer", "other_request", "other_call", "missing_false", "missing_ID", "changed_bytes", "wrong_count"} {
		t.Run(kind, func(t *testing.T) {
			var c forward11Create
			loadPublicReceipt(t, "skills-3080-webdesign-next20-create-evidence.json", &c)
			switch kind {
			case "new_producer_as_issuer":
				c.Producer.SHA = next20RuntimeSHA
			case "other_request":
				c.Request = "other"
			case "other_call":
				c.Call = "other"
			case "missing_false":
				c.General = nil
			case "missing_ID":
				c.Rows[0].Same = nil
			case "changed_bytes":
				c.Rows[0].SHA = "other"
			case "wrong_count":
				c.Count--
			}
			if (next20CreationValid(c) == nil) != (kind == "valid") {
				t.Fatal("next20 issuance mutation accepted")
			}
		})
	}
}

func TestNext20RetainedModelUseAndNumericDiscoveryScopes(t *testing.T) {
	var use struct {
		Manifest string `json:"manifest_sha256"`
		Positive int    `json:"positive_cases"`
		Negative int    `json:"negative_cases"`
		Cases    []struct {
			Index    int    `json:"manifest_instruction_index"`
			SHA      string `json:"instruction_sha256"`
			Guide    string `json:"guide_sha256"`
			Decision string `json:"decision_from_live_instruction_buffer"`
			Excluded string `json:"excluded_behavior_reason"`
			Use      *bool  `json:"actual_model_instruction_use"`
			Product  *bool  `json:"actual_product_UI_or_API_execution"`
		} `json:"cases"`
		Supplement struct {
			Current   *bool  `json:"all120_current_buffers_verified"`
			Identical *bool  `json:"instruction40_identical_to_previously_parent_read_buffers"`
			Bundle    string `json:"current_bundle_sha256"`
			AuditRead *bool  `json:"new_parent_literal_read_of_all80_audit_bodies"`
		} `json:"current_snapshot_supplement"`
	}
	u := loadPublicReceipt(t, "skills-3080-webdesign-next20-application-result.json", &use)
	var c forward11Create
	loadPublicReceipt(t, "skills-3080-webdesign-next20-create-evidence.json", &c)
	if use.Manifest != next20ManifestSHA || use.Positive != 20 || use.Negative != 20 || len(use.Cases) != 20 || !recordedTrue(use.Supplement.Current) || !recordedTrue(use.Supplement.Identical) || !recordedFalse(use.Supplement.AuditRead) || use.Supplement.Bundle != "82b1016bd6558cce56aa3599e3ea00d900fc321d76959d28c634dce97b7e0312" {
		t.Fatal("retained instruction use/current snapshot linkage differs")
	}
	for i, r := range use.Cases {
		if r.Index != i*6 || r.SHA != c.Rows[i*6].SHA || r.Guide != c.Rows[i*6+1].SHA || r.Decision == "" || r.Excluded == "" || !recordedTrue(r.Use) || !recordedFalse(r.Product) {
			t.Fatal("planning cases not tied to returned instructions", i)
		}
	}
	var math struct {
		Count  int    `json:"checks"`
		Passed *bool  `json:"passed"`
		UI     *bool  `json:"GPU_or_UI_execution"`
		Code   *bool  `json:"upstream_code_executed"`
		Source string `json:"implementation"`
		SHA    string `json:"implementation_sha256"`
		UseSHA string `json:"application_result_sha256"`
	}
	loadPublicReceipt(t, "skills-3080-webdesign-next20-math-receipt.json", &math)
	if math.Count != 45 || !recordedTrue(math.Passed) || !recordedFalse(math.UI) || !recordedFalse(math.Code) || math.Source != "evidence/skills-3080-next20-independent-math074042.go.txt" {
		t.Fatal("numeric scope differs")
	}
	source, e := os.ReadFile("../../docs/" + math.Source)
	if e != nil {
		t.Fatal(e)
	}
	a := sha256.Sum256(source)
	b := sha256.Sum256(u)
	if math.SHA != hex.EncodeToString(a[:]) || math.UseSHA != hex.EncodeToString(b[:]) {
		t.Fatal("numeric/source application pins differ")
	}
	var dis struct {
		Count   int                `json:"found_count"`
		Targets int                `json:"targets"`
		Errors  *int               `json:"parse_error_count"`
		Model   *bool              `json:"model_called"`
		Exited  *bool              `json:"process_tree_exit_confirmed"`
		Drained *bool              `json:"pending_stdout_read_drained"`
		Found   []newSkillMetadata `json:"found"`
	}
	loadPublicReceipt(t, "skills-3080-webdesign-next20-discovery-receipt.json", &dis)
	if dis.Count != 20 || dis.Targets != 20 || len(dis.Found) != 20 || dis.Errors == nil || *dis.Errors != 0 || !recordedFalse(dis.Model) || !recordedTrue(dis.Exited) || !recordedTrue(dis.Drained) {
		t.Fatal("fresh20 metadata not confirmed")
	}
	seen := map[string]bool{}
	for _, r := range dis.Found {
		if seen[r.Name] || !recordedTrue(r.Central) || !recordedTrue(r.EnabledPresent) || !recordedTrue(r.Enabled) || !recordedTrue(r.Description) {
			t.Fatal("discovery duplicated or lost field")
		}
		seen[r.Name] = true
		found := false
		for _, f := range c.Rows {
			if f.Target == r.Name+"/SKILL.md" {
				found = true
			}
		}
		if !found {
			t.Fatal("foreign discovered entry")
		}
	}
	var limits map[string]json.RawMessage
	loadPublicReceipt(t, "skills-3080-webdesign-next20-installation-evidence.json", &limits)
	for _, field := range []string{"overall_complete", "general_access_granted", "original_registry_recovered", "separate_fresh_model_turn_executed", "implicit_skill_selection_proven", "renderer_or_GPU_or_SDK_execution_proven", "real_UI_changed", "source_scripts_executed", "full_context_budget_proven", "warning_cause_linked"} {
		var v *bool
		if json.Unmarshal(limits[field], &v) != nil || !recordedFalse(v) {
			t.Fatal("unproved guarantee promoted", field)
		}
	}
}
