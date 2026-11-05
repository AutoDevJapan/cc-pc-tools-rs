package store_test

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"autodevjapan/shared-todo-cloud/internal/store"
	bolt "go.etcd.io/bbolt"
)

func TestMetadataReturnsOnlyRootAndMatchingRevision(t *testing.T) {
	s, _, _ := openStore(t)
	rev, meta, err := s.Metadata()
	if err != nil || rev != 0 {
		t.Fatalf("metadata revision=%d, %v", rev, err)
	}
	requireJSON(t, meta, raw(`{"next_id":1}`))
	// Notes are intentionally much larger than the returned metadata.
	item := raw(`{"id":4,"notes":"` + string(bytes.Repeat([]byte("x"), 1<<20)) + `"}`)
	if _, err := s.Update(store.UpdateRequest{RequestID: "metadata", Upserts: []json.RawMessage{item}, Meta: raw(`{"next_id":20,"Root":null,"root":{"projects":["sample"]},"large":9007199254740993}`)}); err != nil {
		t.Fatal(err)
	}
	rev, meta, err = s.Metadata()
	if err != nil || rev != 1 {
		t.Fatalf("metadata revision=%d, %v", rev, err)
	}
	requireJSON(t, meta, raw(`{"next_id":20,"Root":null,"root":{"projects":["sample"]},"large":9007199254740993}`))
	if len(meta) > 200 || bytes.Contains(meta, []byte(`"items"`)) || bytes.Contains(meta, []byte(`"notes"`)) {
		t.Fatal("Metadata assembled item content")
	}
	meta[0] = '!'
	_, again, err := s.Metadata()
	if err != nil || !json.Valid(again) {
		t.Fatal("Metadata bytes alias store memory")
	}
}

func TestAssetPathAliasesKeepExactMetadataAcrossRestart(t *testing.T) {
	s, c, path := openStore(t)
	data := []byte("same fixture bytes\r\n\x00\xff")
	a := store.Asset{SHA256: hash(data), OriginalPath: `fixture\Spec\A.md`, Kind: "spec", Bytes: data}
	b := a
	b.OriginalPath = `fixture\Spec\B.md`
	b.Kind = "history"
	metaA, err := s.PutAsset(a)
	if err != nil {
		t.Fatal(err)
	}
	metaB, err := s.PutAsset(b)
	if err != nil {
		t.Fatal("identical bytes at another path must be allowed:", err)
	}
	for path, want := range map[string]store.AssetMetadata{a.OriginalPath: metaA, b.OriginalPath: metaB} {
		got, err := s.ResolveAsset(path)
		if err != nil || got != want {
			t.Fatalf("alias %s=%+v, %v", path, got, err)
		}
	}
	got, err := s.Asset(a.SHA256)
	if err != nil || !reflect.DeepEqual(got, a) {
		t.Fatal("adding alias replaced first metadata/blob")
	}
	_, err = s.ResolveAsset(`fixture\spec\A.md`)
	requireError(t, err, store.ErrNotFound)
	changed := a
	changed.Bytes = []byte("new bytes at same path")
	changed.SHA256 = hash(changed.Bytes)
	_, err = s.PutAsset(changed)
	requireError(t, err, store.ErrConflict)
	_, err = s.Asset(changed.SHA256)
	requireError(t, err, store.ErrNotFound)
	changed = a
	changed.Kind = "changed-kind"
	_, err = s.PutAsset(changed)
	requireError(t, err, store.ErrConflict)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path, store.WithClock(c.Now))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	for path, want := range map[string]store.AssetMetadata{a.OriginalPath: metaA, b.OriginalPath: metaB} {
		got, err := reopened.ResolveAsset(path)
		if err != nil || got != want {
			t.Fatalf("persisted alias %s=%+v, %v", path, got, err)
		}
	}
	got, err = reopened.Asset(a.SHA256)
	if err != nil || !bytes.Equal(got.Bytes, data) {
		t.Fatal("aliased bytes changed across reopen")
	}
}

func TestAssetEmptyPathIsUnboundAndRawEmptyBytesAreValid(t *testing.T) {
	s, _, _ := openStore(t)
	for _, data := range [][]byte{{}, []byte("unbound fixture")} {
		asset := store.Asset{SHA256: hash(data), Kind: "spec", Bytes: data}
		if _, err := s.PutAsset(asset); err != nil {
			t.Fatal(err)
		}
		got, err := s.Asset(asset.SHA256)
		if err != nil || !bytes.Equal(got.Bytes, data) {
			t.Fatalf("unbound asset=%+v, %v", got, err)
		}
	}
	_, err := s.ResolveAsset("")
	requireError(t, err, store.ErrInvalid)
}

