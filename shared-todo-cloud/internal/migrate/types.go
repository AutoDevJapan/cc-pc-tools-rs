// Package migrate exports a locked legacy ledger and imports its exact bytes.
// It never edits the source ledger or follows references outside its home.
package migrate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

const (
	Format           = "shared-todo-export-v1"
	HistoryDomain    = "cc-todo-note-history-v1"
	ChunkBytes       = 4 << 20
	MaxAssetBytes    = 128 << 20
	maxManifestBytes = 32 << 20
)

// Error contains only a fixed machine code and an optional HTTP status, never
// filesystem errors, remote response bodies, task text or configuration values.
type Error struct {
	Code       string
	HTTPStatus int
}

func (e *Error) Error() string {
	if e.HTTPStatus != 0 {
		return fmt.Sprintf("%s_%d", e.Code, e.HTTPStatus)
	}
	return e.Code
}

func failure(code string) error { return &Error{Code: code} }

func ErrorCode(err error) (string, int) {
	var e *Error
	if errors.As(err, &e) {
		return e.Code, e.HTTPStatus
	}
	return "migration_failed", 0
}

type File struct {
	File         string `json:"file"`
	SHA256       string `json:"sha256"`
	Bytes        int64  `json:"bytes"`
	OriginalPath string `json:"originalPath"`
}

type Asset struct {
	File
	Kind    string `json:"kind"`
	Entries uint64 `json:"entries,omitempty"`
}

// Exists is unknown for an external or linked path. Missing, unreadable and
// unverified are distinct outcomes; none is replaced with an empty document.
type Reference struct {
	ItemID  int64  `json:"itemId"`
	Field   string `json:"field"`
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Status  string `json:"status"`
	Exists  *bool  `json:"exists"`
	SHA256  string `json:"sha256,omitempty"`
	Reason  string `json:"reason,omitempty"`
	Entries uint64 `json:"entries,omitempty"`
}

// Counts deliberately contains no status labels, field names, titles or paths.
type Counts struct {
	Items             int    `json:"items"`
	RootFields        int    `json:"rootFields"`
	ItemFields        int    `json:"itemFields"`
	StatusKinds       int    `json:"statusKinds"`
	ArchiveReferences int    `json:"archiveReferences"`
	ArchiveEntries    uint64 `json:"archiveEntries"`
	SpecReferences    int    `json:"specReferences"`
	SpecImported      int    `json:"specImported"`
	SpecMissing       int    `json:"specMissing"`
	SpecUnavailable   int    `json:"specUnavailable"`
	SpecUnverified    int    `json:"specUnverified"`
	AssetFiles        int    `json:"assetFiles"`
	AssetAliases      int    `json:"assetAliases"`
	AssetBytes        int64  `json:"assetBytes"`
}

type Manifest struct {
	Format     string      `json:"format"`
	CreatedAt  time.Time   `json:"createdAt"`
	Root       File        `json:"root"`
	Counts     Counts      `json:"counts"`
	Assets     []Asset     `json:"assets"`
	References []Reference `json:"references"`
}

// Summary is the only CLI result. The manifest and snapshot stay on disk.
type Summary struct {
	Counts
	RootSHA256    string `json:"rootSHA256"`
	RootBytes     int64  `json:"rootBytes"`
	ChunkCount    int    `json:"chunkCount"`
	Revision      uint64 `json:"revision,omitempty"`
	VerifiedBlobs int    `json:"verifiedBlobs"`
	Finalized     bool   `json:"finalized"`
}

func summarize(m Manifest) Summary {
	return Summary{Counts: m.Counts, RootSHA256: m.Root.SHA256,
		RootBytes: m.Root.Bytes, ChunkCount: int((m.Root.Bytes + ChunkBytes - 1) / ChunkBytes)}
}

func digest(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func validDigest(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && hex.EncodeToString(b) == s
}
