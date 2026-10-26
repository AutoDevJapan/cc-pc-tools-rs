package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

// Integration data must use the installed create API's strict source shape.
// Rich provenance belongs in source.json/the separate meaning document.
func TestSecurityCoverageOperationUsesStrictCreationContract(t *testing.T) {
	checkCreationInputContract(t, "skills-3080-operations/create-security-coverage-214717.json", "cloudflare/security-audit-skill", "e598e694aa506650c7192d5ea3be0e50aca0d356ea00551c53429f0b01eb6391", 4, 9)
}

func TestMotionFourOperationUsesStrictCreationContract(t *testing.T) {
	checkCreationInputContract(t, "skills-3080-operations/create-motion4-214717.json", "MengTo/Skills", "b2a071ba22fefd803f4e9d1f5c777dd79c62d191695586a1a73fd4d0ebd051c6", 4, 24)
}

func TestPricingTwoOperationUsesStrictCreationContract(t *testing.T) {
	checkCreationInputContract(t, "skills-3080-operations/create-pricing2-214717.json", "MengTo/Skills", "b2a071ba22fefd803f4e9d1f5c777dd79c62d191695586a1a73fd4d0ebd051c6", 2, 12)
}

func TestOpenAISixOperationPreservesIndividualPackageNotices(t *testing.T) {
	checkCreationInputContract(t, "skills-3080-operations/create-openai6-214717.json", "openai/skills", "", 6, 36, map[string][2]string{
		"skills/.curated/aspnet-core/SKILL.md":            {"skills/.curated/aspnet-core/LICENSE.txt", "58d1e17ffe5109a7ae296caafcadfdbe6a7d176f0bc4ab01e12a689b0499d8bd"},
		"skills/.curated/chatgpt-apps/SKILL.md":           {"skills/.curated/chatgpt-apps/LICENSE.txt", "a7f55b2cb68997f729e6199720d4d3cae47633aba0254a84b2d36ab284be9d45"},
		"skills/.curated/linear/SKILL.md":                 {"skills/.curated/linear/LICENSE.txt", "58d1e17ffe5109a7ae296caafcadfdbe6a7d176f0bc4ab01e12a689b0499d8bd"},
		"skills/.curated/security-ownership-map/SKILL.md": {"skills/.curated/security-ownership-map/LICENSE.txt", "4dd13869245e356246a5b770723247bbb80a8f07a181d1d3d873a1734297cdb9"},
		"skills/.curated/security-threat-model/SKILL.md":  {"skills/.curated/security-threat-model/LICENSE.txt", "4dd13869245e356246a5b770723247bbb80a8f07a181d1d3d873a1734297cdb9"},
		"skills/.curated/winui-app/SKILL.md":              {"skills/.curated/winui-app/LICENSE.txt", "58d1e17ffe5109a7ae296caafcadfdbe6a7d176f0bc4ab01e12a689b0499d8bd"},
	})
}

func checkCreationInputContract(t *testing.T, file, repo, notice string, sources, files int, individual ...map[string][2]string) {
	t.Helper()
	if len(individual) > 1 || len(individual) == 1 && len(individual[0]) != sources {
		t.Fatal("one exact per-source notice map required")
	}
	var operation struct {
		Domain   string `json:"domain"`
		Todo     int    `json:"todo_id"`
		Decision string `json:"decision_identity_sha256"`
		Writer   struct {
			Task      int    `json:"task"`
			Job       string `json:"job"`
			Spec      string `json:"spec_sha256"`
			Workspace string `json:"workspace"`
		} `json:"writer"`
		Sources []struct {
			Repo        string `json:"repository"`
			Commit      string `json:"commit"`
			Path        string `json:"path"`
			SHA         string `json:"sha256"`
			Acquisition string `json:"acquisition"`
			License     struct {
				Path     string `json:"path"`
				SHA      string `json:"sha256"`
				Scope    string `json:"scope"`
				Accepted bool   `json:"accepted"`
			} `json:"license"`
		} `json:"sources"`
		Files []struct {
			Target  string `json:"target"`
			SHA     string `json:"sha256"`
			Content string `json:"content_base64"`
		} `json:"files"`
	}
	raw := loadPublicReceipt(t, file, &map[string]any{})
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&operation); err != nil {
		t.Fatal(err)
	}
	if operation.Domain != "skills-3080-create-v1" || operation.Todo != 3080 || len(operation.Sources) != sources || len(operation.Files) != files {
		t.Fatal("creation contract lost its finite scope")
	}
	seen := map[string]bool{}
	for _, source := range operation.Sources {
		path, digest := "LICENSE", notice
		if len(individual) == 1 {
			want, ok := individual[0][source.Path]
			if !ok || seen[source.Path] {
				t.Fatal("source is outside or repeated in the individual notice map")
			}
			seen[source.Path] = true
			path, digest = want[0], want[1]
		}
		if source.Repo != repo || !source.License.Accepted || source.License.Path != path || source.License.SHA != digest || source.License.Scope == "" {
			t.Fatal("selected source lost its individual notice scope")
		}
	}
}
