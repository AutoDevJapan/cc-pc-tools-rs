package main

import (
	"path/filepath"
	"syscall"
)

// The owner's standing Skill layout keeps every definition in one central
// directory and exposes it to each agent through a directory junction. This is
// a purpose-specific identity contract for exactly those links. It names no
// individual Skill, grants no write scope and never classifies the agents' own
// configuration, sessions or credentials that sit beside the links.
type ownerSharedSkillsLayout struct {
	central string
	links   []string
}

// Same fixed owner as ownerDefaultRuleConfig: process environment such as
// USERPROFILE must not change this identity.
func ownerSharedSkillsDefaultLayout() ownerSharedSkillsLayout {
	home := "C:/Users/makhmeto"
	links := []string{
		home + "/.claude/skills",
		home + "/.codex/skills",
		home + "/.agents/skills",
		home + "/.cursor/skills",
		home + "/.config/opencode/skills",
	}
	return ownerSharedSkillsLayout{central: "D:/CC/Skills", links: links}
}

// The caller's already selected providers. The diagnostic and normal
// classifications therefore share one physical-metadata reader, one existing
// exclusion reader and one ordinary policy for the central target.
type ownerSharedSkillsEnv struct {
	legacy   func(string) ownerLegacyState
	physical func(string) (string, error)
	classify func(string) ownerPathVerdict
}

// Reports whether the entry itself, not its target, is a directory reparse
// point. Only attributes are read.
func ownerSharedSkillsReparse(path string) (bool, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attributes, err := syscall.GetFileAttributes(name)
	if err != nil {
		return false, err
	}
	const want = syscall.FILE_ATTRIBUTE_REPARSE_POINT | syscall.FILE_ATTRIBUTE_DIRECTORY
	return attributes&want == want, nil
}

// Verifies one path at or below a fixed agent Skills link. handled is true only
// for a positive identity proof whose central target is itself clear: the link
// is a reparse point, it resolves to the central store, the central store is
// not an alias, and the path resolves to exactly the same relative location
// below the store. Every other case is left to the generic physical-identity
// rule unchanged: card or Windows-alias names, existing exclusions, real
// directories, retargeted or nested links, missing or unreadable metadata and
// a central target that is not clear.
func ownerSharedSkillsLinkWith(path string, layout ownerSharedSkillsLayout, env ownerSharedSkillsEnv, reparse func(string) (bool, error)) (ownerPathVerdict, bool) {
	unhandled := ownerPathVerdict{}
	if layout.central == "" || env.legacy == nil || env.physical == nil || env.classify == nil || reparse == nil {
		return unhandled, false
	}
	root := ""
	for _, link := range layout.links {
		if within(link, path) {
			root = link
			break
		}
	}
	if root == "" {
		return unhandled, false
	}
	if state := env.legacy(path); state != ownerLegacyAbsent && state != ownerLegacyClear {
		return unhandled, false
	}
	if ownerCardPath(path) || ownerWindowsAlias(path) {
		return unhandled, false
	}
	central, err := env.physical(layout.central)
	if err != nil || !same(central, layout.central) {
		return unhandled, false
	}
	linked, err := reparse(root)
	if err != nil || !linked {
		return unhandled, false
	}
	resolved, err := env.physical(root)
	if err != nil || !same(resolved, central) {
		return unhandled, false
	}
	target := filepath.Clean(layout.central)
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return unhandled, false
	}
	if rel != "." {
		target = filepath.Join(target, rel)
		actual, err := env.physical(path)
		if err != nil || !same(actual, target) {
			return unhandled, false
		}
	}
	if verdict := env.classify(target); !verdict.AccessAllowed || verdict.Status != "clear" {
		return unhandled, false
	}
	return ownerRuleClear("shared_skills_link_identity"), true
}

// The normal provider for the owner's fixed layout.
func ownerSharedSkillsLinkClassification(path string, env ownerSharedSkillsEnv) (ownerPathVerdict, bool) {
	return ownerSharedSkillsLinkWith(path, ownerSharedSkillsDefaultLayout(), env, ownerSharedSkillsReparse)
}
