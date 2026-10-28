package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

const notice4Manifest = "5ab55cfca8778374e86862905e9c39d5bfe11bea2ae6e3e33ec82f82e7607f89"
const notice4Request = "758a78033f857c1cda0f25deeed5a18a89b16b5730caf3cd758fc3929a789167"
const notice4Call = "call_9a5bc551506640e49f56dc88155e0eaf"

// This operation has its own issuer and readback contract. Do not project its
// provenance through the older three-target shape, which discards these keys.
type notice4Native struct {
	Actor        string             `json:"job"`
	Targets      int                `json:"created_targets"`
	Count        int                `json:"successful_CreateNew_count"`
	Files        int                `json:"created_files"`
	Call         string             `json:"native_call_id"`
	Request      string             `json:"request_sha256"`
	CLI          string             `json:"original_CLI_stdout_sha256"`
	Producer     string             `json:"creator_runtime_sha256"`
	Revision     string             `json:"creator_runtime_revision"`
	Invocation   string             `json:"exact_unmodified_invocation_sha256"`
	Blocks       string             `json:"original_tool_blocks_sha256"`
	At           string             `json:"native_call_timestamp"`
	Thread       string             `json:"thread_id"`
	CaptureReads *int               `json:"central_file_reads_by_capture"`
	Added        *int               `json:"new_adoptions"`
	AddedScope   string             `json:"new_adoptions_scope"`
	OtherBodies  *bool              `json:"other_native_bodies_returned"`
	NewReceipt   *bool              `json:"scope_clarification_is_a_new_issuer_receipt"`
	Failed       *[]json.RawMessage `json:"prior_failed_exact_invocation_attempts"`
}

