package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCapacity3085ContractKeepsOnlyTwoFingerprintedExecutableLeaves(t *testing.T) {
	if len(capacity3085RuntimeLeaves) != 2 {
		t.Fatal("expanded operational qualification")
	}
	want := []string{"D:/CC/Tools/capacity-audit-3h/capacity-audit-3h.exe", "D:/CC/Tools/todo/todo.exe"}
	for index, leaf := range capacity3085RuntimeLeaves {
		if !same(leaf.path, want[index]) || len(leaf.pins) != 2 {
			t.Fatal("different executable or versions")
		}
		for _, pin := range leaf.pins {
			if len(pin.evidence) != 1 || pin.evidence[0].root != capacity3085Source || pin.evidence[0].path != capacity3085Evidence || pin.evidence[0].sha256 != capacity3085EvidenceSHA {
				t.Fatal("unbound current source evidence")
			}
		}
		for _, path := range []string{filepath.Dir(leaf.path), leaf.path + ".bak", leaf.path + ":stream", filepath.Join(filepath.Dir(leaf.path), "state.json"), filepath.Join(filepath.Dir(leaf.path), "runs.jsonl")} {
			if capacity3085RuntimeAllowed(path) {
				t.Fatal("directory, stream, backup or old raw record qualified")
			}
		}
	}
}

func TestCapacity3085ReceiptPinsReviewedSourceAndBuilds(t *testing.T) {
	// Use the checkout's tracked evidence, never a production protection log.
	data, err := os.ReadFile("../docs/evidence/capacity-3085-runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	if hashBytes(data) != capacity3085EvidenceSHA {
		t.Fatal("evidence fingerprint changed")
	}
	var receipt struct {
		Origin         int    `json:"origin_todo_id"`
		Repository     string `json:"repository"`
		Original       bool   `json:"original_registry_recovered"`
		Capacity, Todo struct{ Old, New string }
	}
	// Verify the wire keys independently, rather than decoding missing fields as zero.
	var wire map[string]any
	if json.Unmarshal(data, &receipt) != nil || json.Unmarshal(data, &wire) != nil || receipt.Origin != 3085 || receipt.Repository != "AutoDevJapan/cc-pc-tools-rs" || receipt.Original {
		t.Fatal("wrong current repair scope")
	}
	for i, key := range []string{"capacity", "todo"} {
		entry, ok := wire[key].(map[string]any)
		if !ok {
			t.Fatal("missing build receipt")
		}
		if entry["old_sha256"] != capacity3085RuntimeLeaves[i].pins[0].sha256 || entry["new_sha256"] != capacity3085RuntimeLeaves[i].pins[1].sha256 {
			t.Fatal("runtime pin differs from build receipt")
		}
	}
}
