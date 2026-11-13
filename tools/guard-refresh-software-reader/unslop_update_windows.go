//go:build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

const unslopSourceHead = "a3b5728e4548d6aa454a238df5f8e65c03522ed5"
const unslopMergedHead = "b5859dc0ba8347f19f5c612ec7c5299a7513e77c"
const unslopCandidateSHA = "7897a2aadea70d239ea373cb4a7bd165d2aef357362236ddc05be8620acdfa0d"
const unslopInstalledSHA = "1cc4265103d76d833bebfdf36f8d88bec39ad22043507ce6a1e8cce1ad2072f2d"
const unslopOriginID = "godd-unslop-runtime-warning-release-v1"
const unslopSourceRoot = "C:/Users/makhmeto/Documents/Codex/2026-11-07/https-github-com-autodevjapan-godd-unslop/work/GoDD-Unslop"
const unslopCandidatePath = unslopSourceRoot + "/target/release/godd-unslop.exe"
const unslopInstalledPath = "C:/Users/makhmeto/.cargo/bin/godd-unslop.exe"
const unslopStagePath = "C:/Users/makhmeto/.cargo/bin/godd-unslop.runtime-warning.stage.exe"
const unslopBackupPath = "C:/Users/makhmeto/.cargo/bin/godd-unslop.runtime-warning.before.exe"

var errUnslopOrigin = errors.New("unslop_origin_provider_unavailable")

// Supplied only by the normal producer evidence provider, not CLI arguments,
// an environment value, a caller JSON proof, or a resource-guard override.
type unslopReleaseOrigin struct {
	ID, Source, Merge, CandidateSHA, InstalledSHA                   string
	ProducerRecord, ProducerResult, BuildEvidenceSHA, AcceptanceSHA string
	ProducerExit, CLIExit, MCPExit                                  int
	CandidateIdentity, InstalledIdentity                            softwareIdentity
	SourceIdentity                                                  string
	CompleteCompiledInputs                                          bool
}

type unslopOriginProvider interface {
	Release(context.Context) (unslopReleaseOrigin, error)
	Packet(context.Context) (unslopPacketOrigin, error)
}

type unslopUpdateAccess struct {
	origin    unslopOriginProvider
	open      func(string) (updateObject, error)
	source    func(context.Context, unslopReleaseOrigin) bool
	absent    func(string) bool
	candidate func(softwareIdentity, func([]byte, softwareIdentity) bool) bool
}

type unslopReleasePlan struct {
	source, merge, candidateSHA, installedSHA                       string
	producerRecord, producerResult, buildEvidenceSHA, acceptanceSHA string
}

type unslopUpdateResult struct {
	updateResult
	CompleteCompiledInputs bool   `json:"complete_compiled_inputs"`
	Origin                 string `json:"origin,omitempty"`
}

// A fixed old digest is retained as historical evidence only. The shared
// dispatcher must use a newly captured producer for an installable artifact.
func executeUnslopUpdate(ctx context.Context, prepare bool, access unslopUpdateAccess) unslopUpdateResult {
	// This old generation predates input capture. A caller cannot promote its
	// known digest, text log, or historical success into a new producer receipt.
	return unslopUpdateResult{updateResult: updateResult{Reason: "missing_compiled_input_provenance"}}
}

