package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

const purposeFourSHA = "3e86571b6f2081f83e0820d7fe4215b3d3193f40d5173164c5511e02153074ef"
const purposeFourRequestSHA = "4566059d407bfcbdf89d55ff4a2624ac9de0d321b8ee904a466d2fc6a4d82d0f"
const purposeFourReadbackSHA = "4837df32d58eb92591dc5ab843ba061ce792f7bcc6510ffec87c5bf1f92eea74"
const purposeFourCreateWrapperSHA = "b315aaeb5c6cbfeb0d4af84982102fb86f5e375a4a82e7dbdc9d49008f012aac"

type purposeFourFile struct {
	Target  string `json:"target"`
	Bytes   int    `json:"bytes"`
	SHA     string `json:"sha256"`
	Regular *bool  `json:"regular_non_reparse_single_link"`
	Equal   *bool  `json:"reviewed_bytes_equal"`
}

type purposeFourCreate struct {
	Todo      int    `json:"todo_id"`
	Domain    string `json:"domain"`
	Operation string `json:"operation"`
	Commit    string `json:"source_commit"`
	Files     int    `json:"file_count"`
	Qualified *bool  `json:"qualified"`
	Count     int    `json:"created_files"`
	SHA       string `json:"manifest_sha256"`
	Request   string `json:"request_sha256"`
	General   *bool  `json:"general_access_granted"`
	Registry  *bool  `json:"original_registry_recovered"`
}

type purposeFourReceipt struct {
	Done         *bool             `json:"overall_complete"`
	Created      int               `json:"new_targets_created"`
	Files        int               `json:"files_readback_verified"`
	OldWrites    *int              `json:"old_target_writes"`
	OldReads     *int              `json:"unknown_existing_body_reads"`
	General      *bool             `json:"general_access_granted"`
	Registry     *bool             `json:"original_registry_recovered"`
	Manifest     string            `json:"operation_manifest"`
	SHA          string            `json:"operation_manifest_sha256"`
	Commit       string            `json:"operation_manifest_main_commit"`
	Installed    string            `json:"installed_manifest"`
	InstalledSHA string            `json:"installed_manifest_sha256"`
	Designs      string            `json:"designs"`
	DesignSHA    string            `json:"designs_sha256"`
	FileRows     []purposeFourFile `json:"files"`
	ReadbackSHA  string            `json:"readback_receipt_sha256"`
	ReadbackFile string            `json:"readback_receipt"`
	CreateFile   string            `json:"creation_wrapper"`
	CreateSHA    string            `json:"creation_wrapper_sha256"`
	Create       purposeFourCreate `json:"creation_result"`
	Acceptance   struct {
		Task         int    `json:"todo"`
		Job          string `json:"job"`
		ModelOrigin  string `json:"model_origin"`
		EffortOrigin string `json:"effort_origin"`
		Model        string `json:"model"`
		Effort       string `json:"effort"`
		Native       *bool  `json:"native_verified"`
		ParentRead   *bool  `json:"parent_original_full_read"`
		Spec         string `json:"spec_sha256"`
		Raw          string `json:"raw_sha256"`
		Manifest     string `json:"manifest_sha256"`
		Must         *int   `json:"must"`
	} `json:"acceptance_review"`
	Rejected struct {
		Task     int    `json:"todo"`
		SHA      string `json:"returned_manifest_sha256"`
		Accepted *bool  `json:"machine_receipt_accepted"`
	} `json:"rejected_original_receipt"`
	Discovery struct {
		FoundCount int                `json:"found_count"`
		Errors     *int               `json:"parse_error_count"`
		SameNames  *int               `json:"same_names_other_paths"`
		Model      *bool              `json:"model_called"`
		Exited     *bool              `json:"process_tree_exit_confirmed"`
		Drained    *bool              `json:"pending_stdout_read_drained"`
		Found      []newSkillMetadata `json:"found"`
	} `json:"discovery"`
	Retained struct {
		IDs      []string `json:"managed_adoption_inventory_ids"`
		Expected int      `json:"targets"`
		Found    int      `json:"found_count"`
		Enabled  int      `json:"enabled_count"`
		NamesSHA string   `json:"known_target_names_sha256"`
	} `json:"retained_known_metadata"`
	Use struct {
		Core         *bool           `json:"core_passed"`
		KindCount    int             `json:"example_kind_count"`
		GameExamples json.RawMessage `json:"game_examples"`
		Stable       *bool           `json:"active_bytes_stable_before_after"`
		External     *int            `json:"external_api_calls"`
		Executed     *bool           `json:"source_scripts_executed"`
		Implicit     *bool           `json:"implicit_skill_selection_proven"`
		Separate     *bool           `json:"fresh_separate_model_turn_executed"`
		Exam         struct {
			Questions, Answers, Total int
			Independent               *bool `json:"independent_arithmetic_passed"`
			IDs                       *bool `json:"question_answer_ids_match"`
			Rendered                  *bool `json:"word_or_print_rendered"`
		} `json:"exam"`
		Inventory struct {
			Reload      *bool `json:"actual_v2_file_reload_matches"`
			SaveFailure *bool `json:"injected_save_failure_preserves_committed_state"`
		} `json:"inventory"`
		Fog struct {
			Blocks   *bool `json:"closed_wall_blocks"`
			Renderer *bool `json:"renderer_executed"`
			Game     *bool `json:"actual_game_executed"`
			UI       *bool `json:"ui_changed"`
		} `json:"fog"`
	} `json:"explicit_application"`
	UseSource    string `json:"application_source"`
	UseSHA       string `json:"application_source_sha256"`
	FullBudget   *bool  `json:"full_context_budget_proven"`
	WarningCause *bool  `json:"warning_cause_linked"`
}

