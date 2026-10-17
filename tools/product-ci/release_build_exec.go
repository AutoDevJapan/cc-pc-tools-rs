package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Bounded builder phase.
//
// Contract with the caller (the future orchestration, not this file): hold the
// job/source/role/queue/budget admission AND the exclusive phase reservation
// below before calling. This file takes no exclusion and never raises a limit
// or runs beside another builder, service or runner. The phase is 2 CPU, 8 GiB
// and 512 PIDs: the builder (1.5/6/448), the idle runner (0.5/1/64) and 1 GiB of
// host byte transfer. A receipt with PhaseMayRelease=false means a container,
// volume or introduced tool image may still exist; the caller must keep the
// exclusion and the owned directory until recoverAuditedReleaseBuild succeeds.
const (
	releaseBuilderCPUNano     = 1_500_000_000
	releaseBuilderMemory      = 6 << 30
	releaseBuilderPids        = 448
	releaseIdleRunnerCPUNano  = 500_000_000
	releaseIdleRunnerMemory   = 1 << 30
	releaseIdleRunnerPids     = 64
	releaseHostTransferBudget = 1 << 30
	releasePhaseCPUNano       = 2_000_000_000
	releasePhaseMemory        = 8 << 30
	releasePhasePids          = 512

	releaseBuildArtifactLimit = 256 << 20
	releaseBuildLogLimit      = 32 << 20
	releaseBuildMaxDuration   = 45 * time.Minute
	releaseBuildOutputPath    = "/kaniko/output.tar"
	releaseBuildLabel         = "cc.release-build"
	releaseBuildStateFile     = "release-build-state.json"
	releaseBuildLogFile       = "builder.log"
	releaseBuildConfigDir     = "docker-config"
)

// A profile is a closed set of constants. There is no option that selects an
// image, mount, device, privilege, registry or network from input.
type releaseBuilderProfile struct {
	kind                   string
	nanoCPUs, memory, pids int64
	network                string
	artifactMax            int64
	tmpfs, env             []string
	kernelProof            bool
}

// The audited product build must fetch its digest-pinned base image and
// packages, so it has Docker's ordinary outbound bridge network. It never gets
// host networking, published ports, a socket, a bind, a device or a capability.
var productBuilderProfile = releaseBuilderProfile{
	kind: "audited-product", nanoCPUs: releaseBuilderCPUNano, memory: releaseBuilderMemory, pids: releaseBuilderPids,
	network: "bridge", artifactMax: releaseBuildArtifactLimit,
	tmpfs: []string{"/tmp:rw,nosuid,nodev,size=256m", "/workspace:rw,nosuid,nodev,size=64m"},
	env:   []string{"GOMAXPROCS=2", "GOMEMLIMIT=4GiB"},
}

// Only the CC-authored Go probe fixture uses this profile: no network, at most
// 0.5 CPU, 512 MiB and 128 PIDs, and kernel values are read back from the image.
var fixtureBuilderProfile = releaseBuilderProfile{
	kind: "fixture", nanoCPUs: 500_000_000, memory: 512 << 20, pids: 128,
	network: "none", artifactMax: 32 << 20,
	tmpfs:       []string{"/tmp:rw,nosuid,nodev,size=64m", "/workspace:rw,nosuid,nodev,size=32m"},
	env:         []string{"GOMAXPROCS=2", "GOMEMLIMIT=384MiB"},
	kernelProof: true,
}

type releaseBuilderSpec struct {
	profile                   releaseBuilderProfile
	context                   []byte
	contextSHA256             string
	dockerfile                string
	tree, snapshotSHA256      string
	contextFiles, contextDirs int
	maxDuration               time.Duration
}

func (releaseBuilderSpec) Format(s fmt.State, _ rune) { io.WriteString(s, "<release-builder-spec>") }

func (s releaseBuilderSpec) verifiedContext() ([]byte, error) {
	b := bytes.Clone(s.context)
	if len(b) == 0 || len(b) > releaseBuildContextGzipLimit || digest(b) != s.contextSHA256 {
		return nil, errors.New("builder context changed")
	}
	return b, nil
}

func releaseBuildSpecFromInput(in sealedReleaseBuildInput) (releaseBuilderSpec, error) {
	if _, err := in.verifiedContext(); err != nil || !releaseDockerfileName.MatchString(in.dockerfile) || !releaseObjectSHA(in.tree) {
		return releaseBuilderSpec{}, releaseBuildError("input-rejected")
	}
	return releaseBuilderSpec{profile: productBuilderProfile, context: bytes.Clone(in.context), contextSHA256: in.contextSHA256, dockerfile: in.dockerfile, tree: in.tree, snapshotSHA256: in.snapshotSHA256, contextFiles: in.contextFiles, contextDirs: in.contextDirs, maxDuration: releaseBuildMaxDuration}, nil
}

