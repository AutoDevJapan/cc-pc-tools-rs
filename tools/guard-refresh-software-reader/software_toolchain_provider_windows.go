//go:build windows

package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"
)

const softwareRustManifestURL = "https://static.rust-lang.org/dist/channel-rust-1.95.0.toml"
const softwareRustManifestLimit = 1 << 20
const softwareRustArchiveLimit = 256 << 20
const softwareRustExpandedLimit = 2 << 30
const softwareRustEntryLimit = 131072

var errSoftwareRustUnavailable = errors.New("toolchain_origin_unavailable")
var errSoftwareRustMismatch = errors.New("toolchain_inputs_mismatch")
var errSoftwareRustConsumer = errors.New("toolchain_consumer_failed")
var errSoftwareRustCleanup = errors.New("toolchain_cleanup_failed")

// Keep the established primary classification while exposing cleanup failure
// independently through errors.Is. Neither cause contains an operating-system
// message, path, response body or other free-form native error text.
type softwareRustCleanupFailure struct{ primary error }

func (e *softwareRustCleanupFailure) Error() string { return e.primary.Error() }
func (e *softwareRustCleanupFailure) Unwrap() []error {
	return []error{e.primary, errSoftwareRustCleanup}
}

func softwareRustWithCleanupFailure(primary error) error {
	if primary == nil { primary = errSoftwareRustUnavailable }
	if errors.Is(primary, errSoftwareRustCleanup) { return primary }
	return &softwareRustCleanupFailure{primary: primary}
}

// These interfaces are internal test seams. Only the concrete, fixed-purpose
// constructor below supplies production access; no JSON, path or digest is
// decoded into an implementation of these interfaces.
type softwareRustHeldSession interface {
	// Held returns the same tuple and borrowed buffers for the session lifetime.
	Held() rustToolchainHeld
	Close() error
}

type softwareRustProviderAccess interface {
	Origin(context.Context) (rustToolchainOrigin, error)
	Open(context.Context, rustToolchainOrigin) (softwareRustHeldSession, error)
}

type softwareRustOriginProvider struct{
	access softwareRustProviderAccess
	mu sync.Mutex
	borrowing bool
	retained bool
	owned map[softwareRustHeldSession]*softwareRustOwnedCapability
}

// Only openOwned records an acquired session in its issuer's private ledger.
// The borrowed WithHeld tuple cannot be converted into this capability.
type softwareRustOwnedCapability struct {
	mu sync.Mutex
	issuer *softwareRustOriginProvider
	session softwareRustHeldSession
	lease,holder *softwareRustOwnedSession
	held rustToolchainHeld
	claimed,closed,closing bool
	proof *unslopOwnedCaptureProof
	closeErr error
}

func (p *softwareRustOriginProvider) openOwned(ctx context.Context) (*softwareRustOwnedCapability,error) {
	if p==nil{return nil,errSoftwareRustUnavailable}
	p.mu.Lock();defer p.mu.Unlock()
	// One issuer instance represents one owned lifecycle, including after Close.
	// A later acquisition needs a fresh issuer and cannot revive its old proof.
	if p.borrowing||p.retained||len(p.owned)!=0{return nil,errSoftwareRustMismatch}
	session,err:=p.openRetainedSession(ctx)
	if err!=nil{return nil,err}
	if !unslopRetainedPointer(session){if session!=nil {_ = session.Close()};return nil,errSoftwareRustMismatch}
	if p.owned[session]!=nil{return nil,errSoftwareRustMismatch}
	if _,wrapped:=session.(*softwareRustOwnedSession);wrapped{return nil,errSoftwareRustMismatch}
	if p.owned==nil {p.owned=make(map[softwareRustHeldSession]*softwareRustOwnedCapability)}
	c:=&softwareRustOwnedCapability{issuer:p,session:session,held:session.Held()}
	c.lease=&softwareRustOwnedSession{capability:c};c.holder=c.lease
	p.owned[session]=c
	return c,nil
}

