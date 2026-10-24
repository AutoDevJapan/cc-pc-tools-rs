package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func validateNext20Reception(a extra45Assessment, ledger []cohortLedgerRow) error {
	if err := validateReceivedReading(a, next20ReadingSpec); err != nil {
		return err
	}
	known := map[string]extra45Entry{}
	for _, e := range a.Entries {
		known[e.ID] = e
	}
	matched := map[string]bool{}
	installed, integrated, excluded := 0, 0, 0
	for _, row := range ledger {
		e, selected := known[row.ID]
		if !selected {
			continue
		} // The mandatory whole-source union rejects unknown IDs.
		if matched[row.ID] || row.Source != a.Source || row.Commit != e.Commit || row.Path != e.Path || row.SHA != e.SHA {
			return errors.New("next20 reception: live source disposition identity differs")
		}
		matched[row.ID] = true
		switch row.Disposition {
		case "installed_new_adaptation":
			if !e.Installed || e.Integrated || e.Excluded {
				return errors.New("next20 reception: final flag differs from the live ledger")
			}
			installed++
		case "integrated_existing":
			if !e.Integrated || e.Installed || e.Excluded {
				return errors.New("next20 reception: final flag differs from the live ledger")
			}
			integrated++
		case "excluded":
			if !e.Excluded || e.Installed || e.Integrated {
				return errors.New("next20 reception: final flag differs from the live ledger")
			}
			excluded++
		default:
			return errors.New("next20 reception: unfinished decision counted as final")
		}
	}
	for _, e := range a.Entries {
		if !matched[e.ID] && (!x45False(e.Rights.Copy) || e.Installed || e.Integrated || e.Excluded) {
			return errors.New("next20 reception: unaccepted grant or final disposition was added")
		}
	}
	if installed != a.Installed || integrated != a.Integrated || excluded != a.Excluded {
		return errors.New("next20 reception: decision counts differ from the live ledger")
	}
	return nil
}

func TestNext20ReceptionKeepsRawActorsAndMaskedScansDistinct(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"valid", ""},
		{"synthetic_aligned_future_adoption", ""},
		{"synthetic_wrong_adoption", "next20 reception: live source disposition identity differs"},
		{"raw_loses_workflow", "next20 reading: raw primary acquired masking or lost its reader"},
		{"masked_scan_claims_full", "next20 reading: raw support acquired masking/scanning or a wrong reader"},
		{"forged_consistent_actor", "next20 reading: fixed independent identities or reading actors changed"},
		{"unknown_bytes_zero", "next20 reading: unknown support bytes or reading were invented"},
		{"wrong_source_url", "next20 reading: primary identity/state is absent or conflated"},
		{"copy_grant", "next20 reception: unaccepted grant or final disposition was added"},
		{"final_flag", "next20 reception: unaccepted grant or final disposition was added"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a extra45Assessment
			loadPublicReceipt(t, "skills-3080-sixmile-next20-evaluation.json", &a)
			var current struct{ Entries, Excluded []cohortLedgerRow }
			// Explicit tags are needed for the separate excluded_entries property.
			var all struct {
				Entries  []cohortLedgerRow `json:"entries"`
				Excluded []cohortLedgerRow `json:"excluded_entries"`
			}
			loadPublicReceipt(t, "skills-3080-adoptions.json", &all)
			current.Entries, current.Excluded = all.Entries, all.Excluded
			rows := append(append([]cohortLedgerRow(nil), current.Entries...), current.Excluded...)
			yes, no, actor := true, false, "forged-reader"
			switch tc.name {
			case "synthetic_aligned_future_adoption", "synthetic_wrong_adoption":
				// Relationship fixture only, never permission or an actual adoption.
				e := &a.Entries[0]
				e.Installed = true
				e.Rights.Copy = &yes
				a.Installed++
				row := cohortLedgerRow{ID: e.ID, Source: a.Source, Commit: e.Commit, Path: e.Path, SHA: e.SHA, Disposition: "installed_new_adaptation"}
				if tc.name == "synthetic_wrong_adoption" {
					row.Path = "wrong/SKILL.md"
				}
				rows = append(rows, row)
			case "raw_loses_workflow":
				a.Entries[0].Workflow = &no
			case "wrong_source_url":
				a.Entries[0].URL = "https://example.invalid/other"
			case "copy_grant":
				a.Entries[0].Rights.Copy = &yes
			case "final_flag":
				a.Entries[0].Installed = true
			case "forged_consistent_actor":
				e := &a.Entries[0]
				e.ReadBy, e.Rights.ReadBy = &actor, &actor
				for i := range e.Support {
					if x45True(e.Support[i].Read) {
						e.Support[i].ReadBy = &actor
					}
					if x45True(e.Support[i].Scan) {
						e.Support[i].ScanBy = &actor
					}
				}
			case "masked_scan_claims_full", "unknown_bytes_zero":
				changed := false
				for i := range a.Entries {
					for j := range a.Entries[i].Support {
						s := &a.Entries[i].Support[j]
						if tc.name == "masked_scan_claims_full" && x45True(s.Scan) && s.Mask != nil && *s.Mask > 0 {
							s.Read, s.Workflow = &yes, &yes
							changed = true
						} else if tc.name == "unknown_bytes_zero" && !x45True(s.Saved) {
							zero := 0
							s.Bytes = &zero
							changed = true
						}
						if changed {
							break
						}
					}
					if changed {
						break
					}
				}
				if !changed {
					t.Fatal("no actual fixture matched the mutation")
				}
			}
			err := validateNext20Reception(a, rows)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != tc.want {
				t.Fatalf("%s reached another guard: %v", tc.name, err)
			}
		})
	}
}

