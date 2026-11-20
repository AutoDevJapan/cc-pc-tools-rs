//go:build windows

package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
)

// No caller chooses a command or output. The normal producer must own the
// existing Cargo gate and collect its one command as a complete owned job.
var watchdogBuildArgs = []string{
	"build", "--release", "--locked", "--manifest-path",
	"cc-ocx-watchdog/Cargo.toml", "--bin", "ocx_health_watchdog",
	"--message-format=json",
}

type watchdogSourceSnapshot struct {
	Commit string
	Tree string
	CrateTree string
	WorkspaceManifestBlob string
	WorkspaceLockBlob string
	CompiledInputsSHA256 string
	ConfigurationSHA256 string
	ToolchainSHA256 string
}

func (s watchdogSourceSnapshot) valid() bool {
	return s.Commit == watchdogApprovedCommit &&
		s.Tree == "d980b091aa58e7aef75cba040e9f852ad1e942fc" &&
		s.CrateTree == watchdogApprovedCrate &&
		s.WorkspaceManifestBlob == watchdogCargoManifest &&
		s.WorkspaceLockBlob == watchdogCargoLock &&
		watchdogHex(s.CompiledInputsSHA256, 64) &&
		watchdogHex(s.ConfigurationSHA256, 64) &&
		watchdogHex(s.ToolchainSHA256, 64)
}

type watchdogBuildOutput struct {
	ExitCode int
	ChildReaped bool
	StdoutComplete bool
	StderrComplete bool
	CargoJSON []byte
	Artifact watchdogBuildArtifact
}

type watchdogBuildArtifact interface {
	Identity(context.Context) (watchdogFileIdentity, error)
	WithHeld(context.Context, func(io.ReadSeeker) error) error
	Close() error
}

type watchdogBuildLease interface {
	// One-shot even after Build failure; the existing producer reservation
	// prevents a retry from starting a second formal build.
	Claim(context.Context) error
	Before(context.Context) (watchdogSourceSnapshot, error)
	Build(context.Context, []string) (*watchdogBuildOutput, error)
	After(context.Context) (watchdogSourceSnapshot, error)
	Recheck(context.Context) error
	Close() error
}

type watchdogBuildOpener func(context.Context) (watchdogBuildLease, error)

type watchdogBuildProduct struct {
	Source watchdogSourceSnapshot
	Artifact watchdogBuildArtifact
	Identity watchdogFileIdentity
	CargoJSONSHA256 string
	lease watchdogBuildLease
	closed bool
	closeErr error
}

func (p *watchdogBuildProduct) Close() error {
	if p == nil {
		return nil
	}
	if p.closed {
		return p.closeErr
	}
	p.closed = true
	var es []error
	if p.Artifact != nil {
		es = append(es, p.Artifact.Close())
	}
	if p.lease != nil {
		es = append(es, p.lease.Close())
	}
	p.closeErr = errors.Join(es...)
	return p.closeErr
}

// Exactly one nonfresh bin and a final successful build-finished are required.
// The executable string must match the producer's held artifact binding; it
// never authorizes opening a path.
func watchdogCargoEvidence(raw []byte, executable string) error {
	if len(raw) == 0 || len(raw) > 8<<20 || executable == "" {
		return errors.New("watchdog_cargo_output_incomplete")
	}
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 65536), 1<<20)
	binaries, finishes := 0, 0
	for scanner.Scan() {
		var message struct {
			Reason string
			Success bool
			Fresh *bool
			Executable *string
			Target struct {
				Name string
				Kind []string
				CrateTypes []string `json:"crate_types"`
			}
		}
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			return errors.New("watchdog_cargo_json_invalid")
		}
		if finishes != 0 {
			return errors.New("watchdog_cargo_after_finished")
		}
		if message.Reason == "compiler-artifact" && message.Executable != nil {
			binaries++
			if message.Target.Name != "ocx_health_watchdog" ||
				len(message.Target.Kind) != 1 || message.Target.Kind[0] != "bin" ||
				len(message.Target.CrateTypes) != 1 || message.Target.CrateTypes[0] != "bin" ||
				message.Fresh == nil || *message.Fresh || *message.Executable != executable {
				return errors.New("watchdog_cargo_binary_mismatch")
			}
		}
		if message.Reason == "build-finished" {
			finishes++
			if !message.Success {
				return errors.New("watchdog_cargo_build_failed")
			}
		}
	}
	if scanner.Err() != nil || finishes != 1 || binaries != 1 {
		return errors.New("watchdog_cargo_output_incomplete")
	}
	return nil
}

