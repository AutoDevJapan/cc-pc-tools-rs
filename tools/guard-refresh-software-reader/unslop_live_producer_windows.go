//go:build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const unslopLiveDomain = "godd-unslop-held-producer-v1"
const unslopLiveEvidence = unslopCaptureRoot + "/cargo-stream.evidence.jsonl"
const unslopLiveInputManifest = unslopCaptureRoot + "/compiled-inputs.before.json"

// Small typed messages; no path or arbitrary command is accepted from stdin.
// The native supervisor binds these correlation values to its actual worker.
type unslopLiveReady struct {
	Domain           string                `json:"domain"`
	Phase            string                `json:"phase"`
	Nonce            string                `json:"nonce"`
	Source           string                `json:"source"`
	InputManifestSHA string                `json:"input_manifest_sha256"`
	ArtifactSHA      string                `json:"artifact_sha256"`
	ArtifactBytes    int                   `json:"artifact_bytes"`
	ArtifactIdentity softwareIdentity      `json:"artifact_identity"`
	EvidenceSHA      string                `json:"evidence_sha256"`
	EvidenceBytes    int                   `json:"evidence_bytes"`
	EvidenceIdentity softwareIdentity      `json:"evidence_identity"`
	CompilerProcess  unslopProcessIdentity `json:"compiler_process"`
}
type unslopLiveACK struct {
	Domain           string           `json:"domain"`
	Nonce            string           `json:"nonce"`
	Source           string           `json:"source"`
	InputManifestSHA string           `json:"input_manifest_sha256"`
	ArtifactSHA      string           `json:"artifact_sha256"`
	ArtifactIdentity softwareIdentity `json:"artifact_identity"`
	EvidenceSHA      string           `json:"evidence_sha256"`
}
type unslopLiveFinal struct {
	Ready       unslopLiveReady `json:"ready"`
	Phase       string          `json:"phase"`
	ACKReceived bool            `json:"ack_received"`
}

func unslopACKFor(ready unslopLiveReady) unslopLiveACK {
	return unslopLiveACK{unslopLiveDomain, ready.Nonce, ready.Source, ready.InputManifestSHA, ready.ArtifactSHA, ready.ArtifactIdentity, ready.EvidenceSHA}
}
func unslopReadyShape(r unslopLiveReady) bool {
	return r.Domain == unslopLiveDomain && r.Phase == "ready" && unslopHex(r.Nonce, 32) && r.Source == unslopCaptureSource && unslopHex(r.InputManifestSHA, 64) && unslopHex(r.ArtifactSHA, 64) && r.ArtifactBytes > 0 && r.ArtifactBytes <= 32<<20 && unslopCompleteIdentity(r.ArtifactIdentity) && unslopHex(r.EvidenceSHA, 64) && r.EvidenceBytes > 0 && r.EvidenceBytes <= 8<<20 && unslopCompleteIdentity(r.EvidenceIdentity) && r.CompilerProcess.PID != 0 && r.CompilerProcess.Created != 0
}
func unslopACKMatches(raw []byte, ready unslopLiveReady) bool {
	if !unslopReadyShape(ready) || len(raw) == 0 || len(raw) > 4096 {
		return false
	}
	var ack unslopLiveACK
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(&ack) == nil && d.Decode(new(any)) == io.EOF && ack == unslopACKFor(ready)
}
func unslopFinalMatches(raw []byte, ready unslopLiveReady) bool {
	if len(raw) == 0 || len(raw) > 16<<10 {
		return false
	}
	var final unslopLiveFinal
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(&final) == nil && d.Decode(new(any)) == io.EOF && final.Ready == ready && final.Phase == "ack_received" && final.ACKReceived
}

