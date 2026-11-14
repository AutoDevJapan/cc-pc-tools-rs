//go:build windows

package main

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// The native controller supplies one supervised compiler run, including its
// actual pre-resume process identity. It never supplies a made-up tool call ID.
type unslopNativeBuildRun func(context.Context) askChildResult

type unslopCargoStream struct {
	digest                    hash.Hash
	total                     int
	line                      []byte
	artifact                  []byte
	finished, invalid, closed bool
}

func (s *unslopCargoStream) Write(p []byte) (int, error) {
	count := len(p)
	if s.closed {
		return count, nil
	}
	if s.digest == nil {
		s.digest = sha256.New()
	}
	_, _ = s.digest.Write(p)
	s.total += count
	if s.total > 8<<20 {
		s.invalid = true
		clear(s.line)
		s.line = nil
		return count, nil
	}
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		take := len(p)
		if i >= 0 {
			take = i
		}
		if len(s.line)+take > 1<<20 {
			s.invalid = true
		}
		if !s.invalid {
			s.line = append(s.line, p[:take]...)
		}
		if i < 0 {
			break
		}
		if !s.invalid {
			s.consumeLine()
		}
		clear(s.line)
		s.line = s.line[:0]
		p = p[i+1:]
	}
	return count, nil
}
func (s *unslopCargoStream) consumeLine() {
	if s.finished {
		s.invalid = true
		return
	}
	var item struct {
		Reason     string  `json:"reason"`
		Success    *bool   `json:"success"`
		Fresh      *bool   `json:"fresh"`
		Executable *string `json:"executable"`
		Target     struct {
			Name string   `json:"name"`
			Kind []string `json:"kind"`
		} `json:"target"`
	}
	if json.Unmarshal(s.line, &item) != nil || item.Reason == "" {
		s.invalid = true
		return
	}
	if item.Reason == "build-finished" {
		if item.Success == nil || !*item.Success || len(s.artifact) == 0 {
			s.invalid = true
			return
		}
		s.finished = true
		return
	}
	if item.Reason == "compiler-artifact" && item.Target.Name == "godd-unslop" {
		bin := false
		for _, kind := range item.Target.Kind {
			bin = bin || kind == "bin"
		}
		if bin {
			if len(s.artifact) != 0 || item.Executable == nil || !sameCleanPath(*item.Executable, unslopCaptureOutput) || item.Fresh == nil || *item.Fresh {
				s.invalid = true
				return
			}
			// Store a projection, not compiler diagnostics or environment text.
			s.artifact, _ = json.Marshal(map[string]any{"reason": "compiler-artifact", "target": map[string]any{"name": "godd-unslop", "kind": []string{"bin"}}, "executable": unslopCaptureOutput, "fresh": false})
		}
	}
}
func (s *unslopCargoStream) finish() ([]byte, string, bool) {
	if s.closed {
		return nil, "", false
	}
	s.closed = true
	defer func() { clear(s.line); s.line = nil; clear(s.artifact); s.artifact = nil }()
	if s.invalid || !s.finished || len(s.line) != 0 || s.digest == nil {
		return nil, "", false
	}
	raw := append([]byte(nil), s.artifact...)
	raw = append(raw, []byte("\n{\"reason\":\"build-finished\",\"success\":true}\n")...)
	return raw, hex.EncodeToString(s.digest.Sum(nil)), true
}

const unslopNativeExport = unslopCaptureRoot + "/godd-unslop.captured.exe"
const unslopNativeReceipt = unslopCaptureRoot + "/producer.receipt.json"
const unslopCargoExe = "C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/cargo.exe"
const unslopRustcExe = "C:/Users/makhmeto/.rustup/toolchains/1.95.0-x86_64-pc-windows-msvc/bin/rustc.exe"

type unslopNativeSession struct {
	source, configuration                        *softwareBuildSnapshot
	tools                                        []updateObject
	manifest                                     updateObject
	parents                                      []syscall.Handle
	namespace                                    softwareIdentity
	before                                       unslopCapturedInputs
	manifestSettings                             map[string]string
	build                                        unslopBuildInvocation
	sealed                                       bool
	building, artifactHeld, acknowledged, closed bool
	producerConfirmed                            bool
	run                                          unslopNativeBuildRun
}

