package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"testing"
)

type partialEntry struct {
	ID         string `json:"inventory_id"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	Commit     string `json:"commit"`
	SHA        string `json:"source_sha256"`
	Blob       string `json:"git_blob"`
	Bytes      int    `json:"bytes"`
	Read       bool   `json:"body_read_complete"`
	Installed  bool   `json:"installed_by_todo3080"`
	Integrated bool   `json:"integrated_by_todo3080"`
	Excluded   bool   `json:"excluded_by_todo3080"`
}

type partialAssessment struct {
	Source     string         `json:"source"`
	Commit     string         `json:"commit"`
	Expected   int            `json:"entrypoints_expected"`
	Read       int            `json:"entrypoints_fulltext_read"`
	Unread     int            `json:"entrypoints_unread"`
	Installed  int            `json:"installed"`
	Integrated int            `json:"integrated"`
	Excluded   int            `json:"excluded"`
	Entries    []partialEntry `json:"entries"`
	Pending    []string       `json:"pending_entrypoint_paths"`
}

type partialAdoption struct {
	ID          string `json:"inventory_id"`
	Source      string `json:"source_repo"`
	Commit      string `json:"source_commit"`
	Path        string `json:"source_path"`
	SHA         string `json:"source_sha256"`
	Disposition string `json:"disposition"`
}

// A read path is not an adoption. Validate the unfinished set as well as the
// deployed subset, against the separately pinned inventory and live ledger.
func pathSetSHA(paths []string) string {
	ordered := append([]string(nil), paths...)
	sort.Strings(ordered)
	h := sha256.Sum256([]byte(strings.Join(ordered, "\n") + "\n"))
	return hex.EncodeToString(h[:])
}

func validatePartial(a partialAssessment, ledger []partialAdoption, source, commit string, expected int, rosterSHA string) error {
	if a.Source != source || a.Commit != commit || a.Expected != expected || expected < 1 || a.Read != len(a.Entries) || a.Unread != len(a.Pending) || a.Read+a.Unread != expected {
		return errors.New("partial source coverage does not match its fixed inventory")
	}
	paths, entries := map[string]bool{}, map[string]partialEntry{}
	for _, e := range a.Entries {
		if e.Path == "" || paths[e.Path] || e.ID == "" || entries[e.ID].ID != "" || e.Name == "" || e.Commit != a.Commit || len(e.SHA) != 64 || !e.Read {
			return errors.New("missing, repeated or unread assessed source")
		}
		paths[e.Path], entries[e.ID] = true, e
	}
	for _, p := range a.Pending {
		if p == "" || paths[p] {
			return errors.New("pending source is repeated or already read")
		}
		paths[p] = true
	}
	roster := make([]string, 0, len(paths))
	for p := range paths {
		roster = append(roster, p)
	}
	if pathSetSHA(roster) != rosterSHA {
		return errors.New("partial path set differs from its pinned complete tree")
	}
	matched := map[string]bool{}
	installed, integrated, excluded := 0, 0, 0
	for _, row := range ledger {
		if row.Source != a.Source {
			continue
		}
		e, ok := entries[row.ID]
		if !ok || matched[row.ID] || row.Path != e.Path || row.Commit != e.Commit || row.SHA != e.SHA {
			return errors.New("adopted source differs from its read assessment")
		}
		matched[row.ID] = true
		switch row.Disposition {
		case "installed_new_adaptation":
			if !e.Installed || e.Integrated || e.Excluded {
				return errors.New("installation flag differs from live disposition")
			}
			installed++
		case "integrated_existing":
			if !e.Integrated || e.Installed || e.Excluded {
				return errors.New("integration flag differs from live disposition")
			}
			integrated++
		case "excluded":
			if !e.Excluded || e.Installed || e.Integrated {
				return errors.New("exclusion flag differs from live disposition")
			}
			excluded++
		default:
			return errors.New("candidate counted as a finished disposition")
		}
	}
	for _, e := range a.Entries {
		if (e.Installed || e.Integrated || e.Excluded) && !matched[e.ID] {
			return errors.New("assessment claims a change absent from the ledger")
		}
	}
	if installed != a.Installed || integrated != a.Integrated || excluded != a.Excluded {
		return errors.New("partial source adoption counters differ from the ledger")
	}
	return nil
}

func TestPartialMizchiAssessmentConservesUnfinishedPathsAndAdoptions(t *testing.T) {
	var a partialAssessment
	loadPublicReceipt(t, "skills-3080-mizchi-partial-evaluation.json", &a)
	var ledger struct {
		Entries  []partialAdoption `json:"entries"`
		Excluded []partialAdoption `json:"excluded_entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	var p struct {
		Partial struct {
			Expected   int  `json:"expected"`
			Read       int  `json:"full_body_read"`
			Unread     int  `json:"unread"`
			Complete   bool `json:"complete"`
			Installed  int  `json:"installed"`
			Integrated int  `json:"integrated"`
			Excluded   int  `json:"excluded"`
		} `json:"mizchi_partial_entrypoint_evaluation"`
		Mengto struct {
			Read int `json:"full_body_read"`
		} `json:"mengto_partial_entrypoint_evaluation"`
		Sixmile struct {
			Read int `json:"full_body_read"`
		} `json:"sixmile_body_read_coverage"`
		TotalRead int `json:"all_fulltext_primary_paths_including_partial_sources"`
		Complete  struct {
			Paths int `json:"case_insensitive_skill_named_paths"`
		} `json:"full_source_body_read_coverage"`
		Repos []struct {
			Source string `json:"requested"`
			Commit string `json:"commit"`
			Count  int    `json:"skill_files"`
		} `json:"fixed_public_repos"`
	}
	loadPublicReceipt(t, "skills-3080-progress.json", &p)
	// Fixed independently of the mutable read/pending classification. Updating
	// this anchor requires a new complete tree, not a same-count path edit.
	const pinnedSHA = "24248fae07401fe08d0d063b61e5c2586f0a2d31c631b38f3cd5e5714f451f7a"
	var roster struct {
		Source string   `json:"source"`
		Commit string   `json:"commit"`
		Tree   string   `json:"tree_sha"`
		SHA    string   `json:"path_set_sha256"`
		Paths  []string `json:"paths"`
	}
	loadPublicReceipt(t, "skills-3080-mizchi-fixed-entrypoints.json", &roster)
	if roster.Source != a.Source || roster.Commit != a.Commit || roster.Tree != "afbd6cabdc055717936a1c454627ccc62050705b" || len(roster.Paths) != 76 || roster.SHA != pinnedSHA || pathSetSHA(roster.Paths) != pinnedSHA {
		t.Fatal("fixed entrypoint roster differs from its independent tree anchor")
	}
	if p.Partial.Expected != a.Expected || p.Partial.Read != a.Read || p.Partial.Unread != a.Unread || p.Partial.Complete != (a.Unread == 0) || p.Partial.Installed != a.Installed || p.Partial.Integrated != a.Integrated || p.Partial.Excluded != a.Excluded || p.TotalRead != p.Complete.Paths+a.Read+p.Mengto.Read+p.Sixmile.Read {
		t.Fatal("progress summary differs from the partial assessment")
	}
	matches := 0
	for _, r := range p.Repos {
		if r.Source == a.Source {
			matches++
			if err := validatePartial(a, append(ledger.Entries, ledger.Excluded...), r.Source, r.Commit, r.Count, pinnedSHA); err != nil {
				t.Fatal(err)
			}
		}
	}
	if matches != 1 {
		t.Fatal("partial assessment has no unique pinned source")
	}
}

