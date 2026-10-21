package main

import (
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestPublicSDKProbeRejectsSourceAndConfiguredGuestStates(t *testing.T) {
	id := strings.Repeat("c", 32)
	for _, kind := range []string{"active", "prepared", "source-touched", "configured", "foreign-instance"} {
		t.Run(kind, func(t *testing.T) {
			g := guest{instance: id}
			requestID := id
			switch kind {
			case "active":
				g.active = true
			case "prepared":
				g.prepared = true
			case "source-touched":
				g.sourceTouched = true
			case "configured":
				g.configured = true
			case "foreign-instance":
				requestID = strings.Repeat("d", 32)
			}
			a, b := net.Pipe()
			defer b.Close()
			_ = b.SetDeadline(time.Now().Add(time.Second))
			done := make(chan struct{})
			go func() { g.serve(a); close(done) }()
			if e := json.NewEncoder(b).Encode(request{"probe-public-sdk", requestID}); e != nil {
				t.Fatal(e)
			}
			var p publicSDKProbe
			if e := json.NewDecoder(b).Decode(&p); e != io.EOF {
				t.Fatal("source/configured guest returned a public SDK probe", e)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("rejected SDK probe did not drain")
			}
		})
	}
}

func TestSourceFreeSDKProbeCollectsWithoutMaterializingOrRegistering(t *testing.T) {
	id := strings.Repeat("e", 32)
	g := guest{instance: id}
	a, b := net.Pipe()
	defer b.Close()
	_ = b.SetDeadline(time.Now().Add(90 * time.Second))
	done := make(chan struct{})
	go func() { g.serve(a); close(done) }()
	if e := json.NewEncoder(b).Encode(request{"probe-public-sdk", id}); e != nil {
		t.Fatal(e)
	}
	var p publicSDKProbe
	if e := json.NewDecoder(b).Decode(&p); e != nil {
		t.Fatal("valid source-free probe not implemented", e)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("probe did not drain")
	}
	if p.ID != id || len(p.Rust) != 3 || p.ProductSource || p.RunnerRegistered || g.sourceTouched || g.prepared || g.configured || g.active {
		t.Fatal("SDK observation changed guest source/registration state")
	}
	for i, name := range []string{"compiler", "verbose", "cachekey"} {
		if p.Rust[i].Name != name {
			t.Fatal("probe commands changed")
		}
	}
	// A collected response, including nonzero command exits, is an observation;
	// it deliberately does not assert that this source-free fixture has Rust.
}

func TestFailedSourceAttemptCannotBecomeAPublicSDKGuest(t *testing.T) {
	id := strings.Repeat("f", 32)
	g := guest{instance: id}
	a, b := net.Pipe()
	defer b.Close()
	_ = b.SetDeadline(time.Now().Add(2 * time.Second))
	done := make(chan struct{})
	go func() { g.serve(a); close(done) }()
	if e := json.NewEncoder(b).Encode(request{"materialize-source", id}); e != nil {
		t.Fatal(e)
	}
	if e := writeBlob(b, []byte("invalid source packet")); e != nil {
		t.Fatal(e)
	}
	var rejected sourceResult
	if e := json.NewDecoder(b).Decode(&rejected); e != nil || rejected.Failure == "" {
		t.Fatal("failed source fixture not rejected", e)
	}
	<-done
	if !g.sourceTouched || g.prepared {
		t.Fatal("failed source attempt lost its source-use boundary")
	}
	a2, b2 := net.Pipe()
	defer b2.Close()
	_ = b2.SetDeadline(time.Now().Add(2 * time.Second))
	done2 := make(chan struct{})
	go func() { g.serve(a2); close(done2) }()
	if e := json.NewEncoder(b2).Encode(request{"probe-public-sdk", id}); e != nil {
		t.Fatal(e)
	}
	var p publicSDKProbe
	if e := json.NewDecoder(b2).Decode(&p); e != io.EOF {
		t.Fatal("failed source attempt acquired a source-free probe", e)
	}
	<-done2
}
