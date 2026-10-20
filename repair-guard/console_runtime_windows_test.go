package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

type consoleFixture struct {
	root, leaf, receipt string
	leaves              []consoleLeaf
}

func newConsoleFixture(t *testing.T) consoleFixture {
	t.Helper()
	root, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := consoleFixture{root: root, leaf: filepath.Join(root, "runtime", "console.exe"), receipt: filepath.Join(root, "receipt.json")}
	if err := os.Mkdir(filepath.Dir(f.leaf), 0700); err != nil {
		t.Fatal(err)
	}
	for path, body := range map[string]string{f.leaf: "known executable", f.receipt: "independent source receipt"} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	f.leaves = []consoleLeaf{{path: f.leaf, pins: []consolePin{{sha256: hashBytes([]byte("known executable")), evidence: []consoleEvidence{{f.root, f.receipt, hashBytes([]byte("independent source receipt"))}}}}}}
	return f
}

func (f consoleFixture) allowed(path string) bool {
	return consoleRuntimeWith(path, f.leaves, func(string) bool { return false }, func(root string) bool { return same(root, f.root) })
}

func TestConsoleRuntimeExactBytesAndIndependentOrigin(t *testing.T) {
	f := newConsoleFixture(t)
	if !f.allowed(f.leaf) {
		t.Fatal("known leaf/bytes/receipt/source rejected")
	}
	if err := os.WriteFile(f.leaf, []byte("tampered executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if f.allowed(f.leaf) {
		t.Fatal("changed executable accepted")
	}
	// A second independently pinned version is accepted, but adding a pin
	// without evidence must never turn an arbitrary version into a grant.
	f.leaves[0].pins = append(f.leaves[0].pins, consolePin{sha256: hashBytes([]byte("tampered executable"))})
	if f.allowed(f.leaf) {
		t.Fatal("pin without independent evidence accepted")
	}
	f.leaves[0].pins[1].evidence = f.leaves[0].pins[0].evidence
	if !f.allowed(f.leaf) {
		t.Fatal("second independently pinned version rejected")
	}
	if err := os.WriteFile(f.leaf, []byte("known executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.receipt, []byte("changed independent receipt"), 0600); err != nil {
		t.Fatal(err)
	}
	if f.allowed(f.leaf) {
		t.Fatal("changed receipt accepted")
	}
}

func TestConsoleRuntimeKeepsLeafAndSourceProtection(t *testing.T) {
	f := newConsoleFixture(t)
	if !f.allowed(f.leaf) {
		t.Fatal("positive control rejected")
	}
	if consoleRuntimeWith(f.leaf, f.leaves, func(string) bool { return false }, func(string) bool { return false }) {
		t.Fatal("source origin mismatch accepted")
	}
	for _, protectedPath := range []string{f.leaf, filepath.Dir(f.leaf), f.root, f.receipt} {
		if consoleRuntimeWith(f.leaf, f.leaves, func(p string) bool { return same(p, protectedPath) }, func(string) bool { return true }) {
			t.Fatal("protected leaf/parent/source/receipt accepted")
		}
	}
	for _, path := range []string{filepath.Dir(f.leaf), f.leaf + ".bak", filepath.Join(f.root, "console.exe"), filepath.Join(filepath.Dir(f.leaf), "other.exe"), f.leaf + ":stream"} {
		if f.allowed(path) {
			t.Fatal("unrelated leaf/directory/stream accepted")
		}
	}
	if err := os.Remove(f.leaf); err != nil {
		t.Fatal(err)
	}
	if f.allowed(f.leaf) {
		t.Fatal("missing executable accepted")
	}
	if err := os.Mkdir(f.leaf, 0700); err != nil {
		t.Fatal(err)
	}
	if f.allowed(f.leaf) {
		t.Fatal("directory executable accepted")
	}
}

func TestConsoleRuntimeRefusesHardlinksAndReparsePoints(t *testing.T) {
	f := newConsoleFixture(t)
	if !f.allowed(f.leaf) {
		t.Fatal("positive control rejected")
	}
	alias := filepath.Join(f.root, "alias.exe")
	if err := os.Link(f.leaf, alias); err != nil {
		t.Fatal(err)
	}
	if f.allowed(f.leaf) {
		t.Fatal("hardlinked leaf accepted")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if !f.allowed(f.leaf) {
		t.Fatal("single-link control rejected")
	}
	if err := os.Remove(f.leaf); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alias, []byte("known executable"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(alias, f.leaf); err != nil {
		t.Fatal(err)
	}
	if f.allowed(f.leaf) {
		t.Fatal("symlink leaf accepted")
	}
	if err := os.Remove(f.leaf); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Dir(f.leaf)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "console.exe"), []byte("known executable"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", filepath.Dir(f.leaf), f.root)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("junction fixture: %v %s", err, out)
	}
	defer os.Remove(filepath.Dir(f.leaf))
	if f.allowed(f.leaf) {
		t.Fatal("junction parent accepted")
	}
}

func TestConsoleRuntimeRefusesMissingAndLinkedEvidence(t *testing.T) {
	f := newConsoleFixture(t)
	if !f.allowed(f.leaf) {
		t.Fatal("positive control rejected")
	}
	if err := os.Remove(f.receipt); err != nil {
		t.Fatal(err)
	}
	if f.allowed(f.leaf) {
		t.Fatal("missing evidence accepted")
	}
	other := filepath.Join(f.root, "other-receipt.json")
	if err := os.WriteFile(other, []byte("independent source receipt"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, f.receipt); err != nil {
		t.Fatal(err)
	}
	if f.allowed(f.leaf) {
		t.Fatal("linked evidence accepted")
	}
}

func TestConsoleSourceRejectsInheritedAndRedirectedRepository(t *testing.T) {
	root, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
		cmd.Env = civ6GitEnvironment(os.Environ())
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("fixture git: %v %s", err, out)
		}
	}
	git("init", "-q")
	git("remote", "add", "origin", "https://github.com/AutoDevJapan/cc-pc-tools-rs.git")
	if !consoleSourceVerified(root) {
		t.Fatal("exact source repository rejected")
	}
	child := filepath.Join(root, "not-a-worktree")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	if consoleSourceVerified(child) {
		t.Fatal("inherited parent origin accepted")
	}
	t.Setenv("GIT_DIR", filepath.Join(root, "absent-git-dir"))
	if !consoleSourceVerified(root) {
		t.Fatal("environment override changed source identity")
	}
	if consoleSourceVerified(child) {
		t.Fatal("environment override admitted inherited source")
	}
	git("remote", "set-url", "origin", "https://github.com/AutoDevJapan/unrelated.git")
	if consoleSourceVerified(root) {
		t.Fatal("different repository accepted")
	}
}
