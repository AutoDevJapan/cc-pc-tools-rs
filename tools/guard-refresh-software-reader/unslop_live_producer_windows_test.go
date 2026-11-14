//go:build windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func syntheticUnslopReady() unslopLiveReady {
	return unslopLiveReady{Domain: unslopLiveDomain, Phase: "ready", Nonce: strings.Repeat("a", 32), Source: unslopCaptureSource, InputManifestSHA: digest([]byte("inputs")), ArtifactSHA: digest([]byte("artifact")), ArtifactBytes: 8, ArtifactIdentity: softwareIdentity{"v", "a", "c"}, EvidenceSHA: digest([]byte("evidence")), EvidenceBytes: 8, EvidenceIdentity: softwareIdentity{"v", "e", "c"}, CompilerProcess: unslopProcessIdentity{PID: 1, Created: 1}}
}

type syntheticUnslopFlight struct {
	ready   unslopLiveReady
	ack     unslopLiveACK
	sent    bool
	fail    string
	aborted bool
}

func (f *syntheticUnslopFlight) Ready(context.Context) (unslopLiveReady, error) { return f.ready, nil }
func (f *syntheticUnslopFlight) SendACK(_ context.Context, a unslopLiveACK) error {
	if a != unslopACKFor(f.ready) {
		return errUnslopOrigin
	}
	f.sent = true
	f.ack = a
	return nil
}
func (f *syntheticUnslopFlight) Done(context.Context) (unslopLiveFinal, unslopProducerCompletion, error) {
	final := unslopLiveFinal{Ready: f.ready, Phase: "ack_received", ACKReceived: f.sent}
	done := unslopProducerCompletion{ACKReceived: f.sent, Reaped: true, WaitVerified: true}
	switch f.fail {
	case "missing_ack":
		final.ACKReceived = false
	case "wrong_final":
		final.Ready.Nonce = strings.Repeat("c", 32)
	case "unreaped":
		done.Reaped = false
	case "failed":
		done.Exit = 1
	case "error":
		return final, done, errors.New("owned flight error")
	}
	return final, done, nil
}
func (f *syntheticUnslopFlight) Abort() { f.aborted = true }

func TestUnslopLiveSessionNeedsWorkerACKReceiptAndNormalDone(t *testing.T) {
	for _, mode := range []string{"normal", "missing_ack", "wrong_final", "unreaped", "failed", "error"} {
		t.Run(mode, func(t *testing.T) {
			flight := &syntheticUnslopFlight{ready: syntheticUnslopReady(), fail: mode}
			s := &unslopLiveSession{unslopNativeSession: &unslopNativeSession{artifactHeld: true}, flight: flight, ready: flight.ready}
			if e := s.Acknowledge(context.Background()); e != nil {
				t.Fatal(e)
			}
			_, e := s.AwaitCompletion(context.Background())
			if (e == nil) != (mode == "normal") {
				t.Fatal("unproven worker completion accepted", mode)
			}
			if s.producerConfirmed != (mode == "normal") {
				t.Fatal("local ACK claimed producer reception")
			}
		})
	}
}

func TestUnslopOwnedLiveWorkerChild(t *testing.T) {
	if os.Getenv("UNSLOP_OWNED_LIVE_CHILD") != "1" {
		return
	}
	if !askWorkerParent() {
		os.Exit(2)
	}
	root := os.Getenv("UNSLOP_OWNED_LIVE_ROOT")
	input, e := openUnslopHeldInput(filepath.Join(root, "input"))
	if e != nil {
		os.Exit(3)
	}
	defer input.Close()
	evidence, e := openUnslopHeldInput(filepath.Join(root, "evidence"))
	if e != nil {
		os.Exit(4)
	}
	defer evidence.Close()
	ok := withPinnedCargoOutput(filepath.Join(root, "output.exe"), filepath.Join(root, "alias.exe"), softwareIdentity{}, func(raw []byte, id softwareIdentity) bool {
		r := syntheticUnslopReady()
		r.ArtifactSHA = digest(raw)
		r.ArtifactBytes = len(raw)
		r.ArtifactIdentity = id
		r.EvidenceSHA = digest(evidence.Raw())
		r.EvidenceBytes = len(evidence.Raw())
		r.EvidenceIdentity = evidence.ID()
		if json.NewEncoder(os.Stdout).Encode(r) != nil {
			return false
		}
		line, e := bufio.NewReader(os.Stdin).ReadBytes('\n')
		if e != nil || !unslopACKMatches(line, r) {
			return false
		}
		if os.Getenv("UNSLOP_OWNED_LIVE_MODE") == "no_final" {
			return true
		}
		return json.NewEncoder(os.Stdout).Encode(unslopLiveFinal{Ready: r, Phase: "ack_received", ACKReceived: true}) == nil
	})
	if ok {
		os.Exit(0)
	}
	os.Exit(5)
}

