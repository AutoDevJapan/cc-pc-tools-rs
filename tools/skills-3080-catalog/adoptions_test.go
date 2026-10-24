package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
)

func validateSemanticPending(expected int, reported *int) error {
	if reported == nil || *reported != expected {
		return fmt.Errorf("semantic pending projection differs from ledger pending %d", expected)
	}
	return nil
}

func TestSemanticPendingProjectionRejectsStaleOrMissingCounts(t *testing.T) {
	var ledger struct {
		Pending int `json:"pending_occurrences"`
	}
	var progress struct {
		Candidates struct {
			Pending *int `json:"not_semantically_disposed_occurrences"`
		} `json:"candidate_count_definitions"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	loadPublicReceipt(t, "skills-3080-progress.json", &progress)
	for _, kind := range []string{"valid", "previous_pending", "one_too_few", "missing"} {
		t.Run(kind, func(t *testing.T) {
			reported := progress.Candidates.Pending
			switch kind {
			case "previous_pending":
				v := ledger.Pending + 1
				reported = &v
			case "one_too_few":
				v := ledger.Pending - 1
				reported = &v
			case "missing":
				reported = nil
			}
			if err := validateSemanticPending(ledger.Pending, reported); (err != nil) != (kind != "valid") {
				t.Fatalf("%s: %v", kind, err)
			}
		})
	}
}

func TestActualAdoptionsConserveCatalogAndManifestIdentities(t *testing.T) {
	var ledger struct {
		Total       int `json:"catalog_occurrences"`
		Installed   int `json:"installed_new"`
		Integrated  int `json:"integrated"`
		Excluded    int `json:"excluded"`
		Pending     int `json:"pending_occurrences"`
		Available   int `json:"pending_available_body_occurrences"`
		Unavailable int `json:"unavailable_site_occurrences"`
		Held        int `json:"execution_held_occurrences"`
		Rows        []struct {
			ID          string `json:"inventory_id"`
			Target      string `json:"target"`
			Manifest    string `json:"manifest"`
			SHA         string `json:"entrypoint_sha256"`
			Disposition string `json:"disposition"`
		} `json:"entries"`
		ExcludedRows []json.RawMessage `json:"excluded_entries"`
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	var p struct {
		Installed  int `json:"installed"`
		Integrated int `json:"integrated"`
		Excluded   int `json:"excluded"`
		Total      int `json:"catalog_records"`
	}
	loadPublicReceipt(t, "skills-3080-progress.json", &p)
	if ledger.Installed+ledger.Integrated != len(ledger.Rows) || ledger.Excluded != len(ledger.ExcludedRows) || ledger.Installed != p.Installed || ledger.Integrated != p.Integrated || ledger.Excluded != p.Excluded || ledger.Total != p.Total || ledger.Installed+ledger.Integrated+ledger.Excluded+ledger.Pending != ledger.Total || ledger.Available+ledger.Unavailable+ledger.Held != ledger.Pending {
		t.Fatal("actual adoption counters do not conserve source occurrences")
	}
	ids, names := map[string]bool{}, map[string]bool{}
	installed, integrated := 0, 0
	for _, row := range ledger.Rows {
		if row.ID == "" || ids[row.ID] || row.Target == "" {
			t.Fatal("duplicate source or missing adoption identity")
		}
		ids[row.ID] = true
		switch row.Disposition {
		case "installed_new_adaptation":
			if names[row.Target] {
				t.Fatal("duplicate new skill target")
			}
			names[row.Target] = true
			installed++
		case "integrated_existing":
			integrated++
		default:
			t.Fatal("unverified disposition counted as an adoption")
		}
		var manifest struct {
			Targets []struct {
				Name  string `json:"name"`
				Files []struct {
					Path string `json:"path"`
					SHA  string `json:"sha256"`
				} `json:"files"`
			} `json:"targets"`
		}
		loadPublicReceipt(t, row.Manifest, &manifest)
		matches := 0
		for _, target := range manifest.Targets {
			if target.Name != row.Target {
				continue
			}
			for _, file := range target.Files {
				if file.Path == "SKILL.md" && file.SHA == row.SHA {
					matches++
				}
			}
		}
		if matches != 1 {
			t.Fatal("adoption entrypoint does not match exactly one installed manifest")
		}
	}
	if installed != ledger.Installed || integrated != ledger.Integrated {
		t.Fatal("new and integrated source counts differ from their accepted manifests")
	}
	var docker struct {
		Installed int    `json:"installed"`
		Manifest  string `json:"manifest"`
		SHA       string `json:"manifest_sha256"`
		Discovery struct {
			Found   int `json:"target_matches"`
			Enabled int `json:"enabled_count"`
			Errors  int `json:"parse_errors"`
		} `json:"discovery"`
	}
	loadPublicReceipt(t, "skills-3080-docker-installation-evidence.json", &docker)
	var m struct {
		Targets []struct {
			Name string `json:"name"`
		} `json:"targets"`
	}
	b := loadPublicReceipt(t, docker.Manifest, &m)
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != docker.SHA || len(m.Targets) != docker.Installed || docker.Discovery.Found != docker.Installed || docker.Discovery.Enabled != docker.Installed || docker.Discovery.Errors != 0 {
		t.Fatal("Docker deployment evidence or discovery differs from actual manifest")
	}
}
