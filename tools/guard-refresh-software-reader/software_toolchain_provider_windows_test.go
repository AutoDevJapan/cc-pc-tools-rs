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

type softwareRustRetainedOpeningAccess struct {
	base *softwareRustSyntheticAccess
	cancel context.CancelFunc
	mode string
}

func (a *softwareRustRetainedOpeningAccess) Origin(ctx context.Context) (rustToolchainOrigin, error) {
	origin, err := a.base.Origin(ctx)
	if a.mode == "cancel-origin" { a.cancel() }
	return origin, err
}

func (a *softwareRustRetainedOpeningAccess) Open(ctx context.Context, origin rustToolchainOrigin) (softwareRustHeldSession, error) {
	if a.mode == "missing-session" { a.base.opens++; return nil, nil }
	session, err := a.base.Open(ctx, origin)
	if a.mode == "cancel-open" { a.cancel() }
	return session, err
}

func TestSoftwareRustProviderOpenRetainedTransfersOnlyVerifiedSameLease(t *testing.T) {
	for _, mode := range []string{"normal", "origin-error", "open-error", "missing-session", "origin-mismatch", "held-mismatch", "before-check", "before-mutates", "cancel-before", "cancel-origin", "cancel-open", "cancel-recheck"} {
		t.Run(mode, func(t *testing.T) {
			_, access := syntheticSoftwareRustProvider()
			session := access.session
			access.mode, session.mode = mode, mode
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			provider := &softwareRustOriginProvider{access: &softwareRustRetainedOpeningAccess{base: access, cancel: cancel, mode: mode}}
			if mode == "origin-mismatch" { access.origin.ManifestSHA = strings.Repeat("b", 64) }
			if mode == "held-mismatch" { session.held.Cargo.Identity.Created = "other" }
			if mode == "cancel-before" { cancel() }
			if mode == "cancel-recheck" {
				original := session.held.Recheck
				session.held.Recheck = func(ctx context.Context) error { err := original(ctx); cancel(); return err }
			}
			lease, err := provider.openRetained(ctx)
			if mode == "normal" {
				if err != nil || lease != session || session.closed != 0 || session.checks != 1 { t.Fatal("verified lease was not transferred", err) }
				held := lease.Held()
				if !rustToolchainInputsMatch(held) || &held.Cargo.Raw[0] != &session.held.Cargo.Raw[0] { t.Fatal("lease did not retain original buffers") }
				if lease.Close() != nil || session.closed != 1 { t.Fatal("caller did not release exact lease") }
				for _, raw := range [][]byte{held.Cargo.Raw, held.Rustc.Raw} {
					for _, value := range raw { if value != 0 { t.Fatal("caller release left tool bytes") } }
				}
			} else {
				if err == nil || lease != nil { t.Fatal("invalid or cancelled opening returned a lease", mode) }
				wantClosed := 1
				if mode == "cancel-before" || mode == "cancel-origin" || mode == "origin-error" || mode == "missing-session" { wantClosed = 0 }
				if session.closed != wantClosed { t.Fatal("failed opening did not retain and close its own resources once", mode, session.closed) }
				if strings.Contains(err.Error(), "private") { t.Fatal("opening exposed free error text") }
			}
			if (mode == "cancel-before" || mode == "cancel-origin" || mode == "origin-error") && access.opens != 0 { t.Fatal("opening continued after origin failure") }
		})
	}
	var absent *softwareRustOriginProvider
	if lease, err := absent.openRetained(context.Background()); lease != nil || err == nil { t.Fatal("absent provider returned a lease") }
	provider, access := syntheticSoftwareRustProvider()
	if lease, err := provider.openRetained(nil); lease != nil || err == nil || access.origins != 0 { t.Fatal("nil context reached provider") }
}

