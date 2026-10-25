//go:build windows

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func omb3255RuntimeTestRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	name, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		t.Fatal(err)
	}
	h, err := syscall.CreateFile(name, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, 0x02000000, 0)
	if err != nil {
		t.Fatal(err)
	}
	physical, ok := omb3255HandlePath(h)
	syscall.CloseHandle(h)
	if !ok {
		t.Fatal("fixture physical directory unavailable")
	}
	return physical
}

func TestOMB3255RuntimeMetadataPurposeKeepsProtection(t *testing.T) {
	clear := omb3255GuardVerdict{Access: true, Status: "clear"}
	unknown := omb3255GuardVerdict{Unverified: true, Blocked: true, Status: "unverified"}
	if !omb3255MetadataPurpose(clear, clear, unknown, true) {
		t.Fatal("independently bound metadata purpose unavailable")
	}
	for _, c := range []struct {
		s, b, n omb3255GuardVerdict
		bound   bool
	}{
		{clear, clear, unknown, false}, {unknown, clear, unknown, true}, {clear, unknown, unknown, true},
		{clear, clear, omb3255GuardVerdict{Status: "protected", Blocked: true, Categories: 1}, true},
		{clear, clear, omb3255GuardVerdict{Status: "unverified", Unverified: true, Categories: 1}, true},
		{clear, clear, omb3255GuardVerdict{Status: "unverified"}, true},
		{clear, clear, omb3255GuardVerdict{Status: "unverified", Unverified: true}, true},
		{clear, clear, omb3255GuardVerdict{Status: "unverified", Unverified: true, Access: true, Blocked: true}, true},
	} {
		if omb3255MetadataPurpose(c.s, c.b, c.n, c.bound) {
			t.Fatal("protection or source uncertainty overridden")
		}
	}
	if unknown.Access || !unknown.Unverified || !unknown.Blocked {
		t.Fatal("purpose qualification changed general guard input")
	}
}

func TestOMB3255RuntimeGuardUsesProtectedStateNotOverloadedWireName(t *testing.T) {
	clear := omb3255GuardVerdict{Access: true, Status: "clear"}
	for _, state := range []string{"clear", "unverified", "protected"} {
		wire := obj{"ok": true, "access_allowed": state == "clear", "client_resource": state != "clear", "classification_unverified": state == "unverified", "protection_status": state, "protection_categories": []any{}}
		if state == "protected" {
			wire["protection_categories"] = []any{"client_project"}
		}
		verdict, err := omb3255ParseGuard(wire)
		if err != nil || omb3255MetadataPurpose(clear, clear, verdict, true) != (state != "protected") {
			t.Fatalf("guard tri-state misclassified: %s", state)
		}
		for _, field := range []string{"access_allowed", "client_resource", "classification_unverified"} {
			wire[field] = !wire[field].(bool)
			if _, err := omb3255ParseGuard(wire); err == nil {
				t.Fatalf("inconsistent guard field accepted: %s/%s", state, field)
			}
			wire[field] = !wire[field].(bool)
		}
	}
}

func TestOMB3255RuntimePinnedHandleDeniesWritesAndRename(t *testing.T) {
	root := omb3255RuntimeTestRoot(t)
	path := filepath.Join(root, "synthetic.jsonl")
	if err := os.WriteFile(path, []byte("synthetic\n"), 0600); err != nil {
		t.Fatal(err)
	}
	control, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal("positive writable fixture control failed")
	}
	control.Close()
	source, err := omb3255OpenPinnedRuntime(path)
	if err != nil {
		t.Fatal("plain owned fixture refused")
	}
	defer source.Close()
	if !source.Unchanged() {
		t.Fatal("stable same handle refused")
	}
	writer, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err == nil {
		writer.Close()
		t.Fatal("writer admitted while source was pinned")
	}
	if err := os.Rename(path, filepath.Join(root, "renamed.jsonl")); err == nil {
		t.Fatal("pinned source renamed")
	}
	if !source.Unchanged() {
		t.Fatal("refused mutation changed source identity")
	}
}

func TestOMB3255RuntimePinnedSourceRejectsLinks(t *testing.T) {
	root := omb3255RuntimeTestRoot(t)
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(target, "synthetic.jsonl")
	if err := os.WriteFile(path, []byte("synthetic\n"), 0600); err != nil {
		t.Fatal(err)
	}
	control, err := omb3255OpenPinnedRuntime(path)
	if err != nil {
		t.Fatal("positive source control failed")
	}
	control.Close()
	alias := filepath.Join(root, "hardlink.jsonl")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	if source, err := omb3255OpenPinnedRuntime(alias); err == nil {
		source.Close()
		t.Fatal("shared source admitted")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "junction")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", `$ErrorActionPreference='Stop'; New-Item -ItemType Junction -Path $env:CC_3255_LINK -Target $env:CC_3255_TARGET | Out-Null`)
	hideCommand(cmd)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "CC_3255_LINK="+link, "CC_3255_TARGET="+target)
	if err := cmd.Run(); err != nil {
		t.Skipf("junction creation unavailable: %v", err)
	}
	if source, err := omb3255OpenPinnedRuntime(filepath.Join(link, "synthetic.jsonl")); err == nil {
		source.Close()
		t.Fatal("ancestor junction admitted")
	}
}
