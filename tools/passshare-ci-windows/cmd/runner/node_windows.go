//go:build windows && amd64

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The Node pin matches the reviewed Windows workflow. Rust uses only stable,
// per the parent's runtime specification; no second toolchain is installed.
const nodeRelease = "v22.16.0"
const pnpmRelease = "10.11.0"

func nodeHome(p Plan) string {
	return filepath.Join(p.runtime(), "sdk", "node22", "node-"+nodeRelease+"-win-x64")
}

func nodeArchiveDigest(raw []byte, name string) (string, error) {
	want := ""
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && f[1] == name {
			if want != "" || !hex64.MatchString(f[0]) {
				return "", errors.New("ambiguous official Node checksum")
			}
			want = f[0]
		}
	}
	if want == "" {
		return "", errors.New("official Node22 checksum absent")
	}
	return want, nil
}

func prepareNode(p Plan) error {
	name := "node-" + nodeRelease + "-win-x64.zip"
	base := "https://nodejs.org/dist/" + nodeRelease + "/"
	sums, e := publicDistribution(base+"SHASUMS256.txt", 64<<10)
	if e != nil {
		return e
	}
	want, e := nodeArchiveDigest(sums, name)
	if e != nil {
		return e
	}
	b, e := publicDistribution(base+name, 64<<20)
	if e != nil {
		return e
	}
	if digest(b) != want {
		return errors.New("official Node22 distribution hash mismatch")
	}
	if e = extractZip(filepath.Join(p.runtime(), "sdk", "node22"), b); e != nil {
		return e
	}
	return saveNew(filepath.Join(p.Root, "node-distribution.json"), map[string]any{"url": base + name, "sha256": want, "bytes": len(b), "version": nodeRelease})
}

func initializeNode(p Plan, env []string) error {
	node := filepath.Join(nodeHome(p), "node.exe")
	b, e := sdkCommand(p, "node22", node, env, "--version")
	if e != nil {
		return e
	}
	if strings.TrimSpace(string(b)) != nodeRelease {
		return errors.New("prepared Node22 version differs")
	}
	// npm's config and cache are inside this job's private home. Installation
	// executes no package scripts and cannot touch the owner's global tools.
	args := []string{filepath.Join(nodeHome(p), "node_modules", "npm", "bin", "npm-cli.js"), "install", "--global", "--prefix", nodeHome(p), "--cache", filepath.Join(p.runtime(), "npm-cache"), "--registry=https://registry.npmjs.org", "--ignore-scripts", "--no-audit", "--no-fund", "pnpm@" + pnpmRelease}
	if _, e = sdkCommand(p, "pnpm-install", node, env, args...); e != nil {
		return e
	}
	b, e = sdkCommand(p, "pnpm-version", node, env, filepath.Join(nodeHome(p), "node_modules", "pnpm", "bin", "pnpm.cjs"), "--version")
	if e != nil {
		return e
	}
	if strings.TrimSpace(string(b)) != pnpmRelease {
		return errors.New("prepared pnpm version differs")
	}
	return nil
}

func measureWebView(p Plan) error {
	// Read only machine SDK installations. Never discover an owner's profile.
	for _, base := range []string{`C:\Program Files (x86)\Microsoft\EdgeWebView\Application`, `C:\Program Files\Microsoft\EdgeWebView\Application`} {
		entries, e := os.ReadDir(base)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if !entry.IsDir() || strings.Trim(entry.Name(), "0123456789.") != "" {
				continue
			}
			path := filepath.Join(base, entry.Name(), "msedgewebview2.exe")
			f, e := os.Open(path)
			if e != nil {
				continue
			}
			fi, e := f.Stat()
			f.Close()
			if e == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
				return saveNew(filepath.Join(p.runtime(), "sdk-webview2.json"), map[string]any{"path": path, "version": entry.Name(), "read_access": true, "shared_acl_changed": false})
			}
		}
	}
	return errors.New("machine WebView2 runtime unavailable to filtered service account")
}
