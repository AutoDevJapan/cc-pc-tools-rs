//go:build windows && amd64

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeChecksumRequiresOneExactOfficialAsset(t *testing.T) {
	name := "node-" + nodeRelease + "-win-x64.zip"
	hash := strings.Repeat("a", 64)
	for _, raw := range []string{"", hash + "  other.zip", "bad  " + name, hash + "  " + name + "\n" + hash + "  " + name} {
		if _, e := nodeArchiveDigest([]byte(raw), name); e == nil {
			t.Fatal("invalid checksum inventory accepted")
		}
	}
	if got, e := nodeArchiveDigest([]byte(hash+"  "+name+"\r\n"), name); e != nil || got != hash {
		t.Fatal(got, e)
	}
}

func TestWorkerUsesOnlyItsPrivateSDKAndHomes(t *testing.T) {
	p := fixturePlan()
	for _, k := range []string{"HOME", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "GH_CONFIG_DIR", "npm_config_userconfig"} {
		t.Setenv(k, "owner-must-not-propagate")
	}
	m := map[string]string{}
	for _, entry := range envFor(p) {
		k, v, _ := strings.Cut(entry, "=")
		if strings.Contains(v, "owner-must-not-propagate") {
			t.Fatal("owner profile propagated", k)
		}
		m[k] = v
	}
	for _, k := range []string{"HOME", "USERPROFILE", "LOCALAPPDATA", "APPDATA", "CARGO_HOME", "RUSTUP_HOME", "npm_config_cache", "npm_config_userconfig", "PNPM_HOME"} {
		if e := beneath(p.Root, m[k]); e != nil {
			t.Fatal(k, e)
		}
	}
	if !strings.HasPrefix(m["PATH"], nodeHome(p)+";") || m["PSW_ROOT"] != p.Root || m["PSW_HELPER"] != filepath.Join(p.Root, "helper.exe") || m["RUSTUP_TOOLCHAIN"] != "stable" {
		t.Fatal("prepared SDK contract differs")
	}
	if m["GH_CONFIG_DIR"] != "" {
		t.Fatal("GitHub auth configuration crossed to worker")
	}
}

func TestServiceLockDoesNotDependOnCallerEnvironment(t *testing.T) {
	p := fixturePlan()
	a := claimLockPath(p)
	t.Setenv("ProgramData", t.TempDir())
	if b := claimLockPath(p); a != b || !filepath.IsAbs(b) || !strings.Contains(b, `passshare-ci-windows`) {
		t.Fatal("service and gate lock namespaces can diverge", a, b)
	}
}

func TestSDKRootPathBudgetBeforeMachineMutation(t *testing.T) {
	for _, root := range []string{strings.Repeat("a", 101), strings.Repeat("😀", 51)} {
		if sdkPathBudget(root) == nil {
			t.Fatal("unsupported MSVC root accepted")
		}
	}
	if e := sdkPathBudget(`D:\CC\Work\p2997\codex-owned-` + strings.Repeat("a", 32)); e != nil {
		t.Fatal(e)
	}
}