func (c *softwareRustOwnedCapability) current() bool {
	if c==nil||c.issuer==nil||!unslopRetainedPointer(c.session){return false}
	c.issuer.mu.Lock();issued:=c.issuer.owned[c.session]==c;c.issuer.mu.Unlock()
	c.mu.Lock();defer c.mu.Unlock()
	if !issued||c.closed{return false}
	held:=c.session.Held()
	return len(held.Cargo.Raw)>0&&len(held.Rustc.Raw)>0&&len(c.held.Cargo.Raw)>0&&len(c.held.Rustc.Raw)>0&&&held.Cargo.Raw[0]==&c.held.Cargo.Raw[0]&&&held.Rustc.Raw[0]==&c.held.Rustc.Raw[0]&&rustToolchainInputsMatch(held)
}

func (c *softwareRustOwnedCapability) claim(proof *unslopOwnedCaptureProof) bool {
	if !c.current(){return false}
	c.mu.Lock();defer c.mu.Unlock()
	if c.claimed||c.closed||proof==nil||proof.capability!=c||proof.controller==nil||proof.controller.journal==nil||proof.generation!=proof.controller.journal.generation {return false}
	c.claimed=true;c.proof=proof;return true
}

func (c *softwareRustOwnedCapability) Close() error {
	if c==nil{return nil}
	c.mu.Lock();claimed:=c.claimed;lease:=c.lease;c.mu.Unlock()
	if claimed||lease==nil{return errSoftwareRustMismatch}
	return lease.Close()
}

type softwareRustOwnedSession struct {
	capability *softwareRustOwnedCapability
	owner *unslopRetainedOwner
	ticket *unslopRetainedTicket
}

func (s *softwareRustOwnedSession) current() bool {
	if s==nil||s.capability==nil||!s.capability.current(){return false}
	c:=s.capability;c.mu.Lock();defer c.mu.Unlock()
	return !c.closed&&c.holder==s
}

func (s *softwareRustOwnedSession) Held() rustToolchainHeld {
	if !s.current(){return rustToolchainHeld{}}
	return s.capability.session.Held()
}

func (s *softwareRustOwnedSession) move() (*softwareRustOwnedSession,error) {
	if !s.current(){return nil,errSoftwareRustMismatch}
	c:=s.capability;c.mu.Lock();defer c.mu.Unlock()
	if c.closed||c.holder!=s{return nil,errSoftwareRustMismatch}
	next:=&softwareRustOwnedSession{capability:c};c.holder=next
	return next,nil
}

func (s *softwareRustOwnedSession) actual() (softwareRustHeldSession,bool) {
	if !s.current(){return nil,false}
	return s.capability.session,true
}

func (s *softwareRustOwnedSession) Close() error {
	if s==nil||s.capability==nil{return errSoftwareRustMismatch}
	c:=s.capability
	if c.issuer==nil||!unslopRetainedPointer(c.session){return errSoftwareRustMismatch}
	c.issuer.mu.Lock();issued:=c.issuer.owned[c.session]==c;c.issuer.mu.Unlock()
	if !issued{return errSoftwareRustMismatch}
	c.mu.Lock()
	if c.holder!=s{c.mu.Unlock();return errSoftwareRustMismatch}
	if c.closed{err:=c.closeErr;if c.closing{err=errSoftwareRustCleanup};c.mu.Unlock();return err}
	proof:=c.proof;c.mu.Unlock()
	if s.owner!=nil {
		s.owner.mu.Lock();entry:=s.owner.entries[s.ticket]
		allowed:=entry!=nil&&entry.lease==s&&entry.report.State=="release_in_progress"&&entry.report.Collection=="verified"&&entry.report.CloseAttempts==1
		s.owner.mu.Unlock();if !allowed{return errSoftwareRustMismatch}
	} else if proof!=nil&&!proof.canCloseLease(){return errSoftwareRustMismatch}
	c.mu.Lock()
	if c.holder!=s||c.closed{c.mu.Unlock();return errSoftwareRustMismatch}
	// Invalidate before native Close. Even failure never permits a second Close.
	c.closed=true;c.closing=true;session:=c.session;c.mu.Unlock()
	err:=session.Close()
	c.mu.Lock();c.closeErr=err;c.closing=false;c.mu.Unlock()
	return err
}

