package main

import (
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestSDKPreparationRejectsForeignOrUsedGuestBeforeReadingBlobs(t *testing.T) {
	id := strings.Repeat("a", 32)
	for _, state := range []string{"active", "foreign", "prepared", "configured", "source-attempt", "sdk-attempt"} {
		t.Run(state, func(t *testing.T) {
			g := guest{instance: id}
			requestID := id
			switch state {
			case "active":
				g.active = true
			case "foreign":
				requestID = strings.Repeat("b", 32)
			case "prepared":
				g.prepared = true
			case "configured":
				g.configured = true
			case "source-attempt":
				g.sourceTouched = true
			case "sdk-attempt":
				g.sdkAttempted = true
			}
			a, b := net.Pipe()
			defer b.Close()
			_ = b.SetDeadline(time.Now().Add(time.Second))
			done := make(chan struct{})
			go func() { g.serve(a); close(done) }()
			if e := json.NewEncoder(b).Encode(request{"prepare-sdk", requestID}); e != nil {
				t.Fatal(e)
			}
			// No manifest/blob is supplied. An admitted request would block.
			var r sdkResult
			if e := json.NewDecoder(b).Decode(&r); e != nil {
				t.Fatal("SDK admission did not reject before blob input", e)
			}
			<-done
			if r.ID != id || r.Failure != "sdk_guest_state_rejected" || r.ProductSource != (state == "prepared" || state == "source-attempt") || r.RunnerRegistered != (state == "configured") {
				t.Fatal("SDK rejection lost actual guest identity/state", r)
			}
		})
	}
}

func TestFailedSDKAttemptRejectsEverySubsequentRequestKind(t *testing.T) {
	id := strings.Repeat("d", 32)
	g := guest{instance: id}
	a, b := net.Pipe()
	_ = b.SetDeadline(time.Now().Add(time.Second))
	done := make(chan struct{})
	go func() { g.serve(a); close(done) }()
	if e := json.NewEncoder(b).Encode(request{"prepare-sdk", id}); e != nil {
		t.Fatal(e)
	}
	if e := writeBlob(b, []byte("invalid manifest")); e != nil {
		t.Fatal(e)
	}
	var failed sdkResult
	if e := json.NewDecoder(b).Decode(&failed); e != nil || failed.Failure != "sdk_manifest_rejected" {
		t.Fatal("SDK failure control did not reach the installer", e)
	}
	_ = b.Close()
	<-done
	for _, kind := range []string{"probe-public-sdk", "live-job", "materialize-source", "configure-offline", "normal", "cancel", "continue", "status", "prepare-sdk"} {
		t.Run(kind, func(t *testing.T) {
			a, b := net.Pipe()
			defer b.Close()
			_ = b.SetDeadline(time.Now().Add(time.Second))
			done := make(chan struct{})
			go func() { g.serve(a); close(done) }()
			if e := json.NewEncoder(b).Encode(request{kind, id}); e != nil {
				t.Fatal(e)
			}
			var response sdkResult
			e := json.NewDecoder(b).Decode(&response)
			if kind == "prepare-sdk" {
				if e != nil || response.Failure != "sdk_guest_state_rejected" {
					t.Fatal("SDK retry was not rejected", e)
				}
			} else if e != io.EOF {
				t.Fatal("consumed SDK guest accepted another operation", e)
			}
			<-done
			if g.active || g.prepared || g.configured || g.sourceTouched || !g.sdkAttempted || len(g.results) != 0 {
				t.Fatal("rejected request changed guest state")
			}
		})
	}
}

func TestCleanSDKAttemptReadsManifestAndCannotRetryAfterFailure(t *testing.T) {
	id := strings.Repeat("c", 32)
	g := guest{instance: id}
	for attempt := 0; attempt < 2; attempt++ {
		a, b := net.Pipe()
		_ = b.SetDeadline(time.Now().Add(time.Second))
		done := make(chan struct{})
		go func() { g.serve(a); close(done) }()
		if e := json.NewEncoder(b).Encode(request{"prepare-sdk", id}); e != nil {
			t.Fatal(e)
		}
		if attempt == 0 {
			if e := writeBlob(b, []byte("invalid manifest")); e != nil {
				t.Fatal("clean SDK request was rejected before its manifest", e)
			}
		}
		var r sdkResult
		if e := json.NewDecoder(b).Decode(&r); e != nil {
			t.Fatal(e)
		}
		_ = b.Close()
		<-done
		expected := "sdk_manifest_rejected"
		if attempt == 1 {
			expected = "sdk_guest_state_rejected"
		}
		if r.ID != id || r.Failure != expected || r.ProductSource || r.RunnerRegistered || g.active || !g.sdkAttempted {
			t.Fatal("failed SDK attempt lost its admission boundary", r)
		}
	}
}
