package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func skillsTestManifest() skillsManifest {
	body := []byte("---\nname: sample\ndescription: 合成テスト用\n---\n公開資料から作成した例。\n")
	return skillsManifest{Domain: "skills-3080-create-v1", Todo: 3080, Decision: skillsDecisionSHA,
		Writer:  skillsWriter{Task: 3080, Job: "20261024-235901-todo-3080", SpecSHA: strings.Repeat("1", 64), Workspace: skillsParentRoot},
		Sources: []skillsSource{{Repository: "public/example", Commit: strings.Repeat("a", 40), Path: "skills/sample/SKILL.md", SHA: strings.Repeat("2", 64), Acquisition: "already-acquired", License: skillsLicense{Path: "LICENSE", SHA: strings.Repeat("3", 64), Scope: "selected entrypoint", Accepted: true}}},
		Files:   []skillsOutput{{Target: "sample/SKILL.md", SHA: hashBytes(body), Content: base64.StdEncoding.EncodeToString(body)}}}
}

func TestSkills3080ManifestContract(t *testing.T) {
	good := skillsTestManifest()
	raw, _ := json.Marshal(good)
	if _, ok := skillsParseManifest(raw); !ok {
		t.Fatal("positive manifest rejected")
	}
	cases := map[string]func(*skillsManifest){
		"scope":                func(m *skillsManifest) { m.Decision = strings.Repeat("0", 64) },
		"parent":               func(m *skillsManifest) { m.Todo = 3669 },
		"writer":               func(m *skillsManifest) { m.Writer.Task = 3674 },
		"empty-source":         func(m *skillsManifest) { m.Sources = nil },
		"source-hash":          func(m *skillsManifest) { m.Sources[0].SHA = strings.Repeat("0", 64) },
		"source-path-escape":   func(m *skillsManifest) { m.Sources[0].Path = "../secrets.json" },
		"license-not-accepted": func(m *skillsManifest) { m.Sources[0].License.Accepted = false },
		"license-scope-empty":  func(m *skillsManifest) { m.Sources[0].License.Scope = "" },
		"held647":              func(m *skillsManifest) { m.Sources[0].Acquisition = "held647" },
		"output-hash":          func(m *skillsManifest) { m.Files[0].SHA = strings.Repeat("4", 64) },
		"output-escape":        func(m *skillsManifest) { m.Files[0].Target = "sample/../../outside" },
		"output-alias":         func(m *skillsManifest) { m.Files[0].Target = "sample/SKILL.md:alias" },
		"card":                 func(m *skillsManifest) { m.Files[0].Target = "credit-card/SKILL.md" },
		"empty-bytes":          func(m *skillsManifest) { m.Files[0].Content = "" },
		"missing-entrypoint":   func(m *skillsManifest) { m.Files[0].Target = "sample/references/a.md" },
		"duplicate":            func(m *skillsManifest) { m.Files = append(m.Files, m.Files[0]) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var m skillsManifest
			_ = json.Unmarshal(raw, &m)
			mutate(&m)
			b, _ := json.Marshal(m)
			if _, ok := skillsParseManifest(b); ok {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
	for _, b := range [][]byte{[]byte(`{"domain":"x","domain":"x"}`), append(raw, []byte(` {}`)...), []byte(`{}`), []byte(`null`), []byte(`{"secret":"do not return"}`)} {
		if _, ok := skillsParseManifest(b); ok {
			t.Fatal("invalid JSON accepted")
		}
	}
}
