package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type consoleImportFixture struct {
	profile   consoleImportProfile
	protected func(string) bool
}

func newConsoleImportFixture(t *testing.T) consoleImportFixture {
	t.Helper()
	root, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	source, candidate := filepath.Join(root, "permanent-source"), filepath.Join(root, "exact-import")
	for _, path := range []string{source, candidate} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		currentFixtureGit(t, path, "init", "-q")
		currentFixtureGit(t, path, "config", "core.autocrlf", "false")
	}
	currentFixtureGit(t, source, "remote", "add", "origin", "https://github.com/AutoDevJapan/cc-pc-tools-rs.git")
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(source, "source.txt"), "original console source\n")
	write(filepath.Join(source, "package.cfg"), "original dependencies\n")
	write(filepath.Join(source, "workspace.cfg"), "original workspace\n")
	write(filepath.Join(source, "independent-proof.json"), "original generation and source classification receipt\n")
	write(filepath.Join(candidate, "source.txt"), "original console source\n")
	write(filepath.Join(candidate, "package.cfg"), "five fixed public dependencies\n")
	write(filepath.Join(candidate, "generated.cfg"), "single-member workspace\n")
	for _, path := range []string{source, candidate} {
		currentFixtureGit(t, path, "add", ".")
		currentFixtureGit(t, path, "-c", "user.name=fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixed source")
	}
	gitValue := func(path, expression string) string {
		t.Helper()
		b, err := civ6GitAt(path, "rev-parse", expression)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(b))
	}
	f := consoleImportFixture{profile: consoleImportProfile{
		root: candidate, head: gitValue(candidate, "HEAD"), tree: gitValue(candidate, "HEAD^{tree}"),
		sourceRoot: source, sourceHead: gitValue(source, "HEAD"),
		evidence:     []consoleEvidence{{source, filepath.Join(source, "independent-proof.json"), hashBytes([]byte("original generation and source classification receipt\n"))}},
		sourceInputs: []consoleEvidence{{source, filepath.Join(source, "workspace.cfg"), hashBytes([]byte("original workspace\n"))}},
		files: []consoleImportFile{
			{"source.txt", hashBytes([]byte("original console source\n")), hashBytes([]byte("original console source\n")), gitValue(source, "HEAD:source.txt")},
			{"package.cfg", hashBytes([]byte("original dependencies\n")), hashBytes([]byte("five fixed public dependencies\n")), gitValue(source, "HEAD:package.cfg")},
			{"generated.cfg", "", hashBytes([]byte("single-member workspace\n")), ""},
		},
	}, protected: func(string) bool { return false }}
	return f
}

func (f consoleImportFixture) allowed(path string) bool {
	return consoleImportWith(path, f.profile, f.protected, func(root string) bool {
		return permanentSourceVerifiedWith(root, []permanentSource{{f.profile.sourceRoot, f.profile.sourceHead}})
	}, consoleImportGitVerified)
}

func TestConsoleImportQualifiesCopiesDerivedManifestAndGeneratedSettings(t *testing.T) {
	f := newConsoleImportFixture(t)
	for _, file := range f.profile.files {
		if !f.allowed(filepath.Join(f.profile.root, file.relative)) {
			t.Fatalf("proven file refused: %s", file.relative)
		}
	}
	currentFixtureGit(t, f.profile.root, "remote", "add", "origin", "https://github.com/AutoDevJapan/local-ai-console.git")
	if !f.allowed(filepath.Join(f.profile.root, "source.txt")) {
		t.Fatal("adding the exact private destination changed the existing source qualification")
	}
}

func TestConsoleImportRefusesParentsUnknownLeavesAndHeldFixtures(t *testing.T) {
	f := newConsoleImportFixture(t)
	if !f.allowed(filepath.Join(f.profile.root, "source.txt")) {
		t.Fatal("positive control")
	}
	for _, path := range []string{f.profile.root, filepath.Dir(f.profile.root), filepath.Join(f.profile.root, "unknown.txt"), filepath.Join(f.profile.root, "source.txt.bak"), filepath.Join(f.profile.root, "source.txt:stream"), filepath.Join(f.profile.root, ".git", "config"), filepath.Join(f.profile.root, "localai-web-rs/tests/fixtures/operations/client-projects.txt"), filepath.Join(f.profile.root, "localai-web-rs/tests/fixtures/operations/todo/todo.json")} {
		if f.allowed(path) {
			t.Fatalf("outside exact import accepted: %s", path)
		}
	}
}

