package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"autodevjapan/shared-todo-cloud/internal/assetio"
)

type Session struct {
	client     *Client
	cache      string
	mu         sync.Mutex
	lease      Lease
	base       *document
	canonical  []byte
	failure    error
	cancel     context.CancelFunc
	done       chan struct{}
	namespace  string
	assetHome  string
	resolution string
}

var errLineage = errors.New("todo_remote_state_lineage_mismatch")

// Begin runs after the existing OS byte lock. A valid cache at the same state
// reuses its exact bytes without parsing or serializing the complete document.
func Begin(ctx context.Context, c *Client, clientID, cache string) (*Session, error) {
	return beginSession(ctx, c, clientID, cache, 25*time.Second)
}
func beginSession(ctx context.Context, c *Client, clientID, cache string, renewEvery time.Duration) (*Session, error) {
	ctx, cancelBegin := context.WithTimeout(ctx, 65*time.Second)
	defer cancelBegin()
	pending, err := readPending(cache)
	if err != nil {
		return nil, err
	}
	ns := namespace(c.base, clientID)
	if pending != nil && pending.Namespace != ns {
		return nil, &CommitUnconfirmed{}
	}
	forceFresh := pending != nil || needsFresh(cache)
	lease, err := c.acquire(ctx, clientID)
	if err != nil {
		if pending != nil {
			if pending.Confirmed != nil {
				return nil, &CacheResyncRequired{}
			}
			return nil, &CommitUnconfirmed{}
		}
		return nil, err
	}
	renewCtx, cancel := context.WithCancel(context.Background())
	s := &Session{client: c, cache: cache, lease: lease, cancel: cancel, done: make(chan struct{}), namespace: ns, assetHome: filepath.Dir(cache)}
	go s.keepAlive(renewCtx, renewEvery)
	if err = s.resolvePending(ctx, pending); err != nil {
		_ = s.Close()
		if pending != nil && pending.Confirmed != nil {
			return nil, &CacheResyncRequired{}
		}
		return nil, err
	}
	raw, state, cacheErr := loadCacheEntry(cache)
	if cacheErr != nil {
		if err = preserveForeign(cache); err != nil {
			_ = s.Close()
			return nil, err
		}
	}
	reuse := !forceFresh && cacheErr == nil && sameState(state.Revision, state.StateID, lease.Revision, lease.StateID)
	var d *document
	if !reuse {
		if !forceFresh && cacheErr == nil && state.StateID != "" && lease.StateID != "" && state.Revision < lease.Revision {
			d, err = parseDocument(raw)
			if err == nil {
				d, err = s.pullChanges(ctx, d, state, lease)
			}
			if err == nil {
				raw, err = d.bytes()
			}
		} else {
			err = errLineage
		}
		if err != nil && (errors.Is(err, errLineage) || errors.Is(err, errResponseTooLarge) || cacheErr != nil) {
			// Repeated history may exceed the bounded wire reader even when the
			// current document fits. Keep this lease and its exact pinned state;
			// only a fully validated snapshot is allowed to replace the cache.
			raw, d, err = s.snapshotPreservingCache(ctx, lease, cacheErr == nil)
		}
	}
	s.mu.Lock()
	if err == nil {
		err = s.healthy()
	}
	if err == nil && !reuse {
		err = saveCache(cache, raw, lease.Revision, lease.StateID)
	}
	if err == nil && pending != nil {
		err = clearResolved(cache, pending, s.resolution)
	}
	if err == nil && !reuse {
		err = removeIfPresent(invalidPath(cache))
	}
	if err == nil {
		s.base = d
		s.canonical = raw
	}
	s.mu.Unlock()
	if err != nil {
		_ = s.Close()
		if pending != nil && pending.Confirmed != nil {
			return nil, &CacheResyncRequired{}
		}
		return nil, err
	}
	return s, nil
}
func sameState(rev uint64, id string, otherRev uint64, otherID string) bool {
	return rev == otherRev && id != "" && id == otherID
}

func (s *Session) snapshotPreservingCache(ctx context.Context, pinned Lease, preserve bool) ([]byte, *document, error) {
	if preserve {
		if err := preserveForeign(s.cache); err != nil {
			return nil, nil, err
		}
	}
	return s.snapshot(ctx, pinned)
}