// Errors carry only a fixed class, never Docker stderr, argv, names or bytes.
type releaseBuildError string

func (e releaseBuildError) Error() string { return "release build: " + string(e) }

// Safe to serialize: opaque identifiers, hashes, counts and booleans only.
type releaseBuildReceipt struct {
	Kind                                                        string
	Nonce, ContainerName, ToolImage                             string
	ToolImageID                                                 string
	ToolImagePreexisting, ToolImageIntroduced, ToolImageRemoved bool
	CPUNano, MemoryBytes, Pids                                  int64
	Network                                                     string
	SnapshotTree, SnapshotSHA256                                string
	ContextSHA256                                               string
	ContextBytes                                                int64
	ContextFiles, ContextDirs                                   int
	CreateAttempted, UncertainCreate                            bool
	ContainerID                                                 string
	OuterIsolationVerified                                      bool
	Started                                                     bool
	ExitCode                                                    int
	OOMKilled                                                   bool
	LogBytes                                                    int64
	LogSHA256                                                   string
	LogTruncated                                                bool
	ArtifactBytes                                               int64
	ArtifactSHA256                                              string
	KernelProofVerified                                         bool
	KernelProof                                                 map[string]string
	PIDZero, ContainerRemoved, ContainerAbsent, VolumesAbsent   bool
	VolumeNames                                                 []string
	PhaseMayRelease, RecoveryRequired                           bool
	Succeeded                                                   bool
	Failure                                                     string
	ExecutionAuthorized                                         bool

	artifact []byte
}

func (releaseBuildReceipt) Format(s fmt.State, _ rune) { io.WriteString(s, "<release-build-receipt>") }

// verifiedArtifact returns a private copy of the image archive only if it still
// matches the recorded size and digest. The archive is never written to the host.
func (r releaseBuildReceipt) verifiedArtifact() ([]byte, error) {
	b := bytes.Clone(r.artifact)
	if !r.Succeeded || len(b) == 0 || int64(len(b)) != r.ArtifactBytes || digest(b) != r.ArtifactSHA256 {
		return nil, errors.New("release build artifact unavailable")
	}
	return b, nil
}

// builderDocker is the only way this file talks to Docker.
type builderDocker interface {
	run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error
}

type cliBuilderDocker struct{ config string }

func (d cliBuilderDocker) run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args ...string) error {
	c := cmd(ctx, docker, append([]string{"--config", d.config}, args...)...)
	c.Stdin, c.Stdout, c.Stderr = stdin, stdout, stderr
	return c.Run()
}

// executeAuditedReleaseBuild runs the single audited build. See the phase
// contract above: it neither waits for nor grants the phase reservation.
func executeAuditedReleaseBuild(ctx context.Context, input sealedReleaseBuildInput, ownedDir string) (releaseBuildReceipt, error) {
	spec, err := releaseBuildSpecFromInput(input)
	if err != nil {
		return releaseBuildReceipt{Failure: "input-rejected"}, err
	}
	return runReleaseBuilder(ctx, func(config string) builderDocker { return cliBuilderDocker{config} }, spec, ownedDir)
}

// recoverAuditedReleaseBuild removes exactly the resources named by the saved
// reservation of an earlier run in ownedDir (for example after a crash).
func recoverAuditedReleaseBuild(ctx context.Context, ownedDir string) (releaseBuildReceipt, error) {
	return recoverReleaseBuilder(ctx, func(config string) builderDocker { return cliBuilderDocker{config} }, ownedDir)
}

var (
	releaseNoncePattern     = regexp.MustCompile("^[0-9a-f]{32}$")
	releaseContainerPattern = regexp.MustCompile("^[0-9a-f]{64}$")
	releaseVolumePattern    = regexp.MustCompile("^[0-9a-zA-Z][0-9a-zA-Z_.-]{0,127}$")
)

type builderRun struct {
	parent   context.Context
	d        builderDocker
	spec     releaseBuilderSpec
	dir      string
	rec      releaseBuildReceipt
	artifact []byte
}

