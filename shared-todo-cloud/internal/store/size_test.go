package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestJSONSizeMatchesCompactAndOneSpacePretty(t *testing.T) {
	for _, raw := range []string{
		`{}`, `[]`, `[[],{},[null,true,false,-0,1.23000e-9,9007199254740993]]`,
		`{"id":1,"quoted\"key":"a\\b\"c\n\t<>&\u2028","x":[{"Upper":null,"upper":false},[]]}`,
		" \r\n { \"id\":1, \"nested\": [ {\"x\": 2},3 ] } \t\r\n",
	} {
		for _, depth := range []int{0, 2, 7} {
			var compact, pretty bytes.Buffer
			if err := json.Compact(&compact, []byte(raw)); err != nil {
				t.Fatal(err)
			}
			if err := json.Indent(&pretty, compact.Bytes(), strings.Repeat(" ", depth), " "); err != nil {
				t.Fatal(err)
			}
			got, err := measureJSON([]byte(raw), uint64(depth))
			if err != nil || got.Raw != uint64(len(raw)) || got.Compact != uint64(compact.Len()) || got.Pretty != uint64(pretty.Len()) {
				t.Fatalf("size differs from standard JSON formatters at depth %d: %+v, %v", depth, got, err)
			}
		}
	}
}

func assertSizeIndex(t *testing.T, s *Store) {
	t.Helper()
	snap, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var compact, pretty bytes.Buffer
	if err := json.Compact(&compact, snap.Document); err != nil {
		t.Fatal(err)
	}
	if err := json.Indent(&pretty, compact.Bytes(), "", " "); err != nil {
		t.Fatal(err)
	}
	err = s.view(func(tx *bolt.Tx) error {
		total, err := decodeSize(tx.Bucket(bControl).Get(kItemSizes))
		if err != nil {
			return err
		}
		root, err := object(tx.Bucket(bControl).Get(kMeta))
		if err != nil {
			return err
		}
		root["items"] = json.RawMessage("[]")
		base, err := measureJSON(encodeObject(root), 0)
		if err != nil {
			return err
		}
		rawBytes, compactBytes, prettyBytes := base.Raw, base.Compact, base.Pretty
		if total.Count != 0 {
			rawBytes += total.Raw + total.Count - 1
			compactBytes += total.Compact + total.Count - 1
			prettyBytes += total.Pretty + total.Count*4 + 1
		}
		if rawBytes != uint64(len(snap.Document)) || compactBytes != uint64(compact.Len()) || prettyBytes != uint64(pretty.Len()) {
			t.Fatalf("indexed document bytes differ: raw=%d compact=%d pretty=%d", rawBytes, compactBytes, prettyBytes)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDocumentSizeIndexTracksCreatesUpdatesDeletesAndMeta(t *testing.T) {
	s, _ := stateFixture(t)
	assertSizeIndex(t, s)
	first, err := s.Update(UpdateRequest{RequestID: "size-seed", Upserts: []json.RawMessage{
		json.RawMessage(`{ "id":9,"lastJob":null,"lastjob":{"x":[1,{},[]]},"decimal":1.2300e-7 }`),
		json.RawMessage(`{"id":2,"title":"<>&日本語"}`),
	}, Meta: json.RawMessage(`{"next_id":10,"Root":null,"root":[[1,2],{}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	assertSizeIndex(t, s)
	second, err := s.Update(UpdateRequest{RequestID: "size-mixed", BaseRevision: first.Revision, BaseStateID: first.StateID,
		Upserts: []json.RawMessage{json.RawMessage(`{"id":9,"lastJob":false,"lastjob":null,"nested":[[],{"x":"a\nb"}]}`)},
		Creates: []json.RawMessage{json.RawMessage(`{"unknown":{"Big":9007199254740993,"small":null}}`)}, DeleteIDs: []int64{2}})
	if err != nil || len(second.CreatedIDs) != 1 || second.CreatedIDs[0] != 10 {
		t.Fatalf("mixed update: %+v, %v", second, err)
	}
	assertSizeIndex(t, s)
	third, err := s.Update(UpdateRequest{RequestID: "size-meta", BaseRevision: second.Revision, Meta: json.RawMessage(`{"next_id":11,"Root":{"different":[null,false,{"name":"test"}]}}`)})
	if err != nil {
		t.Fatal(err)
	}
	assertSizeIndex(t, s)
	if _, err := s.Update(UpdateRequest{RequestID: "size-delete", BaseRevision: third.Revision, DeleteIDs: []int64{9, 10}}); err != nil {
		t.Fatal(err)
	}
	assertSizeIndex(t, s)
}

func TestSizeIndexMigrationPreservesRootStateHistoryAndReceipts(t *testing.T) {
	s, path := stateFixture(t)
	req := UpdateRequest{RequestID: "size-migration", Upserts: []json.RawMessage{json.RawMessage(`{ "id":3,"lastJob":null,"lastjob":false,"unknown":[9007199254740993,1.2300e-9] }`)}, Meta: json.RawMessage(`{"next_id":4,"stateId":"raw-root-name","Unknown":null}`)}
	receipt, err := s.Update(req)
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	history, err := s.Changes(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	oldHistory, _ := json.Marshal(history)
	if err := s.write(func(tx *bolt.Tx) error {
		if err := tx.DeleteBucket(bItemSizes); err != nil {
			return err
		}
		return tx.Bucket(bControl).Delete(kItemSizes)
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := reopened.Snapshot()
	if err != nil || after.Revision != before.Revision || after.StateID != before.StateID || !bytes.Equal(after.Document, before.Document) {
		t.Fatal("size migration changed authoritative state")
	}
	afterHistory, err := reopened.Changes(0, 10)
	newHistory, _ := json.Marshal(afterHistory)
	if err != nil || !bytes.Equal(oldHistory, newHistory) {
		t.Fatal("size migration changed history")
	}
	replayed, err := reopened.Update(req)
	if err != nil || !replayed.Duplicate || replayed.Revision != receipt.Revision || replayed.StateID != receipt.StateID {
		t.Fatal("size migration changed operation receipt/idempotency")
	}
	assertSizeIndex(t, reopened)
	if _, err := reopened.Update(UpdateRequest{RequestID: "after-size-migration", BaseRevision: receipt.Revision, BaseStateID: receipt.StateID, Creates: []json.RawMessage{json.RawMessage(`{"title":"new"}`)}}); err != nil {
		t.Fatal(err)
	}
	assertSizeIndex(t, reopened)
}

func TestOversizedPCDocumentRejectsWebAndLeaseCommitAtomically(t *testing.T) {
	s, _ := stateFixture(t)
	large := strings.Repeat("x", 33<<20)
	seedItem := json.RawMessage(`{"id":1,"notes":"` + large + `","lastJob":null,"lastjob":false,"unknown":9007199254740993}`)
	if _, err := s.Update(UpdateRequest{RequestID: "bounded-seed", Upserts: []json.RawMessage{seedItem}}); err != nil {
		t.Fatal(err)
	}
	before, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var sizeBefore []byte
	_ = s.view(func(tx *bolt.Tx) error { sizeBefore = bytes.Clone(tx.Bucket(bControl).Get(kItemSizes)); return nil })
	tooLarge := json.RawMessage(`{"id":2,"note":"` + large + `"}`)
	assertRejected := func(t *testing.T, requestID string, err error) {
		t.Helper()
		if !errors.Is(err, ErrDocumentTooLarge) || err.Error() != "invalid: document_too_large" {
			t.Fatalf("wrong size rejection: %v", err)
		}
		after, e := s.Snapshot()
		if e != nil || after.Revision != before.Revision || after.StateID != before.StateID || !bytes.Equal(after.Document, before.Document) {
			t.Fatal("oversized transaction changed root or identity")
		}
		if _, e = s.Operation(requestID); !errors.Is(e, ErrNotFound) {
			t.Fatal("oversized transaction saved an operation receipt")
		}
		if _, e = s.Item(2); !errors.Is(e, ErrNotFound) {
			t.Fatal("oversized transaction leaked item")
		}
		_ = s.view(func(tx *bolt.Tx) error {
			if !bytes.Equal(sizeBefore, tx.Bucket(bControl).Get(kItemSizes)) {
				t.Fatal("oversized transaction changed size totals")
			}
			return nil
		})
		page, e := s.Changes(before.Revision, 10)
		if e != nil || len(page.Changes) != 0 {
			t.Fatal("oversized transaction leaked history")
		}
	}
	_, err = s.Update(UpdateRequest{RequestID: "web-size-rejected", BaseRevision: before.Revision, BaseStateID: before.StateID, Upserts: []json.RawMessage{tooLarge}})
	assertRejected(t, "web-size-rejected", err)
	lease, err := s.Acquire("size-client", "size-acquire")
	if err != nil {
		t.Fatal(err)
	}
	req := CommitRequest{RequestID: "lease-size-rejected", LeaseID: lease.ID, BaseRevision: before.Revision, BaseStateID: before.StateID, Upserts: []json.RawMessage{tooLarge}}
	_, err = s.Commit(req)
	assertRejected(t, req.RequestID, err)
	// A rejected request did not poison idempotency or consume next_id/state.
	req.Upserts = []json.RawMessage{json.RawMessage(`{"id":2,"note":null}`)}
	accepted, err := s.Commit(req)
	if err != nil || accepted.Revision != before.Revision+1 {
		t.Fatalf("corrected request failed: %v", err)
	}
	retry, err := s.Commit(req)
	if err != nil || !retry.Duplicate || retry.StateID != accepted.StateID {
		t.Fatal("corrected operation is not idempotent")
	}
}

func TestCompactDocumentThatExceedsPrettyLimitIsRejected(t *testing.T) {
	s, _ := stateFixture(t)
	if _, err := s.Update(UpdateRequest{RequestID: "pretty-base", Upserts: []json.RawMessage{json.RawMessage(`{"id":1,"note":"` + strings.Repeat("p", 8<<20) + `"}`)}}); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Snapshot()
	// Compact is only 16 KiB; its deeply nested pretty form adds ~64 MB.
	deep := json.RawMessage(`{"id":2,"unknown":` + strings.Repeat("[", 8000) + `null` + strings.Repeat("]", 8000) + `}`)
	_, err := s.Update(UpdateRequest{RequestID: "pretty-rejected", BaseRevision: before.Revision, Upserts: []json.RawMessage{deep}})
	if !errors.Is(err, ErrDocumentTooLarge) {
		t.Fatalf("pretty-only overflow was accepted: %v", err)
	}
	after, _ := s.Snapshot()
	if after.Revision != before.Revision || after.StateID != before.StateID || !bytes.Equal(after.Document, before.Document) {
		t.Fatal("pretty overflow changed state")
	}
	// Root metadata is part of the same budget, even with no changed items.
	meta := json.RawMessage(`{"next_id":2,"Unknown":` + strings.Repeat("[", 8000) + `null` + strings.Repeat("]", 8000) + `}`)
	_, err = s.Update(UpdateRequest{RequestID: "pretty-meta-rejected", BaseRevision: before.Revision, Meta: meta})
	if !errors.Is(err, ErrDocumentTooLarge) {
		t.Fatalf("metadata-only pretty overflow was accepted: %v", err)
	}
	after, _ = s.Snapshot()
	if after.Revision != before.Revision || after.StateID != before.StateID || !bytes.Equal(after.Document, before.Document) {
		t.Fatal("metadata overflow changed state")
	}
	if _, err := s.Operation("pretty-meta-rejected"); !errors.Is(err, ErrNotFound) {
		t.Fatal("metadata overflow saved an operation receipt")
	}
}

func TestChangesByteBudgetSplitsRepeatedMegabyteUpdatesWithoutGaps(t *testing.T) {
	s, _ := stateFixture(t)
	row := json.RawMessage(`{"id":1,"note":"` + strings.Repeat("x", 1<<20) + `"}`)
	first, err := s.Update(UpdateRequest{RequestID: "history-seed", Upserts: []json.RawMessage{row}})
	if err != nil {
		t.Fatal(err)
	}
	// Synthetic historical no-op upserts have the same current document. Seed
	// them in one fixture transaction rather than performing 79 fsync commits.
	err = s.write(func(tx *bolt.Tx) error {
		parent := first.StateID
		for rev := uint64(2); rev <= 80; rev++ {
			id := fmt.Sprintf("%064x", rev)
			if err := putJSON(tx.Bucket(bChanges), key(rev), Change{Revision: rev, StateID: id, ParentStateID: parent, Upserts: []json.RawMessage{row}, DeleteIDs: []int64{}, Meta: json.RawMessage(`{"next_id":2}`)}); err != nil {
				return err
			}
			parent = id
		}
		if err := tx.Bucket(bControl).Put(kRevision, key(80)); err != nil {
			return err
		}
		return tx.Bucket(bControl).Put(kStateID, []byte(parent))
	})
	if err != nil {
		t.Fatal(err)
	}
	after, parent := uint64(0), ""
	pages := 0
	for {
		page, err := s.Changes(after, 128)
		if err != nil {
			t.Fatal(err)
		}
		wire, err := json.Marshal(page)
		if err != nil || len(wire)+1 > MaxChangePageBytes || len(page.Changes) == 0 {
			t.Fatal("changes page exceeds its wire budget or stopped progressing")
		}
		for _, change := range page.Changes {
			if change.Revision != after+1 || parent != "" && change.ParentStateID != parent {
				t.Fatal("byte pagination skipped history or broke lineage")
			}
			after, parent = change.Revision, change.StateID
		}
		if page.NextAfter != after || page.HasMore != (after < 80) || page.StateID != fmt.Sprintf("%064x", 80) {
			t.Fatal("byte pagination returned an invalid cursor/state")
		}
		pages++
		if !page.HasMore {
			break
		}
	}
	if after != 80 || pages < 2 {
		t.Fatal("fixture did not exercise byte pagination")
	}
}
