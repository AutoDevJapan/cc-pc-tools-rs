//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

const rustToolchainVersion = "1.95.0"
const rustToolchainHost = "x86_64-pc-windows-msvc"
const rustToolchainName = rustToolchainVersion + "-" + rustToolchainHost
const rustToolchainInstallRoot = "C:/Users/makhmeto/.rustup"
const rustToolchainPhysicalRoot = "D:/CC/Toolchains/rustup"
const rustToolchainManifestSHA = "821ff14e4c4a1cbe1e8915f35aff0a3fbbdf8d293ad48ab8f31e3b0440c581f9"

// Public release metadata, not hashes or qualification of the installed files.
// This fixed projection is from channel-rust-1.95.0.toml, date 2026-04-16.
const rustToolchainCargoURL = "https://static.rust-lang.org/dist/2026-04-16/cargo-1.95.0-x86_64-pc-windows-msvc.tar.gz"
const rustToolchainCargoArchiveSHA = "2d68113a00b98f0dec6d0e8473f82e08cec00c392115933a57dbfe9d3c8b2d8c"
const rustToolchainRustcURL = "https://static.rust-lang.org/dist/2026-04-16/rustc-1.95.0-x86_64-pc-windows-msvc.tar.gz"
const rustToolchainRustcArchiveSHA = "b1101cba184fda0da47658772d04423fdb86cc9ed888cac3b29d0e9f55faec53"
const rustToolchainStdURL = "https://static.rust-lang.org/dist/2026-04-16/rust-std-1.95.0-x86_64-pc-windows-msvc.tar.gz"
const rustToolchainStdArchiveSHA = "aa56f95b4817f562c0ada0abee3511a802a948303404e8fc872d0371ae0693fc"

// Only a normal, source-bound software-origin factory may supply this provider.
// There is deliberately no JSON/CLI receipt decoder, default native factory,
// path opener, execution operation or resource-guard override in this slice.
// WithHeld calls visit synchronously exactly once on success; it retains the
// alias, parents, leaves and buffers, then reports recheck/close failures.
type rustToolchainOriginProvider interface {
	WithHeld(context.Context, func(rustToolchainHeld) bool) error
}

type rustToolchainComponent struct {
	Name, URL, ArchiveSHA, ExecutableSHA string
}

// ExecutableSHA is derived by the provider from the authenticated component
// payload. Matching this struct cannot authenticate its producer by itself.
type rustToolchainOrigin struct {
	Version, Host, ManifestSHA string
	Components                [3]rustToolchainComponent
}

type rustToolchainAlias struct {
	InstallRoot, PhysicalRoot                         string
	AliasIdentity, ResolvedIdentity, TargetIdentity    softwareIdentity
	AliasACLSHA, TargetACLSHA                          string
}

type rustToolchainHeldFile struct {
	InstallPath, PhysicalPath        string
	Identity, ViaInstallIdentity     softwareIdentity
	ACLSHA                           string
	Raw                              []byte
}

// Borrowed inputs are valid only within WithHeld. The consumer must use the
// same buffers and recheck before and after consuming them; it must not reopen
// PhysicalPath or retain Raw beyond the callback. The native provider must
// verify the one fixed junction, plain descendants, single-link regular files,
// actual ACL policy and current identities before it reads any installed body.
type rustToolchainHeld struct {
	Origin       rustToolchainOrigin
	Alias        rustToolchainAlias
	Cargo, Rustc rustToolchainHeldFile
	Recheck      func(context.Context) error
}

// This is a pure consistency check, not an origin authenticator or a grant.
// The provider's qualification precedes construction of the held inputs; a
// caller-supplied path, digest or fabricated provider cannot establish it.
func rustToolchainInputsMatch(h rustToolchainHeld) bool {
	if h.Recheck == nil || h.Origin.Version != rustToolchainVersion || h.Origin.Host != rustToolchainHost || h.Origin.ManifestSHA != rustToolchainManifestSHA {
		return false
	}
	expected := [3]rustToolchainComponent{
		{Name: "cargo", URL: rustToolchainCargoURL, ArchiveSHA: rustToolchainCargoArchiveSHA},
		{Name: "rustc", URL: rustToolchainRustcURL, ArchiveSHA: rustToolchainRustcArchiveSHA},
		{Name: "rust-std", URL: rustToolchainStdURL, ArchiveSHA: rustToolchainStdArchiveSHA},
	}
	for i, component := range h.Origin.Components {
		if component.Name != expected[i].Name || component.URL != expected[i].URL || component.ArchiveSHA != expected[i].ArchiveSHA {
			return false
		}
		if i < 2 {
			if !rustToolchainSHA(component.ExecutableSHA) {
				return false
			}
		} else if component.ExecutableSHA != "" {
			return false
		}
	}
	a := h.Alias
	if a.InstallRoot != rustToolchainInstallRoot || a.PhysicalRoot != rustToolchainPhysicalRoot || !rustToolchainIdentity(a.AliasIdentity) || !rustToolchainIdentity(a.TargetIdentity) || a.ResolvedIdentity != a.TargetIdentity || a.AliasIdentity == a.TargetIdentity || !rustToolchainSHA(a.AliasACLSHA) || !rustToolchainSHA(a.TargetACLSHA) {
		return false
	}
	if h.Cargo.Identity == h.Rustc.Identity {
		return false
	}
	for i, file := range [2]rustToolchainHeldFile{h.Cargo, h.Rustc} {
		leaf := expected[i].Name + ".exe"
		suffix := "/toolchains/" + rustToolchainName + "/bin/" + leaf
		if file.InstallPath != rustToolchainInstallRoot+suffix || file.PhysicalPath != rustToolchainPhysicalRoot+suffix || !rustToolchainIdentity(file.Identity) || file.ViaInstallIdentity != file.Identity || file.Identity.Volume != a.TargetIdentity.Volume || !rustToolchainSHA(file.ACLSHA) || len(file.Raw) == 0 || len(file.Raw) > 64<<20 {
			return false
		}
		sum := sha256.Sum256(file.Raw)
		if hex.EncodeToString(sum[:]) != h.Origin.Components[i].ExecutableSHA {
			return false
		}
	}
	return true
}

func rustToolchainIdentity(id softwareIdentity) bool {
	return rustToolchainDecimal(id.Volume, 32, true) && rustToolchainDecimal(id.FileID, 64, false) && rustToolchainDecimal(id.Created, 64, false)
}

func rustToolchainDecimal(s string, bits int, zero bool) bool {
	v, err := strconv.ParseUint(s, 10, bits)
	return err == nil && (zero || v != 0) && strconv.FormatUint(v, 10) == s
}

func rustToolchainSHA(s string) bool {
	if len(s) != 64 || s == strings.Repeat("0", 64) {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
