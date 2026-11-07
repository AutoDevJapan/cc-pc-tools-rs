package migrate

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	DefaultBackupKeep = 3
	backupRootFile    = ".todo-cloud-backup-root.json"
	backupLockFile    = ".todo-cloud-backup.lock"
	backupReceiptFile = "backup.json"
	backupRootFormat  = "shared-todo-backup-root-v1"
	backupFormat      = "shared-todo-backup-v1"
)

type BackupSummary struct {
	Counts
	Keep              int   `json:"keep"`
	Created           bool  `json:"created"`
	Reused            bool  `json:"reused"`
	RetainedSnapshots int   `json:"retainedSnapshots"`
	RetainedBytes     int64 `json:"retainedBytes"`
	PrunedSnapshots   int   `json:"prunedSnapshots"`
	PrunedBytes       int64 `json:"prunedBytes"`
	ProtectedEntries  int   `json:"protectedEntries"`
	PruneFailures     int   `json:"pruneFailures"`
	SnapshotBytes     int64 `json:"snapshotBytes"`
	InventoryComplete bool  `json:"inventoryComplete"`
}

type backupOwner struct {
	Format     string `json:"format"`
	ID         string `json:"id"`
	SourcePath string `json:"sourcePath"`
}

type backupReceipt struct {
	Format         string    `json:"format"`
	OwnerID        string    `json:"ownerId"`
	SourcePath     string    `json:"sourcePath"`
	CreatedAt      time.Time `json:"createdAt"`
	Date           string    `json:"date"`
	Generation     uint64    `json:"generation"`
	Nonce          string    `json:"nonce"`
	ContentSHA256  string    `json:"contentSHA256"`
	ManifestSHA256 string    `json:"manifestSHA256"`
}

func (r backupReceipt) name() string {
	return fmt.Sprintf("snapshot-%s-%020d-%s", r.Date, r.Generation, r.Nonce)
}

type backupFile struct {
	path  string
	sha   string
	bytes int64
	info  os.FileInfo
}

type savedBackup struct {
	name    string
	receipt backupReceipt
	counts  Counts
	files   []backupFile
	bytes   int64
	info    os.FileInfo
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", failure("backup_random_failed")
	}
	return hex.EncodeToString(b), nil
}

func isHex(s string, n int) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == n && hex.EncodeToString(b) == s
}

func sameDirectoryPath(a, b string) bool {
	rel, err := filepath.Rel(a, b)
	return err == nil && rel == "."
}

// Reject links at every directory component. os.Root then pins the actual
// directory used by validation and retention rather than following later links.
func backupDirectory(path string, create bool) (*os.Root, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil || strings.TrimSpace(path) == "" || forbiddenPath(abs) {
		return nil, "", failure("invalid_backup_directory")
	}
	volume := filepath.VolumeName(abs) + string(filepath.Separator)
	root, err := os.OpenRoot(volume)
	if err != nil {
		return nil, "", failure("backup_directory_unavailable")
	}
	defer root.Close()
	rel, err := filepath.Rel(volume, abs)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return nil, "", failure("invalid_backup_directory")
	}
	var part string
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		part = filepath.Join(part, component)
		info, err := root.Lstat(part)
		if os.IsNotExist(err) && create {
			if root.Mkdir(part, 0700) != nil {
				return nil, "", failure("backup_directory_unavailable")
			}
			info, err = root.Lstat(part)
		}
		if err != nil || !info.IsDir() || linked(info) {
			return nil, "", failure("backup_directory_unverified")
		}
	}
	child, err := root.OpenRoot(rel)
	if err != nil {
		return nil, "", failure("backup_directory_unavailable")
	}
	return child, abs, nil
}

func backupMember(root, name string) (string, error) {
	if !filepath.IsAbs(root) || name == "." || filepath.Base(name) != name || !filepath.IsLocal(name) || forbiddenPath(name) {
		return "", failure("backup_path_outside_root")
	}
	abs := filepath.Join(root, name)
	if _, ok := within(root, abs); !ok {
		return "", failure("backup_path_outside_root")
	}
	return abs, nil
}