func holdUnslopWorkerInputs(ctx context.Context, manifestSHA, drive string) ([]updateObject, func() error, error) {
	var held []updateObject
	var parents []syscall.Handle
	closeAll := func() error {
		var errs []error
		for i := len(held) - 1; i >= 0; i-- {
			errs = append(errs, held[i].Close())
		}
		for i := len(parents) - 1; i >= 0; i-- {
			errs = append(errs, syscall.CloseHandle(parents[i]))
		}
		return errors.Join(errs...)
	}
	if !unslopHex(manifestSHA, 64) || len(drive) != 2 || drive[1] != ':' || drive[0] < 'P' || drive[0] > 'Z' {
		return nil, closeAll, errUnslopOrigin
	}
	proof, e := openUpdateObject(unslopLiveInputManifest)
	if e != nil {
		return nil, closeAll, e
	}
	held = append(held, proof)
	if digest(proof.Raw()) != manifestSHA {
		return nil, closeAll, errUnslopOrigin
	}
	var inputs unslopCapturedInputs
	d := json.NewDecoder(bytes.NewReader(proof.Raw()))
	d.DisallowUnknownFields()
	if d.Decode(&inputs) != nil || d.Decode(new(any)) != io.EOF {
		return nil, closeAll, errUnslopOrigin
	}
	if _, ok := unslopCapturedManifest(inputs); !ok {
		return nil, closeAll, errUnslopOrigin
	}
	var cfg *os.File
	for _, group := range []struct {
		files []unslopCapturedInput
		root  string
	}{{inputs.Sources, unslopCaptureRoot + "/source"}, {inputs.Dependencies, unslopCaptureRoot + "/source"}, {inputs.Configuration, unslopCaptureRoot}} {
		for _, record := range group.files {
			if ctx.Err() != nil {
				return nil, closeAll, ctx.Err()
			}
			p := filepath.Join(group.root, filepath.FromSlash(record.Path))
			o, e := openUnslopHeldInput(p)
			if e != nil {
				return nil, closeAll, e
			}
			held = append(held, o)
			if o.ID() != record.Identity || digest(o.Raw()) != record.SHA {
				return nil, closeAll, errUnslopOrigin
			}
			if record.Path == "cargo-home/config.toml" {
				cfg = o.(*nativeUpdateObject).f
			}
		}
	}
	if cfg == nil {
		return nil, closeAll, errUnslopOrigin
	}
	settings := mp4CompilerEnvironment(unslopCaptureRoot + "/cargo-home")
	if mp4ConfigurationDigest(context.WithValue(ctx, askChildEnvironment{}, settings)) != inputs.ConfigurationSHA {
		return nil, closeAll, errors.New("worker_compiler_configuration_mismatch")
	}
	var toolHashes []string
	for _, p := range []string{unslopCargoExe, unslopRustcExe, "D:/CC/Tools/codex-jobs/cargo_gate.exe", "D:/CC/Tools/sccache/sccache.exe"} {
		raw, ok := askGitCommand(ctx, installedGuardPath, "--guard-path", p)
		var q struct {
			Allowed bool `json:"access_allowed"`
			Client  bool `json:"client_resource"`
		}
		valid := json.Unmarshal(raw, &q) == nil && ok && q.Allowed && !q.Client
		clear(raw)
		if !valid {
			return nil, closeAll, errUnslopOrigin
		}
		o, e := openUpdateObject(p)
		if e != nil {
			return nil, closeAll, e
		}
		held = append(held, o)
		toolHashes = append(toolHashes, digest(o.Raw()))
	}
	toolManifest, _ := json.Marshal(toolHashes)
	if digest(toolManifest) != inputs.ToolchainSHA {
		return nil, closeAll, errors.New("worker_toolchain_mismatch")
	}
	// The worker only borrows the parent's newly created empty Cargo cwd.
	mapping, e := cargoDriveMapping(drive)
	if e != nil || mapping != "\\??\\"+filepath.Join(unslopCaptureRoot, "cargo-home", "cwd") {
		return nil, closeAll, errUnslopOrigin
	}
	return held, closeAll, nil
}

