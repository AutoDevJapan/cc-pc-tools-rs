package main

import (
	"bytes"
	"encoding/json"
	"testing"
)

func skillsPublicWireFixture() []byte {
	m := skillsTestManifest()
	b, _ := json.Marshal(object{"domain": "skills-3080-create-v2", "sources": m.Sources, "files": m.Files})
	return b
}

func TestSkills3080PublicManifestWireHasNoInternalIdentity(t *testing.T) {
	m, ok := skillsParseManifest(skillsPublicWireFixture())
	if !ok {
		t.Fatal("source-only public manifest rejected")
	}
	if m.Writer != (skillsWriter{}) || m.Decision != "" {
		t.Fatal("public bytes invented private authority")
	}
	b, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{`"writer"`, `"todo_id"`, `"job"`, `"workspace"`, `"spec_sha256"`, `"decision_identity_sha256"`} {
		if bytes.Contains(b, []byte(name)) {
			t.Fatalf("internal field published: %s", name)
		}
	}
}

func TestSkills3080PublicManifestRejectsInternalAndUnknownFields(t *testing.T) {
	for _, key := range []string{"writer", "Writer", "todo_id", "decision_identity_sha256", "manifest_author", "private_binding", "unknown"} {
		t.Run(key, func(t *testing.T) {
			v, _ := conversationJSON(skillsPublicWireFixture())
			v[key] = nil
			b, _ := json.Marshal(v)
			if _, ok := skillsParseManifest(b); ok {
				t.Fatal("internal/unknown field accepted by public schema")
			}
		})
	}
}