func newBuildReceipt(spec releaseBuilderSpec) releaseBuildReceipt {
	return releaseBuildReceipt{Kind: spec.profile.kind, ToolImage: releaseBuilderTool, CPUNano: spec.profile.nanoCPUs, MemoryBytes: spec.profile.memory, Pids: spec.profile.pids, Network: spec.profile.network,
		SnapshotTree: spec.tree, SnapshotSHA256: spec.snapshotSHA256, ContextSHA256: spec.contextSHA256, ContextBytes: int64(len(spec.context)), ContextFiles: spec.contextFiles, ContextDirs: spec.contextDirs, ExitCode: -1}
}

func runReleaseBuilder(parent context.Context, factory func(string) builderDocker, spec releaseBuilderSpec, ownedDir string) (releaseBuildReceipt, error) {
	b := &builderRun{parent: parent, spec: spec, dir: ownedDir, rec: newBuildReceipt(spec)}
	if _, err := spec.verifiedContext(); err != nil || spec.maxDuration <= 0 || !releaseDockerfileName.MatchString(spec.dockerfile) || (spec.profile.kind != "audited-product" && spec.profile.kind != "fixture") {
		b.rec.Failure = "input-rejected"
		return b.rec, releaseBuildError(b.rec.Failure)
	}
	if class := b.begin(factory); class != "" {
		b.rec.Failure = class
		return b.rec, releaseBuildError(class)
	}
	b.execute()
	b.dispose()
	return b.finish()
}

func recoverReleaseBuilder(parent context.Context, factory func(string) builderDocker, ownedDir string) (releaseBuildReceipt, error) {
	b := &builderRun{parent: parent, dir: ownedDir}
	if class := validateOwnedDir(ownedDir, true); class != "" {
		return releaseBuildReceipt{Failure: class}, releaseBuildError(class)
	}
	raw, err := os.ReadFile(filepath.Join(ownedDir, releaseBuildStateFile))
	if err != nil || len(raw) > 1<<20 || json.Unmarshal(raw, &b.rec) != nil || !releaseNoncePattern.MatchString(b.rec.Nonce) || b.rec.ContainerName != "cc-release-build-"+b.rec.Nonce || b.rec.ToolImage != releaseBuilderTool {
		return releaseBuildReceipt{Failure: "state-unavailable"}, releaseBuildError("state-unavailable")
	}
	for _, v := range b.rec.VolumeNames {
		if !releaseVolumePattern.MatchString(v) {
			return releaseBuildReceipt{Failure: "state-unavailable"}, releaseBuildError("state-unavailable")
		}
	}
	b.d = factory(filepath.Join(ownedDir, releaseBuildConfigDir))
	b.rec.Failure, b.rec.Succeeded, b.rec.artifact = "", false, nil
	b.dispose()
	b.rec.Failure = "recovered"
	if !b.rec.PhaseMayRelease {
		b.rec.Failure = "recovery-required"
	}
	b.rec.RecoveryRequired = !b.rec.PhaseMayRelease
	_ = b.persist()
	if b.rec.RecoveryRequired {
		return b.rec, releaseBuildError("recovery-required")
	}
	return b.rec, nil
}

func validateOwnedDir(dir string, allowContent bool) string {
	if dir == "" || !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return "owned-dir-rejected"
	}
	fi, err := os.Lstat(dir)
	if err != nil || !fi.IsDir() || fi.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return "owned-dir-rejected"
	}
	if !allowContent {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			return "owned-dir-rejected" // A fresh safe-delete owned directory only.
		}
	}
	return ""
}

// begin records the reservation before any Docker resource exists.
func (b *builderRun) begin(factory func(string) builderDocker) string {
	if validateOwnedDir(b.dir, false) != "" {
		return "owned-dir-rejected"
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "state-unavailable"
	}
	b.rec.Nonce = hex.EncodeToString(raw)
	b.rec.ContainerName = "cc-release-build-" + b.rec.Nonce
	config := filepath.Join(b.dir, releaseBuildConfigDir)
	if os.Mkdir(config, 0o700) != nil {
		return "owned-dir-rejected"
	}
	b.d = factory(config)
	if b.persist() != nil {
		return "state-unavailable"
	}
	return ""
}

