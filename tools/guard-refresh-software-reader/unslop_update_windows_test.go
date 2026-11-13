//go:build windows

package main

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
)

type unslopTestProvider struct {
	release unslopReleaseOrigin
	packet  unslopPacketOrigin
	err     error
}

type unslopSyntheticCapture struct {
	before, after               unslopCapturedInputs
	build                       unslopBuildInvocation
	raw                         []byte
	id                          softwareIdentity
	fault                       string
	builds, artifactReads, acks int
	closed                      bool
	cancelBefore                func()
}

type syntheticUnslopPending struct {
	commits, aborts int
	fault           string
}

func (p *syntheticUnslopPending) Commit(context.Context) error {
	p.commits++
	if p.fault == "commit_failed" {
		return errors.New("synthetic commit failed")
	}
	return nil
}
func (p *syntheticUnslopPending) Abort() error { p.aborts++; return nil }

func (s *unslopSyntheticCapture) Before() unslopCapturedInputs {
	if s.cancelBefore != nil {
		s.cancelBefore()
	}
	return s.before
}
func (s *unslopSyntheticCapture) Build(context.Context) (unslopBuildInvocation, error) {
	s.builds++
	return s.build, nil
}
func (s *unslopSyntheticCapture) After(context.Context) (unslopCapturedInputs, error) {
	return s.after, nil
}
func (s *unslopSyntheticCapture) WithArtifact(use func([]byte, softwareIdentity) bool) bool {
	s.artifactReads++
	if s.fault == "pin_skipped_callback" {
		return true
	}
	accepted := use(s.raw, s.id)
	if s.fault == "pin_repeated_callback" {
		return use(s.raw, s.id)
	}
	return accepted && s.fault != "pin_final_failed"
}
func (s *unslopSyntheticCapture) Acknowledge(context.Context) error {
	s.acks++
	if s.fault == "ack_failed" {
		return errors.New("synthetic ack failed")
	}
	return nil
}
func (s *unslopSyntheticCapture) Close() error {
	s.closed = true
	if s.fault == "close_failed" {
		return errors.New("synthetic cleanup failed")
	}
	return nil
}