const receivedNamespaceRows = "55a00af5245009d89fb78ee476515539fd99f4df9e5454a4bf8bfecd6fab8764"
const receivedExpandedRights = "177b90aa2a8caded4582c0391be8fd3af1e7ce8642121bdfc4711b8d77e3928e"
const receivedRightsDefinitions = "dc66ef51adca451474aa419a4e1c76a0c59dbf78c7bc267f069b92bc2cc58afb"
const receivedCorrectionTargets = "9ef66dd80771a1bf4e47a4c86587dcebb8e521ce8341562946a1ab271af1f488"

// Anchors come from original3565/3566 definitions/order and the earlier coded
// snapshot, not from the current expanded public fields being validated.
type publicReadingRelations struct {
	Entries []struct {
		ID        string         `json:"inventory_id"`
		Path      string         `json:"path"`
		SHA       string         `json:"source_sha256"`
		Namespace string         `json:"reader_reference_namespace"`
		Row       *int           `json:"reader_reference_row_number"`
		Ordinal   *int           `json:"reader_one_based_ordinal"`
		Rights    map[string]any `json:"individual_rights_review"`
	} `json:"entries"`
	Namespaces []struct {
		Todo int `json:"reader_todo"`
		Base int `json:"reference_base"`
		Rows []struct {
			Number  int    `json:"reference_number"`
			Ordinal int    `json:"one_based_ordinal"`
			ID      string `json:"inventory_id"`
			Path    string `json:"path"`
			SHA     string `json:"primary_sha256"`
		} `json:"rows"`
	} `json:"reader_reference_namespaces"`
	Definitions map[string]any `json:"reader_rights_definitions"`
	Corrections []string       `json:"parent_policy_corrections"`
	Provenance  []struct {
		Number   int               `json:"correction_number"`
		IDs      []json.RawMessage `json:"inventory_ids"`
		Resolved string            `json:"resolved_wording"`
	} `json:"parent_policy_correction_provenance"`
}

