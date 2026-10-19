//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func sdkFixture(t *testing.T) (config, receipt, *sdkScope) {
	t.Helper()
	c, root := fixture(t)
	r := owned(t, c, root)
	bin := filepath.Join(r.Path, sdkBin)
	if err := os.MkdirAll(bin, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(bin, "rustup.exe"), "synthetic rustup")
	for _, name := range sdkAliases {
		if err := os.Symlink("rustup.exe", filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	return c, r, &sdkScope{}
}

func TestSDKQuarantinePreservesRelativeLinksAndAllBytes(t *testing.T) {
	c, r, s := sdkFixture(t)
	_, err := c.quarantine(r.ID, "sdk-links")
	code(t, err, "SDK_RECOVERY_SCOPE_MISMATCH")
	_, err = c.plan(r.ID)
	code(t, err, "LINK_OR_REPARSE_POINT")
	q, err := c.quarantineScoped(r.ID, "sdk-links", s)
	if err != nil {
		t.Fatal(err)
	}
	if exists(r.Path) || q["bytes"] != int64(16) {
		t.Fatal(q)
	}
	if target, err := os.Readlink(filepath.Join(q["destination"].(string), "files", sdkBin, "cargo.exe")); err != nil || target != "rustup.exe" {
		t.Fatal(target, err)
	}
}

func TestSDKRejectsUnlistedLinkAndExternalHardlink(t *testing.T) {
	for _, kind := range []string{"symlink", "hardlink", "git", "secret"} {
		t.Run(kind, func(t *testing.T) {
			c, r, s := sdkFixture(t)
			outside := filepath.Join(filepath.Dir(r.Path), "outside")
			write(t, outside, "retained external data")
			var err error
			switch kind {
			case "symlink":
				err = os.Symlink(outside, filepath.Join(r.Path, "extra"))
			case "hardlink":
				err = os.Link(outside, filepath.Join(r.Path, "extra"))
			case "git":
				err = os.Mkdir(filepath.Join(r.Path, ".git"), 0700)
			case "secret":
				write(t, filepath.Join(r.Path, ".credentials"), "synthetic only")
			}
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.quarantineScoped(r.ID, "sdk-links", s)
			if err == nil || !exists(r.Path) || exists(c.Quarantine) {
				t.Fatal("unexpected move", err)
			}
		})
	}
}

func TestSDKRejectsAliasRetargetAndContentChange(t *testing.T) {
	for _, kind := range []string{"retarget", "content", "missing", "absolute", "junction-directory"} {
		t.Run(kind, func(t *testing.T) {
			_, r, s := sdkFixture(t)
			before, err := s.snapshot(r.Path)
			if err != nil {
				t.Fatal(err)
			}
			alias := filepath.Join(r.Path, sdkBin, "cargo.exe")
			if kind == "content" {
				write(t, filepath.Join(r.Path, sdkBin, "rustup.exe"), "modified rustup")
			} else {
				if err = os.Remove(alias); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "retarget":
					err = os.Symlink("missing.exe", alias)
				case "absolute":
					err = os.Symlink(filepath.Join(r.Path, sdkBin, "rustup.exe"), alias)
				case "junction-directory":
					err = os.Symlink(filepath.Dir(alias), alias)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if err = s.verify(r.Path, before, r); err == nil {
				t.Fatal("accepted changed inventory")
			}
		})
	}
}

func TestSDKReceiptScopeCannotBeForgedWithNewOwnedReceipt(t *testing.T) {
	c, root := fixture(t)
	r := owned(t, c, root)
	r.ID = "7155bdd37afc8e75e4dbf39509a5f18c"
	r.Path = filepath.Join(root, "codex-owned-"+r.ID)
	if err := os.Mkdir(r.Path, 0700); err != nil {
		t.Fatal(err)
	}
	r.Integrity = digest(r)
	modify(t, c, r)
	_, err := c.sdkRecovery(r.ID)
	code(t, err, "TARGET_IDENTITY_CHANGED")
}

func TestSDKInternalCacheJunctionPreservedWithoutFollowing(t *testing.T) {
	c, r, _ := sdkFixture(t)
	s := &sdkScope{source: r.Path}
	target := filepath.Join(r.Path, sdkCache, "IE")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(target, "synthetic-cache"), "synthetic")
	junction := filepath.Join(r.Path, sdkCache, "Content.IE5")
	if _, err := runHidden(10*time.Second, "cmd.exe", "/c", "mklink", "/J", junction, target); err != nil {
		t.Fatal(err)
	}
	before, err := s.snapshot(r.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, duplicate := before[filepath.Join(sdkCache, "Content.IE5", "synthetic-cache")]; duplicate {
		t.Fatal("followed directory junction")
	}
	q, err := c.quarantineScoped(r.ID, "sdk-links", s)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.Readlink(filepath.Join(q["destination"].(string), "files", sdkCache, "Content.IE5"))
	if err != nil || !sdkTargetSame(got, target) {
		t.Fatal("changed absolute junction bytes", got, err)
	}
}

func TestSDKExternalCacheJunctionRejectedBeforeHashing(t *testing.T) {
	c, r, _ := sdkFixture(t)
	s := &sdkScope{source: r.Path}
	cache := filepath.Join(r.Path, sdkCache)
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := runHidden(10*time.Second, "cmd.exe", "/c", "mklink", "/J", filepath.Join(cache, "Content.IE5"), filepath.Dir(r.Path)); err != nil {
		t.Fatal(err)
	}
	_, err := c.quarantineScoped(r.ID, "sdk-links", s)
	code(t, err, "SDK_CACHE_JUNCTION_MISMATCH")
	if !exists(r.Path) || exists(c.Quarantine) {
		t.Fatal("moved external junction")
	}
}