func unslopQualifiedSource(ctx context.Context) bool {
	raw, ok := askGitCommand(ctx, installedGuardPath, "--guard-path", unslopSourceRoot)
	defer clear(raw)
	var result struct {
		Allowed    bool `json:"access_allowed"`
		Client     bool `json:"client_resource"`
		Unverified bool `json:"classification_unverified"`
	}
	if !ok || json.Unmarshal(raw, &result) != nil || !result.Allowed || result.Client || result.Unverified {
		return false
	}
	raw, ok = askGitCommand(ctx, "C:/Program Files/Git/cmd/git.exe", "-C", unslopSourceRoot, "--no-replace-objects", "rev-parse", unslopCaptureSource+"^{tree}")
	if !ok || strings.TrimSpace(string(raw)) != unslopCaptureTree {
		clear(raw)
		return false
	}
	clear(raw)
	raw, ok = askGitCommand(ctx, "C:/Program Files/Git/cmd/git.exe", "-C", unslopSourceRoot, "--no-replace-objects", "merge-base", "--is-ancestor", unslopCaptureSource, "refs/remotes/origin/main")
	clear(raw)
	return ok && ctx.Err() == nil
}

func unslopReadSource(ctx context.Context) (map[string][]byte, error) {
	if !unslopQualifiedSource(ctx) {
		return nil, errors.New("unslop_source_unqualified")
	}
	result := map[string][]byte{}
	valid := false
	defer func() {
		if !valid {
			for _, b := range result {
				clear(b)
			}
		}
	}()
	for path, blob := range unslopCaptureSourceBlobs() {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		raw, ok := askGitCommand(ctx, "C:/Program Files/Git/cmd/git.exe", "-C", unslopSourceRoot, "--no-replace-objects", "cat-file", "blob", blob)
		if !ok || len(raw) > 2<<20 {
			clear(raw)
			return nil, errUpdateObject
		}
		gitHash := sha1.New()
		_, _ = fmt.Fprintf(gitHash, "blob %d\x00", len(raw))
		_, _ = gitHash.Write(raw)
		if hex.EncodeToString(gitHash.Sum(nil)) != blob {
			clear(raw)
			return nil, errUpdateObject
		}
		result[path] = raw
	}
	valid = true
	return result, nil
}

func beginUnslopNative(ctx context.Context, run unslopNativeBuildRun) (result *unslopNativeSession, err error) {
	if ctx.Err() != nil || run == nil {
		return nil, errUnslopOrigin
	}
	inputs, e := unslopReadSource(ctx)
	if e != nil {
		return nil, e
	}
	defer func() {
		for _, raw := range inputs {
			clear(raw)
		}
	}()
	s := &unslopNativeSession{run: run}
	defer func() {
		if err != nil {
			err = errors.Join(err, s.Close())
		}
	}()
	// This is a newly admitted producer namespace. A collision is preserved;
	// the old release and old packet are never read to qualify this operation.
	for _, p := range []string{unslopSourceRoot, filepath.Join(unslopSourceRoot, "target")} {
		h, ok := openPlain(p, true)
		if !ok {
			return nil, errUpdateObject
		}
		s.parents = append(s.parents, h)
	}
	if os.Mkdir(unslopCaptureRoot, 0700) != nil {
		return nil, errors.New("unslop_capture_namespace_collision_or_unavailable")
	}
	h, ok := openPlain(unslopCaptureRoot, true)
	if !ok {
		return nil, errUpdateObject
	}
	s.parents = append(s.parents, h)
	var info syscall.ByHandleFileInformation
	if syscall.GetFileInformationByHandle(h, &info) != nil {
		return nil, errUpdateObject
	}
	s.namespace = softwareIdentity{fmtUint(uint64(info.VolumeSerialNumber)), fmtUint(uint64(info.FileIndexHigh)<<32 | uint64(info.FileIndexLow)), fmtUint(uint64(info.CreationTime.HighDateTime)<<32 | uint64(info.CreationTime.LowDateTime))}
	for _, p := range []string{unslopCaptureRoot + "/source", unslopCaptureRoot + "/cargo-home", unslopCaptureRoot + "/cargo"} {
		if os.Mkdir(p, 0700) != nil {
			return nil, errUpdateObject
		}
	}
	s.source = &softwareBuildSnapshot{Root: unslopCaptureRoot + "/source", paths: map[string]bool{}}
	if snapshotDirectories(s.source.Root, s.source.Root, s.source) != nil {
		return nil, errUpdateObject
	}
	keys := make([]string, 0, len(inputs))
	for path := range inputs {
		keys = append(keys, path)
	}
	sort.Strings(keys)
	for _, path := range keys {
		p := filepath.Join(s.source.Root, filepath.FromSlash(path))
		if snapshotDirectories(filepath.Dir(p), s.source.Root, s.source) != nil || s.source.create(p, inputs[path]) != nil {
			return nil, errUpdateObject
		}
	}
	s.source.LockSHA = digest(inputs["Cargo.lock"])
	if e = s.source.admitCargoVendor(ctx, inputs["Cargo.lock"], readLockedCargoArchive); e != nil {
		return nil, e
	}
	s.configuration = &softwareBuildSnapshot{Root: unslopCaptureRoot + "/cargo-home", paths: map[string]bool{}}
	if snapshotDirectories(s.configuration.Root, s.configuration.Root, s.configuration) != nil || s.configuration.create(s.configuration.Root+"/config.toml", []byte(mp4CargoConfiguration)) != nil || s.configuration.bindCargoDirectory() != nil {
		return nil, errUpdateObject
	}
	// Only normal, fixed toolchain software is opened, after the guard checks
	// each exact leaf. No Cargo credentials or existing hidden config is read.
	for _, p := range []string{unslopCargoExe, unslopRustcExe, "D:/CC/Tools/codex-jobs/cargo_gate.exe", "D:/CC/Tools/sccache/sccache.exe"} {
		raw, ok := askGitCommand(ctx, installedGuardPath, "--guard-path", p)
		var q struct {
			Allowed bool `json:"access_allowed"`
			Client  bool `json:"client_resource"`
		}
		parsed := json.Unmarshal(raw, &q)
		clear(raw)
		if !ok || parsed != nil || !q.Allowed || q.Client {
			return nil, errors.New("unslop_toolchain_unqualified")
		}
		o, e := openUpdateObject(p)
		if e != nil {
			return nil, e
		}
		s.tools = append(s.tools, o)
	}
	if s.source.sealDirectories() != nil || s.configuration.sealDirectories() != nil {
		return nil, errUpdateObject
	}
	s.sealed = true
	s.manifestSettings = mp4CompilerEnvironment(s.configuration.Root)
	s.before, e = s.snapshot(ctx)
	if e != nil {
		return nil, e
	}
	if _, ok := unslopCapturedManifest(s.before); !ok {
		return nil, errors.New("unslop_native_inputs_incomplete")
	}
	return s, nil
}

