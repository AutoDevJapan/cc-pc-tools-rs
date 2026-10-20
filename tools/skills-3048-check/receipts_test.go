package main

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestPublishedReceipts(t *testing.T) {
	data, err := os.ReadFile("../../docs/skills-3048-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err = json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if len(m.Targets) != 14 {
		t.Fatalf("expected 4 new and 10 existing skills, got %d", len(m.Targets))
	}
	shaRE := regexp.MustCompile(`^[a-f0-9]{64}$`)
	newCount := 0
	names := map[string]bool{}
	for _, target := range m.Targets {
		if names[target.Name] {
			t.Fatal("duplicate target")
		}
		names[target.Name] = true
		if target.New {
			newCount++
			if target.BeforeSHA != "absent" {
				t.Fatal("missing absence")
			}
		} else if !shaRE.MatchString(target.BeforeSHA) {
			t.Fatal("missing before hash")
		}
		if target.Positive == "" || target.Negative == "" {
			t.Fatal("missing scenario")
		}
		files := map[string]bool{}
		for _, f := range target.Files {
			if !shaRE.MatchString(f.SHA) {
				t.Fatal("missing file hash")
			}
			files[f.Path] = true
		}
		if !files["SKILL.md"] || !files["references/sources-3048.md"] {
			t.Fatal("unmanifested entrypoint/provenance")
		}
	}
	if newCount != 4 {
		t.Fatalf("new skills: %d", newCount)
	}
	data, err = os.ReadFile("../../docs/skills-3048-sources.json")
	if err != nil {
		t.Fatal(err)
	}
	var sourceDoc struct {
		SourceCount int `json:"source_count"`
		Sources     []struct {
			URL        string   `json:"url"`
			Commit     string   `json:"commit"`
			License    string   `json:"license"`
			Decision   string   `json:"decision"`
			Researched bool     `json:"researched"`
			Artifacts  []File   `json:"read_artifacts"`
			Targets    []string `json:"targets"`
		} `json:"sources"`
	}
	if err = json.Unmarshal(data, &sourceDoc); err != nil {
		t.Fatal(err)
	}
	expected := []string{"Nutlope/hallmark", "MengTo/Skills", "x1xhlol/system-prompts-and-models-of-ai-tools", "hirokaji/jp-ui-contracts", "openai/skills", "mattpocock/skills", "VoltAgent/awesome-design-md", "obra/superpowers", "kepano/obsidian-skills", "6mile/skills", "cloudflare/security-audit-skill", "mizchi/skills", "docker/skills", "Tencent/teamai-cli", "nateherkai/AIS-OS"}
	urls := map[string]bool{}
	if sourceDoc.SourceCount != 16 || len(sourceDoc.Sources) != 16 {
		t.Fatal("16 requested sources are required")
	}
	for _, s := range sourceDoc.Sources {
		if urls[strings.ToLower(s.URL)] {
			t.Fatal("duplicate source")
		}
		urls[strings.ToLower(s.URL)] = true
		for _, targetName := range s.Targets {
			matched := false
			for _, target := range m.Targets {
				if target.Name != targetName {
					continue
				}
				for _, source := range target.Sources {
					if strings.EqualFold(strings.TrimPrefix(source, "https://github.com/"), strings.TrimPrefix(s.URL, "https://github.com/")) {
						matched = true
					}
				}
			}
			if !matched {
				t.Fatalf("unmatched source/target: %s -> %s", s.URL, targetName)
			}
		}
		if !s.Researched || s.License == "" || s.Decision == "" || len(s.Artifacts) == 0 {
			t.Fatal("incomplete source receipt")
		}
		if strings.HasPrefix(s.URL, "https://github.com/") && !regexp.MustCompile(`^[a-f0-9]{40}$`).MatchString(s.Commit) {
			t.Fatal("missing fixed commit")
		}
		for _, f := range s.Artifacts {
			if f.Path == "" || !shaRE.MatchString(f.SHA) {
				t.Fatal("missing artifact hash")
			}
		}
	}
	for _, repo := range expected {
		if !urls[strings.ToLower("https://github.com/"+repo)] {
			t.Fatalf("missing requested repo %s", repo)
		}
	}
	if !urls["https://agent-skills.jp/category"] {
		t.Fatal("missing web catalog")
	}
}
