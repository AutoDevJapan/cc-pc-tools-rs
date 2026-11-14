//go:build windows

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func syntheticSoftwareRustManifest() []byte {
	return []byte(fmt.Sprintf("manifest-version = \"2\"\ndate = \"2026-04-16\"\n[pkg.rust]\nversion = \"1.95.0 (59807616e 2026-04-14)\"\n[pkg.cargo.target.x86_64-pc-windows-msvc]\navailable = true\nurl = %q\nhash = %q\n[pkg.rustc.target.x86_64-pc-windows-msvc]\navailable = true\nurl = %q\nhash = %q\n[pkg.rust-std.target.x86_64-pc-windows-msvc]\navailable = true\nurl = %q\nhash = %q\n", rustToolchainCargoURL, rustToolchainCargoArchiveSHA, rustToolchainRustcURL, rustToolchainRustcArchiveSHA, rustToolchainStdURL, rustToolchainStdArchiveSHA))
}

func TestSoftwareRustManifestProjectionIsNotOriginAuthority(t *testing.T) {
	raw := syntheticSoftwareRustManifest()
	got, err := softwareRustManifestProjection(raw)
	if err != nil || got.Version != rustToolchainVersion || got.Host != rustToolchainHost || got.Components[0].URL != rustToolchainCargoURL || got.Components[1].ArchiveSHA != rustToolchainRustcArchiveSHA || got.Components[2].Name != "rust-std" {
		t.Fatal("fixed synthetic projection rejected", err)
	}
	// Structurally correct fields are deliberately insufficient: only the
	// independent complete official manifest digest reaches the native origin.
	checksum := []byte(rustToolchainManifestSHA+"  channel-rust-1.95.0.toml\n")
	if _, err := softwareRustVerifiedManifest(raw, checksum); err == nil {
		t.Fatal("synthetic manifest admitted as official source")
	}
	for _, fault := range []string{"version", "date", "host", "missing", "duplicate-section", "duplicate-key", "http", "foreign-host", "authority", "digest", "unavailable", "malformed"} {
		t.Run(fault, func(t *testing.T) {
			changed := string(raw)
			switch fault {
			case "version": changed = strings.Replace(changed, `version = "1.95.0 (`, `version = "1.96.0 (`, 1)
			case "date": changed = strings.Replace(changed, `date = "2026-04-16"`, `date = "2026-04-17"`, 1)
			case "host": changed = strings.ReplaceAll(changed, "target.x86_64-pc-windows-msvc]", "target.aarch64-pc-windows-msvc]")
			case "missing": changed = changed[:strings.Index(changed, "[pkg.rust-std.target.")]
			case "duplicate-section": changed += "\n[pkg.cargo.target.x86_64-pc-windows-msvc]\n"
			case "duplicate-key": changed = strings.Replace(changed, "available = true", "available = true\navailable = true", 1)
			case "http": changed = strings.Replace(changed, "https://static.rust-lang.org/", "http://static.rust-lang.org/", 1)
			case "foreign-host": changed = strings.Replace(changed, "https://static.rust-lang.org/", "https://example.invalid/", 1)
			case "authority": changed = strings.Replace(changed, "https://static.rust-lang.org/", "https://user@static.rust-lang.org/", 1)
			case "digest": changed = strings.Replace(changed, rustToolchainCargoArchiveSHA, strings.Repeat("a", 64), 1)
			case "unavailable": changed = strings.Replace(changed, "available = true", "available = false", 1)
			case "malformed": changed = strings.Replace(changed, "[pkg.rust]", "[pkg.rust", 1)
			}
			if _, err := softwareRustManifestProjection([]byte(changed)); err == nil { t.Fatal("invalid fixed metadata accepted", fault) }
		})
	}
}

type softwareRustSyntheticTarEntry struct { name string; kind byte; raw []byte }

