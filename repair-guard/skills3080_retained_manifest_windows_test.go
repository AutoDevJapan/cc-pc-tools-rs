package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkills3080NativeUpdateRetainsOriginalManifestAuthor(t *testing.T) {
	for _, kind := range []string{"valid", "other_repair_author", "other_repair_spec", "other_original_author", "other_original_spec", "other_commit", "changed_purpose", "ordinary_content_review", "protected"} {
		t.Run(kind, func(t *testing.T) {
			purpose, err := os.ReadFile(filepath.Join("testdata", "skills3080-native609-purpose.md"))
			if err != nil || len(purpose) != 6608 || hashBytes(purpose) != "3f3521cc55b7e8037a9cf53922452e0fe1cfa0b3a50b9e0a6104ace698a5bf4e" {
				t.Fatal("unchanged assigned public purpose fixture")
			}
			body, err := os.ReadFile(filepath.Join("..", "docs", "skills-3080-operations", "create-travel3-214717.json"))
			if err != nil || hashBytes(body) != "11070b772c86f7ffb459be2f75eaf6d0a436ce9dfdf0d9247eb81c6e433ede51" {
				t.Fatal("unchanged public manifest")
			}
			m, ok := skillsParseManifest(body)
			if !ok || m.Writer.Job != "20261026-214717-todo-3080" {
				t.Fatal("original manifest author")
			}
			c := skillsPublicScopeFixture()
			c.Domain, c.Instructions = "skills-3080-public-repair-v1", skillsPublicRepairInstructions
			c.Commit = "6fdbdeee3933ca5aa124747a13c70fb94414ff27"
			c.Author = skillsWriter{3080, "20261027-175645-todo-3080", "91a65cb17d2e5c4b18655ecb3126ea1a212ad655e716a6c349321876071a9435", skillsParentRoot}
			c.PurposePath, c.Purpose = "docs/skills-3080-native-update609-task175645.md", string(purpose)
			c.ManifestPath = "docs/skills-3080-operations/create-travel3-214717.json"
			switch kind {
			case "other_repair_author":
				c.Author.Job = "20261027-175646-todo-3080"
			case "other_repair_spec":
				c.Author.SpecSHA = strings.Repeat("f", 64)
			case "other_original_author":
				m.Writer.Job = "20261026-214718-todo-3080"
				body, _ = json.Marshal(m)
			case "other_original_spec":
				m.Writer.SpecSHA = strings.Repeat("f", 64)
				body, _ = json.Marshal(m)
			case "other_commit":
				c.Commit = strings.Repeat("f", 40)
			case "changed_purpose":
				c.Purpose += "changed"
				purpose = []byte(c.Purpose)
			case "ordinary_content_review":
				c.Domain, c.Instructions = "skills-3080-public-review-v1", skillsPublicReviewInstructions
				c.PurposePath, c.Purpose = "", ""
			}
			c.ManifestSHA = hashBytes(body)
			c.ManifestDeclaration = "SKILLS3080_MANIFEST_SHA256 " + c.ManifestSHA
			f := skillsPublicReviewFile{c.ManifestPath, skillsGitBlobID(body), c.ManifestSHA, int64(len(body))}
			c.Files = []skillsPublicReviewFile{f}
			if c.Purpose != "" {
				c.Files = append(c.Files, skillsPublicReviewFile{c.PurposePath, skillsGitBlobID(purpose), hashBytes(purpose), int64(len(purpose))})
			}
			treeID := strings.Repeat("d", 40)
			entries := []object{{"path": "docs", "type": "tree", "mode": "040000"}, {"path": "docs/skills-3080-operations", "type": "tree", "mode": "040000"}}
			for _, v := range c.Files {
				entries = append(entries, object{"path": v.Path, "type": "blob", "mode": "100644", "sha": v.Blob, "size": v.Bytes})
			}
			calls, reads := 0, 0
			gh := func(args ...string) ([]byte, bool) {
				calls++
				var v object
				switch args[3] {
				case "repos/AutoDevJapan/cc-pc-tools-rs":
					v = object{"id": c.RepositoryID, "full_name": c.Repository, "owner": object{"login": "AutoDevJapan"}, "private": false}
				case "repos/AutoDevJapan/cc-pc-tools-rs/git/commits/" + c.Commit:
					v = object{"sha": c.Commit, "tree": object{"sha": treeID}}
				case "repos/AutoDevJapan/cc-pc-tools-rs/git/trees/" + treeID + "?recursive=1":
					v = object{"sha": treeID, "truncated": false, "tree": entries}
				default:
					t.Fatal("unselected public API")
				}
				b, _ := json.Marshal(v)
				return b, true
			}
			fs := skillsFS{blocked: func(string) bool { return kind == "protected" }}
			ok = skillsPublicInputsWith(c, &fs, gh, func(id string) ([]byte, bool) {
				reads++
				if id == f.Blob {
					return body, true
				}
				if len(c.Files) == 2 && id == c.Files[1].Blob {
					return purpose, true
				}
				t.Fatal("unselected local blob")
				return nil, false
			}, kind != "ordinary_content_review")
			if ok != (kind == "valid") {
				t.Fatalf("retained manifest %s accepted=%v", kind, ok)
			}
			if kind == "valid" && (calls != 3 || reads != 2) {
				t.Fatal("positive did not independently prove and read both public inputs")
			}
			if kind == "protected" && reads != 0 {
				t.Fatal("protected public input was opened")
			}
		})
	}
}
