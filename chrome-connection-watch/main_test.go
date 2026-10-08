package main

import "testing"

func TestTransportRequired(t *testing.T) {
	r := report{ExtensionInstalled: true, ExtensionEnabled: true, ManifestValid: true, HostBinaryPresent: true}
	r.finish()
	if r.Ready || r.Category != "extension_transport_disconnected" {
		t.Fatal(r)
	}
	r.Connected = true
	r.finish()
	if !r.Ready || r.Category != "ready" {
		t.Fatal(r)
	}
}

func TestInventoryRejectsIncompleteEvidence(t *testing.T) {
	for _, raw := range []string{`[]`, `null`, `[{}]`, `[{"pipe":"p","instance":"i","generation":"g"}]`} {
		ok, err := inventoryConnected([]byte(raw))
		if ok || err != nil {
			t.Fatalf("%s: %v %v", raw, ok, err)
		}
	}
	if ok, err := inventoryConnected([]byte(`[{"pipe":"p","instance":"i","generation":"g","tabs":[]}]`)); !ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
	if _, err := inventoryConnected([]byte(`failed`)); err == nil {
		t.Fatal("accepted malformed inventory")
	}
}

func TestDesktopReferenceScopedToNodeRepl(t *testing.T) {
	cfg := []byte("[other]\nSKY_CUA_NATIVE_PIPE = \"1\"\n[mcp_servers.node_repl.env]\nSKY_CUA_NATIVE_PIPE = \"1\"\nSKY_CUA_NATIVE_PIPE_DIRECTORY = '\\\\.\\pipe\\test'\n[next]\n")
	enabled, pipe := desktopReference(cfg)
	if !enabled || pipe != `\\.\pipe\test` {
		t.Fatalf("%v %q", enabled, pipe)
	}
	if enabled, _ := desktopReference([]byte("[other]\nSKY_CUA_NATIVE_PIPE = \"1\"\n")); enabled {
		t.Fatal("accepted unrelated section")
	}
}
