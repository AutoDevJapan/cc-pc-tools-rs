package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func apacheFixture(t *testing.T) (string, map[string]any) {
	t.Helper()
	d := t.TempDir()
	notice, err := os.ReadFile("../../docs/skills-3080-docker-APACHE.txt")
	if err != nil {
		t.Fatal(err)
	}
	body := []byte("---\nname: sample\nlicense: Apache-2.0\n---\nPublic fixture procedure.\n")
	var packed bytes.Buffer
	z := gzip.NewWriter(&packed)
	if _, err = z.Write(body); err != nil {
		t.Fatal(err)
	}
	if err = z.Close(); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{"body.gz": packed.Bytes(), "notice.txt": notice} {
		if err = os.WriteFile(filepath.Join(d, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	commit := strings.Repeat("a", 40)
	records, _ := json.Marshal([]SourceRecord{{Repo: "docker/skills", Commit: commit, Path: "skills/sample/SKILL.md", SHA: digest(body), ContentPath: "body.gz"}})
	if err = os.WriteFile(filepath.Join(d, "records.json"), records, 0600); err != nil {
		t.Fatal(err)
	}
	tree := strings.Repeat("b", 40)
	plan := map[string]any{"source_repo": "docker/skills", "source_commit": commit, "source_tree": tree,
		"license": map[string]any{"identifier": "Apache-2.0", "preserved_notice": "notice.txt", "source_sha256": digest(notice)},
		"targets": []any{map[string]any{"name": "sample", "description": "sandboxの設定を確認するときに使う。", "positive": "check owned sandbox", "negative": "change unrelated service", "source_path": "skills/sample/SKILL.md", "source_sha256": digest(body), "source_url": "https://github.com/docker/skills/blob/" + commit + "/skills/sample/SKILL.md", "retrieved_at": "2026-10-20T08:40:53Z", "guidance": []string{"必要な設定だけを調べる。"}, "adaptations": "Japanese adaptation; no upstream code executed.", "upstream_status": "fixed_public", "upstream_status_adoption_reason": "Maintain this reviewed fixed procedure.", "upstream_disable_model_invocation": false, "derivative_invocation": map[string]any{"mode": "implicit_with_narrow_trigger", "reason": "Only this requested workflow."}, "individual_license_review": map[string]any{"source_tree": tree, "tree_complete": true, "license_paths": []string{"LICENSE"}, "nested_license_notice_copying_paths": []string{}, "scope": "selected entrypoint only", "checked_at": "2026-10-21T00:00:00Z"}}}}
	return d, plan
}

func renderFixture(t *testing.T, d string, p map[string]any) (Bundle, error) {
	t.Helper()
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(d, "plan.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return render([]string{filepath.Join(d, "plan.json")}, filepath.Join(d, "records.json"), d)
}

func TestApachePreservesNoticeAndMarksModifiedEntrypoint(t *testing.T) {
	d, p := apacheFixture(t)
	b, err := renderFixture(t, d, p)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range b.Targets[0].Files {
		files[f.Path] = f.Content
	}
	for _, text := range []string{"Docker, Inc.", "Apache-2.0", "日本語へ改変", "公式版ではない", "skills/sample/SKILL.md", strings.Repeat("a", 40)} {
		if !strings.Contains(files["SKILL.md"], text) {
			t.Fatalf("modified entrypoint lost %q", text)
		}
	}
	if strings.Contains(files["references/sources.md"], "許諾: MIT") {
		t.Fatal("Apache relabeled as MIT")
	}
	notice, _ := os.ReadFile(filepath.Join(d, "notice.txt"))
	if files["references/LICENSE.txt"] != string(notice) {
		t.Fatal("notice changed")
	}
}

func TestApacheRejectsIncompleteIndividualRightsEvidence(t *testing.T) {
	for _, which := range []string{"null", "empty", "tree", "incomplete", "paths", "date", "notice"} {
		t.Run(which, func(t *testing.T) {
			d, p := apacheFixture(t)
			target := p["targets"].([]any)[0].(map[string]any)
			review := target["individual_license_review"].(map[string]any)
			switch which {
			case "null":
				target["individual_license_review"] = nil
			case "empty":
				target["individual_license_review"] = map[string]any{}
			case "tree":
				review["source_tree"] = strings.Repeat("c", 40)
			case "incomplete":
				review["tree_complete"] = false
			case "paths":
				review["license_paths"] = []string{}
			case "date":
				review["checked_at"] = "yesterday"
			case "notice":
				review["nested_license_notice_copying_paths"] = []string{"NOTICE"}
			}
			if _, err := renderFixture(t, d, p); err == nil {
				t.Fatal("unreviewed license scope accepted")
			}
		})
	}
}
