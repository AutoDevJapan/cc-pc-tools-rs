package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkills3080CreationPathRetainsAccepted624Software(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docs", "evidence", "skills-3080-guard-before-native-path-eab175645.json"))
	if err != nil {
		t.Fatal(err)
	}
	var observation struct {
		SHA      string `json:"sha256"`
		Head     string `json:"actual_cli_issued_source_revision"`
		Bytes    int    `json:"bytes"`
		PR       int    `json:"previous_software_PR"`
		Merge    string `json:"previous_software_merge"`
		Verified bool   `json:"both_formal_native_Luna_max_and_M_S_N_zero_independently_verified"`
	}
	if json.Unmarshal(raw, &observation) != nil || observation.SHA != "eab69e4fc8e52f2ad6858ef4e8498e8cc6030ac8ce7f788251a1aae476dc5d55" || observation.Head != "034520d84d52f76dcd7dc7ef371910d6acd8ae9f" || observation.Merge != observation.Head || observation.Bytes != 13425152 || observation.PR != 624 || !observation.Verified {
		t.Fatal("independent pre-candidate accepted software observation changed")
	}
	current := skillsProducer{strings.Repeat("7", 64), strings.Repeat("8", 40)}
	for _, tc := range []struct {
		sha, head string
		want      bool
	}{
		{observation.SHA, observation.Head, true},
		{observation.SHA, strings.Repeat("a", 40), false},
		{strings.Repeat("b", 64), observation.Head, false},
		{strings.Repeat("b", 64), strings.Repeat("a", 40), false},
	} {
		if skillsAcceptedProducer(skillsProducer{tc.sha, tc.head}, current) != tc.want || official3894InstalledBaseline(tc.sha, tc.head) != tc.want {
			t.Errorf("accepted software pair %s/%s does not match %v", tc.sha, tc.head, tc.want)
		}
	}
}