func (s *Session) snapshot(ctx context.Context, pinned Lease) ([]byte, *document, error) {
	var snap Snapshot
	if err := s.client.call(ctx, "GET", "/api/v1/state", nil, &snap); err != nil {
		return nil, nil, err
	}
	if snap.Revision != pinned.Revision || snap.StateID != pinned.StateID {
		return nil, nil, errors.New("todo_remote_snapshot_revision_mismatch")
	}
	d, err := parseDocument(snap.Document)
	return snap.Document, d, err
}
func (s *Session) pullChanges(ctx context.Context, d *document, state cacheState, pinned Lease) (*document, error) {
	for state.Revision < pinned.Revision {
		var page Changes
		if err := s.client.call(ctx, "GET", fmt.Sprintf("/api/v1/changes?after=%d&limit=128", state.Revision), nil, &page); err != nil {
			return nil, err
		}
		if page.Revision != pinned.Revision || (page.StateID != "" && page.StateID != pinned.StateID) || len(page.Changes) == 0 {
			return nil, errLineage
		}
		for _, change := range page.Changes {
			if change.Revision != state.Revision+1 || change.Revision > pinned.Revision || change.ParentStateID != state.StateID || change.StateID == "" {
				return nil, errLineage
			}
			if err := d.apply(change); err != nil {
				return nil, err
			}
			state.Revision = change.Revision
			state.StateID = change.StateID
		}
		if page.NextAfter != state.Revision || page.HasMore != (state.Revision < pinned.Revision) {
			return nil, errLineage
		}
	}
	if state.StateID != pinned.StateID {
		return nil, errLineage
	}
	return d, nil
}
func (s *Session) healthy() error {
	if s.failure != nil {
		return s.failure
	}
	if !time.Now().Before(s.lease.deadline) {
		return errors.New("todo_remote_lease_expired")
	}
	return nil
}
func (s *Session) keepAlive(ctx context.Context, renewEvery time.Duration) {
	defer close(s.done)
	ticker := time.NewTicker(renewEvery)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.failure == nil {
				lease, err := s.client.renew(ctx, s.lease.ID)
				if err != nil {
					s.failure = errors.New("todo_remote_lease_renew_failed")
				} else if lease.Revision != s.lease.Revision || lease.StateID != s.lease.StateID {
					s.failure = errors.New("todo_remote_lease_revision_changed")
				} else {
					s.lease = lease
				}
			}
			s.mu.Unlock()
		}
	}
}
func (s *Session) Revision() uint64 { s.mu.Lock(); defer s.mu.Unlock(); return s.lease.Revision }
func (s *Session) StateID() string  { s.mu.Lock(); defer s.mu.Unlock(); return s.lease.StateID }

