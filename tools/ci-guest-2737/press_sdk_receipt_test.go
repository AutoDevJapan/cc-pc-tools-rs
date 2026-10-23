package main

import (
	"encoding/json"
	"testing"
)

func TestPressSDKReceiptRejectsAbsentFailedPartialAndSubstitutedPreparation(t *testing.T) {
	good := pressSDKInventory{OS: "ubuntu-24.04", Chrome: "Google Chrome 156.0.8078.47", Rust: "rustc 1.95.0 (59807616e 2026-04-14)"}
	for _, stage := range []string{"apt-get/update", "apt-get/install", "apt-get/update", "rustup/toolchain", "chrome/--version", "rustc/+1.95.0"} {
		good.Steps = append(good.Steps, pressSDKStep{Stage: stage})
	}
	good.Steps[4].Stdout = []byte(good.Chrome + "\n")
	good.Steps[5].Stdout = []byte(good.Rust + "\n")
	if validPressSDKInventory(nil) || !validPressSDKInventory(&good) {
		t.Fatal("receipt presence validation failed")
	}
	for _, mutate := range []func(*pressSDKInventory){
		func(p *pressSDKInventory) { p.OS = "ubuntu-22.04" },
		func(p *pressSDKInventory) { p.Rust = "rustc 1.97.1" },
		func(p *pressSDKInventory) { p.Chrome = "Chromium 156" },
		func(p *pressSDKInventory) { p.Steps = nil },
		func(p *pressSDKInventory) { p.Steps = p.Steps[:5] },
		func(p *pressSDKInventory) { p.Steps[1].Exit = 1 },
		func(p *pressSDKInventory) { p.Steps[2].Stage = "apt-get/install" },
		func(p *pressSDKInventory) { p.Steps[4].Stdout = []byte("Google Chrome substituted\n") },
		func(p *pressSDKInventory) { p.Steps[5].Stdout = []byte("rustc substituted\n") },
		func(p *pressSDKInventory) { p.Steps[0].Stderr = make([]byte, (32<<10)+1) },
	} {
		b, e := json.Marshal(good)
		if e != nil {
			t.Fatal(e)
		}
		var p pressSDKInventory
		if json.Unmarshal(b, &p) != nil {
			t.Fatal("receipt wire fixture failed")
		}
		mutate(&p)
		if validPressSDKInventory(&p) {
			t.Fatal("incomplete or inconsistent receipt admitted before token")
		}
	}
}