func validatePublicReadingRelations(a publicReadingRelations) error {
	if len(a.Entries) != 45 || len(a.Namespaces) != 2 || len(a.Corrections) != 13 || len(a.Provenance) != 13 {
		return errors.New("public relations: fixed scope differs")
	}
	byID := map[string]int{}
	for i, e := range a.Entries {
		if e.ID == "" {
			return errors.New("public relations: source identity absent")
		}
		if _, ok := byID[e.ID]; ok {
			return errors.New("public relations: source identity repeated")
		}
		byID[e.ID] = i
	}
	var namespaceLines []string
	seen := map[string]bool{}
	for _, n := range a.Namespaces {
		if (n.Todo != 3565 && n.Todo != 3566) || len(n.Rows) != 20 || n.Base != map[int]int{3565: 1, 3566: 0}[n.Todo] {
			return errors.New("public relations: source namespace/base differs")
		}
		for i, row := range n.Rows {
			idx, ok := byID[row.ID]
			if !ok || seen[row.ID] {
				return errors.New("public relations: namespace source missing or repeated")
			}
			e := a.Entries[idx]
			if row.Number != i+n.Base || row.Ordinal != i+1 || e.Path != row.Path || e.SHA != row.SHA || e.Namespace != fmt.Sprint(n.Todo) || e.Row == nil || *e.Row != row.Number || e.Ordinal == nil || *e.Ordinal != row.Ordinal {
				return errors.New("public relations: source row and entry disagree")
			}
			seen[row.ID] = true
			namespaceLines = append(namespaceLines, fmt.Sprintf("%d\t%d\t%d\t%d\t%s\t%s\t%s\n", n.Todo, n.Base, row.Number, row.Ordinal, row.ID, row.Path, row.SHA))
		}
	}
	if len(seen) != 40 || x45Digest(namespaceLines) != receivedNamespaceRows {
		return errors.New("public relations: source namespace differs from received order")
	}
	for _, e := range a.Entries {
		if e.Namespace != "" && !seen[e.ID] {
			return errors.New("public relations: entry has no source namespace row")
		}
	}
	bare := regexp.MustCompile(`\b(?:S1|N1|U1)\b|top\.shared_rights`)
	var rightsLines []string
	for _, e := range a.Entries {
		values := map[string]any{}
		for _, key := range []string{"historical_reader_scope", "not_covered", "unconfirmed"} {
			if v, ok := e.Rights[key]; ok {
				b, err := json.Marshal(v)
				if err != nil {
					return err
				}
				if bare.Match(b) {
					return errors.New("public relations: current rights terms are unresolved")
				}
				values[key] = v
			}
		}
		if _, expanded := e.Rights["historical_reader_shorthand"]; expanded {
			b, err := json.Marshal(values)
			if err != nil {
				return err
			}
			rightsLines = append(rightsLines, e.ID+"\t"+string(b)+"\n")
		}
	}
	if len(rightsLines) != 24 || x45Digest(rightsLines) != receivedExpandedRights {
		return errors.New("public relations: expanded rights differ from received definitions")
	}
	defs := map[string]any{"reader3565_shared_rights": a.Definitions["reader3565_shared_rights"], "reader3566": a.Definitions["reader3566"]}
	b, err := json.Marshal(defs)
	if err != nil {
		return err
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != receivedRightsDefinitions {
		return errors.New("public relations: original rights definitions changed")
	}
	var targetLines []string
	for i, p := range a.Provenance {
		if p.Number != i+1 || p.Resolved != a.Corrections[i] {
			return errors.New("public relations: correction provenance differs")
		}
		ids := []string{}
		for _, raw := range p.IDs {
			var id string
			if json.Unmarshal(raw, &id) != nil || id == "" {
				return errors.New("public relations: null or empty correction target")
			}
			if _, ok := byID[id]; !ok {
				return errors.New("public relations: correction target is unknown")
			}
			ids = append(ids, id)
		}
		sort.Strings(ids)
		targetLines = append(targetLines, fmt.Sprintf("%d\t%s\n", p.Number, strings.Join(ids, "\t")))
	}
	if x45Digest(targetLines) != receivedCorrectionTargets {
		return errors.New("public relations: correction targets differ from received mapping")
	}
	if !strings.Contains(a.Corrections[4], "is not an exclusion") || !strings.Contains(a.Corrections[4], "All candidates remain accounted for") || !strings.Contains(a.Corrections[5], "rights question is needed now") || !strings.Contains(a.Corrections[6], "not an exclusion") {
		return errors.New("public relations: original owner-scope constraints were weakened")
	}
	return nil
}

func TestPublicReadingRelationsPreserveSourceOrderRightsAndTargets(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"valid", ""},
		{"wrong_base", "public relations: source namespace/base differs"},
		{"entry_row", "public relations: source row and entry disagree"},
		{"unresolved_rights", "public relations: current rights terms are unresolved"},
		{"changed_rights", "public relations: expanded rights differ from received definitions"},
		{"changed_legend", "public relations: original rights definitions changed"},
		{"null_target", "public relations: null or empty correction target"},
		{"unknown_target", "public relations: correction target is unknown"},
		{"known_target_swap", "public relations: correction targets differ from received mapping"},
		{"global_target_added", "public relations: correction targets differ from received mapping"},
		{"scope_weakened", "public relations: original owner-scope constraints were weakened"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a publicReadingRelations
			loadPublicReceipt(t, "skills-3080-sixmile-next45-evaluation.json", &a)
			switch tc.name {
			case "wrong_base":
				a.Namespaces[1].Base = 1
			case "entry_row":
				v := 99
				a.Entries[0].Row = &v
			case "unresolved_rights":
				a.Entries[0].Rights["unconfirmed"] = "U1"
			case "changed_rights":
				for i := range a.Entries {
					if _, ok := a.Entries[i].Rights["historical_reader_shorthand"]; ok {
						a.Entries[i].Rights["unconfirmed"] = "different"
						break
					}
				}
			case "changed_legend":
				a.Definitions["reader3566"].(map[string]any)["S1"] = "different"
			case "null_target":
				a.Provenance[0].IDs = []json.RawMessage{json.RawMessage("null")}
			case "unknown_target":
				a.Provenance[0].IDs = []json.RawMessage{json.RawMessage(`"unknown"`)}
			case "known_target_swap", "global_target_added":
				id, err := json.Marshal(a.Entries[0].ID)
				if err != nil {
					t.Fatal(err)
				}
				if tc.name == "known_target_swap" {
					a.Provenance[3].IDs = []json.RawMessage{id}
				} else {
					a.Provenance[0].IDs = []json.RawMessage{id}
				}
			case "scope_weakened":
				a.Corrections[4] = "No current use alone does not final-exclude."
				a.Provenance[4].Resolved = a.Corrections[4]
			}
			err := validatePublicReadingRelations(a)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != tc.want {
				t.Fatalf("%s reached another guard: %v", tc.name, err)
			}
		})
	}
}