func runUnslopNativeBuildWorker(args []string, out io.Writer) (exit int) {
	if len(args) != 4 || args[0] != "--unslop-native-build-worker" || !unslopHex(args[1], 32) || !unslopHex(args[2], 64) || !askWorkerParent() {
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), 13*time.Minute)
	defer cancel()
	stopRead := context.AfterFunc(ctx, func() { _ = os.Stdin.Close() })
	defer stopRead()
	held, closeAll, e := holdUnslopWorkerInputs(ctx, args[2], args[3])
	defer func() {
		if closeAll() != nil {
			exit = 1
		}
	}()
	if e != nil {
		return 1
	}
	settings := mp4CompilerEnvironment(unslopCaptureRoot + "/cargo-home")
	childCtx := context.WithValue(ctx, askChildEnvironment{}, settings)
	childCtx = context.WithValue(childCtx, askChildExactEnvironment{}, true)
	childCtx = context.WithValue(childCtx, askChildCaptureKey{}, askChildCapture{})
	child := askOwnedChildAt(childCtx, args[3]+"\\", "D:/CC/Tools/codex-jobs/cargo_gate.exe", unslopCaptureCommand()...)
	defer clear(child.Raw)
	if child.Capture == nil {
		return 1
	}
	defer clear(child.Capture.Stdout)
	if !child.Capture.Complete || child.Capture.Overflow || !child.Reaped || !child.WaitVerified || child.TimedOut || child.Exit != 0 || child.Capture.NativeProcess == nil {
		return 1
	}
	projection, valid := unslopCargoProjection(child.Capture.Stdout)
	clear(projection)
	if !valid {
		return 1
	}
	if _, e = createAskArtifactCopy(unslopLiveEvidence, child.Capture.Stdout); e != nil {
		return 1
	}
	evidence, e := openUpdateObject(unslopLiveEvidence)
	if e != nil {
		return 1
	}
	defer func() {
		if evidence.Close() != nil {
			exit = 1
		}
	}()
	if digest(evidence.Raw()) != digest(child.Capture.Stdout) {
		return 1
	}
	aliases, e := softwareLinks(unslopCaptureOutput)
	if e != nil {
		return 1
	}
	alias, ok := unslopCargoAlias(aliases)
	if !ok {
		return 1
	}
	accepted := withPinnedCargoOutput(unslopCaptureOutput, alias, softwareIdentity{}, func(raw []byte, id softwareIdentity) bool {
		if ctx.Err() != nil {
			return false
		}
		// Every input handle remains held throughout Cargo and this ACK read.
		for _, o := range held {
			if !unslopCompleteIdentity(o.ID()) {
				return false
			}
		}
		ready := unslopLiveReady{Domain: unslopLiveDomain, Phase: "ready", Nonce: args[1], Source: unslopCaptureSource, InputManifestSHA: args[2], ArtifactSHA: digest(raw), ArtifactBytes: len(raw), ArtifactIdentity: id, EvidenceSHA: digest(evidence.Raw()), EvidenceBytes: len(evidence.Raw()), EvidenceIdentity: evidence.ID(), CompilerProcess: unslopProcessIdentity{PID: child.Capture.NativeProcess.PID, Created: child.Capture.NativeProcess.Creation}}
		if !unslopReadyShape(ready) || json.NewEncoder(out).Encode(ready) != nil {
			return false
		}
		line, e := bufio.NewReader(io.LimitReader(os.Stdin, 4097)).ReadBytes('\n')
		defer clear(line)
		if e != nil || !unslopACKMatches(line, ready) || ctx.Err() != nil {
			return false
		}
		return json.NewEncoder(out).Encode(unslopLiveFinal{Ready: ready, Phase: "ack_received", ACKReceived: true}) == nil
	})
	if !accepted {
		return 1
	}
	return 0
}

func marshalUnslopCanonicalInputs(input unslopCapturedInputs) ([]byte, error) {
	copyOf := input
	copyOf.Sources = append([]unslopCapturedInput(nil), input.Sources...)
	copyOf.Dependencies = append([]unslopCapturedInput(nil), input.Dependencies...)
	copyOf.Configuration = append([]unslopCapturedInput(nil), input.Configuration...)
	for _, files := range [][]unslopCapturedInput{copyOf.Sources, copyOf.Dependencies, copyOf.Configuration} {
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	}
	return json.Marshal(copyOf)
}

