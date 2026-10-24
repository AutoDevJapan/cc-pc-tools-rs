package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

var primaryCoverageProjection = regexp.MustCompile(`固定([0-9]+)\+別枠mizchi([0-9]+)\+MengTo全入口([0-9]+)\+6mile部分([0-9]+)=([0-9]+)source paths`)

func validatePrimaryCoverageProjection(definition string, components []int) error {
	matches := primaryCoverageProjection.FindAllStringSubmatch(definition, -1)
	if len(matches) != 1 || len(components) != 5 {
		return errors.New("primary coverage projection is missing or ambiguous")
	}
	for i, want := range components {
		got, err := strconv.Atoi(matches[0][i+1])
		if err != nil || got != want {
			return fmt.Errorf("primary coverage projection differs at component %d", i)
		}
	}
	return nil
}

func TestPrimaryCoverageDescriptionMatchesCanonicalComponentCounts(t *testing.T) {
	var p struct {
		Definition string `json:"primary_coverage_definition"`
		Total      int    `json:"all_fulltext_primary_paths_including_partial_sources"`
		Complete   struct {
			Paths int `json:"case_insensitive_skill_named_paths"`
		} `json:"full_source_body_read_coverage"`
		Mizchi struct {
			Read int `json:"full_body_read"`
		} `json:"mizchi_partial_entrypoint_evaluation"`
		Mengto struct {
			Read int `json:"full_body_read"`
		} `json:"mengto_partial_entrypoint_evaluation"`
		Sixmile struct {
			Read int `json:"full_body_read"`
		} `json:"sixmile_partial_entrypoint_evaluation"`
	}
	loadPublicReceipt(t, "skills-3080-progress.json", &p)
	want := []int{p.Complete.Paths, p.Mizchi.Read, p.Mengto.Read, p.Sixmile.Read, p.Total}
	for _, kind := range []string{"valid", "stale_sixmile", "stale_total", "missing", "ambiguous"} {
		t.Run(kind, func(t *testing.T) {
			reported := p.Definition
			switch kind {
			case "stale_sixmile":
				reported = strings.Replace(reported, "6mile部分"+strconv.Itoa(p.Sixmile.Read), "6mile部分"+strconv.Itoa(p.Sixmile.Read-10), 1)
			case "stale_total":
				reported = strings.Replace(reported, "="+strconv.Itoa(p.Total)+"source paths", "="+strconv.Itoa(p.Total-10)+"source paths", 1)
			case "missing":
				reported = ""
			case "ambiguous":
				reported += reported
			}
			if err := validatePrimaryCoverageProjection(reported, want); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}

type evaluatedResource struct {
	Path     string `json:"path"`
	SHA      string `json:"source_sha256"`
	Saved    bool   `json:"body_saved"`
	Read     bool   `json:"body_read"`
	Method   string `json:"read_method"`
	Executed bool   `json:"executed"`
}

func validHex(s string, n int) bool {
	_, err := hex.DecodeString(s)
	return len(s) == n && err == nil
}

func TestOpenAIEntryEvaluationConservesPinnedPathsNotNames(t *testing.T) {
	var a struct {
		Source     string                              `json:"source"`
		Commit     string                              `json:"commit"`
		Tree       string                              `json:"tree_sha"`
		Expected   int                                 `json:"entrypoints_expected"`
		Read       int                                 `json:"entrypoints_read"`
		Installed  int                                 `json:"installed"`
		Integrated int                                 `json:"integrated"`
		Excluded   int                                 `json:"excluded"`
		Pending    []string                            `json:"pending_entrypoint_paths"`
		Counts     map[string]int                      `json:"candidate_dispositions"`
		Support    struct{ Count, Saved, Unsaved int } `json:"support_totals"`
		Reader     struct {
			NoticePaths  int `json:"notice_paths"`
			NoticeUnique int `json:"notice_unique"`
			Full         int `json:"body_full_output"`
			Diff         int `json:"body_complete_baseline_diff"`
		} `json:"reader"`
		Notices []struct {
			Path   string `json:"path"`
			SHA    string `json:"sha256"`
			Rights bool   `json:"rights_accepted"`
		} `json:"individual_notice_roster"`
		Entries []struct {
			partialEntry
			URL         string              `json:"source_url"`
			Bytes       int                 `json:"bytes"`
			Method      string              `json:"body_read_method"`
			Disposition string              `json:"candidate_disposition"`
			Reason      string              `json:"reason"`
			Changes     string              `json:"required_changes"`
			License     []string            `json:"license_paths"`
			Compared    string              `json:"comparison_limit"`
			Independent bool                `json:"independent_review_complete"`
			SupportRead bool                `json:"support_read_complete"`
			Support     []evaluatedResource `json:"supporting_resources"`
		} `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-openai-entrypoint-evaluation.json", &a)
	if a.Source != "openai/skills" || a.Commit != "49f948faa9258a0c61caceaf225e179651397431" || a.Tree != "224b363928698e143876aef008232ed73d8e6f81" || a.Expected != 44 || a.Read != 44 || len(a.Entries) != 44 || len(a.Pending) != 0 || a.Installed+a.Integrated+a.Excluded != 0 {
		t.Fatal("entrypoint read coverage or fixed source changed")
	}
	notices, uniqueNotices := map[string]bool{}, map[string]bool{}
	for _, n := range a.Notices {
		if n.Path == "" || notices[n.Path] || !validHex(n.SHA, 64) || n.Rights {
			t.Fatal("notice identity or rights claim changed")
		}
		notices[n.Path], uniqueNotices[n.SHA] = true, true
	}
	if len(notices) != 46 || len(uniqueNotices) != 11 || a.Reader.NoticePaths != 46 || a.Reader.NoticeUnique != 11 {
		t.Fatal("notice occurrences and unique texts were conflated")
	}
	ids, paths, supportPaths, counts := map[string]bool{}, []string{}, map[string]bool{}, map[string]int{}
	saved, full, diff := 0, 0, 0
	for _, e := range a.Entries {
		if e.ID == "" || ids[e.ID] || e.Name == "" || !e.Read || !validHex(e.SHA, 64) || e.Bytes < 1 || e.Commit != a.Commit || e.URL != "https://github.com/"+a.Source+"/blob/"+a.Commit+"/"+e.Path || e.Reason == "" || e.Changes == "" || e.Disposition == "" || e.Compared == "" || e.Independent || e.SupportRead || e.Installed || e.Integrated || e.Excluded {
			t.Fatal("read candidate lost identity or claimed installation")
		}
		ids[e.ID], paths = true, append(paths, e.Path)
		switch e.Method {
		case "full_output_read":
			full++
		case "full_curated_base_plus_complete_six_line_diff":
			diff++
		default:
			t.Fatalf("unknown body reading method %q", e.Method)
		}
		counts[e.Disposition]++
		if len(e.License) == 0 {
			t.Fatal("no individual notice scope")
		}
		for _, p := range e.License {
			if !notices[p] {
				t.Fatal("individual license absent from pinned roster")
			}
		}
		for _, s := range e.Support {
			if s.Path == "" || supportPaths[s.Path] || s.Read || s.Executed || s.Saved != validHex(s.SHA, 64) {
				t.Fatal("unread support claimed read, execution or unavailable hash")
			}
			supportPaths[s.Path] = true
			if s.Saved {
				saved++
			}
		}
	}
	// Computed from all case-insensitive SKILL.md paths of the fixed tree,
	// independently of candidate disposition and duplicate names.
	if pathSetSHA(paths) != "536ff6554d0fd7965a9e3bdb767636ee84f064804e99a3115b52ffa559b4fe55" || full != 43 || diff != 1 || a.Reader.Full != full || a.Reader.Diff != diff || !reflect.DeepEqual(counts, a.Counts) {
		t.Fatal("fixed paths, read methods or categories changed")
	}
	if len(supportPaths) != 690 || saved != 485 || a.Support.Count != len(supportPaths) || a.Support.Saved != saved || a.Support.Unsaved != len(supportPaths)-saved {
		t.Fatal("support saved and unread counters differ")
	}
	// Pinned from the complete source tree and content index, independently of
	// the mutable evaluation file. A valid-looking replacement hash is not enough.
	sort.Slice(a.Entries, func(i, j int) bool { return a.Entries[i].Path < a.Entries[j].Path })
	h := sha256.New()
	for _, e := range a.Entries {
		fmt.Fprintf(h, "%s\t%s\t%d\n", e.Path, e.SHA, e.Bytes)
	}
	if hex.EncodeToString(h.Sum(nil)) != "3c13c1ba2e72fc6ad90814e188aa4919c4b9195c5edd1a33773afb0f3d4daf3a" {
		t.Fatal("source path/hash/bytes differ from the pinned corpus")
	}
}

type canonicalAdoption struct {
	partialAdoption
	Target   string `json:"target"`
	Manifest string `json:"manifest"`
}

type duplicateDecision struct {
	partialAdoption
	URL            string `json:"source_url"`
	Bytes          int    `json:"source_body_bytes"`
	OriginalID     string `json:"duplicate_of_inventory_id"`
	OriginalRepo   string `json:"duplicate_of_source_repo"`
	OriginalCommit string `json:"duplicate_of_source_commit"`
	OriginalPath   string `json:"duplicate_of_source_path"`
	Target         string `json:"canonical_existing_target"`
	Manifest       string `json:"canonical_existing_manifest"`
	Type           string `json:"exclusion_type"`
	Reason         string `json:"reason"`
	Scope          string `json:"decision_scope"`
	CopyRights     bool   `json:"copy_license_accepted"`
	Copied         bool   `json:"new_body_copied"`
	Executed       bool   `json:"source_scripts_executed"`
}

func validateDuplicateDecisions(rows []duplicateDecision, adopted []canonicalAdoption) error {
	canonical, seen := map[string]canonicalAdoption{}, map[string]bool{}
	for _, a := range adopted {
		canonical[a.ID] = a
	}
	for _, d := range rows {
		a, ok := canonical[d.OriginalID]
		if !ok || d.ID == "" || seen[d.ID] || canonical[d.ID].ID != "" || d.SHA != a.SHA || d.OriginalRepo != a.Source || d.OriginalCommit != a.Commit || d.OriginalPath != a.Path || d.Target != a.Target || d.Manifest != a.Manifest || d.Bytes < 1 || !validHex(d.SHA, 64) || !validHex(d.Commit, 40) || d.Path == "" || d.Source == "" || d.URL == "" || d.Disposition != "excluded" || d.Type != "exact_body_duplicate_of_existing_catalog_adoption" || d.Reason == "" || d.Scope == "" || d.CopyRights || d.Copied || d.Executed {
			return errors.New("duplicate exclusion lost its canonical source or claimed a new adoption")
		}
		seen[d.ID] = true
	}
	return nil
}

func TestDuplicateExclusionsKeepCanonicalAdoptionsAndSeparateLedgerRows(t *testing.T) {
	var decisions struct {
		Entries []duplicateDecision `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-duplicate-decisions.json", &decisions)
	var ledger struct {
		Entries    []canonicalAdoption `json:"entries"`
		Excluded   []duplicateDecision `json:"excluded_entries"`
		Count      int                 `json:"excluded"`
		Installed  int                 `json:"installed_new"`
		Integrated int                 `json:"integrated"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	var progress struct {
		Installed  int `json:"installed"`
		Integrated int `json:"integrated"`
	}
	loadPublicReceipt(t, "skills-3080-progress.json", &progress)
	if ledger.Installed != progress.Installed || ledger.Integrated != progress.Integrated {
		t.Fatal("adoption rows differ from independently published progress counts")
	}
	var ledgerDuplicates []duplicateDecision
	for _, row := range ledger.Excluded {
		switch row.Type {
		case "exact_body_duplicate_of_existing_catalog_adoption":
			ledgerDuplicates = append(ledgerDuplicates, row)
		case "individual_proprietary_permission_not_evidenced":
			// Validated against its notice in sixmile_assessment_test.go.
		default:
			t.Fatal("unknown exclusion type must not be treated as a duplicate")
		}
	}
	if len(ledger.Entries) != ledger.Installed+ledger.Integrated || ledger.Count != 8 || ledger.Count != len(ledger.Excluded) || len(decisions.Entries) != 7 || !reflect.DeepEqual(decisions.Entries, ledgerDuplicates) {
		t.Fatal("exclusion receipt differs from ledger or was counted as an adoption")
	}
	if err := validateDuplicateDecisions(decisions.Entries, ledger.Entries); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"unknown_canonical", "duplicate", "adopted_id", "wrong_sha", "wrong_path", "wrong_commit", "wrong_repo", "wrong_manifest", "wrong_target", "claimed_copy_license", "claimed_copy", "claimed_execution", "not_excluded"} {
		t.Run(kind, func(t *testing.T) {
			rows := append([]duplicateDecision(nil), decisions.Entries...)
			switch kind {
			case "unknown_canonical":
				rows[0].OriginalID = "missing"
			case "duplicate":
				rows = append(rows, rows[0])
			case "adopted_id":
				rows[0].ID = rows[0].OriginalID
			case "wrong_sha":
				rows[0].SHA = strings.Repeat("e", 64)
			case "wrong_path":
				rows[0].OriginalPath += "changed"
			case "wrong_commit":
				rows[0].OriginalCommit = strings.Repeat("e", 40)
			case "wrong_repo":
				rows[0].OriginalRepo = "other/skills"
			case "wrong_manifest":
				rows[0].Manifest = "other.json"
			case "wrong_target":
				rows[0].Target = "different"
			case "claimed_copy_license":
				rows[0].CopyRights = true
			case "claimed_copy":
				rows[0].Copied = true
			case "claimed_execution":
				rows[0].Executed = true
			case "not_excluded":
				rows[0].Disposition = "installed_new_adaptation"
			}
			if err := validateDuplicateDecisions(rows, ledger.Entries); err == nil {
				t.Fatal("invalid duplicate decision accepted")
			}
		})
	}
}

func TestCoverageSeparatesCompleteSourcesMizchiAndAuthoredDrafts(t *testing.T) {
	var p struct {
		Candidates int `json:"candidate_designs"`
		TotalRead  int `json:"all_fulltext_primary_paths_including_partial_sources"`
		SkillRead  int `json:"total_skill_named_body_read"`
		Coverage   struct {
			Repos  int            `json:"repos"`
			Paths  int            `json:"case_insensitive_skill_named_paths"`
			ByRepo map[string]int `json:"by_repo"`
		} `json:"full_source_body_read_coverage"`
		Mizchi struct {
			Read int `json:"full_body_read"`
		} `json:"mizchi_partial_entrypoint_evaluation"`
		Mengto struct {
			Read int `json:"full_body_read"`
		} `json:"mengto_partial_entrypoint_evaluation"`
		Sixmile struct {
			Read int `json:"full_body_read"`
		} `json:"sixmile_partial_entrypoint_evaluation"`
		Authored struct {
			Additional int      `json:"additional_authored_drafts_not_in_plans"`
			Total      int      `json:"authored_total"`
			Sources    []string `json:"additional_draft_sources"`
		} `json:"candidate_count_definitions"`
	}
	loadPublicReceipt(t, "skills-3080-progress.json", &p)
	sum := 0
	for repo, n := range p.Coverage.ByRepo {
		if strings.Contains(strings.ToLower(repo), "mizchi") || strings.Contains(strings.ToLower(repo), "mengto") || n < 1 {
			t.Fatal("separate partial source double-counted")
		}
		sum += n
	}
	if sum != 137 || len(p.Coverage.ByRepo) != p.Coverage.Repos || p.SkillRead != sum || p.Coverage.Paths != sum || p.Mizchi.Read != 76 || p.Mengto.Read < 127 || p.TotalRead != sum+p.Mizchi.Read+p.Mengto.Read+p.Sixmile.Read {
		t.Fatal("body read coverage does not conserve source paths")
	}
	var sql struct {
		Drafts []any `json:"drafts"`
	}
	loadPublicReceipt(t, "skills-3080-mizchi-sql-draft.json", &sql)
	if p.Authored.Total != p.Candidates+p.Authored.Additional || p.Authored.Additional != len(sql.Drafts)+1 || !reflect.DeepEqual(p.Authored.Sources, []string{"skills-3080-mizchi-sql-draft.json", "skills-3080-mizchi-mutation-draft.json"}) {
		t.Fatal("authored drafts differ from plans plus pending SQL and mutation drafts")
	}
	var designs struct {
		Entries []struct {
			Source     string `json:"source"`
			Read       int    `json:"entrypoints_read"`
			Expected   int    `json:"entrypoints_expected"`
			Installed  bool   `json:"installed"`
			Integrated bool   `json:"integrated"`
			Excluded   bool   `json:"excluded"`
			Writes     int    `json:"central_write"`
		} `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-design-entrypoint-evaluation.json", &designs)
	seen := map[string]bool{}
	for _, d := range designs.Entries {
		if seen[d.Source] || (d.Source != "Nutlope/hallmark" && d.Source != "hirokaji/jp-ui-contracts") || d.Read != 1 || d.Expected != 1 || d.Installed || d.Integrated || d.Excluded || d.Writes != 0 {
			t.Fatal("existing 3048 integration was counted as a new adoption")
		}
		seen[d.Source] = true
	}
	if len(seen) != 2 {
		t.Fatal("design source scope changed")
	}
}

func TestReader3458SupportAndHistoricalReviewMetadata(t *testing.T) {
	var a struct {
		Reader struct {
			SHA       string `json:"raw_sha256"`
			Preserved string `json:"preserved_at"`
			Bodies    int    `json:"body_fulltext_count"`
			Support   int    `json:"support_metadata_count"`
			Full      int    `json:"readme_fulltext_count"`
			Template  int    `json:"readme_description_template_count"`
			Unread    int    `json:"support_nonreadme_unread"`
			Saved     int    `json:"support_nonreadme_saved"`
			Unsaved   int    `json:"support_nonreadme_unsaved"`
		} `json:"reader3458"`
		Entries []struct {
			Path        string              `json:"path"`
			BodyReceipt string              `json:"body_read_receipt_sha256"`
			Recorded    string              `json:"parent_evaluation_recorded_at"`
			Independent bool                `json:"independent_review_complete"`
			Support     []evaluatedResource `json:"supporting_resources"`
		} `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-mizchi-partial-evaluation.json", &a)
	preserved, err := time.Parse(time.RFC3339Nano, a.Reader.Preserved)
	if err != nil {
		t.Fatal(err)
	}
	bodies, support, full, template, unread, saved, unsaved := 0, 0, 0, 0, 0, 0, 0
	for _, e := range a.Entries {
		// No current full evaluation is independently accepted in this snapshot.
		// Historical fixed-head reviews remain separate from that assertion.
		if e.Independent {
			t.Fatal("current evaluation claims unbound acceptance")
		}
		if e.BodyReceipt != a.Reader.SHA {
			continue
		}
		bodies++
		recorded, err := time.Parse(time.RFC3339Nano, e.Recorded)
		if err != nil || recorded.Before(preserved) {
			t.Fatal("evaluation predates parent receipt preservation")
		}
		rootReadme := strings.TrimSuffix(e.Path, "SKILL.md") + "README.md"
		for _, r := range e.Support {
			support++
			if r.Path == rootReadme {
				switch r.Method {
				case "reader_fulltext":
					full++
				case "reader_description_and_exact_template_comparison", "reader_description_template_plus_complete_cheat_sheet_difference":
					template++
				default:
					t.Fatal("unclassified entry README reading method")
				}
			} else {
				if r.Read || r.Executed {
					t.Fatal("unread support claimed reading or execution")
				}
				unread++
				if r.Saved {
					saved++
				} else {
					unsaved++
				}
			}
		}
	}
	if bodies != 15 || support != 111 || full != 9 || template != 6 || unread != 96 || saved != 36 || unsaved != 60 || bodies != a.Reader.Bodies || support != a.Reader.Support || full != a.Reader.Full || template != a.Reader.Template || unread != a.Reader.Unread || saved != a.Reader.Saved || unsaved != a.Reader.Unsaved || support != full+template+unread || unread != saved+unsaved {
		t.Fatal("reader support counters differ from the 15 received entrypoints")
	}
}
