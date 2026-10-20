package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func discordAskFixture(t *testing.T) (home string) {
	t.Helper()
	base, err := physical(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home = filepath.Join(base, "discord-ask")
	if err := os.MkdirAll(filepath.Join(home, "unattended"), 0700); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestDiscordAskRuntimeQualifiesOnlyTheThreeInstalledLeavesAndUpdateNames(t *testing.T) {
	home := discordAskFixture(t)
	none := func(string) bool { return false }
	installed := []string{"unattended-runner.exe", "discord-ask-tick-hidden.vbs", filepath.Join("unattended", "config.json")}
	for _, name := range installed {
		leaf := filepath.Join(home, name)
		if discordAskRuntimeIn(leaf, home, none) {
			t.Fatalf("absent installed leaf %s accepted", name)
		}
		if err := os.WriteFile(leaf, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		if !discordAskRuntimeIn(leaf, home, none) {
			t.Fatalf("existing installed leaf %s rejected", name)
		}
	}
	for _, name := range []string{"unattended-runner-pr250.atomic-backup.exe", "unattended-runner-install-pr250.private.json"} {
		leaf := filepath.Join(home, name)
		if !discordAskRuntimeIn(leaf, home, none) {
			t.Fatalf("uncreated update leaf %s rejected", name)
		}
		if err := os.WriteFile(leaf, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		if !discordAskRuntimeIn(leaf, home, none) {
			t.Fatalf("existing update leaf %s rejected", name)
		}
	}
	// Every sibling in the same directories stays unqualified, including the
	// conversation records and notification credentials next to the config.
	for _, name := range []string{
		"unattended-runner.exe.bak", "Unattended-Runner.exe", "unattended-runner.exe:stream", "unattended-runner-prX.atomic-backup.exe",
		"unattended-runner-pr.atomic-backup.exe", "unattended-runner-pr1234567.atomic-backup.exe", "unattended-runner-install-pr1.private.log",
		"discord-ask-tick-hidden.vbs.bak", "discord-ask.exe", "discord-notify.json", "config.json", "recovery-messages.json",
		filepath.Join("unattended", "config.json.bak"), filepath.Join("unattended", "conversation-questions.json"),
		filepath.Join("unattended", "conversations", "r1", "question.json"), filepath.Join("unattended", "unattended-runner.exe"),
		filepath.Join("unattended", "discord-ask-tick-hidden.vbs"), filepath.Join("unattended", "Config.json"),
		filepath.Join("other", "unattended-runner.exe"), filepath.Join("unattended", "sub", "config.json"),
	} {
		if discordAskRuntimeIn(filepath.Join(home, name), home, none) {
			t.Fatalf("unrelated name %q accepted", name)
		}
	}
	if discordAskRuntimeIn(home, home, none) || discordAskRuntimeIn(filepath.Join(home, "unattended"), home, none) || discordAskRuntimeIn(filepath.Dir(home), home, none) {
		t.Fatal("directory accepted")
	}
	asDirectory := filepath.Join(home, "unattended-runner-install-pr9.private.json")
	if err := os.Mkdir(asDirectory, 0700); err != nil {
		t.Fatal(err)
	}
	if discordAskRuntimeIn(asDirectory, home, none) {
		t.Fatal("directory accepted as a leaf")
	}
}

func TestDiscordAskRuntimeKeepsProtectionAndRefusesRedirection(t *testing.T) {
	home := discordAskFixture(t)
	leaf := filepath.Join(home, "unattended-runner.exe")
	config := filepath.Join(home, "unattended", "config.json")
	for _, p := range []string{leaf, config} {
		if err := os.WriteFile(p, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name      string
		protected func(string) bool
	}{
		{"leaf", func(p string) bool { return same(p, leaf) }},
		{"home", func(p string) bool { return same(p, home) }},
		{"anything", func(string) bool { return true }},
	} {
		if discordAskRuntimeIn(leaf, home, c.protected) {
			t.Fatalf("protected %s accepted", c.name)
		}
	}
	if discordAskRuntimeIn(config, home, func(p string) bool { return same(p, filepath.Dir(config)) }) {
		t.Fatal("protected configuration directory accepted")
	}
	if discordAskRuntimeIn(config, home, func(p string) bool { return same(p, config) }) {
		t.Fatal("protected configuration accepted")
	}

	none := func(string) bool { return false }
	// A junction cannot stand in for the real directory or the config directory.
	link := filepath.Join(filepath.Dir(home), "junction")
	mklink := exec.Command("cmd", "/c", "mklink", "/J", link, home)
	mklink.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, err := mklink.CombinedOutput(); err != nil {
		t.Skipf("junction unavailable: %v %s", err, out)
	}
	defer os.Remove(link)
	if discordAskRuntimeIn(filepath.Join(link, "unattended-runner.exe"), link, none) || discordAskRuntimeIn(filepath.Join(link, "unattended", "config.json"), link, none) {
		t.Fatal("junctioned directory accepted")
	}
	inner := filepath.Join(filepath.Dir(home), "inner")
	if err := os.Mkdir(inner, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "config.json"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(home, "unattended"), filepath.Join(home, "moved")); err != nil {
		t.Fatal(err)
	}
	mklink = exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(home, "unattended"), inner)
	mklink.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, err := mklink.CombinedOutput(); err != nil {
		t.Skipf("junction unavailable: %v %s", err, out)
	}
	defer os.Remove(filepath.Join(home, "unattended"))
	if discordAskRuntimeIn(config, home, none) {
		t.Fatal("junctioned configuration directory accepted")
	}

	// A link standing in for the executable is not a regular file.
	if err := os.Remove(leaf); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(filepath.Dir(home), "elsewhere.exe")
	if err := os.WriteFile(target, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, leaf); err != nil {
		t.Skipf("file symlink unavailable: %v", err)
	}
	if discordAskRuntimeIn(leaf, home, none) {
		t.Fatal("linked executable accepted")
	}
}

// The guard's three names must stay the paths that the verified source uses.
func TestDiscordAskRuntimeNamesMatchTheVerifiedRunnerSource(t *testing.T) {
	read := func(rel string) string {
		b, err := os.ReadFile(filepath.Join("..", "unattended-runner", rel))
		if err != nil {
			t.Fatal(err)
		}
		return strings.ReplaceAll(string(b), "\\", "/")
	}
	tick := read("install-tick.ps1")
	for _, want := range []string{discordAskDirectory + "/unattended-runner.exe", discordAskDirectory + "/discord-ask-tick-hidden.vbs"} {
		if !strings.Contains(tick, want) {
			t.Fatalf("install-tick.ps1 does not name %s", want)
		}
	}
	if !strings.Contains(read("conversation_cli.go"), discordAskDirectory+"/unattended/config.json") {
		t.Fatal("conversation_cli.go does not name the default configuration")
	}
}