func executeUnslopReleasePlan(ctx context.Context, prepare bool, plan unslopReleasePlan, access unslopUpdateAccess) unslopUpdateResult {
	result := unslopUpdateResult{updateResult: updateResult{Reason: "unslop_origin_provider_unavailable"}}
	if ctx.Err() != nil {
		result.Reason = "unslop_cancelled"
		return result
	}
	if access.origin == nil {
		return result
	}
	proof, err := access.origin.Release(ctx)
	if err != nil {
		return result
	}
	if !proof.CompleteCompiledInputs {
		result.Reason = "missing_compiled_input_provenance"
		return result
	}
	validID := func(id softwareIdentity) bool { return id.Volume != "" && id.FileID != "" && id.Created != "" }
	if proof.ID != unslopOriginID || proof.Source != plan.source || proof.Merge != plan.merge || proof.CandidateSHA != plan.candidateSHA || proof.InstalledSHA != plan.installedSHA ||
		proof.ProducerRecord != plan.producerRecord || proof.ProducerResult != plan.producerResult || proof.BuildEvidenceSHA != plan.buildEvidenceSHA || proof.AcceptanceSHA != plan.acceptanceSHA ||
		proof.ProducerExit != 0 || proof.CLIExit != 0 || proof.MCPExit != 0 || proof.SourceIdentity == "" || !validID(proof.CandidateIdentity) || !validID(proof.InstalledIdentity) {
		result.Reason = "unslop_producer_provenance_mismatch"
		return result
	}
	if access.source == nil || !access.source(ctx, proof) || ctx.Err() != nil {
		result.Reason = "unslop_source_unqualified"
		return result
	}
	if access.candidate == nil || access.open == nil || access.absent == nil {
		result.Reason = "unslop_consumer_unavailable"
		return result
	}
	// The candidate pin owns its buffer for the whole callback. Never reopen
	// that pathname between checking its digest and preparing the same bytes.
	result.Reason = "unslop_candidate_unqualified"
	callbacks := 0
	pinned := access.candidate(proof.CandidateIdentity, func(raw []byte, id softwareIdentity) bool {
		callbacks++
		if callbacks != 1 {
			return false
		}
		if ctx.Err() != nil || id != proof.CandidateIdentity || len(raw) == 0 || digest(raw) != plan.candidateSHA {
			return false
		}
		old, err := access.open(unslopInstalledPath)
		if err != nil {
			if old != nil {
				_ = old.Close()
			}
			result.Reason = "unslop_installed_unqualified"
			return false
		}
		defer func() {
			if old.Close() != nil {
				result.Qualified = false
				result.Prepared = false
				result.Reason = "unslop_close_failed"
			}
		}()
		if old.ID() != proof.InstalledIdentity || digest(old.Raw()) != plan.installedSHA {
			result.Reason = "unslop_installed_mismatch"
			return false
		}
		if !access.absent(unslopStagePath) || !access.absent(unslopBackupPath) {
			result.Reason = "software_leaf_collision"
			return false
		}
		acl, err := old.ACL()
		if err != nil || acl == "" || ctx.Err() != nil {
			result.Reason = "unslop_acl_or_context_unqualified"
			return false
		}
		result.Qualified = true
		result.Reason = "verified_no_write"
		result.Domain = "godd-unslop-fixed-software-prepare-v1"
		result.Origin = proof.ID
		result.Target = unslopInstalledPath
		result.Stage = unslopStagePath
		result.Backup = unslopBackupPath
		result.CurrentSHA = plan.installedSHA
		result.CandidateSHA = plan.candidateSHA
		oldID := old.ID()
		result.CurrentIdentity = &oldID
		result.ACLHash = digest([]byte(acl))
		result.SourceHead = plan.source
		result.SourceMerged = plan.merge
		result.CompleteCompiledInputs = true
		if !prepare {
			return true
		}
		for _, item := range []struct {
			path     string
			raw      []byte
			identity **softwareIdentity
		}{{unslopBackupPath, old.Raw(), &result.BackupIdentity}, {unslopStagePath, raw, &result.StageIdentity}} {
			if ctx.Err() != nil {
				result.Qualified = false
				result.Reason = "prepare_incomplete_preserved"
				return false
			}
			id, err := old.CreateNew(item.path, item.raw, acl)
			if id.FileID != "" {
				result.Created = append(result.Created, item.path)
				*item.identity = &id
			}
			if err != nil {
				result.Qualified = false
				result.Reason = "prepare_incomplete_preserved"
				return false
			}
		}
		if ctx.Err() != nil {
			result.Qualified = false
			result.Reason = "prepare_incomplete_preserved"
			return false
		}
		result.Prepared = true
		result.Reason = "prepared_two_fixed_software_leaves"
		return true
	})
	if !pinned || callbacks != 1 {
		result.Qualified = false
		result.Prepared = false
		if len(result.Created) > 0 {
			result.Reason = "prepare_incomplete_preserved"
		} else if result.Reason == "verified_no_write" {
			result.Reason = "unslop_candidate_pin_failed"
		}
	}
	if ctx.Err() != nil {
		result.Qualified = false
		result.Prepared = false
		if len(result.Created) > 0 {
			result.Reason = "prepare_incomplete_preserved"
		} else {
			result.Reason = "unslop_cancelled"
		}
	}
	return result
}