func TestPartialAssessmentRejectsLostPathsAndUnrecordedChanges(t *testing.T) {
	for _, kind := range []string{"valid", "overlap", "lost_pending", "same_count_substitution", "duplicate_id", "unrecorded_integration", "wrong_source_hash", "valid_integration", "unassessed_id", "duplicate_row", "wrong_path", "wrong_commit", "unknown_disposition", "other_repo"} {
		t.Run(kind, func(t *testing.T) {
			e := partialEntry{ID: "github:one", Path: "one/SKILL.md", Name: "one", Commit: strings.Repeat("a", 40), SHA: strings.Repeat("b", 64), Read: true}
			a := partialAssessment{Source: "example/skills", Commit: e.Commit, Expected: 2, Read: 1, Unread: 1, Entries: []partialEntry{e}, Pending: []string{"two/SKILL.md"}}
			var rows []partialAdoption
			switch kind {
			case "overlap":
				a.Pending[0] = e.Path
			case "lost_pending":
				a.Pending, a.Unread, a.Expected = nil, 0, 1
			case "same_count_substitution":
				a.Pending[0] = "typo/SKILL.md"
			case "duplicate_id":
				e.Path, e.Name = "two/SKILL.md", "two"
				a.Entries, a.Pending, a.Read, a.Unread = append(a.Entries, e), nil, 2, 0
			case "unrecorded_integration", "wrong_source_hash", "valid_integration", "unassessed_id", "duplicate_row", "wrong_path", "wrong_commit", "unknown_disposition", "other_repo":
				a.Entries[0].Integrated, a.Integrated = true, 1
				if kind != "unrecorded_integration" {
					rows = []partialAdoption{{ID: e.ID, Source: a.Source, Commit: e.Commit, Path: e.Path, SHA: e.SHA, Disposition: "integrated_existing"}}
					if kind == "wrong_source_hash" {
						rows[0].SHA = strings.Repeat("c", 64)
					}
					switch kind {
					case "unassessed_id":
						rows[0].ID = "github:unassessed"
					case "duplicate_row":
						rows = append(rows, rows[0])
					case "wrong_path":
						rows[0].Path = "wrong/SKILL.md"
					case "wrong_commit":
						rows[0].Commit = strings.Repeat("c", 40)
					case "unknown_disposition":
						rows[0].Disposition = "candidate"
					case "other_repo":
						other := rows[0]
						other.Source, other.Path, other.Commit = "other/skills", "different/SKILL.md", "unrelated"
						rows = append(rows, other)
					}
				}
			}
			err := validatePartial(a, rows, "example/skills", strings.Repeat("a", 40), 2, pathSetSHA([]string{"one/SKILL.md", "two/SKILL.md"}))
			wantError := kind != "valid" && kind != "valid_integration" && kind != "other_repo"
			if (err != nil) != wantError {
				t.Fatalf("source conservation detection: %s: %v", kind, err)
			}
		})
	}
}