const receivedNext20Namespace = "f9c638e1798583f8bd243f7305be6d13181d341f6b8ea49c7db1fbc45ac7dab0"
const receivedNext20DefaultScope = "69211d9a637807bb8cfc4329d8f7d3a664104d9fef8a3acdc773472ad914836e"

type next20Relations struct {
	Entries []struct {
		ID          string `json:"inventory_id"`
		Path        string `json:"path"`
		SHA         string `json:"source_sha256"`
		Namespace   string `json:"reader_reference_namespace"`
		Row         *int   `json:"reader_original_row"`
		Disposition string `json:"candidate_disposition"`
		Installed   bool   `json:"installed_by_todo3080"`
		Integrated  bool   `json:"integrated_by_todo3080"`
		Excluded    bool   `json:"excluded_by_todo3080"`
		Rights      struct {
			Scope  string `json:"historical_reader_scope"`
			Intake string `json:"reader_scope_shorthand_at_intake"`
		} `json:"individual_rights_review"`
		Historical struct {
			NotOwner  *bool  `json:"not_owner_policy"`
			NotFinal  *bool  `json:"not_final_disposition"`
			Namespace string `json:"reader_reference_namespace"`
			Row       *int   `json:"reader_original_row"`
		} `json:"historical_reader_proposal"`
	} `json:"entries"`
	Namespaces []struct {
		Todo int `json:"reader_todo"`
		Rows []struct {
			Row  int    `json:"row"`
			ID   string `json:"inventory_id"`
			Path string `json:"path"`
			SHA  string `json:"primary_sha256"`
		} `json:"rows"`
	} `json:"reader_namespaces"`
	Definitions struct {
		Default string `json:"default"`
	} `json:"reader_scope_definitions"`
	Corrections []string `json:"parent_policy_corrections"`
}

