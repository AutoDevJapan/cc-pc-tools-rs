package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkills3080CreationUsesTheReviewedPublicationAndRegisteredAuthor(t *testing.T) {
	for _, kind := range []string{"valid", "changed_commit_same_manifest", "changed_path", "changed_sha", "unregistered_author", "future_author", "comparison_only", "legacy_typed_valid", "legacy_typed_changed_commit", "legacy_untyped_valid"} {
		t.Run(kind, func(t *testing.T) {
			raw := skillsPublicWireFixture()
			m, ok := skillsParseManifest(raw)
			if !ok {
				t.Fatal("public fixture")
			}
			c := skillsPublicScopeFixture()
			c.ManifestSHA = hashBytes(raw)
			c.ManifestDeclaration = "SKILLS3080_MANIFEST_SHA256 " + c.ManifestSHA
			c.Files[0] = skillsPublicReviewFile{Path: c.ManifestPath, Blob: skillsGitBlobID(raw), SHA: c.ManifestSHA, Bytes: int64(len(raw))}
			creator := c.Author
			creator.Job = "20261025-120000-todo-3080"
			if strings.HasPrefix(kind, "legacy_") {
				m = skillsTestManifest()
				m.Writer = c.Author
				creator = c.Author
			}
			r := skillsRequest{Writer: creator, ManifestCommit: c.Commit, ManifestPath: c.ManifestPath, ManifestSHA: c.ManifestSHA}
			row := skillsTask{ID: 3080, Jobs: []string{c.Author.Job, creator.Job}}
			if creator.Job == c.Author.Job {
				row.Jobs = []string{creator.Job}
			}
			switch kind {
			case "changed_commit_same_manifest", "legacy_typed_changed_commit":
				r.ManifestCommit = strings.Repeat("d", 40)
			case "changed_path":
				r.ManifestPath += ".other.json"
			case "changed_sha":
				r.ManifestSHA = strings.Repeat("e", 64)
			case "unregistered_author":
				row.Jobs = []string{creator.Job}
			case "future_author":
				row.Jobs = []string{creator.Job, c.Author.Job}
			case "comparison_only":
				c.Instructions = skillsPublicReviewComparisonInstructions
				c.Comparison = &skillsPublicReviewComparison{Commit: strings.Repeat("f", 40), Files: []skillsPublicReviewBaseFile{{Path: c.ManifestPath, Absent: true}}}
			}
			spec, err := json.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := skillsParsePublicReview(spec); !ok {
				t.Fatal("typed control review did not parse")
			}
			if kind == "legacy_untyped_valid" {
				spec = []byte("legacy registered content review")
			}
			bound, accepted := skillsBindCreationReview(m, r, skillsReceived{Spec: spec}, skillsRegistry{Tasks: map[int]skillsTask{3080: row}})
			want := kind == "valid" || kind == "legacy_typed_valid" || kind == "legacy_untyped_valid"
			if accepted != want {
				t.Fatalf("publication binding accepted=%v want=%v", accepted, want)
			}
			if accepted && m.publicV2 && (bound.Writer != creator || bound.proposalAuthor != c.Author) {
				t.Fatal("creator and original author conflated")
			}
		})
	}
}