func validatePurposeFour(e purposeFourReceipt) error {
	zero := func(n *int) bool { return n != nil && *n == 0 }
	if !recordedFalse(e.Done) || e.Created != 4 || e.Files != 24 || !zero(e.OldWrites) || !zero(e.OldReads) || !recordedFalse(e.General) || !recordedFalse(e.Registry) || e.SHA != purposeFourSHA || e.Commit != "67e7e9c9dc41b5afd0c0e62ba1e476f44f5f477e" || !recordedTrue(e.Create.Qualified) || e.Create.Count != 24 || e.Create.SHA != e.SHA || e.Create.Request != purposeFourRequestSHA || !recordedFalse(e.Create.General) || !recordedFalse(e.Create.Registry) {
		return errors.New("exact new-only creation boundary differs")
	}
	if e.Create.Todo != 3080 || e.Create.Domain != "skills-3080-purpose-v1" || e.Create.Operation != "CreateNew" || e.Create.Commit != e.Commit || e.Create.Files != 24 || e.ReadbackSHA != purposeFourReadbackSHA || e.CreateSHA != purposeFourCreateWrapperSHA {
		return errors.New("original create/readback receipt identity differs")
	}
	a := e.Acceptance
	if a.Task != 3761 || a.Job != "20261025-080256-todo-3761" || a.ModelOrigin != "owner_request" || a.EffortOrigin != "owner_request" || a.Model != "gpt-6-luna" || a.Effort != "max" || !recordedTrue(a.Native) || !recordedTrue(a.ParentRead) || a.Spec != "079e07c498bb590e0a8f04fe1e54198280668c269c61abd3ac146c1858abb7b8" || a.Raw != "60727648f488c7e4b56f8dc2bccaf1b537fdbd57db97a5a88cedbf15de357ee3" || a.Manifest != purposeFourSHA || !zero(a.Must) || e.Rejected.Task != 3756 || len(e.Rejected.SHA) != 63 || !recordedFalse(e.Rejected.Accepted) {
		return errors.New("actual acceptance and rejected transcription receipt conflated")
	}
	d := e.Discovery
	if d.FoundCount != 4 || !zero(d.Errors) || !zero(d.SameNames) || !recordedFalse(d.Model) || !recordedTrue(d.Exited) || !recordedTrue(d.Drained) || !recordedFalse(e.FullBudget) || !recordedFalse(e.WarningCause) {
		return errors.New("discovery or full-context claim differs")
	}
	seen := map[string]bool{}
	for _, f := range d.Found {
		if seen[f.Name] || !recordedTrue(f.Central) || !recordedTrue(f.EnabledPresent) || !recordedTrue(f.Enabled) || !recordedTrue(f.Description) {
			return errors.New("discovery target missing or duplicated")
		}
		seen[f.Name] = true
	}
	if len(seen) != 4 {
		return errors.New("four discovery identities missing")
	}
	u := e.Use
	if !recordedTrue(u.Stable) || !zero(u.External) || !recordedFalse(u.Executed) || !recordedFalse(u.Implicit) || !recordedFalse(u.Separate) || u.Exam.Questions != 7 || u.Exam.Answers != 7 || u.Exam.Total != 35 || !recordedTrue(u.Exam.Independent) || !recordedTrue(u.Exam.IDs) || !recordedFalse(u.Exam.Rendered) || !recordedTrue(u.Inventory.Reload) || !recordedTrue(u.Inventory.SaveFailure) || !recordedTrue(u.Fog.Blocks) || !recordedFalse(u.Fog.Renderer) || !recordedFalse(u.Fog.Game) || !recordedFalse(u.Fog.UI) {
		return errors.New("explicit examples overclaim runtime/rendering or omit failures")
	}
	if e.Retained.Expected != 52 || e.Retained.Found != 52 || e.Retained.Enabled != 52 || len(e.Retained.IDs) != 39 || e.Retained.NamesSHA != "ef74467e2c3ca5ed9a39f83c6ff3b84eddf500e8cb3545e979233e4abd763e9c" {
		return errors.New("managed retention snapshot differs")
	}
	return nil
}

