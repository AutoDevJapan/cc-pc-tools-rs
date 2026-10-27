package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

const next3OperationSHA = "5df4ba1cf1946271e5d03d6b6e02009f0c29d2c8025a9b8be0161a3d8aefae70"
const next3Actor = "20261027-175645-todo-3080"

func next3RecordedZero(value *int) bool { return value != nil && *value == 0 }

func validateNext3LifecycleHistory(raw []byte) error {
	var value struct {
		Stage           string `json:"stage"`
		Operations      *int   `json:"central_operations"`
		Adoptions       *int   `json:"new_adoptions"`
		ExtraOperations *int   `json:"publication_additional_central_operations"`
		ExtraAdoptions  *int   `json:"publication_additional_adoptions"`
		History         *struct {
			Stage       string `json:"stage"`
			Operations  *int   `json:"central_operations"`
			Adoptions   *int   `json:"new_adoptions"`
			ProposalSHA string `json:"original_proposal_sha256"`
		} `json:"pre_creation_history"`
		Targets []struct {
			Name       string `json:"name"`
			Kind       string `json:"target_kind"`
			Deployment string `json:"deployment_state"`
			Qualified  *bool  `json:"pre_mutation_absence_qualification_completed"`
			History    *struct {
				Kind       string `json:"target_kind"`
				Deployment string `json:"deployment_state"`
				Qualified  *bool  `json:"pre_mutation_absence_qualification_completed"`
				Before     string `json:"before_sha256"`
				Presence   *struct {
					At     string `json:"observed_at"`
					Exists *bool  `json:"path_exists"`
					Opened *bool  `json:"existing_contents_opened"`
					Future *bool  `json:"future_absence_not_asserted"`
				} `json:"directory_presence_observation"`
			} `json:"pre_creation_history"`
		} `json:"targets"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Stage != "actual_created_readback_discovered_and_explicitly_applied" || !next3Value(value.Operations, 1) || !next3Value(value.Adoptions, 3) || !next3RecordedZero(value.ExtraOperations) || !next3RecordedZero(value.ExtraAdoptions) || value.History == nil || value.History.Stage != "proposal_not_qualified_or_placed" || !next3RecordedZero(value.History.Operations) || !next3RecordedZero(value.History.Adoptions) || value.History.ProposalSHA != "0e0ff688af3fbcd8042d88ab640b01b6e012be318561f58f2f24e1c55d71d151" || len(value.Targets) != 3 {
		return errors.New("next3 current lifecycle or original preparation history lost")
	}
	seen := map[string]bool{}
	for _, target := range value.Targets {
		if seen[target.Name] || (target.Name != "aerobase-travel-hotels" && target.Name != "aerobase-travel-lounges" && target.Name != "aerobase-travel-wallet") || target.Kind != "newly_created_adaptation" || target.Deployment != "created_once_and_current_readback_verified" || !recordedTrue(target.Qualified) || target.History == nil {
			return errors.New("next3 target current state or preparation history missing")
		}
		seen[target.Name] = true
		h := target.History
		if h.Kind != "new_adaptation_intent" || h.Deployment != "proposed_uninstalled" || !recordedFalse(h.Qualified) || h.Before != "absent" || h.Presence == nil || h.Presence.At != "2026-10-27T12:56:18Z" || !recordedFalse(h.Presence.Exists) || !recordedFalse(h.Presence.Opened) || !recordedTrue(h.Presence.Future) {
			return errors.New("next3 original target observation erased or promoted to current absence")
		}
	}
	return nil
}

func TestNext3RetainsBothRootAndTargetPreparationHistory(t *testing.T) {
	for _, file := range []string{"skills-3080-next3-actual-bundle175645.json", "skills-3080-installed-next3-manifest175645.json"} {
		for _, change := range []string{"valid", "root missing", "root null", "root state changed", "target missing", "target null", "old qualification promoted", "old presence erased"} {
			t.Run(file+"/"+change, func(t *testing.T) {
				var value map[string]any
				loadPublicReceipt(t, file, &value)
				target := received27Object(t, received27Array(t, value["targets"])[0])
				switch change {
				case "root missing":
					delete(value, "pre_creation_history")
				case "root null":
					value["pre_creation_history"] = nil
				case "root state changed":
					received27Object(t, value["pre_creation_history"])["stage"] = "actual"
				case "target missing":
					delete(target, "pre_creation_history")
				case "target null":
					target["pre_creation_history"] = nil
				case "old qualification promoted":
					received27Object(t, target["pre_creation_history"])["pre_mutation_absence_qualification_completed"] = true
				case "old presence erased":
					delete(received27Object(t, target["pre_creation_history"]), "directory_presence_observation")
				}
				raw, e := json.Marshal(value)
				if e != nil {
					t.Fatal(e)
				}
				e = validateNext3LifecycleHistory(raw)
				if (change == "valid") != (e == nil) {
					t.Fatalf("%s: %v", change, e)
				}
			})
		}
	}
}

func TestNext3PublishedLifecycleIsCurrent(t *testing.T) {
	for _, name := range []string{"skills-3080-next3-actual-bundle175645.json", "skills-3080-installed-next3-manifest175645.json"} {
		var value struct {
			Stage   string `json:"stage"`
			Targets []struct {
				Deployment string `json:"deployment_state"`
				Qualified  *bool  `json:"pre_mutation_absence_qualification_completed"`
			} `json:"targets"`
		}
		loadPublicReceipt(t, name, &value)
		if value.Stage != "actual_created_readback_discovered_and_explicitly_applied" || len(value.Targets) != 3 {
			t.Fatalf("%s still mixes proposal and actual lifecycle", name)
		}
		for _, target := range value.Targets {
			if target.Deployment != "created_once_and_current_readback_verified" || !recordedTrue(target.Qualified) {
				t.Fatal("actual placement or completed pre-mutation qualification missing")
			}
		}
	}
}

func TestNext3FullReviewRejectsChangedOrMissingOwnerOrigins(t *testing.T) {
	for _, key := range []string{"model_origin", "effort_origin"} {
		for _, change := range []string{"policy", "missing", "null"} {
			t.Run(key+"/"+change, func(t *testing.T) {
				var value map[string]any
				loadPublicReceipt(t, "skills-3080-next3-installation-evidence175645.json", &value)
				review := received27Object(t, value["qualified_independent_review_native"])
				metadata := received27Object(t, review["metadata"])
				switch change {
				case "missing":
					delete(metadata, key)
				case "null":
					metadata[key] = nil
				default:
					metadata[key] = change
				}
				raw, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if validateNext3CurrentReview(raw) == nil {
					t.Fatal("owner-request origin was missing or changed but accepted")
				}
			})
		}
	}
}

func validateNext3Header(i travel3Install, n travel3Native, r travel3Read) error {
	if i.New != 3 || i.Created != 1 || i.Files != 21 || i.Bytes != 65566 || i.Instructions != 6 || i.InstructionBytes != 9206 || i.Creator != next3Actor || i.Manifest != next3OperationSHA || !recordedFalse(i.Done) || !recordedFalse(i.Implicit) || !recordedFalse(i.ThirdParty) || i.Raw != 0 {
		return errors.New("next3 actual installation boundary differs")
	}
	f := i.Frozen
	if f.Task != 4129 || f.Job != "20261028-054325-todo-4129" || f.Spec != "62dd891e54730d9b3a4daf82331b21fb3c05b3ea5efa508b1bdff632c8e58f44" || f.Raw != "c593ea9e665a19a8ebd1bac348ba061b203a73b21b3ca6e7facc0fc299a94661" || f.Manifest != next3OperationSHA || f.Must != 0 || !recordedTrue(f.Frozen) {
		return errors.New("next3 original full reviewer must remain frozen")
	}
	if n.Count != 1 || n.Files != 21 || n.Call != "call_b3c15733a51840c3a0f58ba684ca3c1c" || n.Request != "b74fe84e8da49c63f75cfe41cb905c0493d60eeaa51edfa31fd9182874a0dda2" || n.CLI != "077c2cd83fb00b3c06577edd63cfcb2a2e8451b7e6474a1cd974b954fa6cb2c4" || n.Producer != "9be9a066053183a099e78133571d394ecbb2845372e3cf603e292c74dac1a8d3" || n.Revision != "deb430b8b089fde2391dbd243c01db48de48d493" {
		return errors.New("next3 independent issuance anchor differs")
	}
	if r.Files != 21 || r.Bytes != 65566 || !recordedTrue(r.IDs) || !recordedTrue(r.Buffers) || !recordedFalse(r.General) || !recordedFalse(r.Registry) || len(r.Rows) != 21 {
		return errors.New("next3 current buffers or IDs differ")
	}
	return nil
}

func validateNext3CurrentReview(raw []byte) error {
	var extra struct {
		Budget *bool `json:"full_discovery_or_context_budget_proven"`
		Frozen struct {
			Must, Suggestion, Nit *int
			Full                  *bool `json:"full_content_no_comparison"`
		} `json:"frozen_full_content_review"`
		Review struct {
			Qualified             *bool `json:"normal_qualified"`
			Read                  *bool `json:"parent_full_original_read"`
			Must, Suggestion, Nit *int
			Metadata              struct {
				Job           string `json:"id"`
				Model, Effort string
				ModelOrigin   string `json:"model_origin"`
				EffortOrigin  string `json:"effort_origin"`
				Spec          string `json:"spec_sha256"`
				Final         string `json:"final_sha256"`
			}
			Native []struct{ Model, Effort string } `json:"actual_native"`
		} `json:"qualified_independent_review_native"`
	}
	if json.Unmarshal(raw, &extra) != nil || !recordedFalse(extra.Budget) || !recordedTrue(extra.Frozen.Full) || !next3RecordedZero(extra.Frozen.Must) || !next3RecordedZero(extra.Frozen.Suggestion) || !next3RecordedZero(extra.Frozen.Nit) || !recordedTrue(extra.Review.Qualified) || !recordedTrue(extra.Review.Read) || !next3RecordedZero(extra.Review.Must) || !next3RecordedZero(extra.Review.Suggestion) || !next3RecordedZero(extra.Review.Nit) {
		return errors.New("next3 explicit full review and limits absent")
	}
	m := extra.Review.Metadata
	if m.Job != "20261028-054325-todo-4129" || m.Model != "gpt-6-luna" || m.Effort != "max" || m.ModelOrigin != "owner_request" || m.EffortOrigin != "owner_request" || m.Spec != "62dd891e54730d9b3a4daf82331b21fb3c05b3ea5efa508b1bdff632c8e58f44" || m.Final != "c593ea9e665a19a8ebd1bac348ba061b203a73b21b3ca6e7facc0fc299a94661" || len(extra.Review.Native) < 1 {
		return errors.New("next3 actual native/spec/result differs")
	}
	for _, n := range extra.Review.Native {
		if n.Model != "gpt-6-luna" || n.Effort != "max" {
			return errors.New("next3 actual native model or effort differs")
		}
	}
	return nil
}

func validateNext3NonAuthorshipSource(raw []byte) error {
	var s struct {
		NonAuthorship *bool `json:"notice_statement_is_source_published_license_not_independent_authorship_verification"`
		Copied        *bool `json:"source_original_body_copied"`
		Masked        *bool `json:"masked_original_same_buffer_preserved"`
		Meta          *bool `json:"source_meta_json_body_unread"`
		Current       struct {
			Raw     *bool  `json:"raw_full_read"`
			Ordered *bool  `json:"masked_workflow_literally_read_in_order"`
			Reader  string `json:"reader_job"`
		} `json:"parent_current_primary_read"`
	}
	if json.Unmarshal(raw, &s) != nil || !recordedTrue(s.NonAuthorship) || !recordedFalse(s.Copied) || !recordedTrue(s.Masked) || !recordedTrue(s.Meta) || !recordedFalse(s.Current.Raw) || !recordedTrue(s.Current.Ordered) || s.Current.Reader != next3Actor {
		return errors.New("next3 masked current reading cannot grant authorship or unseen text")
	}
	return nil
}

func validateNext3Application(a travel3Application, bodies map[string][]byte) error {
	if a.Actor != next3Actor || !recordedTrue(a.Read) || !recordedTrue(a.Applied) || a.Buffers != 6 || a.Bytes != 9206 || a.Count != 8 || len(a.Rows) != 8 || a.Service != 0 || !recordedFalse(a.Implicit) || len(a.Pins) != 6 {
		return errors.New("next3 actual bounded text application differs")
	}
	seen := map[string]bool{}
	for _, p := range a.Pins {
		b, ok := bodies[p.Target]
		if !ok || seen[p.Target] || p.Reader != next3Actor || len(b) != p.Bytes || travel3Digest(b) != p.SHA || !(strings.HasSuffix(p.Target, "/SKILL.md") || strings.HasSuffix(p.Target, "/references/guide.md")) {
			return errors.New("next3 current instruction pin differs")
		}
		seen[p.Target] = true
	}
	// These are the eight already performed hypothetical text decisions, not
	// a judge of arbitrary future advice or proof of real service transactions.
	want := []struct {
		skill, scenario string
		facts           []string
	}{
		{"aerobase-travel-hotels", "same-room-two-night-total", []string{"250USD", "税", "実予約指示"}},
		{"aerobase-travel-hotels", "offer-prebook-not-booked", []string{"200受付", "予約成立", "実カード"}},
		{"aerobase-travel-hotels", "rebook-timeout-unknown-states", []string{"未確認", "盲再送", "旧予約"}},
		{"aerobase-travel-lounges", "ninety-minute-connection", []string{"10分", "45分", "保証しない"}},
		{"aerobase-travel-lounges", "listing-is-not-admission", []string{"当日の空き", "Pro", "wallet"}},
		{"aerobase-travel-wallet", "provided-noncard-points-cpp", []string{"2.00", "1.70", "未確認"}},
		{"aerobase-travel-wallet", "zero-missing-currency", []string{"分母0", "欠けた現金総額を0にせず", "無指定Gmail"}},
		{"all3", "source-api-advertising-and-rights", []string{"API-only/429", "第三者", "未読"}},
	}
	for i, w := range want {
		r := a.Rows[i]
		if r.Skill != w.skill || r.Case != w.scenario || strings.TrimSpace(r.Applied) == "" || r.Calls == nil || *r.Calls != 0 {
			return fmt.Errorf("next3 recorded case %d differs", i+1)
		}
		for _, f := range w.facts {
			if !strings.Contains(r.Decision, f) {
				return fmt.Errorf("next3 case %d lost its decisive fact or limit", i+1)
			}
		}
	}
	return nil
}

func TestNext3ActualEvidence(t *testing.T) {
	if len(validatedNext3Dispositions(t)) != 3 {
		t.Fatal("new actual3 missing")
	}
}

func TestNext3RejectsWrongIssuanceAndScope(t *testing.T) {
	for _, kind := range []string{"valid", "extra creation", "wrong native call", "old full reviewer", "changed frozen result", "missing same ID", "missing buffer proof", "general access", "implicit selection", "third party rights", "wrong file total"} {
		t.Run(kind, func(t *testing.T) {
			var i travel3Install
			var n travel3Native
			var r travel3Read
			loadPublicReceipt(t, "skills-3080-next3-installation-evidence175645.json", &i)
			loadPublicReceipt(t, "skills-3080-next3-create-evidence175645.json", &n)
			loadPublicReceipt(t, "skills-3080-next3-consume-evidence175645.json", &r)
			yes := true
			switch kind {
			case "extra creation":
				n.Count = 2
			case "wrong native call":
				n.Call = "copied"
			case "old full reviewer":
				i.Frozen.Task = 4033
			case "changed frozen result":
				i.Frozen.Raw = "changed"
			case "missing same ID":
				r.IDs = nil
			case "missing buffer proof":
				r.Buffers = nil
			case "general access":
				r.General = &yes
			case "implicit selection":
				i.Implicit = &yes
			case "third party rights":
				i.ThirdParty = &yes
			case "wrong file total":
				r.Files = 20
			}
			e := validateNext3Header(i, n, r)
			if (kind == "valid") != (e == nil) {
				t.Fatalf("%s: %v", kind, e)
			}
		})
	}
}

func validatedNext3Dispositions(t *testing.T) map[string]cohortLedgerRow {
	t.Helper()
	var install travel3Install
	installationRaw := loadPublicReceipt(t, "skills-3080-next3-installation-evidence175645.json", &install)
	if err := validateNext3CurrentReview(installationRaw); err != nil {
		t.Fatal(err)
	}
	wantNames := map[string]string{"precreation": "skills-3080-next3-precreation-qualification175645.json", "bundle": "skills-3080-next3-actual-bundle175645.json", "manifest": "skills-3080-installed-next3-manifest175645.json", "creation": "skills-3080-next3-create-evidence175645.json", "readback": "skills-3080-next3-consume-evidence175645.json", "structure": "skills-3080-next3-actual-structure175645.json", "discovery": "skills-3080-next3-discovery175645.json", "application": "skills-3080-next3-application175645.json", "design": "skills-3080-next3-designs175645.json"}
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
	for _, key := range []string{"bundle", "manifest"} {
		if err := validateNext3LifecycleHistory(rawPins[key]); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateNext3Precreation(rawPins["precreation"], rawPins["creation"], rawPins["readback"]); err != nil {
		t.Fatal(err)
	}
	if err := validateNext3FlowProof(installationRaw, rawPins["creation"], rawPins["readback"]); err != nil {
		t.Fatal(err)
	}
	if e := validateNext3Header(install, native, read); e != nil {
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
	raw := loadPublicReceipt(t, "skills-3080-operations/create-travel-next3-175645.json", &operation)
	if travel3Digest(raw) != next3OperationSHA || len(operation.Sources) != 3 || len(operation.Files) != 21 {
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
	if sum != 65566 {
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
	if count != 21 {
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
	if json.Unmarshal(rawPins["application"], &application) != nil || validateNext3Application(application, bodies) != nil {
		t.Fatal("travel3 actual bounded text application differs")
	}
	var structure struct {
		Exit   int    `json:"exit"`
		Stdout string `json:"stdout"`
	}
	if json.Unmarshal(rawPins["structure"], &structure) != nil || structure.Exit != 0 || !strings.HasPrefix(structure.Stdout, "PASS: 3 explicit skills;") {
		t.Fatal("travel3 actual buffer structure check absent")
	}
	expectedIDs := map[string]string{"aerobase-travel-hotels": "github:f753b00dede8c609f3f058adb0e80834db2b5d21ac1ab766e2b6226cb6882475", "aerobase-travel-lounges": "github:5f2060a1af74a008a8625912793220e95725198d9bfceb25f1efd49447c8cc0f", "aerobase-travel-wallet": "github:8eec7dd96fe7e61c15a53691cb3176ca1db6762a4e076690d38f40d7d1395b70"}
	result := map[string]cohortLedgerRow{}
	for _, s := range operation.Sources {
		name := filepath.Base(filepath.Dir(s.Path))
		if expectedIDs[name] == "" || s.Repository != "6mile/skills" || s.Commit != "920714f17d81e0a95c7a9bc51faf0a7921cd982d" || s.Path != "skills/kurosh87/"+name+"/SKILL.md" || !recordedTrue(s.License.Accepted) || s.License.Path != "skills/kurosh87/"+name+"/LICENSE.txt" || s.License.SHA != "619bd2ca1801541f1923b1f5d343e59067030fb865eae56a207fdf32fc0f8e81" || !strings.Contains(s.License.Scope, "directly reviewed visible workflow") || !strings.Contains(s.License.Scope, "Withheld text is not adopted") {
			t.Fatal("travel3 source/individual notice scope differs")
		}
		if travel3Digest(bodies[name+"/references/LICENSE.txt"]) != s.License.SHA || bodies[name+"/references/original-skill.masked.txt"] == nil {
			t.Fatal("travel3 notice or declared masked view missing")
		}
		if e := validateNext3NonAuthorshipSource(bodies[name+"/references/source.json"]); e != nil {
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
