package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const skillsComparisonTestInstructions = "comparisonがある場合は、選択した同じpathの固定baseとheadだけを比較する。宣言したbaseで存在しないpathは追加として扱う。変更と必要な支持の読了範囲を区別し、変更のない範囲を全読したとは報告しない。"

func TestSkills3080PublicValidatorNamespace(t *testing.T) {
	for _, path := range []string{"tools/skills-3080-catalog/forward11_deployment_test.go", "tools/skills-3080-catalog/main.go", "tools/skills-3080-catalog/go.mod"} {
		if !skillsPublicReviewPath(path) {
			t.Errorf("original validator rejected: %s", path)
		}
	}
	for _, path := range []string{"tools/other/main.go", "repair-guard/main_windows.go", "tools/skills-3080-catalog/private/secret.go", "tools/skills-3080-catalog/.env", "tools/skills-3080-catalog/tool.exe", "tools/skills-3080-catalog/x.go.txt", "tools/skills-3080-catalog/../other.go"} {
		if skillsPublicReviewPath(path) {
			t.Errorf("unrelated input admitted: %s", path)
		}
	}
}

func TestSkills3080ComparisonResultCannotBecomeCreationAcceptance(t *testing.T) {
	c := skillsPublicScopeFixture()
	received := skillsReceived{Metadata: object{"model": "gpt-6-luna", "effort": "max"}, Native: []object{{"model": "gpt-6-luna", "effort": "max"}}, Body: []byte(`SKILLS3080_ACCEPTANCE {"domain":"skills-3080-review-v1","manifest_sha256":"` + c.ManifestSHA + `","accepted":true,"must":0}`)}
	received.Spec, _ = json.Marshal(c)
	if !skillsReviewAccepted(received, c.ManifestSHA, "gpt-6.1-sol") {
		t.Fatal("control content review not exercised")
	}
	c.Instructions = skillsPublicReviewComparisonInstructions
	c.Comparison = &skillsPublicReviewComparison{Commit: strings.Repeat("2", 40), Files: []skillsPublicReviewBaseFile{{Path: c.ManifestPath, Absent: true}}}
	received.Spec, _ = json.Marshal(c)
	if _, ok := skillsParsePublicReview(received.Spec); !ok {
		t.Fatal("comparison control is invalid")
	}
	if skillsReviewAccepted(received, c.ManifestSHA, "gpt-6.1-sol") {
		t.Fatal("raw change review became whole-output acceptance")
	}
}

