package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func materialEightPurposeSpec() purposeDeploymentSpec {
	return purposeDeploymentSpec{
		ReceiptFile: "skills-3080-material-camera8-installation-evidence.json", ManifestFile: "skills-3080-operations/create-material-camera8-074042.json", InstalledFile: "skills-3080-installed-material-camera8-manifest.json", DesignFile: "skills-3080-material-camera8-designs.json", CreateFile: "skills-3080-material-camera8-create-receipt.json", ReadbackFile: "skills-3080-material-camera8-readback-receipt.json", UseSource: "evidence/skills-3080-material-camera8-use-074042.go.txt", ApplicationMarker: "TestMaterialCamera8ActualExplicitUse",
		ManifestSHA: "e41f3b1b908bf097c38f8260ef667b35caadd06dfb1b47ca6ae0faec657a2a81", RequestSHA: "89085313a648caaddca412b1e88f34d4e8e84bb15ffd49968ea7053cc1490163", MainCommit: "def3c8a84dae05636ae007f34fbb8a9870b60a53", CreateSHA: "5c9cbdf5afd7f9624df7cefc8b02e2828765b054d7983c66f64b50021432880a", ReadbackSHA: "0c77e79355de66414eae4e10432ecf2add0cea37bbc0a2cedc5a67373ea3e27a", ActorJob: "20261025-074042-todo-3080", TargetCount: 8, SourceCount: 8, FileCount: 48, RetainedCount: 74, RetainedIDs: 61, NamesSHA: "bb79276dbd2d8711f515f8ce2d54dd045343870f72884e10054d53e158e1cb2a", ReviewTask: 3799, ReviewJob: "20261025-140531-todo-3799", ReviewSpec: "099d54c0aa89e878bb272caded89c03306eab3f10c95074417f959acbc30a5d1", ReviewRaw: "39cab8dad5c0ef4aa0f2d8735def51dded2b93713c70357ed96d2577af644522",
		WantedIDs: map[string]string{
			"3d-cloth-material":           "github:709d1f4fe1039448270b3f743f5432fbeb31d85416d441d4dba335b9e2df97eb",
			"3d-high-poly-models":         "github:70122bd962c6e17475151962141da342b373dc115a4383be3a7f50d6f4ad2216",
			"3d-high-resolution-textures": "github:57934e4f29ef479db656c2b1371bde1c4bc66382e9920b868b0576badda6e0e5",
			"3d-metal-material":           "github:31c71080ac741cb3feeaa3bb3dbb57ac6a76efc26ab8dc75ab5c62cf03075bb1",
			"3d-paper-material":           "github:3d98ac5b5f881acf71e673a93b79049d696259c36a80fd92a25d8f8a2ee24489",
			"3d-sky-background":           "github:eecbfba6d29822509e21b1923bd81b051f45ab1f14461d42b51dff92f30faf90",
			"3d-virtual-tour":             "github:4626a28f6ca9f66b3a74b43507b256211a1cf0fae273355b0cbb4f7ec2ea0e69",
			"3d-wood-material":            "github:2f1222d0143baefe038638b6325bac4b1aa4a9c9989daa3ffcaf655e7f050715",
		},
	}
}
func validateMaterialEightReceipt(e purposeFourReceipt) error {
	if err := validatePurposeSetHeader(e, materialEightPurposeSpec()); err != nil {
		return err
	}
	u := e.Use
	if !recordedTrue(u.Core) || u.KindCount != 8 || u.External == nil || *u.External != 0 || !recordedFalse(u.Executed) || !recordedFalse(u.Implicit) || !recordedFalse(u.Separate) || len(u.Applications) == 0 {
		return errors.New("eight bounded planning applications missing or overclaimed")
	}
	return nil
}
func validatedMaterialEightDispositions(t *testing.T) map[string]cohortLedgerRow {
	t.Helper()
	s := materialEightPurposeSpec()
	var e struct {
		Application    string `json:"application_result"`
		ApplicationSHA string `json:"application_result_sha256"`
		Test           string `json:"application_test_receipt"`
		TestSHA        string `json:"application_test_receipt_sha256"`
		Discovery      string `json:"discovery_receipt"`
		DiscoverySHA   string `json:"discovery_receipt_sha256"`
		Source         string `json:"application_source"`
		SourceSHA      string `json:"application_source_sha256"`
		Models         string `json:"application_models"`
		ModelsSHA      string `json:"application_models_sha256"`
		Use            struct {
			Applications json.RawMessage `json:"applications"`
		} `json:"explicit_application"`
	}
	loadPublicReceipt(t, s.ReceiptFile, &e)
	if e.Application != "skills-3080-material-camera8-application-result.json" || e.ApplicationSHA != "e7bb7c737b45ea7fdfba7ec1ba05174691976579e8f2c53324664f56db8efcf6" || e.Test != "skills-3080-material-camera8-application-test-receipt.json" || e.TestSHA != "ad81b74ae1acab3e06739df5f58fc361746b44a6199e8fa13e81694051411fea" || e.Discovery != "skills-3080-material-camera8-discovery-receipt.json" || e.DiscoverySHA != "03fd08c2b5808c37d3d857bc5d7f9a0b22fc42b26c998f27452c06e7dca5184c" || e.Source != s.UseSource || e.SourceSHA != "ff2000f2a790dff5b5b13a0c74071e907adfe4c64ba997644bdd32c05ed10384" || e.Models != "evidence/skills-3080-material-camera8-models-074042.go.txt" || e.ModelsSHA != "8677f65098745136c7449a078a9938676a1bb67eaeecfdc2d02290c03d3a2eff" {
		t.Fatal("independent original artifact pins differ")
	}
	for _, f := range []struct{ path, sha string }{{e.Application, e.ApplicationSHA}, {e.Test, e.TestSHA}, {e.Discovery, e.DiscoverySHA}, {e.Source, e.SourceSHA}, {e.Models, e.ModelsSHA}} {
		checkUtilityArtifactSHA(t, f.path, f.sha)
	}
	var app struct {
		Job       string            `json:"job"`
		Manifest  string            `json:"manifest_sha256"`
		Commit    string            `json:"source_commit"`
		Files     map[string]string `json:"installed_files"`
		Read      *int              `json:"explicit_skill_read"`
		Cases     *int              `json:"bounded_planning_cases"`
		Passed    *bool             `json:"positive_and_negative_examples_passed"`
		Discovery *bool             `json:"fresh_discovery_completed_before_use"`
		Writes    *int              `json:"central_writes"`
		Executed  *int              `json:"source_scripts_executed"`
		Real      *bool             `json:"real_browser_game_scene_GPU_executed"`
		Implicit  *bool             `json:"implicit_skill_selection_proved"`
		Warning   *bool             `json:"warning_cause_proved"`
		Done      *bool             `json:"whole16_complete"`
	}
	raw := loadPublicReceipt(t, e.Application, &app)
	a, ae := uniqueEvidenceJSON(raw)
	b, be := uniqueEvidenceJSON(e.Use.Applications)
	if ae != nil || be != nil || !reflect.DeepEqual(a, b) {
		t.Fatal("summary changed original application evidence")
	}
	if app.Job != s.ActorJob || app.Manifest != s.ManifestSHA || app.Commit != s.MainCommit || app.Read == nil || *app.Read != 8 || app.Cases == nil || *app.Cases != 8 || !recordedTrue(app.Passed) || !recordedTrue(app.Discovery) || app.Writes == nil || *app.Writes != 0 || app.Executed == nil || *app.Executed != 0 || !recordedFalse(app.Real) || !recordedFalse(app.Implicit) || !recordedFalse(app.Warning) || !recordedFalse(app.Done) {
		t.Fatal("application actor or unperformed runtime claim differs")
	}
	var op struct {
		Files []struct {
			Target string `json:"target"`
			SHA    string `json:"sha256"`
		}
	}
	loadPublicReceipt(t, s.ManifestFile, &op)
	if len(app.Files) != 48 {
		t.Fatal("used file mapping incomplete")
	}
	for _, f := range op.Files {
		if app.Files[f.Target] != f.SHA {
			t.Fatal("used instruction changed from reviewed output")
		}
	}
	var run utilityRecordedRun
	loadPublicReceipt(t, e.Test, &run)
	if run.Exit == nil || *run.Exit != 0 || !recordedFalse(run.Timed) || run.Out == nil || run.Err == nil || *run.Err != "" || !strings.Contains(*run.Out, "TestMaterialCamera8ActualExplicitUse") || !strings.Contains(*run.Out, "PASS") {
		t.Fatal("actual bounded application test did not complete")
	}
	var dis struct {
		Targets      *int               `json:"targets"`
		Found        *int               `json:"found_count"`
		Rows         *int               `json:"observed_row_count"`
		Enabled      *int               `json:"enabled_count"`
		All          *bool              `json:"all_requested_enabled"`
		Errors       *int               `json:"parse_error_count"`
		Duplicates   *int               `json:"duplicate_observed_names"`
		Other        *int               `json:"same_names_other_paths"`
		Descriptions *int               `json:"new_description_match_count"`
		Known        string             `json:"known_target_names_sha256"`
		Observed     string             `json:"observed_unique_names_sha256"`
		Model        *bool              `json:"model_called"`
		Exit         *bool              `json:"process_tree_exit_confirmed"`
		Drain        *bool              `json:"pending_stdout_read_drained"`
		Failure      json.RawMessage    `json:"failure"`
		New          []newSkillMetadata `json:"new_target_rows"`
	}
	loadPublicReceipt(t, e.Discovery, &dis)
	for _, n := range []*int{dis.Targets, dis.Found, dis.Rows, dis.Enabled} {
		if n == nil || *n != 74 {
			t.Fatal("actual detailed discovery count differs")
		}
	}
	for _, n := range []*int{dis.Errors, dis.Duplicates, dis.Other} {
		if n == nil || *n != 0 {
			t.Fatal("actual detailed discovery error or identity ambiguity")
		}
	}
	if !recordedTrue(dis.All) || dis.Descriptions == nil || *dis.Descriptions != 8 || dis.Known != s.NamesSHA || dis.Observed != s.NamesSHA || !recordedFalse(dis.Model) || !recordedTrue(dis.Exit) || !recordedTrue(dis.Drain) || !bytes.Equal(bytes.TrimSpace(dis.Failure), []byte("null")) || len(dis.New) != 8 {
		t.Fatal("fresh discovery omitted outcomes or overstated context")
	}
	seen := map[string]bool{}
	for _, r := range dis.New {
		if s.WantedIDs[r.Name] == "" || seen[r.Name] || !recordedTrue(r.Central) || !recordedTrue(r.EnabledPresent) || !recordedTrue(r.Enabled) || !recordedTrue(r.Description) {
			t.Fatal("new detailed discovery row differs")
		}
		seen[r.Name] = true
	}
	return validatedPurposeDeployment(t, s, validateMaterialEightReceipt)
}
func TestPurposeMaterialEightActualDeploymentAndBoundedUse(t *testing.T) {
	validatedMaterialEightDispositions(t)
}
func TestPurposeMaterialEightRejectsMissingAndInflatedEvidence(t *testing.T) {
	for _, kind := range []string{"valid", "missing_created", "missing_general", "missing_registry", "missing_exit", "missing_drain", "render_claim", "unknown_actor", "wrong_notice_receipt", "missing_use", "borrowed_manifest"} {
		t.Run(kind, func(t *testing.T) {
			var e purposeFourReceipt
			loadPublicReceipt(t, materialEightPurposeSpec().ReceiptFile, &e)
			yes := true
			switch kind {
			case "missing_created":
				e.Create.Count = 0
			case "missing_general":
				e.Create.General = nil
			case "missing_registry":
				e.Create.Registry = nil
			case "missing_exit":
				e.Discovery.Exited = nil
			case "missing_drain":
				e.Discovery.Drained = nil
			case "render_claim":
				e.WarningCause = &yes
			case "unknown_actor":
				e.Acceptance.Job = "other"
			case "wrong_notice_receipt":
				e.ReadbackSHA = "other"
			case "missing_use":
				e.Use.Core = nil
			case "borrowed_manifest":
				e.SHA = utilityEightPurposeSpec().ManifestSHA
			}
			if err := validateMaterialEightReceipt(e); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}
