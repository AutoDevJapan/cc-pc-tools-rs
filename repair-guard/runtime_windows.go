package main

import (
	"os"
	"path/filepath"
	"regexp"
	"syscall"
)

// This existing PC tool is named by tools/product-ci/main.go in the verified
// repair repository. Its executable needs ordinary bug fixes during unattended
// operation. This is not permission to inspect its directory, configuration,
// original private registry, or any other executable.
const publicationGateRuntime = "D:/CC/Tools/codex-jobs/gh_gate.exe"

// The existing dispatcher is the explicit target of the owner's 2026-10-20
// worker-resumption repair (ToDo3047). Configuration and history stay excluded.
const schedulerRuntime = "D:/CC/Tools/scheduler/scheduler.exe"

// The owner explicitly requested repair of this installed progress display
// (ToDo3020/3021, restated 2026-10-20). Only the existing distribution leaves
// qualify. Its directory, collector, credentials and unrelated tools do not.
const progressTUIDirectory = "D:/CC/Tools/codex-progress-tui"

func progressTUIRuntime(absolute, target, directory string) bool {
	if !same(filepath.Dir(absolute), directory) {
		return false
	}
	for _, name := range []string{"codex.exe", "codex-code-mode-host.exe", "manifest.json"} {
		if runtimeExecutable(absolute, target, filepath.Join(directory, name)) {
			return true
		}
	}
	return false
}

// The installed product-ci tool and the files its normal update writes. Only
// these exact leaf names directly under the physical directory qualify.
const productCIDirectory = "D:/CC/Tools/product-ci"

var productCILeaf = regexp.MustCompile(`^(?:product-ci\.exe|product-ci-linux-[0-9a-f]{7}|product-ci-pr[0-9]{1,6}\.atomic-backup\.exe|install-pr[0-9]{1,6}\.private\.json|install-receipt\.private\.json)$`)

func runtimeExecutable(absolute, target, known string) bool {
	if !same(absolute, known) || !same(absolute, target) {
		return false
	}
	info, err := os.Lstat(absolute)
	return err == nil && info.Mode().IsRegular()
}

// plainEntry is true for an existing entry that is not a link, junction or
// other reparse point. Recent Go releases no longer report junctions as
// symlinks, so the Windows attribute is read directly.
func plainEntry(info os.FileInfo) bool {
	data, ok := info.Sys().(*syscall.Win32FileAttributeData)
	return ok && data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0
}

// productCIRuntime qualifies one exact leaf of the installed product-ci
// distribution: an existing regular file, or a not-yet-created name whose
// parent is the real directory itself. Links, junctions, reparse points,
// directories, other names and anything the protection check flags are
// refused. It reads no file contents and creates no record.
func productCIRuntime(absolute, directory string, protected func(string) bool) bool {
	name := filepath.Base(absolute)
	if !productCILeaf.MatchString(name) || !same(filepath.Dir(absolute), directory) || protected(absolute) {
		return false
	}
	parent, err := physical(directory)
	if err != nil || !same(parent, directory) || protected(parent) {
		return false
	}
	if info, err := os.Lstat(parent); err != nil || !info.IsDir() || !plainEntry(info) {
		return false
	}
	info, err := os.Lstat(absolute)
	if os.IsNotExist(err) {
		return true
	}
	if err != nil || !info.Mode().IsRegular() || !plainEntry(info) {
		return false
	}
	resolved, err := physical(absolute)
	return err == nil && filepath.Base(resolved) == name && same(filepath.Dir(resolved), parent) && !protected(resolved)
}