const unslopCaptureSource = "0c12029d1a383f406bd0bdbf87d40bc2af1b1279"
const unslopCaptureTree = "b1888c01a2710fd4a47c88f171e08139da908fde"
const unslopCaptureDomain = "godd-unslop-captured-release-v1"
const unslopCaptureRoot = unslopSourceRoot + "/target/unslop-captured-release-v1"
const unslopCaptureOutput = unslopCaptureRoot + "/cargo/release/godd-unslop.exe"

func unslopCaptureSourceBlobs() map[string]string {
	return map[string]string{
		"Cargo.toml":                             "1066eed16fb9ca324f4359b8c7a09d6e570b3d4c",
		"Cargo.lock":                             "6837c52b654881ab1e8a32486631a3434acd4738",
		"src/lib.rs":                             "234cb4f5d804e9ece0bb1c59ffdec05e8a6cea10",
		"src/main.rs":                            "dae4d56981b05c4511f39e4638ba34cfdedc5881",
		"src/relationships.rs":                   "3337013281256bc3dc6bc1913550218bd77de1cb",
		"src/rewrite.rs":                         "c47aabc72ec5804712f06564de71de7c104c7141",
		"src/rpc.rs":                             "7aab0b15b283e2ad727d4a7e5ec96903cdd3453d",
		"src/skill.rs":                           "e61886a7eec4cf565671689e607b9634e4524de2",
		"src/style.rs":                           "716bf2ba1bade500dc74c00c0dd5baf3b4d012bd",
		"docs/distribution/godd-unslop-SKILL.md": "697fd9503e9800d568f7062dfd6decb34fe9ef9b",
	}
}

// Admission is made by the normal source/snapshot controller before Build.
// These types are internal integration boundaries, never JSON/CLI proof input.
type unslopCapturedInput struct {
	Path, Blob, SHA string
	Identity        softwareIdentity
}

type unslopCapturedInputs struct {
	Source, Merge, Tree, Namespace, ConfigurationSHA, ToolchainSHA, LockSHA string
	NamespaceIdentity                                                       softwareIdentity
	Sources, Dependencies, Configuration                                    []unslopCapturedInput
}

type unslopBuildInvocation struct {
	CommandSHA, CargoJSONSHA                          string
	CargoStreamSHA                                    string
	CargoStream                                       []byte
	ToolCallID                                        *string
	NativeProcess                                     *unslopProcessIdentity
	Started, Assigned, WaitVerified, Reaped, TimedOut bool
	Exit                                              int
	CargoJSON                                         []byte
}

type unslopProcessIdentity struct {
	PID     uint32
	Created uint64
}

type unslopProducerSession interface {
	Before() unslopCapturedInputs
	Build(context.Context) (unslopBuildInvocation, error)
	After(context.Context) (unslopCapturedInputs, error)
	WithArtifact(func([]byte, softwareIdentity) bool) bool
	Acknowledge(context.Context) error
	Close() error
}

type unslopCaptureReceipt struct {
	Domain, Source, InstalledTarget, Namespace, InputManifestSHA string
	CandidateSHA, CargoJSONSHA, CommandSHA                       string
	CargoStreamSHA                                               string
	ToolCallID                                                   *string
	NativeProcess                                                *unslopProcessIdentity
	CandidateIdentity                                            softwareIdentity
	Bytes                                                        int
	CompleteCompiledInputs, ProducerAcknowledged, ProducerReaped bool
}

type unslopCaptureAccess struct {
	// The shared dispatcher binds its native producer, never a caller factory.
	begin func(context.Context) (unslopProducerSession, error)
	// Stage consumes the same buffer into a private CreateNew pending export.
	// Its normal controller must re-pin that export in Commit before making
	// the receipt final. No final receipt exists inside the artifact callback.
	stage func(context.Context, unslopCaptureReceipt, []byte) (unslopPendingCapture, error)
}

type unslopPendingCapture interface {
	Commit(context.Context) error
	Abort() error
}

type unslopCaptureResult struct {
	Qualified         bool                  `json:"qualified"`
	Reason            string                `json:"reason"`
	Receipt           *unslopCaptureReceipt `json:"receipt,omitempty"`
	DataAccessGranted bool                  `json:"data_access_granted"`
	UpdatePerformed   bool                  `json:"update_performed"`
}

func unslopCaptureCommand() []string {
	args := []string{"build", "--locked", "--offline", "--release", "--message-format=json", "--manifest-path", unslopCaptureRoot + "/source/Cargo.toml", "--target-dir", unslopCaptureRoot + "/cargo", "--bin", "godd-unslop"}
	return append(args, cargoVendorBuildArgs(unslopCaptureRoot+"/source")...)
}