func writeBackupFile(root *os.Root, name string, raw []byte) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return failure("backup_write_failed")
	}
	_, err = f.Write(raw)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return failure("backup_write_failed")
	}
	return nil
}

func backupLock(root *os.Root) (*os.File, error) {
	if info, err := root.Lstat(backupLockFile); err == nil {
		if linked(info) || !info.Mode().IsRegular() || info.Size() != 0 {
			return nil, failure("backup_lock_unverified")
		}
	} else if !os.IsNotExist(err) {
		return nil, failure("backup_lock_unavailable")
	}
	f, err := root.OpenFile(backupLockFile, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, failure("backup_lock_unavailable")
	}
	if !singleLink(f) || lockFile(f) != nil {
		f.Close()
		return nil, failure("backup_lock_busy")
	}
	return f, nil
}

func loadBackupOwner(root *os.Root, source string) (backupOwner, error) {
	if _, err := root.Lstat(backupRootFile); os.IsNotExist(err) {
		id, err := randomHex(16)
		if err != nil {
			return backupOwner{}, err
		}
		owner := backupOwner{Format: backupRootFormat, ID: id, SourcePath: source}
		b, _ := json.Marshal(owner)
		if err := writeBackupFile(root, backupRootFile, b); err != nil {
			return backupOwner{}, err
		}
		return owner, nil
	}
	b, err := readUnder(root, backupRootFile, 64<<10)
	var owner backupOwner
	if err != nil || !backupMetadata(b, &owner) || owner.Format != backupRootFormat || !isHex(owner.ID, 16) {
		return owner, failure("backup_owner_unverified")
	}
	if owner.SourcePath != source {
		return owner, failure("backup_source_mismatch")
	}
	f, err := root.Open(backupRootFile)
	if err != nil {
		return owner, failure("backup_owner_unverified")
	}
	valid := singleLink(f)
	f.Close()
	if !valid {
		return owner, failure("backup_owner_unverified")
	}
	return owner, nil
}

