package main

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func skillsPublicAliasFixture(t *testing.T) (skillsManifest, skillsSavedSource) {
	t.Helper()
	m := skillsTestManifest()
	raw := []byte("public original\n")
	notice := []byte("public MIT notice\n")
	s := m.Sources[0]
	s.Path = "skills/original/SKILL.md"
	s.SHA = hashBytes(raw)
	s.License.SHA = hashBytes(notice)
	m.Sources[0] = s
	meta, _ := json.Marshal(object{"schema": "skills-3080-source-v2", "source": s, "source_bytes": len(raw), "source_git_blob": skillsGitBlobID(raw)})
	for path, body := range map[string][]byte{"sample/references/source.json": meta, "sample/references/LICENSE.txt": notice, "sample/references/original-skill.masked.txt": raw} {
		m.Files = append(m.Files, skillsOutput{Target: path, SHA: hashBytes(body), Content: base64.StdEncoding.EncodeToString(body)})
	}
	b, _ := json.Marshal(object{"domain": "skills-3080-create-v2", "sources": m.Sources, "files": m.Files})
	parsed, ok := skillsParseManifest(b)
	if !ok {
		t.Fatal("public alias input")
	}
	return parsed, skillsSavedSource{Path: s.Path, Bytes: int64(len(raw)), Blob: skillsGitBlobID(raw)}
}

func TestSkills3080PublicAliasBindsSourceWithoutPublishingWriter(t *testing.T) {
	m, row := skillsPublicAliasFixture(t)
	bound, ok := skillsMaskedBindings(m)
	if !ok || len(bound) != 1 || !skillsMaskedMetadataMatches(row, m.Files, "sample/references/original-skill.masked.txt") {
		t.Fatal("public source tuple/bytes/blob binding lost")
	}
	wrong := row
	wrong.Bytes++
	if skillsMaskedMetadataMatches(wrong, m.Files, "sample/references/original-skill.masked.txt") {
		t.Fatal("wrong acquired bytes accepted")
	}
}

func TestSkills3080PublicSourceMetadataRejectsInternalBinding(t *testing.T) {
	for _, key := range []string{"reformulation_writer", "Reformulation_Writer", "writer", "job", "workspace", "spec_sha256", "decision_identity_sha256", "manifest_author"} {
		t.Run(key, func(t *testing.T) {
			m, _ := skillsPublicAliasFixture(t)
			for i, f := range m.Files {
				if f.Target == "sample/references/source.json" {
					v, _ := conversationJSON(f.body)
					v[key] = "internal"
					b, _ := json.Marshal(v)
					m.Files[i].Content = base64.StdEncoding.EncodeToString(b)
					m.Files[i].SHA = hashBytes(b)
				}
			}
			b, _ := json.Marshal(m)
			if _, ok := skillsParseManifest(b); ok {
				t.Fatal("private metadata accepted by public manifest")
			}
		})
	}
}