func captureUnslopRelease(ctx context.Context, access unslopCaptureAccess) (result unslopCaptureResult) {
	result.Reason = "missing_producer_provenance"
	if ctx.Err() != nil {
		result.Reason = "unslop_cancelled"
		return
	}
	if access.begin == nil || access.stage == nil {
		return
	}
	session, err := access.begin(ctx)
	if err != nil || session == nil {
		return
	}
	closed := false
	defer func() {
		if !closed && session.Close() != nil {
			result.Qualified = false
			result.Receipt = nil
			result.Reason = "capture_cleanup_failed_preserved"
		}
	}()
	before := session.Before()
	manifest, valid := unslopCapturedManifest(before)
	if !valid {
		result.Reason = "capture_inputs_unqualified_before_build"
		return
	}
	if ctx.Err() != nil {
		result.Reason = "unslop_cancelled"
		return
	}
	built, err := session.Build(ctx)
	defer clear(built.CargoJSON)
	defer clear(built.CargoStream)
	command, _ := json.Marshal(unslopCaptureCommand())
	processBound := built.NativeProcess != nil && built.NativeProcess.PID != 0 && built.NativeProcess.Created != 0
	if err != nil || ctx.Err() != nil || !processBound || built.ToolCallID != nil || !unslopHex(built.CargoJSONSHA, 64) || !unslopHex(built.CargoStreamSHA, 64) || built.CommandSHA != digest(command) || !built.Started || !built.Assigned || !built.WaitVerified || !built.Reaped || built.TimedOut || built.Exit != 0 {
		result.Reason = "capture_producer_failed_or_uncollected"
		return
	}
	projection, projected := unslopCargoProjection(built.CargoStream)
	defer clear(projection)
	if digest(built.CargoJSON) != built.CargoJSONSHA || digest(built.CargoStream) != built.CargoStreamSHA || !projected || !bytes.Equal(projection, built.CargoJSON) {
		result.Reason = "capture_cargo_evidence_unqualified"
		return
	}
	after, err := session.After(ctx)
	afterManifest, valid := unslopCapturedManifest(after)
	if err != nil || !valid || manifest != afterManifest || ctx.Err() != nil {
		result.Reason = "capture_inputs_changed_after_build"
		return
	}
	var pending unslopPendingCapture
	committed := false
	defer func() {
		if pending != nil && !committed && pending.Abort() != nil {
			result.Reason = "capture_pending_preserved"
		}
	}()
	var receipt unslopCaptureReceipt
	callbacks := 0
	result.Reason = "capture_artifact_unqualified"
	pinned := session.WithArtifact(func(raw []byte, id softwareIdentity) bool {
		callbacks++
		if callbacks != 1 || ctx.Err() != nil || len(raw) == 0 || len(raw) > 32<<20 || !unslopCompleteIdentity(id) {
			return false
		}
		if session.Acknowledge(ctx) != nil || ctx.Err() != nil {
			result.Reason = "capture_ack_failed"
			return false
		}
		process := *built.NativeProcess
		receipt = unslopCaptureReceipt{Domain: unslopCaptureDomain, Source: unslopCaptureSource, InstalledTarget: unslopInstalledPath, Namespace: unslopCaptureRoot, InputManifestSHA: manifest, CandidateSHA: digest(raw), CargoJSONSHA: built.CargoJSONSHA, ToolCallID: built.ToolCallID, NativeProcess: &process, CommandSHA: built.CommandSHA, CandidateIdentity: id, Bytes: len(raw), CompleteCompiledInputs: true, ProducerAcknowledged: true, ProducerReaped: true}
		receipt.CargoStreamSHA = built.CargoStreamSHA
		var e error
		pending, e = access.stage(ctx, receipt, raw)
		if e != nil || pending == nil {
			result.Reason = "capture_pending_failed_preserved"
			return false
		}
		return true
	})
	if !pinned || callbacks != 1 || pending == nil || ctx.Err() != nil {
		return
	}
	closed = true
	if session.Close() != nil {
		result.Reason = "capture_cleanup_failed_preserved"
		return
	}
	if ctx.Err() != nil {
		result.Reason = "unslop_cancelled"
		return
	}
	// Commit rechecks the held export and publishes only after successful ACK,
	// producer collection, original-input comparison, and snapshot cleanup.
	if pending.Commit(ctx) != nil {
		result.Reason = "capture_receipt_failed_preserved"
		return
	}
	committed = true
	result.Qualified = true
	result.Reason = "captured_release_receipt_published"
	result.Receipt = &receipt
	return
}

