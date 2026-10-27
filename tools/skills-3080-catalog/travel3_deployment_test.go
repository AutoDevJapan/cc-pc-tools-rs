package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const travel3OperationSHA = "11070b772c86f7ffb459be2f75eaf6d0a436ce9dfdf0d9247eb81c6e433ede51"
const travel3Actor = "20261026-214717-todo-3080"

type travel3Native struct {
	Count    int    `json:"successful_CreateNew_count"`
	Files    int    `json:"created_files"`
	Call     string `json:"native_call_id"`
	Request  string `json:"request_sha256"`
	CLI      string `json:"original_CLI_stdout_sha256"`
	Producer string `json:"creator_runtime_sha256"`
	Revision string `json:"creator_runtime_revision"`
}
type travel3Read struct {
	Files    int   `json:"qualified_current_files"`
	Bytes    int   `json:"bytes"`
	IDs      *bool `json:"current_IDs_match_creation"`
	Buffers  *bool `json:"all_same_read_buffers_match_manifest"`
	General  *bool `json:"general_access_granted"`
	Registry *bool `json:"original_registry_recovered"`
	Rows     []struct {
		Index      int    `json:"index"`
		Target     string `json:"target"`
		SHA        string `json:"sha256"`
		Bytes      int    `json:"bytes"`
		ID         string `json:"created_and_current_object_identity_sha256"`
		SameID     *bool  `json:"creation_ID_matches_current"`
		SameBuffer *bool  `json:"same_handle_buffer_returned"`
		Reviewed   *bool  `json:"reviewed_bytes_equal"`
	} `json:"files"`
}
type travel3Install struct {
	New              int    `json:"new_targets_created"`
	Created          int    `json:"successful_CreateNew_count"`
	Files            int    `json:"current_files_readback_verified"`
	Bytes            int    `json:"bytes"`
	Instructions     int    `json:"instruction_buffers"`
	InstructionBytes int    `json:"instruction_bytes"`
	Creator          string `json:"creator_job"`
	Manifest         string `json:"creation_manifest_sha"`
	Done             *bool  `json:"whole16_complete"`
	Implicit         *bool  `json:"implicit_skill_selection_proven"`
	ThirdParty       *bool  `json:"third_party_assets_services_or_unknown_central_rights_accepted"`
	Raw              int    `json:"new_global_source_raw_credit"`
	Frozen           struct {
		Task     int    `json:"task"`
		Job      string `json:"job"`
		Spec     string `json:"spec_sha256"`
		Raw      string `json:"final_sha256"`
		Manifest string `json:"manifest_sha256"`
		Must     int    `json:"Must"`
		Frozen   *bool  `json:"frozen"`
	} `json:"frozen_full_content_review"`
	Pins map[string]struct {
		File string `json:"file"`
		SHA  string `json:"sha256"`
	} `json:"referenced_artifacts"`
}

type travel3DiscoveryRow struct {
	Name        string `json:"name"`
	Central     *bool  `json:"selected_central_path"`
	Enabled     *bool  `json:"enabled"`
	Description *bool  `json:"description_matches"`
}
type travel3Decision struct {
	Skill    string `json:"skill"`
	Case     string `json:"case"`
	Decision string `json:"decision"`
	Applied  string `json:"applied"`
	Calls    *int   `json:"service_calls"`
}
type travel3Application struct {
	Actor    string `json:"actor"`
	Read     *bool  `json:"current_instructions_literally_read_before_decisions"`
	Applied  *bool  `json:"all_scenario_meanings_applied"`
	Buffers  int    `json:"instruction_buffers"`
	Bytes    int    `json:"instruction_bytes"`
	Count    int    `json:"explicit_decision_count"`
	Service  int    `json:"new_service_or_booking_payment_monitoring_or_notification_operations"`
	Implicit *bool  `json:"implicit_skill_selection_proven"`
	Pins     []struct {
		Target string `json:"target"`
		SHA    string `json:"sha256"`
		Bytes  int    `json:"bytes"`
		Reader string `json:"actual_semantic_reader"`
	} `json:"instruction_pins"`
	Rows []travel3Decision `json:"decisions"`
}

