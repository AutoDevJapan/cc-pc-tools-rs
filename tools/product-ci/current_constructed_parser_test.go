package main

import (
	"cc/product-ci/inputqual"
	"encoding/json"
	"testing"
)

func TestConstructedRecordsUseOfficialParser(t *testing.T) {
	original, err := json.Marshal(map[string]any{"final_review": map[string]any{"candidate": "cbd688871651e46d4821a4535d57eaa408105ff2", "status": "succeeded", "must": 0, "response_sha256": "synthetic-retained-response-digest"}})
	if err != nil {
		t.Fatal(err)
	}
	records, err := inputqual.AssembleRecords(original)
	if err != nil {
		t.Fatal(err)
	}
	b, err := parseCurrentPRSnapshot(records.Pin, records.Review, records.Acceptance)
	if err != nil || b.RepoID != 1285649124 || b.PR != 995 || b.Head != "cbd688871651e46d4821a4535d57eaa408105ff2" || b.Kind != "secret" || b.RunID != 39384472279 || b.JobIDs[0] != 118500893552 {
		t.Fatal("constructor representation rejected or changed by official parser")
	}
	if _, err := parseCurrentPRSnapshot(records.Pin, records.Acceptance, records.Review); err == nil {
		t.Fatal("constructed evidence roles substituted")
	}
	changed := append(append([]byte(nil), records.Review...), byte(' '))
	if _, err := parseCurrentPRSnapshot(records.Pin, changed, records.Acceptance); err == nil {
		t.Fatal("changed constructed bytes accepted")
	}
	// Assembling arbitrary caller-owned bytes is not an admission route.
	if _, err := readCurrentPRInput("synthetic-unqualified-records"); err == nil {
		t.Fatal("unqualified records became an executable input")
	}
}
