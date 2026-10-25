package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// Each row describes an observed deployment, not an installation ceiling.
// New sets reuse the same manifest/individual-notice/creation/readback loader.
type purposeDeploymentSpec struct {
	ReceiptFile, ManifestFile, InstalledFile, DesignFile, CreateFile, ReadbackFile, UseSource, ApplicationMarker string
	ManifestSHA, RequestSHA, MainCommit, CreateSHA, ReadbackSHA, ActorJob                                        string
	TargetCount, SourceCount, FileCount, RetainedCount, RetainedIDs                                              int
	NamesSHA                                                                                                     string
	ReviewTask                                                                                                   int
	ReviewJob, ReviewSpec, ReviewRaw                                                                             string
	WantedIDs                                                                                                    map[string]string
}

func fourPurposeSpec() purposeDeploymentSpec {
	return purposeDeploymentSpec{
		ReceiptFile: "skills-3080-four-installation-evidence.json", ManifestFile: "skills-3080-operations/create-four-074042.json", InstalledFile: "skills-3080-installed-four-manifest.json", DesignFile: "skills-3080-four-designs.json", CreateFile: "skills-3080-four-create-receipt.json", ReadbackFile: "skills-3080-four-readback-receipt.json", UseSource: "evidence/skills-3080-four-use-074042.go.txt", ApplicationMarker: "TestInstalledFourExplicitApplication",
		ManifestSHA: purposeFourSHA, RequestSHA: purposeFourRequestSHA, MainCommit: "67e7e9c9dc41b5afd0c0e62ba1e476f44f5f477e", CreateSHA: purposeFourCreateWrapperSHA, ReadbackSHA: purposeFourReadbackSHA, ActorJob: "20261025-074042-todo-3080", TargetCount: 4, SourceCount: 4, FileCount: 24, RetainedCount: 52, RetainedIDs: 39, NamesSHA: "ef74467e2c3ca5ed9a39f83c6ff3b84eddf500e8cb3545e979233e4abd763e9c", ReviewTask: 3761, ReviewJob: "20261025-080256-todo-3761", ReviewSpec: "079e07c498bb590e0a8f04fe1e54198280668c269c61abd3ac146c1858abb7b8", ReviewRaw: "60727648f488c7e4b56f8dc2bccaf1b537fdbd57db97a5a88cedbf15de357ee3",
		WantedIDs: map[string]string{"timezone-helper": "github:ac72b94f8ac90a84d51a96b3d16e288d93b11f455d2b33aa50d0d320af2029e1", "exam-generator": "github:0c071dac30d780613fbf2bbb1e5d81e62d028883da584cf9fa9bd68ca33a838b", "build-game-inventory": "github:dcfad5899bfe731a670bd01b4cfafa5a4aa3d03303f40df2b562279b41baf405", "implement-fog-of-war": "github:c51dc53e6bbbd50fb36e0dc24236e430698fe779352ac996e34115a0cbc5c882"},
	}
}

