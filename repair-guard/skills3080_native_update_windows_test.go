package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkills3080Accepted594DistributionSurvivesNativeLogUpdate(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docs", "evidence", "skills-3080-guard-before-native609-16386.json"))
	if err != nil {
		t.Fatal(err)
	}
	var observation struct {
		SHA       string `json:"sha256"`
		Head      string `json:"source_head"`
		Observed  string `json:"actual_installed_sha256"`
		Bytes     int    `json:"bytes"`
		Published string `json:"published_distribution_result"`
		Candidate *bool  `json:"candidate_generated"`
		General   *bool  `json:"general_access_granted"`
		Registry  *bool  `json:"original_registry_recovered"`
	}
	if json.Unmarshal(raw, &observation) != nil || observation.SHA != "16386d55e418ce271cfed923edede1e7472bb93aa691b15409bcdb99753b9899" || observation.Head != "61de98373f8cb481302394be11d7d75cfef08a52" || observation.Observed != observation.SHA || observation.Bytes != 13315072 || observation.Published != "https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/594#issuecomment-6442689917" || observation.Candidate == nil || *observation.Candidate || observation.General == nil || *observation.General || observation.Registry == nil || *observation.Registry {
		t.Fatal("independently published pre-candidate software observation changed")
	}
	current := skillsProducer{strings.Repeat("7", 64), strings.Repeat("8", 40)}
	for _, tc := range []struct {
		name, sha, head string
		accepted        bool
	}{
		{"published_pair", observation.SHA, observation.Head, true},
		{"same_hash_other_source", observation.SHA, strings.Repeat("a", 40), false},
		{"same_source_unknown_hash", strings.Repeat("b", 64), observation.Head, false},
		{"unknown_pair", strings.Repeat("b", 64), strings.Repeat("a", 40), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := skillsAcceptedProducer(skillsProducer{tc.sha, tc.head}, current); got != tc.accepted {
				t.Errorf("Skills software comparison = %v, want %v", got, tc.accepted)
			}
			if got := official3894InstalledBaseline(tc.sha, tc.head); got != tc.accepted {
				t.Errorf("shared updater software comparison = %v, want %v", got, tc.accepted)
			}
		})
	}
}
