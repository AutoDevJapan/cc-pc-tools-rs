//go:build windows

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewRegistrationSourceGitIgnoresReplacementObjects(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("Git unavailable")
	}
	root := t.TempDir()
	env := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if !strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			env = append(env, entry)
		}
	}
	env = append(env, "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	prepare := func(cmd *exec.Cmd) *exec.Cmd { hideCommand(cmd); cmd.Env = env; return cmd }
	run := func(args ...string) string {
		cmd := prepare(exec.Command("git", append([]string{"-C", root}, args...)...))
		b, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("fixture Git failed: %v", err)
		}
		return strings.TrimSpace(string(b))
	}
	run("init", "-q", "--template=")
	if err := os.Mkdir(filepath.Join(root, "record-recovery"), 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "record-recovery", "example.txt")
	commit := func(text string) string {
		if err := os.WriteFile(file, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		run("add", "record-recovery/example.txt")
		run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "-q", "-m", "fixture")
		return run("rev-parse", "HEAD")
	}
	original := commit("first")
	current := commit("second")
	run("replace", original, current)
	if !reviewRegistrationModuleMatch([]byte(run("rev-parse", original+":record-recovery", "HEAD:record-recovery"))) {
		t.Fatal("positive replacement attack was not established")
	}
	cmd := reviewRegistrationGit(context.Background(), "rev-parse", original+":record-recovery", "HEAD:record-recovery")
	cmd.Args[3] = root
	b, err := prepare(cmd).Output()
	if err != nil {
		t.Fatal(err)
	}
	if reviewRegistrationModuleMatch(b) {
		t.Fatal("replacement objects hid a changed source module")
	}
	cmd = reviewRegistrationGit(context.Background(), "diff", "--quiet", original, "--", "record-recovery")
	cmd.Args[3] = root
	err = prepare(cmd).Run()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 1 {
		t.Fatalf("real changed inputs were not detected: %v", err)
	}
}