func (b *builderRun) persist() error {
	raw, err := json.MarshalIndent(b.rec, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(b.dir, releaseBuildStateFile+".tmp")
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(raw)
	serr := f.Sync()
	cerr := f.Close()
	if werr != nil || serr != nil || cerr != nil {
		return errors.New("state write failed")
	}
	return os.Rename(tmp, filepath.Join(b.dir, releaseBuildStateFile))
}

func dockerText(ctx context.Context, d builderDocker, limit int, args ...string) (string, string, error) {
	out, stderr := boundedBuffer{limit: limit}, boundedBuffer{limit: 64 << 10}
	err := d.run(ctx, nil, &out, &stderr, args...)
	return strings.TrimSpace(out.String()), stderr.String(), err
}

// A cancelled or expired context outranks any secondary Docker failure.
func (b *builderRun) classify(ctx context.Context, fallback string) string {
	if err := b.parent.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "timeout"
		}
		return "canceled"
	}
	if ctx.Err() != nil {
		return "timeout"
	}
	return fallback
}

func (b *builderRun) execute() {
	ctx, cancel := context.WithTimeout(b.parent, b.spec.maxDuration)
	defer cancel()
	for _, step := range []func(context.Context) string{b.phaseClear, b.toolImage, b.create, b.verifyOuter, b.build, b.collect} {
		if class := step(ctx); class != "" {
			b.rec.Failure = class
			return
		}
	}
}

// A builder left by an earlier run (even one that crashed) means the phase is not
// free. Nothing is pulled or created until its owner has recovered it.
func (b *builderRun) phaseClear(ctx context.Context) string {
	out, _, err := dockerText(ctx, b.d, 1<<20, "container", "ls", "--all", "--no-trunc", "--quiet", "--filter", "label="+releaseBuildLabel)
	switch {
	case err != nil:
		return b.classify(ctx, "phase-uncertain")
	case out != "":
		return "phase-busy"
	}
	return ""
}

// Absent is concluded only from Docker's own "no such image" answer.
func (b *builderRun) imagePresent(ctx context.Context) (bool, error) {
	out, stderr, err := dockerText(ctx, b.d, 1<<16, "image", "inspect", "--format", "{{.Id}}", releaseBuilderTool)
	if err == nil && strings.HasPrefix(out, "sha256:") {
		return true, nil
	}
	if err != nil && strings.Contains(strings.ToLower(stderr), "no such image") {
		return false, nil
	}
	return false, errors.New("tool image state uncertain")
}

func (b *builderRun) toolImage(ctx context.Context) string {
	present, err := b.imagePresent(ctx)
	if err != nil {
		return b.classify(ctx, "tool-image-unavailable")
	}
	if present {
		b.rec.ToolImagePreexisting = true
	} else {
		b.rec.ToolImageIntroduced = true
		if b.persist() != nil {
			return "state-unavailable"
		}
		stderr := boundedBuffer{limit: 64 << 10}
		if b.d.run(ctx, nil, io.Discard, &stderr, "pull", releaseBuilderTool) != nil {
			return b.classify(ctx, "tool-image-unavailable")
		}
	}
	out, _, err := dockerText(ctx, b.d, 1<<20, "image", "inspect", "--format", "{{.Id}} {{json .RepoDigests}}", releaseBuilderTool)
	id, digests, _ := strings.Cut(out, " ")
	var refs []string
	if err != nil || !strings.HasPrefix(id, "sha256:") || json.Unmarshal([]byte(digests), &refs) != nil {
		return b.classify(ctx, "tool-image-unavailable")
	}
	for _, r := range refs {
		if r == releaseBuilderTool {
			b.rec.ToolImageID = id
			return ""
		}
	}
	return "tool-image-mismatch"
}

func formatCPUs(nano int64) string { return strconv.FormatFloat(float64(nano)/1e9, 'f', -1, 64) }

// The create argv is fixed except for the opaque nonce and the validated
// Dockerfile name. No host environment value, credential or path is included.
func (s releaseBuilderSpec) createArgs(nonce, name string) []string {
	p := s.profile
	args := []string{"create", "--name", name, "--label", releaseBuildLabel + "=" + nonce, "--label", "cc.todo=2799", "--init", "--user", "0:0",
		"--network", p.network, "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--cpus", formatCPUs(p.nanoCPUs), "--memory", strconv.FormatInt(p.memory, 10), "--memory-swap", strconv.FormatInt(p.memory, 10), "--pids-limit", strconv.FormatInt(p.pids, 10), "--interactive"}
	for _, e := range p.env {
		args = append(args, "--env", e)
	}
	for _, t := range p.tmpfs {
		args = append(args, "--tmpfs", t)
	}
	return append(args, releaseBuilderTool, "--context=tar://stdin", "--dockerfile="+s.dockerfile, "--destination=cc-release-build:"+nonce,
		"--no-push", "--no-push-cache", "--cache=false", "--tar-path="+releaseBuildOutputPath, "--verbosity=warn", "--log-format=json")
}