// Only admitted snapshot records call this reader. A zero-byte dependency is
// a real compiler input, unlike the non-empty executable reader's contract.
func openUnslopHeldInput(path string) (updateObject, error) {
	o := &nativeUpdateObject{}
	ok := false
	defer func() {
		if !ok {
			o.Close()
		}
	}()
	var paths []string
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		paths = append(paths, p)
		if filepath.Dir(p) == p {
			break
		}
	}
	for i := len(paths) - 1; i >= 0; i-- {
		h, good := openPlain(paths[i], true)
		if !good {
			return nil, errUpdateObject
		}
		o.parents = append(o.parents, h)
	}
	h, good := openPlain(path, false)
	if !good {
		return nil, errUpdateObject
	}
	o.f = os.NewFile(uintptr(h), path)
	stat, e := o.f.Stat()
	if e != nil || stat.Size() < 0 || stat.Size() > cargoVendorFileLimit {
		return nil, errUpdateObject
	}
	o.id, e = fileSoftwareIdentity(o.f)
	if e != nil {
		return nil, e
	}
	o.raw, e = io.ReadAll(io.LimitReader(o.f, cargoVendorFileLimit+1))
	if e != nil || int64(len(o.raw)) != stat.Size() {
		return nil, errUpdateObject
	}
	ok = true
	return o, nil
}

// Used in source proofs and tests; no secret text is included in identifiers.
func unslopWorkerCorrelation(nonce, manifest, drive string) []string {
	return []string{"--unslop-native-build-worker", nonce, manifest, strings.ToUpper(drive)}
}
func unslopWorkerLabel(pid uint32, created uint64) string {
	return fmt.Sprintf("native-worker/%s/%s", strconv.FormatUint(uint64(pid), 10), strconv.FormatUint(created, 10))
}

type unslopLiveFlight interface {
	Ready(context.Context) (unslopLiveReady, error)
	SendACK(context.Context, unslopLiveACK) error
	Done(context.Context) (unslopLiveFinal, unslopProducerCompletion, error)
	Abort()
}
type unslopLiveStart func(context.Context, string, string, string) (unslopLiveFlight, error)

type unslopLiveSession struct {
	*unslopNativeSession
	start     unslopLiveStart
	flight    unslopLiveFlight
	ready     unslopLiveReady
	evidence  updateObject
	completed bool
}

func (s *unslopLiveSession) Build(ctx context.Context) (unslopBuildInvocation, error) {
	if s.building || s.closed || ctx.Err() != nil || s.start == nil {
		return unslopBuildInvocation{}, errUnslopOrigin
	}
	s.building = true
	manifest, e := marshalUnslopCanonicalInputs(s.before)
	if e != nil {
		return unslopBuildInvocation{}, e
	}
	defer clear(manifest)
	manifestSHA, ok := unslopCapturedManifest(s.before)
	if !ok || digest(manifest) != manifestSHA {
		return unslopBuildInvocation{}, errUnslopOrigin
	}
	if _, e = createAskArtifactCopy(unslopLiveInputManifest, manifest); e != nil {
		return unslopBuildInvocation{}, e
	}
	s.manifest, e = openUpdateObject(unslopLiveInputManifest)
	if e != nil || digest(s.manifest.Raw()) != manifestSHA {
		return unslopBuildInvocation{}, errUnslopOrigin
	}
	var nonceBytes [16]byte
	if _, e = rand.Read(nonceBytes[:]); e != nil {
		return unslopBuildInvocation{}, e
	}
	nonce := hex.EncodeToString(nonceBytes[:])
	clear(nonceBytes[:])
	s.flight, e = s.start(ctx, nonce, manifestSHA, s.configuration.cargoDrive)
	if e != nil {
		return unslopBuildInvocation{}, e
	}
	r, e := s.flight.Ready(ctx)
	if e != nil || !unslopReadyShape(r) || r.Nonce != nonce || r.InputManifestSHA != manifestSHA {
		return unslopBuildInvocation{}, errUnslopOrigin
	}
	s.ready = r
	s.evidence, e = openUpdateObject(unslopLiveEvidence)
	if e != nil {
		return unslopBuildInvocation{}, e
	}
	raw := s.evidence.Raw()
	if s.evidence.ID() != r.EvidenceIdentity || len(raw) != r.EvidenceBytes || digest(raw) != r.EvidenceSHA {
		return unslopBuildInvocation{}, errUnslopOrigin
	}
	projection, valid := unslopCargoProjection(raw)
	if !valid {
		return unslopBuildInvocation{}, errUnslopOrigin
	}
	command, _ := json.Marshal(unslopCaptureCommand())
	process := r.CompilerProcess
	s.build = unslopBuildInvocation{CommandSHA: digest(command), CargoStreamSHA: digest(raw), CargoJSONSHA: digest(projection), CargoStream: raw, CargoJSON: projection, NativeProcess: &process, Started: true, Assigned: true, WaitVerified: true, Reaped: true, Exit: 0}
	// These lifecycle fields describe the inner compiler. The outer producer
	// remains live and held until SendACK/Done below establish its completion.
	return s.build, nil
}

