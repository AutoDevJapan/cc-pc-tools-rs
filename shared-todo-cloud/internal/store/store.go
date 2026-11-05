package store

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	bolt "go.etcd.io/bbolt"
)

var (
	bControl    = []byte("control")
	bItems      = []byte("items")
	bOrder      = []byte("item_order")
	bPositions  = []byte("item_positions")
	bSummaries  = []byte("summaries")
	bChanges    = []byte("changes")
	bOperations = []byte("operations")
	bAcquires   = []byte("acquires")
	bAssets     = []byte("assets")
	bBlobs      = []byte("asset_blobs")
	bAssetPaths = []byte("asset_paths")
	bImports    = []byte("imports")
	kVersion    = []byte("version")
	kRevision   = []byte("revision")
	kStateID    = []byte("state_id")
	kNext       = []byte("next")
	kMeta       = []byte("meta")
	kLease      = []byte("lease")
	allBuckets  = [][]byte{bControl, bItems, bOrder, bPositions, bSummaries, bChanges, bOperations, bAcquires, bAssets, bBlobs, bAssetPaths, bImports}
)

type Store struct {
	db  *bolt.DB
	now func() time.Time
}

type Option func(*Store) error

// WithClock injects the lease clock. It must be safe for concurrent callers.
func WithClock(now func() time.Time) Option {
	return func(s *Store) error {
		if now == nil {
			return invalid("nil_clock")
		}
		s.now = now
		return nil
	}
}

// Open creates or opens a private bbolt file. Sync is enabled; callers must not
// share the same file through multiple Store instances/processes concurrently.
func Open(path string, options ...Option) (*Store, error) {
	s := &Store{now: time.Now}
	for _, opt := range options {
		if opt == nil {
			return nil, invalid("nil_option")
		}
		if err := opt(s); err != nil {
			return nil, err
		}
	}
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second, NoSync: false})
	if err != nil {
		return nil, err
	}
	s.db = db
	err = db.Update(func(tx *bolt.Tx) error {
		control := tx.Bucket(bControl)
		if control != nil {
			if bytes.Equal(control.Get(kVersion), []byte{1}) {
				if err := migrateAssetsV1(tx); err != nil {
					return err
				}
			}
			if !bytes.Equal(control.Get(kVersion), []byte{2}) {
				return fmt.Errorf("store: unsupported schema version")
			}
			for _, name := range allBuckets {
				if tx.Bucket(name) == nil {
					return fmt.Errorf("store: missing bucket %s", name)
				}
			}
			return ensureStateID(tx)
		}
		for _, name := range allBuckets {
			if _, err := tx.CreateBucket(name); err != nil {
				return err
			}
		}
		control = tx.Bucket(bControl)
		for _, kv := range []struct{ k, v []byte }{
			{kVersion, []byte{2}}, {kRevision, key(0)}, {kNext, key(1)},
			{kMeta, []byte(`{"next_id":1}`)},
		} {
			if err := control.Put(kv.k, kv.v); err != nil {
				return err
			}
		}
		return ensureStateID(tx)
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return databaseError(s.db.Close()) }

func databaseError(err error) error {
	if errors.Is(err, bolt.ErrDatabaseNotOpen) {
		return ErrClosed
	}
	return err
}

func (s *Store) view(fn func(*bolt.Tx) error) error  { return databaseError(s.db.View(fn)) }
func (s *Store) write(fn func(*bolt.Tx) error) error { return databaseError(s.db.Update(fn)) }

func key(n uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], n)
	return b[:]
}

func revision(tx *bolt.Tx) uint64 { return binary.BigEndian.Uint64(tx.Bucket(bControl).Get(kRevision)) }
func stateID(tx *bolt.Tx) string  { return string(tx.Bucket(bControl).Get(kStateID)) }

func randomStateID() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}
func ensureStateID(tx *bolt.Tx) error {
	value := stateID(tx)
	if value != "" {
		if !validDigest(value) {
			return invalid("invalid_state_id")
		}
		return nil
	}
	value, err := randomStateID()
	if err != nil {
		return err
	}
	return tx.Bucket(bControl).Put(kStateID, []byte(value))
}

// Version reads only two small control keys in one consistent transaction.
// State IDs survive restart and distinguish equal revision numbers on forks.
func (s *Store) Version() (rev uint64, id string, err error) {
	err = s.view(func(tx *bolt.Tx) error { rev = revision(tx); id = stateID(tx); return nil })
	return
}
func nextID(tx *bolt.Tx) int64 { return int64(binary.BigEndian.Uint64(tx.Bucket(bControl).Get(kNext))) }

