//go:build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOwnedProducerFlightChild(t *testing.T) {
	mode := os.Getenv("ASK_PRODUCER_FLIGHT_CHILD")
	if mode == "" {
		return
	}
	if !askWorkerParent() {
		os.Exit(6)
	}
	root := os.Getenv("ASK_PRODUCER_FLIGHT_ROOT")
	a, b := filepath.Join(root, "artifact.exe"), filepath.Join(root, "alias.exe")
	ready := askProducerReady{Domain: "godd-unslop-held-producer-v1", Phase: "ready", Nonce: strings.Repeat("a", 32), Source: strings.Repeat("b", 40), InputManifestSHA: strings.Repeat("c", 64), ArtifactSHA: digest([]byte("owned artifact")), ArtifactBytes: 14, EvidenceSHA: digest([]byte("owned evidence")), EvidenceBytes: 14, EvidenceIdentity: softwareIdentity{"1", "2", "3"}, CompilerProcess: askProducerCompilerProcess{PID: 1, Created: 1}}
	if mode == "exit-before-ready" {
		os.Exit(3)
	}
	ok := withPinnedCargoOutput(a, b, softwareIdentity{}, func(raw []byte, id softwareIdentity) bool {
		ready.ArtifactIdentity = id
		if mode == "wrong-nonce" {
			ready.Nonce = strings.Repeat("d", 32)
		}
		encoded, _ := json.Marshal(ready)
		if mode == "duplicate-key" {
			encoded = append([]byte(`{"domain":"godd-unslop-held-producer-v1",`), encoded[1:]...)
		}
		encoded = append(encoded, '\n')
		_, _ = os.Stdout.Write(encoded)
		if mode == "not-reading-ack" {
			time.Sleep(10 * time.Second)
			return false
		}
		line, e := bufio.NewReader(os.Stdin).ReadBytes('\n')
		defer clear(line)
		var ack askProducerACK
		if e != nil || !askProducerJSON(line, &ack) || ack != (askProducerACK{ready.Domain, ready.Nonce, ready.Source, ready.InputManifestSHA, ready.ArtifactSHA, ready.ArtifactIdentity, ready.EvidenceSHA}) {
			return false
		}
		final := askProducerFinal{Ready: ready, Phase: "ack_received", ACKReceived: true}
		if mode == "wrong-final" {
			final.Ready.ArtifactSHA = strings.Repeat("e", 64)
		}
		_ = json.NewEncoder(os.Stdout).Encode(final)
		return mode != "failure-after-final"
	})
	if ok {
		os.Exit(0)
	}
	os.Exit(1)
}

func TestOwnedProducerFlightRetainsArtifactUntilRealACKAndExit(t *testing.T) {
	for _, mode := range []string{"normal", "wrong-nonce", "duplicate-key", "wrong-ack", "wrong-final", "failure-after-final", "abort", "not-reading-ack", "exit-before-ready"} {
		t.Run(mode, func(t *testing.T) {
			root := physicalSoftwareFixture(t)
			a, b := filepath.Join(root, "artifact.exe"), filepath.Join(root, "alias.exe")
			if os.WriteFile(a, []byte("owned artifact"), 0600) != nil || os.Link(a, b) != nil {
				t.Fatal("fixture")
			}
			t.Setenv("ASK_PRODUCER_FLIGHT_CHILD", mode)
			t.Setenv("ASK_PRODUCER_FLIGHT_ROOT", root)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			expected := askProducerExpected{"godd-unslop-held-producer-v1", strings.Repeat("a", 32), strings.Repeat("b", 40), strings.Repeat("c", 64)}
			flight := askOwnedProducerStartUnchecked(ctx, os.Args[0], []string{"-test.run=^TestOwnedProducerFlightChild$"}, expected)
			defer flight.Abort()
			if mode != "wrong-nonce" && mode != "duplicate-key" && mode != "exit-before-ready" {
				select {
				case ready, ok := <-flight.Ready:
					if !ok {
						t.Fatal("ready unavailable")
					}
					if !withPinnedCargoOutput(a, b, ready.ArtifactIdentity, func(raw []byte, id softwareIdentity) bool {
						return bytes.Equal(raw, []byte("owned artifact")) && digest(raw) == ready.ArtifactSHA
					}) {
						t.Fatal("held artifact mismatch")
					}
					if os.WriteFile(a, []byte("foreign"), 0600) == nil {
						t.Fatal("producer released artifact before ACK")
					}
					if mode == "abort" {
						flight.Abort()
					} else {
						ack := askProducerACK{ready.Domain, ready.Nonce, ready.Source, ready.InputManifestSHA, ready.ArtifactSHA, ready.ArtifactIdentity, ready.EvidenceSHA}
						if mode == "wrong-ack" {
							ack.ArtifactSHA = strings.Repeat("f", 64)
						}
						flight.Accept <- ack
						if mode == "not-reading-ack" {
							flight.Abort()
						}
					}
				case <-time.After(3 * time.Second):
					t.Fatal("producer never ready")
				}
			}
			select {
			case done := <-flight.Done:
				if !done.Child.Reaped || !done.Child.WaitVerified {
					t.Fatal("producer not collected", done)
				}
				if done.ProtocolComplete != (mode == "normal") {
					t.Fatal("wrong protocol completion", mode, done)
				}
				if mode == "normal" && (!done.ACKSent || !done.ACKReceived || done.Child.Exit != 0 || done.NativeProcess == nil) {
					t.Fatal("real ACK or native identity missing")
				}
				if os.WriteFile(a, []byte("released"), 0600) != nil {
					t.Fatal("producer handle remains held after Done")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("producer/ACK writer not joined")
			}
		})
	}
}

func TestOwnedProducerLaunchRejectsNonfixedTuple(t *testing.T) {
	if _, e := askOwnedProducerStart(context.Background(), "other.exe", nil, askProducerExpected{}); e == nil {
		t.Fatal("arbitrary producer accepted")
	}
}

func TestOwnedRootNativeWaitIsSeparateFromContextAndIOErrors(t *testing.T) {
	if os.Getenv("ASK_NATIVE_WAIT_CHILD") == "1" {
		os.Exit(0)
	}
	t.Setenv("ASK_NATIVE_WAIT_CHILD", "1")
	cmd := exec.Command(os.Args[0], "-test.run=^TestOwnedRootNativeWaitIsSeparateFromContextAndIOErrors$")
	if err := cmd.Run(); err != nil || cmd.ProcessState == nil || !cmd.ProcessState.Exited() || cmd.ProcessState.ExitCode() != 0 {
		t.Fatal("native wait fixture", err)
	}
	for _, waitErr := range []error{nil, context.Canceled, context.DeadlineExceeded, exec.ErrWaitDelay} {
		if !askRootWaitVerified(cmd.ProcessState, waitErr) {
			t.Fatal("collected native root lost due to aggregate wait error", waitErr)
		}
		if askRootWaitVerified(nil, waitErr) {
			t.Fatal("missing native root accepted")
		}
	}
}
