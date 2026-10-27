package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func skillsAssignedIssuerRepairScope(t *testing.T) skillsPublicReviewScope {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "skills3080-runtime-issuer-repair-scope.json"))
	if err != nil {
		t.Fatal(err)
	}
	if hashBytes(raw) != "5d5ac7a6eff7871a94e0c885b5eb45129bebf892e74354202b03b5f19b29c86b" {
		t.Fatal("the assigned original repair specification bytes changed")
	}
	var c skillsPublicReviewScope
	if !skillsStrict(raw, &c) {
		t.Fatal("immutable assigned public repair scope is not valid JSON")
	}
	return c
}

func TestSkills3080AssignedIssuerRepairScopeAcceptedWithoutRewrite(t *testing.T) {
	c := skillsAssignedIssuerRepairScope(t)
	raw, _ := json.Marshal(c)
	if _, ok := skillsParsePublicRepair(raw); !ok {
		t.Fatal("the assigned exact public software evidence prevents normal original-result reception")
	}
}

func TestSkills3080IssuerRepairSoftwareEvidenceStaysExact(t *testing.T) {
	for _, kind := range []string{"other_commit", "other_purpose", "other_path", "other_sha", "other_blob", "other_bytes", "ordinary_repair", "ordinary_review"} {
		t.Run(kind, func(t *testing.T) {
			c := skillsAssignedIssuerRepairScope(t)
			index := -1
			for i, f := range c.Files {
				if f.Path == "docs/evidence/official-ci3387-known-guard-baseline-f30f9e76.json" {
					index = i
				}
			}
			if index < 0 {
				t.Fatal("original declared evidence is missing")
			}
			switch kind {
			case "other_commit":
				c.Commit = strings.Repeat("a", 40)
			case "other_purpose":
				c.Purpose += "changed"
			case "other_path":
				c.Files[index].Path = "docs/evidence/official-ci3387-known-guard-baseline-521c4c4d.json"
			case "other_sha":
				c.Files[index].SHA = strings.Repeat("b", 64)
			case "other_blob":
				c.Files[index].Blob = strings.Repeat("c", 40)
			case "other_bytes":
				c.Files[index].Bytes++
			case "ordinary_repair", "ordinary_review":
				manifest := c.Files[1]
				if manifest.Path != c.ManifestPath {
					t.Fatal("original manifest identity changed")
				}
				c.Files = []skillsPublicReviewFile{manifest, c.Files[index]}
				c.Purpose, c.PurposePath = "", ""
				if kind == "ordinary_review" {
					c.Domain, c.Instructions = "skills-3080-public-review-v1", skillsPublicReviewInstructions
				}
			}
			raw, _ := json.Marshal(c)
			if kind == "ordinary_review" {
				if _, ok := skillsParsePublicReview(raw); ok {
					t.Fatal("software evidence broadened ordinary content-review scope")
				}
			} else if _, ok := skillsParsePublicRepair(raw); ok {
				t.Fatal("software evidence exceeded its exact assigned public scope")
			}
		})
	}
}

func TestSkills3080Accepted817DistributionSurvivesItsSuccessor(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docs", "evidence", "skills-3080-guard-installed-81700412.json"))
	if err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		SHA      string `json:"sha256"`
		Head     string `json:"source_head"`
		Review   string `json:"review_job"`
		Observed string `json:"actual_installed_sha256"`
	}
	if json.Unmarshal(raw, &evidence) != nil || evidence.SHA != "94c08be91776df277a6a3442e71f291103345641558657e1872e36520ad924e0" || evidence.Head != "817004120a458b3a37830478512da0aa8040c9d0" || evidence.Review != "20261027-102822-todo-3896" || evidence.Observed != evidence.SHA {
		t.Fatal("the already accepted normal distribution observation changed")
	}
	p := skillsProducer{evidence.SHA, evidence.Head}
	current := skillsProducer{strings.Repeat("7", 64), strings.Repeat("8", 40)}
	if !skillsAcceptedProducer(p, current) {
		t.Fatal("the accepted normal Windows distribution was lost in its successor")
	}
	p.Revision = strings.Repeat("a", 40)
	if skillsAcceptedProducer(p, current) {
		t.Fatal("same software hash with an unaccepted source revision was trusted")
	}
}

func TestSkills3080AcceptedAd5DistributionSurvivesScopedSuccessor(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "docs", "evidence", "skills-3080-guard-before-scope589-ad5.json"))
	if err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		SHA  string `json:"sha256"`
		Head string `json:"source_head"`
		Job  string `json:"independent_deployment_job"`
	}
	if json.Unmarshal(raw, &evidence) != nil || evidence.SHA != "ad5d37c7b216264d2e99007a2e6f3468e7529803454d88f1954cc74a7beb4783" || evidence.Head != "0d97c1952ff6f8b47a3d40939941639b35309408" || evidence.Job != "20261027-093919-todo-3387" {
		t.Fatal("the independently accepted shared distribution observation changed")
	}
	p := skillsProducer{evidence.SHA, evidence.Head}
	current := skillsProducer{strings.Repeat("7", 64), strings.Repeat("8", 40)}
	if !skillsAcceptedProducer(p, current) {
		t.Fatal("the shared normal distribution was lost in its scoped successor")
	}
	p.Revision = strings.Repeat("a", 40)
	if skillsAcceptedProducer(p, current) {
		t.Fatal("same shared software hash with a different source was trusted")
	}
}
