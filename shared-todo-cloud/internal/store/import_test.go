package store_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"autodevjapan/shared-todo-cloud/internal/store"
)

func TestImportChunksSurviveRestartAndRestoreOriginalBytes(t *testing.T) {
	s, c, path := openStore(t)
	// Larger than one chunk; even whitespace, CRLF and the original array order
	// must be recoverable from the source asset rather than a remarshal.
	note, _ := json.Marshal(strings.Repeat("東", store.MaxChunkBytes/3+100))
	document := []byte(" {\r\n  \"unknown\": {\"count\":900719925474099312345,\"Count\":null},\r\n  \"items\": [" +
		`{ "id":9, "lastJob":null, "lastjob":false, "notes":` + string(note) + `}, {"id":2,"status":"open","absentCase":null}],` +
		"\r\n  \"next_id\":50\r\n}\r\n")
	if len(document) <= store.MaxChunkBytes {
		t.Fatal("fixture does not cross a chunk boundary")
	}
	first := document[:store.MaxChunkBytes]
	if err := s.ImportChunk("restart-import", 0, first); err != nil {
		t.Fatal(err)
	}
	if err := s.ImportChunk("restart-import", 0, first); err != nil {
		t.Fatal("identical chunk retry failed:", err)
	}
	requireError(t, s.ImportChunk("restart-import", 0, []byte("different")), store.ErrConflict)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path, store.WithClock(c.Now))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.ImportChunk("restart-import", 1, document[store.MaxChunkBytes:]); err != nil {
		t.Fatal(err)
	}
	req := store.FinalizeRequest{SHA256: hash(document), ChunkCount: 2, Bytes: int64(len(document)), OriginalPath: `fixture\archive\MixedCase.json`}
	result, err := reopened.Finalize("restart-import", req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Revision != 1 || result.Items != 2 || result.Bytes != int64(len(document)) || result.SHA256 != hash(document) {
		t.Fatalf("import=%+v", result)
	}
	state := snapshot(t, reopened)
	requireJSON(t, state.Document, document)
	source, err := reopened.Asset(req.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(source.Bytes, document) || hash(source.Bytes) != result.SHA256 || source.OriginalPath != req.OriginalPath || source.Kind != store.SnapshotKind {
		t.Fatal("original bytes/path/kind not retained exactly")
	}
	if _, err := reopened.Update(store.UpdateRequest{RequestID: "advance", BaseRevision: 1, Creates: []json.RawMessage{raw(`{"title":"next"}`)}}); err != nil {
		t.Fatal(err)
	}
	retry, err := reopened.Finalize("restart-import", req)
	if err != nil || !reflect.DeepEqual(result, retry) {
		t.Fatalf("finalize retry=%+v, %v", retry, err)
	}
	if snapshot(t, reopened).Revision != 2 {
		t.Fatal("finalize retry overwrote an advanced ledger")
	}
	changes, err := reopened.Changes(0, 1)
	if err != nil || !changes.HasMore || len(changes.Changes) != 1 || len(changes.Changes[0].Upserts) != 2 {
		t.Fatalf("import change not recorded: %v", err)
	}
	requireError(t, reopened.ImportChunk("restart-import", 0, first), store.ErrConflict)
	req.OriginalPath = "changed"
	_, err = reopened.Finalize("restart-import", req)
	requireError(t, err, store.ErrConflict)
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	third, err := store.Open(path, store.WithClock(c.Now))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = third.Close() })
	source, err = third.Asset(hash(document))
	if err != nil || !bytes.Equal(source.Bytes, document) {
		t.Fatalf("source asset did not survive restart: %v", err)
	}
	req.OriginalPath = `fixture\archive\MixedCase.json`
	if _, err := third.Finalize("restart-import", req); err != nil {
		t.Fatal("finalize receipt did not survive restart:", err)
	}
}