func (b *builderRun) containersByNonce(ctx context.Context) ([]string, error) {
	out, _, err := dockerText(ctx, b.d, 1<<20, "container", "ls", "--all", "--no-trunc", "--quiet", "--filter", "label="+releaseBuildLabel+"="+b.rec.Nonce)
	if err != nil {
		return nil, errors.New("container listing failed")
	}
	var ids []string
	for _, line := range strings.Fields(out) {
		if !releaseContainerPattern.MatchString(line) {
			return nil, errors.New("container listing invalid")
		}
		ids = append(ids, line)
	}
	return ids, nil
}

func (b *builderRun) create(ctx context.Context) string {
	b.rec.CreateAttempted = true
	if b.persist() != nil { // Reserved before the daemon can know the name.
		return "state-unavailable"
	}
	out, _, err := dockerText(ctx, b.d, 4096, b.spec.createArgs(b.rec.Nonce, b.rec.ContainerName)...)
	if err == nil && releaseContainerPattern.MatchString(out) {
		b.rec.ContainerID = out
		_ = b.persist()
		return ""
	}
	// The daemon may have created it before the CLI result was lost. Only the
	// exact nonce can be recovered, and a recovered container is never started.
	rctx, cancel := context.WithTimeout(context.WithoutCancel(b.parent), time.Minute)
	defer cancel()
	for attempt := 0; attempt < 3; attempt++ {
		ids, err := b.containersByNonce(rctx)
		if err != nil || len(ids) > 1 {
			b.rec.UncertainCreate = true
			return "create-uncertain"
		}
		if len(ids) == 1 {
			b.rec.UncertainCreate, b.rec.ContainerID = true, ids[0]
			_ = b.persist()
			return "create-uncertain"
		}
		time.Sleep(releaseCreateRecheckPause)
	}
	return b.classify(ctx, "create-failed")
}

type builderInspect struct {
	ID    string `json:"Id"`
	Name  string
	State struct {
		Status     string
		Running    bool
		ExitCode   int
		Pid        int
		OOMKilled  bool
		Error      string
		FinishedAt string
	}
	Config struct {
		Image     string
		User      string
		OpenStdin bool
		Labels    map[string]string
	}
	HostConfig struct {
		Privileged                          bool
		Binds, CapAdd, CapDrop, SecurityOpt []string
		Devices, DeviceRequests             []json.RawMessage
		VolumesFrom, Links                  []string
		NanoCpus, Memory, MemorySwap        int64
		PidsLimit                           *int64
		NetworkMode, PidMode, IpcMode       string
		UsernsMode, UTSMode, CgroupnsMode   string
		ReadonlyRootfs, PublishAllPorts     bool
		Init                                *bool
		PortBindings                        map[string]json.RawMessage
		Tmpfs                               map[string]string
	}
	Mounts []struct {
		Type, Name, Destination string
	}
}

func (b *builderRun) inspect(ctx context.Context, id string) (builderInspect, error) {
	var info builderInspect
	out, _, err := dockerText(ctx, b.d, 1<<20, "container", "inspect", "--format", "{{json .}}", id)
	if err != nil || json.Unmarshal([]byte(out), &info) != nil {
		return builderInspect{}, errors.New("container inspection failed")
	}
	return info, nil
}

func (b *builderRun) ownedIdentity(info builderInspect) bool {
	return strings.TrimPrefix(info.Name, "/") == b.rec.ContainerName && info.Config.Labels[releaseBuildLabel] == b.rec.Nonce && info.Config.Image == releaseBuilderTool
}

