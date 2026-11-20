package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

type ownerSharedSkillsFixture struct {
	base    string
	central string
	layout  ownerSharedSkillsLayout
}

// The fixture root is canonical, so lexical and physical names agree except
// through the junctions that each test creates deliberately.
func ownerSharedSkillsNewFixture(t *testing.T, agents ...string) ownerSharedSkillsFixture {
	t.Helper()
	base, err := ownerPhysicalPath(currentShortFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	central := filepath.Join(base, "central")
	skill := filepath.Join(central, "public-skill")
	if err := os.MkdirAll(skill, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: public-skill\ndescription: synthetic fixture\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	layout := ownerSharedSkillsLayout{central: central}
	for _, agent := range agents {
		layout.links = append(layout.links, filepath.Join(base, "profile", agent, "skills"))
	}
	return ownerSharedSkillsFixture{base: base, central: central, layout: layout}
}

// mklink /J needs no privilege. Removing the junction itself never touches its
// target, and it runs before the fixture directory is removed.
func ownerSharedSkillsJunction(t *testing.T, link, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cmd", "/d", "/c", "mklink", "/J", link, target)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("junction fixture failed: %v: %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
}

func ownerSharedSkillsEnvFor(f ownerSharedSkillsFixture, classified *[]string) ownerSharedSkillsEnv {
	return ownerSharedSkillsEnv{
		legacy:   func(string) ownerLegacyState { return ownerLegacyAbsent },
		physical: ownerRulePhysicalPath,
		classify: func(target string) ownerPathVerdict {
			*classified = append(*classified, target)
			if within(f.central, target) {
				return ownerRuleClear("fixture_central")
			}
			return ownerRuleUnknown("fixture_outside_central")
		},
	}
}

func TestOwnerSharedSkillsLinkAdmitsOnlyTheVerifiedCentralJunction(t *testing.T) {
	f := ownerSharedSkillsNewFixture(t, "first", "second")
	for _, link := range f.layout.links {
		ownerSharedSkillsJunction(t, link, f.central)
	}
	first, second := f.layout.links[0], f.layout.links[1]
	for _, tc := range []struct{ name, path, target string }{
		{"link root", first, f.central},
		{"existing definition", filepath.Join(first, "public-skill", "SKILL.md"), filepath.Join(f.central, "public-skill", "SKILL.md")},
		{"second agent", filepath.Join(second, "public-skill"), filepath.Join(f.central, "public-skill")},
		{"creation path", filepath.Join(first, "new-skill", "SKILL.md"), filepath.Join(f.central, "new-skill", "SKILL.md")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var classified []string
			v, handled := ownerSharedSkillsLinkWith(tc.path, f.layout, ownerSharedSkillsEnvFor(f, &classified), ownerSharedSkillsReparse)
			if !handled || !v.AccessAllowed || v.Status != "clear" || v.Basis != "shared_skills_link_identity" || len(v.Categories) != 0 {
				t.Fatal("verified shared Skills link was not admitted", v.Basis)
			}
			if len(classified) != 1 || !same(classified[0], tc.target) {
				t.Fatal("central target was not classified exactly once", classified)
			}
		})
	}
}

func TestOwnerSharedSkillsLinkLeavesEveryDoubtfulPathToTheGenericRule(t *testing.T) {
	f := ownerSharedSkillsNewFixture(t, "good", "plain", "other", "missing")
	good, plain, other, missing := f.layout.links[0], f.layout.links[1], f.layout.links[2], f.layout.links[3]
	ownerSharedSkillsJunction(t, good, f.central)
	otherTarget := filepath.Join(f.base, "other-target")
	outside := filepath.Join(f.base, "outside")
	for _, dir := range []string{plain, otherTarget, outside} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{filepath.Join(plain, "file.txt"), filepath.Join(outside, "secret.txt")} {
		if err := os.WriteFile(file, []byte("synthetic\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ownerSharedSkillsJunction(t, other, otherTarget)
	ownerSharedSkillsJunction(t, filepath.Join(f.central, "escape"), outside)
	aliasCentral := filepath.Join(f.base, "central-alias")
	ownerSharedSkillsJunction(t, aliasCentral, f.central)

	var classified []string
	env := ownerSharedSkillsEnvFor(f, &classified)
	check := func(name, path string, layout ownerSharedSkillsLayout, env ownerSharedSkillsEnv, reparse func(string) (bool, error)) {
		t.Helper()
		t.Run(name, func(t *testing.T) {
			if v, handled := ownerSharedSkillsLinkWith(path, layout, env, reparse); handled || v.AccessAllowed {
				t.Fatal("doubtful shared Skills path was admitted", v.Basis)
			}
		})
	}
	definition := filepath.Join(good, "public-skill")
	check("real directory root", plain, f.layout, env, ownerSharedSkillsReparse)
	check("real directory child", filepath.Join(plain, "file.txt"), f.layout, env, ownerSharedSkillsReparse)
	check("retargeted link root", other, f.layout, env, ownerSharedSkillsReparse)
	check("retargeted link child", filepath.Join(other, "anything"), f.layout, env, ownerSharedSkillsReparse)
	check("missing link", filepath.Join(missing, "anything"), f.layout, env, ownerSharedSkillsReparse)
	check("nested link escaping the store", filepath.Join(good, "escape", "secret.txt"), f.layout, env, ownerSharedSkillsReparse)
	check("nested link directory", filepath.Join(good, "escape"), f.layout, env, ownerSharedSkillsReparse)
	check("central path itself", filepath.Join(f.central, "public-skill", "SKILL.md"), f.layout, env, ownerSharedSkillsReparse)
	check("name prefix of a link", good+"-other", f.layout, env, ownerSharedSkillsReparse)
	check("card name", filepath.Join(good, "credit-cards.json"), f.layout, env, ownerSharedSkillsReparse)
	check("trailing dot alias", definition+".", f.layout, env, ownerSharedSkillsReparse)
	check("alternate data stream", filepath.Join(definition, "SKILL.md")+":stream", f.layout, env, ownerSharedSkillsReparse)
	check("link metadata unavailable", definition, f.layout, env, func(string) (bool, error) { return false, os.ErrPermission })
	check("missing reparse reader", definition, f.layout, env, nil)
	check("missing providers", definition, f.layout, ownerSharedSkillsEnv{}, ownerSharedSkillsReparse)
	check("central store reached through an alias", definition, ownerSharedSkillsLayout{central: aliasCentral, links: []string{good}}, env, ownerSharedSkillsReparse)
	if len(classified) != 0 {
		t.Fatal("identity failures reached the central classification", classified)
	}

	exclusion := env
	exclusion.legacy = func(string) ownerLegacyState { return ownerLegacyClient }
	check("existing client exclusion", definition, f.layout, exclusion, ownerSharedSkillsReparse)
	exclusion.legacy = func(string) ownerLegacyState { return ownerLegacyUnavailable }
	check("existing exclusion unavailable", definition, f.layout, exclusion, ownerSharedSkillsReparse)
	unknown := env
	unknown.classify = func(string) ownerPathVerdict { return ownerRuleUnknown("fixture_not_clear") }
	check("central target unknown", definition, f.layout, unknown, ownerSharedSkillsReparse)
	protected := env
	protected.classify = func(string) ownerPathVerdict { return ownerRuleProtected("fixture_client") }
	check("central target protected", definition, f.layout, protected, ownerSharedSkillsReparse)
}

func TestOwnerSharedSkillsLinkOnlyUpgradesTheVerifiedLinkVerdict(t *testing.T) {
	f := ownerSharedSkillsNewFixture(t, "good", "other")
	good, other := f.layout.links[0], f.layout.links[1]
	ownerSharedSkillsJunction(t, good, f.central)
	otherTarget := filepath.Join(f.base, "other-target")
	if err := os.MkdirAll(otherTarget, 0o700); err != nil {
		t.Fatal(err)
	}
	ownerSharedSkillsJunction(t, other, otherTarget)

	generic := ownerPolicyAccess{
		legacy:       func(string) ownerLegacyState { return ownerLegacyAbsent },
		physical:     ownerRulePhysicalPath,
		organization: func(string) ownerPathVerdict { return ownerRuleClear("fixture_clear") },
	}
	shared := generic
	shared.sharedSkills = func(path string, env ownerSharedSkillsEnv) (ownerPathVerdict, bool) {
		return ownerSharedSkillsLinkWith(path, f.layout, env, ownerSharedSkillsReparse)
	}
	definition := filepath.Join(good, "public-skill", "SKILL.md")
	if v := ownerPolicyClassify(definition, generic); v.AccessAllowed || v.Basis != "physical_identity_mismatch" {
		t.Fatal("generic rule changed for a link path", v.Basis)
	}
	if v := ownerPolicyClassify(definition, shared); !v.AccessAllowed || v.Status != "clear" || v.Basis != "shared_skills_link_identity" {
		t.Fatal("verified link path was not admitted", v.Basis)
	}
	if v := ownerPolicyClassify(filepath.Join(other, "anything"), shared); v.AccessAllowed || v.Basis != "physical_identity_mismatch" {
		t.Fatal("retargeted link path changed", v.Basis)
	}
	if v := ownerPolicyClassify(filepath.Join(f.central, "public-skill", "SKILL.md"), shared); !v.AccessAllowed || v.Basis != "fixture_clear" {
		t.Fatal("ordinary central path changed", v.Basis)
	}
	exclusion := shared
	exclusion.legacy = func(string) ownerLegacyState { return ownerLegacyClient }
	if v := ownerPolicyClassify(definition, exclusion); v.AccessAllowed || v.Status != "protected" || v.Basis != "legacy_exclusion" {
		t.Fatal("existing client exclusion lost priority", v.Basis)
	}
}

func TestOwnerSharedSkillsReparseReadsTheEntryNotItsTarget(t *testing.T) {
	f := ownerSharedSkillsNewFixture(t, "linked")
	link := f.layout.links[0]
	ownerSharedSkillsJunction(t, link, f.central)
	for _, tc := range []struct {
		name, path string
		want       bool
	}{
		{"junction", link, true},
		{"real directory", f.central, false},
		{"regular file", filepath.Join(f.central, "public-skill", "SKILL.md"), false},
	} {
		if linked, err := ownerSharedSkillsReparse(tc.path); err != nil || linked != tc.want {
			t.Fatal("reparse attribute differs", tc.name, linked, err)
		}
	}
	if _, err := ownerSharedSkillsReparse(filepath.Join(f.base, "missing")); err == nil {
		t.Fatal("missing entry reported attributes")
	}
}

func TestOwnerSharedSkillsDefaultLayoutIsTheFixedFiveAgentLinks(t *testing.T) {
	layout := ownerSharedSkillsDefaultLayout()
	want := []string{
		`C:\Users\makhmeto\.claude\skills`,
		`C:\Users\makhmeto\.codex\skills`,
		`C:\Users\makhmeto\.agents\skills`,
		`C:\Users\makhmeto\.cursor\skills`,
		`C:\Users\makhmeto\.config\opencode\skills`,
	}
	if !same(layout.central, `D:\CC\Skills`) || len(layout.links) != len(want) {
		t.Fatal("shared Skills layout changed")
	}
	for i := range want {
		if !same(layout.links[i], want[i]) {
			t.Fatal("agent link path changed", i)
		}
	}
	access := ownerNormalPolicyAccess(nil)
	if access.sharedSkills == nil || access.installed == nil {
		t.Fatal("normal classification lacks the shared provider set")
	}
}