func validatePurposeFourFiles(rows []purposeFourFile, wanted map[string]purposeFourFile) error {
	return validatePurposeFileRows(rows, wanted, 24)
}

func validatePurposeFileRows(rows []purposeFourFile, wanted map[string]purposeFourFile, count int) error {
	if len(rows) != count || len(wanted) != count {
		return errors.New("readback explicit output count differs")
	}
	seen := map[string]bool{}
	for _, r := range rows {
		w, ok := wanted[r.Target]
		if !ok || seen[r.Target] || r.Bytes != w.Bytes || r.SHA != w.SHA || !recordedTrue(r.Regular) || !recordedTrue(r.Equal) {
			return errors.New("readback target/path/SHA/bytes/state differs from reviewed output")
		}
		seen[r.Target] = true
	}
	return nil
}

func validatedPurposeDeployment(t *testing.T, s purposeDeploymentSpec, validate func(purposeFourReceipt) error) map[string]cohortLedgerRow {
	t.Helper()
	var e purposeFourReceipt
	receipt := loadPublicReceipt(t, s.ReceiptFile, &e)
	if _, err := uniqueEvidenceJSON(receipt); err != nil {
		t.Fatal(err)
	}
	if e.Manifest != s.ManifestFile || e.Installed != s.InstalledFile || e.Designs != s.DesignFile || e.CreateFile != s.CreateFile || e.ReadbackFile != s.ReadbackFile || e.UseSource != s.UseSource {
		t.Fatal("unexpected purpose artifact path")
	}
	if err := validate(e); err != nil {
		t.Fatal(err)
	}
	var operation struct {
		Sources []struct {
			Repo    string `json:"repository"`
			Commit  string `json:"commit"`
			Path    string `json:"path"`
			SHA     string `json:"sha256"`
			License struct {
				Path     string `json:"path"`
				SHA      string `json:"sha256"`
				Scope    string `json:"scope"`
				Accepted bool   `json:"accepted"`
			} `json:"license"`
		} `json:"sources"`
		Files []struct {
			Target string `json:"target"`
			SHA    string `json:"sha256"`
			Base64 string `json:"content_base64"`
		} `json:"files"`
	}
	raw := loadPublicReceipt(t, e.Manifest, &operation)
	h := sha256.Sum256(raw)
	if hex.EncodeToString(h[:]) != s.ManifestSHA || len(operation.Sources) != s.SourceCount || len(operation.Files) != s.FileCount {
		t.Fatal("independent fixed operation anchor differs")
	}
	actual := map[string]string{}
	expectedFiles := map[string]purposeFourFile{}
	preserved := map[string]bool{}
	for _, f := range operation.Files {
		b, err := base64.StdEncoding.DecodeString(f.Base64)
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != f.SHA || actual[f.Target] != "" {
			t.Fatal("reviewed output mismatch/duplicate")
		}
		actual[f.Target] = f.SHA
		expectedFiles[f.Target] = purposeFourFile{Target: f.Target, Bytes: len(b), SHA: f.SHA}
		preserved[f.SHA] = true
	}
	if err := validatePurposeFileRows(e.FileRows, expectedFiles, s.FileCount); err != nil {
		t.Fatal(err)
	}
	if e.ReadbackFile != s.ReadbackFile || e.CreateFile != s.CreateFile {
		t.Fatal("unexpected original receipt path")
	}
	var readback struct {
		Job       string            `json:"job"`
		SHA       string            `json:"manifest_sha256"`
		Request   string            `json:"creation_request_sha256"`
		CreateSHA string            `json:"creation_receipt_sha256"`
		Created   int               `json:"new_targets_created"`
		Files     int               `json:"files_readback_verified"`
		OldWrites *int              `json:"old_target_writes"`
		OldReads  *int              `json:"old_unknown_body_reads"`
		General   *bool             `json:"general_access_granted"`
		Registry  *bool             `json:"original_registry_recovered"`
		Rows      []purposeFourFile `json:"files"`
	}
	rb := loadPublicReceipt(t, e.ReadbackFile, &readback)
	rh := sha256.Sum256(rb)
	if hex.EncodeToString(rh[:]) != s.ReadbackSHA || readback.Job != s.ActorJob || readback.SHA != s.ManifestSHA || readback.Request != s.RequestSHA || readback.CreateSHA != s.CreateSHA || readback.Created != s.TargetCount || readback.Files != s.FileCount || readback.OldWrites == nil || *readback.OldWrites != 0 || readback.OldReads == nil || *readback.OldReads != 0 || !recordedFalse(readback.General) || !recordedFalse(readback.Registry) {
		t.Fatal("original readback identity/boundary differs")
	}
	if err := validatePurposeFileRows(readback.Rows, expectedFiles, s.FileCount); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(readback.Rows, e.FileRows) {
		t.Fatal("published file rows differ from independent original readback")
	}
	var wrapper struct {
		Exit   *int   `json:"exit"`
		Timed  *bool  `json:"timed_out"`
		Stdout string `json:"stdout"`
		Stderr string `json:"stderr"`
	}
	cb := loadPublicReceipt(t, e.CreateFile, &wrapper)
	ch := sha256.Sum256(cb)
	if hex.EncodeToString(ch[:]) != s.CreateSHA || wrapper.Exit == nil || *wrapper.Exit != 0 || !recordedFalse(wrapper.Timed) || wrapper.Stderr != "" {
		t.Fatal("original CreateNew command did not complete")
	}
	var command purposeFourCreate
	if json.Unmarshal([]byte(wrapper.Stdout), &command) != nil || !reflect.DeepEqual(command, e.Create) {
		t.Fatal("published creation result differs from exact original command")
	}
	var manifest struct {
		Targets []struct {
			Name   string `json:"name"`
			New    bool   `json:"new"`
			Before string `json:"before_sha256"`
			Files  []struct {
				Path string `json:"path"`
				SHA  string `json:"sha256"`
			} `json:"files"`
		} `json:"targets"`
	}
	installed := loadPublicReceipt(t, e.Installed, &manifest)
	h = sha256.Sum256(installed)
	if hex.EncodeToString(h[:]) != e.InstalledSHA || len(manifest.Targets) != s.TargetCount {
		t.Fatal("installed manifest not fixed")
	}
	fileCount := 0
	targetSeen := map[string]bool{}
	for _, r := range manifest.Targets {
		if targetSeen[r.Name] || !r.New || r.Before != "absent" {
			t.Fatal("creation/absence not fixed")
		}
		targetSeen[r.Name] = true
		for _, f := range r.Files {
			if actual[r.Name+"/"+f.Path] != f.SHA {
				t.Fatal("actual manifest diverges from reviewed output")
			}
			fileCount++
		}
	}
	if fileCount != s.FileCount {
		t.Fatal("installed output omitted")
	}
	var designs struct {
		Targets []directSkillDesign `json:"targets"`
	}
	db := loadPublicReceipt(t, e.Designs, &designs)
	h = sha256.Sum256(db)
	if hex.EncodeToString(h[:]) != e.DesignSHA || len(designs.Targets) != s.TargetCount {
		t.Fatal("source design identity missing")
	}
	wantedIDs := s.WantedIDs
	accepted := map[string]cohortLedgerRow{}
	for _, d := range designs.Targets {
		if wantedIDs[d.Name] != d.ID || accepted[d.ID].ID != "" {
			t.Fatal("fixed accepted occurrence identity differs")
		}
		if !targetSeen[d.Name] || actual[d.Name+"/SKILL.md"] != d.EntrySHA {
			t.Fatal("design target differs")
		}
		matched := 0
		for _, s := range operation.Sources {
			if s.Repo == d.Repo && s.Commit == d.Commit && s.Path == d.Path && s.SHA == d.SHA && s.License.SHA == d.NoticeSHA && s.License.Accepted && s.License.Scope != "" && preserved[s.SHA] && preserved[s.License.SHA] {
				matched++
			}
		}
		if matched != 1 {
			t.Fatal("individual source/notice not preserved and scoped")
		}
		accepted[d.ID] = cohortLedgerRow{ID: d.ID, Source: d.Repo, Commit: d.Commit, Path: d.Path, SHA: d.SHA, Disposition: "installed_new_adaptation"}
	}
	for _, f := range e.Discovery.Found {
		if !targetSeen[f.Name] {
			t.Fatal("discovered unrelated target counted")
		}
	}
	entries, original := managedMetadataSources(t)
	if err := validateManagedMetadataSnapshot(e.Retained.IDs, e.Retained.Expected, e.Retained.NamesSHA, entries, original); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.UseSource, "..") || !strings.HasPrefix(e.UseSource, "evidence/") {
		t.Fatal("application source path differs")
	}
	source, err := os.ReadFile("../../docs/" + e.UseSource)
	if err != nil {
		t.Fatal(err)
	}
	h = sha256.Sum256(source)
	if hex.EncodeToString(h[:]) != e.UseSHA || !bytes.Contains(source, []byte(s.ApplicationMarker)) {
		t.Fatal("executed application source hash differs")
	}
	return accepted
}

