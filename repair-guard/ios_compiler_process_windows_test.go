package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIOSCompilerNativeCommandContainsAndReapsGateChildren(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The command launches a child process for the timeout case. Success/error
	// returns are gated on the exact owned Windows job's active count reaching 0.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := filepath.Join(os.Getenv("SystemRoot"), "System32", "cmd.exe")
	raw, err := iosCompilerCommand(ctx, cmd, "/d", "/c", "echo metadata-ok")
	if err != nil || strings.TrimSpace(string(raw)) != "metadata-ok" {
		t.Fatal("normal contained metadata command", err)
	}
	clear(raw)
	ctx2, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	var before, after uint32
	raw, err = iosCompilerCommandObserved(ctx2, cmd, []string{"/d", "/c", "ping -n 20 127.0.0.1 >nul"}, func(a, b uint32) { before, after = a, b })
	if err == nil || len(raw) != 0 || ctx2.Err() == nil {
		t.Fatal("timed out tree accepted", err)
	}
	if before < 2 || after != 0 {
		t.Fatal("tree fixture did not prove child creation and full drain", before, after)
	}
	if raw, err := iosCompilerCommand(context.Background(), exe+".missing"); err == nil || len(raw) != 0 {
		t.Fatal("failed launch accepted")
	}
}
