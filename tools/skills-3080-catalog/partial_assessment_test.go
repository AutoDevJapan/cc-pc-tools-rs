package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type partialEntry struct {
	ID         string `json:"inventory_id"`
	Path       string `json:"path"`
	Name       string `json:"name"`
	Commit     string `json:"commit"`
	SHA        string `json:"source_sha256"`
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
func validatePartial(a partialAssessment, ledger []partialAdoption, source, commit string, expected int) error {
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
		Entries []partialAdoption `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	var p struct {
		Repos []struct {
			Source string `json:"requested"`
			Commit string `json:"commit"`
			Count  int    `json:"skill_files"`
		} `json:"fixed_public_repos"`
	}
	loadPublicReceipt(t, "skills-3080-progress.json", &p)
	matches := 0
	for _, r := range p.Repos {
		if r.Source == a.Source {
			matches++
			if err := validatePartial(a, ledger.Entries, r.Source, r.Commit, r.Count); err != nil {
				t.Fatal(err)
			}
		}
	}
	if matches != 1 {
		t.Fatal("partial assessment has no unique pinned source")
	}
}

func TestPartialAssessmentRejectsLostPathsAndUnrecordedChanges(t *testing.T) {
	for _, kind := range []string{"valid", "overlap", "lost_pending", "duplicate_id", "unrecorded_integration", "wrong_source_hash", "valid_integration"} {
		t.Run(kind, func(t *testing.T) {
			e := partialEntry{ID: "github:one", Path: "one/SKILL.md", Name: "one", Commit: strings.Repeat("a", 40), SHA: strings.Repeat("b", 64), Read: true}
			a := partialAssessment{Source: "example/skills", Commit: e.Commit, Expected: 2, Read: 1, Unread: 1, Entries: []partialEntry{e}, Pending: []string{"two/SKILL.md"}}
			var rows []partialAdoption
			switch kind {
			case "overlap":
				a.Pending[0] = e.Path
			case "lost_pending":
				a.Pending, a.Unread, a.Expected = nil, 0, 1
			case "duplicate_id":
				e.Path, e.Name = "two/SKILL.md", "two"
				a.Entries, a.Pending, a.Read, a.Unread = append(a.Entries, e), nil, 2, 0
			case "unrecorded_integration", "wrong_source_hash", "valid_integration":
				a.Entries[0].Integrated, a.Integrated = true, 1
				if kind != "unrecorded_integration" {
					rows = []partialAdoption{{ID: e.ID, Source: a.Source, Commit: e.Commit, Path: e.Path, SHA: e.SHA, Disposition: "integrated_existing"}}
					if kind == "wrong_source_hash" {
						rows[0].SHA = strings.Repeat("c", 64)
					}
				}
			}
			err := validatePartial(a, rows, "example/skills", strings.Repeat("a", 40), 2)
			wantError := kind != "valid" && kind != "valid_integration"
			if (err != nil) != wantError {
				t.Fatalf("source conservation detection: %s: %v", kind, err)
			}
		})
	}
}

type latestDraftReview struct {
	Todo      int    `json:"todo"`
	Head      string `json:"head"`
	Must      *int   `json:"must"`
	SHA       string `json:"raw_sha256"`
	Remaining bool   `json:"remaining_independent_confirmation"`
}

func validateDraftReview(pending bool, r latestDraftReview) error {
	if pending {
		return nil
	}
	if r.Todo < 1 || len(r.Head) != 40 || len(r.SHA) != 64 || r.Must == nil || *r.Must != 0 || r.Remaining {
		return errors.New("draft marked accepted without a completed latest review")
	}
	if _, err := hex.DecodeString(r.SHA); err != nil {
		return err
	}
	_, err := hex.DecodeString(r.Head)
	return err
}

func TestDraftReviewCannotCloseWithAnUnresolvedOrMissingReceipt(t *testing.T) {
	var d struct {
		Pending  bool `json:"review_pending"`
		Reviewed struct {
			Latest latestDraftReview `json:"latest_review"`
		} `json:"reviewed_draft"`
	}
	loadPublicReceipt(t, "skills-3080-mizchi-sql-draft.json", &d)
	if err := validateDraftReview(d.Pending, d.Reviewed.Latest); err != nil {
		t.Fatal(err)
	}
	// Reproduce the prior false flag, including a missing current-version receipt.
	if validateDraftReview(false, latestDraftReview{}) == nil {
		t.Fatal("unreviewed draft accepted")
	}
	zero, one := 0, 1
	good := latestDraftReview{Todo: 1, Head: strings.Repeat("a", 40), Must: &zero, SHA: strings.Repeat("b", 64)}
	if err := validateDraftReview(false, good); err != nil {
		t.Fatal(err)
	}
	for _, must := range []*int{nil, &one} {
		b, _ := json.Marshal(good)
		var bad latestDraftReview
		if err := json.Unmarshal(b, &bad); err != nil {
			t.Fatal(err)
		}
		bad.Must = must
		if validateDraftReview(false, bad) == nil {
			t.Fatal("unfinished review accepted")
		}
	}
}