func (s *unslopNativeSession) snapshot(ctx context.Context) (unslopCapturedInputs, error) {
	r := unslopCapturedInputs{Source: unslopCaptureSource, Merge: unslopCaptureSource, Tree: unslopCaptureTree, Namespace: unslopCaptureRoot, NamespaceIdentity: s.namespace, LockSHA: s.source.LockSHA}
	if ctx.Err() != nil || !s.sealed || !s.source.exact() || !s.configuration.exact() || !s.configuration.cargoDirectoryUnchanged() {
		return r, errUpdateObject
	}
	for _, snapshot := range []*softwareBuildSnapshot{s.source, s.configuration} {
		for _, f := range snapshot.files {
			id, e := fileSoftwareIdentity(f)
			if e != nil {
				return r, e
			}
			physical, e := os.Stat(f.Name())
			held, e2 := f.Stat()
			if e != nil || e2 != nil || !os.SameFile(physical, held) {
				return r, errUpdateObject
			}
			if _, e = f.Seek(0, io.SeekStart); e != nil {
				return r, e
			}
			raw, e := io.ReadAll(io.LimitReader(f, cargoVendorFileLimit+1))
			if e != nil || len(raw) > cargoVendorFileLimit {
				clear(raw)
				return r, errUpdateObject
			}
			rel, e := filepath.Rel(snapshot.Root, f.Name())
			if e != nil {
				clear(raw)
				return r, e
			}
			path := filepath.ToSlash(rel)
			entry := unslopCapturedInput{Path: path, SHA: digest(raw), Identity: id}
			clear(raw)
			if snapshot == s.configuration {
				entry.Path = "cargo-home/" + path
				r.Configuration = append(r.Configuration, entry)
			} else if strings.HasPrefix(path, "vendor/") {
				r.Dependencies = append(r.Dependencies, entry)
			} else {
				entry.Blob = unslopCaptureSourceBlobs()[path]
				r.Sources = append(r.Sources, entry)
			}
		}
	}
	settings := s.manifestSettings
	if settings == nil {
		settings = mp4CompilerEnvironment(s.configuration.Root)
	}
	configCtx := context.WithValue(ctx, askChildEnvironment{}, settings)
	r.ConfigurationSHA = mp4ConfigurationDigest(configCtx)
	var tools []string
	for _, o := range s.tools {
		tools = append(tools, digest(o.Raw()))
	}
	encoded, _ := json.Marshal(tools)
	r.ToolchainSHA = digest(encoded)
	return r, nil
}
func (s *unslopNativeSession) Before() unslopCapturedInputs { return s.before }
func (s *unslopNativeSession) After(ctx context.Context) (unslopCapturedInputs, error) {
	return s.snapshot(ctx)
}
func (s *unslopNativeSession) Build(ctx context.Context) (unslopBuildInvocation, error) {
	if ctx.Err() != nil || s.building || s.closed || !s.sealed {
		return unslopBuildInvocation{}, errUnslopOrigin
	}
	s.building = true
	stream := &unslopCargoStream{}
	child := s.run(ctx)
	defer clear(child.Raw)
	if child.Capture == nil {
		return unslopBuildInvocation{}, errors.New("unslop_native_capture_missing")
	}
	raw := child.Capture.Stdout
	transferred := false
	defer func() {
		if !transferred {
			clear(raw)
		}
	}()
	if ctx.Err() != nil || !child.Capture.Complete || child.Capture.Overflow || child.Capture.NativeProcess == nil || len(raw) > 8<<20 {
		return unslopBuildInvocation{}, errors.New("unslop_native_capture_incomplete")
	}
	process := &unslopProcessIdentity{PID: child.Capture.NativeProcess.PID, Created: child.Capture.NativeProcess.Creation}
	_, _ = stream.Write(raw)
	projection, fullSHA, ok := stream.finish()
	if !ok {
		return unslopBuildInvocation{}, errors.New("unslop_cargo_stream_unqualified")
	}
	command, _ := json.Marshal(unslopCaptureCommand())
	s.build = unslopBuildInvocation{CommandSHA: digest(command), CargoJSONSHA: digest(projection), CargoStreamSHA: fullSHA, CargoStream: raw, NativeProcess: process, Started: child.Started, Assigned: child.Assigned, WaitVerified: child.WaitVerified, Reaped: child.Reaped, TimedOut: child.TimedOut, Exit: child.Exit, CargoJSON: projection}
	transferred = true
	return s.build, nil
}
func (s *unslopNativeSession) WithArtifact(use func([]byte, softwareIdentity) bool) bool {
	if !s.build.Reaped || s.build.Exit != 0 || s.closed {
		return false
	}
	names, e := softwareLinks(unslopCaptureOutput)
	if e != nil {
		return false
	}
	alias, ok := unslopCargoAlias(names)
	if !ok {
		return false
	}
	return withPinnedCargoOutput(unslopCaptureOutput, alias, softwareIdentity{}, func(raw []byte, id softwareIdentity) bool {
		s.artifactHeld = true
		defer func() { s.artifactHeld = false }()
		return use(raw, id)
	})
}

