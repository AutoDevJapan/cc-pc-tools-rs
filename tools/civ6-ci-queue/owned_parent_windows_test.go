package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWindowsOwnedRootUsesQualifiedWorktree(t *testing.T) {
	const id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, parent := range []string{windowsOwnedParent, windowsOwnedParent + "-other", t.TempDir()} {
		called := false
		s := fixedSafeDelete{Exe: writePinned(t, "safe-delete.exe", "synthetic, never executed")}
		s.Run = func(_ context.Context, _ string, _ []byte, _ int64, args ...string) ([]byte, error) {
			called = true
			want := []string{"create", "--project", "civ6-ci-queue", "--content", "job-1-1-1", "--root", windowsOwnedParent}
			if !reflect.DeepEqual(args, want) {
				t.Fatalf("unexpected ownership producer call: %v", args)
			}
			return json.Marshal(map[string]any{"id": id, "state": "owned", "path": filepath.Join(parent, "codex-owned-"+id)})
		}
		_, err := s.Create(context.Background(), "civ6-ci-queue", "job-1-1-1")
		if !called || (err == nil) != strings.EqualFold(parent, windowsOwnedParent) {
			t.Fatalf("ownership response parent %s was misclassified: %v", parent, err)
		}
	}
}
