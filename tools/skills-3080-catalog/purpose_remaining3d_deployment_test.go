package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func remainingSixPurposeSpec() purposeDeploymentSpec {
	return purposeDeploymentSpec{
		ReceiptFile: "skills-3080-remaining3d6-installation-evidence.json", ManifestFile: "skills-3080-operations/create-remaining3d6-074042.json", InstalledFile: "skills-3080-installed-remaining3d6-manifest.json", DesignFile: "skills-3080-remaining3d6-designs.json", CreateFile: "skills-3080-remaining3d6-create-receipt.json", ReadbackFile: "skills-3080-remaining3d6-readback-receipt.json", UseSource: "evidence/skills-3080-remaining3d6-use-074042.go.txt", ApplicationMarker: "TestRemaining3D6ActualExplicitUse",
		ManifestSHA: "12d0b617a97c9a11eadc815628c55982442e828f4817fdf8a09e604b4ce9dc34", RequestSHA: "6f569c8f3ce8cc7b5015a7bcaebf5e7148c9866271fac3a995905ec79186e6c8", MainCommit: "d5c04f9dd0e5d90c286b013139ab37f2a0d76c50", CreateSHA: "7715f1af242ed065fc47d3df31d5ca23b3f3010f05cd538740d47f83521970fd", ReadbackSHA: "b1d5d1ab4d0895db5c320743c98a4ffaac92cb5062485669afaea93b8e23fa81", ActorJob: "20261025-074042-todo-3080", TargetCount: 6, SourceCount: 6, FileCount: 36, RetainedCount: 80, RetainedIDs: 67, NamesSHA: "2d9ef027e7cd7116531371e6818bba86334296db558ca9726f16e2fa3799844a", ReviewTask: 3810, ReviewJob: "20261025-154203-todo-3810", ReviewSpec: "0a39cb14e6c61b9a08df8e654134cdfde9bb1d685c3de3354871d363dfbed808", ReviewRaw: "5b8d65e4f05c2dc801beae0349780bd03f1ef25301d24106a23cb9cd71b90b08",
		WantedIDs: map[string]string{
			"3d-falling-leaves":          "github:fab88427bee2bdceb455b105d4cc67286c5137688358e999728494f258e7bd3a",
			"3d-orbit-inspect-demo":      "github:7954413ce35f01682a5e1d2323dbd97230efe385a21df4f86c7040bfa845485c",
			"3d-sky-rays":                "github:32b954e6fb09280ec5b99faf879afdbbfdd5c7ca20ccedc3d59ba01b5ad9c856",
			"3d-ultra-realistic-water":   "github:896c34ba4a99ae3711407ba96fda39996823607f6a3b2e6ddd456f62af1b19a6",
			"3d-underwater-god-rays":     "github:902a0a522b75574bc5a87780cf16f5ea986493124e4bdb578182b36bf6f21df5",
			"3d-wood-lighting-scorecard": "github:796ba2120ce77e2b5a8f6eeba65b7e3367cc775594bdb59dbf4cf527094c069a",
		},
	}
}
func validateRemainingSixReceipt(e purposeFourReceipt) error {
	if err := validatePurposeSetHeader(e, remainingSixPurposeSpec()); err != nil {
		return err
	}
	u := e.Use
	if !recordedTrue(u.Core) || u.KindCount != 6 || u.External == nil || *u.External != 0 || !recordedFalse(u.Executed) || !recordedFalse(u.Implicit) || !recordedFalse(u.Separate) || len(u.Applications) == 0 {
		return errors.New("six bounded planning applications missing or overclaimed")
	}
	return nil
}
func validatedRemainingSixDispositions(t *testing.T) map[string]cohortLedgerRow {
	t.Helper()
	s := remainingSixPurposeSpec()
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
	if e.Application != "skills-3080-remaining3d6-application-result.json" || e.ApplicationSHA != "f7d31b5351a26bbed4d5811cbc2d17dd593feff1bbc11b847e54db57022b30bd" || e.Test != "skills-3080-remaining3d6-application-test-receipt.json" || e.TestSHA != "4defd7490d928a887f9c7ef2b78fda717a543880c0e3564f4fda859c5a82fa17" || e.Discovery != "skills-3080-remaining3d6-discovery-receipt.json" || e.DiscoverySHA != "1ca2fc38f15a81be692c2d20f5e1363a26ba2fcfb097db30fc44b81a55dbe248" || e.Source != s.UseSource || e.SourceSHA != "5d12236cfc10c4edd9827ee05869d9d17b149f8cfd2673c9d1256872f80788c7" || e.Models != "evidence/skills-3080-remaining3d6-models-074042.go.txt" || e.ModelsSHA != "fe9eed5d9b98d903019ec1137bda2ff1ce725e0799943f7ad0e7a9ef630d48e6" {
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
	if app.Job != s.ActorJob || app.Manifest != s.ManifestSHA || app.Commit != s.MainCommit || app.Read == nil || *app.Read != 6 || app.Cases == nil || *app.Cases != 6 || !recordedTrue(app.Passed) || !recordedTrue(app.Discovery) || app.Writes == nil || *app.Writes != 0 || app.Executed == nil || *app.Executed != 0 || !recordedFalse(app.Real) || !recordedFalse(app.Implicit) || !recordedFalse(app.Warning) || !recordedFalse(app.Done) {
		t.Fatal("application actor or unperformed runtime claim differs")
	}
	var op struct {
		Files []struct {
			Target string `json:"target"`
			SHA    string `json:"sha256"`
		}
	}
	loadPublicReceipt(t, s.ManifestFile, &op)
	if len(app.Files) != 36 {
		t.Fatal("used file mapping incomplete")
	}
	for _, f := range op.Files {
		if app.Files[f.Target] != f.SHA {
			t.Fatal("used instruction changed from reviewed output")
		}
	}
	var run utilityRecordedRun
	loadPublicReceipt(t, e.Test, &run)
	if run.Exit == nil || *run.Exit != 0 || !recordedFalse(run.Timed) || run.Out == nil || run.Err == nil || *run.Err != "" || !strings.Contains(*run.Out, "TestRemaining3D6ActualExplicitUse") || !strings.Contains(*run.Out, "PASS") {
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
		if n == nil || *n != 80 {
			t.Fatal("actual detailed discovery count differs")
		}
	}
	for _, n := range []*int{dis.Errors, dis.Duplicates, dis.Other} {
		if n == nil || *n != 0 {
			t.Fatal("actual detailed discovery error or identity ambiguity")
		}
	}
	if !recordedTrue(dis.All) || dis.Descriptions == nil || *dis.Descriptions != 6 || dis.Known != s.NamesSHA || dis.Observed != s.NamesSHA || !recordedFalse(dis.Model) || !recordedTrue(dis.Exit) || !recordedTrue(dis.Drain) || !bytes.Equal(bytes.TrimSpace(dis.Failure), []byte("null")) || len(dis.New) != 6 {
		t.Fatal("fresh discovery omitted outcomes or overstated context")
	}
	seen := map[string]bool{}
	for _, r := range dis.New {
		if s.WantedIDs[r.Name] == "" || seen[r.Name] || !recordedTrue(r.Central) || !recordedTrue(r.EnabledPresent) || !recordedTrue(r.Enabled) || !recordedTrue(r.Description) {
			t.Fatal("new detailed discovery row differs")
		}
		seen[r.Name] = true
	}
	return validatedPurposeDeployment(t, s, validateRemainingSixReceipt)
}
func TestPurposeRemainingSixActualDeploymentAndBoundedUse(t *testing.T) {
	validatedRemainingSixDispositions(t)
}
func TestPurposeRemainingSixRejectsMissingAndInflatedEvidence(t *testing.T) {
	for _, kind := range []string{"valid", "missing_created", "missing_general", "missing_registry", "missing_exit", "missing_drain", "render_claim", "unknown_actor", "wrong_notice_receipt", "missing_use", "borrowed_manifest"} {
		t.Run(kind, func(t *testing.T) {
			var e purposeFourReceipt
			loadPublicReceipt(t, remainingSixPurposeSpec().ReceiptFile, &e)
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
			if err := validateRemainingSixReceipt(e); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}
