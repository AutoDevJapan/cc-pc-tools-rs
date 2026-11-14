//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
)

// These bytes describe no installed executable and never reach a native open.
// The fixture's provider provenance is synthetic, independent of matcher output.
func rustToolchainSyntheticInputs() rustToolchainHeld {
	hash := func(raw []byte) string {
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	id := func(file uint64) softwareIdentity {
		return softwareIdentity{Volume: "1234", FileID: strconv.FormatUint(file, 10), Created: "134000000000000000"}
	}
	cargo, rustc := []byte("synthetic fixed cargo component payload"), []byte("synthetic fixed rustc component payload")
	return rustToolchainHeld{
		Origin: rustToolchainOrigin{
			Version: "1.95.0", Host: "x86_64-pc-windows-msvc",
			ManifestSHA: "821ff14e4c4a1cbe1e8915f35aff0a3fbbdf8d293ad48ab8f31e3b0440c581f9",
			Components: [3]rustToolchainComponent{
				{Name: "cargo", URL: "https://static.rust-lang.org/dist/2026-04-16/cargo-1.95.0-x86_64-pc-windows-msvc.tar.gz", ArchiveSHA: "2d68113a00b98f0dec6d0e8473f82e08cec00c392115933a57dbfe9d3c8b2d8c", ExecutableSHA: hash(cargo)},
				{Name: "rustc", URL: "https://static.rust-lang.org/dist/2026-04-16/rustc-1.95.0-x86_64-pc-windows-msvc.tar.gz", ArchiveSHA: "b1101cba184fda0da47658772d04423fdb86cc9ed888cac3b29d0e9f55faec53", ExecutableSHA: hash(rustc)},
				{Name: "rust-std", URL: "https://static.rust-lang.org/dist/2026-04-16/rust-std-1.95.0-x86_64-pc-windows-msvc.tar.gz", ArchiveSHA: "aa56f95b4817f562c0ada0abee3511a802a948303404e8fc872d0371ae0693fc"},
			},
		},
		Alias: rustToolchainAlias{
			InstallRoot: "C:/Users/makhmeto/.rustup", PhysicalRoot: "D:/CC/Toolchains/rustup",
			AliasIdentity: id(1), ResolvedIdentity: id(2), TargetIdentity: id(2),
			AliasACLSHA: hash([]byte("synthetic alias acl")), TargetACLSHA: hash([]byte("synthetic target acl")),
		},
		Cargo: rustToolchainHeldFile{
			InstallPath: "C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/cargo.exe",
			PhysicalPath: "D:/CC/Toolchains/rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/cargo.exe",
			Identity: id(3), ViaInstallIdentity: id(3), ACLSHA: hash([]byte("synthetic cargo acl")), Raw: cargo,
		},
		Rustc: rustToolchainHeldFile{
			InstallPath: "C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/rustc.exe",
			PhysicalPath: "D:/CC/Toolchains/rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/rustc.exe",
			Identity: id(4), ViaInstallIdentity: id(4), ACLSHA: hash([]byte("synthetic rustc acl")), Raw: rustc,
		},
		Recheck: func(context.Context) error { return nil },
	}
}

func TestRustToolchainInputsMatchFixedHeldTupleWithoutIO(t *testing.T) {
	h := rustToolchainSyntheticInputs()
	cargo, rustc := bytes.Clone(h.Cargo.Raw), bytes.Clone(h.Rustc.Raw)
	h.Recheck = func(context.Context) error {
		t.Fatal("pure matcher must not call a provider or perform a recheck")
		return nil
	}
	if !rustToolchainInputsMatch(h) {
		t.Fatal("fixed synthetic tuple was rejected")
	}
	if !bytes.Equal(cargo, h.Cargo.Raw) || !bytes.Equal(rustc, h.Rustc.Raw) {
		t.Fatal("pure match mutated held bytes")
	}
}

func TestRustToolchainInputsRejectOriginSubstitution(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*rustToolchainHeld)
	}{
		{"zero-origin", func(h *rustToolchainHeld) { h.Origin = rustToolchainOrigin{} }},
		{"channel-alias", func(h *rustToolchainHeld) { h.Origin.Version = "stable" }},
		{"other-version", func(h *rustToolchainHeld) { h.Origin.Version = "1.95.1" }},
		{"other-host", func(h *rustToolchainHeld) { h.Origin.Host = "x86_64-pc-windows-gnu" }},
		{"missing-manifest", func(h *rustToolchainHeld) { h.Origin.ManifestSHA = "" }},
		{"other-manifest", func(h *rustToolchainHeld) { h.Origin.ManifestSHA = strings.Repeat("a", 64) }},
		{"duplicate-component", func(h *rustToolchainHeld) { h.Origin.Components[1] = h.Origin.Components[0] }},
		{"swapped-components", func(h *rustToolchainHeld) { h.Origin.Components[0], h.Origin.Components[1] = h.Origin.Components[1], h.Origin.Components[0] }},
		{"other-component", func(h *rustToolchainHeld) { h.Origin.Components[2].Name = "clippy" }},
		{"http-url", func(h *rustToolchainHeld) { h.Origin.Components[0].URL = strings.Replace(h.Origin.Components[0].URL, "https:", "http:", 1) }},
		{"mirror-url", func(h *rustToolchainHeld) { h.Origin.Components[0].URL = strings.Replace(h.Origin.Components[0].URL, "static.rust-lang.org", "other.invalid", 1) }},
		{"url-query", func(h *rustToolchainHeld) { h.Origin.Components[0].URL += "?redirect=1" }},
		{"other-archive", func(h *rustToolchainHeld) { h.Origin.Components[1].ArchiveSHA = strings.Repeat("a", 64) }},
		{"xz-not-gzip", func(h *rustToolchainHeld) { h.Origin.Components[0].URL = strings.TrimSuffix(h.Origin.Components[0].URL, ".gz") + ".xz" }},
		{"archive-is-not-executable", func(h *rustToolchainHeld) { h.Origin.Components[0].ExecutableSHA = h.Origin.Components[0].ArchiveSHA }},
		{"uppercase-executable-hash", func(h *rustToolchainHeld) { h.Origin.Components[0].ExecutableSHA = strings.ToUpper(h.Origin.Components[0].ExecutableSHA) }},
		{"std-executable-claim", func(h *rustToolchainHeld) { h.Origin.Components[2].ExecutableSHA = h.Origin.Components[1].ExecutableSHA }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := rustToolchainSyntheticInputs()
			tc.edit(&h)
			if rustToolchainInputsMatch(h) {
				t.Fatal("substituted distribution input accepted")
			}
		})
	}
}