type notice4Read struct {
	Actor      string `json:"job"`
	Call       string `json:"creation_native_call"`
	Manifest   string `json:"manifest_sha256"`
	Request    string `json:"request_sha256"`
	Producer   string `json:"current_observed_normally_admitted_producer"`
	Revision   string `json:"current_revision"`
	Files      int    `json:"qualified_current_files"`
	Bytes      int    `json:"bytes"`
	BatchCount int    `json:"bounded_normal_batches"`
	NewCalls   *int   `json:"new_creation_calls_by_reader"`
	RawCredit  *int   `json:"new_global_raw_source_read_credit"`
	Atomic     *bool  `json:"atomic_snapshot_across_files_or_historical_continuity_proven"`
	OSWriter   string `json:"current_OS_writer"`
	IDs        *bool  `json:"current_IDs_match_creation"`
	Buffers    *bool  `json:"all_same_read_buffers_match_manifest"`
	General    *bool  `json:"general_access_granted"`
	Registry   *bool  `json:"original_registry_recovered"`
	Rows       []struct {
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

type notice4Header struct {
	Creator  string `json:"creator_job"`
	Targets  int    `json:"new_targets_created"`
	Files    int    `json:"current_files_readback_verified"`
	Bytes    int    `json:"bytes"`
	Manifest string `json:"creation_manifest_sha256"`
	Runtime  string `json:"creator_runtime_sha256"`
	Revision string `json:"creator_runtime_revision"`
	Frozen   struct {
		Task     int    `json:"task"`
		Job      string `json:"job"`
		Spec     string `json:"spec_sha256"`
		Final    string `json:"final_sha256"`
		Frozen   *bool  `json:"frozen"`
		Never    *bool  `json:"never_edit_spec_or_restart_this_task"`
		Creator  string `json:"creator"`
		Manifest string `json:"manifest_sha256"`
		Request  string `json:"request_sha256"`
		Call     string `json:"actual_native_creation"`
		Files    int    `json:"successful_creation_file_count"`
		Targets  int    `json:"successful_target_count"`
		Full     *bool  `json:"full_content_no_comparison"`
	} `json:"frozen_full_content_review"`
	Review struct {
		Raw        string `json:"original_sha256"`
		Spec       string `json:"spec_sha256"`
		Model      string `json:"model"`
		Effort     string `json:"effort"`
		Must       *int   `json:"Must"`
		Suggestion *int   `json:"Suggestion"`
		Nit        *int   `json:"Nit"`
		All        *bool  `json:"all13_inputs_and28_payloads"`
		ParentRead *bool  `json:"parent_full_original_read"`
		Native     []struct {
			Model  string `json:"model"`
			Effort string `json:"effort"`
			Source string `json:"source"`
		} `json:"actual_native"`
	} `json:"qualified_full_review"`
	Pins map[string]struct {
		File string `json:"file"`
		SHA  string `json:"sha256"`
	} `json:"referenced_artifacts"`
	Implicit   *bool `json:"implicit_skill_selection_proven"`
	Budget     *bool `json:"full_discovery_or_context_budget_proven"`
	ThirdParty *bool `json:"third_party_assets_services_or_unknown_central_rights_accepted"`
	Done       *bool `json:"whole16_complete"`
}

type notice4Case struct {
	Skill    string `json:"skill"`
	Kind     string `json:"kind"`
	Input    string `json:"input"`
	Decision string `json:"decision"`
}

func validateNotice4Cases(rows []notice4Case) error {
	if len(rows) != 8 {
		return errors.New("notice4 case count differs")
	}
	// Preserve the already performed input/decision pairs, including the real
	// workflow versus synthetic-payment boundary. These independent fixed pins
	// check a historical record, not the quality of arbitrary future advice.
	pins := []string{
		"5b9f181ecc0f574e73ed1d94595d080063155f8d5aca721179845836e8087762",
		"a960864ff7fbb6ac834922fc8e1868561cd5027eef3ba6de6fa7227e59f6ea71",
		"085cb7a13099e6e738186207d29871653563de257bfc7a74683294417e424920",
		"0ea864becce6c1f4409963d15a082829c1f59245ce4eb43837c64138e6eebaf4",
		"2a27f378ffeeca4db9b006a9945d59d1956a04f2bf8dc9407ca712c41b10f4eb",
		"c23b3904a3f38a15349c81af0c31dbe9abd75111551f2f098fd9dd32f43e18e4",
		"849bece3c36017290846ba2c18af8d37f8c3f64c635a46c36ea69a2a204a7f0a",
		"9dd090a72803ed94d8f61173bcc520eed24defc4d6ea3a68c1d87135b2c0fb37",
	}
	for j, r := range rows {
		want := []string{"rlm-controller", "rlm-controller", "parse-payment-qr", "parse-payment-qr", "payroll-batch-reconciliation", "payroll-batch-reconciliation", "payment-status-reconciliation", "payment-status-reconciliation"}[j]
		if r.Skill != want || r.Decision == "" || (j < 2) != strings.HasPrefix(r.Kind, "real current-workflow") || (j >= 2) != strings.HasPrefix(r.Kind, "synthetic text-only") {
			return errors.New("notice4 application classification differs")
		}
		if r.Input == "" || travel3Digest([]byte(strings.Join([]string{r.Skill, r.Kind, r.Input, r.Decision}, "\n"))) != pins[j] {
			return errors.New("notice4 actual recorded input or decision differs")
		}
	}
	return nil
}

func validateNotice4Header(i notice4Header, n notice4Native, r notice4Read) error {
	if i.Creator != next3Actor || i.Targets != 4 || i.Files != 28 || i.Bytes != 89144 || i.Manifest != notice4Manifest || i.Runtime != "f8b2ba6e66e60b06bbbc46c2c0f3d6eedc0208d40444c30e37b70afe29db4fc7" || !recordedFalse(i.Implicit) || !recordedFalse(i.Budget) || !recordedFalse(i.ThirdParty) || !recordedFalse(i.Done) {
		return errors.New("notice4 actual scope, producer or boundaries differ")
	}
	f := i.Frozen
	if f.Task != 4184 || f.Job != "20261028-105110-todo-4184" || f.Spec != "3cb28efe25d36e48c426a0ca14ebaaf8f3e8cd1213f97758dfc0f95f1d9a24df" || f.Final != "204f8d4175e5158c71a73259252c16c8faedbf909d979f333ad8361554962a06" || !recordedTrue(f.Frozen) || !recordedTrue(f.Never) || n.Call != notice4Call || n.Count != 1 || n.Request != notice4Request || r.Files != 28 || r.Bytes != 89144 || !recordedTrue(r.IDs) || !recordedTrue(r.Buffers) || !recordedFalse(r.General) {
		return errors.New("notice4 full reviewer, once-only creation or current identity differs")
	}
	q := i.Review
	if q.Raw != f.Final || q.Spec != f.Spec || q.Model != "gpt-6-luna" || q.Effort != "max" || !recordedCount(q.Must, 0) || !recordedCount(q.Suggestion, 0) || !recordedCount(q.Nit, 0) || !recordedTrue(q.All) || !recordedTrue(q.ParentRead) || len(q.Native) == 0 {
		return errors.New("notice4 full review model, result or13/28 scope differs")
	}
	for _, ctx := range q.Native {
		if ctx.Model != q.Model || ctx.Effort != q.Effort || ctx.Source != "native_turn_context" {
			return errors.New("notice4 actual full-review native identity differs")
		}
	}
	if i.Revision != "a71330f543969b4585f01b7359a84a2fa528c9f2" || n.Revision != i.Revision || n.Producer != i.Runtime || n.Files != 28 || n.CLI != "96a3ba604ad620a70b86cdb1eae108e4fb5b32e6dbc5eaf9c7626591d3994560" || !recordedFalse(r.Registry) {
		return errors.New("notice4 original creation stdout, producer or recovery scope differs")
	}
	if n.Actor != i.Creator || n.Targets != i.Targets || f.Creator != i.Creator || f.Targets != i.Targets || f.Files != i.Files || f.Manifest != i.Manifest || f.Request != n.Request || f.Call != n.Call || !recordedTrue(f.Full) {
		return errors.New("notice4 actual issuer, target count or frozen review binding differs")
	}
	if r.Actor != i.Creator || r.Call != n.Call || r.Manifest != i.Manifest || r.Request != n.Request || r.Producer != n.Producer || r.Revision != n.Revision || r.BatchCount != 4 || !recordedCount(r.NewCalls, 0) || !recordedCount(r.RawCredit, 0) || !recordedFalse(r.Atomic) || r.OSWriter != "unproved" {
		return errors.New("notice4 current readback issuer or provenance differs")
	}
	if n.Invocation != "5fa20e6bd8299e8c4c424878042dee788647e0c6deb01b708f0897aaf4fdd640" || n.Blocks != "5f29cb5ebf81a51937e0495b098a70b8c5b177a4e018bb38f83f3892a54040e7" || n.At != "2026-10-28T05:42:53.185Z" || n.Thread != "01a16bab-c238-7293-953d-d0f447cf2c05" || !recordedCount(n.CaptureReads, 0) || !recordedCount(n.Added, 0) || n.AddedScope != "additional_ledger_entries_written_by_capture_only" || !recordedFalse(n.OtherBodies) || !recordedFalse(n.NewReceipt) || n.Failed == nil || len(*n.Failed) != 0 {
		return errors.New("notice4 original native capture identity or capture-only scope differs")
	}
	return nil
}

// Bind later final decisions to actual placement evidence without changing the
// original primary-reading records or approving equal-body sibling occurrences.
func validatedNoticeNext4Dispositions(t *testing.T) map[string]cohortLedgerRow {
	t.Helper()
	var install notice4Header
	loadPublicReceipt(t, "skills-3080-notice-next4-installation-evidence175645.json", &install)
	wantPins := map[string]string{"bundle": "skills-3080-notice-next4-actual-bundle175645.json", "manifest": "skills-3080-installed-notice-next4-manifest175645.json", "creation": "skills-3080-notice-next4-create-evidence175645.json", "readback": "skills-3080-notice-next4-consume-evidence175645.json", "structure": "skills-3080-notice-next4-actual-structure175645.json", "discovery": "skills-3080-notice-next4-discovery175645.json", "application": "skills-3080-notice-next4-application175645.json", "design": "skills-3080-notice-next4-actual-designs175645.json", "precreation": "skills-3080-notice-next4-precreation-qualification175645.json"}
	// These are the independently received historical records for this one
	// creation. A later observation needs a new record; updating the manifest's
	// own pointer cannot silently change the accepted issuer or readback facts.
	fixedRecords := map[string]string{
		"creation":    "8fac2380eb3610eebf12e3f36f960368005ca871b86f4e9f00d6c5387024d9d9",
		"readback":    "0f6fc5a4d7ccbc8ef5bb6dc76d4aaee50697ad6074acfe0e578a25bf5bd97485",
		"application": "94f01cc1d68aae72c45948f4edd314d3e00e9ce09a17d30df873dff2e0300a01",
	}
	rawPins := map[string][]byte{}
	for kind, file := range wantPins {
		pin, ok := install.Pins[kind]
		if !ok || pin.File != file || filepath.Base(file) != file {
			t.Fatal("notice4 artifact namespace differs")
		}
		var v any
		b := loadPublicReceipt(t, file, &v)
		if travel3Digest(b) != pin.SHA {
			t.Fatal("notice4 artifact hash differs")
		}
		if fixed, exists := fixedRecords[kind]; exists && travel3Digest(b) != fixed {
			t.Fatal("notice4 original historical record changed despite pointer update")
		}
		rawPins[kind] = b
	}
	if len(install.Pins) != len(wantPins) {
		t.Fatal("notice4 artifact membership differs")
	}
	var native notice4Native
	var read notice4Read
	if json.Unmarshal(rawPins["creation"], &native) != nil || json.Unmarshal(rawPins["readback"], &read) != nil || validateNotice4Header(install, native, read) != nil {
		t.Fatal("notice4 current acceptance incomplete")
	}
	var op struct {
		Files []struct {
			Target string `json:"target"`
			SHA    string `json:"sha256"`
			Body   string `json:"content_base64"`
		} `json:"files"`
	}
	opRaw := loadPublicReceipt(t, "skills-3080-operations/create-notice-next4-175645.json", &op)
	if travel3Digest(opRaw) != notice4Manifest || len(op.Files) != 28 || len(read.Rows) != 28 {
		t.Fatal("notice4 fixed original operation or current count differs")
	}
	bodies := map[string][]byte{}
	total := 0
	for _, f := range op.Files {
		b, e := base64.StdEncoding.DecodeString(f.Body)
		if e != nil || bodies[f.Target] != nil || travel3Digest(b) != f.SHA {
			t.Fatal("notice4 reviewed payload differs")
		}
		bodies[f.Target] = b
		total += len(b)
	}
	if total != 89144 {
		t.Fatal("notice4 original byte sum differs")
	}
	seen := map[int]bool{}
	for _, r := range read.Rows {
		if r.Index < 0 || r.Index >= len(op.Files) || seen[r.Index] {
			t.Fatal("notice4 current indices missing/repeated")
		}
		seen[r.Index] = true
		f := op.Files[r.Index]
		if r.Target != f.Target || r.SHA != f.SHA || r.Bytes != len(bodies[f.Target]) || !validHex(r.ID, 64) || !recordedTrue(r.SameID) || !recordedTrue(r.SameBuffer) || !recordedTrue(r.Reviewed) {
			t.Fatal("notice4 current readback not bound to reviewed payload")
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
	if json.Unmarshal(rawPins["bundle"], &bundle) != nil || len(bundle.Targets) != 4 {
		t.Fatal("notice4 bundle differs")
	}
	files := map[string]bool{}
	names := map[string]bool{}
	for _, target := range bundle.Targets {
		if names[target.Name] || len(target.Files) != 7 {
			t.Fatal("notice4 target membership differs")
		}
		names[target.Name] = true
		for _, f := range target.Files {
			key := target.Name + "/" + f.Path
			if files[key] || bodies[key] == nil || string(bodies[key]) != f.Content || travel3Digest([]byte(f.Content)) != f.SHA {
				t.Fatal("notice4 actual buffer differs")
			}
			files[key] = true
		}
	}
	if len(files) != 28 {
		t.Fatal("notice4 omitted actual file")
	}
	var discovery struct {
		Found  int                `json:"found_count"`
		Errors int                `json:"parse_error_count"`
		Called *bool              `json:"model_called"`
		Exited *bool              `json:"process_tree_exit_confirmed"`
		Rows   []newSkillMetadata `json:"found"`
	}
	if json.Unmarshal(rawPins["discovery"], &discovery) != nil || discovery.Found != 4 || discovery.Errors != 0 || !recordedFalse(discovery.Called) || !recordedTrue(discovery.Exited) || len(discovery.Rows) != 4 {
		t.Fatal("notice4 fresh bounded discovery differs")
	}
	found := map[string]bool{}
	for _, r := range discovery.Rows {
		if !names[r.Name] || found[r.Name] || !recordedTrue(r.Central) || !recordedTrue(r.EnabledPresent) || !recordedTrue(r.Enabled) || !recordedTrue(r.Description) {
			t.Fatal("notice4 discovery row differs")
		}
		found[r.Name] = true
	}
	var app struct {
		Actor        string `json:"consumer_job"`
		Applied      *bool  `json:"all_scenario_meanings_applied"`
		Files        int    `json:"instruction_files"`
		Bytes        int    `json:"instruction_bytes"`
		Real         int    `json:"actual_current_workflow_decisions"`
		Synthetic    int    `json:"synthetic_decisions"`
		Calls        *int   `json:"external_source_or_service_or_payment_operations"`
		Instructions []struct {
			Target string `json:"target"`
			Bytes  int    `json:"bytes"`
			SHA    string `json:"sha256"`
			Read   *bool  `json:"literal_current_buffer_read_in_actual_parent_turn"`
		} `json:"instructions"`
		Decisions []notice4Case `json:"decisions"`
	}
	if json.Unmarshal(rawPins["application"], &app) != nil || app.Actor != next3Actor || !recordedTrue(app.Applied) || app.Files != 8 || app.Bytes != 13040 || app.Real != 2 || app.Synthetic != 6 || !recordedCount(app.Calls, 0) || len(app.Instructions) != 8 || len(app.Decisions) != 8 {
		t.Fatal("notice4 explicit real/synthetic application differs")
	}
	ins := map[string]bool{}
	nbytes := 0
	for _, r := range app.Instructions {
		b := bodies[r.Target]
		if b == nil || ins[r.Target] || !recordedTrue(r.Read) || travel3Digest(b) != r.SHA || len(b) != r.Bytes || !(strings.HasSuffix(r.Target, "/SKILL.md") || strings.HasSuffix(r.Target, "/references/guide.md")) {
			t.Fatal("notice4 applied instruction differs")
		}
		ins[r.Target] = true
		nbytes += len(b)
	}
	if nbytes != 13040 {
		t.Fatal("notice4 instruction byte sum differs")
	}
	if err := validateNotice4Cases(app.Decisions); err != nil {
		t.Fatal(err)
	}
	var structure struct {
		Exit   *int   `json:"exit"`
		Output string `json:"stdout"`
		Writes *int   `json:"additional_central_writes"`
		Bundle struct {
			File string `json:"file"`
			SHA  string `json:"sha256"`
		} `json:"bundle"`
	}
	if json.Unmarshal(rawPins["structure"], &structure) != nil || !recordedCount(structure.Exit, 0) || !recordedCount(structure.Writes, 0) || structure.Output != "PASS: 4 explicit skills; structure, descriptions, references, hashes, scenario pairs; in-memory bundle only; no deployment implied\n" || structure.Bundle.File != wantPins["bundle"] || structure.Bundle.SHA != travel3Digest(rawPins["bundle"]) {
		t.Fatal("notice4 actual buffer structure check absent")
	}
	var precreation struct {
		CurrentAbsence *bool  `json:"current_absence_asserted"`
		Revalidated    *bool  `json:"mutation_revalidated_before_CreateNew"`
		ExtraCalls     *int   `json:"new_Create_calls_by_publication"`
		Call           string `json:"successful_native_creation_call"`
		Response       struct {
			Qualified *bool  `json:"qualified"`
			Created   *int   `json:"created_files"`
			Files     int    `json:"file_count"`
			Manifest  string `json:"manifest_sha256"`
			Request   string `json:"request_sha256"`
			General   *bool  `json:"general_access_granted"`
		} `json:"response_projection"`
	}
	if json.Unmarshal(rawPins["precreation"], &precreation) != nil || !recordedFalse(precreation.CurrentAbsence) || !recordedTrue(precreation.Revalidated) || !recordedCount(precreation.ExtraCalls, 0) || precreation.Call != notice4Call || !recordedTrue(precreation.Response.Qualified) || !recordedCount(precreation.Response.Created, 0) || precreation.Response.Files != 28 || precreation.Response.Manifest != notice4Manifest || precreation.Response.Request != notice4Request || !recordedFalse(precreation.Response.General) {
		t.Fatal("notice4 historical qualification cannot become current absence or new creation")
	}
	result := map[string]cohortLedgerRow{}
	for name := range names {
		var source struct {
			ID     string `json:"source_inventory_id"`
			Source struct {
				Repo    string `json:"repository"`
				Commit  string `json:"commit"`
				Path    string `json:"path"`
				SHA     string `json:"sha256"`
				License struct {
					SHA      string `json:"sha256"`
					Scope    string `json:"scope"`
					Accepted *bool  `json:"accepted"`
				} `json:"license"`
			} `json:"source"`
			Profile struct {
				Raw     *bool `json:"raw_full"`
				Unknown *bool `json:"unseen_meanings_remain_unknown"`
			} `json:"primary_read_profile"`
			Copied        *bool `json:"source_original_body_copied"`
			NonAuthorship *bool `json:"source_published_notice_not_independently_verified_authorship"`
		}
		if json.Unmarshal(bodies[name+"/references/source.json"], &source) != nil || source.ID == "" || result[source.ID].ID != "" || source.Source.Repo != "6mile/skills" || source.Source.Commit != "920714f17d81e0a95c7a9bc51faf0a7921cd982d" || !recordedTrue(source.Source.License.Accepted) || source.Source.License.Scope == "" || travel3Digest(bodies[name+"/references/LICENSE.txt"]) != source.Source.License.SHA || !recordedFalse(source.Profile.Raw) || !recordedTrue(source.Profile.Unknown) || !recordedFalse(source.Copied) || !recordedTrue(source.NonAuthorship) {
			t.Fatal("notice4 individually scoped original source differs")
		}
		result[source.ID] = cohortLedgerRow{ID: source.ID, Source: source.Source.Repo, Commit: source.Source.Commit, Path: source.Source.Path, SHA: source.Source.SHA, Disposition: "installed_new_adaptation"}
	}
	var ledger struct {
		Rows []cohortLedgerRow `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	matched := map[string]bool{}
	for _, r := range ledger.Rows {
		if want, ok := result[r.ID]; ok {
			if want != r || matched[r.ID] {
				t.Fatal("notice4 live ledger identity differs")
			}
			matched[r.ID] = true
		}
	}
	if len(matched) != 4 {
		t.Fatal("notice4 live acceptance missing")
	}
	return result
}

func TestNoticeNext4ActualAcceptance(t *testing.T) { validatedNoticeNext4Dispositions(t) }

func TestNoticeNext4RejectsUnboundCurrentAcceptance(t *testing.T) {
	for _, change := range []string{"valid", "reviewer", "review bytes", "extra creation", "current identity missing", "general access", "third party rights", "review model", "review effort", "review suggestion", "review scope missing", "review native", "native stdout", "native revision", "native producer", "creation actor", "created targets", "read actor", "read call", "read manifest", "read request", "read producer", "read revision", "frozen actor", "frozen manifest", "frozen targets", "capture scope", "capture reads", "unknown writer promoted"} {
		t.Run(change, func(t *testing.T) {
			var i notice4Header
			var n notice4Native
			var r notice4Read
			loadPublicReceipt(t, "skills-3080-notice-next4-installation-evidence175645.json", &i)
			loadPublicReceipt(t, "skills-3080-notice-next4-create-evidence175645.json", &n)
			loadPublicReceipt(t, "skills-3080-notice-next4-consume-evidence175645.json", &r)
			yes := true
			switch change {
			case "reviewer":
				i.Frozen.Task = 4033
			case "review bytes":
				i.Frozen.Final = "changed"
			case "extra creation":
				n.Count = 2
			case "current identity missing":
				r.IDs = nil
			case "general access":
				r.General = &yes
			case "third party rights":
				i.ThirdParty = &yes
			case "review model":
				i.Review.Model = "gpt-6.1-sol"
			case "review effort":
				i.Review.Effort = "low"
			case "review suggestion":
				one := 1
				i.Review.Suggestion = &one
			case "review scope missing":
				i.Review.All = nil
			case "review native":
				i.Review.Native[0].Model = "changed"
			case "native stdout":
				n.CLI = "changed"
			case "native revision":
				n.Revision = "changed"
			case "native producer":
				n.Producer = "changed"
			case "creation actor":
				n.Actor = "20261026-214717-todo-3080"
			case "created targets":
				n.Targets = 3
			case "read actor":
				r.Actor = "20261026-214717-todo-3080"
			case "read call":
				r.Call = "different"
			case "read manifest":
				r.Manifest = "different"
			case "read request":
				r.Request = "different"
			case "read producer":
				r.Producer = "different"
			case "read revision":
				r.Revision = "different"
			case "frozen actor":
				i.Frozen.Creator = "20261026-214717-todo-3080"
			case "frozen manifest":
				i.Frozen.Manifest = "different"
			case "frozen targets":
				i.Frozen.Targets = 3
			case "capture scope":
				n.AddedScope = "actual_creation_targets"
			case "capture reads":
				one := 1
				n.CaptureReads = &one
			case "unknown writer promoted":
				r.OSWriter = "proved"
			}
			if (change == "valid") != (validateNotice4Header(i, n, r) == nil) {
				t.Fatal("unbound evidence was accepted or valid evidence rejected")
			}
		})
	}
}

func TestNoticeNext4RetainsRecordedInputsAndDecisions(t *testing.T) {
	for _, change := range []string{"valid", "missing input", "different input", "claimed real payment"} {
		t.Run(change, func(t *testing.T) {
			var app struct {
				Rows []notice4Case `json:"decisions"`
			}
			loadPublicReceipt(t, "skills-3080-notice-next4-application175645.json", &app)
			switch change {
			case "missing input":
				app.Rows[2].Input = ""
			case "different input":
				app.Rows[2].Input = "different amount"
			case "claimed real payment":
				app.Rows[2].Decision = "実サービスで送金を実行した。"
			}
			if (change == "valid") != (validateNotice4Cases(app.Rows) == nil) {
				t.Fatal("recorded input/decision changed while classification stayed the same")
			}
		})
	}
}
