package main

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

func withStdin(t *testing.T, data []byte, f func()) {
	t.Helper()
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	_, _ = w.Write(data)
	_ = w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old; _ = r.Close() }()
	f()
}

func TestGuestRunnerRejectsRegistrationIdentityDrift(t *testing.T) {
	reached := 0
	sentinel := errors.New("configuration step reached")
	old := configureRunner
	configureRunner = func(guestPacket) error { reached++; return sentinel }
	t.Cleanup(func() { configureRunner = old })
	good := guestPacket{Plan: rustPlan(), Kind: KindRust, Name: "civ6-ci-linux-rust-1", Token: "X", Nonce: rep("a", 64), Custom: linuxLabel + "," + jobLabel(rustPlan()), Defaults: true}
	cq := guestPacket{Plan: codeQLPlan("Analyze (rust)"), Kind: KindCodeQL, Name: "civ6-ci-linux-codeql-1", Token: "X", Nonce: rep("a", 64), Custom: codeQLLabel}
	mutations := map[string]guestPacket{}
	add := func(name string, p guestPacket, m func(*guestPacket)) { m(&p); mutations[name] = p }
	add("no token", good, func(p *guestPacket) { p.Token = "" })
	add("short nonce", good, func(p *guestPacket) { p.Nonce = "a" })
	add("other label", good, func(p *guestPacket) { p.Custom = "someone-else" })
	add("no default labels for Linux", good, func(p *guestPacket) { p.Defaults = false })
	add("default labels for CodeQL", cq, func(p *guestPacket) { p.Defaults = true })
	add("CodeQL with the Linux label", cq, func(p *guestPacket) { p.Custom = linuxLabel })
	add("name of the other kind", good, func(p *guestPacket) { p.Name = "civ6-ci-linux-codeql-1" })
	add("path in name", good, func(p *guestPacket) { p.Name = "civ6-ci-linux-rust-/../x" })
	add("kind mismatch", good, func(p *guestPacket) { p.Kind = KindCodeQL })
	add("bad plan", good, func(p *guestPacket) { p.Plan.Head = "x" })
	for name, p := range mutations {
		withStdin(t, mustJSON(p), func() {
			if e := guestRunner(); e == nil || errors.Is(e, sentinel) {
				t.Errorf("accepted %s: %v", name, e)
			}
		})
	}
	extra := string(mustJSON(good))
	extra = extra[:len(extra)-1] + ",\"Command\":\"evil\"}"
	withStdin(t, []byte(extra), func() {
		if e := guestRunner(); e == nil || errors.Is(e, sentinel) {
			t.Errorf("unknown field accepted: %v", e)
		}
	})
	if reached != 0 {
		t.Fatalf("registration configuration ran %d times for invalid packets", reached)
	}
	// Control: valid packets of both kinds pass the same boundary.
	for _, p := range []guestPacket{good, cq} {
		withStdin(t, mustJSON(p), func() {
			if e := guestRunner(); !errors.Is(e, sentinel) {
				t.Errorf("valid %s packet did not reach configuration: %v", p.Kind, e)
			}
		})
	}
	if reached != 2 {
		t.Fatalf("configuration reached %d times, want 2", reached)
	}
}

func TestGuestPacketRoundTripKeepsPolicy(t *testing.T) {
	p := guestPacket{Plan: rustPlan(), Kind: KindRust, Policy: fixturePolicy(), Nonce: rep("a", 64)}
	var back guestPacket
	if json.Unmarshal(mustJSON(p), &back) != nil || back.Plan != p.Plan || !reflect.DeepEqual(back.Policy, p.Policy) {
		t.Fatal("packet lost fields")
	}
}