// openRetained returns ownership of one verified session to the normal
// controller. The caller must keep it until native collection is proven, or
// move it to the retained owner. It does not enable the normal factory.
func (p *softwareRustOriginProvider) openRetained(ctx context.Context) (session softwareRustHeldSession, result error) {
	if p==nil{return nil,errSoftwareRustUnavailable}
	p.mu.Lock();defer p.mu.Unlock()
	if p.borrowing||p.retained||len(p.owned)!=0{return nil,errSoftwareRustMismatch}
	session,err:=p.openRetainedSession(ctx);if err==nil {p.retained=true}
	return session,err
}

func (p *softwareRustOriginProvider) openRetainedSession(ctx context.Context) (session softwareRustHeldSession, result error) {
	if ctx == nil {
		return nil, context.Canceled
	}
	if ctx.Err() != nil { return nil, ctx.Err() }
	if p == nil || p.access == nil {
		return nil, errors.New("toolchain_origin_provider_unavailable")
	}
	origin, err := p.access.Origin(ctx)
	if err != nil {
		return nil, softwareRustFixedError(ctx, err)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	opened, err := p.access.Open(ctx, origin)
	transferred := false
	defer func() {
		if !transferred && opened != nil {
			if opened.Close() != nil {
				result = softwareRustWithCleanupFailure(softwareRustFixedError(ctx, result))
			}
		}
	}()
	if err != nil || opened == nil {
		return nil, softwareRustFixedError(ctx, err)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	held := opened.Held()
	if held.Origin != origin || !rustToolchainInputsMatch(held) {
		return nil, errSoftwareRustMismatch
	}
	if err = held.Recheck(ctx); err != nil {
		return nil, softwareRustFixedError(ctx, err)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !rustToolchainInputsMatch(held) {
		return nil, errSoftwareRustMismatch
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	transferred = true
	return opened, nil
}

func (p *softwareRustOriginProvider) WithHeld(ctx context.Context, visit func(rustToolchainHeld) bool) (result error) {
	if ctx == nil {
		return context.Canceled
	}
	if ctx.Err() != nil { return ctx.Err() }
	if p == nil || p.access == nil {
		return errors.New("toolchain_origin_provider_unavailable")
	}
	if visit == nil {
		return errSoftwareRustConsumer
	}
	p.mu.Lock()
	if p.borrowing||p.retained||len(p.owned)!=0 {p.mu.Unlock();return errSoftwareRustMismatch}
	p.borrowing=true;p.mu.Unlock()
	defer func(){p.mu.Lock();p.borrowing=false;p.mu.Unlock()}()
	session, err := p.openRetainedSession(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if session.Close() != nil {
			result = softwareRustWithCleanupFailure(softwareRustFixedError(ctx, result))
		}
	}()
	held := session.Held()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	accepted := visit(held)
	checkErr := held.Recheck(ctx)
	matches := rustToolchainInputsMatch(held)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if checkErr != nil {
		return softwareRustFixedError(ctx, checkErr)
	}
	if !matches {
		return errSoftwareRustMismatch
	}
	if !accepted {
		return errSoftwareRustConsumer
	}
	return nil
}

func softwareRustFixedError(ctx context.Context, err error) error {
	var primary error = errSoftwareRustUnavailable
	if ctx != nil && ctx.Err() != nil {
		primary = ctx.Err()
	} else if errors.Is(err, context.Canceled) {
		primary = context.Canceled
	} else if errors.Is(err, context.DeadlineExceeded) {
		primary = context.DeadlineExceeded
	} else if errors.Is(err, errSoftwareRustMismatch) {
		primary = errSoftwareRustMismatch
	} else if errors.Is(err, errSoftwareRustConsumer) {
		primary = errSoftwareRustConsumer
	}
	if errors.Is(err, errSoftwareRustCleanup) { return softwareRustWithCleanupFailure(primary) }
	return primary
}

// The normal factory remains closed until the native adapter's current-object
// purpose qualification and ACL policy have their independent acceptance.
// Public metadata alone is never permission to open the installed body.
func normalSoftwareRustOriginProvider(_ unslopToolchainPurpose) rustToolchainOriginProvider {
	return nil
}

type softwareRustNativeAccess struct{}

func (softwareRustNativeAccess) Origin(ctx context.Context) (rustToolchainOrigin, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	transport := &http.Transport{Proxy: nil, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	manifest, err := softwareRustPublicBytes(ctx, client, softwareRustManifestURL, softwareRustManifestLimit)
	if err != nil {
		return rustToolchainOrigin{}, err
	}
	defer clear(manifest)
	checksum, err := softwareRustPublicBytes(ctx, client, softwareRustManifestURL+".sha256", 256)
	if err != nil {
		return rustToolchainOrigin{}, err
	}
	defer clear(checksum)
	origin, err := softwareRustVerifiedManifest(manifest, checksum)
	if err != nil {
		return rustToolchainOrigin{}, err
	}
	for i, component := range origin.Components {
		archive, err := softwareRustPublicBytes(ctx, client, component.URL, softwareRustArchiveLimit)
		if err != nil {
			return rustToolchainOrigin{}, err
		}
		executable, err := softwareRustArchiveExecutable(ctx, component.Name, archive, component.ArchiveSHA)
		clear(archive)
		if err != nil {
			clear(executable)
			return rustToolchainOrigin{}, err
		}
		if i < 2 {
			origin.Components[i].ExecutableSHA = digest(executable)
		}
		clear(executable)
	}
	return origin, nil
}

func softwareRustPublicBytes(ctx context.Context, client *http.Client, address string, limit int64) ([]byte, error) {
	allowed := address == softwareRustManifestURL || address == softwareRustManifestURL+".sha256" || address == rustToolchainCargoURL || address == rustToolchainRustcURL || address == rustToolchainStdURL
	if !allowed || ctx.Err() != nil {
		return nil, softwareRustFixedError(ctx, nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, errSoftwareRustUnavailable
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, softwareRustFixedError(ctx, err)
	}
	if response.StatusCode != http.StatusOK || response.ContentLength > limit {
		_ = response.Body.Close()
		return nil, errSoftwareRustUnavailable
	}
	// Only public release bytes are read here. Raw error bodies, credentials,
	// local configuration and local module/toolchain caches are never inputs.
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	closeErr := response.Body.Close()
	if err != nil || closeErr != nil || len(raw) == 0 || int64(len(raw)) > limit || ctx.Err() != nil {
		clear(raw)
		return nil, softwareRustFixedError(ctx, err)
	}
	return raw, nil
}

func softwareRustVerifiedManifest(raw, checksum []byte) (rustToolchainOrigin, error) {
	fields := strings.Fields(string(checksum))
	if len(raw) == 0 || len(raw) > softwareRustManifestLimit || len(checksum) > 256 || len(fields) != 2 || fields[0] != rustToolchainManifestSHA || fields[1] != "channel-rust-1.95.0.toml" || digest(raw) != rustToolchainManifestSHA {
		return rustToolchainOrigin{}, errSoftwareRustMismatch
	}
	return softwareRustManifestProjection(raw)
}

// A fixed-release projection, not a general TOML reader or an authority
// decoder. The production caller checks the independent complete-file digest
// first. Structural tests deliberately exercise this parser without that grant.
func softwareRustManifestProjection(raw []byte) (rustToolchainOrigin, error) {
	if len(raw) == 0 || len(raw) > softwareRustManifestLimit {
		return rustToolchainOrigin{}, errSoftwareRustMismatch
	}
	origin := rustToolchainOrigin{Version: rustToolchainVersion, Host: rustToolchainHost, ManifestSHA: rustToolchainManifestSHA, Components: [3]rustToolchainComponent{
		{Name: "cargo", URL: rustToolchainCargoURL, ArchiveSHA: rustToolchainCargoArchiveSHA},
		{Name: "rustc", URL: rustToolchainRustcURL, ArchiveSHA: rustToolchainRustcArchiveSHA},
		{Name: "rust-std", URL: rustToolchainStdURL, ArchiveSHA: rustToolchainStdArchiveSHA},
	}}
	selected := map[string]int{}
	for i, component := range origin.Components {
		selected["pkg."+component.Name+".target."+rustToolchainHost] = i
	}
	sections := map[string]bool{}
	values := map[string]map[string]string{"": {}}
	section := ""
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 16384)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return rustToolchainOrigin{}, errSoftwareRustMismatch
			}
			section = line[1:len(line)-1]
			if !strings.HasPrefix(section, "[") {
				if sections[section] {
					return rustToolchainOrigin{}, errSoftwareRustMismatch
				}
				sections[section] = true
			}
			continue
		}
		_, component := selected[section]
		if section != "" && section != "pkg.rust" && !component {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return rustToolchainOrigin{}, errSoftwareRustMismatch
		}
		key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if values[section] == nil {
			values[section] = map[string]string{}
		}
		if _, duplicate := values[section][key]; duplicate {
			return rustToolchainOrigin{}, errSoftwareRustMismatch
		}
		values[section][key] = value
	}
	if scanner.Err() != nil || values[""]["manifest-version"] != `"2"` || values[""]["date"] != `"2026-04-16"` || !strings.HasPrefix(values["pkg.rust"]["version"], `"1.95.0 (`) || !strings.HasSuffix(values["pkg.rust"]["version"], `)"`) {
		return rustToolchainOrigin{}, errSoftwareRustMismatch
	}
	for section, i := range selected {
		v := values[section]
		if v["available"] != "true" || v["url"] != strconv.Quote(origin.Components[i].URL) || v["hash"] != strconv.Quote(origin.Components[i].ArchiveSHA) {
			return rustToolchainOrigin{}, errSoftwareRustMismatch
		}
	}
	return origin, nil
}

func softwareRustArchiveExecutable(ctx context.Context, component string, raw []byte, expectedSHA string) (result []byte, failure error) {
	if ctx.Err() != nil || len(raw) == 0 || len(raw) > softwareRustArchiveLimit || digest(raw) != expectedSHA || component != "cargo" && component != "rustc" && component != "rust-std" {
		return nil, errSoftwareRustMismatch
	}
	zip, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, errSoftwareRustMismatch
	}
	defer zip.Close()
	limit := &io.LimitedReader{R: zip, N: softwareRustExpandedLimit + 1}
	archive := tar.NewReader(limit)
	root := component+"-"+rustToolchainVersion+"-"+rustToolchainHost
	wanted := root+"/"+component+"/bin/"+component+".exe"
	seen := map[string]bool{}
	var total int64
	defer func() { if failure != nil { clear(result); result = nil } }()
	for count := 0; ; count++ {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		header, err := archive.Next()
		if err == io.EOF {
			break
		}
		if err != nil || count >= softwareRustEntryLimit || limit.N <= 0 {
			return result, errSoftwareRustMismatch
		}
		name := strings.TrimSuffix(header.Name, "/")
		if !softwareRustArchivePath(name) || name != root && !strings.HasPrefix(name, root+"/") || seen[strings.ToLower(name)] || header.Size < 0 || header.Size > 512<<20 || total > softwareRustExpandedLimit-header.Size || header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir {
			return result, errSoftwareRustMismatch
		}
		seen[strings.ToLower(name)] = true
		total += header.Size
		if header.Typeflag == tar.TypeDir {
			if header.Size != 0 { return result, errSoftwareRustMismatch }
			continue
		}
		if name == wanted && component != "rust-std" {
			if header.Size <= 0 || header.Size > 64<<20 {
				return result, errSoftwareRustMismatch
			}
			result = make([]byte, int(header.Size))
			if _, err := io.ReadFull(archive, result); err != nil {
				return result, errSoftwareRustMismatch
			}
		} else if _, err := io.Copy(io.Discard, archive); err != nil {
			return result, errSoftwareRustMismatch
		}
	}
	// Read the same authenticated gzip to completion, including its checksum.
	if _, err := io.Copy(io.Discard, limit); err != nil || limit.N <= 0 || ctx.Err() != nil || component != "rust-std" && len(result) == 0 {
		return result, errSoftwareRustMismatch
	}
	return result, nil
}

func softwareRustArchivePath(name string) bool {
	if name == "" || len(name) > 4096 || strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		for _, r := range part {
			if r < 32 || r > 126 || strings.ContainsRune(`<>|?*"`, r) { return false }
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' { return false }
	}
	return true
}

type softwareRustNativeNode struct {
	f *os.File
	path, resolved, aclSHA string
	id softwareIdentity
	reparse bool
}

type softwareRustNativeSession struct {
	nodes []softwareRustNativeNode
	held rustToolchainHeld
}

func (softwareRustNativeAccess) Open(ctx context.Context, origin rustToolchainOrigin) (softwareRustHeldSession, error) {
	s := &softwareRustNativeSession{held: rustToolchainHeld{Origin: origin}}
	return softwareRustOpenWithSession(ctx, origin, s)
}

// Native access and owned synthetic controls call the same opening controller.
// No caller supplies this interface through a runtime argument or a receipt.
type softwareRustOpeningSession interface {
	softwareRustHeldSession
	state() *rustToolchainHeld
	openNode(string, string, bool, bool) (softwareRustNativeNode, error)
	fixedJunction(softwareRustNativeNode) bool
	readNode(context.Context, softwareRustNativeNode, int) ([]byte, error)
	recheck(context.Context) error
}

func softwareRustOpenWithSession(ctx context.Context, origin rustToolchainOrigin, s softwareRustOpeningSession) (result softwareRustHeldSession, failure error) {
	held := s.state()
	held.Origin = origin
	good := false
	defer func() {
		if !good && s.Close() != nil { failure = softwareRustWithCleanupFailure(softwareRustFixedError(ctx, failure)) }
	}()
	if ctx.Err() != nil { return nil, ctx.Err() }
	open := func(path, resolved string, directory, reparse bool) (softwareRustNativeNode, error) {
		if err := ctx.Err(); err != nil { return softwareRustNativeNode{}, err }
		node, err := s.openNode(path, resolved, directory, reparse)
		if ctx.Err() != nil { return softwareRustNativeNode{}, ctx.Err() }
		return node, err
	}
	// Plain ancestors are pinned on both sides. The only reparse exception is
	// the one fixed install junction, checked by its native reparse payload.
	for _, root := range []string{filepath.Dir(rustToolchainInstallRoot), rustToolchainPhysicalRoot} {
		var parents []string
		for p := root; ; p = filepath.Dir(p) { parents = append(parents, p); if filepath.Dir(p) == p { break } }
		for i := len(parents)-1; i >= 0; i-- {
			if _, err := open(parents[i], parents[i], true, false); err != nil { return nil, err }
		}
	}
	alias, err := open(rustToolchainInstallRoot, rustToolchainInstallRoot, true, true)
	if err != nil { return nil, err }
	if !s.fixedJunction(alias) { return nil, errSoftwareRustMismatch }
	if ctx.Err() != nil { return nil, ctx.Err() }
	resolved, err := open(rustToolchainInstallRoot, rustToolchainPhysicalRoot, true, false)
	if err != nil { return nil, err }
	target, err := open(rustToolchainPhysicalRoot, rustToolchainPhysicalRoot, true, false)
	if err != nil { return nil, err }
	if resolved.id != target.id { return nil, errSoftwareRustMismatch }
	held.Alias = rustToolchainAlias{InstallRoot: rustToolchainInstallRoot, PhysicalRoot: rustToolchainPhysicalRoot, AliasIdentity: alias.id, ResolvedIdentity: resolved.id, TargetIdentity: target.id, AliasACLSHA: alias.aclSHA, TargetACLSHA: target.aclSHA}
	for i, name := range []string{"cargo", "rustc"} {
		if ctx.Err() != nil { return nil, ctx.Err() }
		suffix := "/toolchains/"+rustToolchainName
		for _, tail := range []string{"/toolchains", suffix, suffix+"/bin"} {
			if _, err := open(rustToolchainInstallRoot+tail, rustToolchainPhysicalRoot+tail, true, false); err != nil { return nil, err }
		}
		install, physical := rustToolchainInstallRoot+suffix+"/bin/"+name+".exe", rustToolchainPhysicalRoot+suffix+"/bin/"+name+".exe"
		node, err := open(install, physical, false, false)
		if err != nil { return nil, err }
		if ctx.Err() != nil { return nil, ctx.Err() }
		raw, err := s.readNode(ctx, node, i)
		if err != nil { return nil, err }
		file := rustToolchainHeldFile{InstallPath: install, PhysicalPath: physical, Identity: node.id, ViaInstallIdentity: node.id, ACLSHA: node.aclSHA, Raw: raw}
		if i == 0 { held.Cargo = file } else { held.Rustc = file }
		if ctx.Err() != nil { return nil, ctx.Err() }
		if digest(raw) != origin.Components[i].ExecutableSHA { return nil, errSoftwareRustMismatch }
	}
	held.Recheck = s.recheck
	if ctx.Err() != nil { return nil, ctx.Err() }
	if !rustToolchainInputsMatch(*held) { return nil, errSoftwareRustMismatch }
	if err := s.recheck(ctx); err != nil { return nil, err }
	if ctx.Err() != nil { return nil, ctx.Err() }
	good = true
	return s, nil
}

func (s *softwareRustNativeSession) state() *rustToolchainHeld { return &s.held }
func (s *softwareRustNativeSession) fixedJunction(node softwareRustNativeNode) bool { return softwareRustFixedJunction(node.f) }
func (s *softwareRustNativeSession) readNode(ctx context.Context, node softwareRustNativeNode, index int) ([]byte, error) {
	if ctx.Err() != nil { return nil, ctx.Err() }
	stat, err := node.f.Stat()
	if err != nil || stat.Size() <= 0 || stat.Size() > 64<<20 { return nil, errSoftwareRustMismatch }
	if ctx.Err() != nil { return nil, ctx.Err() }
	raw := make([]byte, int(stat.Size()))
	if index == 0 { s.held.Cargo.Raw = raw } else { s.held.Rustc.Raw = raw }
	// A synchronous file read is not forcibly interrupted. Check the context
	// before and after each bounded read and never begin another after cancel.
	for offset := 0; offset < len(raw); {
		if ctx.Err() != nil { return nil, ctx.Err() }
		end := offset + (64 << 10)
		if end > len(raw) { end = len(raw) }
		n, readErr := node.f.Read(raw[offset:end])
		offset += n
		if ctx.Err() != nil { return nil, ctx.Err() }
		if readErr != nil && !(readErr == io.EOF && offset == len(raw)) || n == 0 { return nil, errSoftwareRustMismatch }
	}
	return raw, nil
}

func (s *softwareRustNativeSession) openNode(path, resolved string, directory, reparse bool) (softwareRustNativeNode, error) {
	for _, old := range s.nodes { if old.path == path && old.reparse == reparse { return old, nil } }
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil { return softwareRustNativeNode{}, errSoftwareRustUnavailable }
	access, flags, share := uint32(syscall.GENERIC_READ), uint32(0x00200000), uint32(syscall.FILE_SHARE_READ)
	if directory { access = 0x20080; flags |= 0x02000000; if !reparse { share |= syscall.FILE_SHARE_WRITE } }
	if directory && !reparse && path != rustToolchainInstallRoot { flags |= 0x00200000 }
	if directory && !reparse && path == rustToolchainInstallRoot { flags &^= 0x00200000 }
	h, err := syscall.CreateFile(ptr, access, share, nil, syscall.OPEN_EXISTING, flags, 0)
	if err != nil { return softwareRustNativeNode{}, errSoftwareRustUnavailable }
	f := os.NewFile(uintptr(h), path)
	node := softwareRustNativeNode{f: f, path: path, resolved: resolved, reparse: reparse}
	info, err := softwareRustNodeInfo(f, directory, reparse)
	actual, ok := handlePath(h)
	acl, aclErr := updateFileACL(f)
	if err != nil || !ok || !strings.EqualFold(filepath.Clean(actual), filepath.Clean(resolved)) || aclErr != nil { _ = f.Close(); return softwareRustNativeNode{}, errSoftwareRustMismatch }
	node.id, node.aclSHA = info, digest([]byte(acl))
	s.nodes = append(s.nodes, node)
	return node, nil
}

func softwareRustNodeInfo(f *os.File, directory, reparse bool) (softwareIdentity, error) {
	var i syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &i) != nil || (i.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0) != directory || (i.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0) != reparse || !directory && i.NumberOfLinks != 1 { return softwareIdentity{}, errSoftwareRustMismatch }
	return softwareIdentity{fmt.Sprint(i.VolumeSerialNumber), fmt.Sprint(uint64(i.FileIndexHigh)<<32|uint64(i.FileIndexLow)), fmt.Sprint(uint64(i.CreationTime.HighDateTime)<<32|uint64(i.CreationTime.LowDateTime))}, nil
}

