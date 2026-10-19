package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRoutesInteractiveAndLeavesExecAndAdministrativeCommandsOfficial(t *testing.T) {
	for _, args := range [][]string{{}, {"--no-daemon"}, {"-m", "gpt-6.1-sol", "resume", "thread"}, {"-c", "a=b", "fork", "--last"}, {"please work on this"}, {"agents"}} {
		if !interactive(args) {
			t.Fatalf("expected interactive: %q", args)
		}
	}
	for _, args := range [][]string{{"--version"}, {"-h"}, {"-c", "a=b", "exec", "--json", "prompt"}, {"app-server"}, {"login"}, {"mcp", "list"}, {"update"}, {"remote-control"}} {
		if interactive(args) {
			t.Fatalf("expected official: %q", args)
		}
	}
	for _, command := range []string{"review", "app", "queue", "migrate-rollouts", "tcp-tunnel", "execpolicy", "responses-api-proxy", "stdio-to-uds", "a", "cloud-tasks"} {
		if interactive([]string{command}) {
			t.Fatalf("administrative command routed to patched TUI: %s", command)
		}
	}
}

func TestManifestVersionAndBothBinaryHashesBindActivation(t *testing.T) {
	root := t.TempDir()
	custom := filepath.Join(root, "codex-progress-tui")
	pkg := filepath.Join(root, "package")
	collector := filepath.Join(root, "agent-progress")
	for _, dir := range []string{custom, pkg, collector} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	write := func(path string, data []byte) {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(pkg, "package.json"), []byte(`{"version":"0.163.0"}`))
	write(filepath.Join(custom, "codex.exe"), []byte("test codex executable"))
	write(filepath.Join(custom, "codex-code-mode-host.exe"), []byte("test official code mode host"))
	write(filepath.Join(collector, "agent-progress.exe"), []byte("test collector"))
	m := manifest{Version: "0.163.0", CodexSHA: digest(filepath.Join(custom, "codex.exe")), CollectorSHA: digest(filepath.Join(collector, "agent-progress.exe")), CodeModeHostSHA: digest(filepath.Join(custom, "codex-code-mode-host.exe"))}
	data, _ := json.Marshal(m)
	write(filepath.Join(custom, "manifest.json"), data)
	if progressBinary(custom, pkg) == "" {
		t.Fatal("matching manifest rejected")
	}
	write(filepath.Join(custom, "codex-code-mode-host.exe"), []byte("changed host"))
	if progressBinary(custom, pkg) != "" {
		t.Fatal("changed code mode host accepted")
	}
	write(filepath.Join(custom, "codex-code-mode-host.exe"), []byte("test official code mode host"))
	write(filepath.Join(collector, "agent-progress.exe"), []byte("changed collector"))
	if progressBinary(custom, pkg) != "" {
		t.Fatal("changed collector accepted")
	}
	write(filepath.Join(collector, "agent-progress.exe"), []byte("test collector"))
	write(filepath.Join(pkg, "package.json"), []byte(`{"version":"0.162.1"}`))
	if progressBinary(custom, pkg) != "" {
		t.Fatal("updated official version accepted stale patch")
	}
}

