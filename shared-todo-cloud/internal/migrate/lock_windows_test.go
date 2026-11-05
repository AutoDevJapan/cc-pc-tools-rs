//go:build windows

package migrate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestExportUsesLegacyWindowsByteZeroOnly(t *testing.T) {
	for _, offset := range []uint32{0, 1} {
		t.Run(string(rune('0'+offset)), func(t *testing.T) {
			f := newFixture(t, false)
			lock, err := os.OpenFile(filepath.Join(f.home, "todo.lock"), os.O_RDWR, 0600)
			if err != nil {
				t.Fatal("fixture lock open failed")
			}
			defer lock.Close()
			ov := windows.Overlapped{Offset: offset}
			if windows.LockFileEx(windows.Handle(lock.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ov) != nil {
				t.Fatal("native lock setup failed")
			}
			defer windows.UnlockFileEx(windows.Handle(lock.Fd()), 0, 1, 0, &ov)
			_, err = Export(context.Background(), f.home, f.out)
			if offset == 0 {
				requireCode(t, err, "source_lock_busy")
			} else if err != nil {
				t.Fatalf("adjacent byte incorrectly blocked export: %v", err)
			}
		})
	}
}

func TestWindowsExtendedPathsKeepExactLegacyAliases(t *testing.T) {
	f := newFixture(t, false)
	item := f.document["items"].([]any)[0].(map[string]any)
	original := item["spec"].(string)
	item["spec"], item["spec_history"] = `\\?\`+original, []any{}
	f.save(t)
	mustExport(t, f)
	m := readManifest(t, f.out)
	foundLegacy, foundNative := false, false
	for _, a := range m.Assets {
		if a.OriginalPath == `\\?\`+original {
			foundLegacy = true
		}
		if a.OriginalPath == original {
			foundNative = true
		}
	}
	if !foundLegacy || !foundNative {
		t.Fatal("extended path or native alias was lost")
	}
	p, err := loadExport(context.Background(), f.out)
	if err != nil {
		t.Fatalf("extended path round trip: %v", err)
	}
	p.root.Close()
}