func backupMetadata(b []byte, out any) bool {
	if validateJSON(b) != nil {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(out) == nil
}

func backupContent(m Manifest) string {
	// Capture asset changes and missing/unverified reference states too. A root
	// digest alone cannot prove that specifications still have the same bytes.
	b, _ := json.Marshal(struct {
		Root       File
		Counts     Counts
		Assets     []Asset
		References []Reference
	}{m.Root, m.Counts, m.Assets, m.References})
	return digest(b)
}

func backupLayout(root *os.Root, m Manifest, receipt bool) ([]backupFile, int64, error) {
	files := map[string]backupFile{}
	for _, a := range m.Assets {
		files[filepath.FromSlash(a.File.File)] = backupFile{path: filepath.FromSlash(a.File.File), sha: a.SHA256, bytes: a.Bytes}
	}
	files["snapshot.json"] = backupFile{path: "snapshot.json", sha: m.Root.SHA256, bytes: m.Root.Bytes}
	metadata := []string{"manifest.json"}
	if receipt {
		metadata = append(metadata, backupReceiptFile)
	}
	for _, name := range metadata {
		b, err := readUnder(root, name, maxManifestBytes)
		if err != nil {
			return nil, 0, failure("backup_layout_unverified")
		}
		files[name] = backupFile{path: name, sha: digest(b), bytes: int64(len(b))}
	}
	if err := backupMembers(root, files); err != nil {
		return nil, 0, err
	}
	var out []backupFile
	var size int64
	for name, file := range files {
		if ordinary(root, name) != nil {
			return nil, 0, failure("backup_layout_unverified")
		}
		f, err := root.Open(name)
		if err != nil {
			return nil, 0, failure("backup_layout_unverified")
		}
		info, statErr := f.Stat()
		valid := statErr == nil && info.Size() == file.bytes && singleLink(f)
		f.Close()
		if !valid {
			return nil, 0, failure("backup_layout_unverified")
		}
		file.info = info
		out = append(out, file)
		size += file.bytes
	}
	// Keep the receipt until last, so a partial deletion never looks like a
	// complete ordinary export that the retention routine may claim later.
	sort.Slice(out, func(i, j int) bool {
		if out[i].path == backupReceiptFile {
			return false
		}
		if out[j].path == backupReceiptFile {
			return true
		}
		return out[i].path < out[j].path
	})
	return out, size, nil
}

func backupMembers(root *os.Root, files map[string]backupFile) error {
	for _, dir := range []string{".", "assets"} {
		if dir != "." {
			info, err := root.Lstat(dir)
			if err != nil || linked(info) || !info.IsDir() {
				return failure("backup_layout_unverified")
			}
		}
		f, err := root.Open(dir)
		if err != nil {
			return failure("backup_layout_unverified")
		}
		entries, err := f.ReadDir(-1)
		f.Close()
		if err != nil {
			return failure("backup_layout_unverified")
		}
		for _, entry := range entries {
			if dir == "." && entry.Name() == "assets" {
				continue
			}
			if _, ok := files[filepath.Join(dir, entry.Name())]; !ok {
				return failure("backup_has_unknown_files")
			}
		}
	}
	return nil
}

func inspectBackup(ctx context.Context, root *os.Root, name string, owner backupOwner) (savedBackup, error) {
	info, err := root.Lstat(name)
	if err != nil || linked(info) || !info.IsDir() {
		return savedBackup{}, failure("backup_generation_unverified")
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return savedBackup{}, failure("backup_generation_unverified")
	}
	opened, err := child.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		child.Close()
		return savedBackup{}, failure("backup_generation_unverified")
	}
	// Establish ownership before reading an unknown directory's payload files.
	b, err := readUnder(child, backupReceiptFile, 64<<10)
	var receipt backupReceipt
	if err != nil || !backupMetadata(b, &receipt) || receipt.Format != backupFormat || receipt.OwnerID != owner.ID || receipt.SourcePath != owner.SourcePath || receipt.Generation == 0 || !isHex(receipt.Nonce, 8) || receipt.CreatedAt.IsZero() || receipt.Date != receipt.CreatedAt.In(time.FixedZone("JST", 9*3600)).Format("20060102") || receipt.name() != name {
		child.Close()
		return savedBackup{}, failure("backup_receipt_unverified")
	}
	manifest, err := readUnder(child, "manifest.json", maxManifestBytes)
	if err != nil || digest(manifest) != receipt.ManifestSHA256 {
		child.Close()
		return savedBackup{}, failure("backup_receipt_unverified")
	}
	p, err := loadExportRoot(ctx, child)
	if err != nil {
		return savedBackup{}, err
	}
	defer p.root.Close()
	if p.manifest.Root.OriginalPath != owner.SourcePath || receipt.ContentSHA256 != backupContent(p.manifest) {
		return savedBackup{}, failure("backup_receipt_unverified")
	}
	files, size, err := backupLayout(p.root, p.manifest, true)
	if err != nil {
		return savedBackup{}, err
	}
	return savedBackup{name: name, receipt: receipt, files: files, bytes: size, counts: p.manifest.Counts, info: info}, nil
}