func (s *unslopLiveSession) WithArtifact(use func([]byte, softwareIdentity) bool) bool {
	if !unslopReadyShape(s.ready) || s.flight == nil || s.completed || s.closed {
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
	return withPinnedCargoOutput(unslopCaptureOutput, alias, s.ready.ArtifactIdentity, func(raw []byte, id softwareIdentity) bool {
		if len(raw) != s.ready.ArtifactBytes || digest(raw) != s.ready.ArtifactSHA {
			return false
		}
		s.artifactHeld = true
		defer func() { s.artifactHeld = false }()
		return use(raw, id)
	})
}
func (s *unslopLiveSession) Acknowledge(ctx context.Context) error {
	if ctx.Err() != nil || !s.artifactHeld || s.acknowledged || s.flight == nil {
		return errUnslopOrigin
	}
	if e := s.flight.SendACK(ctx, unslopACKFor(s.ready)); e != nil {
		return e
	}
	s.acknowledged = true
	return nil
}
func (s *unslopLiveSession) AwaitCompletion(ctx context.Context) (unslopProducerCompletion, error) {
	if !s.acknowledged || s.flight == nil {
		return unslopProducerCompletion{}, errUnslopOrigin
	}
	final, result, e := s.flight.Done(ctx)
	if e != nil || final.Ready != s.ready || final.Phase != "ack_received" || !final.ACKReceived || !result.ACKReceived || !result.Reaped || !result.WaitVerified || result.Exit != 0 {
		if result.Reaped {
			s.completed = true
		}
		return unslopProducerCompletion{}, errUnslopOrigin
	}
	s.completed = true
	s.producerConfirmed = true
	return result, nil
}
func (s *unslopLiveSession) Close() error {
	var errs []error
	if s.flight != nil && !s.completed {
		s.flight.Abort()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, finished, e := s.flight.Done(ctx)
		cancel()
		if !finished.Reaped {
			return errors.Join(e, errors.New("live_worker_collection_unknown_inputs_preserved"))
		}
		if e != nil {
			errs = append(errs, e)
		}
	}
	if s.evidence != nil {
		errs = append(errs, s.evidence.Close())
		s.evidence = nil
	}
	errs = append(errs, s.unslopNativeSession.Close())
	return errors.Join(errs...)
}

func liveUnslopCaptureAccess(start unslopLiveStart) unslopCaptureAccess {
	var live *unslopLiveSession
	return unslopCaptureAccess{begin: func(ctx context.Context) (unslopProducerSession, error) {
		base, e := beginUnslopNative(ctx, func(context.Context) askChildResult { return askChildResult{} })
		if e != nil {
			return nil, e
		}
		live = &unslopLiveSession{unslopNativeSession: base, start: start}
		return live, nil
	}, stage: func(ctx context.Context, r unslopCaptureReceipt, raw []byte) (unslopPendingCapture, error) {
		if live == nil {
			return nil, errUnslopOrigin
		}
		return live.unslopNativeSession.stage(ctx, r, raw)
	}}
}

type unslopOwnedFlight struct {
	flight *askOwnedProducerFlight
	ready  unslopLiveReady
	result *askProducerFlightResult
}

func fromOwnedReady(r askProducerReady) unslopLiveReady {
	return unslopLiveReady{Domain: r.Domain, Phase: r.Phase, Nonce: r.Nonce, Source: r.Source, InputManifestSHA: r.InputManifestSHA, ArtifactSHA: r.ArtifactSHA, ArtifactBytes: r.ArtifactBytes, ArtifactIdentity: r.ArtifactIdentity, EvidenceSHA: r.EvidenceSHA, EvidenceBytes: r.EvidenceBytes, EvidenceIdentity: r.EvidenceIdentity, CompilerProcess: unslopProcessIdentity{PID: r.CompilerProcess.PID, Created: r.CompilerProcess.Created}}
}
func (f *unslopOwnedFlight) Ready(ctx context.Context) (unslopLiveReady, error) {
	select {
	case r, ok := <-f.flight.Ready:
		if !ok {
			return unslopLiveReady{}, errUnslopOrigin
		}
		f.ready = fromOwnedReady(r)
		return f.ready, nil
	case <-ctx.Done():
		return unslopLiveReady{}, ctx.Err()
	}
}
func (f *unslopOwnedFlight) SendACK(ctx context.Context, a unslopLiveACK) error {
	if a != unslopACKFor(f.ready) {
		return errUnslopOrigin
	}
	ack := askProducerACK{Domain: a.Domain, Nonce: a.Nonce, Source: a.Source, InputManifestSHA: a.InputManifestSHA, ArtifactSHA: a.ArtifactSHA, ArtifactIdentity: a.ArtifactIdentity, EvidenceSHA: a.EvidenceSHA}
	select {
	case f.flight.Accept <- ack:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (f *unslopOwnedFlight) Done(ctx context.Context) (unslopLiveFinal, unslopProducerCompletion, error) {
	if f.result == nil {
		select {
		case r, ok := <-f.flight.Done:
			if !ok {
				return unslopLiveFinal{}, unslopProducerCompletion{}, errUnslopOrigin
			}
			f.result = &r
		case <-ctx.Done():
			return unslopLiveFinal{}, unslopProducerCompletion{}, ctx.Err()
		}
	}
	r := f.result
	defer clear(r.Child.Raw)
	if r.Child.Capture != nil {
		defer clear(r.Child.Capture.Stdout)
	}
	final := unslopLiveFinal{Ready: fromOwnedReady(r.Final.Ready), Phase: r.Final.Phase, ACKReceived: r.Final.ACKReceived}
	completed := unslopProducerCompletion{ACKReceived: r.ACKReceived && r.ACKSent, Reaped: r.Child.Reaped, WaitVerified: r.Child.WaitVerified, Exit: r.Child.Exit}
	if !r.ProtocolComplete || r.NativeProcess == nil || r.NativeProcess.PID == 0 || r.NativeProcess.Creation == 0 {
		return final, completed, errUnslopOrigin
	}
	return final, completed, nil
}
func (f *unslopOwnedFlight) Abort() { f.flight.Abort() }

func normalUnslopLiveCaptureAccess() unslopCaptureAccess {
	return liveUnslopCaptureAccess(func(ctx context.Context, nonce, manifest, drive string) (unslopLiveFlight, error) {
		self, e := os.Executable()
		if e != nil {
			return nil, e
		}
		flight, e := askOwnedProducerStart(ctx, self, unslopWorkerCorrelation(nonce, manifest, drive), askProducerExpected{Domain: unslopLiveDomain, Nonce: nonce, Source: unslopCaptureSource, InputManifestSHA: manifest})
		if e != nil {
			return nil, e
		}
		return &unslopOwnedFlight{flight: flight}, nil
	})
}
