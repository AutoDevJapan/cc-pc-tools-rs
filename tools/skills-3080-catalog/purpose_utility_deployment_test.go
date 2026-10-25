package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func utilityEightPurposeSpec() purposeDeploymentSpec {
	return purposeDeploymentSpec{
		ReceiptFile: "skills-3080-utility8-installation-evidence.json", ManifestFile: "skills-3080-operations/create-utility8-074042.json", InstalledFile: "skills-3080-installed-utility8-manifest.json", DesignFile: "skills-3080-utility8-designs.json", CreateFile: "skills-3080-utility8-create-receipt.json", ReadbackFile: "skills-3080-utility8-readback-receipt.json", UseSource: "evidence/skills-3080-utility8-use-v2-074042.go.txt", ApplicationMarker: "TestUtility8ExplicitApplications",
		ManifestSHA: "7e95c0cac90d6a5e0b83c3236b5320da95fe6b600798abd41c75fb0a0e8f734d", RequestSHA: "d4294453c880d53072054253550b192fc9c141d935ee225f71ad854e02508e5b", MainCommit: "a815e8e5d67a13cdc54cd95e6df5f0c2aa434e05", CreateSHA: "405af6d191692d68855cfb43cfa4e261cf0bb916518d186cb50372509cd55e5b", ReadbackSHA: "3fe126e2ce71c802a0f7ea4fc634fbfd9d6c31b54f59d15f96239361304b839f", ActorJob: "20261025-074042-todo-3080", TargetCount: 8, SourceCount: 8, FileCount: 48, RetainedCount: 66, RetainedIDs: 53, NamesSHA: "a2e50acb5fb628e8b2f448d982510e99590a675f00f73f8c32edcec0aedae7ca", ReviewTask: 3784, ReviewJob: "20261025-102633-todo-3784", ReviewSpec: "14d314dc4c1868b6d293309ea0e2c968fd9e4f82dd2971291a86823c01a9ec70", ReviewRaw: "92cc1fc90525e6df44a23c7f932ee49c862347d51d67712bb91a5a1047c78d9b",
		WantedIDs: map[string]string{
			"build-game-changelog":        "github:d8dff1294478f735c0e39073429164e1bd87d6c0d78feb7315c839a0b187fa98",
			"test-playable-web-games":     "github:a5a3b85d3994a4db7d3a009b1aafbe6f5ba608b3dff72e59bd91378f23f8b9d1",
			"audit-reference-originality": "github:0b5385b0e0a144c0e88b6a7f9317f92d50d78285ab48cf5164332d0af46ae35e",
			"browser-video-recording":     "github:ad7dd0ac60bec1e813b9f90cf244ce880593d4fb4a0702de21348a1477b49300",
			"stitched-full-page-capture":  "github:eaf8e98a4a20bde971033d3afc6ff8622fdfff9e026871e86cc9dcfa9a860af5",
			"video-to-superprompt":        "github:47eaf17ef451cbe3e1435dce403be55f89a48c416b943671e48541380b005028",
			"html-to-interaction-prompts": "github:4e9631ebafe558782152a55e70d44f8e87d4080b542c738bccd5ad28fa9e492c",
			"optimize-web-animations":     "github:79d285cb8c6bd8ba7ba2afb6dc42bc341f77fb3b179fc473b19dab9acb91a0c9",
		},
	}
}

func validateUtilityEightReceipt(e purposeFourReceipt) error {
	if err := validatePurposeSetHeader(e, utilityEightPurposeSpec()); err != nil {
		return err
	}
	u := e.Use
	if !recordedTrue(u.Core) || u.KindCount != 8 || u.External == nil || *u.External != 0 || !recordedFalse(u.Executed) || !recordedFalse(u.Implicit) || !recordedFalse(u.Separate) || len(u.Applications) == 0 {
		return errors.New("eight scoped applications missing or overclaim external use")
	}
	return nil
}