func scanBackups(ctx context.Context, root *os.Root, owner backupOwner) ([]savedBackup, int, error) {
	f, err := root.Open(".")
	if err != nil {
		return nil, 0, failure("backup_directory_unavailable")
	}
	defer f.Close()
	var found []savedBackup
	protected := 0
	for {
		entries, err := f.ReadDir(100)
		if err != nil && err != io.EOF {
			return nil, protected, failure("backup_directory_unavailable")
		}
		for _, entry := range entries {
			if entry.Name() == backupRootFile || entry.Name() == backupLockFile {
				continue
			}
			if cancelled(ctx) != nil {
				return nil, protected, failure("cancelled")
			}
			if !strings.HasPrefix(entry.Name(), "snapshot-") {
				protected++
				continue
			}
			backup, err := inspectBackup(ctx, root, entry.Name(), owner)
			if err != nil {
				protected++
				continue
			}
			found = append(found, backup)
		}
		if err == io.EOF {
			break
		}
	}
	sort.Slice(found, func(i, j int) bool {
		a, b := found[i].receipt, found[j].receipt
		if a.Generation != b.Generation {
			return a.Generation > b.Generation
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return found[i].name > found[j].name
	})
	return found, protected, nil
}

// Delete listed, rechecked members only; never recursively remove a directory
// that might have acquired unknown files. Absolute containment is checked as
// well as using the held os.Root for every filesystem operation.
func removeBackup(ctx context.Context, root *os.Root, name string, files []backupFile, directoryInfo os.FileInfo) error {
	if _, err := backupMember(root.Name(), name); err != nil {
		return err
	}
	info, err := root.Lstat(name)
	if err != nil || linked(info) || !os.SameFile(info, directoryInfo) {
		return failure("backup_prune_unverified")
	}
	child, err := root.OpenRoot(name)
	if err != nil {
		return failure("backup_prune_unverified")
	}
	expected := make(map[string]backupFile, len(files))
	for _, file := range files {
		expected[file.path] = file
	}
	if err := backupMembers(child, expected); err != nil {
		child.Close()
		return err
	}
	assetsInfo, err := child.Lstat("assets")
	if err != nil {
		child.Close()
		return failure("backup_prune_unverified")
	}
	for _, file := range files {
		if file.path == backupReceiptFile {
			continue
		}
		if err := removeBackupFile(ctx, child, file); err != nil {
			child.Close()
			return err
		}
	}
	currentAssets, err := child.Lstat("assets")
	if err != nil || linked(currentAssets) || !os.SameFile(assetsInfo, currentAssets) || child.Remove("assets") != nil {
		child.Close()
		return failure("backup_prune_failed")
	}
	if receipt, exists := expected[backupReceiptFile]; exists {
		if err := removeBackupFile(ctx, child, receipt); err != nil {
			child.Close()
			return err
		}
	}
	child.Close()
	current, err := root.Lstat(name)
	if err != nil || linked(current) || !os.SameFile(current, directoryInfo) || root.Remove(name) != nil {
		return failure("backup_prune_failed")
	}
	return nil
}

func removeBackupFile(ctx context.Context, root *os.Root, file backupFile) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	if !filepath.IsLocal(file.path) {
		return failure("backup_path_outside_root")
	}
	if _, ok := within(root.Name(), filepath.Join(root.Name(), file.path)); !ok {
		return failure("backup_path_outside_root")
	}
	current, err := root.Lstat(file.path)
	if err != nil || linked(current) || !os.SameFile(current, file.info) {
		return failure("backup_prune_unverified")
	}
	b, err := readUnder(root, file.path, max(MaxAssetBytes, maxManifestBytes))
	if err != nil || int64(len(b)) != file.bytes || digest(b) != file.sha {
		return failure("backup_prune_unverified")
	}
	f, err := root.Open(file.path)
	if err != nil {
		return failure("backup_prune_unverified")
	}
	valid := singleLink(f)
	f.Close()
	if !valid || root.Remove(file.path) != nil {
		return failure("backup_prune_failed")
	}
	return nil
}

// Backup performs one finite daily capture. Scheduling is owned by its caller.
func Backup(ctx context.Context, home, out string, keep int) (BackupSummary, error) {
	return backupAt(ctx, home, out, keep, time.Now())
}

