package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPublishedExecutionSnapshotMatchesPinnedEvidenceAndOccurrenceUnits(t *testing.T) {
	var m ExecutionManifest
	raw := loadPublicReceipt(t, "skills-3080-execution-evidence.json", &m)
	var s ExecutionSummary
	loadPublicReceipt(t, "skills-3080-execution-queue-summary.json", &s)
	e, err := loadExecutionEvidence(filepath.Join("..", "..", "docs"), m)
	if err != nil {
		t.Fatal(err)
	}
	mh := sha256.Sum256(raw)
	if bytes.Contains(raw, []byte("\r")) {
		t.Fatal("pinned execution manifest must use canonical LF checkout bytes")
	}
	if s.ManifestSHA != hex.EncodeToString(mh[:]) {
		t.Fatal("queue summary was generated from different manifest bytes; do not mix working CRLF and committed LF")
	}
	if s.State != "complete" || s.CatalogSHA != m.CatalogSHA || s.CatalogSHA != "2d73c9a038a41cc220a9687b935bfa1e0b315b65d9bd6d3e54ddb4111972876c" || m.Records != 73220 || s.Records != m.Records || s.ManifestSHA != hex.EncodeToString(mh[:]) || !evidenceHex(s.QueueSHA, 64) || s.Assessments != len(e.Assessments) || s.Final != len(e.Decisions) {
		t.Fatal("snapshot input or occurrence universe differs")
	}
	var p struct {
		Missing    int `json:"site_entries_without_body"`
		Held       int `json:"execution_held_shapes"`
		Installed  int `json:"installed"`
		Integrated int `json:"integrated"`
		Excluded   int `json:"excluded"`
	}
	loadPublicReceipt(t, "skills-3080-progress.json", &p)
	pending := 0
	for id := range e.Assessments {
		if _, ok := e.Decisions[id]; !ok {
			pending++
		}
	}
	expected := map[string]int{"held_body_unavailable": p.Missing, "held_execution_review": p.Held, "recorded_final_disposition": s.Final, "assessed_needs_current_disposition": pending, "needs_semantic_and_individual_rights_review": s.Records - s.Final - p.Missing - p.Held - pending}
	if !reflect.DeepEqual(s.Stages, expected) || s.Decisions["installed_new_adaptation"] != p.Installed || s.Decisions["integrated_existing"] != p.Integrated || s.Decisions["excluded"] != p.Excluded {
		t.Fatal("current decisions or hold categories were conflated")
	}
	found := false
	for _, a := range m.Artifacts {
		if a.File == "skills-3080-sixmile-received50-evaluation.json" {
			found = a.Rows == 50
		}
	}
	if !found {
		t.Fatal("received cohort missing from the whole-catalog cursor")
	}
}

func TestExecutionArtifactPinIsCheckedBeforeItsRowsAreConsumed(t *testing.T) {
	var m ExecutionManifest
	loadPublicReceipt(t, "skills-3080-execution-evidence.json", &m)
	for _, name := range []string{"changed_hash", "parent_escape", "absolute", "wrong_row_count"} {
		t.Run(name, func(t *testing.T) {
			copy := m
			copy.Artifacts = append([]ExecutionArtifact(nil), m.Artifacts...)
			switch name {
			case "changed_hash":
				copy.Artifacts[0].SHA = "0000000000000000000000000000000000000000000000000000000000000000"
			case "parent_escape":
				copy.Artifacts[0].File = "../" + copy.Artifacts[0].File
			case "absolute":
				copy.Artifacts[0].File = filepath.Join(string(os.PathSeparator), copy.Artifacts[0].File)
			case "wrong_row_count":
				copy.Artifacts[0].Rows++
			}
			if _, err := loadExecutionEvidence(filepath.Join("..", "..", "docs"), copy); err == nil {
				t.Fatal("bad evidence binding accepted")
			}
		})
	}
}
