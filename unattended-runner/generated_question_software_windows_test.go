//go:build windows

package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestGeneratedQuestionInstalledSoftwarePathBoundary(t *testing.T) {
	cache := `C:\Users\fixture\AppData\Local`
	valid := filepath.Join(cache, "OpenAI", "Codex", "bin", "0123456789abcdef", "codex.exe")
	if !generatedQuestionInstalledPath(cache, valid) {
		t.Fatal("normal installed location rejected")
	}
	for _, path := range []string{filepath.Join(cache, "copy", "codex.exe"), filepath.Join(filepath.Dir(valid), "other.exe"), filepath.Join(cache, "OpenAI", "Codex", "bin", "unknown", "codex.exe"), filepath.Join(cache, "OpenAI", "Codex", "bin", "codex.exe"), `C:\elsewhere\OpenAI\Codex\bin\0123456789abcdef\codex.exe`} {
		if generatedQuestionInstalledPath(cache, path) {
			t.Fatal("unrelated executable admitted", path)
		}
	}
}

func TestGeneratedQuestionInstalledSoftwareLiveReadOnly(t *testing.T) {
	if os.Getenv("GENERATED_QUESTION_LIVE_READER") != "1" {
		t.Skip("explicit local installed reader acceptance only")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	thread := os.Getenv("CODEX_THREAD_ID")
	if !generatedUUID.MatchString(thread) {
		t.Fatal("actual caller thread metadata required")
	}
	path, err := exec.LookPath("codex.exe")
	if err != nil {
		t.Fatal(err)
	}
	lease, err := generatedQuestionOpenSoftware(ctx, path)
	if err != nil {
		t.Fatal("provided software trust failed", err)
	}
	lease.Close()
	rpc, err := generatedQuestionStartRPC(ctx)
	if err != nil {
		t.Fatal("normal initialize failed", err)
	}
	raw, readErr := rpc.call(ctx, "thread/read", map[string]any{"threadId": thread, "includeTurns": false})
	var result struct{ Thread struct{ ID string } }
	if readErr == nil {
		readErr = json.Unmarshal(raw, &result)
		if result.Thread.ID != thread {
			t.Error("self thread metadata differs")
		}
	}
	closeErr := rpc.close()
	if readErr != nil || closeErr != nil {
		t.Fatal("normal read/close failed", readErr, closeErr)
	}
	if rpc.cmd.ProcessState == nil || !rpc.cmd.ProcessState.Success() {
		t.Fatal("local readonly app server was not reaped successfully")
	}
	t.Log("same provided CLI: signer/owner/identity lease; initialize; self metadata-only read; close; process reaped; no thread start/resume/input")
}

func TestGeneratedQuestionSoftwareRequiresSignerPublisherAndOwner(t *testing.T) {
	if !generatedQuestionSoftwareFacts(true, "OpenAI OpCo, LLC", true) {
		t.Fatal("genuine installed software rejected")
	}
	for _, test := range []struct {
		valid     bool
		publisher string
		owner     bool
	}{{false, "OpenAI OpCo, LLC", true}, {true, "another publisher", true}, {true, "OpenAI OpCo, LLC", false}} {
		if generatedQuestionSoftwareFacts(test.valid, test.publisher, test.owner) {
			t.Fatal("untrusted software accepted", test)
		}
	}
}

func TestGeneratedQuestionSoftwareRejectsNonSymlinkParentReparse(t *testing.T) {
	root := filepath.Clean(t.TempDir())
	info, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "OpenAI", "Codex", "bin", "0123456789abcdef", "codex.exe")
	if err := generatedQuestionSoftwareParents(root, path, func(string) (os.FileInfo, error) { return info, nil }); err != nil {
		t.Fatal("plain parent control failed", err)
	}
	reads := 0
	err = generatedQuestionSoftwareParents(root, path, func(string) (os.FileInfo, error) { reads++; return conversationMediaReparseInfo{info}, nil })
	if err == nil {
		t.Fatal("non-symlink reparse parent accepted")
	}
	if reads != 1 {
		t.Fatal("continued through rejected parent", reads)
	}
}
