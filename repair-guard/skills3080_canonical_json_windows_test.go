package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSkills3080StrictProtocolRejectsCaseAliases(t *testing.T) {
	for _, kind := range []string{"canonical", "domain_alias", "domain_duplicate_case", "nested_alias", "nested_duplicate_case", "array_alias", "array_duplicate_case", "unknown_field", "exact_duplicate"} {
		t.Run(kind, func(t *testing.T) {
			c := skillsPublicScopeFixture()
			raw, _ := json.Marshal(c)
			switch kind {
			case "domain_alias":
				raw = []byte(strings.Replace(string(raw), `"domain":`, `"Domain":`, 1))
			case "domain_duplicate_case":
				raw = []byte(strings.Replace(string(raw), `"domain":`, `"Domain":"synthetic-private-scope","domain":`, 1))
			case "nested_alias":
				raw = []byte(strings.Replace(string(raw), `"workspace":`, `"Workspace":`, 1))
			case "nested_duplicate_case":
				raw = []byte(strings.Replace(string(raw), `"workspace":`, `"Workspace":"synthetic-private-source","workspace":`, 1))
			case "array_alias":
				raw = []byte(strings.Replace(string(raw), `"blob":`, `"Blob":`, 1))
			case "array_duplicate_case":
				raw = []byte(strings.Replace(string(raw), `"blob":`, `"Blob":"synthetic-private-object","blob":`, 1))
			case "unknown_field":
				raw = append([]byte(`{"synthetic_unknown":true,`), raw[1:]...)
			case "exact_duplicate":
				raw = []byte(strings.Replace(string(raw), `"domain":`, `"domain":"synthetic-private-scope","domain":`, 1))
			}
			_, ok := skillsParsePublicReview(raw)
			if ok != (kind == "canonical") {
				t.Fatalf("typed %s accepted=%v", kind, ok)
			}
		})
	}
}

func TestSkills3080StrictAcceptanceCannotOverrideFalseWithAlias(t *testing.T) {
	for _, key := range []string{"accepted", "must", "head"} {
		t.Run(key, func(t *testing.T) {
			var value struct {
				Accepted bool   `json:"accepted"`
				Must     int    `json:"must"`
				Head     string `json:"head"`
			}
			raw := `{"accepted":true,"must":0,"head":"fixed"}`
			if !skillsStrict([]byte(raw), &value) {
				t.Fatal("canonical control not exercised")
			}
			old := map[string]string{"accepted": `"Accepted":false,"accepted":`, "must": `"Must":3,"must":`, "head": `"Head":"other","head":`}[key]
			raw = strings.Replace(raw, `"`+key+`":`, old, 1)
			if skillsStrict([]byte(raw), &value) {
				t.Fatal("case alias overrode a contradictory acceptance field")
			}
		})
	}
}

func TestSkills3080CanonicalFieldsPreserveOptionalAndMapValues(t *testing.T) {
	type protocol struct {
		Enabled bool                              `json:"enabled,omitempty"`
		Writer  *skillsWriter                     `json:"writer,omitempty"`
		Files   map[string]skillsPublicReviewFile `json:"files"`
	}
	var v protocol
	raw := []byte(`{"enabled":false,"writer":null,"files":{"arbitrary-data-key":{"path":"public-path","blob":"id","sha256":"digest","bytes":1}}}`)
	if !skillsStrict(raw, &v) {
		t.Fatal("canonical explicit zero/null or a data map key was rejected")
	}
	bad := strings.Replace(string(raw), `"path":`, `"Path":`, 1)
	if skillsStrict([]byte(bad), &v) {
		t.Fatal("typed map value accepted an aliased schema key")
	}
}
