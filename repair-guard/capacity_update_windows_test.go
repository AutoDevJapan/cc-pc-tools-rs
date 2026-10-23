package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCapacityUpdatePurposeKeepsGenericDataClassificationAndRejectsMixedFlags(t *testing.T) {
	target := "D:/CC/Tools/todo/todo.exe"
	for _, args := range [][]string{{"--verify-capacity-runtime-update", target, "--guard-path", target}, {"--verify-capacity-runtime-update", target, "--verify-product-resume", "1626"}, {"--verify-capacity-runtime-update", target, "--verify-current-succession", "2839"}, {"--verify-capacity-runtime-update", target, "extra"}, {"--verify-capacity-runtime-update"}, {"--verify-capacity-runtime-update", "relative"}} {
		var out, err bytes.Buffer
		calls := 0
		code := runGuardCLIWithRuntime(args, &out, &err, func(int) object { return object{"verified": false} }, func(string) object { calls++; return object{"qualified": true} })
		var got object
		if code == 0 || json.Unmarshal(out.Bytes(), &got) != nil || got["qualified"] != false || calls != 0 {
			t.Fatal("mixed/malformed purpose reached verifier")
		}
	}
	var out, err bytes.Buffer
	calls := 0
	code := runGuardCLIWithRuntime([]string{"--verify-capacity-runtime-update", target}, &out, &err, func(int) object { return object{"verified": false} }, func(string) object { calls++; return object{"qualified": true, "data_access_granted": false} })
	if code != 0 || calls != 1 {
		t.Fatal("valid purpose route unavailable")
	}
	if len(capacity3085UpdateProfile.leaves) != 2 {
		t.Fatal("expanded qualification")
	}
	for _, leaf := range capacity3085UpdateProfile.leaves {
		for _, path := range []string{filepath.Dir(leaf.target), leaf.target + ".bak", leaf.target + ":stream", filepath.Join(filepath.Dir(leaf.target), "state.json"), filepath.Join(filepath.Dir(leaf.target), "runs.jsonl")} {
			if capacity3085RuntimeReceipt(path)["qualified"] != false {
				t.Fatal("purpose extended to data/directory/alternate leaf")
			}
		}
	}
}

func TestCapacityUpdateBindsCurrentTargetNewArtifactAndIndependentSource(t *testing.T) {
	f := newConsoleFixture(t)
	sourcePath := filepath.Join(f.root, "new.exe")
	if err := os.WriteFile(sourcePath, []byte("new reviewed software"), 0600); err != nil {
		t.Fatal(err)
	}
	p := capacityUpdateProfile{root: f.root, evidence: f.receipt, evidenceSHA: f.leaves[0].pins[0].evidence[0].sha256, leaves: []capacityUpdateLeaf{{"fixture", f.leaf, sourcePath, f.leaves[0].pins[0].sha256, hashBytes([]byte("new reviewed software"))}}}
	allowed := func(path string) bool {
		return capacity3085RuntimeWith(path, p, func(string) bool { return false }, func(root string) bool { return same(root, f.root) })["qualified"] == true
	}
	if !allowed(f.leaf) {
		t.Fatal("verified software update rejected")
	}
	alias := filepath.Join(f.root, "candidate-alias.exe")
	if err := os.Link(sourcePath, alias); err != nil {
		t.Fatal(err)
	}
	if allowed(f.leaf) {
		t.Fatal("multiply linked build output accepted as distribution candidate")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if !allowed(f.leaf) {
		t.Fatal("independent distribution candidate rejected")
	}
	// A purpose receipt must never turn into a general data classification.
	if allowed(f.root) || allowed(f.receipt) || allowed(sourcePath) {
		t.Fatal("purpose widened")
	}
	if capacity3085RuntimeWith(f.leaf, p, func(path string) bool { return same(path, f.leaf) }, func(string) bool { return true })["qualified"] != false {
		t.Fatal("original protection bypassed")
	}
	if capacity3085RuntimeWith(f.leaf, p, func(string) bool { return false }, func(string) bool { return false })["qualified"] != false {
		t.Fatal("unverified source accepted")
	}
	if err := os.WriteFile(sourcePath, []byte("unknown source"), 0600); err != nil {
		t.Fatal(err)
	}
	if allowed(f.leaf) {
		t.Fatal("changed candidate accepted")
	}
	if err := os.WriteFile(sourcePath, []byte("new reviewed software"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.leaf, []byte("unknown installed software"), 0600); err != nil {
		t.Fatal(err)
	}
	if allowed(f.leaf) {
		t.Fatal("unknown installed bytes accepted")
	}
	if err := os.WriteFile(f.leaf, []byte("new reviewed software"), 0600); err != nil {
		t.Fatal(err)
	}
	if !allowed(f.leaf) {
		t.Fatal("matching already-installed candidate rejected")
	}
	if err := os.WriteFile(f.receipt, []byte("changed evidence"), 0600); err != nil {
		t.Fatal(err)
	}
	if allowed(f.leaf) {
		t.Fatal("changed evidence accepted")
	}
}

func TestCapacityUpdateRejectsMixedModelPurposeAtPublicCLI(t *testing.T) {
	target := "D:/CC/Tools/todo/todo.exe"
	for _, args := range [][]string{
		{"--verify-capacity-runtime-update", target, "--verify-model-runtime-update", target},
		{"--verify-model-runtime-update", target, "--verify-capacity-runtime-update", target},
	} {
		var out, err bytes.Buffer
		code := runGuardCLI(args, &out, &err, func(int) object {
			t.Fatal("mixed purposes reached an unrelated verifier")
			return nil
		})
		var got object
		if code != 1 || json.Unmarshal(out.Bytes(), &got) != nil || len(got) != 1 || got["qualified"] != false {
			t.Fatalf("mixed purposes did not return a bounded rejection: code=%d output=%q", code, out.String())
		}
	}
}