// SetOwnedHome binds a staging/cutover session to the real configured PC home.
// Ordinary sessions already use the directory containing their cache.
func (s *Session) SetOwnedHome(home string) error {
	if !filepath.IsAbs(home) {
		return errors.New("todo_remote_asset_home_must_be_absolute")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.assetHome = filepath.Clean(home)
	return nil
}

// Commit pins both the lease/revision and the source state. Uncertain network
// outcomes never install the candidate or replay its domain edit at a new rev.
func (s *Session) Commit(ctx context.Context, b []byte) (Receipt, error) {
	ctx, cancelCommit := context.WithTimeout(ctx, 60*time.Second)
	defer cancelCommit()
	next, err := parseDocument(b)
	if err != nil {
		return Receipt{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = s.healthy(); err != nil {
		return Receipt{}, err
	}
	if s.base == nil {
		s.base, err = parseDocument(s.canonical)
		if err != nil {
			return Receipt{}, err
		}
	}
	s.base.preserveUnchanged(next)
	if _, err = next.bytes(); err != nil {
		return Receipt{}, err
	}
	commit, changed := s.base.diff(next)
	if !changed {
		return Receipt{Revision: s.lease.Revision, StateID: s.lease.StateID}, nil
	}
	lease, err := s.client.renew(ctx, s.lease.ID)
	if err != nil || lease.Revision != s.lease.Revision || lease.StateID != s.lease.StateID {
		s.failure = errors.New("todo_remote_lease_renew_failed")
		return Receipt{}, s.failure
	}
	s.lease = lease
	if err = assetio.UploadNew(ctx, s.base.items, commit.Upserts, s.assetHome, s.client.putAsset); err != nil {
		return Receipt{}, err
	}
	commit.RequestID, err = requestID()
	if err != nil {
		return Receipt{}, err
	}
	commit.LeaseID = s.lease.ID
	commit.BaseRevision = s.lease.Revision
	commit.BaseStateID = s.lease.StateID
	body, err := encodeJSON(commit)
	if err != nil {
		return Receipt{}, err
	}
	pending := &pendingCommit{Namespace: s.namespace, RequestID: commit.RequestID, BaseRevision: commit.BaseRevision, BaseStateID: commit.BaseStateID, CandidateSHA256: digest(b), BodySHA256: digest(body), Body: body}
	if err = savePending(s.cache, pending); err != nil {
		return Receipt{}, errors.New("todo_remote_pending_write_failed")
	}
	commitCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	receipt, err := s.client.commitBody(commitCtx, commit.RequestID, body)
	if err != nil {
		var uncertain *CommitUnconfirmed
		if errors.As(err, &uncertain) {
			_ = invalidate(s.cache, event{Kind: "commit_unconfirmed", RequestID: commit.RequestID, Revision: commit.BaseRevision, SHA256: pending.CandidateSHA256})
		} else {
			_ = clearResolved(s.cache, pending, "commit_rejected")
		}
		s.failure = err
		return Receipt{}, err
	}
	if receipt.Revision != s.lease.Revision+1 {
		s.failure = &CommitUnconfirmed{}
		return Receipt{}, s.failure
	}
	// A confirmed domain commit is successful even if cache refresh later fails.
	// Keep its flushed pending journal as a durable invalidation until repair.
	pending.Confirmed = &receipt
	_ = savePending(s.cache, pending)
	_ = invalidate(s.cache, event{Kind: "commit_confirmed_cache_pending", RequestID: commit.RequestID, Revision: receipt.Revision})
	receipt, err = s.refreshConfirmed(ctx, receipt, pending)
	if err != nil {
		s.failure = &CacheResyncRequired{}
		return receipt, nil
	}
	return receipt, nil
}

func (s *Session) refreshConfirmed(ctx context.Context, receipt Receipt, pending *pendingCommit) (Receipt, error) {
	var accepted Changes
	err := s.client.call(ctx, "GET", fmt.Sprintf("/api/v1/changes?after=%d&limit=1", s.lease.Revision), nil, &accepted)
	if errors.Is(err, errResponseTooLarge) && receipt.StateID != "" {
		pinned := s.lease
		pinned.Revision, pinned.StateID = receipt.Revision, receipt.StateID
		raw, canonical, snapshotErr := s.snapshotPreservingCache(ctx, pinned, true)
		if snapshotErr != nil {
			s.failure = errors.New("todo_remote_committed_cache_refresh_required")
			return receipt, s.failure
		}
		return s.installConfirmed(receipt, receipt.StateID, canonical, raw, pending)
	}
	if err != nil || accepted.Revision != receipt.Revision || accepted.NextAfter != receipt.Revision || accepted.HasMore || len(accepted.Changes) != 1 || accepted.Changes[0].Revision != receipt.Revision {
		s.failure = errors.New("todo_remote_committed_cache_refresh_required")
		return receipt, s.failure
	}
	change := accepted.Changes[0]
	if (s.lease.StateID != "" && (change.ParentStateID != s.lease.StateID || change.StateID == "")) ||
		(receipt.StateID != "" && change.StateID != receipt.StateID) || (accepted.StateID != "" && change.StateID != accepted.StateID) {
		s.failure = errors.New("todo_remote_committed_cache_refresh_required")
		return receipt, s.failure
	}
	canonical := cloneDocument(s.base)
	if err = canonical.apply(change); err != nil {
		s.failure = errors.New("todo_remote_committed_cache_refresh_required")
		return receipt, s.failure
	}
	cacheBytes, err := canonical.bytes()
	if err != nil {
		s.failure = errors.New("todo_remote_committed_cache_refresh_required")
		return receipt, s.failure
	}
	return s.installConfirmed(receipt, change.StateID, canonical, cacheBytes, pending)
}

func (s *Session) installConfirmed(receipt Receipt, id string, canonical *document, cacheBytes []byte, pending *pendingCommit) (Receipt, error) {
	s.lease.Revision = receipt.Revision
	s.lease.StateID = id
	s.base = canonical
	s.canonical = cacheBytes
	if err := saveCache(s.cache, cacheBytes, receipt.Revision, id); err != nil {
		s.failure = errors.New("todo_remote_committed_cache_refresh_required")
		return receipt, s.failure
	}
	if err := clearResolved(s.cache, pending, "commit_confirmed_cache_current"); err != nil {
		return receipt, err
	}
	if err := removeIfPresent(invalidPath(s.cache)); err != nil {
		return receipt, err
	}
	return receipt, nil
}
func cloneDocument(d *document) *document {
	copy := &document{meta: make(map[string]json.RawMessage, len(d.meta)), items: make(map[int64]json.RawMessage, len(d.items)), order: append([]int64(nil), d.order...)}
	for k, v := range d.meta {
		copy.meta[k] = v
	}
	for k, v := range d.items {
		copy.items[k] = v
	}
	return copy
}
func (s *Session) Close() error {
	s.cancel()
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lease.ID == "" {
		return nil
	}
	id := s.lease.ID
	s.lease.ID = ""
	s.failure = errors.New("todo_remote_session_closed")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return s.client.release(ctx, id)
}
