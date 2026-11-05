package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func backupTime(day int) time.Time {
	return time.Date(2026, 1, day, 12, 0, 0, 0, time.FixedZone("JST", 9*3600))
}

func ownedBackups(t *testing.T, home, out string) []savedBackup {
	t.Helper()
	root, err := os.OpenRoot(out)
	if err != nil {
		t.Fatal("fixture backup root unavailable")
	}
	defer root.Close()
	owner, err := loadBackupOwner(root, filepath.Join(home, "todo.json"))
	if err != nil {
		t.Fatal("fixture owner unavailable")
	}
	backups, _, err := scanBackups(t.Context(), root, owner)
	if err != nil {
		t.Fatalf("fixture inventory: %v", err)
	}
	return backups
}

func mustBackup(t *testing.T, f *fixture, out string, keep, day int) BackupSummary {
	t.Helper()
	result, err := backupAt(t.Context(), f.home, out, keep, backupTime(day))
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	return result
}

func copyBackupFixture(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0700)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), b, 0600)
	})
	if err != nil {
		t.Fatal("fiction backup copy failed")
	}
}

func TestDailyBackupReuseRetentionAndExactContent(t *testing.T) {
	f := newFixture(t, false)
	out := filepath.Join(t.TempDir(), "daily")
	first := mustBackup(t, f, out, 3, 1)
	if !first.Created || first.Reused || first.RetainedSnapshots != 1 || first.SnapshotBytes <= int64(len(f.raw)) || !first.InventoryComplete || first.SpecMissing != 1 || first.SpecUnverified != 3 {
		t.Fatal("initial backup aggregate differs")
	}
	initial := ownedBackups(t, f.home, out)[0]
	manifestBefore, _ := os.ReadFile(filepath.Join(out, initial.name, "manifest.json"))
	rootBefore, _ := os.ReadFile(filepath.Join(out, initial.name, "snapshot.json"))
	if !bytes.Equal(rootBefore, f.raw) {
		t.Fatal("backup changed original root bytes")
	}
	reused := mustBackup(t, f, out, 3, 1)
	manifestAfter, _ := os.ReadFile(filepath.Join(out, initial.name, "manifest.json"))
	if reused.Created || !reused.Reused || reused.RetainedSnapshots != 1 || reused.RetainedBytes != first.RetainedBytes || !bytes.Equal(manifestBefore, manifestAfter) || reused.ProtectedEntries != 0 {
		t.Fatal("same-day reuse changed an immutable backup or left a pending directory")
	}
	for day := 2; day <= 4; day++ {
		f.document["items"].([]any)[0].(map[string]any)["note"] = strings.Repeat("f", day)
		f.save(t)
		result := mustBackup(t, f, out, 3, day)
		if result.RetainedSnapshots != min(day, 3) || !result.Created || result.Reused {
			t.Fatal("daily retention count differs")
		}
		if day == 4 && (result.PrunedSnapshots != 1 || result.PrunedBytes != first.SnapshotBytes) {
			t.Fatal("old generation or measured byte count differs")
		}
	}
	backups := ownedBackups(t, f.home, out)
	if len(backups) != 3 || backups[0].receipt.Generation != 4 || backups[2].receipt.Generation != 2 {
		t.Fatal("retention did not keep the latest three generations")
	}
	var size int64
	for _, backup := range backups {
		size += backup.bytes
		if _, err := VerifyExport(t.Context(), filepath.Join(out, backup.name)); err != nil {
			t.Fatalf("retained local verify: %v", err)
		}
	}
	last := mustBackup(t, f, out, 3, 4)
	if last.RetainedBytes != size || !last.Reused {
		t.Fatal("retained capacity aggregate differs")
	}
	source, _ := os.ReadFile(filepath.Join(f.home, "todo.json"))
	if !bytes.Equal(source, f.raw) {
		t.Fatal("backup modified the live cache")
	}
	if _, err := os.Stat(filepath.Join(out, initial.name)); !os.IsNotExist(err) {
		t.Fatal("expired backup was not removed")
	}
}

func TestBackupDetectsAssetAndMissingReferenceChangesWithSameRoot(t *testing.T) {
	f := newFixture(t, false)
	out := filepath.Join(t.TempDir(), "daily")
	mustBackup(t, f, out, 3, 1)
	rootHash := digest(f.raw)
	spec := f.document["items"].([]any)[0].(map[string]any)["spec"].(string)
	writeFixture(t, spec, []byte("new fixture spec bytes"))
	changed := mustBackup(t, f, out, 3, 1)
	if !changed.Created || changed.Reused || changed.RetainedSnapshots != 2 || digest(f.raw) != rootHash {
		t.Fatal("root-only reuse hid changed specification bytes")
	}
	writeFixture(t, filepath.Join(f.home, "specs", "missing.md"), []byte("recovered fixture specification"))
	recovered := mustBackup(t, f, out, 3, 1)
	if !recovered.Created || recovered.SpecMissing != 0 || recovered.SpecImported != 4 || recovered.RetainedSnapshots != 3 {
		t.Fatal("changed reference availability was hidden by reuse")
	}
	// Reverting to older content must capture a new latest generation, rather
	// than reusing and then deleting the only backup of the current state.
	writeFixture(t, spec, f.spec)
	if os.Remove(filepath.Join(f.home, "specs", "missing.md")) != nil {
		t.Fatal("fixture removal failed")
	}
	reverted := mustBackup(t, f, out, 1, 1)
	if !reverted.Created || reverted.Reused || reverted.RetainedSnapshots != 1 || reverted.PrunedSnapshots != 3 {
		t.Fatal("content reversion retention lost the current capture")
	}
}

