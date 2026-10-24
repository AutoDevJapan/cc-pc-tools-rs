package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProgressConservesObservedCatalogAndCandidateBatches(t *testing.T) {
	var p struct {
		Records    int            `json:"catalog_records"`
		Available  int            `json:"available_body_occurrences"`
		Unique     int            `json:"unique_body_sha256"`
		Duplicates int            `json:"redundant_body_occurrences"`
		Missing    int            `json:"site_entries_without_body"`
		Held       int            `json:"execution_held_shapes"`
		Candidates int            `json:"candidate_designs"`
		Units      map[string]int `json:"units"`
		Repos      []struct {
			Repo   string `json:"requested"`
			Commit string `json:"commit"`
			Skills int    `json:"skill_files"`
		} `json:"fixed_public_repos"`
	}
	b, e := os.ReadFile("../../docs/skills-3080-progress.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &p); e != nil {
		t.Fatal(e)
	}
	units, skills := 0, 0
	repos := map[string]string{}
	for _, n := range p.Units {
		units += n
	}
	for _, r := range p.Repos {
		skills += r.Skills
		repos[strings.ToLower(r.Repo)] = r.Commit
	}
	if units != p.Records || p.Available+p.Missing+p.Held != p.Records || p.Unique+p.Duplicates != p.Available || skills != p.Units["github_skill_entry"] {
		t.Fatal("published catalog counters do not conserve source occurrences")
	}
	files, e := filepath.Glob("../../docs/skills-3080-batch*.json")
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	seen := map[string]bool{}
	for _, f := range files {
		var plan struct {
			Repo    string `json:"source_repo"`
			Commit  string `json:"source_commit"`
			Targets []struct {
				Name string `json:"name"`
			} `json:"targets"`
		}
		b, e = os.ReadFile(f)
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(b, &plan); e != nil {
			t.Fatal(e)
		}
		if repos[strings.ToLower(plan.Repo)] != plan.Commit {
			t.Fatal("candidate commit differs from recorded source tree")
		}
		for _, target := range plan.Targets {
			if seen[target.Name] {
				t.Fatal("cross-batch candidate name collision")
			}
			seen[target.Name] = true
			count++
		}
	}
	for _, target := range directAuthoredDesigns(t) {
		if repos[strings.ToLower(target.Repo)] != target.Commit || seen[target.Name] {
			t.Fatal("direct individual-source design repeats a name or loses the pinned commit")
		}
		seen[target.Name] = true
		count++
	}
	if count != p.Candidates {
		t.Fatal("published candidate count differs from legacy batches plus direct designs")
	}
}
