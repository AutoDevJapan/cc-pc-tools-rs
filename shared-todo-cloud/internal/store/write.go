package store

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
	"unicode/utf8"

	bolt "go.etcd.io/bbolt"
)

type operationRecord struct {
	Fingerprint string       `json:"fingerprint"`
	Result      CommitResult `json:"result"`
}

func (s *Store) Operation(requestID string) (CommitResult, error) {
	if err := identifier(requestID); err != nil {
		return CommitResult{}, err
	}
	var result CommitResult
	err := s.view(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bOperations).Get([]byte(requestID))
		if raw == nil {
			return &Error{Code: CodeNotFound, Reason: "operation"}
		}
		var record operationRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return err
		}
		result = record.Result
		return nil
	})
	return result, err
}

func replay(tx *bolt.Tx, requestID, hash string) (CommitResult, bool, error) {
	raw := tx.Bucket(bOperations).Get([]byte(requestID))
	if raw == nil {
		return CommitResult{}, false, nil
	}
	var record operationRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return CommitResult{}, false, err
	}
	if record.Fingerprint != hash {
		return CommitResult{}, false, &Error{Code: CodeConflict, Reason: "request_id_reused", Revision: revision(tx)}
	}
	result := record.Result
	result.Duplicate = true
	return result, true, nil
}

func (s *Store) Commit(req CommitRequest) (CommitResult, error) {
	if err := identifier(req.RequestID); err != nil {
		return CommitResult{}, err
	}
	hash, err := fingerprint("commit", req)
	if err != nil {
		return CommitResult{}, err
	}
	var result CommitResult
	err = s.write(func(tx *bolt.Tx) error {
		previous, found, err := replay(tx, req.RequestID, hash)
		if err != nil {
			return err
		}
		if found {
			result = previous
			return nil
		}
		if _, err := requireLease(tx, req.LeaseID, s.now().UTC()); err != nil {
			return err
		}
		result, err = mutate(tx, req.RequestID, hash, req.BaseRevision, req.BaseStateID, nil, req.Upserts, req.DeleteIDs, req.Meta)
		if err != nil {
			return err
		}
		// Parsing/writing a large request must not let an expired writer commit.
		lease, err := requireLease(tx, req.LeaseID, s.now().UTC())
		if err != nil {
			return err
		}
		lease.Revision = revision(tx)
		lease.StateID = stateID(tx)
		return putJSON(tx.Bucket(bControl), kLease, lease)
	})
	if err != nil {
		return CommitResult{}, err
	}
	return result, nil
}

func (s *Store) Update(req UpdateRequest) (CommitResult, error) {
	if err := identifier(req.RequestID); err != nil {
		return CommitResult{}, err
	}
	hash, err := fingerprint("update", req)
	if err != nil {
		return CommitResult{}, err
	}
	var result CommitResult
	err = s.write(func(tx *bolt.Tx) error {
		previous, found, err := replay(tx, req.RequestID, hash)
		if err != nil {
			return err
		}
		if found {
			result = previous
			return nil
		}
		if err := noActiveLease(tx, s.now().UTC()); err != nil {
			return err
		}
		result, err = mutate(tx, req.RequestID, hash, req.BaseRevision, req.BaseStateID, req.Creates, req.Upserts, req.DeleteIDs, req.Meta)
		return err
	})
	if err != nil {
		return CommitResult{}, err
	}
	return result, nil
}

type rawItem struct {
	id     int64
	raw    json.RawMessage
	fields map[string]json.RawMessage
}

