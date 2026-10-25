package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

const forward11ManifestSHA = "0510c7e5d90934f87c8ec97cdf408d923b2e4cab622c144852e61d62821a509b"
const forward11RequestSHA = "c443076215e403c5eeeea9950ba623451f0311cb3d983f2280f88869d6717a04"
const forward11Call = "call_8ba0a2c359ab4af48e577cc5929e5ac6"

type forward11File struct {
	Index    int    `json:"index"`
	Target   string `json:"target"`
	SHA      string `json:"sha256"`
	Bytes    int    `json:"bytes"`
	ID       string `json:"created_and_current_object_identity_sha256"`
	Same     *bool  `json:"creation_ID_matches_current"`
	Regular  *bool  `json:"regular_non_reparse_single_link_verified_by_qualified_consumer"`
	Buffer   *bool  `json:"same_handle_buffer_returned"`
	Reviewed *bool  `json:"reviewed_bytes_equal"`
}
type forward11Create struct {
	Qualified *bool  `json:"qualified"`
	Domain    string `json:"domain"`
	Operation string `json:"operation"`
	Todo      int    `json:"todo_id"`
	Count     int    `json:"created_files"`
	Files     int    `json:"file_count"`
	Request   string `json:"request_sha256"`
	Manifest  string `json:"manifest_sha256"`
	Commit    string `json:"source_commit"`
	Call      string `json:"native_call_id"`
	General   *bool  `json:"general_access_granted"`
	Registry  *bool  `json:"original_registry_recovered"`
	Producer  struct {
		SHA      string `json:"sha256"`
		Revision string `json:"revision"`
	} `json:"producer"`
	OutputSHA string          `json:"original_CLI_stdout_sha256"`
	BlocksSHA string          `json:"original_tool_blocks_sha256"`
	Rows      []forward11File `json:"files"`
}