func unslopCargoAlias(names []string) (string, bool) {
	if len(names) != 2 {
		return "", false
	}
	output, alias := false, ""
	for _, name := range names {
		if sameCleanPath(name, unslopCaptureOutput) {
			if output {
				return "", false
			}
			output = true
			continue
		}
		if !sameCleanPath(filepath.Dir(name), unslopCaptureRoot+"/cargo/release/deps") || alias != "" {
			return "", false
		}
		base := filepath.Base(name)
		if base != "godd_unslop.exe" {
			if !strings.HasPrefix(base, "godd_unslop-") || !strings.HasSuffix(base, ".exe") || !unslopHex(strings.TrimSuffix(strings.TrimPrefix(base, "godd_unslop-"), ".exe"), 16) {
				return "", false
			}
		}
		alias = name
	}
	return alias, output && alias != ""
}
func (s *unslopNativeSession) Acknowledge(ctx context.Context) error {
	return errors.New("live_producer_ack_not_connected")
}
func (s *unslopNativeSession) AwaitCompletion(context.Context) (unslopProducerCompletion, error) {
	return unslopProducerCompletion{}, errors.New("live_producer_ack_not_connected")
}
func (s *unslopNativeSession) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	clear(s.build.CargoStream)
	s.build.CargoStream = nil
	clear(s.build.CargoJSON)
	s.build.CargoJSON = nil
	var errs []error
	if s.source != nil {
		errs = append(errs, s.source.Close())
	}
	if s.configuration != nil {
		errs = append(errs, s.configuration.Close())
	}
	if s.manifest != nil {
		errs = append(errs, s.manifest.Close())
		s.manifest = nil
	}
	for _, o := range s.tools {
		errs = append(errs, o.Close())
	}
	s.tools = nil
	for i := len(s.parents) - 1; i >= 0; i-- {
		errs = append(errs, syscall.CloseHandle(s.parents[i]))
	}
	s.parents = nil
	return errors.Join(errs...)
}