func TestConsoleImportRefusesChangedBytesProvenanceSourceAndOwner(t *testing.T) {
	for _, name := range []string{"candidate", "source", "proof", "source input", "owner", "head", "tree", "source hash", "source blob", "candidate hash", "no evidence", "no source input", "client"} {
		t.Run(name, func(t *testing.T) {
			f := newConsoleImportFixture(t)
			path := filepath.Join(f.profile.root, "source.txt")
			if !f.allowed(path) {
				t.Fatal("positive control")
			}
			changed := ""
			switch name {
			case "candidate":
				changed = path
			case "source":
				changed = filepath.Join(f.profile.sourceRoot, "source.txt")
			case "proof":
				changed = f.profile.evidence[0].path
			case "source input":
				changed = f.profile.sourceInputs[0].path
			case "owner":
				currentFixtureGit(t, f.profile.root, "remote", "add", "origin", "https://github.com/other-owner/local-ai-console.git")
			case "head":
				f.profile.head = strings.Repeat("b", 40)
			case "tree":
				f.profile.tree = strings.Repeat("b", 40)
			case "source hash":
				f.profile.files[0].sourceSHA = strings.Repeat("b", 64)
			case "source blob":
				f.profile.files[0].sourceBlob = strings.Repeat("b", 40)
			case "candidate hash":
				f.profile.files[0].candidateSHA = strings.Repeat("b", 64)
			case "no evidence":
				f.profile.evidence = nil
			case "no source input":
				f.profile.sourceInputs = nil
			case "client":
				f.protected = func(string) bool { return true }
			}
			if changed != "" {
				if err := os.WriteFile(changed, []byte("altered bytes\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if f.allowed(path) {
				t.Fatal("unproven import accepted")
			}
		})
	}
}

func TestConsoleImportRejectsHardlinkedPayloadAndEvidence(t *testing.T) {
	for _, kind := range []string{"candidate", "proof"} {
		t.Run(kind, func(t *testing.T) {
			f := newConsoleImportFixture(t)
			path := filepath.Join(f.profile.root, "source.txt")
			if !f.allowed(path) {
				t.Fatal("positive control")
			}
			linked := path
			if kind == "proof" {
				linked = f.profile.evidence[0].path
			}
			if err := os.Link(linked, filepath.Join(filepath.Dir(linked), "hardlink")); err != nil {
				t.Fatal(err)
			}
			if f.allowed(path) {
				t.Fatal("hardlinked payload or receipt accepted")
			}
		})
	}
}

func TestConsoleImportProductionMappingRetainsExactly53To56(t *testing.T) {
	if len(console3264Import.files) != 56 || len(console3264Import.evidence) != 7 || len(console3264Import.sourceInputs) != 3 {
		t.Fatal("declared exact evidence or file count changed")
	}
	sources, unchanged, generated := 0, 0, 0
	seen := map[string]bool{}
	for _, file := range console3264Import.files {
		if seen[file.relative] || filepath.IsAbs(file.relative) || strings.Contains(file.relative, "..") || !modelDigestValid(file.candidateSHA) {
			t.Fatal("invalid leaf")
		}
		seen[file.relative] = true
		if file.sourceSHA == "" {
			generated++
		} else {
			sources++
			if file.sourceSHA == file.candidateSHA {
				unchanged++
			}
		}
	}
	if sources != 53 || unchanged != 52 || generated != 3 {
		t.Fatal("source derivation counts changed")
	}
	for _, relative := range []string{"localai-web-rs/tests/fixtures/operations/client-projects.txt", "localai-web-rs/tests/fixtures/operations/todo/todo.json"} {
		if seen[relative] {
			t.Fatal("held fixture included")
		}
	}
}