func TestRetentionPreservesUnknownFilesUnownedExportsAndForeignSources(t *testing.T) {
	f := newFixture(t, false)
	out := filepath.Join(t.TempDir(), "daily")
	mustBackup(t, f, out, 10, 1)
	protected := ownedBackups(t, f.home, out)[0]
	writeFixture(t, filepath.Join(out, protected.name, "keep-extra.txt"), []byte("keep fixture extra"))
	writeFixture(t, filepath.Join(out, "unknown.txt"), []byte("keep fixture root member"))
	writeFixture(t, filepath.Join(out, "original-scope", "only-copy.txt"), []byte("keep fixture original"))
	other := newFixture(t, false)
	otherRoot := filepath.Join(t.TempDir(), "other-daily")
	mustBackup(t, other, otherRoot, 3, 1)
	foreign := ownedBackups(t, other.home, otherRoot)[0]
	copyBackupFixture(t, filepath.Join(otherRoot, foreign.name), filepath.Join(out, foreign.name))
	mustExport(t, f)
	copyBackupFixture(t, f.out, filepath.Join(out, "snapshot-unowned"))
	for day := 2; day <= 4; day++ {
		mustBackup(t, f, out, 10, day)
	}
	result := mustBackup(t, f, out, 2, 5)
	if result.RetainedSnapshots != 2 || result.PrunedSnapshots != 2 || result.ProtectedEntries != 5 {
		t.Fatalf("protected inventory aggregate differs: %+v", result)
	}
	for _, path := range []string{
		filepath.Join(out, protected.name, "keep-extra.txt"),
		filepath.Join(out, protected.name, "snapshot.json"),
		filepath.Join(out, "unknown.txt"),
		filepath.Join(out, "original-scope", "only-copy.txt"),
		filepath.Join(out, foreign.name, "snapshot.json"),
		filepath.Join(out, "snapshot-unowned", "snapshot.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("retention changed an unknown or foreign member")
		}
	}
}

func TestBackupRefusesSourceOverlapOtherSourceAndBusyLock(t *testing.T) {
	f := newFixture(t, false)
	for _, path := range []string{f.home, filepath.Join(f.home, "backups"), filepath.Dir(f.home)} {
		_, err := Backup(t.Context(), f.home, path, 3)
		requireCode(t, err, "backup_source_overlap")
	}
	out := filepath.Join(t.TempDir(), "daily")
	mustBackup(t, f, out, 3, 1)
	other := newFixture(t, false)
	_, err := Backup(t.Context(), other.home, out, 3)
	requireCode(t, err, "backup_source_mismatch")
	lock, err := os.OpenFile(filepath.Join(out, backupLockFile), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal("fixture backup lock open failed")
	}
	defer lock.Close()
	if lockFile(lock) != nil {
		t.Fatal("fixture backup lock failed")
	}
	_, err = Backup(t.Context(), f.home, out, 3)
	unlockFile(lock)
	requireCode(t, err, "backup_lock_busy")
	if len(ownedBackups(t, f.home, out)) != 1 {
		t.Fatal("refused backup changed retained snapshots")
	}
	for _, name := range []string{"", ".", "..", filepath.Join("..", "outside"), f.home} {
		if _, err := backupMember(out, name); err == nil {
			t.Fatal("absolute containment accepted a non-member")
		}
	}
}

func TestBackupMemberUsesNativePathSyntax(t *testing.T) {
	root := t.TempDir()
	// A Windows drive-relative path is a normal leaf filename on Unix. The
	// containment helper uses native syntax; backup names themselves are generated.
	const driveRelative = "C:drive-relative"
	if runtime.GOOS == "windows" {
		for _, name := range []string{driveRelative, `C:\absolute`, `\\server\share\file`, `\rooted`} {
			if _, err := backupMember(root, name); err == nil {
				t.Fatal("native Windows non-member was accepted")
			}
		}
		return
	}
	for _, name := range []string{driveRelative, "..cache"} {
		member, err := backupMember(root, name)
		if err != nil || member != filepath.Join(root, name) || !filepath.IsAbs(member) {
			t.Fatal("native leaf filename did not remain inside the absolute root")
		}
		rel, err := filepath.Rel(root, member)
		if err != nil || rel != name {
			t.Fatal("native leaf escaped its root")
		}
	}
}