func validateForward11Create(c forward11Create) error {
	if !recordedTrue(c.Qualified) || c.Domain != "skills-3080-purpose-v1" || c.Operation != "CreateNew" || c.Todo != 3080 || c.Count != 67 || c.Files != 67 || len(c.Rows) != 67 || c.Request != forward11RequestSHA || c.Manifest != forward11ManifestSHA || c.Commit != "8b2b8395191f1c81cd612867f615ecb2f0a6dbea" || c.Call != forward11Call || !recordedFalse(c.General) || !recordedFalse(c.Registry) {
		return errors.New("forward create identity or boundary differs")
	}
	if c.Producer.SHA != "29118cd151cc9ccec8bb6a371382e2a3cc2507d5b36e3837f6cc87793b5b21fb" || c.Producer.Revision != "e1a4c21dc6241c140d4d7364d34f42b16d7005be" || c.OutputSHA != "249efac160bfd4f5825f00d52f7455a4e6174308199a0ae38c2adcea31806e6a" || c.BlocksSHA != "39f8cf5940d7d6f5a9e93f5b48cc97ed24099a388119d18df204acb879705f14" {
		return errors.New("actual producer/native pair differs")
	}
	seen := map[string]bool{}
	for i, r := range c.Rows {
		if r.Index != i || seen[r.Target] || r.Target == "" || !validHex(r.SHA, 64) || !validHex(r.ID, 64) || r.Bytes < 1 || !recordedTrue(r.Same) || !recordedTrue(r.Regular) || !recordedTrue(r.Buffer) || !recordedTrue(r.Reviewed) {
			return errors.New("actual returned file tuple differs")
		}
		seen[r.Target] = true
	}
	return nil
}
func TestForward11ActualCreationCurrentBuffersAndSourceLedger(t *testing.T) {
	var c forward11Create
	loadPublicReceipt(t, "skills-3080-webdesign11-create-evidence.json", &c)
	if e := validateForward11Create(c); e != nil {
		t.Fatal(e)
	}
	var consumed struct {
		Files        int             `json:"qualified_files"`
		Instructions int             `json:"instruction_live_model_read_files"`
		Audit        int             `json:"audit_files_not_new_model_display"`
		Rows         []forward11File `json:"files"`
		Same         *bool           `json:"same_current_object_IDs_verified"`
		Buffers      *bool           `json:"same_handle_buffers_returned"`
		Continuity   *bool           `json:"historical_physical_continuity_proven"`
		Writer       string          `json:"current_os_writer"`
		General      *bool           `json:"general_access_granted"`
		Registry     *bool           `json:"original_registry_recovered"`
		Call         string          `json:"creation_native_call_id"`
		Structure    int             `json:"structure_passed"`
	}
	loadPublicReceipt(t, "skills-3080-webdesign11-consume-evidence.json", &consumed)
	if consumed.Files != 67 || consumed.Instructions != 22 || consumed.Audit != 45 || !reflect.DeepEqual(c.Rows, consumed.Rows) || !recordedTrue(consumed.Same) || !recordedTrue(consumed.Buffers) || !recordedFalse(consumed.Continuity) || consumed.Writer != "unproved" || !recordedFalse(consumed.General) || !recordedFalse(consumed.Registry) || consumed.Call != forward11Call || consumed.Structure != 11 {
		t.Fatal("current buffer/historical guarantee projection differs")
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
	raw := loadPublicReceipt(t, "skills-3080-webdesign11-inputs074042.json", &input)
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != "04d6fef44ef76cbd9ccb9f78cf4fc1e6947f76281d49cffc4341238c727698f4" || len(input.New) != 11 {
		t.Fatal("independent reviewed source input changed")
	}
	var op struct {
		Files []struct {
			Target string `json:"target"`
			SHA    string `json:"sha256"`
			Base64 string `json:"content_base64"`
		} `json:"files"`
	}
	raw = loadPublicReceipt(t, "skills-3080-operations/create-webdesign11-074042.json", &op)
	h = sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != forward11ManifestSHA || len(op.Files) != 67 {
		t.Fatal("accepted operation differs")
	}
	hashes := map[string]string{}
	for i, f := range op.Files {
		if f.Target != c.Rows[i].Target || f.SHA != c.Rows[i].SHA {
			t.Fatal("consumed bytes borrowed another manifest")
		}
		hashes[f.Target] = f.SHA
	}
	want := map[string]purposeAdoptionRow{}
	for _, s := range input.New {
		want[s.ID] = purposeAdoptionRow{cohortLedgerRow: cohortLedgerRow{ID: s.ID, Source: s.Repo, Commit: s.Commit, Path: s.Path, SHA: s.SHA, Disposition: "installed_new_adaptation"}, Target: s.Name, Manifest: "skills-3080-installed-webdesign11-manifest.json", EntrySHA: hashes[s.Name+"/SKILL.md"], Evidence: "skills-3080-webdesign11-installation-evidence.json"}
	}
	var ledger struct {
		Rows []purposeAdoptionRow `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	if e := validatePurposeAdoptionRows(want, ledger.Rows); e != nil {
		t.Fatal(e)
	}
}
func TestForward11RejectsMixedProducerCountsAndMissingCurrentIdentity(t *testing.T) {
	for _, kind := range []string{"valid", "other_producer", "other_request", "other_call", "created_count", "missing_false", "changed_file", "missing_ID"} {
		t.Run(kind, func(t *testing.T) {
			var c forward11Create
			loadPublicReceipt(t, "skills-3080-webdesign11-create-evidence.json", &c)
			switch kind {
			case "other_producer":
				c.Producer.SHA = "other"
			case "other_request":
				c.Request = "other"
			case "other_call":
				c.Call = "call_other"
			case "created_count":
				c.Count--
			case "missing_false":
				c.General = nil
			case "changed_file":
				c.Rows[0].SHA = "other"
			case "missing_ID":
				c.Rows[0].Same = nil
			}
			if (validateForward11Create(c) == nil) != (kind == "valid") {
				t.Fatal("forward tuple mutation accepted")
			}
		})
	}
}
func TestForward11ExplicitApplicationsAndDiscoveryDoNotClaimRenderingOrFullBudget(t *testing.T) {
	var u struct {
		Count    int `json:"case_count"`
		Positive int `json:"positive_count"`
		Negative int `json:"negative_count"`
		Cases    []struct {
			Name     string `json:"name"`
			Applied  *bool  `json:"actual_model_application_completed"`
			Response string `json:"model_response"`
			Negative string `json:"negative_response"`
			Sources  []struct {
				Index int    `json:"index"`
				SHA   string `json:"sha256"`
			} `json:"consumed_instruction_sources"`
		} `json:"cases"`
		UI       *int  `json:"real_product_UI_changes"`
		Queries  *int  `json:"real_questions_or_external_messages_for_fixtures"`
		Scripts  *int  `json:"upstream_source_script_execution"`
		Renderer *bool `json:"renderer_GPU_or_third_party_SDK_success_claimed"`
		Done     *bool `json:"whole16_complete"`
	}
	raw := loadPublicReceipt(t, "skills-3080-webdesign11-application-result.json", &u)
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != "f6c72362471ca9e7f4ac95ee773eac07e8830f06826227aa1e5833d530f12471" {
		t.Fatal("actual model use original bytes changed")
	}
	if u.Count != 11 || u.Positive != 11 || u.Negative != 11 || len(u.Cases) != 11 || u.UI == nil || *u.UI != 0 || u.Queries == nil || *u.Queries != 0 || u.Scripts == nil || *u.Scripts != 0 || !recordedFalse(u.Renderer) || !recordedFalse(u.Done) {
		t.Fatal("bounded model use was promoted to real UI/SDK execution")
	}
	names := map[string]bool{}
	for _, c := range u.Cases {
		if names[c.Name] || !recordedTrue(c.Applied) || len(c.Sources) != 2 || c.Response == "" || c.Negative == "" {
			t.Fatal("explicit live use source missing")
		}
		names[c.Name] = true
	}
	var math struct {
		Checks   int    `json:"numeric_invariants_checked"`
		Passed   *bool  `json:"numeric_invariants_passed"`
		Renderer *bool  `json:"renderer_or_SDK_or_GPU_tested"`
		Source   string `json:"live_use_record_sha256"`
	}
	loadPublicReceipt(t, "skills-3080-webdesign11-math-receipt.json", &math)
	if math.Checks != 79 || !recordedTrue(math.Passed) || !recordedFalse(math.Renderer) || math.Source != hex.EncodeToString(h[:]) {
		t.Fatal("numeric checks exceed their actual scope")
	}
	var d struct {
		Targets int   `json:"targets"`
		Count   int   `json:"found_count"`
		Errors  *int  `json:"parse_error_count"`
		Model   *bool `json:"model_called"`
		Exited  *bool `json:"process_tree_exit_confirmed"`
		Drained *bool `json:"pending_stdout_read_drained"`
		Found   []struct {
			Name        string `json:"name"`
			Enabled     *bool  `json:"enabled"`
			Present     *bool  `json:"enabled_present"`
			Description *bool  `json:"description_matches"`
		} `json:"found"`
	}
	loadPublicReceipt(t, "skills-3080-webdesign11-discovery-receipt.json", &d)
	if d.Targets != 11 || d.Count != 11 || d.Errors == nil || *d.Errors != 0 || !recordedFalse(d.Model) || !recordedTrue(d.Exited) || !recordedTrue(d.Drained) || len(d.Found) != 11 {
		t.Fatal("fresh explicit discovery incomplete")
	}
	for _, r := range d.Found {
		if !names[r.Name] || !recordedTrue(r.Enabled) || !recordedTrue(r.Present) || !recordedTrue(r.Description) {
			t.Fatal("fresh metadata differs from consumed entrypoint")
		}
		delete(names, r.Name)
	}
	if len(names) != 0 {
		t.Fatal("discovery name missing")
	}
}
