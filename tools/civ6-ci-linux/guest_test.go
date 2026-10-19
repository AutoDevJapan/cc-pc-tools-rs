package main

import (
	"encoding/json"
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
	good := guestPacket{Plan: rustPlan(), Kind: KindRust, Name: "civ6-ci-linux-rust-1", Token: "X", Nonce: rep("a", 64), Custom: linuxLabel, Defaults: true}
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
			if e := guestRunner(); e == nil {
				t.Errorf("accepted %s", name)
			}
		})
	}
	extra := string(mustJSON(good))
	extra = extra[:len(extra)-1] + ",\"Command\":\"evil\"}"
	withStdin(t, []byte(extra), func() {
		if e := guestRunner(); e == nil {
			t.Error("unknown field accepted")
		}
	})
}

func TestGuestPacketRoundTripKeepsPolicy(t *testing.T) {
	p := guestPacket{Plan: rustPlan(), Kind: KindRust, Policy: fixturePolicy(), Nonce: rep("a", 64)}
	var back guestPacket
	if json.Unmarshal(mustJSON(p), &back) != nil || back.Plan != p.Plan || !reflect.DeepEqual(back.Policy, p.Policy) {
		t.Fatal("packet lost fields")
	}
}
