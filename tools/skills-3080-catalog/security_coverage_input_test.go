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

func checkCreationInputContract(t *testing.T, file, repo, notice string, sources, files int) {
	t.Helper()
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
	for _, source := range operation.Sources {
		if source.Repo != repo || !source.License.Accepted || source.License.Path != "LICENSE" || source.License.SHA != notice || source.License.Scope == "" {
			t.Fatal("selected source lost its individual notice scope")
		}
	}
}