type unslopNativePending struct {
	receipt        unslopCaptureReceipt
	exportIdentity softwareIdentity
	session        *unslopNativeSession
	committed      bool
	export         updateObject
	sealed         bool
	receiptPath    string
}

type unslopNativeReceiptRecord struct {
	Capture        unslopCaptureReceipt
	ExportIdentity softwareIdentity
}

func (s *unslopNativeSession) stage(ctx context.Context, r unslopCaptureReceipt, raw []byte) (unslopPendingCapture, error) {
	if ctx.Err() != nil || !s.artifactHeld || r.ProducerAcknowledged || r.ProducerReaped || r.Source != unslopCaptureSource || r.Namespace != unslopCaptureRoot || r.Domain != unslopCaptureDomain || !r.CompleteCompiledInputs || digest(raw) != r.CandidateSHA {
		return nil, errUnslopOrigin
	}
	p := &unslopNativePending{receipt: r, session: s, receiptPath: unslopNativeReceipt}
	id, e := createAskArtifactCopy(unslopNativeExport, raw)
	p.exportIdentity = id
	if e == nil {
		p.export, e = openUpdateObject(unslopNativeExport)
		if e == nil && (p.export.ID() != id || digest(p.export.Raw()) != r.CandidateSHA) {
			e = errUnslopOrigin
		}
	}
	return p, e
}
func (p *unslopNativePending) Seal(r unslopCaptureReceipt) error {
	before, after := p.receipt, r
	before.ProducerAcknowledged = false
	before.ProducerReaped = false
	after.ProducerAcknowledged = false
	after.ProducerReaped = false
	left, _ := json.Marshal(before)
	right, _ := json.Marshal(after)
	if !r.ProducerAcknowledged || !r.ProducerReaped || p.session == nil || !p.session.acknowledged || !p.session.producerConfirmed || !bytes.Equal(left, right) {
		return errUnslopOrigin
	}
	p.receipt = r
	p.sealed = true
	return nil
}
func (p *unslopNativePending) Abort() error    { return nil } // preserve partial export; never overwrite or silently delete it
func (p *unslopNativePending) Published() bool { return p.committed }
func (p *unslopNativePending) Close() error {
	if p.export == nil {
		return nil
	}
	e := p.export.Close()
	p.export = nil
	return e
}
func (p *unslopNativePending) Commit(ctx context.Context) error {
	if ctx.Err() != nil || p.committed || !p.sealed || p.session == nil || !p.session.closed || !p.session.acknowledged || !unslopCompleteIdentity(p.exportIdentity) {
		return errUnslopOrigin
	}
	export := p.export
	if export == nil {
		return errUnslopOrigin
	}
	if export.ID() != p.exportIdentity || digest(export.Raw()) != p.receipt.CandidateSHA || len(export.Raw()) != p.receipt.Bytes {
		return errUpdateObject
	}
	value := unslopNativeReceiptRecord{p.receipt, p.exportIdentity}
	raw, e := json.Marshal(value)
	if e != nil {
		return e
	}
	if p.receiptPath == "" {
		return errUnslopOrigin
	}
	if e = unslopPublishReceiptWithHook(ctx, p.receiptPath, raw, nil); e != nil {
		return e
	}
	p.committed = true
	return nil
}

func unslopPublishReceipt(ctx context.Context, path string, raw []byte) error {
	return unslopPublishReceiptWithHook(ctx, path, raw, nil)
}
func unslopPublishReceiptWithHook(ctx context.Context, path string, raw []byte, beforePublish func() error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	f, e := os.OpenFile(path+".pending", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	n, e := f.Write(raw)
	if e != nil || n != len(raw) {
		f.Close()
		return errUpdateObject
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if beforePublish != nil {
		if e = beforePublish(); e != nil {
			return e
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	from, e := syscall.UTF16PtrFromString(path + ".pending")
	if e != nil {
		return e
	}
	to, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return e
	}
	ok, _, e := syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), 8)
	if ok == 0 {
		return e
	}
	return nil
}

// The shared main dispatches only this exact finite operation set. Capturing a
// fresh artifact does not authorize using its unsigned receipt as an update
// grant: the install consumer is pinned/reviewed after observing that artifact.
func runNormalUnslopPurpose(args []string, out io.Writer) int {
	if len(args) != 1 {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	runtime := unslopPurposeRuntime{}
	if args[0] == "--capture-unslop-release" {
		return runNormalUnslopOwnedCapture(ctx, out)
	}
	return runUnslopPurpose(ctx, args, out, runtime)
}
