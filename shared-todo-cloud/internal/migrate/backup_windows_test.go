//go:build windows

package migrate

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"
)

func TestBackupPruneFailureRetainsNewSnapshotAndReportsIncompleteInventory(t *testing.T) {
	f := newFixture(t, false)
	out := filepath.Join(t.TempDir(), "daily")
	mustBackup(t, f, out, 3, 1)
	old := ownedBackups(t, f.home, out)[0]
	path := filepath.Join(out, old.name, "snapshot.json")
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal("fixture path encoding failed")
	}
	// Permit validation reads but deny deletion until this handle is closed.
	file, err := windows.CreateFile(wide, windows.GENERIC_READ, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal("fixture handle open failed")
	}
	defer windows.CloseHandle(file)
	result, err := backupAt(t.Context(), f.home, out, 1, backupTime(2))
	requireCode(t, err, "backup_prune_failed")
	if !result.Created || result.Reused || result.PruneFailures != 1 || result.PrunedSnapshots != 0 || result.InventoryComplete {
		t.Fatal("prune failure was hidden or misreported as a complete inventory")
	}
	if len(ownedBackups(t, f.home, out)) != 1 {
		t.Fatal("prune failure damaged the new verified snapshot")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("locked old snapshot was removed")
	}
}