func TestImportAddsSnapshotAliasWithoutOverwritingExistingBlobMetadata(t *testing.T) {
	s, _, _ := openStore(t)
	document := []byte(`{"next_id":2,"items":[{"id":1}],"source":null}`)
	existing := store.Asset{SHA256: hash(document), OriginalPath: "fixture/spec/source.json", Kind: "spec", Bytes: document}
	meta, err := s.PutAsset(existing)
	if err != nil {
		t.Fatal(err)
	}
	req := stage(t, s, "aliased-import", document)
	if _, err := s.Finalize("aliased-import", req); err != nil {
		t.Fatal(err)
	}
	old, err := s.ResolveAsset(existing.OriginalPath)
	if err != nil || old != meta {
		t.Fatal("import overwrote existing source alias")
	}
	source, err := s.ResolveAsset(req.OriginalPath)
	if err != nil || source.SHA256 != req.SHA256 || source.Kind != store.SnapshotKind || source.Bytes != int64(len(document)) {
		t.Fatalf("snapshot alias=%+v, %v", source, err)
	}
	// The root document digest and a spec's own bytes are independent assets.
	spec := store.Asset{SHA256: hash([]byte("standalone spec")), OriginalPath: "fixture/spec/item.txt", Kind: "spec", Bytes: []byte("standalone spec")}
	if _, err := s.PutAsset(spec); err != nil {
		t.Fatal(err)
	}
	resolved, err := s.ResolveAsset(spec.OriginalPath)
	if err != nil || resolved.SHA256 == req.SHA256 || resolved.SHA256 != spec.SHA256 {
		t.Fatal("spec digest was confused with root digest")
	}
}

func TestOpenMigratesPreviousAssetLayoutWithoutLosingLedger(t *testing.T) {
	s, c, path := openStore(t)
	if _, err := s.Update(store.UpdateRequest{RequestID: "seed", Upserts: []json.RawMessage{raw(`{"id":3,"unknown":null}`)}}); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// Construct only a fictional v1 asset record, as written by the initial
	// single-path layout. No real ledger or service data is used.
	data := []byte("legacy fixture\x00\xff")
	meta := store.AssetMetadata{SHA256: hash(data), OriginalPath: "fixture/legacy.txt", Kind: "history", Bytes: int64(len(data))}
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	err = db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket([]byte("control")).Put([]byte("version"), []byte{1}); err != nil {
			return err
		}
		for _, name := range []string{"asset_blobs", "asset_paths"} {
			if err := tx.DeleteBucket([]byte(name)); err != nil {
				return err
			}
		}
		legacy, err := json.Marshal(struct {
			Metadata store.AssetMetadata `json:"metadata"`
			Data     []byte              `json:"data"`
		}{meta, data})
		if err != nil {
			return err
		}
		return tx.Bucket([]byte("assets")).Put([]byte(meta.SHA256), legacy)
	})
	closeErr := db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
	reopened, err := store.Open(path, store.WithClock(c.Now))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if after := snapshot(t, reopened); !reflect.DeepEqual(before, after) {
		t.Fatal("asset migration changed ledger")
	}
	resolved, err := reopened.ResolveAsset(meta.OriginalPath)
	if err != nil || resolved != meta {
		t.Fatalf("legacy alias=%+v, %v", resolved, err)
	}
	asset, err := reopened.Asset(meta.SHA256)
	if err != nil || !bytes.Equal(asset.Bytes, data) {
		t.Fatal("legacy bytes changed")
	}
	other := asset
	other.OriginalPath = "fixture/other-legacy.txt"
	if _, err := reopened.PutAsset(other); err != nil {
		t.Fatal("migrated blob cannot accept another alias:", err)
	}
}

func TestUpdateCreateReplayAndNumericFingerprint(t *testing.T) {
	s, _, _ := openStore(t)
	req := store.UpdateRequest{RequestID: "web-create", Creates: []json.RawMessage{raw(`{"title":"sample","number":9007199254740992}`)}}
	first, err := s.Update(req)
	if err != nil {
		t.Fatal(err)
	}
	lease := acquire(t, s)
	retry, err := s.Update(req)
	if err != nil || !retry.Duplicate || !reflect.DeepEqual(retry.CreatedIDs, first.CreatedIDs) {
		t.Fatalf("create replay=%+v, %v", retry, err)
	}
	req.Creates = []json.RawMessage{raw(`{"title":"sample","number":9007199254740993}`)}
	_, err = s.Update(req)
	requireError(t, err, store.ErrConflict)
	if err := s.Release(lease.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"null", "0", "1"} {
		_, err = s.Update(store.UpdateRequest{RequestID: "explicit-" + id, BaseRevision: 1, Creates: []json.RawMessage{raw(`{"id":` + id + `,"title":"invalid"}`)}})
		requireError(t, err, store.ErrInvalid)
	}
	if snapshot(t, s).Revision != 1 {
		t.Fatal("replay/invalid create advanced ledger")
	}
}