// PE checks and digest consume the same held stream. No executable runs here.
func watchdogPEAndHash(r io.ReadSeeker, expected watchdogFileIdentity) error {
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return err
	}
	header := make([]byte, 64)
	if _, err := io.ReadFull(r, header); err != nil || header[0] != 'M' || header[1] != 'Z' {
		return errors.New("watchdog_pe_invalid")
	}
	offset := int64(uint32(header[60]) | uint32(header[61])<<8 | uint32(header[62])<<16 | uint32(header[63])<<24)
	if offset < 64 || offset > expected.Bytes-96 {
		return errors.New("watchdog_pe_offset_invalid")
	}
	if _, err := r.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	pe := make([]byte, 96)
	if _, err := io.ReadFull(r, pe); err != nil ||
		string(pe[:4]) != "PE\x00\x00" || pe[4] != 0x64 || pe[5] != 0x86 ||
		pe[24] != 0x0b || pe[25] != 0x02 || pe[92] != 2 || pe[93] != 0 {
		return errors.New("watchdog_pe_machine_or_subsystem")
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(r, expected.Bytes+1))
	if err != nil || n != expected.Bytes ||
		hex.EncodeToString(h.Sum(nil)) != expected.SHA256 {
		return errors.New("watchdog_pe_bytes_changed")
	}
	return nil
}

// artifactPath comes only from the producer's same-handle provenance. It is
// never taken from command-line JSON, Cargo output or an arbitrary hash.
func produceWatchdogBuild(ctx context.Context, open watchdogBuildOpener, artifactPath string) (product *watchdogBuildProduct, err error) {
	if ctx == nil || open == nil || ctx.Err() != nil {
		return nil, errors.New("watchdog_build_context_invalid")
	}
	lease, err := open(ctx)
	var output *watchdogBuildOutput
	committed := false
	defer func() {
		if committed {
			return
		}
		partial := &watchdogBuildProduct{lease: lease}
		if output != nil {
			partial.Artifact = output.Artifact
		}
		if cleanup := partial.Close(); cleanup != nil {
			err = errors.Join(err, cleanup)
			product = partial // retained cleanup failure, never a valid product
		}
	}()
	if err != nil || lease == nil {
		return nil, errors.Join(errors.New("watchdog_build_issuer_unavailable"), err)
	}
	before, err := lease.Before(ctx)
	if err != nil || !before.valid() {
		return nil, errors.New("watchdog_build_source_unbound")
	}
	if ctx.Err() != nil || lease.Recheck(ctx) != nil {
		return nil, errors.New("watchdog_build_generation_changed")
	}
	if err = lease.Claim(ctx); err != nil {
		return nil, errors.New("watchdog_build_already_claimed")
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	output, err = lease.Build(ctx, append([]string(nil), watchdogBuildArgs...))
	if err != nil || output == nil {
		return nil, errors.Join(errors.New("watchdog_build_failed"), err)
	}
	if ctx.Err() != nil || output.ExitCode != 0 || !output.ChildReaped ||
		!output.StdoutComplete || !output.StderrComplete || output.Artifact == nil {
		return nil, errors.New("watchdog_build_not_collected")
	}
	after, err := lease.After(ctx)
	if err != nil || before != after || lease.Recheck(ctx) != nil {
		return nil, errors.New("watchdog_build_inputs_changed")
	}
	if err = watchdogCargoEvidence(output.CargoJSON, artifactPath); err != nil {
		return nil, err
	}
	identity, err := output.Artifact.Identity(ctx)
	if err != nil || !identity.valid() {
		return nil, errors.New("watchdog_build_artifact_identity")
	}
	calls := 0
	var callbackErr error
	var callbackMu sync.Mutex
	callbackClosed := false
	err = output.Artifact.WithHeld(ctx, func(body io.ReadSeeker) error {
		callbackMu.Lock()
		defer callbackMu.Unlock()
		if callbackClosed {
			return errors.New("watchdog_build_late_callback")
		}
		calls++
		if calls != 1 || body == nil {
			callbackErr = errors.Join(callbackErr, errors.New("watchdog_build_held_callback"))
			return callbackErr
		}
		callbackErr = watchdogPEAndHash(body, identity)
		return callbackErr
	})
	callbackMu.Lock()
	callbackClosed = true
	callbackMu.Unlock()
	if err != nil || callbackErr != nil || calls != 1 || ctx.Err() != nil {
		return nil, errors.Join(errors.New("watchdog_build_held_artifact_failed"), err, callbackErr)
	}
	current, err := output.Artifact.Identity(ctx)
	if err != nil || current != identity || lease.Recheck(ctx) != nil || ctx.Err() != nil {
		return nil, errors.New("watchdog_build_final_identity_changed")
	}
	digest := sha256.Sum256(output.CargoJSON)
	product = &watchdogBuildProduct{
		Source: before, Artifact: output.Artifact, Identity: identity,
		CargoJSONSHA256: hex.EncodeToString(digest[:]), lease: lease,
	}
	committed = true
	return product, nil
}

func openNormalWatchdogBuild(context.Context) (watchdogBuildLease, error) {
	return nil, errors.New("watchdog_build_issuer_unavailable")
}
