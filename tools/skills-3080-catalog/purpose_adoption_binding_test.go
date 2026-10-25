package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestPurposeReceiptLoaderRejectsDuplicateKeysBeforeProjection(t *testing.T) {
	raw, err := os.ReadFile("../../docs/skills-3080-adoptions.json")
	if err != nil {
		t.Fatal(err)
	}
	id := utilityEightPurposeSpec().WantedIDs["build-game-changelog"]
	needle, _ := json.Marshal(id)
	if bytes.Count(raw, needle) != 1 {
		t.Fatal("fixed mutation occurrence not unique")
	}
	for _, kind := range []string{"valid", "conflicting_inventory_id", "same_value_inventory_id", "conflicting_count", "trailing_value"} {
		t.Run(kind, func(t *testing.T) {
			fixture := append([]byte(nil), raw...)
			switch kind {
			case "conflicting_inventory_id":
				fixture = bytes.Replace(fixture, needle, append([]byte(`"foreign-occurrence", "inventory_id":`), needle...), 1)
			case "same_value_inventory_id":
				fixture = bytes.Replace(fixture, needle, append(append(append([]byte(nil), needle...), []byte(`, "inventory_id":`)...), needle...), 1)
			case "conflicting_count":
				fixture = append([]byte(`{"installed_new":999,`), bytes.TrimSpace(fixture)[1:]...)
			case "trailing_value":
				fixture = append(fixture, []byte(` {"ignored":true}`)...)
			}
			var projection struct {
				Rows []purposeAdoptionRow `json:"entries"`
			}
			err := decodePublicReceipt(fixture, &projection)
			if (err != nil) != (kind != "valid") {
				t.Fatalf("%s accepted=%v: %v", kind, err == nil, err)
			}
			if kind != "valid" && len(projection.Rows) != 0 {
				t.Fatal("ambiguous input was projected before rejection")
			}
		})
	}
}

type purposeAdoptionRow struct {
	cohortLedgerRow
	Target   string `json:"target"`
	Manifest string `json:"manifest"`
	EntrySHA string `json:"entrypoint_sha256"`
	Evidence string `json:"evidence"`
}

// Other deployments may coexist, but no row may borrow this set's target,
// manifest or evidence while changing the accepted source occurrence.
func validatePurposeAdoptionRows(want map[string]purposeAdoptionRow, rows []purposeAdoptionRow) error {
	if len(want) == 0 {
		return fmt.Errorf("empty expected deployment")
	}
	seen := map[string]bool{}
	for _, row := range rows {
		expected, selected := want[row.ID]
		if !selected {
			for _, w := range want {
				if row.Target == w.Target || row.Manifest == w.Manifest || row.Evidence == w.Evidence {
					return fmt.Errorf("foreign occurrence borrows accepted deployment: %s", row.ID)
				}
			}
			continue
		}
		if seen[row.ID] || row != expected {
			return fmt.Errorf("adoption identity, target or evidence differs: %s", row.ID)
		}
		seen[row.ID] = true
	}
	if len(seen) != len(want) {
		return fmt.Errorf("accepted adoption omitted: %d/%d", len(seen), len(want))
	}
	return nil
}

func expectedPurposeAdoptionRows(t *testing.T, s purposeDeploymentSpec, accepted map[string]cohortLedgerRow, outputHashes map[string]string) map[string]purposeAdoptionRow {
	t.Helper()
	want := map[string]purposeAdoptionRow{}
	for target, id := range s.WantedIDs {
		row, ok := accepted[id]
		sha := outputHashes[target+"/SKILL.md"]
		if !ok || row.ID != id || sha == "" {
			t.Fatal("missing accepted source or reviewed entrypoint", target)
		}
		want[id] = purposeAdoptionRow{cohortLedgerRow: row, Target: target, Manifest: s.InstalledFile, EntrySHA: sha, Evidence: s.ReceiptFile}
	}
	return want
}

func checkPurposeAdoptionRows(t *testing.T, s purposeDeploymentSpec, accepted map[string]cohortLedgerRow, outputHashes map[string]string) {
	t.Helper()
	var ledger struct {
		Rows []purposeAdoptionRow `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	if err := validatePurposeAdoptionRows(expectedPurposeAdoptionRows(t, s, accepted, outputHashes), ledger.Rows); err != nil {
		t.Fatal(err)
	}
}

func TestPurposeAdoptionBindingRejectsLedgerDrift(t *testing.T) {
	s := utilityEightPurposeSpec()
	accepted := validatedUtilityEightDispositions(t)
	var operation struct {
		Files []struct {
			Target string `json:"target"`
			SHA    string `json:"sha256"`
		} `json:"files"`
	}
	loadPublicReceipt(t, s.ManifestFile, &operation)
	hashes := map[string]string{}
	for _, f := range operation.Files {
		hashes[f.Target] = f.SHA
	}
	want := expectedPurposeAdoptionRows(t, s, accepted, hashes)
	var ledger struct {
		Rows []purposeAdoptionRow `json:"entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	for _, kind := range []string{"valid", "source_repo", "source_commit", "source_path", "source_sha", "target", "entrypoint_sha", "disposition", "manifest", "evidence", "missing", "duplicate", "borrowed_identity"} {
		t.Run(kind, func(t *testing.T) {
			rows := append([]purposeAdoptionRow(nil), ledger.Rows...)
			i := -1
			for j, row := range rows {
				if row.ID == s.WantedIDs["build-game-changelog"] {
					i = j
					break
				}
			}
			if i < 0 {
				t.Fatal("mutation target absent")
			}
			switch kind {
			case "source_repo":
				rows[i].Source = "other/repo"
			case "source_commit":
				rows[i].Commit = "different"
			case "source_path":
				rows[i].Path = "other/SKILL.md"
			case "source_sha":
				rows[i].SHA = "different"
			case "target":
				rows[i].Target = "other-target"
			case "entrypoint_sha":
				rows[i].EntrySHA = "different"
			case "disposition":
				rows[i].Disposition = "integrated_existing"
			case "manifest":
				rows[i].Manifest = "other.json"
			case "evidence":
				rows[i].Evidence = "other.json"
			case "missing":
				rows = append(rows[:i], rows[i+1:]...)
			case "duplicate":
				rows = append(rows, rows[i])
			case "borrowed_identity":
				rows[i].ID = "foreign-occurrence"
			}
			if err := validatePurposeAdoptionRows(want, rows); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}