func validatedPurposeFourDispositions(t *testing.T) map[string]cohortLedgerRow {
	return validatedPurposeDeployment(t, fourPurposeSpec(), validatePurposeFour)
}

func TestPurposeQualifiedFourActualDeploymentAndSourceBytes(t *testing.T) {
	validatedPurposeFourDispositions(t)
}

func TestPurposeQualifiedFourRejectsCounterAndEvidenceInflation(t *testing.T) {
	for _, kind := range []string{"valid", "missing_creation", "old_target_write", "general_access", "wrong_review_hash", "old_63_digit_accepted", "unverified_native", "missing_discovery", "context_overclaim", "upstream_execution", "render_overclaim", "lost_save_failure", "wrong_create_domain", "wrong_create_operation", "wrong_create_source", "wrong_create_todo", "wrong_create_count", "wrong_readback_sha", "wrong_wrapper_sha", "wrong_reviewer_job", "wrong_model_origin", "wrong_effort_origin"} {
		t.Run(kind, func(t *testing.T) {
			var e purposeFourReceipt
			loadPublicReceipt(t, "skills-3080-four-installation-evidence.json", &e)
			yes, no := true, false
			one := 1
			switch kind {
			case "missing_creation":
				e.Create.Qualified = nil
			case "old_target_write":
				e.OldWrites = &one
			case "general_access":
				e.General = &yes
			case "wrong_review_hash":
				e.Acceptance.Manifest = e.Rejected.SHA
			case "old_63_digit_accepted":
				e.Rejected.Accepted = &yes
			case "unverified_native":
				e.Acceptance.Native = &no
			case "missing_discovery":
				e.Discovery.Found = e.Discovery.Found[:3]
			case "context_overclaim":
				e.FullBudget = &yes
			case "upstream_execution":
				e.Use.Executed = &yes
			case "render_overclaim":
				e.Use.Fog.Renderer = &yes
			case "lost_save_failure":
				e.Use.Inventory.SaveFailure = nil
			case "wrong_create_domain":
				e.Create.Domain = "other"
			case "wrong_create_operation":
				e.Create.Operation = "qualify-only"
			case "wrong_create_source":
				e.Create.Commit = strings.Repeat("0", 40)
			case "wrong_create_todo":
				e.Create.Todo = 3686
			case "wrong_create_count":
				e.Create.Files = 23
			case "wrong_readback_sha":
				e.ReadbackSHA = strings.Repeat("0", 64)
			case "wrong_wrapper_sha":
				e.CreateSHA = strings.Repeat("0", 64)
			case "wrong_reviewer_job":
				e.Acceptance.Job = "20261025-075031-todo-3756"
			case "wrong_model_origin":
				e.Acceptance.ModelOrigin = "policy"
			case "wrong_effort_origin":
				e.Acceptance.EffortOrigin = "policy"
			}
			if err := validatePurposeFour(e); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}

func TestPurposeFourRejectsChangedReadbackRowsEvenWhenCountIs24(t *testing.T) {
	var e purposeFourReceipt
	loadPublicReceipt(t, "skills-3080-four-installation-evidence.json", &e)
	wanted := map[string]purposeFourFile{}
	for _, r := range e.FileRows {
		wanted[r.Target] = r
	}
	for _, kind := range []string{"valid", "hash", "path", "size", "duplicate", "missing", "regular_false", "readback_false"} {
		t.Run(kind, func(t *testing.T) {
			rows := append([]purposeFourFile(nil), e.FileRows...)
			no := false
			switch kind {
			case "hash":
				rows[0].SHA = strings.Repeat("0", 64)
			case "path":
				rows[0].Target = "other/SKILL.md"
			case "size":
				rows[0].Bytes++
			case "duplicate":
				rows[1] = rows[0]
			case "missing":
				rows = rows[:23]
			case "regular_false":
				rows[0].Regular = &no
			case "readback_false":
				rows[0].Equal = &no
			}
			if err := validatePurposeFourFiles(rows, wanted); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}
