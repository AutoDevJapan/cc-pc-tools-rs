package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// Target names follow the actual Skill naming contract: lowercase letters,
// digits and separated hyphens. A numeric first character does not change any
// source, ownership, permission, alias or exclusive-creation requirement.
func TestSkills3080DigitPrefixedManifestTargets(t *testing.T) {
	for _, name := range []string{"3d-cloth-material", "3d-high-resolution-textures", "2026-review", "3"} {
		t.Run(name, func(t *testing.T) {
			m := skillsTestManifest()
			body := []byte("---\nname: " + name + "\ndescription: 合成テスト用\n---\n公開資料から作成した例。\n")
			m.Files[0] = skillsOutput{Target: name + "/SKILL.md", SHA: hashBytes(body), Content: base64.StdEncoding.EncodeToString(body)}
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := skillsParseManifest(raw)
			if !ok || len(got.Files) != 1 || got.Files[0].Target != name+"/SKILL.md" || string(got.Files[0].body) != string(body) {
				t.Fatal("valid digit-prefixed Skill manifest rejected")
			}
		})
	}
	for _, name := range []string{"-3d", "3d-", "3d--cloth", "3D-cloth", "3d_cloth", "3d.cloth", "3d/../escape", "3d//escape", `3d\cloth`, "3d:cloth", "3d /escape", "3d/aux", "3d/COM1"} {
		t.Run("reject-"+strings.ReplaceAll(name, "/", "_"), func(t *testing.T) {
			m := skillsTestManifest()
			m.Files[0].Target = name + "/SKILL.md"
			raw, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := skillsParseManifest(raw); ok {
				t.Fatal("noncanonical/reserved target accepted")
			}
		})
	}
}

func TestSkills3080Merged3DInputParsesWithoutCentralAccess(t *testing.T) {
	raw, err := os.ReadFile("../docs/skills-3080-operations/create-material-camera8-074042.json")
	if err != nil {
		t.Fatal(err)
	}
	if hashBytes(raw) != "e41f3b1b908bf097c38f8260ef667b35caadd06dfb1b47ca6ae0faec657a2a81" {
		t.Fatal("fixed public fixture differs")
	}
	m, ok := skillsParseManifest(raw)
	if !ok || len(m.Sources) != 8 || len(m.Files) != 48 {
		t.Fatal("the reviewed merged8/48 input was rejected")
	}
	// Parsing cannot qualify source ownership or check absence: those later
	// operations must still run through the original purpose-limited entry.
	if !skillsSourceClaimsPreserved(m) {
		t.Fatal("source/notice preservation is missing")
	}
}

func TestSkills3080KnownA5daSoftwareBaseline(t *testing.T) {
	raw, err := os.ReadFile("../docs/evidence/skills-3080-guard-installed-a5da.json")
	if err != nil {
		t.Fatal(err)
	}
	if hashBytes(raw) != "106d4f8dbb3e73abb24b1f4ebc85b0dc729414068328ee15e733e7df31699d32" {
		t.Fatal("independent installed baseline record differs")
	}
	var v struct {
		Digest   string `json:"observed_installed_sha256"`
		Revision string `json:"public_source_commit"`
		General  *bool  `json:"general_access_granted"`
		Registry *bool  `json:"original_registry_recovered"`
	}
	if json.Unmarshal(raw, &v) != nil || v.Digest != "ba35332c45b5bb9f35c4465ee209c123be1309ec554ef0976c8240d232c451c1" || skillsGuardBaselines[v.Digest] != v.Revision || v.General == nil || *v.General || v.Registry == nil || *v.Registry {
		t.Fatal("known software baseline or unchanged data boundary differs")
	}
	if _, ok := skillsGuardBaselines[strings.Repeat("f", 64)]; ok {
		t.Fatal("unknown software accepted")
	}
}