func backupAt(ctx context.Context, home, out string, keep int, at time.Time) (summary BackupSummary, resultErr error) {
	summary = BackupSummary{Keep: keep}
	defer func() {
		if resultErr != nil {
			summary.InventoryComplete = false
		}
	}()
	if keep < 1 {
		return summary, failure("invalid_backup_keep")
	}
	if err := cancelled(ctx); err != nil {
		return summary, err
	}
	source, home, err := backupDirectory(home, false)
	if err != nil {
		return summary, err
	}
	source.Close()
	outAbs, err := filepath.Abs(out)
	if err != nil || strings.TrimSpace(out) == "" {
		return summary, failure("invalid_backup_directory")
	}
	_, insideSource := within(home, outAbs)
	_, includesSource := within(outAbs, home)
	if sameDirectoryPath(home, outAbs) || insideSource || includesSource {
		return summary, failure("backup_source_overlap")
	}
	root, rootPath, err := backupDirectory(outAbs, true)
	if err != nil {
		return summary, err
	}
	defer root.Close()
	lock, err := backupLock(root)
	if err != nil {
		return summary, err
	}
	defer lock.Close()
	defer unlockFile(lock)
	owner, err := loadBackupOwner(root, filepath.Join(home, "todo.json"))
	if err != nil {
		return summary, err
	}
	backups, protected, err := scanBackups(ctx, root, owner)
	summary.ProtectedEntries = protected
	if err != nil {
		return summary, err
	}
	for _, backup := range backups {
		summary.RetainedSnapshots++
		summary.RetainedBytes += backup.bytes
	}
	summary.InventoryComplete = true
	nonce, err := randomHex(8)
	if err != nil {
		return summary, err
	}
	pending := ".pending-backup-" + nonce
	path, err := backupMember(rootPath, pending)
	if err != nil {
		return summary, err
	}
	if _, err := Export(ctx, home, path); err != nil {
		return summary, err
	}
	child, err := root.OpenRoot(pending)
	if err != nil {
		return summary, failure("backup_capture_unverified")
	}
	p, err := loadExportRoot(ctx, child)
	if err != nil {
		return summary, err
	}
	summary.Counts = p.manifest.Counts
	content := backupContent(p.manifest)
	day := at.In(time.FixedZone("JST", 9*3600)).Format("20060102")
	var reused *savedBackup
	if len(backups) > 0 && backups[0].receipt.Date == day && backups[0].receipt.ContentSHA256 == content {
		reused = &backups[0]
	}
	if reused != nil {
		files, _, err := backupLayout(p.root, p.manifest, false)
		info, statErr := p.root.Stat(".")
		p.root.Close()
		if err != nil || statErr != nil {
			return summary, failure("backup_capture_unverified")
		}
		if err := removeBackup(ctx, root, pending, files, info); err != nil {
			return summary, err
		}
		summary.Reused, summary.SnapshotBytes = true, reused.bytes
	} else {
		generation := uint64(1)
		if len(backups) > 0 {
			if backups[0].receipt.Generation == math.MaxUint64 {
				p.root.Close()
				return summary, failure("backup_generation_overflow")
			}
			generation = backups[0].receipt.Generation + 1
		}
		manifest, err := readUnder(p.root, "manifest.json", maxManifestBytes)
		if err != nil {
			p.root.Close()
			return summary, err
		}
		receipt := backupReceipt{Format: backupFormat, OwnerID: owner.ID, SourcePath: owner.SourcePath, CreatedAt: at.UTC(), Date: day, Generation: generation, Nonce: nonce, ContentSHA256: content, ManifestSHA256: digest(manifest)}
		b, _ := json.Marshal(receipt)
		err = writeBackupFile(p.root, backupReceiptFile, b)
		p.root.Close()
		if err != nil {
			return summary, err
		}
		name := receipt.name()
		if _, err := backupMember(rootPath, name); err != nil {
			return summary, err
		}
		if root.Rename(pending, name) != nil {
			return summary, failure("backup_publish_failed")
		}
		created, err := inspectBackup(ctx, root, name, owner)
		if err != nil {
			return summary, err
		}
		summary.Created, summary.SnapshotBytes = true, created.bytes
		summary.RetainedSnapshots++
		summary.RetainedBytes += created.bytes
		backups = append([]savedBackup{created}, backups...)
	}
	for _, old := range backups[min(keep, len(backups)):] {
		checked, err := inspectBackup(ctx, root, old.name, owner)
		if err != nil || !os.SameFile(old.info, checked.info) {
			summary.PruneFailures++
			summary.InventoryComplete = false
			return summary, failure("backup_prune_unverified")
		}
		if err := removeBackup(ctx, root, checked.name, checked.files, checked.info); err != nil {
			summary.PruneFailures++
			summary.InventoryComplete = false
			return summary, err
		}
		summary.PrunedSnapshots++
		summary.PrunedBytes += checked.bytes
		summary.RetainedSnapshots--
		summary.RetainedBytes -= checked.bytes
	}
	return summary, nil
}
