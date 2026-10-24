//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestReviewMetadataRejectsAncestorJunction(t *testing.T) {
	root := correlationTestRoot(t)
	target := filepath.Join(root, "target")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(target, "todo.json")
	if err := os.WriteFile(path, reviewFixture(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := readReviewSnapshot(path); err != nil {
		t.Fatalf("unlinked positive source failed: %v", err)
	}
	link := filepath.Join(root, "junction")
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='Stop'; New-Item -ItemType Junction -Path $env:CC_REVIEW_METADATA_LINK -Target $env:CC_REVIEW_METADATA_TARGET | Out-Null")
	cmd.Env = append(os.Environ(), "CC_REVIEW_METADATA_LINK="+link, "CC_REVIEW_METADATA_TARGET="+target)
	hideCommand(cmd)
	if err := cmd.Run(); err != nil {
		t.Skipf("junction creation unavailable: %v", err)
	}
	if _, err := readReviewSnapshot(filepath.Join(link, "todo.json")); err == nil {
		t.Fatal("ancestor junction was followed")
	}
}