// The inspected daemon values, not our create argv, decide whether the builder
// has the resource and privilege namespace this phase budgets.
func (b *builderRun) isolationOK(info builderInspect) bool {
	p, h := b.spec.profile, info.HostConfig
	if !b.ownedIdentity(info) || info.ID != b.rec.ContainerID || info.Config.User != "0:0" || !info.Config.OpenStdin || info.State.Running {
		return false
	}
	if h.Privileged || len(h.Binds) != 0 || len(h.CapAdd) != 0 || len(h.Devices) != 0 || len(h.DeviceRequests) != 0 || len(h.VolumesFrom) != 0 || len(h.Links) != 0 || len(h.PortBindings) != 0 || h.PublishAllPorts || h.ReadonlyRootfs {
		return false
	}
	if len(h.CapDrop) != 1 || h.CapDrop[0] != "ALL" || len(h.SecurityOpt) != 1 || (h.SecurityOpt[0] != "no-new-privileges" && h.SecurityOpt[0] != "no-new-privileges:true") {
		return false
	}
	if h.NanoCpus != p.nanoCPUs || h.Memory != p.memory || h.MemorySwap != p.memory || h.PidsLimit == nil || *h.PidsLimit != p.pids || h.NetworkMode != p.network || h.Init == nil || !*h.Init {
		return false
	}
	if h.PidMode != "" || h.UsernsMode != "" || h.UTSMode != "" || (h.IpcMode != "" && h.IpcMode != "private" && h.IpcMode != "shareable") {
		return false
	}
	if len(h.Tmpfs) != len(p.tmpfs) {
		return false
	}
	for _, t := range p.tmpfs {
		dest, opts, _ := strings.Cut(t, ":")
		if h.Tmpfs[dest] != opts {
			return false
		}
	}
	for _, m := range info.Mounts {
		if m.Type == "tmpfs" {
			continue
		}
		if m.Type != "volume" || !releaseVolumePattern.MatchString(m.Name) { // Docker-managed anonymous volume only.
			return false
		}
	}
	return true
}

func (b *builderRun) verifyOuter(ctx context.Context) string {
	info, err := b.inspect(ctx, b.rec.ContainerID)
	if err != nil {
		return b.classify(ctx, "inspect-failed")
	}
	if !b.isolationOK(info) {
		return "isolation-mismatch"
	}
	b.rec.OuterIsolationVerified = true
	return ""
}

// builderLog keeps the raw builder output in the owned directory and exposes
// only its size and digest. It never blocks the child: overflow cancels it.
type builderLog struct {
	mu               sync.Mutex
	f                *os.File
	h                hash.Hash
	limit, written   int64
	overflow, failed bool
	onStop           func()
}

func (l *builderLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	keep := p
	if room := l.limit - l.written; int64(len(p)) > room {
		keep = p[:room]
		l.overflow = true
	}
	if len(keep) > 0 && !l.failed {
		if _, err := l.f.Write(keep); err != nil {
			l.failed = true
		} else {
			l.h.Write(keep)
			l.written += int64(len(keep))
		}
	}
	if (l.overflow || l.failed) && l.onStop != nil {
		l.onStop()
		l.onStop = nil
	}
	return len(p), nil
}

func (b *builderRun) build(ctx context.Context) string {
	input, err := b.spec.verifiedContext()
	if err != nil {
		return "input-rejected"
	}
	f, err := os.OpenFile(filepath.Join(b.dir, releaseBuildLogFile), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return "log-unavailable"
	}
	buildCtx, stop := context.WithCancel(ctx)
	defer stop()
	lw := &builderLog{f: f, h: sha256.New(), limit: releaseBuildLogLimit, onStop: stop}
	b.rec.Started = true
	_ = b.persist()
	// Returns only after the Docker client has drained stdin, stdout and stderr.
	runErr := b.d.run(buildCtx, bytes.NewReader(input), lw, lw, "start", "--attach", "--interactive", b.rec.ContainerID)
	lw.mu.Lock()
	overflow, failed, written, sum := lw.overflow, lw.failed, lw.written, hex.EncodeToString(lw.h.Sum(nil))
	lw.mu.Unlock()
	closeErr := f.Close()
	b.rec.LogBytes, b.rec.LogSHA256, b.rec.LogTruncated = written, sum, overflow
	switch {
	case failed || closeErr != nil:
		return "log-unavailable"
	case overflow:
		return "log-overlimit"
	}
	if class := b.classify(ctx, ""); class != "" {
		return class
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(b.parent), time.Minute)
	defer cancel()
	info, err := b.inspect(sctx, b.rec.ContainerID)
	if err != nil {
		return "inspect-failed"
	}
	switch {
	case info.State.Running || info.State.Status != "exited":
		return "start-failed"
	case info.State.OOMKilled:
		b.rec.OOMKilled = true
		return "build-oom"
	case info.State.ExitCode != 0:
		b.rec.ExitCode = info.State.ExitCode
		return "build-failed"
	case runErr != nil || info.State.Pid != 0:
		return "start-failed"
	}
	b.rec.ExitCode = 0
	return ""
}