func validateNext20Relations(a next20Relations) error {
	if len(a.Entries) != 20 || len(a.Namespaces) != 1 || a.Namespaces[0].Todo != 3601 || len(a.Namespaces[0].Rows) != 20 || len(a.Corrections) < 3 {
		return errors.New("next20 relations: fixed source namespace differs")
	}
	h := sha256.Sum256([]byte(a.Definitions.Default))
	if hex.EncodeToString(h[:]) != receivedNext20DefaultScope {
		return errors.New("next20 relations: original default scope definition differs")
	}
	known := map[string]int{}
	for i, e := range a.Entries {
		if _, ok := known[e.ID]; ok || e.ID == "" {
			return errors.New("next20 relations: repeated source identity")
		}
		known[e.ID] = i
	}
	var lines []string
	for i, row := range a.Namespaces[0].Rows {
		idx, ok := known[row.ID]
		if !ok {
			return errors.New("next20 relations: namespace entry is absent")
		}
		e := a.Entries[idx]
		if row.Row != i+1 || e.Path != row.Path || e.SHA != row.SHA || e.Namespace != "3601" || e.Row == nil || *e.Row != row.Row || e.Historical.Namespace != "3601" || e.Historical.Row == nil || *e.Historical.Row != row.Row {
			return errors.New("next20 relations: entry and source row disagree")
		}
		if !recordedTrue(e.Historical.NotOwner) || !recordedTrue(e.Historical.NotFinal) {
			return errors.New("next20 relations: reader proposal was promoted to owner policy or final decision")
		}
		if !e.Installed && !e.Integrated && !e.Excluded && e.Disposition != "assessed_pending_parent_final_disposition" {
			return errors.New("next20 relations: unfinished candidate was relabelled as a final disposition")
		}
		if strings.HasPrefix(e.Rights.Scope, "default") || e.Rights.Scope == "" || (strings.HasPrefix(e.Rights.Intake, "default") && !strings.Contains(e.Rights.Scope, a.Definitions.Default)) {
			return errors.New("next20 relations: current scope shorthand is unresolved")
		}
		lines = append(lines, fmt.Sprintf("3601\t1\t%d\t%s\t%s\t%s\n", row.Row, row.ID, row.Path, row.SHA))
	}
	if x45Digest(lines) != receivedNext20Namespace {
		return errors.New("next20 relations: namespace differs from original reader order")
	}
	if !strings.Contains(a.Corrections[0], "is not an exclusion") || !strings.Contains(a.Corrections[0], "All candidates remain accounted for") || !strings.Contains(a.Corrections[2], "rights question is needed now") {
		return errors.New("next20 relations: original owner-scope constraints were weakened")
	}
	return nil
}

func TestNext20PublicRelationsAreStandaloneAndKeepReaderProposalsSeparate(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"valid", ""}, {"wrong_row", "next20 relations: entry and source row disagree"}, {"scope_unresolved", "next20 relations: current scope shorthand is unresolved"}, {"definition_changed", "next20 relations: original default scope definition differs"}, {"policy_flag_missing", "next20 relations: reader proposal was promoted to owner policy or final decision"}, {"final_label", "next20 relations: unfinished candidate was relabelled as a final disposition"}, {"scope_weakened", "next20 relations: original owner-scope constraints were weakened"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var a next20Relations
			loadPublicReceipt(t, "skills-3080-sixmile-next20-evaluation.json", &a)
			switch tc.name {
			case "wrong_row":
				v := 99
				a.Entries[0].Row = &v
			case "scope_unresolved":
				a.Entries[0].Rights.Scope = "default"
			case "definition_changed":
				a.Definitions.Default = "changed"
			case "policy_flag_missing":
				a.Entries[0].Historical.NotOwner = nil
			case "final_label":
				a.Entries[0].Disposition = "excluded"
			case "scope_weakened":
				a.Corrections[0] = "No current use alone is not enough."
			}
			err := validateNext20Relations(a)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || err.Error() != tc.want {
				t.Fatalf("%s reached another guard: %v", tc.name, err)
			}
		})
	}
}