// Metadata reads only the root object excluding items, with its matching
// revision. It does not scan or assemble items and returns a copy of the bytes.
func (s *Store) Metadata() (rev uint64, meta json.RawMessage, err error) {
	err = s.view(func(tx *bolt.Tx) error {
		rev = revision(tx)
		meta = bytes.Clone(tx.Bucket(bControl).Get(kMeta))
		return nil
	})
	return rev, meta, err
}

func (s *Store) Snapshot() (Snapshot, error) {
	var result Snapshot
	err := s.view(func(tx *bolt.Tx) error {
		meta, err := object(tx.Bucket(bControl).Get(kMeta))
		if err != nil {
			return err
		}
		items := make([]json.RawMessage, 0)
		err = tx.Bucket(bOrder).ForEach(func(_, id []byte) error {
			items = append(items, tx.Bucket(bItems).Get(id))
			return nil
		})
		if err != nil {
			return err
		}
		meta["items"] = encodeArray(items)
		result = Snapshot{Revision: revision(tx), StateID: stateID(tx), Document: encodeObject(meta)}
		return nil
	})
	return result, err
}

func (s *Store) Item(id int64) (Item, error) {
	if id <= 0 {
		return Item{}, invalid("invalid_item_id")
	}
	var result Item
	err := s.view(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bItems).Get(key(uint64(id)))
		if raw == nil {
			return &Error{Code: CodeNotFound, Reason: "item"}
		}
		result = Item{Revision: revision(tx), Item: bytes.Clone(raw)}
		return nil
	})
	return result, err
}

func pageLimit(n int) (int, error) {
	if n < 0 || n > MaxLimit {
		return 0, invalid("invalid_limit")
	}
	if n == 0 {
		return DefaultLimit, nil
	}
	return n, nil
}

func matches(raw json.RawMessage, wanted string) bool {
	if wanted == "" {
		return true
	}
	var value string
	return json.Unmarshal(raw, &value) == nil && value == wanted
}

func (s *Store) ListSummaries(opts ListOptions) (SummaryPage, error) {
	limit, err := pageLimit(opts.Limit)
	if err != nil || opts.After < 0 {
		return SummaryPage{}, invalid("invalid_pagination")
	}
	search := strings.ToLower(opts.Search)
	result := SummaryPage{Items: make([]Summary, 0), NextAfter: opts.After}
	err = s.view(func(tx *bolt.Tx) error {
		result.Revision = revision(tx)
		if opts.After == math.MaxInt64 {
			return nil
		}
		c := tx.Bucket(bSummaries).Cursor()
		for k, v := c.Seek(key(uint64(opts.After) + 1)); k != nil; k, v = c.Next() {
			var item Summary
			if err := json.Unmarshal(v, &item); err != nil {
				return err
			}
			if !matches(item.Status, opts.Status) || !matches(item.Project, opts.Project) {
				continue
			}
			if opts.OpenOnly && matches(item.Status, "done") {
				continue
			}
			if search != "" && !strings.Contains(strconv.FormatInt(item.ID, 10), search) {
				var title string
				if json.Unmarshal(item.Title, &title) != nil || !strings.Contains(strings.ToLower(title), search) {
					continue
				}
			}
			if len(result.Items) == limit {
				result.HasMore = true
				break
			}
			result.Items = append(result.Items, item)
			result.NextAfter = item.ID
		}
		return nil
	})
	return result, err
}

func (s *Store) Changes(after uint64, limit int) (ChangePage, error) {
	limit, err := pageLimit(limit)
	if err != nil {
		return ChangePage{}, err
	}
	result := ChangePage{Changes: make([]Change, 0), NextAfter: after}
	err = s.view(func(tx *bolt.Tx) error {
		result.Revision = revision(tx)
		result.StateID = stateID(tx)
		if after > result.Revision {
			return &Error{Code: CodeConflict, Reason: "future_revision", Revision: result.Revision}
		}
		if after == result.Revision {
			return nil
		}
		c := tx.Bucket(bChanges).Cursor()
		for k, v := c.Seek(key(after + 1)); k != nil; k, v = c.Next() {
			if len(result.Changes) == limit {
				result.HasMore = true
				break
			}
			var change Change
			if err := json.Unmarshal(v, &change); err != nil {
				return err
			}
			result.Changes = append(result.Changes, change)
			result.NextAfter = change.Revision
		}
		return nil
	})
	return result, err
}

