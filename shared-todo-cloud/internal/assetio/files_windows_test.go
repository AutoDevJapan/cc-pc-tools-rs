//go:build windows

package assetio_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"autodevjapan/shared-todo-cloud/internal/assetio"
)

func TestJunctionEscapeIsRejected(t *testing.T) {
	base := t.TempDir()
	home, external := filepath.Join(base, "home"), filepath.Join(base, "external")
	if os.Mkdir(home, 0700) != nil {
		t.Fatal("home fixture failed")
	}
	ref, archivePath := archive(t, external, 1, payload(1))
	link := filepath.Join(home, "note-history-v1")
	cmd := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", link, filepath.Dir(archivePath))
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x08000000, HideWindow: true}
	if cmd.Run() != nil {
		t.Fatal("junction fixture creation failed")
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	var calls []upload
	upsert := encode(t, map[string]any{"id": 1, "note_history_archives": []any{ref}})
	requireError(t, assetio.UploadNew(context.Background(), nil, []json.RawMessage{upsert}, home, capture(t, &calls)), assetio.ErrPathUnverified)
	if len(calls) != 0 {
		t.Fatal("junction escape was followed")
	}
}

func TestAlternateDataStreamsAreNotAssets(t *testing.T) {
	home := t.TempDir()
	write(t, filepath.Join(home, "spec.md"), []byte("owned fixture"))
	for _, path := range []string{"spec.md:stream", filepath.Join(home, "spec.md:stream")} {
		upsert := encode(t, map[string]any{"id": 1, "spec": path})
		var calls []upload
		requireError(t, assetio.UploadNew(context.Background(), nil, []json.RawMessage{upsert}, home, capture(t, &calls)), assetio.ErrPathUnverified)
		if len(calls) != 0 {
			t.Fatal("alternate stream reached callback")
		}
	}
}
