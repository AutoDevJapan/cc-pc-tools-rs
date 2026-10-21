package main

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestActualAdoptionsConserveCatalogAndManifestIdentities(t *testing.T) {
	var ledger struct {
		Total       int `json:"catalog_occurrences"`
		Installed   int `json:"installed_new"`
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
	}
	loadPublicReceipt(t, "skills-3080-adoptions.json", &ledger)
	var p struct {
		Installed int `json:"installed"`
		Total     int `json:"catalog_records"`
	}
	loadPublicReceipt(t, "skills-3080-progress.json", &p)
	if ledger.Installed != len(ledger.Rows) || ledger.Installed != p.Installed || ledger.Total != p.Total || ledger.Installed+ledger.Pending != ledger.Total || ledger.Available+ledger.Unavailable+ledger.Held != ledger.Pending {
		t.Fatal("actual adoption counters do not conserve source occurrences")
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, row := range ledger.Rows {
		if row.ID == "" || ids[row.ID] || names[row.Target] || row.Disposition != "installed_new_adaptation" {
			t.Fatal("duplicate or non-installed adoption")
		}
		ids[row.ID] = true
		names[row.Target] = true
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