// Only the single regular file written by Kaniko is accepted, streamed straight
// into one exactly-sized buffer; nothing is extracted to the host.
func readSingleFileTar(r io.Reader, limit int64) ([]byte, string) {
	tr := tar.NewReader(r)
	h, err := tr.Next()
	if err != nil {
		return nil, "artifact-missing"
	}
	if h.Typeflag != tar.TypeReg || h.Size <= 0 {
		return nil, "artifact-invalid"
	}
	if h.Size > limit {
		return nil, "artifact-overlimit"
	}
	data := make([]byte, h.Size)
	if _, err := io.ReadFull(tr, data); err != nil {
		return nil, "artifact-invalid"
	}
	if _, err := tr.Next(); err != io.EOF {
		return nil, "artifact-invalid"
	}
	return data, ""
}

func (b *builderRun) collect(ctx context.Context) string {
	cpCtx, cancelCp := context.WithCancel(ctx)
	defer cancelCp()
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		stderr := boundedBuffer{limit: 64 << 10}
		err := b.d.run(cpCtx, nil, pw, &stderr, "cp", b.rec.ContainerID+":"+releaseBuildOutputPath, "-")
		pw.CloseWithError(err)
		done <- err
	}()
	data, class := readSingleFileTar(pr, b.spec.profile.artifactMax)
	if class != "" {
		cancelCp()
	}
	_, _ = io.Copy(io.Discard, pr) // Drain until the Docker client has really finished.
	_ = pr.Close()
	cpErr := <-done
	if class != "" {
		return b.classify(ctx, class)
	}
	if cpErr != nil {
		return b.classify(ctx, "artifact-missing")
	}
	proof, err := checkBuiltImage(data, b.spec.profile)
	if err != nil {
		if b.spec.profile.kernelProof && errors.Is(err, errKernelProof) {
			return "kernel-proof-mismatch"
		}
		return "artifact-invalid"
	}
	b.artifact = data
	b.rec.ArtifactBytes, b.rec.ArtifactSHA256 = int64(len(data)), digest(data)
	if proof != nil {
		b.rec.KernelProof, b.rec.KernelProofVerified = proof, true
	}
	return ""
}

var errKernelProof = errors.New("kernel proof mismatch")

// Pause between the checks that conclude a lost create did not reach the daemon.
var releaseCreateRecheckPause = time.Second

// A Docker-archive image has one manifest with at least one layer. The fixture
// profile additionally reads back what the kernel showed to the build step.
func checkBuiltImage(data []byte, p releaseBuilderProfile) (map[string]string, error) {
	tr := tar.NewReader(bytes.NewReader(data))
	var manifest []byte
	small := map[string][]byte{}
	total := int64(0)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("image archive invalid")
		}
		if h.Typeflag != tar.TypeReg {
			continue
		}
		keep := h.Name == "manifest.json" && h.Size <= 1<<20 || p.kernelProof && h.Size <= 32<<20 && total+h.Size <= 64<<20
		if !keep {
			continue
		}
		b, err := io.ReadAll(io.LimitReader(tr, h.Size))
		if err != nil || int64(len(b)) != h.Size {
			return nil, errors.New("image archive invalid")
		}
		if h.Name == "manifest.json" {
			manifest = b
		} else {
			small[h.Name] = b
			total += h.Size
		}
	}
	var images []struct{ Layers []string }
	if json.Unmarshal(manifest, &images) != nil || len(images) != 1 || len(images[0].Layers) == 0 {
		return nil, errors.New("image manifest invalid")
	}
	if !p.kernelProof {
		return nil, nil
	}
	want := map[string]string{"cpu.max": fmt.Sprintf("%d 100000", p.nanoCPUs/10000), "memory.max": strconv.FormatInt(p.memory, 10), "pids.max": strconv.FormatInt(p.pids, 10), "CapEff": "0000000000000000", "NoNewPrivs": "1"}
	for _, name := range images[0].Layers {
		layer := small[name]
		if len(layer) > 2 && layer[0] == 0x1f && layer[1] == 0x8b {
			gz, err := gzip.NewReader(bytes.NewReader(layer))
			if err != nil {
				return nil, errors.New("image layer invalid")
			}
			layer, err = io.ReadAll(io.LimitReader(gz, 32<<20+1))
			gz.Close()
			if err != nil || len(layer) > 32<<20 {
				return nil, errors.New("image layer invalid")
			}
		}
		lt := tar.NewReader(bytes.NewReader(layer))
		for {
			e, err := lt.Next()
			if err != nil {
				break
			}
			if strings.TrimPrefix(e.Name, "./") != "proof" || e.Size > 4096 {
				continue
			}
			v, _ := io.ReadAll(io.LimitReader(lt, e.Size))
			var got map[string]string
			if json.Unmarshal(v, &got) != nil || len(got) != len(want) {
				return nil, errKernelProof
			}
			for k, w := range want {
				if got[k] != w {
					return nil, errKernelProof
				}
			}
			return got, nil
		}
	}
	return nil, errKernelProof
}