func softwareRustSyntheticTar(t *testing.T, entries []softwareRustSyntheticTarEntry) []byte {
	t.Helper()
	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	w := tar.NewWriter(gz)
	for _, entry := range entries {
		h := &tar.Header{Name: entry.name, Typeflag: entry.kind, Mode: 0600, Size: int64(len(entry.raw))}
		if entry.kind == tar.TypeDir || entry.kind == tar.TypeSymlink || entry.kind == tar.TypeLink { h.Size = 0 }
		if entry.kind == tar.TypeSymlink || entry.kind == tar.TypeLink { h.Linkname = "foreign" }
		if err := w.WriteHeader(h); err != nil { t.Fatal(err) }
		if h.Size != 0 { if _, err := w.Write(entry.raw); err != nil { t.Fatal(err) } }
	}
	if w.Close() != nil || gz.Close() != nil { t.Fatal("synthetic archive close") }
	return out.Bytes()
}

func TestSoftwareRustArchiveReadsAuthenticatedSameBytesAndRejectsNamespaceFaults(t *testing.T) {
	root := "cargo-"+rustToolchainVersion+"-"+rustToolchainHost
	name := root+"/cargo/bin/cargo.exe"
	valid := []softwareRustSyntheticTarEntry{{root, tar.TypeDir, nil}, {name, tar.TypeReg, []byte("public synthetic executable")}}
	raw := softwareRustSyntheticTar(t, valid)
	got, err := softwareRustArchiveExecutable(context.Background(), "cargo", raw, digest(raw))
	if err != nil || string(got) != "public synthetic executable" { t.Fatal("valid synthetic payload rejected", err) }
	clear(got)
	for _, fault := range []string{"digest", "corrupt", "missing", "duplicate", "case", "traversal", "foreign-root", "link", "hardlink", "reserved", "colon", "cancel"} {
		t.Run(fault, func(t *testing.T) {
			entries := append([]softwareRustSyntheticTarEntry(nil), valid...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch fault {
			case "missing": entries = entries[:1]
			case "duplicate": entries = append(entries, entries[1])
			case "case": entries = append(entries, softwareRustSyntheticTarEntry{strings.ToUpper(name), tar.TypeReg, nil})
			case "traversal": entries = append(entries, softwareRustSyntheticTarEntry{root+"/../foreign", tar.TypeReg, nil})
			case "foreign-root": entries = append(entries, softwareRustSyntheticTarEntry{"foreign/data", tar.TypeReg, nil})
			case "link": entries = append(entries, softwareRustSyntheticTarEntry{root+"/link", tar.TypeSymlink, nil})
			case "hardlink": entries = append(entries, softwareRustSyntheticTarEntry{root+"/link", tar.TypeLink, nil})
			case "reserved": entries = append(entries, softwareRustSyntheticTarEntry{root+"/NUL.txt", tar.TypeReg, nil})
			case "colon": entries = append(entries, softwareRustSyntheticTarEntry{root+"/a:stream", tar.TypeReg, nil})
			case "cancel": cancel()
			}
			candidate := softwareRustSyntheticTar(t, entries)
			expected := digest(candidate)
			if fault == "digest" { expected = strings.Repeat("a", 64) }
			if fault == "corrupt" { candidate[len(candidate)-1] ^= 1; expected = digest(candidate) }
			result, err := softwareRustArchiveExecutable(ctx, "cargo", candidate, expected)
			if err == nil || result != nil { t.Fatal("invalid archive returned payload", fault) }
		})
	}
}

type softwareRustSyntheticAccess struct { origin rustToolchainOrigin; session *softwareRustSyntheticSession; mode string; origins, opens int }
type softwareRustSyntheticSession struct { held rustToolchainHeld; closeError bool; closed, checks int; mode string }

func (a *softwareRustSyntheticAccess) Origin(context.Context) (rustToolchainOrigin, error) {
	a.origins++
	if a.mode == "origin-error" { return rustToolchainOrigin{}, errors.New("private origin response") }
	return a.origin, nil
}
func (a *softwareRustSyntheticAccess) Open(context.Context, rustToolchainOrigin) (softwareRustHeldSession, error) {
	a.opens++
	if a.mode == "open-error" { return a.session, errors.New("private path") }
	return a.session, nil
}
func (s *softwareRustSyntheticSession) Held() rustToolchainHeld { return s.held }
func (s *softwareRustSyntheticSession) Close() error {
	s.closed++
	clear(s.held.Cargo.Raw); clear(s.held.Rustc.Raw)
	if s.closeError { return errors.New("private cleanup error") }
	return nil
}
func syntheticSoftwareRustProvider() (*softwareRustOriginProvider, *softwareRustSyntheticAccess) {
	base := newSyntheticUnslopToolchainProvider()
	s := &softwareRustSyntheticSession{held: base.held}
	s.held.Recheck = func(context.Context) error {
		s.checks++
		if s.mode == "before-check" || s.mode == "after-check" && s.checks == 2 { return errors.New("private check") }
		if s.mode == "before-mutates" && s.checks == 1 { s.held.Cargo.Raw[0] ^= 1 }
		return nil
	}
	a := &softwareRustSyntheticAccess{origin: s.held.Origin, session: s}
	return &softwareRustOriginProvider{access: a}, a
}

func TestSoftwareRustProviderRetainsThenClosesAndNeverTurnsLateFailureIntoSuccess(t *testing.T) {
	for _, mode := range []string{"normal", "origin-error", "open-error", "origin-mismatch", "held-mismatch", "before-check", "before-mutates", "after-check", "close-error", "consumer-failed", "consumer-mutates", "cancel-before", "cancel-during"} {
		t.Run(mode, func(t *testing.T) {
			provider, access := syntheticSoftwareRustProvider()
			s := access.session
			access.mode, s.mode = mode, mode
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "origin-mismatch" { access.origin.ManifestSHA = strings.Repeat("b", 64) }
			if mode == "held-mismatch" { s.held.Cargo.Identity.Created = "other" }
			if mode == "close-error" { s.closeError = true }
			if mode == "cancel-before" { cancel() }
			calls := 0
			err := provider.WithHeld(ctx, func(h rustToolchainHeld) bool {
				calls++
				if s.closed != 0 || s.checks != 1 || &h.Cargo.Raw[0] != &s.held.Cargo.Raw[0] { t.Fatal("consumer did not receive held bytes") }
				if mode == "consumer-mutates" { h.Cargo.Raw[0] ^= 1 }
				if mode == "cancel-during" { cancel() }
				return mode != "consumer-failed"
			})
			if (err == nil) != (mode == "normal") { t.Fatal("unexpected provider outcome", mode, err) }
			wantCalls := 1
			if mode == "origin-error" || mode == "open-error" || mode == "origin-mismatch" || mode == "held-mismatch" || mode == "before-check" || mode == "before-mutates" || mode == "cancel-before" { wantCalls = 0 }
			if calls != wantCalls { t.Fatal("invalid input reached callback", mode, calls) }
			if access.opens > 0 && s.closed != 1 { t.Fatal("opened session not closed", mode) }
			if err != nil && strings.Contains(err.Error(), "private") { t.Fatal("raw error escaped") }
		})
	}
	var provider *softwareRustOriginProvider
	if err := provider.WithHeld(context.Background(), func(rustToolchainHeld) bool { t.Fatal("nil provider callback"); return true }); err == nil { t.Fatal("nil provider accepted") }
	if normalSoftwareRustOriginProvider(fixedUnslopToolchainPurpose()) != nil { t.Fatal("native purpose qualification was fabricated") }
}

type softwareRustSyntheticTransport struct { calls int; response *http.Response }
func (s *softwareRustSyntheticTransport) RoundTrip(*http.Request) (*http.Response, error) { s.calls++; return s.response, nil }

func TestSoftwareRustPublicReadRejectsAddressAndStatusBeforeBody(t *testing.T) {
	for _, mode := range []string{"normal", "address", "status", "oversize", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			transport := &softwareRustSyntheticTransport{response: &http.Response{StatusCode: 200, ContentLength: 6, Body: io.NopCloser(strings.NewReader("public"))}}
			client := &http.Client{Transport: transport}
			ctx, cancel := context.WithCancel(context.Background()); defer cancel()
			address, limit := softwareRustManifestURL, int64(16)
			if mode == "address" { address = "https://example.invalid/token" }
			if mode == "status" { transport.response.StatusCode = 403 }
			if mode == "oversize" { limit = 2 }
			if mode == "cancel" { cancel() }
			raw, err := softwareRustPublicBytes(ctx, client, address, limit)
			if (err == nil) != (mode == "normal") || mode != "normal" && raw != nil { t.Fatal("unexpected public intake outcome", mode) }
			if (mode == "address" || mode == "cancel") && transport.calls != 0 { t.Fatal("invalid request reached transport") }
			clear(raw)
		})
	}
}