type acquireRecord struct {
	ClientID string `json:"clientId"`
	LeaseID  string `json:"leaseId"`
}

func readLease(tx *bolt.Tx) (Lease, bool, error) {
	raw := tx.Bucket(bControl).Get(kLease)
	if raw == nil {
		return Lease{}, false, nil
	}
	var lease Lease
	err := json.Unmarshal(raw, &lease)
	return lease, true, err
}

func putJSON(b *bolt.Bucket, k []byte, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return b.Put(k, raw)
}

func identifier(value string) error {
	if value == "" || len(value) > 512 {
		return invalid("invalid_identifier")
	}
	return nil
}

func (s *Store) Acquire(clientID, requestID string) (Lease, error) {
	if err := identifier(clientID); err != nil {
		return Lease{}, err
	}
	if err := identifier(requestID); err != nil {
		return Lease{}, err
	}
	var result Lease
	err := s.write(func(tx *bolt.Tx) error {
		now := s.now().UTC()
		lease, exists, err := readLease(tx)
		if err != nil {
			return err
		}
		if raw := tx.Bucket(bAcquires).Get([]byte(requestID)); raw != nil {
			var saved acquireRecord
			if err := json.Unmarshal(raw, &saved); err != nil {
				return err
			}
			if saved.ClientID != clientID {
				return &Error{Code: CodeConflict, Reason: "acquire_request_reused"}
			}
			if !exists || saved.LeaseID != lease.ID || !now.Before(lease.ExpiresAt) {
				return ErrLeaseExpired
			}
			result = lease
			result.Revision = revision(tx)
			result.StateID = stateID(tx)
			result.TTLMillis = lease.ExpiresAt.Sub(now).Milliseconds()
			if result.TTLMillis <= 0 {
				return ErrLeaseExpired
			}
			return nil
		}
		if exists && now.Before(lease.ExpiresAt) {
			return ErrLocked
		}
		var token [32]byte
		if _, err := rand.Read(token[:]); err != nil {
			return err
		}
		result = Lease{ID: hex.EncodeToString(token[:]), ClientID: clientID,
			Revision: revision(tx), StateID: stateID(tx), TTLMillis: LeaseDuration.Milliseconds(), ExpiresAt: now.Add(LeaseDuration)}
		if err := putJSON(tx.Bucket(bControl), kLease, result); err != nil {
			return err
		}
		return putJSON(tx.Bucket(bAcquires), []byte(requestID), acquireRecord{ClientID: clientID, LeaseID: result.ID})
	})
	return result, err
}

func requireLease(tx *bolt.Tx, id string, now time.Time) (Lease, error) {
	lease, exists, err := readLease(tx)
	if err != nil {
		return Lease{}, err
	}
	if !exists || id == "" || id != lease.ID {
		return Lease{}, &Error{Code: CodeLocked, Reason: "lease_not_owned"}
	}
	if !now.Before(lease.ExpiresAt) {
		return Lease{}, ErrLeaseExpired
	}
	return lease, nil
}

func (s *Store) Renew(leaseID string) (Lease, error) {
	var result Lease
	err := s.write(func(tx *bolt.Tx) error {
		now := s.now().UTC()
		lease, err := requireLease(tx, leaseID, now)
		if err != nil {
			return err
		}
		lease.ExpiresAt = now.Add(LeaseDuration)
		lease.Revision = revision(tx)
		lease.StateID = stateID(tx)
		lease.TTLMillis = LeaseDuration.Milliseconds()
		result = lease
		return putJSON(tx.Bucket(bControl), kLease, lease)
	})
	return result, err
}

func (s *Store) Release(leaseID string) error {
	if err := identifier(leaseID); err != nil {
		return err
	}
	return s.write(func(tx *bolt.Tx) error {
		lease, exists, err := readLease(tx)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		if lease.ID != leaseID {
			return &Error{Code: CodeLocked, Reason: "lease_not_owned"}
		}
		return tx.Bucket(bControl).Delete(kLease)
	})
}

func noActiveLease(tx *bolt.Tx, now time.Time) error {
	lease, exists, err := readLease(tx)
	if err != nil {
		return err
	}
	if exists && now.Before(lease.ExpiresAt) {
		return ErrLocked
	}
	return nil
}