// dispose runs after every outcome, with a context that survives caller
// cancellation. The builder is stopped only after the Docker client has drained
// all I/O; absence is confirmed independently of any remove command's exit code.
func (b *builderRun) dispose() {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(b.parent), 5*time.Minute)
	defer cancel()
	rec := &b.rec
	clean := true
	if rec.CreateAttempted {
		ids, err := b.containersByNonce(ctx)
		switch {
		case err != nil || len(ids) > 1:
			clean = false
		case len(ids) == 1:
			clean = b.disposeContainer(ctx, ids[0])
		}
		after, err := b.containersByNonce(ctx)
		rec.ContainerAbsent = err == nil && len(after) == 0
		rec.VolumesAbsent = true
		for _, v := range rec.VolumeNames {
			out, _, err := dockerText(ctx, b.d, 1<<20, "volume", "ls", "--quiet")
			if err != nil || containsLine(out, v) {
				rec.VolumesAbsent = false
			}
		}
		clean = clean && rec.ContainerAbsent && rec.VolumesAbsent
	} else {
		rec.ContainerAbsent, rec.VolumesAbsent = true, true
	}
	if rec.ToolImageIntroduced {
		present, err := b.imagePresent(ctx)
		if err == nil && present {
			_, _, _ = dockerText(ctx, b.d, 1<<16, "image", "rm", releaseBuilderTool)
			present, err = b.imagePresent(ctx)
		}
		rec.ToolImageRemoved = err == nil && !present
		clean = clean && rec.ToolImageRemoved
	}
	rec.PhaseMayRelease = clean
}

func (b *builderRun) disposeContainer(ctx context.Context, id string) bool {
	rec := &b.rec
	info, err := b.inspect(ctx, id)
	if err != nil || !b.ownedIdentity(info) || info.ID != id {
		return false // Never touch a resource that is not exactly ours.
	}
	rec.ContainerID = id
	if info.State.Running {
		_, _, _ = dockerText(ctx, b.d, 4096, "container", "stop", "--time", "10", id)
		if info, err = b.inspect(ctx, id); err != nil {
			return false
		}
		if info.State.Running {
			_, _, _ = dockerText(ctx, b.d, 4096, "container", "kill", id)
			if info, err = b.inspect(ctx, id); err != nil {
				return false
			}
		}
	}
	if info.State.Running || info.State.Pid != 0 {
		return false
	}
	rec.PIDZero = true
	if info.State.Status == "exited" {
		rec.ExitCode, rec.OOMKilled = info.State.ExitCode, info.State.OOMKilled
	}
	for _, m := range info.Mounts {
		if m.Type == "volume" && releaseVolumePattern.MatchString(m.Name) {
			seen := false
			for _, v := range rec.VolumeNames {
				seen = seen || v == m.Name
			}
			if !seen {
				rec.VolumeNames = append(rec.VolumeNames, m.Name)
			}
		}
	}
	_ = b.persist()
	if _, _, err := dockerText(ctx, b.d, 4096, "container", "rm", "--volumes", id); err != nil {
		return false
	}
	rec.ContainerRemoved = true
	return true
}

func (b *builderRun) finish() (releaseBuildReceipt, error) {
	rec := b.rec
	if rec.Failure == "phase-busy" || rec.Failure == "phase-uncertain" {
		rec.PhaseMayRelease = false // Somebody else's builder may exist; keep the exclusion.
	}
	rec.RecoveryRequired = !rec.PhaseMayRelease
	if rec.Failure == "" && rec.RecoveryRequired {
		rec.Failure = "recovery-required"
	}
	if rec.Failure == "" {
		rec.Succeeded = true
		rec.artifact = b.artifact
	}
	b.rec = rec
	_ = b.persist()
	if rec.Failure != "" {
		if rec.RecoveryRequired && rec.Failure != "recovery-required" {
			return rec, releaseBuildError(rec.Failure + "; recovery-required")
		}
		return rec, releaseBuildError(rec.Failure)
	}
	return rec, nil
}

func containsLine(text, want string) bool {
	for _, line := range strings.Fields(text) {
		if line == want {
			return true
		}
	}
	return false
}