type travel3ReviewResolution struct {
	Domain    string `json:"domain"`
	Publisher string `json:"resolution_publisher_job"`
	Finding   struct {
		ID      string `json:"id"`
		Class   string `json:"classification"`
		Subject string `json:"subject"`
	} `json:"original_finding"`
	Original struct {
		Task       int    `json:"task"`
		Job        string `json:"job"`
		Spec       string `json:"spec_sha256"`
		Final      string `json:"final_sha256"`
		Bytes      int    `json:"final_bytes"`
		Manifest   string `json:"manifest_sha256"`
		Must       *int   `json:"Must"`
		Suggestion *int   `json:"Suggestion"`
		Nit        *int   `json:"Nit"`
		Frozen     *bool  `json:"frozen"`
	} `json:"original_review"`
	Resolution struct {
		Action          string `json:"action"`
		Interpretation  string `json:"interpretation"`
		Flag            string `json:"required_existing_source_flag"`
		FlagValue       *bool  `json:"required_existing_source_flag_value"`
		Files           *int   `json:"source_metadata_files_checked"`
		SpecChanged     *bool  `json:"original_full_review_spec_changed"`
		Restarted       *bool  `json:"original_full_review_restarted"`
		Reclassified    *bool  `json:"historical_findings_reclassified_as_zero"`
		ManifestChanged *bool  `json:"original_creation_manifest_changed"`
		Central         *int   `json:"central_files_changed"`
		Create          *int   `json:"new_CreateNew_operations"`
		Authorship      *bool  `json:"independent_authorship_claimed"`
		Promoted        *bool  `json:"unseen_source_meanings_or_rights_promoted"`
	} `json:"resolution"`
}

const travel3NonAuthorshipFlag = "notice_statement_is_source_published_license_not_independent_authorship_verification"

func validateTravel3FindingResolution(r travel3ReviewResolution, i travel3Install) error {
	f := r.Original
	zero := func(p *int, want int) bool { return p != nil && *p == want }
	if r.Domain != "skills3080-travel3-frozen-review-finding-resolution-v1" || r.Publisher != "20261027-175645-todo-3080" || r.Publisher == i.Creator || r.Finding.ID != "full4083-S1" || r.Finding.Class != "Suggestion" || strings.TrimSpace(r.Finding.Subject) == "" || f.Task != i.Frozen.Task || f.Job != i.Frozen.Job || f.Spec != i.Frozen.Spec || f.Final != i.Frozen.Raw || f.Bytes != 1448 || f.Manifest != i.Manifest || !zero(f.Must, 0) || !zero(f.Suggestion, 1) || !zero(f.Nit, 0) || !recordedTrue(f.Frozen) {
		return errors.New("travel3 original full-review findings must remain frozen and explicit")
	}
	a := r.Resolution
	if a.Action != "clarify_published_notice_scope_and_enforce_explicit_non_authorship_evidence" || a.Interpretation != "Only the reviewed visible text published under the exact per-package notice is accepted; this does not establish independent authorship, unseen text or third-party service/data/support rights." || a.Flag != travel3NonAuthorshipFlag || !recordedTrue(a.FlagValue) || !zero(a.Files, 3) || !recordedFalse(a.SpecChanged) || !recordedFalse(a.Restarted) || !recordedFalse(a.Reclassified) || !recordedFalse(a.ManifestChanged) || !zero(a.Central, 0) || !zero(a.Create, 0) || !recordedFalse(a.Authorship) || !recordedFalse(a.Promoted) {
		return errors.New("travel3 historical Suggestion needs a bounded current resolution")
	}
	return nil
}