func gameSixPurposeSpec() purposeDeploymentSpec {
	return purposeDeploymentSpec{
		ReceiptFile: "skills-3080-game6-installation-evidence.json", ManifestFile: "skills-3080-operations/create-game6-074042.json", InstalledFile: "skills-3080-installed-game6-manifest.json", DesignFile: "skills-3080-game6-designs.json", CreateFile: "skills-3080-game6-create-receipt.json", ReadbackFile: "skills-3080-game6-readback-receipt.json", UseSource: "evidence/skills-3080-game6-use-074042.go.txt", ApplicationMarker: "TestInstalledGameSixExplicitApplication",
		ManifestSHA: "2141bfa3a4d04fd62398741f5e80627e8cb0ef36815ccec6643711540d8bc924", RequestSHA: "a7a92aed62f572ecff390d785f7360b0db740ce2a676da94cdea86d74b724e28", MainCommit: "affcc70d2d9a329128120402c6500409b3d81abf", CreateSHA: "911d91490ff9ce44d044052fc1df967cb2eb045854eed072677fcbace9164a7d", ReadbackSHA: "d45023e7769fa87a03a8b47f750c2e37ad34b44438482ecbf0b9838c32c0855d", ActorJob: "20261025-074042-todo-3080", TargetCount: 6, SourceCount: 6, FileCount: 36, RetainedCount: 58, RetainedIDs: 45, NamesSHA: "c3158d4c2718c6b97e9b9858e613626ac02ce21d9e59f079216d067725e767e8", ReviewTask: 3777, ReviewJob: "20261025-091917-todo-3777", ReviewSpec: "e3fb1feb1f6100e655a50f44991d0b5de950e37aca394b270102fc0dbf9aed25", ReviewRaw: "da49ec829789a13eb186f48679158639cc7b2e4e22c87042f16845bddabd1050",
		WantedIDs: map[string]string{"design-action-combat": "github:efea3e18c8a1c529f0dc118a714c53698807049e54bc8775759fcba4a1fafbec", "tune-enemy-ai": "github:7e7b86ab5a899b4639cffd87d65ca9184dedb899d1ba98d46b6a74f2a49860ca", "build-threejs-enemy-systems": "github:38bb0d4a6f21b0c29cbfa8b6214812a7cdea16a9216c058a1225711c9908280b", "design-game-encounters": "github:382998d3830f6337c6f2838a64421601e482aaf2ac3cb66841051d81e443cf6b", "author-game-levels": "github:77f491d4d72871f2c31152c773d92fe3ee2377d8a0993b9673d562a01250c8e2", "optimize-threejs-games": "github:153988a15c1b7cf16741074f845ee57b203ee0315654f9f5d76196982b339d0e"},
	}
}

func validatePurposeSetHeader(e purposeFourReceipt, s purposeDeploymentSpec) error {
	zero := func(v *int) bool { return v != nil && *v == 0 }
	if !recordedFalse(e.Done) || e.Created != s.TargetCount || e.Files != s.FileCount || !zero(e.OldWrites) || !zero(e.OldReads) || !recordedFalse(e.General) || !recordedFalse(e.Registry) || e.SHA != s.ManifestSHA || e.Commit != s.MainCommit || !recordedTrue(e.Create.Qualified) || e.Create.Count != s.FileCount || e.Create.SHA != e.SHA || e.Create.Request != s.RequestSHA || !recordedFalse(e.Create.General) || !recordedFalse(e.Create.Registry) {
		return errors.New("exact new-only creation boundary differs")
	}
	if e.Create.Todo != 3080 || e.Create.Domain != "skills-3080-purpose-v1" || e.Create.Operation != "CreateNew" || e.Create.Commit != e.Commit || e.Create.Files != s.FileCount || e.ReadbackSHA != s.ReadbackSHA || e.CreateSHA != s.CreateSHA {
		return errors.New("original create/readback receipt identity differs")
	}
	a := e.Acceptance
	if a.Task != s.ReviewTask || a.Job != s.ReviewJob || a.ModelOrigin != "owner_request" || a.EffortOrigin != "owner_request" || a.Model != "gpt-6-luna" || a.Effort != "max" || !recordedTrue(a.Native) || !recordedTrue(a.ParentRead) || a.Spec != s.ReviewSpec || a.Raw != s.ReviewRaw || a.Manifest != s.ManifestSHA || !zero(a.Must) {
		return errors.New("actual acceptance metadata differs")
	}
	d := e.Discovery
	if d.FoundCount != s.TargetCount || !zero(d.Errors) || !zero(d.SameNames) || !recordedFalse(d.Model) || !recordedTrue(d.Exited) || !recordedTrue(d.Drained) || !recordedFalse(e.FullBudget) || !recordedFalse(e.WarningCause) {
		return errors.New("discovery or full-context claim differs")
	}
	seen := map[string]bool{}
	for _, f := range d.Found {
		if _, ok := s.WantedIDs[f.Name]; !ok || seen[f.Name] || !recordedTrue(f.Central) || !recordedTrue(f.EnabledPresent) || !recordedTrue(f.Enabled) || !recordedTrue(f.Description) {
			return errors.New("discovery target missing or duplicated")
		}
		seen[f.Name] = true
	}
	if len(seen) != s.TargetCount {
		return errors.New("discovery identities missing")
	}
	if e.Retained.Expected != s.RetainedCount || e.Retained.Found != s.RetainedCount || e.Retained.Enabled != s.RetainedCount || len(e.Retained.IDs) != s.RetainedIDs || e.Retained.NamesSHA != s.NamesSHA {
		return errors.New("managed retention snapshot differs")
	}
	return nil
}

