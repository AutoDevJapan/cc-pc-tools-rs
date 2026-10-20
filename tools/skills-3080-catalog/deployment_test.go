package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadPublicReceipt(t *testing.T, name string, v any) []byte {
	t.Helper()
	b, err := os.ReadFile("../../docs/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDeploymentCountersDoNotCountUninstalledPlansOrDuplicateDraft(t *testing.T) {
	var evidence struct {
		Installed int    `json:"installed"`
		Files     int    `json:"files_readback_verified"`
		Manifest  string `json:"manifest"`
		SHA       string `json:"manifest_sha256"`
		Done      bool   `json:"overall_complete"`
		Remaining int    `json:"remaining_records"`
		Total     int    `json:"catalog_total"`
		Discovery struct {
			Found   int `json:"target_matches"`
			Enabled int `json:"enabled_count"`
			Errors  int `json:"parse_errors"`
		} `json:"discovery"`
	}
	loadPublicReceipt(t, "skills-3080-installation-evidence.json", &evidence)
	var manifest struct {
		Targets []struct {
			Name  string `json:"name"`
			Files []struct {
				Path string `json:"path"`
				SHA  string `json:"sha256"`
			} `json:"files"`
		} `json:"targets"`
	}
	b := loadPublicReceipt(t, evidence.Manifest, &manifest)
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != evidence.SHA {
		t.Fatal("installed manifest no longer matches deployment evidence")
	}
	names := map[string]bool{}
	fileCount := 0
	for _, target := range manifest.Targets {
		if names[target.Name] {
			t.Fatal("installed target repeated")
		}
		names[target.Name] = true
		paths := map[string]bool{}
		for _, f := range target.Files {
			if paths[f.Path] {
				t.Fatal("installed file repeated")
			}
			paths[f.Path] = true
			raw, err := hex.DecodeString(f.SHA)
			if err != nil || len(raw) != 32 {
				t.Fatal("installed file missing hash")
			}
			fileCount++
		}
	}
	if evidence.Done || evidence.Installed != len(names) || evidence.Files != fileCount || evidence.Remaining+evidence.Installed != evidence.Total || evidence.Discovery.Found != len(names) || evidence.Discovery.Enabled != len(names) || evidence.Discovery.Errors != 0 {
		t.Fatal("deployment counts conflate installed and pending records")
	}
	var progress struct {
		Installed int `json:"installed"`
		Designs   int `json:"candidate_designs"`
	}
	loadPublicReceipt(t, "skills-3080-progress.json", &progress)
	var pending struct {
		Targets []struct {
			Name string `json:"name"`
			SHA  string `json:"source_sha256"`
		} `json:"targets"`
	}
	files, err := filepath.Glob("../../docs/skills-3080-batch*.json")
	if err != nil {
		t.Fatal(err)
	}
	prepared := map[string]string{}
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(b, &pending); err != nil {
			t.Fatal(err)
		}
		for _, target := range pending.Targets {
			if prepared[target.Name] != "" {
				t.Fatal("plan target repeated")
			}
			prepared[target.Name] = target.SHA
		}
	}
	var draft struct {
		Targets []struct {
			Name string `json:"name"`
			SHA  string `json:"source_sha256"`
		} `json:"targets"`
	}
	loadPublicReceipt(t, "skills-3080-docker-draft.json", &draft)
	for _, target := range draft.Targets {
		if prepared[target.Name] != target.SHA {
			t.Fatal("draft lost fixed source identity")
		}
	}
	for name := range names {
		if prepared[name] == "" {
			t.Fatal("installed target has no source plan")
		}
	}
	if progress.Installed < evidence.Installed || progress.Installed > len(prepared) || progress.Designs != len(prepared) {
		t.Fatal("plan/draft/installed counters differ")
	}
}

func TestIndependentNoticesAndInvocationMetadataRemainFixed(t *testing.T) {
	for name, want := range map[string]string{"skills-3080-docker-APACHE.txt": "3bb33f65872155f0801efc9f6732e2868eb338c5ed6a81eec5c775c1382761cb", "skills-3080-humanlayer-MIT.txt": "5f13c18ea00ea5c1384f41745feeca774079164f7f59a92e4ac0899ad217b26f"} {
		b, err := os.ReadFile("../../docs/" + name)
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != want {
			t.Fatal("preserved notice bytes changed")
		}
	}
	var records []struct {
		Path    string `json:"path"`
		Content string `json:"content"`
		SHA     string `json:"sha256"`
	}
	loadPublicReceipt(t, "skills-3080-docker-invocation.json", &records)
	seen := map[string]bool{}
	implicit := 0
	for _, r := range records {
		h := sha256.Sum256([]byte(r.Content))
		if seen[r.Path] || hex.EncodeToString(h[:]) != r.SHA {
			t.Fatal("source metadata identity/hash differs")
		}
		seen[r.Path] = true
		if strings.HasSuffix(r.Path, "/agents/openai.yaml") {
			if !strings.Contains(r.Content, "allow_implicit_invocation: true") {
				t.Fatal("upstream policy changed")
			}
			implicit++
		}
	}
	if len(records) != 16 || implicit != 8 {
		t.Fatal("selected source metadata coverage differs")
	}
}