func TestImportValidatesCompletenessSizeHashAndParsing(t *testing.T) {
	valid := []byte(`{"next_id":10,"items":[{"id":3,"unknown":null}],"root":{"receipt":false}}`)
	tests := []struct {
		name   string
		data   []byte
		mutate func(*store.FinalizeRequest)
		chunks map[int][]byte
		want   error
	}{
		{name: "incomplete", data: valid, mutate: func(r *store.FinalizeRequest) { r.ChunkCount = 2 }, want: store.ErrConflict},
		{name: "missing-index", data: valid, chunks: map[int][]byte{1: valid}, want: store.ErrConflict},
		{name: "extra-chunk", data: valid, chunks: map[int][]byte{0: valid, 1: []byte(" ")}, want: store.ErrConflict},
		{name: "size-too-small", data: valid, mutate: func(r *store.FinalizeRequest) { r.Bytes-- }, want: store.ErrInvalid},
		{name: "size-too-large", data: valid, mutate: func(r *store.FinalizeRequest) { r.Bytes++ }, want: store.ErrInvalid},
		{name: "hash", data: valid, mutate: func(r *store.FinalizeRequest) { r.SHA256 = hash([]byte("wrong")) }, want: store.ErrInvalid},
		{name: "malformed", data: []byte(`{"items":[`), want: store.ErrInvalid},
		{name: "trailing-document", data: []byte(`{"items":[]} {}`), want: store.ErrInvalid},
		{name: "root-null", data: []byte(`null`), want: store.ErrInvalid},
		{name: "items-null", data: []byte(`{"items":null}`), want: store.ErrInvalid},
		{name: "items-object", data: []byte(`{"items":{}}`), want: store.ErrInvalid},
		{name: "items-missing", data: []byte(`{"Items":[]}`), want: store.ErrInvalid},
		{name: "zero-id", data: []byte(`{"next_id":2,"items":[{"id":0}]}`), want: store.ErrInvalid},
		{name: "null-id", data: []byte(`{"next_id":2,"items":[{"id":null}]}`), want: store.ErrInvalid},
		{name: "string-id", data: []byte(`{"next_id":2,"items":[{"id":"1"}]}`), want: store.ErrInvalid},
		{name: "duplicate-id", data: []byte(`{"next_id":2,"items":[{"id":1},{"id":1}]}`), want: store.ErrConflict},
		{name: "next-id-too-low", data: []byte(`{"next_id":3,"items":[{"id":3}]}`), want: store.ErrInvalid},
		{name: "next-id-null", data: []byte(`{"next_id":null,"items":[]}`), want: store.ErrInvalid},
		{name: "duplicate-root-key", data: []byte(`{"items":[],"items":[{"id":1}],"next_id":2}`), want: store.ErrInvalid},
		{name: "duplicate-nested-key", data: []byte(`{"items":[{"id":1,"meta":{"x":1,"x":2}}],"next_id":2}`), want: store.ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := openStore(t)
			chunks := tc.chunks
			if chunks == nil {
				chunks = map[int][]byte{0: tc.data}
			}
			for index, chunk := range chunks {
				if err := s.ImportChunk("bad-import", index, chunk); err != nil {
					t.Fatal(err)
				}
			}
			req := store.FinalizeRequest{SHA256: hash(tc.data), ChunkCount: 1, Bytes: int64(len(tc.data))}
			if tc.mutate != nil {
				tc.mutate(&req)
			}
			_, err := s.Finalize("bad-import", req)
			requireError(t, err, tc.want)
			state := snapshot(t, s)
			if state.Revision != 0 {
				t.Fatal("failed import changed revision")
			}
			requireJSON(t, state.Document, raw(`{"items":[],"next_id":1}`))
			page, err := s.ListSummaries(store.ListOptions{})
			if err != nil || len(page.Items) != 0 {
				t.Fatal("failed import left summaries")
			}
			changes, err := s.Changes(0, 10)
			if err != nil || len(changes.Changes) != 0 {
				t.Fatal("failed import left changes")
			}
			_, err = s.Asset(req.SHA256)
			requireError(t, err, store.ErrNotFound)
			// Failure must leave the virgin ledger usable, with no half import.
			good := stage(t, s, "good-import", valid)
			if _, err := s.Finalize("good-import", good); err != nil {
				t.Fatal("failed import poisoned ledger:", err)
			}
		})
	}
}

