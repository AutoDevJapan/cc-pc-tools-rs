// Package store provides the durable ledger shared by the HTTP and local clients.
// All ledger mutations, revision checks, leases and operation receipts use a single
// bbolt write transaction. It does not perform network or authentication work.
package store

import (
	"encoding/json"
	"fmt"
	"time"
)

const (
	LeaseDuration = 90 * time.Second
	MaxChunkBytes = 4 << 20
	DefaultLimit  = 100
	MaxLimit      = 1000
	SnapshotKind  = "snapshot"
)

type Code string

const (
	CodeConflict Code = "conflict"
	CodeLocked   Code = "locked"
	CodeNotFound Code = "not_found"
	CodeInvalid  Code = "invalid"
	CodeClosed   Code = "closed"
)

// Error is suitable for errors.As and errors.Is. An HTTP adapter can map
// conflict/locked/not_found/invalid to 409/423/404/400 without parsing messages.
// Revision is the observed ledger revision when relevant to a conflict.
type Error struct {
	Code     Code   `json:"code"`
	Reason   string `json:"reason,omitempty"`
	Revision uint64 `json:"revision,omitempty"`
}

func (e *Error) Error() string {
	if e.Reason == "" {
		return string(e.Code)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Reason)
}

func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	return ok && e.Code == t.Code && (t.Reason == "" || e.Reason == t.Reason)
}

var (
	ErrConflict     = &Error{Code: CodeConflict}
	ErrLocked       = &Error{Code: CodeLocked}
	ErrNotFound     = &Error{Code: CodeNotFound}
	ErrInvalid      = &Error{Code: CodeInvalid}
	ErrClosed       = &Error{Code: CodeClosed}
	ErrLeaseExpired = &Error{Code: CodeLocked, Reason: "lease_expired"}
)

type Snapshot struct {
	Revision uint64          `json:"revision"`
	StateID  string          `json:"stateId,omitempty"`
	Document json.RawMessage `json:"document"`
}

type Item struct {
	Revision uint64          `json:"revision"`
	Item     json.RawMessage `json:"item"`
}

// Summary deliberately excludes notes/specs and retains the JSON types of the
// indexed fields. A missing field stays absent and an explicit null stays null.
type Summary struct {
	ID       int64           `json:"id"`
	Title    json.RawMessage `json:"title,omitempty"`
	Status   json.RawMessage `json:"status,omitempty"`
	Project  json.RawMessage `json:"project,omitempty"`
	Priority json.RawMessage `json:"priority,omitempty"`
	Progress json.RawMessage `json:"progress,omitempty"`
}

type ListOptions struct {
	Status   string
	Project  string
	OpenOnly bool
	// Search is a case-insensitive substring of the decimal ID or string title.
	// It filters the summary cursor before pagination is applied.
	Search string
	After  int64
	Limit  int
}

type SummaryPage struct {
	Revision  uint64    `json:"revision"`
	Items     []Summary `json:"items"`
	NextAfter int64     `json:"nextAfter"`
	HasMore   bool      `json:"hasMore"`
}

type Lease struct {
	ID        string    `json:"id"`
	ClientID  string    `json:"clientId"`
	Revision  uint64    `json:"revision"`
	StateID   string    `json:"stateId,omitempty"`
	TTLMillis int64     `json:"ttlMs,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type CommitRequest struct {
	RequestID    string            `json:"requestId"`
	LeaseID      string            `json:"leaseId"`
	BaseRevision uint64            `json:"baseRevision"`
	BaseStateID  string            `json:"baseStateId,omitempty"`
	Upserts      []json.RawMessage `json:"upserts"`
	DeleteIDs    []int64           `json:"deleteIds"`
	// Nil means unchanged. A present value must be an object excluding items;
	// it replaces the complete root metadata, including all unknown fields.
	Meta json.RawMessage `json:"meta,omitempty"`
}

// UpdateRequest is the Web mutation path. It checks the revision and refuses an
// active external lease, without acquiring/replacing/renewing that lease. Creates
// must omit id; their IDs are allocated inside the same write transaction.
type UpdateRequest struct {
	RequestID    string            `json:"requestId"`
	BaseRevision uint64            `json:"baseRevision"`
	BaseStateID  string            `json:"baseStateId,omitempty"`
	Creates      []json.RawMessage `json:"creates"`
	Upserts      []json.RawMessage `json:"upserts"`
	DeleteIDs    []int64           `json:"deleteIds"`
	Meta         json.RawMessage   `json:"meta,omitempty"`
}

type CommitResult struct {
	Revision   uint64  `json:"revision"`
	StateID    string  `json:"stateId,omitempty"`
	Duplicate  bool    `json:"duplicate"`
	Upserts    int     `json:"upserts"`
	Deleted    int     `json:"deleted"`
	CreatedIDs []int64 `json:"createdIds,omitempty"`
}

type Change struct {
	Revision      uint64            `json:"revision"`
	StateID       string            `json:"stateId,omitempty"`
	ParentStateID string            `json:"parentStateId,omitempty"`
	Upserts       []json.RawMessage `json:"upserts"`
	DeleteIDs     []int64           `json:"deleteIds"`
	// Always the full effective root metadata, including allocated next_id.
	Meta json.RawMessage `json:"meta"`
}

type ChangePage struct {
	Revision  uint64   `json:"revision"`
	StateID   string   `json:"stateId,omitempty"`
	Changes   []Change `json:"changes"`
	NextAfter uint64   `json:"nextAfter"`
	HasMore   bool     `json:"hasMore"`
}

type Asset struct {
	SHA256       string `json:"sha256"`
	OriginalPath string `json:"originalPath"`
	Kind         string `json:"kind"`
	Bytes        []byte `json:"-"`
}

type AssetMetadata struct {
	SHA256       string `json:"sha256"`
	OriginalPath string `json:"originalPath"`
	Kind         string `json:"kind"`
	Bytes        int64  `json:"bytes"`
}

type FinalizeRequest struct {
	SHA256       string `json:"sha256"`
	ChunkCount   int    `json:"chunkCount"`
	Bytes        int64  `json:"bytes"`
	OriginalPath string `json:"originalPath,omitempty"`
}

type ImportResult struct {
	Revision   uint64 `json:"revision"`
	StateID    string `json:"stateId,omitempty"`
	Items      int    `json:"items"`
	SHA256     string `json:"sha256"`
	Bytes      int64  `json:"bytes"`
	ChunkCount int    `json:"chunkCount"`
}