func mutate(tx *bolt.Tx, requestID, hash string, base uint64, baseStateID string, creates, upserts []json.RawMessage,
	deletes []int64, meta json.RawMessage) (CommitResult, error) {
	current := revision(tx)
	if current != base {
		return CommitResult{}, &Error{Code: CodeConflict, Reason: "stale_revision", Revision: current}
	}
	if baseStateID != "" && baseStateID != stateID(tx) {
		return CommitResult{}, &Error{Code: CodeConflict, Reason: "stale_state", Revision: current}
	}
	if current == math.MaxUint64 {
		return CommitResult{}, &Error{Code: CodeConflict, Reason: "revision_exhausted", Revision: current}
	}
	items := make([]rawItem, 0, len(upserts)+len(creates))
	seen := make(map[int64]bool, len(upserts)+len(deletes))
	next := nextID(tx)
	for _, raw := range upserts {
		fields, id, err := itemFields(raw)
		if err != nil {
			return CommitResult{}, err
		}
		if seen[id] {
			return CommitResult{}, &Error{Code: CodeConflict, Reason: "duplicate_item_id", Revision: current}
		}
		seen[id] = true
		items = append(items, rawItem{id: id, raw: raw, fields: fields})
		if id >= next {
			next = id + 1
		}
	}
	for _, id := range deletes {
		if id <= 0 {
			return CommitResult{}, invalid("invalid_item_id")
		}
		if seen[id] {
			return CommitResult{}, &Error{Code: CodeConflict, Reason: "duplicate_item_id", Revision: current}
		}
		seen[id] = true
		if tx.Bucket(bItems).Get(key(uint64(id))) == nil {
			return CommitResult{}, &Error{Code: CodeNotFound, Reason: "item"}
		}
	}
	if meta == nil {
		meta = tx.Bucket(bControl).Get(kMeta)
	}
	meta, next, err := normalizedMeta(meta, next, false)
	if err != nil {
		return CommitResult{}, err
	}
	var createdIDs []int64
	for _, raw := range creates {
		fields, err := object(raw)
		if err != nil {
			return CommitResult{}, err
		}
		if _, exists := fields["id"]; exists {
			return CommitResult{}, invalid("create_must_omit_id")
		}
		if next == math.MaxInt64 {
			return CommitResult{}, &Error{Code: CodeConflict, Reason: "ids_exhausted"}
		}
		id := next
		next++
		fields["id"] = json.RawMessage(strconv.FormatInt(id, 10))
		items = append(items, rawItem{id: id, raw: encodeObject(fields), fields: fields})
		createdIDs = append(createdIDs, id)
	}
	// Preserve high water even after deletion or a full metadata replacement.
	meta, next, err = normalizedMeta(meta, next, false)
	if err != nil {
		return CommitResult{}, err
	}
	change := Change{Revision: current + 1, Upserts: make([]json.RawMessage, 0, len(items)),
		DeleteIDs: append([]int64{}, deletes...), Meta: meta}
	for _, item := range items {
		if err := putItem(tx, item); err != nil {
			return CommitResult{}, err
		}
		change.Upserts = append(change.Upserts, item.raw)
	}
	for _, id := range deletes {
		k := key(uint64(id))
		position := tx.Bucket(bPositions).Get(k)
		if position == nil {
			return CommitResult{}, invalid("missing_item_position")
		}
		if err := tx.Bucket(bOrder).Delete(position); err != nil {
			return CommitResult{}, err
		}
		if err := tx.Bucket(bPositions).Delete(k); err != nil {
			return CommitResult{}, err
		}
		if err := tx.Bucket(bItems).Delete(k); err != nil {
			return CommitResult{}, err
		}
		if err := tx.Bucket(bSummaries).Delete(k); err != nil {
			return CommitResult{}, err
		}
	}
	if err := persistRevision(tx, change, next); err != nil {
		return CommitResult{}, err
	}
	result := CommitResult{Revision: change.Revision, StateID: stateID(tx), Upserts: len(items), Deleted: len(deletes), CreatedIDs: createdIDs}
	if err := putJSON(tx.Bucket(bOperations), []byte(requestID), operationRecord{Fingerprint: hash, Result: result}); err != nil {
		return CommitResult{}, err
	}
	return result, nil
}

func putItem(tx *bolt.Tx, item rawItem) error {
	k := key(uint64(item.id))
	if tx.Bucket(bItems).Get(k) == nil {
		order := tx.Bucket(bOrder)
		seq, err := order.NextSequence()
		if err != nil {
			return err
		}
		if seq == 0 {
			return &Error{Code: CodeConflict, Reason: "item_order_exhausted"}
		}
		position := key(seq)
		if err := order.Put(position, k); err != nil {
			return err
		}
		if err := tx.Bucket(bPositions).Put(k, position); err != nil {
			return err
		}
	}
	if err := tx.Bucket(bItems).Put(k, item.raw); err != nil {
		return err
	}
	return putJSON(tx.Bucket(bSummaries), k, summary(item.fields, item.id))
}

func persistRevision(tx *bolt.Tx, change Change, next int64) error {
	control := tx.Bucket(bControl)
	change.ParentStateID = stateID(tx)
	var err error
	change.StateID, err = randomStateID()
	if err != nil {
		return err
	}
	if err := control.Put(kMeta, change.Meta); err != nil {
		return err
	}
	if err := control.Put(kNext, key(uint64(next))); err != nil {
		return err
	}
	if err := control.Put(kRevision, key(change.Revision)); err != nil {
		return err
	}
	if err := control.Put(kStateID, []byte(change.StateID)); err != nil {
		return err
	}
	return putJSON(tx.Bucket(bChanges), key(change.Revision), change)
}

// PutAsset keeps one exact raw blob per digest and immutable metadata per source
// path. Multiple paths (and their own kinds) may point to the same blob. Reusing
// a path for a different digest or kind is a conflict, never an implicit update.
func (s *Store) PutAsset(asset Asset) (AssetMetadata, error) {
	if !validDigest(asset.SHA256) || digest(asset.Bytes) != asset.SHA256 {
		return AssetMetadata{}, invalid("asset_digest_mismatch")
	}
	var result AssetMetadata
	err := s.write(func(tx *bolt.Tx) error {
		var err error
		result, err = putAsset(tx, asset)
		return err
	})
	return result, err
}

