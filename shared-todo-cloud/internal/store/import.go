package store

import (
	"bytes"
	"encoding/json"
	"math"

	bolt "go.etcd.io/bbolt"
)

var (
	bChunks = []byte("chunks")
	kFinal  = []byte("final")
)

type importRecord struct {
	Fingerprint string       `json:"fingerprint"`
	Result      ImportResult `json:"result"`
}

func (s *Store) ImportChunk(importID string, index int, raw []byte) error {
	if err := identifier(importID); err != nil {
		return err
	}
	if index < 0 || len(raw) > MaxChunkBytes {
		return invalid("invalid_chunk")
	}
	return s.write(func(tx *bolt.Tx) error {
		imports := tx.Bucket(bImports)
		b, err := imports.CreateBucketIfNotExists([]byte(importID))
		if err != nil {
			return err
		}
		if b.Get(kFinal) != nil {
			return &Error{Code: CodeConflict, Reason: "import_already_finalized"}
		}
		chunks, err := b.CreateBucketIfNotExists(bChunks)
		if err != nil {
			return err
		}
		k := key(uint64(index))
		if previous := chunks.Get(k); previous != nil {
			if !bytes.Equal(previous, raw) {
				return &Error{Code: CodeConflict, Reason: "chunk_already_exists"}
			}
			return nil
		}
		return chunks.Put(k, raw)
	})
}

// Finalize initializes a virgin ledger only (revision zero, no previous writes).
// A successful retry returns its saved result, even if the ledger has advanced.
// The original document bytes are retained as an Asset with SnapshotKind; its
// originalPath is the supplied path, which may be empty. All initialization and
// its receipt/asset/change are committed together, or none of them are.
func (s *Store) Finalize(importID string, req FinalizeRequest) (ImportResult, error) {
	if err := identifier(importID); err != nil {
		return ImportResult{}, err
	}
	if !validDigest(req.SHA256) || req.ChunkCount <= 0 || req.Bytes <= 0 {
		return ImportResult{}, invalid("invalid_finalize")
	}
	if req.Bytes > MaxDocumentBytes {
		return ImportResult{}, ErrDocumentTooLarge
	}
	hash, err := fingerprint("finalize", req)
	if err != nil {
		return ImportResult{}, err
	}
	var result ImportResult
	err = s.write(func(tx *bolt.Tx) error {
		b := tx.Bucket(bImports).Bucket([]byte(importID))
		if b == nil {
			return &Error{Code: CodeNotFound, Reason: "import"}
		}
		if raw := b.Get(kFinal); raw != nil {
			var record importRecord
			if err := json.Unmarshal(raw, &record); err != nil {
				return err
			}
			if record.Fingerprint != hash {
				return &Error{Code: CodeConflict, Reason: "finalize_request_reused"}
			}
			result = record.Result
			return nil
		}
		if revision(tx) != 0 || tx.Bucket(bItems).Stats().KeyN != 0 {
			return &Error{Code: CodeConflict, Reason: "ledger_not_empty", Revision: revision(tx)}
		}
		if err := noActiveLease(tx, s.now().UTC()); err != nil {
			return err
		}
		chunks := b.Bucket(bChunks)
		if chunks == nil || chunks.Stats().KeyN != req.ChunkCount {
			return &Error{Code: CodeConflict, Reason: "import_incomplete"}
		}
		// Do not allocate a caller-declared buffer before checking actual bytes.
		var raw []byte
		var length int64
		for i := 0; i < req.ChunkCount; i++ {
			chunk := chunks.Get(key(uint64(i)))
			if chunk == nil {
				return &Error{Code: CodeConflict, Reason: "import_incomplete"}
			}
			if len(chunk) > MaxChunkBytes || length > math.MaxInt64-int64(len(chunk)) {
				return invalid("invalid_import_size")
			}
			length += int64(len(chunk))
			if length > req.Bytes {
				return invalid("import_size_mismatch")
			}
			raw = append(raw, chunk...)
		}
		if length != req.Bytes {
			return invalid("import_size_mismatch")
		}
		if digest(raw) != req.SHA256 {
			return invalid("import_digest_mismatch")
		}
		// Validate all nested duplicate keys without converting any number to float.
		if _, err := canonical(raw); err != nil {
			return err
		}
		root, err := object(raw)
		if err != nil {
			return err
		}
		itemsRaw, exists := root["items"]
		if !exists || len(bytes.TrimSpace(itemsRaw)) == 0 || bytes.TrimSpace(itemsRaw)[0] != '[' {
			return invalid("import_items_must_be_array")
		}
		var items []json.RawMessage
		if err := json.Unmarshal(itemsRaw, &items); err != nil {
			return invalid("invalid_import_items")
		}
		delete(root, "items")
		seen := make(map[int64]bool, len(items))
		next := int64(1)
		parsed := make([]rawItem, 0, len(items))
		for _, item := range items {
			fields, id, err := itemFields(item)
			if err != nil {
				return err
			}
			if seen[id] {
				return &Error{Code: CodeConflict, Reason: "duplicate_item_id"}
			}
			seen[id] = true
			if id >= next {
				next = id + 1
			}
			parsed = append(parsed, rawItem{id: id, raw: item, fields: fields})
		}
		meta, next, err := normalizedMeta(encodeObject(root), next, true)
		if err != nil {
			return err
		}
		for _, item := range parsed {
			if err := putItem(tx, item); err != nil {
				return err
			}
		}
		if err := persistRevision(tx, Change{Revision: 1, Upserts: items, DeleteIDs: []int64{}, Meta: meta}, next); err != nil {
			return err
		}
		if _, err := putAsset(tx, Asset{SHA256: req.SHA256, OriginalPath: req.OriginalPath, Kind: SnapshotKind, Bytes: raw}); err != nil {
			return err
		}
		result = ImportResult{Revision: 1, StateID: stateID(tx), Items: len(items), SHA256: req.SHA256, Bytes: length, ChunkCount: req.ChunkCount}
		if err := putJSON(b, kFinal, importRecord{Fingerprint: hash, Result: result}); err != nil {
			return err
		}
		return b.DeleteBucket(bChunks)
	})
	if err != nil {
		return ImportResult{}, err
	}
	return result, nil
}
