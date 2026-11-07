package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
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
	intersection := len(e.Assessments) - pending
	if s.AssessmentAndFinal != intersection || s.FinalOnly != len(e.Decisions)-intersection || s.AssessmentOrFinal != pending+len(e.Decisions) {
		t.Fatal("saved assessment/final intersection, final-only or union differs from pinned occurrence sets")
	}
	expected := map[string]int{"held_body_unavailable": p.Missing, "held_execution_review": p.Held, "recorded_final_disposition": s.Final, "assessed_needs_current_disposition": pending, "needs_semantic_and_individual_rights_review": s.Records - s.Final - p.Missing - p.Held - pending}
	if !reflect.DeepEqual(s.Stages, expected) || s.Decisions["installed_new_adaptation"] != p.Installed || s.Decisions["integrated_existing"] != p.Integrated || s.Decisions["excluded"] != p.Excluded {
		t.Fatal("current decisions or hold categories were conflated")
	}
	if err := publishedReceivedCursorCoverage(m.Artifacts); err != nil {
		t.Fatal(err)
	}
}

// Every independently registered cohort must participate in the occurrence
// cursor, even if someone also regenerates a self-consistent smaller summary.
func publishedReceivedCursorCoverage(artifacts []ExecutionArtifact) error {
	for _, r := range receivedRegistrations {
		found := 0
		for _, a := range artifacts {
			if a.File == r.Artifact {
				if a.Rows != r.Spec.Counts["candidates"] {
					return fmt.Errorf("received cohort cursor count differs: %s", r.ID)
				}
				found++
			}
		}
		if found != 1 {
			return fmt.Errorf("received cohort absent/repeated in cursor: %s", r.ID)
		}
	}
	// Keep these independently received snapshots in the runtime cursor even
	// if someone regenerates a self-consistent smaller manifest and summary.
	for _, required := range []ExecutionArtifact{
		{File: "skills-3080-original3719-next12-256267-evaluation175645.json", SHA: "5e5a5e2fb6c6e342a72e0bbb37c63c8d03f6b495b48c0b35ad6517293905348c", Rows: 12},
		{File: "skills-3080-original3719-next21-268288-evaluation175645.json", SHA: "54b04b462a806d536fcf352c793e4c91a13783373544caa179b196edd64cbc62", Rows: 21},
		{File: "skills-3080-original3719-next6-250255-evaluation175645.json", SHA: "831104060ccb98ddd92a16aaf92011b6dff2b8fda05b28c7e35868d145a03167", Rows: 6},
		{File: "skills-3080-primaryflow-next16-after195-evaluation175645.json", SHA: "fc5168d8c3d1709b44d6d1dc4850815bf776c281ee6604e742c960a6c4b1c16f", Rows: 16},
		{File: "skills-3080-original3719-next4-246249-evaluation175645.json", SHA: "a92204428795cf0f3bbbb8c33bd458e1e3c549c6e1156de2fcc0bbd3ae1c2aac", Rows: 4},
		{File: "skills-3080-primaryflow-next5-after190-evaluation175645.json", SHA: "0d0a901edf7c43bc71b6f3eb4a0aacf4cbd73ca74006db73b352483a5417c2c5", Rows: 5},
		{File: "skills-3080-original3719-next6-240245-evaluation175645.json", SHA: "c9face778d8c89d4832323ff6dabd508291a905b361c2788b70133460795603e", Rows: 6},
		{File: "skills-3080-primaryflow-next15-evaluation175645.json", SHA: "e0d0d65c02bafd1157ebb03873f7f95dc3b2831cae787824338bbcb63023218b", Rows: 15},
		{File: "skills-3080-source9-next2-7to8-visible-evaluation175645.json", SHA: "d5e2d1972f51d44fbd8b9ed32e50ec17a60f80fb13776de480c15ba357a4bcb3", Rows: 2},
		{File: "skills-3080-original3719-next12-228239-evaluation175645.json", SHA: "c23a189ea75961f03bf43d492f2a872c3711a9a188913536feaea7f78b51e475", Rows: 12},
		{File: "skills-3080-primaryflow-next5-evaluation175645.json", SHA: "59e231295f75004536f33de7d45a3369b3c9b55e5730ac11ab3fb99bf6927f19", Rows: 5},
		{File: "skills-3080-original3719-next8-199206-evaluation175645.json", SHA: "0362991564c4bee6b86edc9b52cc934b94d5b47ef4b142985f39b5ed5d8280b2", Rows: 8},
		{File: "skills-3080-original3719-next3-207209-evaluation175645.json", SHA: "80f9eab15b84bd88006cde9d5a61dc438be2fb83259f9accac271ac057ddd442", Rows: 3},
		{File: "skills-3080-source9-first4-visible-evaluation175645.json", SHA: "645df498826e6b38814f2e30ea322097400121df7b22b19b9ae299718c3c670f", Rows: 4},
		{File: "skills-3080-original3719-next18-210227-evaluation175645.json", SHA: "729615f614ed7164363b412a6b0d1ff2ac52ea7b874114c3e7094b4490d2bbe5", Rows: 18},
		{File: "skills-3080-primaryflow-next8-evaluation175645.json", SHA: "fa8670f53f66d72b1f4d9850e06241f583e44057a05037bdd296c55e5910aa7f", Rows: 8},
		{File: "skills-3080-source9-next2-visible-evaluation175645.json", SHA: "8f9f22d4273a317acf5026dcfa791c9ad34695ad9fd84a544218d6e8545bb86a", Rows: 2},
	} {
		if err := assessmentInputRegistered(artifacts, required); err != nil {
			return err
		}
	}
	return nil
}