func putAsset(tx *bolt.Tx, asset Asset) (AssetMetadata, error) {
	if !utf8.ValidString(asset.OriginalPath) || !utf8.ValidString(asset.Kind) {
		return AssetMetadata{}, invalid("invalid_asset_metadata")
	}
	meta := AssetMetadata{SHA256: asset.SHA256, OriginalPath: asset.OriginalPath, Kind: asset.Kind, Bytes: int64(len(asset.Bytes))}
	if asset.OriginalPath != "" {
		if existing := tx.Bucket(bAssetPaths).Get([]byte(asset.OriginalPath)); existing != nil {
			var saved AssetMetadata
			if err := json.Unmarshal(existing, &saved); err != nil {
				return AssetMetadata{}, err
			}
			if saved != meta {
				return AssetMetadata{}, &Error{Code: CodeConflict, Reason: "asset_path_already_exists"}
			}
		}
	}
	k := []byte(asset.SHA256)
	if existing := tx.Bucket(bBlobs).Get(k); existing != nil {
		if !bytes.Equal(existing, asset.Bytes) {
			return AssetMetadata{}, &Error{Code: CodeConflict, Reason: "asset_blob_already_exists"}
		}
	} else {
		if err := tx.Bucket(bBlobs).Put(k, asset.Bytes); err != nil {
			return AssetMetadata{}, err
		}
	}
	// Digest lookup retains the first upload's metadata. ResolveAsset selects
	// the exact metadata for any other path alias. Empty paths are unbound blobs.
	if tx.Bucket(bAssets).Get(k) == nil {
		if err := putJSON(tx.Bucket(bAssets), k, meta); err != nil {
			return AssetMetadata{}, err
		}
	}
	if asset.OriginalPath != "" {
		if err := putJSON(tx.Bucket(bAssetPaths), []byte(asset.OriginalPath), meta); err != nil {
			return AssetMetadata{}, err
		}
	}
	return meta, nil
}

// Asset retrieves the shared blob by digest with its first upload's metadata.
// ResolveAsset returns path-specific metadata when a blob has several aliases.
func (s *Store) Asset(sha256 string) (Asset, error) {
	if !validDigest(sha256) {
		return Asset{}, invalid("invalid_digest")
	}
	var result Asset
	err := s.view(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bAssets).Get([]byte(sha256))
		if raw == nil {
			return &Error{Code: CodeNotFound, Reason: "asset"}
		}
		var meta AssetMetadata
		if err := json.Unmarshal(raw, &meta); err != nil {
			return err
		}
		data := tx.Bucket(bBlobs).Get([]byte(sha256))
		if data == nil {
			return &Error{Code: CodeNotFound, Reason: "asset_blob"}
		}
		result = Asset{SHA256: meta.SHA256, OriginalPath: meta.OriginalPath,
			Kind: meta.Kind, Bytes: bytes.Clone(data)}
		return nil
	})
	return result, err
}

func (s *Store) ResolveAsset(originalPath string) (AssetMetadata, error) {
	if originalPath == "" || !utf8.ValidString(originalPath) {
		return AssetMetadata{}, invalid("invalid_asset_path")
	}
	var result AssetMetadata
	err := s.view(func(tx *bolt.Tx) error {
		raw := tx.Bucket(bAssetPaths).Get([]byte(originalPath))
		if raw == nil {
			return &Error{Code: CodeNotFound, Reason: "asset_path"}
		}
		return json.Unmarshal(raw, &result)
	})
	return result, err
}

// migrateAssetsV1 retains files created before blob/path storage was separated.
// Validation and the version change are part of the same opening transaction.
func migrateAssetsV1(tx *bolt.Tx) error {
	assets := tx.Bucket(bAssets)
	if assets == nil {
		return invalid("missing_assets_bucket")
	}
	for _, name := range [][]byte{bBlobs, bAssetPaths} {
		if _, err := tx.CreateBucketIfNotExists(name); err != nil {
			return err
		}
	}
	var keys [][]byte
	if err := assets.ForEach(func(k, _ []byte) error { keys = append(keys, bytes.Clone(k)); return nil }); err != nil {
		return err
	}
	for _, k := range keys {
		var old struct {
			Metadata AssetMetadata `json:"metadata"`
			Data     []byte        `json:"data"`
		}
		if err := json.Unmarshal(assets.Get(k), &old); err != nil {
			return err
		}
		if old.Metadata.SHA256 != string(k) || !validDigest(string(k)) || digest(old.Data) != string(k) || old.Metadata.Bytes != int64(len(old.Data)) {
			return invalid("asset_digest_mismatch")
		}
		if err := assets.Delete(k); err != nil {
			return err
		}
		if _, err := putAsset(tx, Asset{SHA256: string(k), OriginalPath: old.Metadata.OriginalPath, Kind: old.Metadata.Kind, Bytes: old.Data}); err != nil {
			return err
		}
	}
	return tx.Bucket(bControl).Put(kVersion, []byte{2})
}