func unslopCargoProjection(raw []byte) ([]byte, bool) {
	if !unslopCargoOutput(raw) {
		return nil, false
	}
	artifact, _ := json.Marshal(map[string]any{"reason": "compiler-artifact", "target": map[string]any{"name": "godd-unslop", "kind": []string{"bin"}}, "executable": unslopCaptureOutput, "fresh": false})
	return append(artifact, []byte("\n{\"reason\":\"build-finished\",\"success\":true}\n")...), true
}

func unslopCapturedManifest(inputs unslopCapturedInputs) (string, bool) {
	expectedSources := unslopCaptureSourceBlobs()
	if inputs.Source != unslopCaptureSource || inputs.Merge != unslopCaptureSource || inputs.Tree != unslopCaptureTree || inputs.Namespace != unslopCaptureRoot || !unslopCompleteIdentity(inputs.NamespaceIdentity) || !unslopHex(inputs.ConfigurationSHA, 64) || !unslopHex(inputs.ToolchainSHA, 64) || !unslopHex(inputs.LockSHA, 64) || len(inputs.Sources) != len(expectedSources) || len(inputs.Dependencies) == 0 || len(inputs.Configuration) == 0 {
		return "", false
	}
	seen := map[string]bool{}
	ids := map[softwareIdentity]bool{}
	groups := []struct {
		files []unslopCapturedInput
		kind  string
	}{{inputs.Sources, "source"}, {inputs.Dependencies, "dependency"}, {inputs.Configuration, "configuration"}}
	canonical := inputs
	for n, g := range groups {
		files := append([]unslopCapturedInput(nil), g.files...)
		if len(files) > 20000 {
			return "", false
		}
		for _, f := range files {
			p := f.Path
			if !vendorRelativePath(p) || seen[strings.ToLower(p)] || !unslopHex(f.SHA, 64) || !unslopCompleteIdentity(f.Identity) || ids[f.Identity] {
				return "", false
			}
			if g.kind == "source" {
				if expectedSources[p] == "" || expectedSources[p] != f.Blob {
					return "", false
				}
			} else if g.kind == "dependency" {
				if f.Blob != "" || !strings.HasPrefix(p, "vendor/") {
					return "", false
				}
			} else if f.Blob != "" || !strings.HasPrefix(p, "cargo-home/") {
				return "", false
			}
			seen[strings.ToLower(p)] = true
			ids[f.Identity] = true
		}
		sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
		switch n {
		case 0:
			canonical.Sources = files
		case 1:
			canonical.Dependencies = files
		case 2:
			canonical.Configuration = files
		}
	}
	for _, p := range []string{"Cargo.toml", "Cargo.lock", "src/main.rs", "src/skill.rs", "docs/distribution/godd-unslop-SKILL.md", "cargo-home/config.toml"} {
		if !seen[strings.ToLower(p)] {
			return "", false
		}
	}
	raw, err := json.Marshal(canonical)
	return digest(raw), err == nil
}

func unslopCompleteIdentity(id softwareIdentity) bool {
	return id.Volume != "" && id.FileID != "" && id.Created != ""
}
func unslopHex(value string, n int) bool {
	if len(value) != n {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func unslopCargoOutput(raw []byte) bool {
	if len(raw) == 0 || len(raw) > 8<<20 {
		return false
	}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	artifacts, finished := 0, false
	for scanner.Scan() {
		if finished {
			return false
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
		if json.Unmarshal(scanner.Bytes(), &item) != nil || item.Reason == "" {
			return false
		}
		if item.Reason == "build-finished" {
			if item.Success == nil || !*item.Success || artifacts != 1 {
				return false
			}
			finished = true
		}
		if item.Reason == "compiler-artifact" && item.Target.Name == "godd-unslop" {
			isBin := false
			for _, kind := range item.Target.Kind {
				isBin = isBin || kind == "bin"
			}
			if isBin {
				if item.Executable == nil || !sameCleanPath(*item.Executable, unslopCaptureOutput) || item.Fresh == nil || *item.Fresh {
					return false
				}
				artifacts++
			}
		}
	}
	return scanner.Err() == nil && finished && artifacts == 1
}
