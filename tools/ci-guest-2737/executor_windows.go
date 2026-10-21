package main

import (
	"errors"
	"os"
	"runtime/debug"
)

// Trace the already-running controller. This is not an authority supplied by a
// request or a claim of signed attestation. Admission code is linked, so there
// is no second host program to select, replace, or inherit an environment.
func linkedExecutorIdentity() (map[string]any, error) {
	b, ok := debug.ReadBuildInfo()
	if !ok || b.Main.Path != "cc/ci-guest-2737" {
		return nil, errors.New("fixed controller module rejected")
	}
	core := false
	for _, d := range b.Deps {
		if d.Path == "cc/product-ci" {
			core = true
		}
	}
	revision := ""
	clean := false
	for _, s := range b.Settings {
		if s.Key == "vcs.revision" {
			revision = s.Value
		}
		if s.Key == "vcs.modified" {
			clean = s.Value == "false"
		}
	}
	if !core || !clean || len(revision) != 40 {
		return nil, errors.New("controller needs a clean recorded source build")
	}
	path, e := os.Executable()
	if e != nil {
		return nil, e
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	sha, e := hashReader(f)
	if e != nil {
		return nil, e
	}
	return map[string]any{"module": b.Main.Path, "executor_code_sha": revision, "executable_sha256": sha, "process_id": os.Getpid(), "admission_linked": true, "source_clean": true}, nil
}