func TestSkills3080PublicComparisonProvesBothSelectedTrees(t *testing.T) {
	for _, kind := range []string{"valid_unmerged", "valid_added", "no_ancestor", "identical", "wrong_base", "wrong_merge_base", "base_commit", "base_tree", "base_symlink", "base_sha", "base_bytes", "base_blob", "undeclared_old_file", "false_absence", "different_path", "missing_base_path", "duplicate_base", "other_namespace", "protected", "wrong_instructions"} {
		t.Run(kind, func(t *testing.T) {
			c := skillsPublicScopeFixture()
			m := skillsTestManifest()
			m.Writer = c.Author
			head, _ := json.Marshal(m)
			before := append(append([]byte{}, head...), '\n')
			f := skillsPublicReviewFile{c.ManifestPath, skillsGitBlobID(head), hashBytes(head), int64(len(head))}
			c.Files = []skillsPublicReviewFile{f}
			c.ManifestSHA = f.SHA
			c.ManifestDeclaration = "SKILLS3080_MANIFEST_SHA256 " + f.SHA
			baseID := strings.Repeat("2", 40)
			old := object{"path": f.Path, "blob": skillsGitBlobID(before), "sha256": hashBytes(before), "bytes": len(before)}
			if kind == "valid_added" || kind == "false_absence" {
				old = object{"path": f.Path, "absent": true}
			}
			comparison := object{"commit": baseID, "files": []object{old}}
			raw, _ := json.Marshal(c)
			var contract object
			json.Unmarshal(raw, &contract)
			contract["comparison"] = comparison
			contract["instructions"] = skillsPublicReviewInstructions + skillsComparisonTestInstructions
			switch kind {
			case "wrong_base":
				comparison["commit"] = strings.Repeat("3", 40)
			case "base_sha":
				old["sha256"] = strings.Repeat("3", 64)
			case "base_bytes":
				old["bytes"] = len(before) + 1
			case "base_blob":
				old["blob"] = strings.Repeat("3", 40)
			case "different_path":
				old["path"] = "docs/skills-3080-other.md"
			case "missing_base_path":
				comparison["files"] = []object{}
			case "duplicate_base":
				comparison["files"] = []object{old, old}
			case "other_namespace":
				old["path"] = "tools/unrelated/main.go"
			case "wrong_instructions":
				contract["instructions"] = skillsPublicReviewInstructions
			}
			raw, _ = json.Marshal(contract)
			parsed, valid := skillsParsePublicReview(raw)
			want := kind == "valid_unmerged" || kind == "valid_added"
			if !valid {
				if want {
					t.Fatal("valid fixed comparison rejected before public proof")
				}
				return
			}
			headTree, baseTree := strings.Repeat("4", 40), strings.Repeat("5", 40)
			file := func(blob string, size int64) object {
				return object{"path": f.Path, "type": "blob", "mode": "100644", "sha": blob, "size": size}
			}
			oldFile := file(skillsGitBlobID(before), int64(len(before)))
			if kind == "base_symlink" {
				oldFile["mode"] = "120000"
			}
			parents := []object{{"path": "docs", "type": "tree", "mode": "040000"}, {"path": "docs/skills-3080-operations", "type": "tree", "mode": "040000"}}
			oldEntries := append(append([]object{}, parents...), oldFile)
			if kind == "valid_added" || kind == "undeclared_old_file" {
				oldEntries = parents
			}
			calls, reads := 0, 0
			gh := func(args ...string) ([]byte, bool) {
				calls++
				var value object
				endpoint := args[3]
				switch endpoint {
				case "repos/AutoDevJapan/cc-pc-tools-rs":
					value = object{"id": c.RepositoryID, "full_name": c.Repository, "owner": object{"login": "AutoDevJapan"}, "private": false}
				case "repos/AutoDevJapan/cc-pc-tools-rs/git/commits/" + c.Commit:
					value = object{"sha": c.Commit, "tree": object{"sha": headTree}}
				case "repos/AutoDevJapan/cc-pc-tools-rs/git/commits/" + baseID:
					id := baseID
					if kind == "base_commit" {
						id = strings.Repeat("6", 40)
					}
					value = object{"sha": id, "tree": object{"sha": baseTree}}
				case "repos/AutoDevJapan/cc-pc-tools-rs/git/trees/" + headTree + "?recursive=1":
					value = object{"sha": headTree, "truncated": false, "tree": append(append([]object{}, parents...), file(f.Blob, f.Bytes))}
				case "repos/AutoDevJapan/cc-pc-tools-rs/git/trees/" + baseTree + "?recursive=1":
					id := baseTree
					if kind == "base_tree" {
						id = strings.Repeat("6", 40)
					}
					value = object{"sha": id, "truncated": false, "tree": oldEntries}
				case "repos/AutoDevJapan/cc-pc-tools-rs/compare/" + baseID + "..." + c.Commit:
					value = object{"status": "ahead", "base": baseID, "merge_base": baseID}
					if kind == "no_ancestor" {
						value["status"] = "diverged"
					}
					if kind == "identical" {
						value["status"] = "identical"
					}
					if kind == "wrong_merge_base" {
						value["merge_base"] = strings.Repeat("6", 40)
					}
				default:
					return nil, false
				}
				b, _ := json.Marshal(value)
				return b, true
			}
			fs := skillsFS{blocked: func(string) bool { return kind == "protected" }}
			ok := skillsPublicReviewInputsWith(parsed, &fs, gh, func(id string) ([]byte, bool) {
				reads++
				if id == f.Blob {
					return head, true
				}
				if id == skillsGitBlobID(before) {
					return before, true
				}
				return nil, false
			})
			if ok != want {
				t.Fatalf("comparison %s accepted=%v", kind, ok)
			}
			if want && (calls != 6 || reads < 1) {
				t.Fatal("positive did not prove both commits and selected content")
			}
		})
	}
}
