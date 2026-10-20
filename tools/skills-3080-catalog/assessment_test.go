package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Scope is a pinned source's complete entrypoint set, never an adoption cap.
// These checks conserve identities and categories, not semantic quality.
func TestCompleteSourceAssessmentConservesIdentities(t *testing.T) {
	files, err := filepath.Glob("../../docs/skills-3080-*-assessment.json")
	if err != nil || len(files) == 0 {
		t.Fatal("no complete source assessments")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) { checkSourceAssessment(t, file) })
	}
}

func checkSourceAssessment(t *testing.T, file string) {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var a struct {
		Source   string         `json:"source"`
		Commit   string         `json:"commit"`
		Expected int            `json:"entrypoints_expected"`
		Read     int            `json:"entrypoints_read"`
		Counts   map[string]int `json:"candidate_dispositions"`
		Entries  []struct {
			ID          string `json:"inventory_id"`
			Name        string `json:"name"`
			SHA         string `json:"source_sha256"`
			Commit      string `json:"commit"`
			Disposition string `json:"candidate_disposition"`
			Reason      string `json:"reason"`
			Changes     string `json:"required_changes"`
			Installed   bool   `json:"installed_by_todo3080"`
			Integrated  bool   `json:"integrated_by_todo3080"`
			Excluded    bool   `json:"excluded_by_todo3080"`
		} `json:"entries"`
	}
	if err = json.Unmarshal(b, &a); err != nil {
		t.Fatal(err)
	}
	if a.Source == "" || a.Expected < 1 || a.Read != a.Expected || len(a.Entries) != a.Expected {
		t.Fatal("entrypoint coverage is incomplete")
	}
	ids, names, counts := map[string]bool{}, map[string]string{}, map[string]int{}
	for _, e := range a.Entries {
		if e.ID == "" || ids[e.ID] || e.Name == "" || names[e.Name] != "" || e.Commit != a.Commit || len(e.SHA) != 64 || e.Disposition == "" || e.Reason == "" || e.Changes == "" {
			t.Fatal("missing/duplicate assessment identity or reason")
		}
		if e.Installed || e.Integrated || e.Excluded {
			t.Fatal("candidate assessment claimed a central change")
		}
		ids[e.ID] = true
		names[e.Name] = e.SHA
		counts[e.Disposition]++
	}
	if !reflect.DeepEqual(a.Counts, counts) {
		t.Fatal("category counters do not conserve entries")
	}
	files, err := filepath.Glob("../../docs/skills-3080-batch*.json")
	if err != nil {
		t.Fatal(err)
	}
	planned := map[string]bool{}
	for _, f := range files {
		var p struct {
			Repo    string `json:"source_repo"`
			Commit  string `json:"source_commit"`
			Targets []struct {
				Name string `json:"name"`
				SHA  string `json:"source_sha256"`
			} `json:"targets"`
		}
		b, err = os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(b, &p); err != nil {
			t.Fatal(err)
		}
		if p.Repo != a.Source || p.Commit != a.Commit {
			continue
		}
		for _, c := range p.Targets {
			if planned[c.Name] || names[c.Name] != c.SHA {
				t.Fatal("candidate plan repeats a name or loses its reviewed source identity")
			}
			planned[c.Name] = true
		}
	}
	if len(planned) != counts["new_adaptation_candidate"] {
		t.Fatal("authored plans and new-candidate assessment differ")
	}
}
