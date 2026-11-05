// Package assetio uploads newly referenced local assets before a ledger commit.
// It only reads ordinary files beneath the supplied home and never edits items,
// cache files, configuration or the filesystem. The caller owns the lease and
// commits the original raw JSON only after UploadNew succeeds.
package assetio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	MaxDocumentBytes = 64 << 20
	MaxAssetBytes    = 128 << 20
	HistoryDomain    = "cc-todo-note-history-v1"
)

// Error values are fixed codes. Filesystem paths, task text, parser errors and
// callback errors are never retained, wrapped or included in returned errors.
type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrInvalidInput     Error = "assetio_invalid_input"
	ErrDocumentTooLarge Error = "assetio_document_too_large"
	ErrHomeUnavailable  Error = "assetio_home_unavailable"
	ErrPathUnverified   Error = "assetio_path_unverified"
	ErrFileMissing      Error = "assetio_file_missing"
	ErrFileUnavailable  Error = "assetio_file_unavailable"
	ErrAssetTooLarge    Error = "assetio_asset_too_large"
	ErrArchiveReference Error = "assetio_invalid_archive_reference"
	ErrArchiveDigest    Error = "assetio_archive_digest_or_size_mismatch"
	ErrArchiveDocument  Error = "assetio_invalid_archive_document"
	ErrAliasConflict    Error = "assetio_alias_conflict"
	ErrUpload           Error = "assetio_upload_failed"
	ErrCancelled        Error = "assetio_cancelled"
)

// PutFunc must synchronously PUT the exact raw bytes under both their digest and
// path alias. It must accept repeat uploads of identical sha/path/kind/bytes and
// treat raw as read-only. UploadNew sends every alias, even for identical bytes.
type PutFunc func(ctx context.Context, sha, path, kind string, raw []byte) error

type fileGroup struct {
	rel      string
	kind     string
	aliases  []string
	archives []archiveRef
}

// UploadNew compares only touched item IDs against before, without walking the
// entire ledger. Existing paths, including missing/unretrieved legacy sources,
// are not reopened. New external references are preserved without probing them.
// New owned sources must validate before their PUT; errors stop subsequent PUTs.
// There is no local receipt or mutation: after partial failure, retry with the
// same before/upserts and unchanged asset files using an idempotent PutFunc.
// Metadata changes alone do not discover changes to an unchanged spec path.
func UploadNew(ctx context.Context, before map[int64]json.RawMessage, upserts []json.RawMessage, home string, put PutFunc) error {
	if err := cancelled(ctx); err != nil {
		return err
	}
	refs, err := newReferences(ctx, before, upserts)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return nil
	}
	if strings.TrimSpace(home) == "" || forbidden(home) {
		return ErrHomeUnavailable
	}
	home, err = filepath.Abs(home)
	if err != nil {
		return ErrHomeUnavailable
	}
	groups, err := plan(home, refs)
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return nil
	}
	if put == nil {
		return ErrInvalidInput
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return ErrHomeUnavailable
	}
	defer root.Close()
	for _, group := range groups {
		if err := cancelled(ctx); err != nil {
			return err
		}
		raw, err := readFile(ctx, root, group.rel)
		if err != nil {
			return err
		}
		sha := digest(raw)
		for _, expected := range group.archives {
			if sha != expected.sha || int64(len(raw)) != expected.bytes {
				return ErrArchiveDigest
			}
			if err := validateArchive(raw, expected.id, expected.entries); err != nil {
				return err
			}
		}
		for _, path := range group.aliases {
			if err := cancelled(ctx); err != nil {
				return err
			}
			if err := put(ctx, sha, path, group.kind, raw); err != nil {
				if err := cancelled(ctx); err != nil {
					return err
				}
				return ErrUpload
			}
		}
	}
	return cancelled(ctx)
}

func plan(home string, refs []reference) ([]fileGroup, error) {
	var groups []fileGroup
	byFile := map[string]int{}
	byAlias := map[string]int{}
	for _, ref := range refs {
		if ref.archive != nil {
			ref.path = filepath.Join(home, ref.path)
		}
		rel, owned, err := localReference(home, ref.path)
		if err != nil {
			return nil, err
		}
		if !owned {
			continue
		}
		fileKey := rel
		i, found := byFile[fileKey]
		if !found {
			i = len(groups)
			byFile[fileKey] = i
			groups = append(groups, fileGroup{rel: rel, kind: ref.kind})
		}
		if groups[i].kind != ref.kind {
			return nil, ErrAliasConflict
		}
		if ref.archive != nil {
			groups[i].archives = append(groups[i].archives, *ref.archive)
		}
		for _, path := range []string{ref.path, filepath.Join(home, rel)} {
			if prior, exists := byAlias[path]; exists {
				if prior != i {
					return nil, ErrAliasConflict
				}
				continue
			}
			byAlias[path] = i
			groups[i].aliases = append(groups[i].aliases, path)
		}
	}
	return groups, nil
}

func cancelled(ctx context.Context) error {
	if ctx == nil {
		return ErrInvalidInput
	}
	if ctx.Err() != nil {
		return ErrCancelled
	}
	return nil
}

func digest(raw []byte) string {
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
