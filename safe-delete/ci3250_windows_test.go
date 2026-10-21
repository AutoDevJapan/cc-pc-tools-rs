//go:build windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCI3250RequiresItsOwnQualificationResult(t *testing.T) {
	for _, input := range []string{`{"client_resource":false,"classification_unverified":false}`, `{"client_resource":false,"classification_unverified":false,"ci_retirement_qualified":false}`, `{"client_resource":true,"classification_unverified":false,"ci_retirement_qualified":true}`} {
		if ci3250QualifiedResponse([]byte(input)) {
			t.Fatal("generic/denied qualification borrowed")
		}
	}
	if !ci3250QualifiedResponse([]byte(`{"client_resource":false,"classification_unverified":false,"ci_retirement_qualified":true}`)) {
		t.Fatal("dedicated result refused")
	}
}

func TestCI3250QuarantineKeepsLongPathsGitAndInternalHardlinks(t *testing.T) {
	c, base := fixture(t)
	r := owned(t, c, base)
	s, _ := ci3174FixtureAt(t, r.Path)
	s.captureCollected = true
	s.internalLinks = true
	git := filepath.Join(r.Path, "source", ".git")
	if e := os.MkdirAll(git, 0700); e != nil {
		t.Fatal(e)
	}
	write(t, filepath.Join(git, "config"), "synthetic opaque config")
	deep := filepath.Join(r.Path, strings.Repeat("a", 100), strings.Repeat("b", 100), "leaf")
	if e := os.MkdirAll(filepath.Dir(deep), 0700); e != nil {
		t.Fatal(e)
	}
	write(t, deep, "synthetic long path")
	if e := os.Link(deep, filepath.Join(r.Path, "alias")); e != nil {
		t.Fatal(e)
	}
	before, _, e := s.snapshot(r.Path)
	if e != nil {
		t.Fatal(e)
	}
	s.captureCollected = false
	s.baseline = before
	raw, _ := json.Marshal(r)
	s.originalReceipt = func() ([]byte, error) { return raw, nil }
	out, e := c.quarantinePreserved(r.ID, "windows", nil, s)
	if e != nil {
		t.Fatal(e)
	}
	if exists(r.Path) {
		t.Fatal("source remains")
	}
	if e = s.verify(filepath.Join(out["destination"].(string), "files"), before, r); e != nil {
		t.Fatal(e)
	}
}

func TestCI3250CapturePreservesSourceGitAndOpaqueMetadata(t *testing.T) {
	s, root := ci3174Fixture(t)
	git := filepath.Join(root, "source", ".git")
	if e := os.MkdirAll(git, 0700); e != nil {
		t.Fatal(e)
	}
	write(t, filepath.Join(git, "config"), "synthetic opaque credential marker")
	s.captureCollected = true
	s.maxBytes = 16 << 30
	before, _, e := s.snapshot(root)
	if e != nil {
		t.Fatal(e)
	}
	s.captureCollected = false
	s.baseline = before
	if e = s.verify(root, before, receipt{identityRecord: identityRecord{Volume: s.volume, FileID: s.rootID}}); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(filepath.Join(root, "unexpected.git"), 0700); e != nil {
		t.Fatal(e)
	}
	_, _, e = s.snapshot(root)
	code(t, e, "CI_INVENTORY_CHANGED")
}

func TestCI3250CaptureKeepsOtherProtectedNamesAndLimits(t *testing.T) {
	for _, name := range []string{".git", ".ssh", ".codex", ".agents"} {
		t.Run(name, func(t *testing.T) {
			s, root := ci3174Fixture(t)
			s.captureCollected = true
			if e := os.Mkdir(filepath.Join(root, name), 0700); e != nil {
				t.Fatal(e)
			}
			_, _, e := s.snapshot(root)
			code(t, e, "PROTECTED_PATH")
		})
	}
	s, root := ci3174Fixture(t)
	s.captureCollected = true
	s.maxBytes = 1
	write(t, filepath.Join(root, "large"), "xx")
	_, _, e := s.snapshot(root)
	code(t, e, "CI_SIZE_LIMIT_EXCEEDED")
}

func TestCI3250NeverAdoptsAnOrdinaryReceipt(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	if c.ci3250Candidate(r.ID) {
		t.Fatal("ordinary target promoted")
	}
	_, _, e := c.ci3250Recovery(r.ID)
	code(t, e, "CI_SCOPE_MISMATCH")
}

func TestCI3250InternalHardlinksArePreservedAndExternalLinksRefused(t *testing.T) {
	s, root := ci3174Fixture(t)
	s.captureCollected = true
	s.internalLinks = true
	first := filepath.Join(root, "proxy.exe")
	write(t, first, "synthetic proxy")
	if e := os.Link(first, filepath.Join(root, "proxy-alias.exe")); e != nil {
		t.Fatal(e)
	}
	before, _, e := s.snapshot(root)
	if e != nil {
		t.Fatal(e)
	}
	s.captureCollected = false
	s.baseline = before
	if e = s.verify(root, before, receipt{identityRecord: identityRecord{Volume: s.volume, FileID: s.rootID}}); e != nil {
		t.Fatal(e)
	}
	if e := os.Link(first, filepath.Join(t.TempDir(), "external")); e != nil {
		t.Fatal(e)
	}
	s.captureCollected = true
	s.baseline = nil
	_, _, e = s.snapshot(root)
	code(t, e, "CI_EXTERNAL_HARDLINK")
}