func validateTravel3NonAuthorshipSource(raw []byte) error {
	var s struct {
		NonAuthorship *bool `json:"notice_statement_is_source_published_license_not_independent_authorship_verification"`
		Raw           *bool `json:"primary_raw_full_read_claimed"`
		Masked        *bool `json:"public_original_copy_is_masked"`
		MetaUnread    *bool `json:"source_meta_json_body_unread"`
	}
	if json.Unmarshal(raw, &s) != nil || !recordedTrue(s.NonAuthorship) || !recordedFalse(s.Raw) || !recordedTrue(s.Masked) || !recordedTrue(s.MetaUnread) {
		return errors.New("travel3 source notice cannot establish authorship or hidden source meaning")
	}
	return nil
}

func validateTravel3DiscoveryRow(r travel3DiscoveryRow, bodies map[string][]byte) error {
	if bodies[r.Name+"/SKILL.md"] == nil || !recordedTrue(r.Central) || !recordedTrue(r.Enabled) || !recordedTrue(r.Description) {
		return errors.New("travel3 discovery target or selected central root differs")
	}
	return nil
}

func validateTravel3Application(a travel3Application, bodies map[string][]byte) error {
	if a.Actor != travel3Actor || !recordedTrue(a.Read) || !recordedTrue(a.Applied) || a.Buffers != 6 || a.Bytes != 11285 || a.Count != 8 || len(a.Rows) != 8 || a.Service != 0 || !recordedFalse(a.Implicit) || len(a.Pins) != 6 {
		return errors.New("travel3 actual bounded text application differs")
	}
	seen := map[string]bool{}
	for _, p := range a.Pins {
		b, ok := bodies[p.Target]
		if !ok || seen[p.Target] || p.Reader != travel3Actor || len(b) != p.Bytes || travel3Digest(b) != p.SHA || !(strings.HasSuffix(p.Target, "/SKILL.md") || strings.HasSuffix(p.Target, "/references/guide.md")) {
			return errors.New("travel3 actual instruction pin differs")
		}
		seen[p.Target] = true
	}
	// This is acceptance of eight fixed, actually recorded scenarios, not a
	// language judge for arbitrary future advice. Check their decisive facts
	// and boundaries instead of accepting any eight unrelated strings.
	want := []struct {
		skill, applied, scenario string
		facts                    []string
	}{
		{"aerobase-flight-awards", "positive", "40000マイル", []string{"1.25cpp", "1.00cpp", "未確認"}},
		{"aerobase-flight-awards", "negative", "必要マイル0", []string{"0で割らず", "欠損を0にせず", "医学的安全を保証しない"}},
		{"aerobase-flight-deals", "positive", "60000円から55000円", []string{"5000円", "取得時刻", "通知成功は未確認"}},
		{"aerobase-flight-deals", "negative", "HTTP200", []string{"順位を作らない", "設定受付", "配送成功の証拠ではない"}},
		{"aerobase-travel-activities", "positive", "利用可能180分", []string{"240分", "60分不足", "予約確認は別"}},
		{"aerobase-travel-activities", "negative", "recovery-safe", []string{"未確認", "断言しない", "本人の利用制限へしない"}},
		{"all3 current boundary", "current-task-boundary", "API-only", []string{"APIキー利用へ進まず", "別経路迂回は行わず", "全体の上限にしない"}},
		{"all3 current boundary", "current-task-boundary", "初回canonical Create", []string{"成功後は再Createせず", "原full4083を凍結", "全16未完"}},
	}
	for i, w := range want {
		r := a.Rows[i]
		if r.Skill != w.skill || r.Applied != w.applied || r.Calls == nil || *r.Calls != 0 || !strings.Contains(r.Case, w.scenario) {
			return fmt.Errorf("travel3 scenario %d differs", i+1)
		}
		for _, fact := range w.facts {
			if !strings.Contains(r.Decision, fact) {
				return fmt.Errorf("travel3 scenario %d lost its recorded decision boundary", i+1)
			}
		}
	}
	return nil
}