func softwareRustFixedJunction(f *os.File) bool {
	var raw [16384]byte
	var count uint32
	if syscall.DeviceIoControl(syscall.Handle(f.Fd()), 0x900a8, nil, 0, &raw[0], uint32(len(raw)), &count, nil) != nil || count < 16 || binary.LittleEndian.Uint32(raw[:4]) != 0xa0000003 { return false }
	offset, length := int(binary.LittleEndian.Uint16(raw[8:10])), int(binary.LittleEndian.Uint16(raw[10:12]))
	if offset%2 != 0 || length%2 != 0 || 16+offset+length > int(count) { return false }
	words := make([]uint16, length/2)
	for i := range words { words[i] = binary.LittleEndian.Uint16(raw[16+offset+2*i:]) }
	substitute := string(utf16.Decode(words))
	expected := `\??\`+strings.ReplaceAll(rustToolchainPhysicalRoot, "/", `\`)
	return strings.EqualFold(substitute, expected)
}

func (s *softwareRustNativeSession) Held() rustToolchainHeld { return s.held }

func (s *softwareRustNativeSession) recheck(ctx context.Context) error {
	if ctx.Err() != nil { return ctx.Err() }
	for _, node := range s.nodes {
		if ctx.Err() != nil { return ctx.Err() }
		info, err := node.f.Stat()
		if err != nil { return errSoftwareRustUnavailable }
		id, err := softwareRustNodeInfo(node.f, info.IsDir(), node.reparse)
		path, ok := handlePath(syscall.Handle(node.f.Fd()))
		acl, aclErr := updateFileACL(node.f)
		if ctx.Err() != nil { return ctx.Err() }
		if err != nil || id != node.id || !ok || !strings.EqualFold(filepath.Clean(path), filepath.Clean(node.resolved)) || aclErr != nil || digest([]byte(acl)) != node.aclSHA || node.reparse && !softwareRustFixedJunction(node.f) { return errSoftwareRustMismatch }
	}
	if !rustToolchainInputsMatch(s.held) { return errSoftwareRustMismatch }
	return nil
}

func (s *softwareRustNativeSession) Close() error {
	clear(s.held.Cargo.Raw)
	clear(s.held.Rustc.Raw)
	s.held.Cargo.Raw, s.held.Rustc.Raw = nil, nil
	var failed bool
	for i := len(s.nodes)-1; i >= 0; i-- { if s.nodes[i].f.Close() != nil { failed = true } }
	s.nodes = nil
	if failed { return errSoftwareRustCleanup }
	return nil
}
