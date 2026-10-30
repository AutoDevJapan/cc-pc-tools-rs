package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func executionRepositoryFixture(t *testing.T) (string, ExecutionManifest, ExecutionEvidence) {
	t.Helper()
	input, m, e := executionFixture()
	var out bytes.Buffer
	dec := json.NewDecoder(strings.NewReader(input))
	for i := 0; i < m.Records; i++ {
		var r Record
		if err := dec.Decode(&r); err != nil {
			t.Fatal(err)
		}
		if r.ID == "first" {
			r.Repo = "example/skills"
		}
		if err := json.NewEncoder(&out).Encode(r); err != nil {
			t.Fatal(err)
		}
	}
	h := sha256.Sum256(out.Bytes())
	m.CatalogSHA = hex.EncodeToString(h[:])
	a := e.Assessments["first"]
	a.SourceRepo = "example/skills"
	e.Assessments["first"] = a
	return out.String(), m, e
}

func TestExecutionAssessmentRequiresCatalogRepository(t *testing.T) {
	for _, repo := range []string{"example/skills", "different/skills", ""} {
		t.Run("repository="+repo, func(t *testing.T) {
			input, m, e := executionRepositoryFixture(t)
			a := e.Assessments["first"]
			a.SourceRepo = repo
			e.Assessments["first"] = a
			_, err := executionQueue(strings.NewReader(input), &bytes.Buffer{}, m, e)
			if (err == nil) != (repo == "example/skills") {
				t.Fatalf("known catalog repository matched unverified assessment repository %q: %v", repo, err)
			}
		})
	}
}

func TestExecutionRepeatedAssessmentRepositoryCannotChange(t *testing.T) {
	for _, repo := range []string{"example/skills", "different/skills", ""} {
		t.Run("repository="+repo, func(t *testing.T) {
			_, manifest, root, _ := executionDiskFixture(t)
			data, err := os.ReadFile(manifest)
			if err != nil {
				t.Fatal(err)
			}
			var m ExecutionManifest
			if err = json.Unmarshal(data, &m); err != nil {
				t.Fatal(err)
			}
			_, _, e := executionRepositoryFixture(t)
			a := e.Assessments["first"]
			for i, source := range []string{"example/skills", repo} {
				a.SourceRepo = source
				data, err = json.Marshal(struct {
					Entries []executionIdentity `json:"entries"`
				}{[]executionIdentity{a}})
				if err != nil {
					t.Fatal(err)
				}
				file := fmt.Sprintf("skills-3080-assessment-%d.json", i)
				if err = os.WriteFile(filepath.Join(root, file), data, 0600); err != nil {
					t.Fatal(err)
				}
				h := sha256.Sum256(data)
				m.Artifacts = append(m.Artifacts, ExecutionArtifact{File: file, SHA: hex.EncodeToString(h[:]), Rows: 1})
			}
			loaded, err := loadExecutionEvidence(root, m)
			if repo == "example/skills" {
				if err != nil || len(loaded.References["first"]) != 2 {
					t.Fatalf("matching independent references lost: %v", err)
				}
			} else if err == nil {
				t.Fatalf("conflicting assessment repository %q accepted", repo)
			}
		})
	}
}

func TestExecutionLegacyRepositoryFieldsPreserveExplicitProvenance(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
		bad             bool
	}{
		{"source_repo", `{"source_repo":"example/skills"}`, "example/skills", false},
		{"repo", `{"repo":"example/skills"}`, "example/skills", false},
		{"source", `{"source":"example/skills"}`, "example/skills", false},
		{"equal_aliases", `{"source_repo":"example/skills","repo":"example/skills","source":"example/skills"}`, "example/skills", false},
		{"conflicting_repo", `{"source_repo":"example/skills","repo":"different/skills"}`, "", true},
		{"conflicting_source", `{"source_repo":"example/skills","source":"different/skills"}`, "", true},
		{"label_is_not_repository", `{"source":"saved public source"}`, "", false},
		{"url_is_not_repository", `{"source":"https://github.com/example/skills"}`, "", false},
		{"missing_is_unknown", `{}`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r executionIdentity
			if err := json.Unmarshal([]byte(tc.raw), &r); err != nil {
				t.Fatal(err)
			}
			err := normalizeExecutionRepository(&r)
			if (err != nil) != tc.bad || !tc.bad && r.SourceRepo != tc.want {
				t.Fatalf("provenance changed or conflicting fields accepted: %+v / %v", r, err)
			}
		})
	}
}

func TestExecutionFinalDecisionRequiresKnownCatalogRepository(t *testing.T) {
	for _, repo := range []string{"example/skills", "different/skills", ""} {
		t.Run("repository="+repo, func(t *testing.T) {
			input, m, e := executionRepositoryFixture(t)
			d := e.Assessments["first"]
			d.SourceRepo = repo
			d.Disposition = "integrated_existing"
			e.Decisions["first"] = d
			_, err := executionQueue(strings.NewReader(input), &bytes.Buffer{}, m, e)
			if (err == nil) != (repo == "example/skills") {
				t.Fatalf("final decision repository %q accepted without equality: %v", repo, err)
			}
		})
	}
}