func assessmentInputRegistered(artifacts []ExecutionArtifact, required ExecutionArtifact) error {
	found := 0
	for _, artifact := range artifacts {
		if artifact.File == required.File {
			if artifact.Rows != required.Rows || artifact.SHA != required.SHA {
				return fmt.Errorf("received snapshot pin/count differs: %s", required.File)
			}
			found++
		}
	}
	if found != 1 {
		return fmt.Errorf("received snapshot missing/repeated: %s", required.File)
	}
	return nil
}

// Bind fidelity cases using this helper to the runtime cursor. Independent
// tests that do not call it need their own manifest-registration assertion.
// A checked file on disk cannot substitute for its execution-manifest link.
func requireAssessmentInputRegistered(t *testing.T, file, sha string, rows int) {
	t.Helper()
	var m ExecutionManifest
	loadPublicReceipt(t, "skills-3080-execution-evidence.json", &m)
	if err := assessmentInputRegistered(m.Artifacts, ExecutionArtifact{File: file, SHA: sha, Rows: rows}); err != nil {
		t.Fatal(err)
	}
}

// Individually received snapshots also belong to the current occurrence
// cursor. Their files existing on disk cannot substitute for manifest links.
func TestPublishedCursorRetainsReceivedBatchesWithIndependentPins(t *testing.T) {
	var m ExecutionManifest
	loadPublicReceipt(t, "skills-3080-execution-evidence.json", &m)
	required := []ExecutionArtifact{
		{File: "skills-3080-original3719-next12-256267-evaluation175645.json", SHA: "5e5a5e2fb6c6e342a72e0bbb37c63c8d03f6b495b48c0b35ad6517293905348c", Rows: 12},
		{File: "skills-3080-original3719-next21-268288-evaluation175645.json", SHA: "54b04b462a806d536fcf352c793e4c91a13783373544caa179b196edd64cbc62", Rows: 21},
		{File: "skills-3080-original3719-next6-250255-evaluation175645.json", SHA: "831104060ccb98ddd92a16aaf92011b6dff2b8fda05b28c7e35868d145a03167", Rows: 6},
		{File: "skills-3080-primaryflow-next16-after195-evaluation175645.json", SHA: "fc5168d8c3d1709b44d6d1dc4850815bf776c281ee6604e742c960a6c4b1c16f", Rows: 16},
		{File: "skills-3080-original3719-next4-246249-evaluation175645.json", SHA: "a92204428795cf0f3bbbb8c33bd458e1e3c549c6e1156de2fcc0bbd3ae1c2aac", Rows: 4},
		{File: "skills-3080-primaryflow-next5-after190-evaluation175645.json", SHA: "0d0a901edf7c43bc71b6f3eb4a0aacf4cbd73ca74006db73b352483a5417c2c5", Rows: 5},
		{File: "skills-3080-original3719-next6-240245-evaluation175645.json", SHA: "c9face778d8c89d4832323ff6dabd508291a905b361c2788b70133460795603e", Rows: 6},
		{File: "skills-3080-primaryflow-next15-evaluation175645.json", SHA: "e0d0d65c02bafd1157ebb03873f7f95dc3b2831cae787824338bbcb63023218b", Rows: 15},
		{File: "skills-3080-source9-next2-7to8-visible-evaluation175645.json", SHA: "d5e2d1972f51d44fbd8b9ed32e50ec17a60f80fb13776de480c15ba357a4bcb3", Rows: 2},
		{File: "skills-3080-original3719-next12-228239-evaluation175645.json", SHA: "c23a189ea75961f03bf43d492f2a872c3711a9a188913536feaea7f78b51e475", Rows: 12},
		{File: "skills-3080-primaryflow-next5-evaluation175645.json", SHA: "59e231295f75004536f33de7d45a3369b3c9b55e5730ac11ab3fb99bf6927f19", Rows: 5},
		{File: "skills-3080-original3719-next8-199206-evaluation175645.json", SHA: "0362991564c4bee6b86edc9b52cc934b94d5b47ef4b142985f39b5ed5d8280b2", Rows: 8},
		{File: "skills-3080-original3719-next3-207209-evaluation175645.json", SHA: "80f9eab15b84bd88006cde9d5a61dc438be2fb83259f9accac271ac057ddd442", Rows: 3},
		{File: "skills-3080-source9-first4-visible-evaluation175645.json", SHA: "645df498826e6b38814f2e30ea322097400121df7b22b19b9ae299718c3c670f", Rows: 4},
		{File: "skills-3080-original3719-next18-210227-evaluation175645.json", SHA: "729615f614ed7164363b412a6b0d1ff2ac52ea7b874114c3e7094b4490d2bbe5", Rows: 18},
		{File: "skills-3080-primaryflow-next8-evaluation175645.json", SHA: "fa8670f53f66d72b1f4d9850e06241f583e44057a05037bdd296c55e5910aa7f", Rows: 8},
		{File: "skills-3080-source9-next2-visible-evaluation175645.json", SHA: "8f9f22d4273a317acf5026dcfa791c9ad34695ad9fd84a544218d6e8545bb86a", Rows: 2},
	}
	for _, pin := range required {
		t.Run(pin.File, func(t *testing.T) {
			found := 0
			for _, artifact := range m.Artifacts {
				if artifact.File == pin.File {
					if artifact.Rows != pin.Rows || artifact.SHA != pin.SHA {
						t.Fatal("current received artifact no longer has its independent finalized pin/count")
					}
					found++
				}
			}
			if found != 1 {
				t.Fatal("current received artifact absent or repeated in actual manifest")
			}
			for _, mutation := range []string{"omit", "row_count", "digest", "duplicate"} {
				t.Run(mutation, func(t *testing.T) {
					changed := make([]ExecutionArtifact, 0, len(m.Artifacts)+1)
					for _, artifact := range m.Artifacts {
						if artifact.File == pin.File {
							switch mutation {
							case "omit":
								continue
							case "row_count":
								artifact.Rows--
							case "digest":
								artifact.SHA = "0000000000000000000000000000000000000000000000000000000000000000"
							case "duplicate":
								changed = append(changed, artifact)
							}
						}
						changed = append(changed, artifact)
					}
					if err := publishedReceivedCursorCoverage(changed); err == nil {
						t.Fatal("current received observations disappeared or changed despite an otherwise self-consistent cursor")
					}
				})
			}
		})
	}
	var summary ExecutionSummary
	loadPublicReceipt(t, "skills-3080-execution-queue-summary.json", &summary)
	evidence, err := loadExecutionEvidence(filepath.Join("..", "..", "docs"), m)
	if err != nil {
		t.Fatal(err)
	}
	if !publishedAssessmentCountMatches(summary.Assessments, len(evidence.Assessments)) {
		t.Fatal("the published cursor lost already received assessments")
	}
}

