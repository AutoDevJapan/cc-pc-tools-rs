package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
)

func TestProductCIRuntimeQualifiesOnlyExactDistributionLeaves(t *testing.T) {
	base, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "product-ci")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	none := func(string) bool { return false }
	for _, name := range []string{"product-ci.exe", "product-ci-linux-3568568", "product-ci-pr205.atomic-backup.exe", "install-pr205.private.json", "install-receipt.private.json"} {
		leaf := filepath.Join(dir, name)
		if !productCIRuntime(leaf, dir, none) {
			t.Fatalf("uncreated %s rejected", name)
		}
		if err := os.WriteFile(leaf, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		if !productCIRuntime(leaf, dir, none) {
			t.Fatalf("existing %s rejected", name)
		}
	}
	for _, name := range []string{
		"product-ci.exe.bak", "product-ci.exe:stream", "Product-CI.exe", "product-ci-linux-356856", "product-ci-linux-35685688",
		"product-ci-linux-356856G", "product-ci-linux-3568568.bak", "product-ci-prX.atomic-backup.exe", "product-ci-pr.atomic-backup.exe",
		"product-ci-pr205.before.exe", "install-pr205.private.log", "install-prX.private.json", "install-receipt.json",
		"inspect-pr185.private.log", "config.json", "other.exe", "evidence", ".ci-work", "gitleaks-linux-b109bc5f",
	} {
		if productCIRuntime(filepath.Join(dir, name), dir, none) {
			t.Fatalf("unrelated name %q accepted", name)
		}
	}
	if productCIRuntime(dir, dir, none) || productCIRuntime(dir, base, none) {
		t.Fatal("parent directory accepted")
	}
	nested := filepath.Join(dir, "sub")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	if productCIRuntime(filepath.Join(nested, "product-ci.exe"), dir, none) || productCIRuntime(filepath.Join(base, "product-ci.exe"), dir, none) {
		t.Fatal("leaf outside the exact directory accepted")
	}
	asDirectory := filepath.Join(dir, "install-pr9.private.json")
	if err := os.Mkdir(asDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if productCIRuntime(asDirectory, dir, none) {
		t.Fatal("directory accepted as a leaf")
	}
}

func TestProductCIRuntimeKeepsProtectionAndRefusesRedirection(t *testing.T) {
	base, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(base, "product-ci")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	leaf := filepath.Join(dir, "product-ci.exe")
	if err := os.WriteFile(leaf, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name      string
		protected func(string) bool
	}{
		{"leaf", func(p string) bool { return same(p, leaf) }},
		{"parent", func(p string) bool { return same(p, dir) }},
		{"anything", func(string) bool { return true }},
	} {
		if productCIRuntime(leaf, dir, c.protected) {
			t.Fatalf("protected %s accepted", c.name)
		}
		if c.name != "leaf" && productCIRuntime(filepath.Join(dir, "install-pr1.private.json"), dir, c.protected) {
			t.Fatalf("protected uncreated leaf accepted (%s)", c.name)
		}
	}
	if productCIRuntime(filepath.Join(base, "missing", "product-ci.exe"), filepath.Join(base, "missing"), func(string) bool { return false }) {
		t.Fatal("absent parent directory accepted")
	}

	// A junction cannot stand in for the real directory.
	real := filepath.Join(base, "real")
	link := filepath.Join(base, "junction")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "product-ci.exe"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	mklink := exec.Command("cmd", "/c", "mklink", "/J", link, real)
	mklink.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, err := mklink.CombinedOutput(); err != nil {
		t.Skipf("junction unavailable: %v %s", err, out)
	}
	defer os.Remove(link)
	none := func(string) bool { return false }
	if productCIRuntime(filepath.Join(link, "product-ci.exe"), link, none) || productCIRuntime(filepath.Join(link, "install-pr1.private.json"), link, none) {
		t.Fatal("junctioned directory accepted")
	}

	// A link standing in for the leaf is a reparse point, not a regular file.
	target := filepath.Join(base, "elsewhere.exe")
	if err := os.WriteFile(target, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "product-ci-linux-abcdef0")
	if err := os.Symlink(target, alias); err != nil {
		t.Skipf("file symlink unavailable: %v", err)
	}
	if productCIRuntime(alias, dir, none) {
		t.Fatal("linked leaf accepted")
	}
}

func TestRuntimeExecutableOnlyQualifiesTheExistingExactFile(t *testing.T) {
	for _, name := range []string{"gh_gate.exe", filepath.Base(schedulerRuntime), filepath.Base(jobsRuntime)} {
		t.Run(name, func(t *testing.T) {
			root, err := physical(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			known := filepath.Join(root, name)
			if err := os.WriteFile(known, []byte("fixture"), 0600); err != nil {
				t.Fatal(err)
			}
			if !runtimeExecutable(known, known, known) {
				t.Fatal("existing exact executable rejected")
			}
			for _, candidate := range []string{root, known + ".bak", filepath.Join(root, "config.json"), filepath.Join(root, "other.exe")} {
				if runtimeExecutable(candidate, candidate, known) {
					t.Fatal("unrelated resource accepted")
				}
			}
			if runtimeExecutable(known, filepath.Join(root, "redirected.exe"), known) {
				t.Fatal("resolved target mismatch accepted")
			}
			missing := filepath.Join(root, "missing.exe")
			if runtimeExecutable(missing, missing, missing) {
				t.Fatal("uncreated executable accepted")
			}
			dir := filepath.Join(root, "directory.exe")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if runtimeExecutable(dir, dir, dir) {
				t.Fatal("directory accepted as executable")
			}
		})
	}
}

func TestSchedulerRuntimeRefusesJunctionRedirection(t *testing.T) {
	root, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(root, "real")
	link := filepath.Join(root, "junction")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, filepath.Base(schedulerRuntime)), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("cmd", "/c", "mklink", "/J", link, real)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("junction fixture: %v %s", err, output)
	}
	defer os.Remove(link)
	info, err := os.Lstat(link)
	if err != nil || plainEntry(info) {
		t.Fatalf("junction fixture is not a reparse point: %v", err)
	}
	realLeaf := filepath.Join(real, filepath.Base(schedulerRuntime))
	if !runtimeExecutable(realLeaf, realLeaf, realLeaf) {
		t.Fatal("ordinary scheduler fixture rejected")
	}
	leaf := filepath.Join(link, filepath.Base(schedulerRuntime))
	target, err := physical(leaf)
	if err != nil {
		// allowed() rejects a failed physical resolution before runtimeExecutable.
		return
	}
	if runtimeExecutable(leaf, target, leaf) {
		t.Fatal("junction redirected scheduler accepted")
	}
}

