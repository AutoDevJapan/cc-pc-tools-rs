package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionQueueValidatesEveryVisiblePrimaryMemberAgainstCatalog(t *testing.T) {
	for _, change := range []string{"valid", "invented ID", "repository", "commit", "path", "sha256", "git_blob", "bytes", "duplicate member", "missing member", "missing metadata", "absent catalog member"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			first := map[string]any{"inventory_id": "github:" + strings.Repeat("a", 64), "repo": "example/skills", "commit": strings.Repeat("c", 40), "path": "skills/first/SKILL.md", "sha256": strings.Repeat("d", 64), "git_blob": strings.Repeat("e", 40), "bytes": 42}
			second := map[string]any{}
			for key, value := range first {
				second[key] = value
			}
			second["inventory_id"], second["path"] = "github:"+strings.Repeat("b", 64), "skills/second/SKILL.md"
			var catalog bytes.Buffer
			for _, member := range []map[string]any{first, second} {
				if change == "absent catalog member" && member["inventory_id"] == second["inventory_id"] {
					continue
				}
				row := map[string]any{"inventory_unit": "github_skill_entry", "machine_body_read": true}
				for key, value := range member {
					row[key] = value
				}
				if err := json.NewEncoder(&catalog).Encode(row); err != nil {
					t.Fatal(err)
				}
			}
			entry := map[string]any{"inventory_id": first["inventory_id"], "source_repo": first["repo"], "source_commit": first["commit"], "path": first["path"], "source_sha256": first["sha256"], "git_blob": first["git_blob"], "bytes": first["bytes"], "member_metadata": []any{first, second}}
			switch change {
			case "invented ID":
				second["inventory_id"] = "github:" + strings.Repeat("f", 64)
			case "repository":
				second["repo"] = "other/skills"
			case "commit", "git_blob":
				second[change] = strings.Repeat("f", 40)
			case "sha256":
				second[change] = strings.Repeat("f", 64)
			case "path":
				second[change] = "skills/absent/SKILL.md"
			case "bytes":
				second[change] = 43
			case "duplicate member":
				entry["member_metadata"] = []any{first, first}
			case "missing member":
				entry["member_metadata"] = []any{first}
			case "missing metadata":
				delete(entry, "member_metadata")
			}
			catalogSHA := travel3Digest(catalog.Bytes())
			journal := map[string]any{"domain": "skills3080-visible-primary-assessment-journal-v1", "catalog_sha256": catalogSHA, "representative_body_groups": 1, "bound_member_metadata": 2, "entries": []any{entry}}
			write := func(name string, value any) ExecutionArtifact {
				b, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(root, name), b, 0600); err != nil {
					t.Fatal(err)
				}
				return ExecutionArtifact{File: name, SHA: travel3Digest(b)}
			}
			artifact := write(visiblePrimaryJournalFile, journal)
			artifact.Rows = 1
			ledger := write("skills-3080-member-fixture-ledger.json", map[string]any{"entries": []any{}})
			count := 2
			if change == "absent catalog member" {
				count = 1
			}
			manifest := ExecutionManifest{CatalogSHA: catalogSHA, Records: count, Artifacts: []ExecutionArtifact{artifact}, Ledger: ledger}
			evidence, err := loadExecutionEvidence(root, manifest)
			var output bytes.Buffer
			var summary ExecutionSummary
			if err == nil {
				summary, err = executionQueue(bytes.NewReader(catalog.Bytes()), &output, manifest, evidence)
			}
			if change != "valid" {
				if err == nil {
					t.Fatal("changed nested member accepted despite freshly matching artifact and catalog pins")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if summary.Records != 2 || summary.Assessments != 1 || summary.Final != 0 || summary.Stages["needs_semantic_and_individual_rights_review"] != 1 {
				t.Fatal("member verification became assessment credit or adoption")
			}
		})
	}
}
