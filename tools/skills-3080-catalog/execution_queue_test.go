package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

func executionFixture() (string, ExecutionManifest, ExecutionEvidence) {
	hash := strings.Repeat("a", 64)
	rows := []Record{{ID: "first", Unit: "github_skill_entry", SHA: hash, BodyRead: true, Path: "first/SKILL.md", Commit: strings.Repeat("c", 40)}, {ID: "alias", Unit: "site_index_entry", SHA: hash, BodyRead: true}, {ID: "absent", Unit: "site_index_entry"}, {ID: "held", Unit: "unread_additional_text_support_shape"}}
	var text bytes.Buffer
	enc := json.NewEncoder(&text)
	for _, r := range rows {
		_ = enc.Encode(r)
	}
	h := sha256.Sum256(text.Bytes())
	return text.String(), ExecutionManifest{CatalogSHA: hex.EncodeToString(h[:]), Records: 4}, ExecutionEvidence{Assessments: map[string]executionIdentity{"first": {ID: "first", SHA: hash, Path: "first/SKILL.md", Commit: strings.Repeat("c", 40)}}, References: map[string][]ExecutionReference{"first": {{"assessment.json", 1}}}, Decisions: map[string]executionIdentity{}}
}

func TestExecutionQueueKeepsAllOccurrencesAndDoesNotApproveEqualBodies(t *testing.T) {
	input, m, e := executionFixture()
	var out bytes.Buffer
	s, err := executionQueue(strings.NewReader(input), &out, m, e)
	if err != nil {
		t.Fatal(err)
	}
	if s.Records != 4 || s.Assessments != 1 || s.Final != 0 || s.Stages["assessed_needs_current_disposition"] != 1 || s.Stages["needs_semantic_and_individual_rights_review"] != 1 || s.Stages["held_execution_review"] != 1 || s.Stages["held_body_unavailable"] != 1 {
		t.Fatalf("incorrect stages: %+v", s)
	}
	dec := json.NewDecoder(&out)
	var first, alias ExecutionRow
	if err = dec.Decode(&first); err != nil {
		t.Fatal(err)
	}
	if err = dec.Decode(&alias); err != nil {
		t.Fatal(err)
	}
	if len(first.References) != 1 || len(alias.References) != 0 || alias.RelatedBody != "first" || alias.Decision != "" || alias.Stage != "needs_semantic_and_individual_rights_review" {
		t.Fatal("body relationship became approval or lost its reference")
	}
}

func TestExecutionQueueRejectsStaleEvidenceAndUnmatchedFinals(t *testing.T) {
	for _, name := range []string{"wrong_catalog", "wrong_count", "unknown_assessment", "wrong_source_hash", "wrong_source_path", "unknown_final", "held_final", "wrong_final_hash", "wrong_final_path", "unfinished_final", "missing_assessment_commit", "missing_final_commit"} {
		t.Run(name, func(t *testing.T) {
			input, m, e := executionFixture()
			switch name {
			case "wrong_catalog":
				m.CatalogSHA = strings.Repeat("0", 64)
			case "wrong_count":
				m.Records--
			case "unknown_assessment":
				e.Assessments["unknown"] = executionIdentity{ID: "unknown", SHA: strings.Repeat("a", 64)}
			case "wrong_source_hash":
				x := e.Assessments["first"]
				x.SHA = strings.Repeat("b", 64)
				e.Assessments["first"] = x
			case "wrong_source_path":
				x := e.Assessments["first"]
				x.Path = "other/SKILL.md"
				e.Assessments["first"] = x
			case "unknown_final":
				e.Decisions["unknown"] = executionIdentity{ID: "unknown", SHA: strings.Repeat("a", 64), Disposition: "excluded"}
			case "held_final":
				e.Decisions["held"] = executionIdentity{ID: "held", SHA: strings.Repeat("a", 64), Disposition: "excluded"}
			case "wrong_final_hash":
				e.Decisions["first"] = executionIdentity{ID: "first", SHA: strings.Repeat("b", 64), Disposition: "excluded"}
			case "wrong_final_path":
				e.Decisions["first"] = executionIdentity{ID: "first", SHA: strings.Repeat("a", 64), Path: "other/SKILL.md", Disposition: "excluded"}
			case "unfinished_final":
				e.Decisions["first"] = executionIdentity{ID: "first", SHA: strings.Repeat("a", 64), Path: "first/SKILL.md", Disposition: "pending"}
			case "missing_assessment_commit":
				x := e.Assessments["first"]
				x.Commit = ""
				e.Assessments["first"] = x
			case "missing_final_commit":
				x := e.Assessments["first"]
				x.Commit = ""
				x.Disposition = "excluded"
				e.Decisions["first"] = x
			}
			if _, err := executionQueue(strings.NewReader(input), &bytes.Buffer{}, m, e); err == nil {
				t.Fatal("invalid universe accepted")
			}
		})
	}
}

func TestExecutionQueueRecordsExactExistingDecisionWithoutApplyingItToAliases(t *testing.T) {
	input, m, e := executionFixture()
	d := e.Assessments["first"]
	d.Disposition = "integrated_existing"
	e.Decisions["first"] = d
	var out bytes.Buffer
	s, err := executionQueue(strings.NewReader(input), &out, m, e)
	if err != nil {
		t.Fatal(err)
	}
	if s.Final != 1 || s.Decisions["integrated_existing"] != 1 || s.Stages["needs_semantic_and_individual_rights_review"] != 1 {
		t.Fatal("recorded decision transferred to another occurrence")
	}
}

func TestExecutionSummarySeparatesAssessmentIntersectionAndFinalOnlyLegacy(t *testing.T) {
	for _, mode := range []string{"assessment_only", "overlap", "legacy_only", "overlap_and_legacy"} {
		t.Run(mode, func(t *testing.T) {
			input, m, e := executionFixture()
			if mode == "overlap" || mode == "overlap_and_legacy" {
				d := e.Assessments["first"]
				d.Disposition = "integrated_existing"
				e.Decisions["first"] = d
			}
			if mode == "legacy_only" || mode == "overlap_and_legacy" {
				e.Decisions["alias"] = executionIdentity{ID: "alias", SHA: strings.Repeat("a", 64), Disposition: "excluded"}
			}
			s, err := executionQueue(strings.NewReader(input), &bytes.Buffer{}, m, e)
			if err != nil {
				t.Fatal(err)
			}
			wantOverlap, wantLegacy := 0, 0
			if mode == "overlap" || mode == "overlap_and_legacy" {
				wantOverlap = 1
			}
			if mode == "legacy_only" || mode == "overlap_and_legacy" {
				wantLegacy = 1
			}
			if s.AssessmentAndFinal != wantOverlap || s.FinalOnly != wantLegacy || s.AssessmentOrFinal != 1+wantLegacy || s.Assessments != s.Stages["assessed_needs_current_disposition"]+s.AssessmentAndFinal || s.Final != s.AssessmentAndFinal+s.FinalOnly {
				t.Fatalf("assessment/final sets conflated: %+v", s)
			}
			if len(s.Limits) == 0 || !strings.Contains(s.Limits[len(s.Limits)-1], "assessment/final intersection") {
				t.Fatal("generated summary omits denominator explanation")
			}
		})
	}
}