func syntheticUnslopCapture() *unslopSyntheticCapture {
	input := func(path string, blob bool) unslopCapturedInput {
		b := ""
		if blob {
			b = unslopCaptureSourceBlobs()[path]
		}
		return unslopCapturedInput{Path: path, Blob: b, SHA: digest([]byte(path)), Identity: softwareIdentity{"synthetic-volume", path, "synthetic-created"}}
	}
	var sources []unslopCapturedInput
	for path := range unslopCaptureSourceBlobs() {
		sources = append(sources, input(path, true))
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	before := unslopCapturedInputs{Source: unslopCaptureSource, Merge: unslopCaptureSource, Tree: unslopCaptureTree, Namespace: unslopCaptureRoot, NamespaceIdentity: softwareIdentity{"v", "synthetic-new-directory", "created"}, ConfigurationSHA: digest([]byte("synthetic-config")), ToolchainSHA: digest([]byte("synthetic-toolchain")), LockSHA: digest([]byte("synthetic-lock")), Sources: sources, Dependencies: []unslopCapturedInput{input("vendor/example-1.0.0/src/lib.rs", false)}, Configuration: []unslopCapturedInput{input("cargo-home/config.toml", false)}}
	encoded, _ := json.Marshal(before)
	var after unslopCapturedInputs
	_ = json.Unmarshal(encoded, &after)
	cmd, _ := json.Marshal(unslopCaptureCommand())
	output := map[string]any{"reason": "compiler-artifact", "target": map[string]any{"name": "godd-unslop", "kind": []string{"bin"}}, "executable": unslopCaptureOutput, "fresh": false}
	b, _ := json.Marshal(output)
	b = append(b, []byte("\n{\"reason\":\"build-finished\",\"success\":true}\n")...)
	return &unslopSyntheticCapture{before: before, after: after, raw: []byte("synthetic new exe"), id: softwareIdentity{"v", "new exe", "created"}, build: unslopBuildInvocation{NativeProcess: &unslopProcessIdentity{PID: 1, Created: 1}, CommandSHA: digest(cmd), CargoJSONSHA: digest(b), Started: true, Assigned: true, WaitVerified: true, Reaped: true, Exit: 0, CargoJSON: b}}
}

func TestUnslopCaptureRequiresBeforeAfterCargoAndController(t *testing.T) {
	for _, fault := range []string{"normal", "missing_provider", "source_missing_before", "config_missing_before", "dependency_missing_before", "embedded_guide_missing", "input_added_after", "input_identity_changed", "wrong_command", "missing_call_id", "failed_child", "uncollected_child", "missing_cargo_json", "cargo_hash_mismatch", "cargo_failed", "cargo_fresh", "cargo_wrong_output", "cargo_duplicate_output", "cancelled", "cancelled_in_before", "ack_failed", "pin_final_failed", "pin_skipped_callback", "pin_repeated_callback", "publish_failed", "commit_failed", "close_failed"} {
		t.Run(fault, func(t *testing.T) {
			s := syntheticUnslopCapture()
			s.fault = fault
			switch fault {
			case "source_missing_before":
				s.before.Sources = nil
			case "config_missing_before":
				s.before.Configuration = nil
			case "dependency_missing_before":
				s.before.Dependencies = nil
			case "embedded_guide_missing":
				s.before.Sources = s.before.Sources[:4]
			case "input_added_after":
				s.after.Sources = append(s.after.Sources, s.after.Sources[0])
			case "input_identity_changed":
				s.after.Sources[0].Identity.FileID = "changed"
			case "wrong_command":
				s.build.CommandSHA = strings.Repeat("0", 64)
			case "missing_call_id":
				s.build.NativeProcess = nil
			case "failed_child":
				s.build.Exit = 1
			case "uncollected_child":
				s.build.Reaped = false
			case "missing_cargo_json":
				s.build.CargoJSON = nil
			case "cargo_hash_mismatch":
				s.build.CargoJSONSHA = strings.Repeat("0", 64)
			case "cargo_failed":
				s.build.CargoJSON = []byte(`{"reason":"build-finished","success":false}`)
			case "cargo_fresh":
				s.build.CargoJSON = []byte(strings.ReplaceAll(string(s.build.CargoJSON), `"fresh":false`, `"fresh":true`))
			case "cargo_wrong_output":
				s.build.CargoJSON = []byte(strings.ReplaceAll(string(s.build.CargoJSON), unslopCaptureOutput, unslopCaptureOutput+".other"))
			case "cargo_duplicate_output":
				s.build.CargoJSON = append(s.build.CargoJSON, s.build.CargoJSON...)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if fault == "cancelled" {
				cancel()
			}
			if fault == "cancelled_in_before" {
				s.cancelBefore = cancel
			}
			pubs := 0
			pending := &syntheticUnslopPending{fault: fault}
			a := unslopCaptureAccess{begin: func(context.Context) (unslopProducerSession, error) { return s, nil }, stage: func(_ context.Context, r unslopCaptureReceipt, b []byte) (unslopPendingCapture, error) {
				pubs++
				if len(b) != len(s.raw) || &b[0] != &s.raw[0] || r.CandidateSHA != digest(s.raw) || r.InstalledTarget != unslopInstalledPath || r.CandidateIdentity != s.id || !r.CompleteCompiledInputs || !r.ProducerReaped || !r.ProducerAcknowledged {
					t.Fatal("publisher received unbound artifact")
				}
				if fault == "publish_failed" {
					return pending, errors.New("synthetic publication failure")
				}
				return pending, nil
			}}
			if fault == "missing_provider" {
				a.begin = nil
			}
			r := captureUnslopRelease(ctx, a)
			if fault == "normal" {
				if !r.Qualified || r.Receipt == nil || pubs != 1 || pending.commits != 1 || pending.aborts != 0 || s.builds != 1 || s.artifactReads != 1 || s.acks != 1 || !s.closed {
					t.Fatal(r)
				}
			} else if r.Qualified || r.Receipt != nil {
				t.Fatal("unproven capture accepted", r)
			}
			if fault != "normal" && fault != "commit_failed" && pending.commits != 0 {
				t.Fatal("failed capture published final receipt")
			}
			if pubs > 0 && fault != "normal" && pending.aborts != 1 {
				t.Fatal("failed capture abandoned pending result")
			}
			if fault == "source_missing_before" || fault == "config_missing_before" || fault == "dependency_missing_before" || fault == "embedded_guide_missing" || fault == "cancelled" || fault == "cancelled_in_before" || fault == "missing_provider" {
				if s.builds != 0 || s.artifactReads != 0 || pubs != 0 {
					t.Fatal("invalid before-capture reached build")
				}
			}
			if r.DataAccessGranted || r.UpdatePerformed {
				t.Fatal("capture claimed installation")
			}
		})
	}
}

func TestUnslopLegacyCannotAdoptHistoricalDigestAsProducerProof(t *testing.T) {
	reads := 0
	r := executeUnslopUpdate(context.Background(), true, unslopUpdateAccess{origin: unslopTestProvider{}, open: func(string) (updateObject, error) { reads++; return nil, nil }})
	if r.Qualified || r.Prepared || reads != 0 || r.Reason != "missing_compiled_input_provenance" {
		t.Fatal(r)
	}
}

func (p unslopTestProvider) Release(context.Context) (unslopReleaseOrigin, error) {
	return p.release, p.err
}
func (p unslopTestProvider) Packet(context.Context) (unslopPacketOrigin, error) {
	return p.packet, p.err
}

func TestUnslopReleaseConsumerUsesOriginBeforeHeldSoftware(t *testing.T) {
	for _, fault := range []string{"verify", "prepare", "missing_provider", "provider_error", "bad_producer", "failed_build", "failed_acceptance", "incomplete_inputs", "source_unverified", "same_hash_wrong_identity", "bad_candidate", "bad_old", "collision", "partial", "pin_failed_after_callback", "pin_skipped_callback", "pin_repeated_callback", "cancelled"} {
		t.Run(fault, func(t *testing.T) {
			plan := unslopReleasePlan{source: unslopSourceHead, merge: unslopMergedHead, candidateSHA: unslopCandidateSHA, installedSHA: unslopInstalledSHA, producerRecord: "synthetic-producer-record", producerResult: "synthetic-producer-result", buildEvidenceSHA: digest([]byte("synthetic-build-evidence")), acceptanceSHA: digest([]byte("synthetic-acceptance"))}
			candidate := []byte("owned synthetic new software")
			before := []byte("owned synthetic old software")
			plan.candidateSHA, plan.installedSHA = digest(candidate), digest(before)
			oldID, newID := softwareIdentity{"v", "old", "c"}, softwareIdentity{"v", "new", "c"}
			proof := unslopReleaseOrigin{ID: unslopOriginID, Source: plan.source, Merge: plan.merge, CandidateSHA: plan.candidateSHA, InstalledSHA: plan.installedSHA, ProducerRecord: plan.producerRecord, ProducerResult: plan.producerResult, BuildEvidenceSHA: plan.buildEvidenceSHA, AcceptanceSHA: plan.acceptanceSHA, CandidateIdentity: newID, InstalledIdentity: oldID, SourceIdentity: "verified-original-source", ProducerExit: 0, CLIExit: 0, MCPExit: 0}
			proof.CompleteCompiledInputs = fault != "incomplete_inputs"
			if fault == "bad_producer" {
				proof.ProducerResult = "caller-asserted"
			}
			if fault == "failed_build" {
				proof.ProducerExit = 1
			}
			if fault == "failed_acceptance" {
				proof.CLIExit = 2
			}
			if fault == "source_unverified" {
				proof.SourceIdentity = ""
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if fault == "cancelled" {
				cancel()
			}
			opens, writes, candidateReads := 0, 0, 0
			var written [][]byte
			old := &fakeUpdateObject{raw: before, id: oldID, acl: "synthetic-acl"}
			if fault == "bad_old" {
				old.raw = []byte("different old")
			}
			old.write = func(path string, raw []byte, acl string) (softwareIdentity, error) {
				if path != unslopStagePath && path != unslopBackupPath || acl != old.acl {
					t.Fatal("other write or ACL")
				}
				expected := before
				if path == unslopStagePath {
					expected = candidate
				}
				if len(raw) != len(expected) || &raw[0] != &expected[0] {
					t.Fatal("verified bytes reopened or copied")
				}
				writes++
				written = append(written, append([]byte(nil), raw...))
				id := softwareIdentity{"v", path, "c"}
				if fault == "partial" && path == unslopStagePath {
					return id, errors.New("synthetic failure")
				}
				return id, nil
			}
			access := unslopUpdateAccess{origin: unslopTestProvider{release: proof}, source: func(context.Context, unslopReleaseOrigin) bool { return fault != "source_unverified" }, open: func(path string) (updateObject, error) {
				opens++
				if path != unslopInstalledPath {
					t.Fatal("other target")
				}
				return old, nil
			}, absent: func(string) bool { return fault != "collision" }, candidate: func(expected softwareIdentity, use func([]byte, softwareIdentity) bool) bool {
				candidateReads++
				if fault == "pin_skipped_callback" {
					return true
				}
				id := newID
				if fault == "same_hash_wrong_identity" {
					id.FileID = "replaced"
				}
				raw := candidate
				if fault == "bad_candidate" {
					raw = []byte("wrong software")
				}
				accepted := use(raw, id)
				if fault == "pin_repeated_callback" {
					return use(raw, id)
				}
				return accepted && fault != "pin_failed_after_callback"
			}}
			if fault == "missing_provider" {
				access.origin = nil
			}
			if fault == "provider_error" {
				access.origin = unslopTestProvider{err: errUnslopOrigin}
			}
			prepare := fault != "verify" && fault != "pin_failed_after_callback" && fault != "pin_skipped_callback" && fault != "pin_repeated_callback"
			result := executeUnslopReleasePlan(ctx, prepare, plan, access)
			switch fault {
			case "verify":
				if !result.Qualified || result.Prepared || writes != 0 {
					t.Fatal(result)
				}
			case "prepare":
				if !result.Qualified || !result.Prepared || writes != 2 || len(written) != 2 {
					t.Fatal(result)
				}
			case "partial":
				if result.Qualified || result.Prepared || len(result.Created) != 2 || result.Reason != "prepare_incomplete_preserved" {
					t.Fatal(result)
				}
			default:
				if result.Qualified || result.Prepared || writes != 0 {
					t.Fatal("invalid reached prepare", result)
				}
			}
			if fault == "missing_provider" || fault == "provider_error" || fault == "bad_producer" || fault == "failed_build" || fault == "failed_acceptance" || fault == "incomplete_inputs" || fault == "source_unverified" || fault == "cancelled" {
				if opens != 0 || candidateReads != 0 {
					t.Fatal("unqualified origin reached software")
				}
			}
			if opens > 0 && !old.closed {
				t.Fatal("old handle not closed")
			}
			if result.Qualified && !result.CompleteCompiledInputs || result.SoftwareReplaceAllowed || result.DataAccessGranted || result.UpdatePerformed {
				t.Fatal("preparation invented provenance or installation")
			}
		})
	}
}
