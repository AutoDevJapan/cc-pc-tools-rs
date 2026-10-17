//go:build windows && amd64

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func toolEntry(name string, args []string) (int, error) {
	self, e := os.Executable()
	if e != nil {
		return 1, e
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(self)))
	p, e := readPlan(root)
	if e != nil {
		return 1, e
	}
	if !samePath(self, filepath.Join(p.runtime(), "cargo", "bin", name+".exe")) || (name != "cargo" && name != "rustup") {
		return 1, errors.New("fixed tool-entry location required")
	}
	if e = checkControl(root, false); e != nil {
		return 1, e
	}
	if e = checkToolSource(p); e != nil {
		return 1, e
	}
	// Refuse self-update, which could overwrite the guard in CARGO_HOME/bin.
	if name == "rustup" && len(args) > 0 && args[0] == "self" {
		return 1, errors.New("finite SDK excludes rustup self modification")
	}
	c := exec.Command(filepath.Join(p.runtime(), "sdk", "bootstrap", "bin", name+".exe"), args...)
	hidden(c)
	c.WaitDelay = 2 * time.Second
	c.Env = runtimeSDKEnv(p, os.Environ())
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	e = c.Run()
	if c.ProcessState == nil {
		return 1, errors.New("fixed SDK tool did not start")
	}
	if e != nil {
		return c.ProcessState.ExitCode(), nil
	}
	return 0, nil
}

func checkToolSource(p Plan) error {
	if p.RuntimeFixture {
		h, tree, _ := syntheticSource()
		q := p
		q.Head = h
		q.Tree = tree
		if e := verifySource(q, p.source(), false); e != nil {
			return e
		}
		path := filepath.Join(p.runtime(), "fixture-source-guard.json")
		if _, e := os.Lstat(path); os.IsNotExist(e) {
			if e = verifySource(q, p.source(), true); e != nil {
				return e
			}
			return saveNew(path, map[string]any{"head": h, "tree": tree, "source_verified": true, "synthetic_only": true, "official_ci_claimed": false})
		} else if e != nil {
			return e
		}
		return nil
	}
	if p.Fixture != "" {
		return errors.New("lifecycle fixture cannot invoke SDK")
	}
	if !samePath(os.Getenv("GITHUB_WORKSPACE"), p.source()) {
		return errors.New("tool entry requires fixed official workspace")
	}
	if _, e := os.Lstat(filepath.Join(p.runtime(), "verify-source.json")); os.IsNotExist(e) {
		return hook(p, "verify-source")
	} else if e != nil {
		return e
	}
	// Every subsequent invocation still checks context and source. The first
	// receipt alone cannot conceal a checkout that was changed later.
	b, e := os.ReadFile(filepath.Join(p.runtime(), "verify-source.json"))
	if e != nil {
		return e
	}
	if _, e = validateHookData(p, "verify-source", b); e != nil {
		return e
	}
	return verifySource(p, p.source(), false)
}

func toolAlias() string {
	name := strings.ToLower(filepath.Base(os.Args[0]))
	if name == "cargo.exe" {
		return "cargo"
	}
	if name == "rustup.exe" {
		return "rustup"
	}
	return ""
}
