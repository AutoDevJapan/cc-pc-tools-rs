package main

import (
	"encoding/json"
	"testing"
)

func TestSkills3080RepairPurposeDelivery(t *testing.T) {
	c := skillsPublicScopeFixture()
	c.Domain, c.Instructions = "skills-3080-public-repair-v1", skillsPublicRepairInstructions
	raw, _ := json.Marshal(c)
	var fields map[string]any
	json.Unmarshal(raw, &fields)
	p := "# 固定PUBLICの実装目標\n旧版を残して改訂する。\n"
	path := "docs/skills-3080-purpose-test.md"
	file := skillsPublicReviewFile{path, skillsGitBlobID([]byte(p)), hashBytes([]byte(p)), int64(len(p))}
	fields["purpose"], fields["purpose_path"] = p, path
	fields["files"] = append(c.Files, file)
	for _, kind := range []string{"valid", "old", "body_changed", "path_changed", "length_changed", "sha_changed", "blob_changed", "review", "empty", "private"} {
		t.Run(kind, func(t *testing.T) {
			b, _ := json.Marshal(fields)
			var f map[string]any
			json.Unmarshal(b, &f)
			switch kind {
			case "old":
				delete(f, "purpose")
				delete(f, "purpose_path")
			case "body_changed":
				f["purpose"] = p + "posthoc"
			case "path_changed":
				f["purpose_path"] = path + ".other"
			case "length_changed":
				f["files"].([]any)[1].(map[string]any)["bytes"] = len(p) + 1
			case "sha_changed":
				f["files"].([]any)[1].(map[string]any)["sha256"] = c.ManifestSHA
			case "blob_changed":
				f["files"].([]any)[1].(map[string]any)["blob"] = c.Commit
			case "review":
				f["domain"], f["instructions"] = c.Domain[:len(c.Domain)-len("repair-v1")]+"review-v1", skillsPublicReviewInstructions
			case "empty":
				f["purpose"] = ""
			case "private":
				f["purpose_path"] = "private/notes.md"
			}
			b, _ = json.Marshal(f)
			_, ok := skillsParsePublicRepair(b)
			if ok != (kind == "valid" || kind == "old") {
				t.Fatalf("%s accepted=%v", kind, ok)
			}
			if kind == "review" {
				if _, ok := skillsParsePublicReview(b); ok {
					t.Fatal("review acquired repair purpose")
				}
			}
		})
	}
}