func TestImportAssetConflictRollsBackInitialization(t *testing.T) {
	s, _, _ := openStore(t)
	document := []byte(`{"next_id":2,"items":[{"id":1,"status":"open"}]}`)
	// The asset conflict occurs after items, summary and change writes have run.
	asset := store.Asset{SHA256: hash(document), Kind: "spec", OriginalPath: "fixture/spec.json", Bytes: document}
	if _, err := s.PutAsset(asset); err != nil {
		t.Fatal(err)
	}
	req := stage(t, s, "atomic-import", document)
	// Reusing a source path is a conflict; a different path for the same bytes
	// is intentionally allowed and must not overwrite either alias.
	req.OriginalPath = asset.OriginalPath
	_, err := s.Finalize("atomic-import", req)
	requireError(t, err, store.ErrConflict)
	if snapshot(t, s).Revision != 0 {
		t.Fatal("partial initialization committed")
	}
	_, err = s.Item(1)
	requireError(t, err, store.ErrNotFound)
	page, err := s.ListSummaries(store.ListOptions{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("partial summary committed")
	}
	changes, err := s.Changes(0, 100)
	if err != nil || len(changes.Changes) != 0 {
		t.Fatal("partial history committed")
	}
	restored, err := s.Asset(asset.SHA256)
	if err != nil || !reflect.DeepEqual(asset, restored) {
		t.Fatal("failed import changed existing asset")
	}
	// Chunk receipt/deletion must also roll back, so it is still the same failure.
	_, err = s.Finalize("atomic-import", req)
	requireError(t, err, store.ErrConflict)
}

func TestImportCanCompleteAfterMissingChunkArrives(t *testing.T) {
	s, _, _ := openStore(t)
	document := []byte(`{"next_id":5,"items":[{"id":4}]}`)
	split := len(document) / 2
	if err := s.ImportChunk("incomplete", 0, document[:split]); err != nil {
		t.Fatal(err)
	}
	req := store.FinalizeRequest{SHA256: hash(document), Bytes: int64(len(document)), ChunkCount: 2}
	_, err := s.Finalize("incomplete", req)
	requireError(t, err, store.ErrConflict)
	if err := s.ImportChunk("incomplete", 1, document[split:]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finalize("incomplete", req); err != nil {
		t.Fatal(err)
	}
}

func TestImportRefusesExternalLeaseAndAnyPreviouslyWrittenLedger(t *testing.T) {
	s, _, _ := openStore(t)
	document := []byte(`{"next_id":2,"items":[{"id":1}]}`)
	req := stage(t, s, "initial", document)
	lease := acquire(t, s)
	_, err := s.Finalize("initial", req)
	requireError(t, err, store.ErrLocked)
	if err := s.Release(lease.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Finalize("initial", req); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(store.UpdateRequest{RequestID: "empty-ledger", BaseRevision: 1, DeleteIDs: []int64{1}}); err != nil {
		t.Fatal(err)
	}
	second := stage(t, s, "overwrite", document)
	_, err = s.Finalize("overwrite", second)
	requireError(t, err, store.ErrConflict)
	state := snapshot(t, s)
	if state.Revision != 2 {
		t.Fatal("overwrite changed revision")
	}
	requireJSON(t, state.Document, raw(`{"items":[],"next_id":2}`))
}

func TestImportChunkLimitsAndMissingImport(t *testing.T) {
	s, _, _ := openStore(t)
	requireError(t, s.ImportChunk("fixture", -1, []byte("x")), store.ErrInvalid)
	requireError(t, s.ImportChunk("fixture", 0, make([]byte, store.MaxChunkBytes+1)), store.ErrInvalid)
	_, err := s.Finalize("missing", store.FinalizeRequest{SHA256: hash([]byte("x")), Bytes: 1, ChunkCount: 1})
	requireError(t, err, store.ErrNotFound)
}
