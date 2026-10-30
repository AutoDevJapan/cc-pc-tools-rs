package main

import (
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// This is a proposed CreateNew input, never an installation receipt. Pin the
// authored bytes and constrain the exact new target and individually read
// references before the normal Creator/absence/mutation checks are attempted.
func TestRentalProposalKeepsExactSevenOutputsAndLimitedRights(t *testing.T) {
	var bundle struct {
		Root    string `json:"root"`
		Targets []struct {
			Name     string `json:"name"`
			Positive string `json:"positive"`
			Negative string `json:"negative"`
			Files    []struct {
				Path    string `json:"path"`
				SHA     string `json:"sha256"`
				Content string `json:"content"`
			} `json:"files"`
		} `json:"targets"`
	}
	raw := loadPublicReceipt(t, "skills-3080-rental-ledger-proposal175645.json", &bundle)
	if travel3Digest(raw) != "3e47165b9c3ad0f2f300bab8aa139137eb2794c4c0bd7a6bacd71217de31a3b4" || bundle.Root != "D:/CC/Skills" || len(bundle.Targets) != 1 {
		t.Fatal("fixed authored bundle/root differs")
	}
	target := bundle.Targets[0]
	if target.Name != "rental-ledger-reconciliation" || target.Positive == "" || target.Negative == "" || len(target.Files) != 7 {
		t.Fatal("exact finite target and scenario pair required")
	}
	files := map[string]string{}
	for _, file := range target.Files {
		if files[file.Path] != "" || travel3Digest([]byte(file.Content)) != file.SHA {
			t.Fatal("authored output duplicated or changed")
		}
		files[file.Path] = file.Content
	}
	if travel3Digest([]byte(files["references/LICENSE.txt"])) != "6f6bb192fb5854c274ca1bd5f3705ad6d4577099d2db95f46c90d0657ffb830d" || len(files["references/LICENSE.txt"]) != 1066 {
		t.Fatal("actual individual MIT notice not preserved byte-for-byte")
	}
	reference := files["references/original-skill.masked.txt"]
	if travel3Digest([]byte(reference)) != "bd49e5f1792aec6a35c85552adffd2f6d14a185abfd059316268661bcdaf62fb" || len(strings.Split(strings.TrimSuffix(reference, "\n"), "\n")) != 205 || strings.Count(reference, "[WITHHELD physical line ") != 39 {
		t.Fatal("actual visible source changed or withheld lines restored")
	}
	var op struct {
		Domain string `json:"domain"`
		Writer struct {
			Task int    `json:"task"`
			Job  string `json:"job"`
			Spec string `json:"spec_sha256"`
		} `json:"writer"`
		Todo     int    `json:"todo_id"`
		Decision string `json:"decision_identity_sha256"`
		Sources  []struct {
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
			Target string `json:"target"`
			SHA    string `json:"sha256"`
			Body   string `json:"content_base64"`
		} `json:"files"`
	}
	raw = loadPublicReceipt(t, "skills-3080-operations/create-rental-ledger-175645.json", &op)
	if travel3Digest(raw) != "9181080be684764ecc89a6c8a77138f45e79507b470fb76c66ecc0fb6c5a3572" || op.Domain != "skills-3080-create-v1" || op.Writer.Task != 3080 || op.Todo != 3080 || op.Writer.Job != "20261027-175645-todo-3080" || op.Writer.Spec != "91a65cb17d2e5c4b18655ecb3126ea1a212ad655e716a6c349321876071a9435" || op.Decision != "32b8d0d676cb58e82ef256fad0a7003ab62d3047db2ca0c71676f09fdac93fe4" || len(op.Sources) != 1 || len(op.Files) != 7 {
		t.Fatal("bounded new-creation purpose/actor differs")
	}
	source := op.Sources[0]
	if source.Repo != "6mile/skills" || source.Commit != "920714f17d81e0a95c7a9bc51faf0a7921cd982d" || source.Path != "skills/mailnike/propclaw/SKILL.md" || source.SHA != "ffcebd1dd25bf25767d69cc242b597309233c1383c65a53281bcbbd2529b1ac0" || source.License.Path != "skills/mailnike/propclaw/LICENSE.txt" || source.License.SHA != "6f6bb192fb5854c274ca1bd5f3705ad6d4577099d2db95f46c90d0657ffb830d" || !source.License.Accepted || source.License.Scope == "" {
		t.Fatal("individual source/notice/purpose absent")
	}
	seen := map[string]bool{}
	for _, file := range op.Files {
		relative, ok := strings.CutPrefix(file.Target, target.Name+"/")
		body, e := base64.StdEncoding.DecodeString(file.Body)
		if !ok || seen[relative] || e != nil || files[relative] == "" || string(body) != files[relative] || travel3Digest(body) != file.SHA {
			t.Fatal("operation escapes target, omits or rewrites authored bytes")
		}
		seen[relative] = true
	}
	var alias map[string]any
	if e := json.Unmarshal([]byte(files["references/source.json"]), &alias); e != nil {
		t.Fatal(e)
	}
	wantRaw, e := json.Marshal(source)
	if e != nil {
		t.Fatal(e)
	}
	var wantSource any
	if e = json.Unmarshal(wantRaw, &wantSource); e != nil || !reflect.DeepEqual(alias["source"], wantSource) || alias["reformulation_writer"] != op.Writer.Job || alias["source_bytes"] != float64(12496) || alias["source_git_blob"] != "8b2648997678e0840c819e9fc023efb20b4b1ebc" {
		t.Fatal("canonical renamed masked-reference binding differs from the immutable creation source")
	}
	var prep map[string]any
	loadPublicReceipt(t, "skills-3080-rental-ledger-preparation175645.json", &prep)
	for _, field := range []string{"qualified_target_absence_completed", "full_Creator_review_received", "whole16_complete", "independent_authorship_third_party_or_current_legal_claims_proven", "full_current_catalog_discovery_or_context_budget_proven"} {
		if prep[field] != false {
			t.Fatal("unproved qualification/rights/acceptance promoted")
		}
	}
	for _, field := range []string{"created_files", "new_adoptions", "central_operations"} {
		if prep[field] != float64(0) {
			t.Fatal("proposal became actual installation")
		}
	}
}