func TestSoftwareRustProviderPreservesPrimaryAndCleanupFailures(t *testing.T) {
	for _, entry := range []string{"open-retained", "with-held"} {
		for _, mode := range []string{"open-error", "cancel-open", "origin-mismatch", "held-mismatch", "before-check", "before-mutates", "cancel-recheck", "normal", "consumer-failed", "consumer-mutates", "cancel-during", "after-check"} {
			if entry == "open-retained" && (mode == "normal" || mode == "consumer-failed" || mode == "consumer-mutates" || mode == "cancel-during" || mode == "after-check") { continue }
			t.Run(entry+"/"+mode, func(t *testing.T) {
				_, access := syntheticSoftwareRustProvider()
				session := access.session
				session.closeError, access.mode, session.mode = true, mode, mode
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				provider := &softwareRustOriginProvider{access: &softwareRustRetainedOpeningAccess{base: access, cancel: cancel, mode: mode}}
				primary := error(errSoftwareRustUnavailable)
				switch mode {
				case "origin-mismatch": access.origin.ManifestSHA = strings.Repeat("b", 64); primary = errSoftwareRustMismatch
				case "held-mismatch": session.held.Cargo.Identity.Created = "other"; primary = errSoftwareRustMismatch
				case "before-mutates", "consumer-mutates": primary = errSoftwareRustMismatch
				case "cancel-open", "cancel-during": primary = context.Canceled
				case "cancel-recheck":
					original := session.held.Recheck
					session.held.Recheck = func(ctx context.Context) error { err := original(ctx); cancel(); return err }
					primary = context.Canceled
				case "consumer-failed": primary = errSoftwareRustConsumer
				}
				calls := 0
				var err error
				if entry == "open-retained" {
					var lease softwareRustHeldSession
					lease, err = provider.openRetained(ctx)
					if lease != nil { _ = lease.Close(); t.Fatal("failed opening transferred a lease") }
				} else {
					err = provider.WithHeld(ctx, func(held rustToolchainHeld) bool {
						calls++
						if mode == "cancel-during" { cancel() }
						if mode == "consumer-mutates" { held.Cargo.Raw[0] ^= 1 }
						return mode != "consumer-failed"
					})
				}
				if !errors.Is(err, primary) || !errors.Is(err, errSoftwareRustCleanup) || err.Error() != primary.Error() {
					t.Fatal("primary or independent cleanup failure was lost", mode)
				}
				if session.closed != 1 || access.opens != 1 { t.Fatal("failed session was not closed exactly once", session.closed, access.opens) }
				wantCalls := 0
				if entry == "with-held" && (mode == "normal" || mode == "consumer-failed" || mode == "consumer-mutates" || mode == "cancel-during" || mode == "after-check") { wantCalls = 1 }
				if calls != wantCalls { t.Fatal("failed opening reached consumer", calls, wantCalls) }
				// Reclassification at another boundary must preserve both facts.
				fixed := softwareRustFixedError(ctx, err)
				if !errors.Is(fixed, primary) || !errors.Is(fixed, errSoftwareRustCleanup) || fixed.Error() != primary.Error() { t.Fatal("fixed projection erased a cause") }
			})
		}
	}
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

type softwareRustOpeningFixture struct {
	held, template rustToolchainHeld
	mode string
	cancel context.CancelFunc
	opens, reads, closed int
	closeError bool
}

func (s *softwareRustOpeningFixture) state() *rustToolchainHeld { return &s.held }
func (s *softwareRustOpeningFixture) Held() rustToolchainHeld { return s.held }
func (s *softwareRustOpeningFixture) openNode(path, resolved string, directory, reparse bool) (softwareRustNativeNode, error) {
	s.opens++
	id, acl := s.template.Alias.TargetIdentity, s.template.Alias.TargetACLSHA
	if reparse { id, acl = s.template.Alias.AliasIdentity, s.template.Alias.AliasACLSHA }
	if !directory {
		file := s.template.Rustc
		if strings.HasSuffix(path, "/cargo.exe") { file = s.template.Cargo }
		id, acl = file.Identity, file.ACLSHA
	}
	if s.mode == "parent" && s.opens == 1 || s.mode == "cargo-open" && !directory && strings.HasSuffix(path, "/cargo.exe") { s.cancel() }
	return softwareRustNativeNode{path:path, resolved:resolved, id:id, aclSHA:acl, reparse:reparse}, nil
}
func (s *softwareRustOpeningFixture) fixedJunction(softwareRustNativeNode) bool { return true }
func (s *softwareRustOpeningFixture) readNode(_ context.Context, _ softwareRustNativeNode, index int) ([]byte, error) {
	s.reads++
	raw := bytes.Clone(s.template.Rustc.Raw)
	if index == 0 { raw = bytes.Clone(s.template.Cargo.Raw); s.held.Cargo.Raw = raw } else { s.held.Rustc.Raw = raw }
	if s.mode == "cargo-read" && index == 0 { s.cancel() }
	return raw, nil
}
func (s *softwareRustOpeningFixture) recheck(ctx context.Context) error { return ctx.Err() }
func (s *softwareRustOpeningFixture) Close() error {
	s.closed++
	clear(s.held.Cargo.Raw); clear(s.held.Rustc.Raw)
	if s.closeError { return errors.New("private native cleanup text") }
	return nil
}

func TestSoftwareRustNativeOpeningCleanupSurvivesProviderProjection(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	base := newSyntheticUnslopToolchainProvider()
	session := &softwareRustOpeningFixture{template:base.held, mode:"parent", cancel:cancel, closeError:true}
	lease, err := softwareRustOpenWithSession(ctx, base.held.Origin, session)
	if lease != nil || session.closed != 1 || session.opens != 1 || session.reads != 0 { t.Fatal("native opening continued after cancellation") }
	fixed := softwareRustFixedError(ctx, err)
	if !errors.Is(err, context.Canceled) || !errors.Is(err, errSoftwareRustCleanup) || !errors.Is(fixed, context.Canceled) || !errors.Is(fixed, errSoftwareRustCleanup) { t.Fatal("native cleanup or cancellation was lost") }
	if fixed.Error() != context.Canceled.Error() { t.Fatal("native error text escaped") }
}

func TestSoftwareRustOpeningCancellationStopsLaterStages(t *testing.T) {
	for _, mode := range []string{"normal", "before", "parent", "cargo-open", "cargo-read"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background()); defer cancel()
			base := newSyntheticUnslopToolchainProvider()
			s := &softwareRustOpeningFixture{template:base.held, mode:mode, cancel:cancel}
			if mode == "before" { cancel() }
			result, err := softwareRustOpenWithSession(ctx, base.held.Origin, s)
			if mode == "normal" {
				if err != nil || result == nil || s.reads != 2 || s.closed != 0 { t.Fatal("normal opening failed", err) }
				if result.Close() != nil || s.closed != 1 { t.Fatal("normal session close") }
				return
			}
			if err == nil || result != nil || s.closed != 1 { t.Fatal("cancelled opening accepted or not closed", err) }
			wantReads := 0
			if mode == "cargo-read" { wantReads = 1 }
			if s.reads != wantReads { t.Fatal("cancelled opening performed later body read", mode, s.reads, wantReads) }
			if mode == "before" && s.opens != 0 || mode == "parent" && s.opens != 1 { t.Fatal("cancelled opening performed later metadata open", mode, s.opens) }
		})
	}
}
