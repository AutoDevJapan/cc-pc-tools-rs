package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func twoTargets() Bundle {
	b := fixtureBundle()
	c := b.Targets[0]
	c.Name = "second"
	c.Files = append([]BundleFile(nil), c.Files...)
	last := len(c.Files) - 1
	c.Files[last].Content = strings.Replace(c.Files[last].Content, "name: sample", "name: second", 1)
	c.Files[last].SHA = digest([]byte(c.Files[last].Content))
	b.Targets = append(b.Targets, c)
	return b
}

func TestExistingSecondTargetPreventsEveryWrite(t *testing.T) {
	d := t.TempDir()
	root := filepath.Join(d, "skills")
	if err := os.MkdirAll(filepath.Join(root, "second"), 0700); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(d, "receipt")
	if err := apply(twoTargets(), root, receipt); err == nil {
		t.Fatal("accepted second existing target")
	}
	for _, p := range []string{filepath.Join(root, "sample"), receipt} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Fatalf("preflight created %s", p)
		}
	}
}

func TestPartialFailureKeepsEvidenceWithoutCompleteOrRetryOverwrite(t *testing.T) {
	d := t.TempDir()
	root := filepath.Join(d, "skills")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(d, "receipt")
	calls := 0
	create := func(p string) (*os.File, error) {
		calls++
		if calls == 7 {
			return nil, errors.New("injected creation failure")
		}
		return os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	}
	err := applyBound(twoTargets(), root, receipt, strings.Repeat("d", 64), create)
	if err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("failure missing: %v", err)
	}
	raw, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`"phase":"started"`, `"bundle_sha256":"` + strings.Repeat("d", 64) + `"`, `"phase":"failed"`, `injected creation failure`} {
		if !bytes.Contains(raw, []byte(fragment)) {
			t.Fatalf("missing receipt evidence %s", fragment)
		}
	}
	if bytes.Contains(raw, []byte(`"phase":"complete"`)) {
		t.Fatal("failed apply marked complete")
	}
	if _, err = os.Stat(filepath.Join(root, "sample", "SKILL.md")); err != nil {
		t.Fatal("earlier completed target lost")
	}
	if _, err = os.Lstat(filepath.Join(root, "sample", "SKILL.md.partial")); !os.IsNotExist(err) {
		t.Fatal("completed entrypoint left partial")
	}
	if _, err = os.Lstat(filepath.Join(root, "second", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatal("failed target was published")
	}
	if err = apply(twoTargets(), root, filepath.Join(d, "retry")); err == nil || !strings.Contains(err.Error(), "target already exists") {
		t.Fatalf("retry did not preserve partial output: %v", err)
	}
}

func TestReceiptInsideSkillsAndReservedNamesRejectedBeforeWrite(t *testing.T) {
	d := t.TempDir()
	if err := apply(fixtureBundle(), d, filepath.Join(d, "receipt")); err == nil {
		t.Fatal("receipt inside root accepted")
	}
	for _, name := range []string{"con", "nul", "com1", "lpt9"} {
		b := fixtureBundle()
		b.Targets[0].Name = name
		if err := validate(b); err == nil {
			t.Fatal("reserved Windows name accepted")
		}
	}
	entries, err := os.ReadDir(d)
	if err != nil || len(entries) != 0 {
		t.Fatal("rejection mutated root")
	}
}

func TestReadOnlyPreflightDoesNotCreateReceiptOrTargets(t *testing.T) {
	d := t.TempDir()
	root := filepath.Join(d, "skills")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := preflight(twoTargets(), root, filepath.Join(d, "receipt")); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatal("preflight wrote targets")
	}
	if _, err = os.Lstat(filepath.Join(d, "receipt")); !os.IsNotExist(err) {
		t.Fatal("preflight wrote receipt")
	}
}