func TestBodyAliasTriageDoesNotBecomeSemanticReadingOrFinalDisposition(t *testing.T) {
	var s struct {
		Catalog    string `json:"catalog_sha256"`
		Rows       int    `json:"catalog_occurrences"`
		Available  int    `json:"available_body_occurrences"`
		Raw        int    `json:"unique_raw_bodies"`
		ValidRaw   int    `json:"valid_normalized_raw_bodies"`
		FailedRaw  int    `json:"non_utf8_raw_bodies"`
		Valid      int    `json:"valid_normalized_occurrences"`
		Failed     int    `json:"non_utf8_occurrences"`
		Held       int    `json:"unavailable_or_held_occurrences"`
		Remaining  *int   `json:"remaining_unmapped_occurrences"`
		Mapped     *bool  `json:"all_available_occurrences_mapped"`
		Normalized *bool  `json:"all_available_bodies_normalized"`
		Semantic   *int   `json:"semantic_primary_read_delta"`
		Final      *int   `json:"final_disposition_delta"`
		Central    *int   `json:"central_read_write"`
		Executed   *bool  `json:"source_code_executed"`
		Fetch      *int   `json:"external_fetches"`
		HeldFetch  *int   `json:"held647_fetches"`
		BodyText   *int   `json:"raw_source_bodies_in_this_public_artifact"`
		Leads      int    `json:"pending_alias_leads"`
		Caches     []struct {
			Name    string `json:"name"`
			SHA     string `json:"sha256"`
			Bytes   int    `json:"bytes"`
			Records int    `json:"records"`
		} `json:"cache_files"`
		Failures []struct {
			ID     string `json:"inventory_id"`
			Reason string `json:"reason"`
		} `json:"failed_rows"`
		Matches []struct {
			ID   string `json:"inventory_id"`
			Body struct {
				Raw string `json:"raw_sha256"`
				SHA string `json:"normalized_body_sha256"`
			} `json:"body"`
			Canonical []managedMetadataAdoption `json:"canonical_adoptions"`
			Pending   *bool                     `json:"header_and_support_comparison_pending"`
			Rights    *bool                     `json:"individual_rights_not_accepted"`
			Final     *bool                     `json:"final_disposition_applied"`
		} `json:"matches"`
	}
	loadPublicReceipt(t, "skills-3080-body-alias-scan.json", &s)
	expectedCaches := map[string]struct {
		SHA            string
		Bytes, Records int
	}{
		"catalog-body-digest-cache123649.jsonl":   {"2f42ed5598c61103dc245f48c7ad38e61006264c8ce0c6f79a532cd0ffa066f5", 11523029, 28870},
		"catalog-body-segment2-cache123649.jsonl": {"6a74e50d4b7efb49c3ea085b03d79005638aab48f18e73863a46094df2a72c7c", 8584912, 20012},
		"catalog-body-segment3-cache123649.jsonl": {"9cfe970f019294197383953d7ca154f0de897e0bbea9bb549f5896b0284ab367", 7662902, 17858},
	}
	cacheSeen := map[string]bool{}
	records := 0
	for _, c := range s.Caches {
		expected, ok := expectedCaches[c.Name]
		if !ok || cacheSeen[c.Name] || c.SHA != expected.SHA || c.Bytes != expected.Bytes || c.Records != expected.Records {
			t.Fatal("machine cache identity differs from the independent completed segments")
		}
		cacheSeen[c.Name] = true
		records += c.Records
	}
	if len(cacheSeen) != 3 || records != s.Raw {
		t.Fatal("cache rows do not cover the stated raw bodies")
	}
	if s.Catalog != "2d73c9a038a41cc220a9687b935bfa1e0b315b65d9bd6d3e54ddb4111972876c" || s.Rows != 73220 || s.Available != 72145 || s.Raw != 66740 || s.ValidRaw+s.FailedRaw != s.Raw || s.ValidRaw != 66713 || s.FailedRaw != 27 || s.Valid != 72118 || s.Failed != 27 || s.Valid+s.Failed != s.Available || s.Available+s.Held != s.Rows || s.Held != 1075 || !recordedTrue(s.Mapped) || !recordedFalse(s.Normalized) || s.Leads != 8 || len(s.Matches) != 8 || len(s.Failures) != 27 || !recordedFalse(s.Executed) {
		t.Fatal("body triage units, failures or claim scope differ")
	}
	for _, v := range []*int{s.Remaining, s.Semantic, s.Final, s.Central, s.Fetch, s.HeldFetch, s.BodyText} {
		if v == nil || *v != 0 {
			t.Fatal("machine triage acquired an unrecorded/semantic/final/operational claim")
		}
	}
	entries, _ := managedMetadataSources(t)
	byID := map[string]string{}
	for _, e := range entries {
		byID[e.ID] = e.Target
	}
	seen := map[string]bool{}
	for _, m := range s.Matches {
		if m.ID == "" || seen[m.ID] || !validHex(m.Body.Raw, 64) || !validHex(m.Body.SHA, 64) || len(m.Canonical) != 1 || !recordedTrue(m.Pending) || !recordedTrue(m.Rights) || !recordedFalse(m.Final) {
			t.Fatal("alias lead was duplicated, unbound or promoted")
		}
		seen[m.ID] = true
		for _, c := range m.Canonical {
			if c.ID == m.ID || byID[c.ID] != c.Target || c.Target == "" {
				t.Fatal("alias canonical is not an actual adopted/integrated source")
			}
		}
	}
	for _, f := range s.Failures {
		if f.ID == "" || seen[f.ID] || f.Reason != "non_utf8_body_pending" {
			t.Fatal("failed source was dropped, duplicated or treated as normalized")
		}
		seen[f.ID] = true
	}
}