func TestUnslopOwnedLiveProducerRetainsFilesUntilACKAndReaps(t *testing.T) {
	for _, mode := range []string{"normal", "wrong_ack", "missing_ack", "no_final"} {
		t.Run(mode, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			for name, text := range map[string]string{"input": "owned inputs", "evidence": "owned compiler evidence", "output.exe": "owned binary"} {
				if e := os.WriteFile(filepath.Join(root, name), []byte(text), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if e := os.Link(filepath.Join(root, "output.exe"), filepath.Join(root, "alias.exe")); e != nil {
				t.Fatal(e)
			}
			t.Setenv("UNSLOP_OWNED_LIVE_CHILD", "1")
			t.Setenv("UNSLOP_OWNED_LIVE_ROOT", root)
			t.Setenv("UNSLOP_OWNED_LIVE_MODE", mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			r := syntheticUnslopReady()
			flight := askOwnedProducerStartUnchecked(ctx, os.Args[0], []string{"-test.run=^TestUnslopOwnedLiveWorkerChild$"}, askProducerExpected{Domain: r.Domain, Nonce: r.Nonce, Source: r.Source, InputManifestSHA: r.InputManifestSHA})
			var ready askProducerReady
			select {
			case value, ok := <-flight.Ready:
				if !ok {
					t.Fatal("worker did not become ready")
				}
				ready = value
			case <-ctx.Done():
				t.Fatal("ready timed out")
			}
			if e := os.WriteFile(filepath.Join(root, "input"), []byte("changed"), 0600); e == nil {
				t.Fatal("input not held until ACK")
			}
			if e := os.Rename(filepath.Join(root, "alias.exe"), filepath.Join(root, "alias.moved")); e == nil {
				t.Fatal("artifact alias not held until ACK")
			}
			select {
			case <-flight.Done:
				t.Fatal("producer exited before ACK")
			default:
			}
			ack := askProducerACK{Domain: ready.Domain, Nonce: ready.Nonce, Source: ready.Source, InputManifestSHA: ready.InputManifestSHA, ArtifactSHA: ready.ArtifactSHA, ArtifactIdentity: ready.ArtifactIdentity, EvidenceSHA: ready.EvidenceSHA}
			if mode == "wrong_ack" {
				ack.Nonce = strings.Repeat("c", 32)
			}
			if mode == "missing_ack" {
				flight.Abort()
			} else {
				flight.Accept <- ack
			}
			var done askProducerFlightResult
			select {
			case done = <-flight.Done:
			case <-ctx.Done():
				flight.Abort()
				t.Fatal("worker failed to finish")
			}
			if !done.Child.Reaped || !done.Child.WaitVerified {
				t.Fatal("worker not reaped")
			}
			if (mode == "normal") != done.ProtocolComplete {
				t.Fatal("ACK completion mismatch", mode, done)
			}
			if mode == "normal" && (!done.ACKReceived || !done.ACKSent || done.Child.Exit != 0) {
				t.Fatal("normal ACK not received")
			}
			if e := os.WriteFile(filepath.Join(root, "input"), []byte("after completion"), 0600); e != nil {
				t.Fatal("input hold leaked", e)
			}
			if e := os.Rename(filepath.Join(root, "alias.exe"), filepath.Join(root, "alias.moved")); e != nil {
				t.Fatal("artifact hold leaked", e)
			}
			t.Log(fmt.Sprintf("ownedlive=%s real_handles_held_until_ACK=true reaped=true", mode))
		})
	}
}

// The compiler/input fixtures are synthetic; WithArtifact, Acknowledge and
// AwaitCompletion are the product methods, and the artifact is a held Windows
// file with its real numeric identity. No product output path is written.
type unslopLiveFixtureSession struct {
	*unslopLiveSession
	fixture *unslopSyntheticCapture
}

func (s *unslopLiveFixtureSession) Before() unslopCapturedInputs { return s.fixture.before }
func (s *unslopLiveFixtureSession) Build(context.Context) (unslopBuildInvocation, error) {
	return s.fixture.build, nil
}
func (s *unslopLiveFixtureSession) After(context.Context) (unslopCapturedInputs, error) {
	return s.fixture.after, nil
}
func (s *unslopLiveFixtureSession) Close() error { s.closed = true; return nil }

func TestUnslopLiveReadyIncludesCreationIdentityAtHeldArtifact(t *testing.T) {
	root := physicalSoftwareFixture(t)
	a, b := filepath.Join(root, "image.exe"), filepath.Join(root, "alias.exe")
	raw := []byte("owned artifact")
	if os.WriteFile(a, raw, 0600) != nil || os.Link(a, b) != nil {
		t.Fatal("owned alias fixture")
	}
	var observed softwareIdentity
	if !withPinnedCargoOutput(a, b, softwareIdentity{}, func(_ []byte, id softwareIdentity) bool { observed = id; return true }) || !askProducerID(observed) {
		t.Fatal("numeric native metadata observation failed")
	}
	created, err := strconv.ParseUint(observed.Created, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	for _, boundary := range []string{"native-pinner", "caller-gate"} {
		for _, fault := range []string{"matching", "different-created"} {
			t.Run(boundary+"/"+fault, func(t *testing.T) {
				fixture := syntheticUnslopCapture()
				manifest, valid := unslopCapturedManifest(fixture.before)
				if !valid {
					t.Fatal("input fixture")
				}
				r := syntheticUnslopReady()
				r.InputManifestSHA = manifest
				r.ArtifactIdentity = observed
				r.EvidenceIdentity = observed
				r.ArtifactSHA, r.ArtifactBytes = digest(raw), len(raw)
				if fault == "different-created" {
					r.ArtifactIdentity.Created = strconv.FormatUint(created^1, 10)
				}
				frame, err := json.Marshal(r)
				if err != nil {
					t.Fatal(err)
				}
				parser := &askProducerRuntime{expected: askProducerExpected{r.Domain, r.Nonce, r.Source, manifest}, ready: make(chan askProducerReady, 1)}
				if !parser.receiveReady(frame) {
					t.Fatal("fixture rejected by actual Ready parser")
				}
				r = fromOwnedReady(<-parser.ready)
				flight := &syntheticUnslopFlight{ready: r}
				live := &unslopLiveSession{unslopNativeSession: &unslopNativeSession{}, flight: flight, ready: r}
				pins, observedCallbacks := 0, 0
				var heldData []byte
				live.pinArtifact = func(expected softwareIdentity, use func([]byte, softwareIdentity) bool) bool {
					pins++
					if expected != r.ArtifactIdentity {
						t.Fatal("Ready identity not passed to pinner")
					}
					if boundary == "caller-gate" {
						// A deficient provider still returns the real held identity.
						// Isolate the caller gate so a second guard cannot hide its deletion.
						expected = softwareIdentity{}
					}
					return withPinnedCargoOutput(a, b, expected, func(data []byte, id softwareIdentity) bool {
						observedCallbacks++
						heldData = data
						return use(data, id)
					})
				}
				session := &unslopLiveFixtureSession{unslopLiveSession: live, fixture: fixture}
				stages := 0
				pending := &syntheticUnslopPending{}
				result := captureUnslopRelease(context.Background(), unslopCaptureAccess{
					begin: func(context.Context) (unslopProducerSession, error) { return session, nil },
					stage: func(_ context.Context, receipt unslopCaptureReceipt, data []byte) (unslopPendingCapture, error) {
						stages++
						if !live.artifactHeld || receipt.CandidateIdentity != observed || receipt.CandidateSHA != digest(raw) || receipt.ProducerAcknowledged || receipt.ProducerReaped || string(data) != string(raw) || len(heldData) != len(data) || &heldData[0] != &data[0] {
							t.Fatal("stage not bound to live held artifact")
						}
						if os.WriteFile(a, []byte("replacement"), 0600) == nil {
							t.Fatal("stage lost the native artifact hold")
						}
						return pending, nil
					},
				})
				matching := fault == "matching"
				wantStages := 0
				if matching {
					wantStages = 1
				}
				if result.Qualified != matching || stages != wantStages || flight.sent != matching || pending.commits != wantStages || live.acknowledged != matching || live.producerConfirmed != matching || !live.closed || live.artifactHeld || pins != 1 {
					t.Fatalf("Created boundary reached stage or ACK incorrectly: boundary=%s fault=%s qualified=%t stages=%d ACK=%t commits=%d result=%+v", boundary, fault, result.Qualified, stages, flight.sent, pending.commits, result)
				}
				if !matching && result.Reason != "capture_artifact_unqualified" {
					t.Fatal("wrong rejection gate", result.Reason)
				}
				wantCallbacks := wantStages
				if boundary == "caller-gate" {
					wantCallbacks = 1
				}
				if observedCallbacks != wantCallbacks {
					t.Fatal("wrong identity gate exercised", observedCallbacks, wantCallbacks)
				}
				if matching && flight.ack != unslopACKFor(r) {
					t.Fatal("ACK differs from parsed Ready")
				}
				t.Logf("actual Ready parser=true WithArtifact=true stage=%d ACK=%t numeric-created=true boundary=%s", stages, flight.sent, boundary)
			})
		}
	}
}

func TestUnslopLiveManifestMatchesCoreAndHoldsEmptyInput(t *testing.T) {
	input := syntheticUnslopCapture().before
	for i, j := 0, len(input.Sources)-1; i < j; i, j = i+1, j-1 {
		input.Sources[i], input.Sources[j] = input.Sources[j], input.Sources[i]
	}
	raw, e := marshalUnslopCanonicalInputs(input)
	sha, valid := unslopCapturedManifest(input)
	if e != nil || !valid || digest(raw) != sha {
		t.Fatal("worker manifest differs from validated core")
	}
	path := filepath.Join(physicalSoftwareFixture(t), "empty-input")
	if os.WriteFile(path, nil, 0600) != nil {
		t.Fatal("fixture")
	}
	o, e := openUnslopHeldInput(path)
	if e != nil || len(o.Raw()) != 0 || !unslopCompleteIdentity(o.ID()) {
		t.Fatal("empty dependency rejected", e)
	}
	if e = o.Close(); e != nil {
		t.Fatal(e)
	}
}
func TestUnslopLiveACKRequiresSameHeldGeneration(t *testing.T) {
	for _, fault := range []string{"normal", "wrong_nonce", "wrong_hash", "wrong_identity", "wrong_manifest", "wrong_evidence", "missing", "trailing"} {
		t.Run(fault, func(t *testing.T) {
			r := syntheticUnslopReady()
			ack := unslopACKFor(r)
			switch fault {
			case "wrong_nonce":
				ack.Nonce = strings.Repeat("b", 32)
			case "wrong_hash":
				ack.ArtifactSHA = digest([]byte("other"))
			case "wrong_identity":
				ack.ArtifactIdentity.FileID = "replacement"
			case "wrong_manifest":
				ack.InputManifestSHA = digest([]byte("other"))
			case "wrong_evidence":
				ack.EvidenceSHA = digest([]byte("other"))
			}
			raw, _ := json.Marshal(ack)
			if fault == "missing" {
				raw = nil
			}
			if fault == "trailing" {
				raw = append(raw, []byte("{}")...)
			}
			if unslopACKMatches(raw, r) != (fault == "normal") {
				t.Fatal("unbound ACK accepted", fault)
			}
		})
	}
}
func TestUnslopLiveFinalIsNotInferredFromSuccessfulExit(t *testing.T) {
	r := syntheticUnslopReady()
	for _, mode := range []string{"normal", "no_ack", "wrong_nonce", "exit_only"} {
		f := unslopLiveFinal{Ready: r, Phase: "ack_received", ACKReceived: true}
		if mode == "no_ack" {
			f.ACKReceived = false
		}
		if mode == "wrong_nonce" {
			f.Ready.Nonce = strings.Repeat("b", 32)
		}
		raw, _ := json.Marshal(f)
		if mode == "exit_only" {
			raw = []byte(`{"exit":0}`)
		}
		if unslopFinalMatches(raw, r) != (mode == "normal") {
			t.Fatal("exit replaced actual ACK receipt", mode)
		}
	}
}