func travel3Digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func validateTravel3Header(i travel3Install, n travel3Native, r travel3Read) error {
	if i.New != 3 || i.Created != 1 || i.Files != 18 || i.Bytes != 36454 || i.Instructions != 6 || i.InstructionBytes != 11285 || i.Creator != travel3Actor || i.Manifest != travel3OperationSHA || !recordedFalse(i.Done) || !recordedFalse(i.Implicit) || !recordedFalse(i.ThirdParty) || i.Raw != 0 {
		return errors.New("travel3 actual installation boundary differs")
	}
	f := i.Frozen
	if f.Task != 4083 || f.Job != "20261027-103930-todo-4083" || f.Spec != "2ca4d1f6126c341b4971c5adb7fa4d2040023f72342705136ce533616ad73092" || f.Raw != "b9def53ec95455af26f44ea1bbbbf24415a52b07ce3571ca491ab14b565ef000" || f.Manifest != travel3OperationSHA || f.Must != 0 || !recordedTrue(f.Frozen) {
		return errors.New("travel3 immutable full review differs")
	}
	if n.Count != 1 || n.Files != 18 || n.Call != "call_0ecb74f0b1ee4248ac9620f14020d78b" || n.Request != "b8bc23095da855e165cee11563dfb7d979d950a5693197e29517da17cfd5b7af" || n.CLI != "0185ef684d6a19040b9e3403782a6d6e06f43888fd17c60643cec2726fde8903" || n.Producer != "c80d60ab470e52ef9fbe53c1e039d1fcdc31429e291c3f156a953b9b6059d02f" || n.Revision != "444eae8714ee8b6199c9ed95208ac40272aeb8ce" {
		return errors.New("travel3 independent actual issuance anchor differs")
	}
	if r.Files != 18 || r.Bytes != 36454 || !recordedTrue(r.IDs) || !recordedTrue(r.Buffers) || !recordedFalse(r.General) || !recordedFalse(r.Registry) || len(r.Rows) != 18 {
		return errors.New("travel3 current same-ID buffer evidence differs")
	}
	return nil
}

