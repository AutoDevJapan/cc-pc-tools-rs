package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"
)

func stateFixture(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ledger.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}
func TestStateIDChainsAcrossAtomicRevisionsAndPinnedCAS(t *testing.T) {
	s, _ := stateFixture(t)
	rev, initial, err := s.Version()
	if err != nil || rev != 0 || !validDigest(initial) {
		t.Fatal("initial version missing")
	}
	lease, err := s.Acquire("fixture-pc", "fixture-acquire")
	if err != nil {
		t.Fatal(err)
	}
	if lease.StateID != initial || lease.TTLMillis != LeaseDuration.Milliseconds() {
		t.Fatal("lease did not pin state and TTL")
	}
	req := CommitRequest{RequestID: "fixture-commit", LeaseID: lease.ID, BaseRevision: 0, BaseStateID: initial, Upserts: []json.RawMessage{json.RawMessage(`{"id":7,"lastJob":null,"lastjob":"keep","float":1.2300e-7}`)}, Meta: json.RawMessage(`{"next_id":8,"stateId":"original-root-field","unknown":9007199254740993}`)}
	result, err := s.Commit(req)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Changes(0, 1)
	if err != nil {
		t.Fatal(err)
	}
	renewed, err := s.Renew(lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, versionID, _ := s.Version()
	if result.StateID == initial || result.StateID != versionID || snap.StateID != versionID || renewed.StateID != versionID || renewed.Revision != 1 || len(page.Changes) != 1 {
		t.Fatal("state envelope was not updated atomically")
	}
	if page.StateID != versionID || page.Changes[0].StateID != versionID || page.Changes[0].ParentStateID != initial {
		t.Fatal("broken revision parent chain")
	}
	for _, fragment := range []string{`"stateId":"original-root-field"`, `1.2300e-7`, `9007199254740993`, `"lastJob":null`, `"lastjob":"keep"`} {
		if !bytes.Contains(snap.Document, []byte(fragment)) {
			t.Fatal("root/raw schema changed")
		}
	}
	replay, err := s.Commit(req)
	if err != nil || !replay.Duplicate || replay.StateID != result.StateID {
		t.Fatal("receipt did not retain state identity")
	}
	_, afterReplay, _ := s.Version()
	if afterReplay != versionID {
		t.Fatal("replay allocated another state")
	}
	req.RequestID = "fixture-wrong-state"
	req.BaseRevision = 1
	req.BaseStateID = initial
	if _, err = s.Commit(req); !errors.Is(err, ErrConflict) {
		t.Fatal("same revision with different state was accepted")
	}
	afterRevision, afterID, _ := s.Version()
	if afterRevision != 1 || afterID != versionID {
		t.Fatal("rejected state CAS changed the ledger")
	}
	// A transaction which fails after persistRevision rolls its ID, bytes and
	// receipt back together, rather than publishing an identity for partial data.
	rollback := errors.New("fixture rollback")
	err = s.write(func(tx *bolt.Tx) error {
		if e := persistRevision(tx, Change{Revision: 2, Meta: json.RawMessage(`{"next_id":8}`)}, 8); e != nil {
			return e
		}
		return rollback
	})
	if !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	afterRevision, afterID, _ = s.Version()
	if afterRevision != 1 || afterID != versionID {
		t.Fatal("aborted transaction published state ID")
	}
}

func TestImportIdentityIsPersistentAndDifferentForIdenticalNewLedgers(t *testing.T) {
	raw := []byte(`{"next_id":2,"Unknown":{"n":1.00,"null":null},"items":[{"id":1,"spec":"C:/fixture/original.md"}]}`)
	var previous string
	for i := 0; i < 2; i++ {
		s, path := stateFixture(t)
		_, emptyID, _ := s.Version()
		if err := s.ImportChunk("fixture-import", 0, raw); err != nil {
			t.Fatal(err)
		}
		req := FinalizeRequest{SHA256: digest(raw), ChunkCount: 1, Bytes: int64(len(raw)), OriginalPath: "C:/fixture/todo.json"}
		result, err := s.Finalize("fixture-import", req)
		if err != nil {
			t.Fatal(err)
		}
		if result.StateID == "" || result.StateID == emptyID || result.StateID == previous {
			t.Fatal("import reused unrelated state identity")
		}
		page, err := s.Changes(0, 1)
		if err != nil || page.Changes[0].ParentStateID != emptyID || page.Changes[0].StateID != result.StateID {
			t.Fatal("import parent identity lost")
		}
		asset, err := s.Asset(req.SHA256)
		if err != nil || !bytes.Equal(asset.Bytes, raw) {
			t.Fatal("import no longer retained exact original bytes")
		}
		if err = s.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		rev, id, err := reopened.Version()
		if err != nil || rev != 1 || id != result.StateID {
			t.Fatal("restart changed the state identity")
		}
		retry, err := reopened.Finalize("fixture-import", req)
		if err != nil || retry.StateID != id {
			t.Fatal("import replay changed state")
		}
		_ = reopened.Close()
		previous = id
	}
}

func TestRestoredDatabaseForkAtSameRevisionHasDifferentIdentity(t *testing.T) {
	s, path := stateFixture(t)
	first, err := s.Update(UpdateRequest{RequestID: "base", Upserts: []json.RawMessage{json.RawMessage(`{"id":1,"branch":"base"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	backup, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	branchA, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a, err := branchA.Update(UpdateRequest{RequestID: "branch-a", BaseRevision: 1, BaseStateID: first.StateID, Upserts: []json.RawMessage{json.RawMessage(`{"id":1,"branch":"a"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	_ = branchA.Close()
	if err = os.WriteFile(path, backup, 0600); err != nil {
		t.Fatal(err)
	}
	branchB, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer branchB.Close()
	b, err := branchB.Update(UpdateRequest{RequestID: "branch-b", BaseRevision: 1, BaseStateID: first.StateID, Upserts: []json.RawMessage{json.RawMessage(`{"id":1,"branch":"b"}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if a.Revision != 2 || b.Revision != 2 || a.StateID == b.StateID {
		t.Fatal("different rollback branches alias the same version")
	}
	page, err := branchB.Changes(1, 1)
	if err != nil || page.Changes[0].ParentStateID != first.StateID || page.Changes[0].StateID != b.StateID {
		t.Fatal("restored branch lost parent state")
	}
}

func TestVersionReadsOnlyControlAndLegacyOpenDoesNotRewriteRawData(t *testing.T) {
	s, path := stateFixture(t)
	_, err := s.Update(UpdateRequest{RequestID: "fixture", Upserts: []json.RawMessage{json.RawMessage(`{ "id":1, "lastJob":null, "float":1.000 }`)}})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if err = s.write(func(tx *bolt.Tx) error { return tx.Bucket(bControl).Delete(kStateID) }); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := reopened.Snapshot()
	if err != nil || !bytes.Equal(snap.Document, after.Document) || after.StateID == snap.StateID {
		t.Fatal("identity upgrade rewrote data or reused an unverified state")
	}
	if err = reopened.write(func(tx *bolt.Tx) error { return tx.Bucket(bControl).Put(kMeta, []byte("fixture invalid JSON")) }); err != nil {
		t.Fatal(err)
	}
	rev, id, err := reopened.Version()
	if err != nil || rev != 1 || id != after.StateID {
		t.Fatal("cheap Version parsed root JSON")
	}
	if _, err = reopened.Snapshot(); err == nil {
		t.Fatal("invalid root fixture did not exercise parse path")
	}
}

func TestReplayedAcquireReportsRemainingServerTTL(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	s, err := Open(filepath.Join(t.TempDir(), "ttl.db"), WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	lease, err := s.Acquire("fixture-pc", "fixture-retry")
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(30 * time.Second)
	retry, err := s.Acquire("fixture-pc", "fixture-retry")
	if err != nil || retry.ID != lease.ID || retry.StateID != lease.StateID || retry.TTLMillis != 60000 || !retry.ExpiresAt.Equal(lease.ExpiresAt) {
		t.Fatal("replayed acquire extended apparent lease lifetime")
	}
	now = lease.ExpiresAt.Add(-time.Microsecond)
	if _, err = s.Acquire("fixture-pc", "fixture-retry"); !errors.Is(err, ErrLeaseExpired) {
		t.Fatal("submillisecond remainder must not be advertised as a fresh fixed lease")
	}
}