func TestEveryFinishedDispositionRequiresMatchingLedgerAndAssessment(t *testing.T) {
	for _, disposition := range []string{"installed_new_adaptation", "integrated_existing", "excluded"} {
		for _, kind := range []string{"valid", "flag_only", "missing_flag", "counter_minus_one", "counter_plus_one", "extra_installed_flag", "extra_integrated_flag", "extra_excluded_flag"} {
			if (disposition == "installed_new_adaptation" && kind == "extra_installed_flag") || (disposition == "integrated_existing" && kind == "extra_integrated_flag") || (disposition == "excluded" && kind == "extra_excluded_flag") {
				continue
			}
			t.Run(disposition+"_"+kind, func(t *testing.T) {
				e := partialEntry{ID: "github:one", Path: "one/SKILL.md", Name: "one", Commit: strings.Repeat("a", 40), SHA: strings.Repeat("b", 64), Read: true}
				a := partialAssessment{Source: "example/skills", Commit: e.Commit, Expected: 2, Read: 1, Unread: 1, Entries: []partialEntry{e}, Pending: []string{"two/SKILL.md"}}
				switch disposition {
				case "installed_new_adaptation":
					a.Entries[0].Installed, a.Installed = true, 1
				case "integrated_existing":
					a.Entries[0].Integrated, a.Integrated = true, 1
				case "excluded":
					a.Entries[0].Excluded, a.Excluded = true, 1
				}
				rows := []partialAdoption{{ID: e.ID, Source: a.Source, Commit: e.Commit, Path: e.Path, SHA: e.SHA, Disposition: disposition}}
				switch kind {
				case "flag_only":
					rows = nil
					a.Installed, a.Integrated, a.Excluded = 0, 0, 0
				case "missing_flag":
					a.Entries[0] = e
				case "counter_minus_one", "counter_plus_one":
					delta := -1
					if kind == "counter_plus_one" {
						delta = 1
					}
					switch disposition {
					case "installed_new_adaptation":
						a.Installed += delta
					case "integrated_existing":
						a.Integrated += delta
					case "excluded":
						a.Excluded += delta
					}
				case "extra_installed_flag":
					a.Entries[0].Installed = true
				case "extra_integrated_flag":
					a.Entries[0].Integrated = true
				case "extra_excluded_flag":
					a.Entries[0].Excluded = true
				}
				err := validatePartial(a, rows, a.Source, a.Commit, 2, pathSetSHA([]string{"one/SKILL.md", "two/SKILL.md"}))
				if (err != nil) != (kind != "valid") {
					t.Fatalf("disposition must agree in both directions: %v", err)
				}
			})
		}
	}
}