// Add a new accepted source only through its full original operation, exact
// real issuance, current buffers, individual notice scope and explicit use.
// Historic received cohorts and their null/raw/masked profiles stay immutable.
func validatedTravel3Dispositions(t *testing.T) map[string]cohortLedgerRow {
	t.Helper()
	var install travel3Install
	loadPublicReceipt(t, "skills-3080-travel3-installation-evidence214717.json", &install)
	wantNames := map[string]string{"bundle": "skills-3080-travel3-actual-bundle214717.json", "manifest": "skills-3080-installed-travel3-manifest214717.json", "creation": "skills-3080-travel3-create-evidence214717.json", "readback": "skills-3080-travel3-consume-evidence214717.json", "structure": "skills-3080-travel3-actual-structure214717.json", "discovery": "skills-3080-travel3-discovery214717.json", "application": "skills-3080-travel3-application214717.json", "design": "skills-3080-travel3-designs214717.json"}
	rawPins := map[string][]byte{}
	for kind, file := range wantNames {
		pin, ok := install.Pins[kind]
		if !ok || pin.File != file || filepath.Base(file) != file {
			t.Fatal("travel3 reference namespace differs")
		}
		var v any
		b := loadPublicReceipt(t, file, &v)
		if travel3Digest(b) != pin.SHA {
			t.Fatal("travel3 referenced actual bytes changed")
		}
		rawPins[kind] = b
	}
	if len(install.Pins) != len(wantNames) {
		t.Fatal("travel3 unknown actual artifact")
	}
	var native travel3Native
	var read travel3Read
	if json.Unmarshal(rawPins["creation"], &native) != nil || json.Unmarshal(rawPins["readback"], &read) != nil {
		t.Fatal("travel3 actual evidence cannot decode")
	}
	if e := validateTravel3Header(install, native, read); e != nil {
		t.Fatal(e)
	}
	var resolution travel3ReviewResolution
	loadPublicReceipt(t, "skills-3080-travel3-review-resolution175645.json", &resolution)
	if e := validateTravel3FindingResolution(resolution, install); e != nil {
		t.Fatal(e)
	}
	var operation struct {
		Sources []struct {
			Repository string `json:"repository"`
			Commit     string `json:"commit"`
			Path       string `json:"path"`
			SHA        string `json:"sha256"`
			License    struct {
				Path     string `json:"path"`
				SHA      string `json:"sha256"`
				Scope    string `json:"scope"`
				Accepted *bool  `json:"accepted"`
			} `json:"license"`
		} `json:"sources"`
		Files []struct {
			Target string `json:"target"`
			SHA    string `json:"sha256"`
			Body   string `json:"content_base64"`
		} `json:"files"`
	}
	raw := loadPublicReceipt(t, "skills-3080-operations/create-travel3-214717.json", &operation)
	if travel3Digest(raw) != travel3OperationSHA || len(operation.Sources) != 3 || len(operation.Files) != 18 {
		t.Fatal("travel3 independent operation changed")
	}
	bodies := map[string][]byte{}
	sum := 0
	for _, f := range operation.Files {
		b, e := base64.StdEncoding.DecodeString(f.Body)
		if e != nil || travel3Digest(b) != f.SHA || bodies[f.Target] != nil {
			t.Fatal("travel3 original output mismatch")
		}
		bodies[f.Target] = b
		sum += len(b)
	}
	if sum != 36454 {
		t.Fatal("travel3 original byte sum differs")
	}
	seen := map[int]bool{}
	for _, r := range read.Rows {
		if r.Index < 0 || r.Index >= len(operation.Files) || seen[r.Index] {
			t.Fatal("travel3 current index repeated or missing")
		}
		seen[r.Index] = true
		f := operation.Files[r.Index]
		if r.Target != f.Target || r.SHA != f.SHA || r.Bytes != len(bodies[f.Target]) || len(r.ID) != 64 || !recordedTrue(r.SameID) || !recordedTrue(r.SameBuffer) || !recordedTrue(r.Reviewed) {
			t.Fatal("travel3 current file does not match reviewed original")
		}
	}
	var bundle struct {
		Targets []struct {
			Name  string `json:"name"`
			Files []struct {
				Path    string `json:"path"`
				SHA     string `json:"sha256"`
				Content string `json:"content"`
			} `json:"files"`
		} `json:"targets"`
	}
	if json.Unmarshal(rawPins["bundle"], &bundle) != nil || len(bundle.Targets) != 3 {
		t.Fatal("travel3 actual bundle target count")
	}
	count := 0
	for _, target := range bundle.Targets {
		for _, f := range target.Files {
			key := target.Name + "/" + f.Path
			if string(bodies[key]) != f.Content || travel3Digest([]byte(f.Content)) != f.SHA {
				t.Fatal("travel3 actual bundle changed reviewed bytes")
			}
			count++
		}
	}
	if count != 18 {
		t.Fatal("travel3 actual bundle file count")
	}
	var discovery struct {
		Found     int                   `json:"found_count"`
		Parse     int                   `json:"parse_error_count"`
		Elsewhere int                   `json:"same_names_other_paths"`
		Exited    *bool                 `json:"process_tree_exit_confirmed"`
		Rows      []travel3DiscoveryRow `json:"found"`
	}
	if json.Unmarshal(rawPins["discovery"], &discovery) != nil || discovery.Found != 3 || discovery.Parse != 0 || discovery.Elsewhere != 0 || !recordedTrue(discovery.Exited) || len(discovery.Rows) != 3 {
		t.Fatal("travel3 fresh metadata incomplete")
	}
	found := map[string]bool{}
	for _, r := range discovery.Rows {
		if found[r.Name] || validateTravel3DiscoveryRow(r, bodies) != nil {
			t.Fatal("travel3 discovery target mismatch")
		}
		found[r.Name] = true
	}
	var application travel3Application
	if json.Unmarshal(rawPins["application"], &application) != nil || validateTravel3Application(application, bodies) != nil {
		t.Fatal("travel3 actual bounded text application differs")
	}
	var structure struct {
		Exit   int    `json:"exit"`
		Stdout string `json:"stdout"`
	}
	if json.Unmarshal(rawPins["structure"], &structure) != nil || structure.Exit != 0 || !strings.HasPrefix(structure.Stdout, "PASS: 3 explicit skills;") {
		t.Fatal("travel3 actual buffer structure check absent")
	}
	expectedIDs := map[string]string{"aerobase-flight-awards": "github:9440b3c1d3efc7897fb77a3b5ae23e6e21c830f564db18b331ba58b185e79624", "aerobase-flight-deals": "github:c21e85f1d697be2a8fd7307a49311d479591c25281c18de4c92f20d42ab8abb0", "aerobase-travel-activities": "github:b8dc79367a5f2d302052107f33da6b3ab28924279a6804c9452e8074ec1d2e3e"}
	result := map[string]cohortLedgerRow{}
	for _, s := range operation.Sources {
		name := filepath.Base(filepath.Dir(s.Path))
		if expectedIDs[name] == "" || s.Repository != "6mile/skills" || s.Commit != "920714f17d81e0a95c7a9bc51faf0a7921cd982d" || s.Path != "skills/kurosh87/"+name+"/SKILL.md" || !recordedTrue(s.License.Accepted) || s.License.Path != "skills/kurosh87/"+name+"/LICENSE.txt" || s.License.SHA != "619bd2ca1801541f1923b1f5d343e59067030fb865eae56a207fdf32fc0f8e81" || !strings.Contains(s.License.Scope, "visible selected authored workflow") || !strings.Contains(s.License.Scope, "Withheld source lines remain unread") {
			t.Fatal("travel3 source/individual notice scope differs")
		}
		if travel3Digest(bodies[name+"/references/LICENSE.txt"]) != s.License.SHA || bodies[name+"/references/original-skill.masked.txt"] == nil {
			t.Fatal("travel3 notice or declared masked view missing")
		}
		if e := validateTravel3NonAuthorshipSource(bodies[name+"/references/source.json"]); e != nil {
			t.Fatal(e)
		}
		result[expectedIDs[name]] = cohortLedgerRow{ID: expectedIDs[name], Source: s.Repository, Commit: s.Commit, Path: s.Path, SHA: s.SHA, Disposition: "installed_new_adaptation"}
	}
	var ledger struct {
		Entries []cohortLedgerRow `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	matched := map[string]bool{}
	for _, r := range ledger.Entries {
		if want, ok := result[r.ID]; ok {
			if r != want || matched[r.ID] {
				t.Fatal("travel3 live acceptance row differs")
			}
			matched[r.ID] = true
		}
	}
	if len(matched) != 3 {
		t.Fatal("travel3 actual acceptance missing live row")
	}
	return result
}

func TestTravel3HistoricalSuggestionCannotDisappearOrGrantAuthorship(t *testing.T) {
	for _, kind := range []string{"historical count erased", "resolution missing", "original review restarted", "authorship promoted", "publisher changed to creator", "publisher missing", "different finding", "classification changed", "classification missing", "different interpretation", "unrecorded effect count"} {
		t.Run(kind, func(t *testing.T) {
			var i travel3Install
			loadPublicReceipt(t, "skills-3080-travel3-installation-evidence214717.json", &i)
			var r travel3ReviewResolution
			loadPublicReceipt(t, "skills-3080-travel3-review-resolution175645.json", &r)
			if e := validateTravel3FindingResolution(r, i); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "historical count erased":
				v := 0
				r.Original.Suggestion = &v
			case "resolution missing":
				r.Resolution.Action = ""
			case "original review restarted":
				v := true
				r.Resolution.Restarted = &v
			case "authorship promoted":
				v := true
				r.Resolution.Authorship = &v
			case "publisher changed to creator":
				r.Publisher = i.Creator
			case "publisher missing":
				r.Publisher = ""
			case "different finding":
				r.Finding.ID = "another-finding"
			case "classification changed":
				r.Finding.Class = "Nit"
			case "classification missing":
				r.Finding.Class = ""
			case "different interpretation":
				r.Resolution.Interpretation = "Independent authorship is confirmed."
			case "unrecorded effect count":
				r.Resolution.Central = nil
			}
			if validateTravel3FindingResolution(r, i) == nil {
				t.Fatal("invalid historical finding resolution accepted")
			}
		})
	}
	for _, value := range []any{nil, false} {
		meta := map[string]any{travel3NonAuthorshipFlag: value, "primary_raw_full_read_claimed": false, "public_original_copy_is_masked": true, "source_meta_json_body_unread": true}
		raw, e := json.Marshal(meta)
		if e != nil {
			t.Fatal(e)
		}
		if validateTravel3NonAuthorshipSource(raw) == nil {
			t.Fatal("missing or false non-authorship evidence accepted")
		}
	}
}

func TestTravel3RejectsWrongCentralRootAndUnrelatedDecisions(t *testing.T) {
	var op struct {
		Files []struct {
			Target string `json:"target"`
			Body   string `json:"content_base64"`
		} `json:"files"`
	}
	loadPublicReceipt(t, "skills-3080-operations/create-travel3-214717.json", &op)
	bodies := map[string][]byte{}
	for _, f := range op.Files {
		b, e := base64.StdEncoding.DecodeString(f.Body)
		if e != nil {
			t.Fatal(e)
		}
		bodies[f.Target] = b
	}
	var d struct {
		Rows []travel3DiscoveryRow `json:"found"`
	}
	loadPublicReceipt(t, "skills-3080-travel3-discovery214717.json", &d)
	for _, row := range d.Rows {
		if e := validateTravel3DiscoveryRow(row, bodies); e != nil {
			t.Fatal(e)
		}
		row.Central = nil
		if validateTravel3DiscoveryRow(row, bodies) == nil {
			t.Fatal("missing selected central root accepted")
		}
		v := false
		row.Central = &v
		if validateTravel3DiscoveryRow(row, bodies) == nil {
			t.Fatal("different selected central root accepted")
		}
	}
	for _, kind := range []string{"unrelated decision", "wrong skill", "different instruction", "missing per-case call count"} {
		t.Run(kind, func(t *testing.T) {
			var a travel3Application
			loadPublicReceipt(t, "skills-3080-travel3-application214717.json", &a)
			if e := validateTravel3Application(a, bodies); e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "unrelated decision":
				a.Rows[0].Decision = "現在の予約が確定した。"
			case "wrong skill":
				a.Rows[4].Skill = "aerobase-flight-deals"
			case "different instruction":
				a.Pins[0].Target = a.Pins[1].Target
			case "missing per-case call count":
				a.Rows[7].Calls = nil
			}
			if validateTravel3Application(a, bodies) == nil {
				t.Fatal("invalid recorded application accepted")
			}
		})
	}
}
func TestTravel3RejectsMissingOrInflatedIssuanceAndBoundaryClaims(t *testing.T) {
	for _, kind := range []string{"missing frozen", "different full review", "replayed creation", "different native call", "grant escalation", "current IDs unverified"} {
		t.Run(kind, func(t *testing.T) {
			var i travel3Install
			loadPublicReceipt(t, "skills-3080-travel3-installation-evidence214717.json", &i)
			var n travel3Native
			loadPublicReceipt(t, "skills-3080-travel3-create-evidence214717.json", &n)
			var r travel3Read
			loadPublicReceipt(t, "skills-3080-travel3-consume-evidence214717.json", &r)
			switch kind {
			case "missing frozen":
				i.Frozen.Frozen = nil
			case "different full review":
				i.Frozen.Spec = strings.Repeat("0", 64)
			case "replayed creation":
				n.Count = 2
			case "different native call":
				n.Call = "call_synthetic"
			case "grant escalation":
				v := true
				i.ThirdParty = &v
			case "current IDs unverified":
				r.IDs = nil
			}
			if e := validateTravel3Header(i, n, r); e == nil {
				t.Fatal(fmt.Sprintf("invalid actual evidence accepted: %s", kind))
			}
		})
	}
}