// Validate application and discovery originals before making an accepted map.
// These are recorded Windows observations; CI does not read central Skills,
// rerun browser/game use, or turn a synthetic fixture into runtime acceptance.
func validatedUtilityEightDispositions(t *testing.T) map[string]cohortLedgerRow {
	t.Helper()
	s := utilityEightPurposeSpec()
	var e struct {
		Application    string `json:"application_result"`
		ApplicationSHA string `json:"application_result_sha256"`
		Test           string `json:"application_test_receipt"`
		TestSHA        string `json:"application_test_receipt_sha256"`
		DiscoveryFile  string `json:"discovery_receipt"`
		DiscoverySHA   string `json:"discovery_receipt_sha256"`
		Fixture        string `json:"fixture_preparation_snapshot"`
		FixtureSHA     string `json:"fixture_preparation_snapshot_sha256"`
		SourceSHA      string `json:"application_source_sha256"`
		Probe          string `json:"probe_receipt"`
		ProbeSHA       string `json:"probe_receipt_sha256"`
		Prior          struct {
			Application    string `json:"application_result"`
			ApplicationSHA string `json:"application_result_sha256"`
			Test           string `json:"application_test_receipt"`
			TestSHA        string `json:"application_test_receipt_sha256"`
			Source         string `json:"application_source"`
			SourceSHA      string `json:"application_source_sha256"`
			Accepted       *bool  `json:"accepted_as_current_proof"`
		} `json:"prior_weak_application"`
		Resolution struct {
			Red       string `json:"original_red"`
			RedSHA    string `json:"original_red_sha256"`
			Green     string `json:"green"`
			GreenSHA  string `json:"green_sha256"`
			Source    string `json:"regression_source"`
			SourceSHA string `json:"regression_source_sha256"`
		} `json:"review3789_resolution"`
		Use struct {
			Applications json.RawMessage `json:"applications"`
		} `json:"explicit_application"`
		Discovery struct {
			Found []newSkillMetadata `json:"found"`
		} `json:"discovery"`
	}
	loadPublicReceipt(t, s.ReceiptFile, &e)
	if e.Application != "skills-3080-utility8-application-v2-result.json" || e.ApplicationSHA != "0de6775d229644205cc051e0861a9c7a716e23ee8be284c4bfd6c74dfba34615" || e.Test != "skills-3080-utility8-application-v2-test-receipt.json" || e.TestSHA != "8cc97e0a67630a17fafe74f824e9a1d43f1e19b7888482b7bc0037a016a85a92" || e.SourceSHA != "fde8faed362addae8fbc4d5a36701e7286ded4f2a20e5cc31c06dfd38ffdef6d" || e.DiscoveryFile != "skills-3080-utility8-discovery-receipt.json" || e.DiscoverySHA != "fb01164c58fab7c1bcd3259f19585971464fcacdc90807965eb1394a4e2195b0" {
		t.Fatal("independent original application/discovery anchors differ")
	}
	// Keep the original successful but weak run as history; its source is not
	// retroactively represented as the stricter source used by the new run.
	if !recordedFalse(e.Prior.Accepted) || e.Prior.Application != "skills-3080-utility8-application-result.json" || e.Prior.ApplicationSHA != "6e2c16098bd8389c1bb77670d0abfc7dc50ebcece8075ae7c69b5caf1aa4be84" || e.Prior.Test != "skills-3080-utility8-application-test-receipt.json" || e.Prior.TestSHA != "b4814c7b420de721d3aedb38bb8dfe0ca327c659fff2bf83968571dac63fb8ee" || e.Prior.Source != "evidence/skills-3080-utility8-use-074042.go.txt" || e.Prior.SourceSHA != "204ab636d690b63837960c6306d6bf08ea42b77291f181a257b0419116094344" {
		t.Fatal("original weak application history missing or promoted")
	}
	for _, f := range []struct{ path, sha string }{{e.Prior.Application, e.Prior.ApplicationSHA}, {e.Prior.Test, e.Prior.TestSHA}, {e.Prior.Source, e.Prior.SourceSHA}, {s.UseSource, e.SourceSHA}} {
		checkUtilityArtifactSHA(t, f.path, f.sha)
	}
	var app map[string]any
	raw := loadPublicReceipt(t, e.Application, &app)
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != e.ApplicationSHA {
		t.Fatal("actual application bytes changed")
	}
	derived, de := uniqueEvidenceJSON(e.Use.Applications)
	original, oe := uniqueEvidenceJSON(raw)
	if de != nil || oe != nil || !reflect.DeepEqual(derived, original) {
		t.Fatal("published summary differs from original scoped use")
	}
	if app["actual_job"] != s.ActorJob || app["manifest_sha256"] != s.ManifestSHA || app["all_bounded_cases_passed"] != true || app["explicit_current_worker_application"] != true {
		t.Fatal("actual use actor/manifest/completion differs")
	}
	if app["application_source_sha256"] != e.SourceSHA || app["creation_wrapper_sha256"] != s.CreateSHA || app["probe_receipt_sha256"] != e.ProbeSHA || app["revalidates_original_result_after_review3789"] != true {
		t.Fatal("strict revalidation not bound to executed source and original receipts")
	}
	for _, key := range []string{"new_separate_model_turn_or_implicit_selection", "real_browser_game_UI_or_GPU_execution", "source_scripts_executed", "overall16_complete"} {
		if app[key] != false {
			t.Fatalf("unperformed use claim: %s", key)
		}
	}
	cases, ok := app["cases"].(map[string]any)
	if !ok || len(cases) != 8 {
		t.Fatal("eight bounded cases absent")
	}
	for name := range s.WantedIDs {
		if cases[name] == nil {
			t.Fatal("missing case", name)
		}
	}
	var binding struct {
		Hashes map[string]string `json:"installed_output_hashes"`
	}
	if json.Unmarshal(raw, &binding) != nil || len(binding.Hashes) != 48 {
		t.Fatal("used output bindings missing")
	}
	var operation struct {
		Files []struct {
			Target string
			SHA    string `json:"sha256"`
		}
	}
	loadPublicReceipt(t, s.ManifestFile, &operation)
	for _, f := range operation.Files {
		if binding.Hashes[f.Target] != f.SHA {
			t.Fatal("used instruction differs from reviewed output", f.Target)
		}
	}
	var run utilityRecordedRun
	test := loadPublicReceipt(t, e.Test, &run)
	h = sha256.Sum256(test)
	if hex.EncodeToString(h[:]) != e.TestSHA || run.Exit == nil || *run.Exit != 0 || !recordedFalse(run.Timed) || run.Out == nil || run.Err == nil || *run.Out == "" || *run.Err != "" {
		t.Fatal("actual example test did not end successfully")
	}
	if e.Probe != "skills-3080-utility8-probe-receipt.json" || e.ProbeSHA != "6c1badd61cf0ac18cf422fec3300d7382bdc6905a5605576d529cf0351b0b9b2" {
		t.Fatal("original probe receipt identity differs")
	}
	checkUtilityArtifactSHA(t, e.Probe, e.ProbeSHA)
	run = utilityRecordedRun{}
	loadPublicReceipt(t, e.Probe, &run)
	if run.Exit == nil || *run.Exit != 0 || !recordedFalse(run.Timed) || run.Out == nil || *run.Out == "" || run.Err == nil || *run.Err != "" {
		t.Fatal("probe observation missing, failed or timed out")
	}
	r := e.Resolution
	if r.Red != "skills-3080-utility8-receipt-regression-red.json" || r.RedSHA != "2b46a146472556cfa75deee79d8ec55d4b8442ab9785031b049108dff74b787d" || r.Green != "skills-3080-utility8-receipt-regression-green.json" || r.GreenSHA != "54de5f6bbc2cc65a2615a6c337fdeedef14094dfa8730793063ef8321317b36f" || r.Source != "evidence/skills-3080-utility8-receipt-contract-v2-074042.go.txt" || r.SourceSHA != "342a71c96161271b5dfc2798a6a44a7de5d4e80e476598a0e06a41335134a9ab" {
		t.Fatal("Must regression artifacts differ")
	}
	for _, f := range []struct{ path, sha string }{{r.Red, r.RedSHA}, {r.Green, r.GreenSHA}, {r.Source, r.SourceSHA}} {
		checkUtilityArtifactSHA(t, f.path, f.sha)
	}
	run = utilityRecordedRun{}
	loadPublicReceipt(t, r.Red, &run)
	if run.Exit == nil || *run.Exit == 0 || !recordedFalse(run.Timed) || run.Out == nil || !strings.Contains(*run.Out, "missing_exit") || !strings.Contains(*run.Out, "missing_general") {
		t.Fatal("missing-field regression was not reproduced")
	}
	run = utilityRecordedRun{}
	loadPublicReceipt(t, r.Green, &run)
	if run.Exit == nil || *run.Exit != 0 || !recordedFalse(run.Timed) || run.Out == nil || !strings.Contains(*run.Out, "TestUtility8RequiredReceiptFields") || run.Err == nil || *run.Err != "" {
		t.Fatal("required-field regression did not pass")
	}
	var discovery struct {
		Targets, Found, Enabled, Rows, Duplicates, Descriptions int
		All                                                     *bool
		Names, Observed                                         string
		New                                                     []newSkillMetadata
		Model, Exited, Drained                                  *bool
	}
	var observed struct {
		Targets      int                `json:"targets"`
		Found        int                `json:"found_count"`
		Enabled      int                `json:"enabled_count"`
		Rows         int                `json:"observed_row_count"`
		Duplicates   int                `json:"duplicate_observed_names"`
		Descriptions int                `json:"new_description_match_count"`
		All          *bool              `json:"all_requested_enabled"`
		Names        string             `json:"known_target_names_sha256"`
		Observed     string             `json:"observed_unique_names_sha256"`
		New          []newSkillMetadata `json:"new_target_rows"`
		Model        *bool              `json:"model_called"`
		Exited       *bool              `json:"process_tree_exit_confirmed"`
		Drained      *bool              `json:"pending_stdout_read_drained"`
	}
	db := loadPublicReceipt(t, e.DiscoveryFile, &observed)
	h = sha256.Sum256(db)
	discovery.Targets, discovery.Found, discovery.Enabled, discovery.Rows, discovery.Duplicates, discovery.Descriptions = observed.Targets, observed.Found, observed.Enabled, observed.Rows, observed.Duplicates, observed.Descriptions
	if hex.EncodeToString(h[:]) != e.DiscoverySHA || discovery.Targets != 66 || discovery.Found != 66 || discovery.Enabled != 66 || discovery.Rows != 66 || discovery.Duplicates != 0 || discovery.Descriptions != 8 || !recordedTrue(observed.All) || observed.Names != s.NamesSHA || observed.Observed != s.NamesSHA || !recordedFalse(observed.Model) || !recordedTrue(observed.Exited) || !recordedTrue(observed.Drained) || !reflect.DeepEqual(observed.New, e.Discovery.Found) {
		t.Fatal("detailed discovery cannot prove exact enabled identities")
	}
	var preparation struct {
		SHA    string `json:"sha256"`
		Use    *bool  `json:"skill_use_executed"`
		Kind   string `json:"kind"`
		Frames []struct {
			SHA string `json:"sha256"`
		}
	}
	if e.Fixture != "skills-3080-utility8-fixture-preparation.json" {
		t.Fatal("fixture preparation path differs")
	}
	fb := loadPublicReceipt(t, e.Fixture, &preparation)
	h = sha256.Sum256(fb)
	if hex.EncodeToString(h[:]) != e.FixtureSHA || preparation.SHA != "7d5dfb92886e4e667aae3f47ae4f2707aa1da87620e8c808eed73e677d452536" || !recordedFalse(preparation.Use) || len(preparation.Frames) != 2 {
		t.Fatal("preparation was changed into use or real recording")
	}
	return validatedPurposeDeployment(t, s, validateUtilityEightReceipt)
}

type utilityRecordedRun struct {
	Exit  *int    `json:"exit"`
	Timed *bool   `json:"timed_out"`
	Out   *string `json:"stdout"`
	Err   *string `json:"stderr"`
}

func checkUtilityArtifactSHA(t *testing.T, path, want string) {
	t.Helper()
	if strings.Contains(path, "..") || strings.HasPrefix(path, "/") {
		t.Fatal("unexpected evidence path")
	}
	raw, err := os.ReadFile("../../docs/" + path)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != want {
		t.Fatal("fixed original artifact differs", path)
	}
}

func TestPurposeUtilityEightActualScopedDeployment(t *testing.T) {
	validatedUtilityEightDispositions(t)
}
