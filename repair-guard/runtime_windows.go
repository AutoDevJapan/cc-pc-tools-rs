package main

import "os"

// This existing PC tool is named by tools/product-ci/main.go in the verified
// repair repository. Its executable needs ordinary bug fixes during unattended
// operation. This is not permission to inspect its directory, configuration,
// original private registry, or any other executable.
const publicationGateRuntime = "D:/CC/Tools/codex-jobs/gh_gate.exe"

func runtimeExecutable(absolute, target, known string) bool {
	if !same(absolute, known) || !same(absolute, target) {
		return false
	}
	info, err := os.Lstat(absolute)
	return err == nil && info.Mode().IsRegular()
}