func TestFailedCaptureDoesNotPruneHealthyBackups(t *testing.T) {
	f := newFixture(t, false)
	out := filepath.Join(t.TempDir(), "daily")
	mustBackup(t, f, out, 3, 1)
	mustBackup(t, f, out, 3, 2)
	writeFixture(t, filepath.Join(f.home, "todo.json"), []byte("invalid fixture snapshot"))
	result, err := backupAt(t.Context(), f.home, out, 1, backupTime(3))
	if err == nil || result.Created || result.PrunedSnapshots != 0 || len(ownedBackups(t, f.home, out)) != 2 {
		t.Fatal("failed capture pruned previous healthy snapshots")
	}
	for _, keep := range []int{0, -1} {
		_, err := Backup(t.Context(), f.home, out, keep)
		requireCode(t, err, "invalid_backup_keep")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = Backup(ctx, f.home, out, 3)
	requireCode(t, err, "cancelled")
}

func TestBackupRetainsCorruptOrLinkedGeneration(t *testing.T) {
	for _, mode := range []string{"corrupt", "symlink", "hardlink"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t, false)
			out := filepath.Join(t.TempDir(), "daily")
			mustBackup(t, f, out, 3, 1)
			old := ownedBackups(t, f.home, out)[0]
			path := filepath.Join(out, old.name, "snapshot.json")
			outside := filepath.Join(t.TempDir(), "keep-original.json")
			writeFixture(t, outside, f.raw)
			before, _ := os.Stat(outside)
			if mode == "corrupt" {
				writeFixture(t, path, []byte("changed fixture bytes"))
			} else {
				if os.Remove(path) != nil {
					t.Fatal("fixture member removal failed")
				}
				var err error
				if mode == "symlink" {
					err = os.Symlink(outside, path)
				} else {
					err = os.Link(outside, path)
				}
				if err != nil {
					t.Skip("fixture link unavailable")
				}
			}
			result := mustBackup(t, f, out, 1, 2)
			if !result.Created || result.RetainedSnapshots != 1 || result.PrunedSnapshots != 0 || result.ProtectedEntries != 1 {
				t.Fatal("invalid or linked generation was pruned")
			}
			if _, err := os.Lstat(path); err != nil {
				t.Fatal("invalid or linked member was touched")
			}
			after, _ := os.Stat(outside)
			b, _ := os.ReadFile(outside)
			if !os.SameFile(before, after) || !bytes.Equal(b, f.raw) {
				t.Fatal("retention changed an outside original")
			}
		})
	}
}

func TestBackupRefusesLinkedRootWithoutTouchingDestination(t *testing.T) {
	f := newFixture(t, false)
	outside := t.TempDir()
	writeFixture(t, filepath.Join(outside, "keep.txt"), []byte("fiction sentinel"))
	link := filepath.Join(t.TempDir(), "linked-root")
	if os.Symlink(outside, link) != nil {
		t.Skip("fixture directory link unavailable")
	}
	_, err := Backup(t.Context(), f.home, link, 3)
	requireCode(t, err, "backup_directory_unverified")
	entries, _ := os.ReadDir(outside)
	if len(entries) != 1 {
		t.Fatal("linked root received a write")
	}
}

func TestBackupLocalVerifyAndRestoreIntoEmptyCloud(t *testing.T) {
	f := newFixture(t, false)
	out := filepath.Join(t.TempDir(), "daily")
	backup := mustBackup(t, f, out, 3, 1)
	name := ownedBackups(t, f.home, out)[0].name
	path := filepath.Join(out, name)
	local, err := VerifyExport(t.Context(), path)
	if err != nil || local.Finalized || local.Counts != backup.Counts || local.VerifiedBlobs != backup.AssetFiles+1 {
		t.Fatalf("offline backup verify: %v", err)
	}
	s, cfg := fixtureCloud(t, f, nil)
	if _, err := Upload(t.Context(), cfg, path); err != nil {
		t.Fatalf("restore import: %v", err)
	}
	if _, err := Verify(t.Context(), cfg, path); err != nil {
		t.Fatalf("restore verification: %v", err)
	}
	state, err := s.Snapshot()
	if err != nil || state.Revision != 1 || !equalJSON(state.Document, f.raw) {
		t.Fatal("empty-ledger restore lost the chosen backup values")
	}
	manifest := readManifest(t, path)
	for _, ref := range manifest.References {
		if ref.Status == "missing" || ref.Status == "unverified" {
			if ref.SHA256 != "" {
				t.Fatal("restore invented data for an unacquired reference")
			}
		}
	}
	b, _ := os.ReadFile(filepath.Join(path, "manifest.json"))
	if !json.Valid(b) {
		t.Fatal("restore damaged backup metadata")
	}
}