func TestRustToolchainInputsRejectAliasAndHeldObjectSubstitution(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*rustToolchainHeld)
	}{
		{"unknown-alias", func(h *rustToolchainHeld) { h.Alias.AliasIdentity = softwareIdentity{} }},
		{"other-install-root", func(h *rustToolchainHeld) { h.Alias.InstallRoot += "-other" }},
		{"other-physical-root", func(h *rustToolchainHeld) { h.Alias.PhysicalRoot += "-other" }},
		{"unresolved-alias", func(h *rustToolchainHeld) { h.Alias.ResolvedIdentity = softwareIdentity{} }},
		{"other-target", func(h *rustToolchainHeld) { h.Alias.TargetIdentity.FileID = "99" }},
		{"alias-is-target", func(h *rustToolchainHeld) { h.Alias.AliasIdentity = h.Alias.TargetIdentity }},
		{"missing-alias-acl", func(h *rustToolchainHeld) { h.Alias.AliasACLSHA = "" }},
		{"missing-target-acl", func(h *rustToolchainHeld) { h.Alias.TargetACLSHA = "" }},
		{"missing-leaf-acl", func(h *rustToolchainHeld) { h.Cargo.ACLSHA = "" }},
		{"invalid-acl", func(h *rustToolchainHeld) { h.Rustc.ACLSHA = strings.Repeat("z", 64) }},
		{"zero-acl", func(h *rustToolchainHeld) { h.Cargo.ACLSHA = strings.Repeat("0", 64) }},
		{"rustup-proxy", func(h *rustToolchainHeld) { h.Cargo.InstallPath = "C:/Users/makhmeto/.cargo/bin/cargo.exe" }},
		{"ads-leaf", func(h *rustToolchainHeld) { h.Rustc.InstallPath += ":other" }},
		{"dot-segment", func(h *rustToolchainHeld) { h.Cargo.PhysicalPath = strings.Replace(h.Cargo.PhysicalPath, "/bin/", "/bin/../bin/", 1) }},
		{"other-leaf", func(h *rustToolchainHeld) { h.Rustc.PhysicalPath = h.Cargo.PhysicalPath }},
		{"other-install-object", func(h *rustToolchainHeld) { h.Cargo.ViaInstallIdentity.FileID = "99" }},
		{"different-volume", func(h *rustToolchainHeld) { h.Rustc.Identity.Volume = "99"; h.Rustc.ViaInstallIdentity = h.Rustc.Identity }},
		{"same-executable-object", func(h *rustToolchainHeld) { h.Rustc.Identity = h.Cargo.Identity; h.Rustc.ViaInstallIdentity = h.Cargo.Identity }},
		{"missing-file-id", func(h *rustToolchainHeld) { h.Cargo.Identity.FileID = ""; h.Cargo.ViaInstallIdentity = h.Cargo.Identity }},
		{"leading-zero-id", func(h *rustToolchainHeld) { h.Cargo.Identity.FileID = "03"; h.Cargo.ViaInstallIdentity = h.Cargo.Identity }},
		{"negative-created", func(h *rustToolchainHeld) { h.Cargo.Identity.Created = "-1"; h.Cargo.ViaInstallIdentity = h.Cargo.Identity }},
		{"zero-file-id", func(h *rustToolchainHeld) { h.Cargo.Identity.FileID = "0"; h.Cargo.ViaInstallIdentity = h.Cargo.Identity }},
		{"overflow-id", func(h *rustToolchainHeld) { h.Cargo.Identity.FileID = "18446744073709551616"; h.Cargo.ViaInstallIdentity = h.Cargo.Identity }},
		{"empty-held-cargo", func(h *rustToolchainHeld) { h.Cargo.Raw = nil }},
		{"changed-cargo", func(h *rustToolchainHeld) { h.Cargo.Raw[0] ^= 1 }},
		{"changed-rustc", func(h *rustToolchainHeld) { h.Rustc.Raw[0] ^= 1 }},
		{"swapped-buffers", func(h *rustToolchainHeld) { h.Cargo.Raw, h.Rustc.Raw = h.Rustc.Raw, h.Cargo.Raw }},
		{"missing-recheck", func(h *rustToolchainHeld) { h.Recheck = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := rustToolchainSyntheticInputs()
			tc.edit(&h)
			if rustToolchainInputsMatch(h) {
				t.Fatal("substituted alias or held object accepted")
			}
		})
	}
}

func TestRustToolchainInputsBoundHeldBytes(t *testing.T) {
	h := rustToolchainSyntheticInputs()
	h.Cargo.Raw = make([]byte, (64<<20)+1)
	sum := sha256.Sum256(h.Cargo.Raw)
	h.Origin.Components[0].ExecutableSHA = hex.EncodeToString(sum[:])
	if rustToolchainInputsMatch(h) {
		t.Fatal("oversized buffer accepted even with a matching payload digest")
	}
}