func TestProgressTUIRuntimeOnlyQualifiesInstalledDistribution(t *testing.T) {
	root, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "progress")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"codex.exe", "codex-code-mode-host.exe", "manifest.json"} {
		leaf := filepath.Join(dir, name)
		if progressTUIRuntime(leaf, leaf, dir) {
			t.Fatal("missing distribution leaf qualified")
		}
		if err := os.WriteFile(leaf, []byte("owned fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		if !progressTUIRuntime(leaf, leaf, dir) {
			t.Fatalf("installed %s rejected", name)
		}
		if progressTUIRuntime(leaf, filepath.Join(root, name), dir) {
			t.Fatal("redirected leaf qualified")
		}
	}
	for _, name := range []string{"codex-launch.exe", "agent-progress.exe", "auth.json", "config.toml", "manifest.json:stream", "codex.exe.bak", "other.exe"} {
		leaf := filepath.Join(dir, name)
		if name != "manifest.json:stream" {
			if err := os.WriteFile(leaf, []byte("unrelated"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if progressTUIRuntime(leaf, leaf, dir) {
			t.Fatalf("unrelated leaf %s qualified", name)
		}
	}
	if progressTUIRuntime(dir, dir, dir) {
		t.Fatal("distribution directory qualified")
	}
	sibling := filepath.Join(root, "codex.exe")
	if err := os.WriteFile(sibling, []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	if progressTUIRuntime(sibling, sibling, dir) {
		t.Fatal("sibling executable qualified")
	}
	nested := filepath.Join(dir, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	nestedLeaf := filepath.Join(nested, "codex.exe")
	if err := os.WriteFile(nestedLeaf, []byte("unrelated"), 0600); err != nil {
		t.Fatal(err)
	}
	if progressTUIRuntime(nestedLeaf, nestedLeaf, dir) {
		t.Fatal("nested executable qualified")
	}
	directoryLeaf := filepath.Join(root, "directory-distribution")
	if err := os.MkdirAll(filepath.Join(directoryLeaf, "codex.exe"), 0700); err != nil {
		t.Fatal(err)
	}
	if progressTUIRuntime(filepath.Join(directoryLeaf, "codex.exe"), filepath.Join(directoryLeaf, "codex.exe"), directoryLeaf) {
		t.Fatal("directory in place of executable qualified")
	}
}