func TestIDAllocationDoesNotOverflowInt64(t *testing.T) {
	s, _, _ := openStore(t)
	_, err := s.Update(store.UpdateRequest{RequestID: "large-id", Upserts: []json.RawMessage{raw(`{"id":9223372036854775806}`)}})
	if err != nil {
		t.Fatal(err)
	}
	_, meta, err := s.Metadata()
	if err != nil {
		t.Fatal(err)
	}
	requireJSON(t, meta, raw(`{"next_id":9223372036854775807}`))
	_, err = s.Update(store.UpdateRequest{RequestID: "overflow", BaseRevision: 1, Creates: []json.RawMessage{raw(`{"title":"too many"}`)}})
	requireError(t, err, store.ErrConflict)
	if snapshot(t, s).Revision != 1 {
		t.Fatal("exhausted allocator advanced revision")
	}
	_, err = s.Update(store.UpdateRequest{RequestID: "max-id", BaseRevision: 1, Upserts: []json.RawMessage{raw(`{"id":9223372036854775807}`)}})
	requireError(t, err, store.ErrInvalid)
	_, err = s.ListSummaries(store.ListOptions{After: math.MaxInt64})
	if err != nil {
		t.Fatal(err)
	}
}

func TestChangeFeedReconstructsImportedAndEditedDocument(t *testing.T) {
	s, _, _ := openStore(t)
	document := []byte(`{"next_id":100,"items":[{"id":9,"lastJob":null},{"id":2}],"oldRoot":false}`)
	req := stage(t, s, "feed", document)
	if _, err := s.Finalize("feed", req); err != nil {
		t.Fatal(err)
	}
	lease := acquire(t, s)
	if _, err := s.Commit(store.CommitRequest{RequestID: "edit", LeaseID: lease.ID, BaseRevision: 1, Upserts: []json.RawMessage{raw(`{"id":9,"lastJob":null,"lastjob":false}`), raw(`{"id":200,"unknown":9007199254740993}`)}, DeleteIDs: []int64{2}}); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(lease.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(store.UpdateRequest{RequestID: "create", BaseRevision: 2, Creates: []json.RawMessage{raw(`{"title":null}`)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(store.UpdateRequest{RequestID: "root-only", BaseRevision: 3, Meta: raw(`{"next_id":202,"Root":null,"root":{"receipt":false}}`)}); err != nil {
		t.Fatal(err)
	}
	var items []json.RawMessage
	var root map[string]json.RawMessage
	after := uint64(0)
	for pages := 0; pages < 10; pages++ {
		page, err := s.Changes(after, 1)
		if err != nil {
			t.Fatal(err)
		}
		for _, change := range page.Changes {
			root = nil // Change.Meta replaces the entire metadata object.
			if err := json.Unmarshal(change.Meta, &root); err != nil {
				t.Fatal(err)
			}
			for _, upsert := range change.Upserts {
				var incoming struct {
					ID int64 `json:"id"`
				}
				if err := json.Unmarshal(upsert, &incoming); err != nil {
					t.Fatal(err)
				}
				replaced := false
				for i, item := range items {
					var existing struct {
						ID int64 `json:"id"`
					}
					if err := json.Unmarshal(item, &existing); err != nil {
						t.Fatal(err)
					}
					if existing.ID == incoming.ID {
						items[i] = upsert
						replaced = true
						break
					}
				}
				if !replaced {
					items = append(items, upsert)
				}
			}
			for _, id := range change.DeleteIDs {
				for i, item := range items {
					var existing struct {
						ID int64 `json:"id"`
					}
					if err := json.Unmarshal(item, &existing); err != nil {
						t.Fatal(err)
					}
					if existing.ID == id {
						items = append(items[:i], items[i+1:]...)
						break
					}
				}
			}
		}
		after = page.NextAfter
		if !page.HasMore {
			break
		}
	}
	root["items"], _ = json.Marshal(items)
	rebuilt, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	want := raw(`{"next_id":202,"Root":null,"root":{"receipt":false},"items":[{"id":9,"lastJob":null,"lastjob":false},{"id":200,"unknown":9007199254740993},{"id":201,"title":null}]}`)
	requireJSON(t, rebuilt, want)
	requireJSON(t, snapshot(t, s).Document, want)
	if after != 4 {
		t.Fatalf("feed ended at %d", after)
	}
}
