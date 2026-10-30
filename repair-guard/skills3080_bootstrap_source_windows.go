package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Only a successfully classified PUBLIC review can supply the independently
// published project document. The global document has one runtime-defined
// location, not a path or digest chosen by the review or its consumer. Neither
// document is returned by the result CLI or added to general path allowances.
func skillsPublicBootstrapSource(s *skillsFS, commit string) ([]byte, bool) {
	project := s.bootstrapProjects[commit]
	if len(project) == 0 {
		return nil, false
	}
	home, err := os.UserHomeDir()
	if err != nil || !same(home, "C:/Users/makhmeto") {
		return nil, false
	}
	path := filepath.Join(home, ".codex", "AGENTS.md")
	// This single operating-instruction source is compared internally. An
	// actual original client exclusion still denies it. Unavailable ordinary
	// source classification does not turn this into a general source grant.
	policy := skillsFS{blocked: func(p string) bool {
		return !same(p, path) || ownerLegacyProtection(p) == ownerLegacyClient || ownerCardPath(p) || ownerNonSourcePath(p)
	}}
	defer policy.close()
	global, ok := policy.read(path, 256<<10)
	if !ok {
		return nil, false
	}
	// Keep the same deny-write handles until native/result consumption ends.
	s.files = append(s.files, policy.files...)
	policy.files = nil
	return skillsAssembleBootstrapSource(global, project)
}

func skillsBootstrapDocument(b []byte) (string, bool) {
	b = bytes.TrimPrefix(b, []byte{0xef, 0xbb, 0xbf})
	if len(b) == 0 || len(b) > 256<<10 || !utf8.Valid(b) || bytes.ContainsRune(b, 0) {
		return "", false
	}
	// Codex assembles trimmed documents. Windows checkout line endings and
	// the UTF-8 file marker are representation differences, not instructions.
	return strings.TrimSpace(strings.ReplaceAll(string(b), "\r\n", "\n")), true
}

func skillsAssembleBootstrapSource(global, project []byte) ([]byte, bool) {
	g, ok := skillsBootstrapDocument(global)
	p, valid := skillsBootstrapDocument(project)
	if !ok || !valid || g == "" || p == "" {
		return nil, false
	}
	return []byte(g + "\n\n--- project-doc ---\n\n" + p), true
}

func skillsBootstrapSourceMatches(body string, source []byte) bool {
	return len(source) > 0 && strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n")) == string(source)
}