func publishedAssessmentCountMatches(actual, registeredUnique int) bool {
	return registeredUnique > 0 && actual == registeredUnique
}

func TestPublishedAssessmentCountRejectsDriftFromAllRegisteredOccurrenceIDs(t *testing.T) {
	var m ExecutionManifest
	loadPublicReceipt(t, "skills-3080-execution-evidence.json", &m)
	evidence, err := loadExecutionEvidence(filepath.Join("..", "..", "docs"), m)
	if err != nil {
		t.Fatal(err)
	}
	expected := len(evidence.Assessments)
	if !publishedAssessmentCountMatches(expected, expected) {
		t.Fatal("registered unique occurrence union rejected")
	}
	for _, actual := range []int{expected - 71, expected - 1, expected + 1} {
		if publishedAssessmentCountMatches(actual, expected) {
			t.Errorf("summary %d accepted for registered unique occurrence union %d", actual, expected)
		}
	}
}

func TestPublishedCursorCannotOmitAnyRegisteredReceivedCohort(t *testing.T) {
	var m ExecutionManifest
	loadPublicReceipt(t, "skills-3080-execution-evidence.json", &m)
	for _, registration := range receivedRegistrations {
		for _, kind := range []string{"missing", "wrong_rows", "duplicate"} {
			t.Run(registration.ID+"/"+kind, func(t *testing.T) {
				rows := append([]ExecutionArtifact(nil), m.Artifacts...)
				i := -1
				for k, r := range rows {
					if r.File == registration.Artifact {
						i = k
						break
					}
				}
				if i < 0 {
					t.Fatal("registered fixture absent")
				}
				switch kind {
				case "missing":
					rows = append(rows[:i], rows[i+1:]...)
				case "wrong_rows":
					rows[i].Rows--
				case "duplicate":
					rows = append(rows, rows[i])
				}
				if publishedReceivedCursorCoverage(rows) == nil {
					t.Fatal("received cursor omission accepted")
				}
			})
		}
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