func validateGameSixReceipt(e purposeFourReceipt) error {
	if err := validatePurposeSetHeader(e, gameSixPurposeSpec()); err != nil {
		return err
	}
	u := e.Use
	if !recordedTrue(u.Core) || u.KindCount != 6 || u.External == nil || *u.External != 0 || !recordedFalse(u.Executed) || !recordedFalse(u.Implicit) || !recordedFalse(u.Separate) || len(u.GameExamples) == 0 {
		return errors.New("six explicit examples missing or overclaim external execution")
	}
	return nil
}

func TestPurposeGameSixReusesActualDeploymentAndIndividualSourceValidation(t *testing.T) {
	s := gameSixPurposeSpec()
	validatedPurposeDeployment(t, s, validateGameSixReceipt)
	var e struct {
		Application    string `json:"application_result"`
		ApplicationSHA string `json:"application_result_sha256"`
		Test           string `json:"application_test_receipt"`
		TestSHA        string `json:"application_test_receipt_sha256"`
		Use            struct {
			Examples json.RawMessage `json:"game_examples"`
		} `json:"explicit_application"`
	}
	loadPublicReceipt(t, s.ReceiptFile, &e)
	if e.Application != "skills-3080-game6-application-result.json" || e.ApplicationSHA != "d235cc6cad1fb774a5e995db167d91a3065fd43843f8c28148da5c42b7ddeaa0" || e.Test != "skills-3080-game6-application-test-receipt.json" || e.TestSHA != "c9451f4527c317c65093e0693c474b854f29d740b8a20375412811e3918f6545" {
		t.Fatal("original application artifacts differ")
	}
	var original map[string]any
	b := loadPublicReceipt(t, e.Application, &original)
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != e.ApplicationSHA {
		t.Fatal("actual application raw bytes changed")
	}
	derived, err := uniqueEvidenceJSON(e.Use.Examples)
	raw, rawErr := uniqueEvidenceJSON(b)
	if err != nil || rawErr != nil || !reflect.DeepEqual(derived, raw) {
		t.Fatal("published use summary differs from original observations")
	}
	for _, key := range []string{"actual_game_executed", "actual_gpu_benchmark", "renderer_executed", "ui_changed", "upstream_scripts_executed", "implicit_skill_selection_proven", "new_separate_model_turn_executed", "whole16_complete"} {
		if original[key] != false {
			t.Fatalf("unproved application claim: %s", key)
		}
	}
	if original["job"] != s.ActorJob || original["manifest_sha256"] != s.ManifestSHA {
		t.Fatal("application actor/manifest not fixed")
	}
	var run struct {
		Exit  *int   `json:"exit"`
		Timed *bool  `json:"timed_out"`
		Out   string `json:"stdout"`
		Err   string `json:"stderr"`
	}
	test := loadPublicReceipt(t, e.Test, &run)
	h = sha256.Sum256(test)
	if hex.EncodeToString(h[:]) != e.TestSHA || run.Exit == nil || *run.Exit != 0 || !recordedFalse(run.Timed) || run.Err != "" {
		t.Fatal("actual example test did not complete successfully")
	}
}

func TestPurposeCommonHeaderRejectsBothCreationCountersAndMixedReceipts(t *testing.T) {
	for _, s := range []purposeDeploymentSpec{fourPurposeSpec(), gameSixPurposeSpec()} {
		for _, kind := range []string{"valid", "created_files", "file_count", "other_set_manifest", "other_set_receipt", "wrong_origin"} {
			t.Run(s.ReceiptFile+"/"+kind, func(t *testing.T) {
				var e purposeFourReceipt
				loadPublicReceipt(t, s.ReceiptFile, &e)
				switch kind {
				case "created_files":
					e.Create.Count--
				case "file_count":
					e.Create.Files--
				case "other_set_manifest":
					e.SHA = "different"
				case "other_set_receipt":
					e.ReadbackSHA = "different"
				case "wrong_origin":
					e.Acceptance.ModelOrigin = "policy"
				}
				if err := validatePurposeSetHeader(e, s); (err != nil) != (kind != "valid") {
					t.Fatalf("%s: %v", kind, err)
				}
			})
		}
	}
}
