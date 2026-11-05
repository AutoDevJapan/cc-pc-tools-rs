package transport

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type CutoverResult struct {
	Activated      bool   `json:"activated"`
	LeaseReleased  bool   `json:"leaseReleased"`
	Revision       uint64 `json:"revision,omitempty"`
	OriginalSHA256 string `json:"originalSHA256,omitempty"`
	BackupPath     string `json:"backupPath,omitempty"`
	RecoveryPath   string `json:"recoveryPath,omitempty"`
}

// Cutover is finite and is never called by the synchronizer or a transaction.
// The caller first upgrades/restarts every legacy writer. Hold the original
// byte lock through backup, last local delta, cache install and activation.
func Cutover(ctx context.Context, cfg Config, configPath, bridgePath string) (result CutoverResult, err error) {
	return CutoverAtRevision(ctx, cfg, configPath, bridgePath, 1)
}

// The expected revision comes from the verified initial import. If another
// writer changed the cloud meanwhile, its work must be reconciled explicitly.
func CutoverAtRevision(ctx context.Context, cfg Config, configPath, bridgePath string, expectedRevision uint64) (result CutoverResult, err error) {
	if expectedRevision == 0 {
		return result, errors.New("todo_remote_cutover_expected_revision_required")
	}
	if err = cfg.Validate(); err != nil {
		return result, err
	}
	if !filepath.IsAbs(configPath) || !filepath.IsAbs(bridgePath) {
		return result, errors.New("todo_remote_cutover_absolute_paths_required")
	}
	if info, e := os.Stat(bridgePath); e != nil || info.IsDir() {
		return result, errors.New("todo_remote_cutover_bridge_unavailable")
	}
	// Verify the supplied file is actually the protected configuration that the
	// marker will reference; do not activate against a different credential/cache.
	installed, e := LoadConfig(configPath)
	if e != nil || installed != cfg {
		return result, errors.New("todo_remote_cutover_configuration_mismatch")
	}
	f, e := os.OpenFile(filepath.Join(filepath.Dir(cfg.CachePath), "todo.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return result, errors.New("todo_remote_lock_open_failed")
	}
	defer f.Close()
	if lockFile(f) != nil {
		return result, errors.New("todo_remote_lock_busy")
	}
	defer unlockFile(f)
	marker := filepath.Join(filepath.Dir(cfg.CachePath), "todo.remote.json")
	if _, e = os.Stat(marker); e == nil {
		return result, errors.New("todo_remote_already_active")
	} else if !errors.Is(e, os.ErrNotExist) {
		return result, errors.New("todo_remote_configuration_unavailable")
	}
	original, e := readFileLimit(cfg.CachePath, MaxDocumentBytes)
	if e != nil {
		return result, errors.New("todo_remote_cutover_original_unavailable")
	}
	if _, e = parseDocument(original); e != nil {
		return result, e
	}
	result.OriginalSHA256 = digest(original)
	result.BackupPath = cfg.CachePath + ".cutover-original-" + result.OriginalSHA256
	if previous, e := readFileLimit(result.BackupPath, MaxDocumentBytes); e == nil {
		if !bytes.Equal(previous, original) {
			return result, errors.New("todo_remote_cutover_backup_mismatch")
		}
	} else if errors.Is(e, os.ErrNotExist) {
		if e = atomicWrite(result.BackupPath, original); e != nil {
			return result, e
		}
	} else {
		return result, errors.New("todo_remote_cutover_backup_unavailable")
	}
	// Begin may replace its read cache before commit. Give it a separate staging
	// cache so neither a failed pull nor an uncertain commit erases the latest
	// local authoritative bytes. The durable raw backup survives every outcome.
	stage := filepath.Join(filepath.Dir(cfg.CachePath), ".todo-cutover-"+result.OriginalSHA256)
	owner := wire(map[string]string{"format": "todo-cutover-v1", "originalSHA256": result.OriginalSHA256, "namespace": namespace(cfg.BaseURL, cfg.ClientID)})
	if e = os.Mkdir(stage, 0700); e == nil {
		e = atomicWrite(filepath.Join(stage, "cutover.json"), owner)
	} else if errors.Is(e, os.ErrExist) {
		var info os.FileInfo
		info, e = os.Lstat(stage)
		if e == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
			e = errors.New("invalid stage")
		}
		if e == nil {
			var previous []byte
			previous, e = readFileLimit(filepath.Join(stage, "cutover.json"), 4096)
			if e == nil && !bytes.Equal(previous, owner) {
				e = errors.New("stage owner mismatch")
			}
		}
	}
	if e != nil {
		return result, errors.New("todo_remote_cutover_stage_failed")
	}
	result.RecoveryPath = stage
	defer func() {
		if !result.Activated && needsFresh(filepath.Join(stage, "todo.json")) {
			return
		}
		if cleanupErr := removeCutoverStage(stage, filepath.Dir(cfg.CachePath)); cleanupErr != nil && err == nil {
			err = cleanupErr
		} else if cleanupErr == nil {
			result.RecoveryPath = ""
		}
	}()
	client, e := NewClient(cfg.BaseURL, cfg.SyncToken)
	if e != nil {
		return result, e
	}
	session, e := Begin(ctx, client, cfg.ClientID, filepath.Join(stage, "todo.json"))
	if e != nil {
		return result, e
	}
	defer func() {
		closeErr := session.Close()
		result.LeaseReleased = closeErr == nil
		if closeErr != nil && err == nil {
			err = errors.New("todo_remote_cutover_release_pending")
		}
	}()
	if e = session.SetOwnedHome(filepath.Dir(cfg.CachePath)); e != nil {
		return result, e
	}
	if session.Revision() == 0 {
		return result, errors.New("todo_remote_cutover_cloud_not_initialized")
	}
	if session.Revision() != expectedRevision && session.resolution != "pending_resolved_applied" {
		return result, errors.New("todo_remote_cutover_cloud_changed")
	}
	if session.Revision() > 1 {
		candidate, parseErr := parseDocument(original)
		if parseErr != nil {
			return result, parseErr
		}
		session.mu.Lock()
		cloud, cloudErr := parseDocument(session.canonical)
		changed := false
		if cloudErr == nil {
			cloud.preserveUnchanged(candidate)
			_, changed = cloud.diff(candidate)
		}
		session.mu.Unlock()
		if cloudErr != nil {
			return result, cloudErr
		}
		if changed {
			return result, errors.New("todo_remote_cutover_changed_cloud_requires_reconciliation")
		}
	}
	receipt, e := session.Commit(ctx, original)
	result.Revision = receipt.Revision
	if e != nil {
		return result, e
	}
	stageCache := filepath.Join(stage, "todo.json")
	canonical, revision, e := loadCache(stageCache)
	if e != nil || revision != receipt.Revision || needsFresh(stageCache) {
		return result, errors.New("todo_remote_cutover_cache_unconfirmed")
	}
	cacheBytes, e := canonical.bytes()
	if e != nil {
		return result, e
	}
	if e = saveCache(cfg.CachePath, cacheBytes, revision, session.StateID()); e != nil {
		return result, e
	}
	if e = atomicWrite(marker, wire(map[string]string{"bridge": bridgePath, "credentials": configPath})); e != nil {
		return result, e
	}
	result.Activated = true
	return result, nil
}

func removeCutoverStage(stage, parent string) error {
	rel, err := filepath.Rel(parent, stage)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || !strings.HasPrefix(filepath.Base(stage), ".todo-cutover-") {
		return errors.New("todo_remote_cutover_stage_path_invalid")
	}
	if os.RemoveAll(stage) != nil {
		return errors.New("todo_remote_cutover_stage_cleanup_failed")
	}
	return nil
}
