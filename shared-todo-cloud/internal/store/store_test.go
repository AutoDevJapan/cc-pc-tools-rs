package store_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"autodevjapan/shared-todo-cloud/internal/store"
)

type clock struct {
	mu       sync.Mutex
	t        time.Time
	sequence []time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)} }
func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sequence) != 0 {
		c.t = c.sequence[0]
		c.sequence = c.sequence[1:]
	}
	return c.t
}
func (c *clock) Advance(d time.Duration)     { c.mu.Lock(); defer c.mu.Unlock(); c.t = c.t.Add(d) }
func (c *clock) Sequence(times ...time.Time) { c.mu.Lock(); defer c.mu.Unlock(); c.sequence = times }

func openStore(t *testing.T) (*store.Store, *clock, string) {
	t.Helper()
	c := newClock()
	path := filepath.Join(t.TempDir(), "ledger.db")
	s, err := store.Open(path, store.WithClock(c.Now))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, c, path
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func hash(b []byte) string { v := sha256.Sum256(b); return hex.EncodeToString(v[:]) }

func requireError(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	var typed *store.Error
	if !errors.As(err, &typed) || typed.Code == "" {
		t.Fatalf("error is not typed: %v", err)
	}
}

func requireJSON(t *testing.T, got, want []byte) {
	t.Helper()
	decode := func(b []byte) any {
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		var result any
		if err := d.Decode(&result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if !reflect.DeepEqual(decode(got), decode(want)) {
		t.Fatalf("JSON differs (got %d bytes, want %d bytes)", len(got), len(want))
	}
}

func snapshot(t *testing.T, s *store.Store) store.Snapshot {
	t.Helper()
	v, err := s.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func acquire(t *testing.T, s *store.Store) store.Lease {
	t.Helper()
	v, err := s.Acquire("fixture-client", "acquire-fixture")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func stage(t *testing.T, s *store.Store, id string, document []byte) store.FinalizeRequest {
	t.Helper()
	count := 0
	for start := 0; start < len(document); start += store.MaxChunkBytes {
		end := start + store.MaxChunkBytes
		if end > len(document) {
			end = len(document)
		}
		if err := s.ImportChunk(id, count, document[start:end]); err != nil {
			t.Fatal(err)
		}
		count++
	}
	return store.FinalizeRequest{SHA256: hash(document), ChunkCount: count, Bytes: int64(len(document)), OriginalPath: "fixture/archive/todo.json"}
}

func TestRestartRetainsLedgerReceiptsLeaseAndAssets(t *testing.T) {
	s, c, path := openStore(t)
	item7 := raw(`{ "id":7, "title":"seven", "status":"open", "project":"sample", "lastJob":null, "lastjob":{"counter":9007199254740993}, "notes":"private fixture text" }`)
	item2 := raw(`{"id":2,"status":null,"nested":{"unknown":false}}`)
	meta := raw(`{"next_id":100,"Root":null,"root":[1,true],"large":123456789012345678901234567890}`)
	first, err := s.Update(store.UpdateRequest{RequestID: "web-first", Upserts: []json.RawMessage{item7, item2}, Meta: meta})
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 || first.Upserts != 2 {
		t.Fatalf("result = %+v", first)
	}
	lease := acquire(t, s)
	c.Advance(25 * time.Second)
	lease, err = s.Renew(lease.ID)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte{0, '\r', '\n', 255, 128, 1}
	asset := store.Asset{SHA256: hash(data), OriginalPath: `fixture\history\seven.txt`, Kind: "history", Bytes: data}
	if _, err := s.PutAsset(asset); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, s)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := store.Open(path, store.WithClock(c.Now))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	after := snapshot(t, reopened)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("snapshot changed across reopen")
	}
	wantDoc := raw(`{"items":[` + string(item7) + `,` + string(item2) + `],"next_id":100,"Root":null,"root":[1,true],"large":123456789012345678901234567890}`)
	requireJSON(t, after.Document, wantDoc)
	item, err := reopened.Item(7)
	if err != nil {
		t.Fatal(err)
	}
	if item.Revision != 1 || !bytes.Equal(item.Item, item7) {
		t.Fatal("raw item changed")
	}
	operation, err := reopened.Operation("web-first")
	if err != nil || !reflect.DeepEqual(first, operation) {
		t.Fatalf("operation = %+v, %v", operation, err)
	}
	savedLease, err := reopened.Acquire("fixture-client", "acquire-fixture")
	if err != nil || !reflect.DeepEqual(lease, savedLease) {
		t.Fatalf("lease = %+v, %v", savedLease, err)
	}
	restored, err := reopened.Asset(asset.SHA256)
	if err != nil || !reflect.DeepEqual(asset, restored) {
		t.Fatalf("asset not restored exactly: %v", err)
	}
	page, err := reopened.ListSummaries(store.ListOptions{Status: "open", Project: "sample"})
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != 7 {
		t.Fatalf("summaries = %+v, %v", page, err)
	}
	changes, err := reopened.Changes(0, 10)
	if err != nil || len(changes.Changes) != 1 || changes.NextAfter != 1 {
		t.Fatalf("changes = %+v, %v", changes, err)
	}
	_, err = reopened.Item(99)
	requireError(t, err, store.ErrNotFound)
}

func TestConcurrentCreatesAllocateUniqueIDsWithCAS(t *testing.T) {
	s, _, _ := openStore(t)
	const workers = 24
	start := make(chan struct{})
	ids := make(chan int64, workers)
	errorsFound := make(chan error, workers)
	var wg sync.WaitGroup
	for n := 0; n < workers; n++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			<-start
			for attempt := 0; attempt < workers*2; attempt++ {
				state, err := s.Snapshot()
				if err != nil {
					errorsFound <- err
					return
				}
				result, err := s.Update(store.UpdateRequest{RequestID: fmt.Sprintf("create-%d", n), BaseRevision: state.Revision,
					Creates: []json.RawMessage{raw(fmt.Sprintf(`{"title":"fixture-%d","unknown":null}`, n))}})
				if errors.Is(err, store.ErrConflict) {
					continue
				}
				if err != nil {
					errorsFound <- err
					return
				}
				if len(result.CreatedIDs) != 1 {
					errorsFound <- fmt.Errorf("unexpected result: %+v", result)
					return
				}
				ids <- result.CreatedIDs[0]
				return
			}
			errorsFound <- errors.New("CAS retries exhausted")
		}(n)
	}
	close(start)
	wg.Wait()
	close(errorsFound)
	close(ids)
	for err := range errorsFound {
		t.Error(err)
	}
	var got []int64
	for id := range ids {
		got = append(got, id)
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	if len(got) != workers {
		t.Fatalf("created %d of %d", len(got), workers)
	}
	for i, id := range got {
		if id != int64(i+1) {
			t.Fatalf("IDs = %v", got)
		}
	}
	state := snapshot(t, s)
	var root map[string]json.RawMessage
	if err := json.Unmarshal(state.Document, &root); err != nil {
		t.Fatal(err)
	}
	if state.Revision != workers || string(root["next_id"]) != "25" {
		t.Fatalf("revision=%d next=%s", state.Revision, root["next_id"])
	}
}

func TestCommitIdempotencyIncludesContentAndWorksAfterExpiry(t *testing.T) {
	s, c, _ := openStore(t)
	lease := acquire(t, s)
	req := store.CommitRequest{RequestID: "commit-1", LeaseID: lease.ID, Upserts: []json.RawMessage{raw(`{"id":1,"title":"first","lastJob":null}`)}}
	first, err := s.Commit(req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Revision != 1 || first.Duplicate {
		t.Fatalf("result = %+v", first)
	}
	c.Advance(store.LeaseDuration)
	// Same semantic object in a different order is still the same request.
	req.Upserts[0] = raw(`{ "lastJob":null, "title":"first", "id":1 }`)
	again, err := s.Commit(req)
	if err != nil || !again.Duplicate || again.Revision != first.Revision {
		t.Fatalf("replay = %+v, %v", again, err)
	}
	for _, changed := range []json.RawMessage{
		raw(`{"id":1,"title":"second","lastJob":null}`),
		raw(`{"id":1,"title":"first"}`),
		raw(`{"id":1,"title":"first","lastjob":null}`),
	} {
		req.Upserts[0] = changed
		_, err := s.Commit(req)
		requireError(t, err, store.ErrConflict)
	}
	if snapshot(t, s).Revision != 1 {
		t.Fatal("replays changed revision")
	}
	result, err := s.Operation("commit-1")
	if err != nil || result.Duplicate || result.Revision != 1 {
		t.Fatalf("operation = %+v, %v", result, err)
	}
	_, err = s.Operation("missing")
	requireError(t, err, store.ErrNotFound)
}

func TestConcurrentIdenticalRequestsCommitOnce(t *testing.T) {
	s, _, _ := openStore(t)
	lease := acquire(t, s)
	req := store.CommitRequest{RequestID: "same", LeaseID: lease.ID, Upserts: []json.RawMessage{raw(`{"id":1}`)}}
	start := make(chan struct{})
	results := make(chan store.CommitResult, 12)
	failures := make(chan error, 12)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			v, err := s.Commit(req)
			if err != nil {
				failures <- err
			} else {
				results <- v
			}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	original, duplicates := 0, 0
	for result := range results {
		if result.Revision != 1 {
			t.Fatalf("result = %+v", result)
		}
		if result.Duplicate {
			duplicates++
		} else {
			original++
		}
	}
	if original != 1 || duplicates != 11 {
		t.Fatalf("originals=%d duplicates=%d", original, duplicates)
	}
	changes, err := s.Changes(0, 100)
	if err != nil || len(changes.Changes) != 1 {
		t.Fatalf("changes: %v", err)
	}
}

func TestLeaseOwnershipRenewalAndExactDeadline(t *testing.T) {
	s, c, _ := openStore(t)
	lease := acquire(t, s)
	if !lease.ExpiresAt.Equal(c.Now().Add(90 * time.Second)) {
		t.Fatal("lease duration is not 90 seconds")
	}
	_, err := s.Acquire("fixture-client", "different-request")
	requireError(t, err, store.ErrLocked)
	_, err = s.Acquire("other-client", "acquire-fixture")
	requireError(t, err, store.ErrConflict)
	_, err = s.Renew("wrong-owner")
	requireError(t, err, store.ErrLocked)
	requireError(t, s.Release("wrong-owner"), store.ErrLocked)
	_, err = s.Commit(store.CommitRequest{RequestID: "wrong", LeaseID: "wrong-owner"})
	requireError(t, err, store.ErrLocked)
	_, err = s.Update(store.UpdateRequest{RequestID: "blocked-web", Creates: []json.RawMessage{raw(`{"title":"test"}`)}})
	requireError(t, err, store.ErrLocked)
	c.Advance(25 * time.Second)
	renewed, err := s.Renew(lease.ID)
	if err != nil || renewed.ID != lease.ID || !renewed.ExpiresAt.Equal(c.Now().Add(90*time.Second)) {
		t.Fatalf("renew = %+v, %v", renewed, err)
	}
	c.Advance(90 * time.Second)
	_, err = s.Renew(lease.ID)
	requireError(t, err, store.ErrLeaseExpired)
	_, err = s.Commit(store.CommitRequest{RequestID: "expired", LeaseID: lease.ID})
	requireError(t, err, store.ErrLeaseExpired)
	_, err = s.Acquire("fixture-client", "acquire-fixture")
	requireError(t, err, store.ErrLeaseExpired)
	newLease, err := s.Acquire("new-client", "new-acquire")
	if err != nil || newLease.ID == lease.ID {
		t.Fatalf("new lease = %+v, %v", newLease, err)
	}
	requireError(t, s.Release(lease.ID), store.ErrLocked)
	_, err = s.Renew(lease.ID)
	requireError(t, err, store.ErrLocked)
	if _, err := s.Renew(newLease.ID); err != nil {
		t.Fatal("old release damaged new lease:", err)
	}
	if err := s.Release(newLease.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(newLease.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(store.UpdateRequest{RequestID: "unblocked-web", Creates: []json.RawMessage{raw(`{"title":"test"}`)}}); err != nil {
		t.Fatal(err)
	}
}

func TestExpiredDuringCommitRollsBackEverything(t *testing.T) {
	s, c, _ := openStore(t)
	lease := acquire(t, s)
	now := c.Now()
	c.Sequence(now, now.Add(store.LeaseDuration))
	_, err := s.Commit(store.CommitRequest{RequestID: "expires-during-write", LeaseID: lease.ID,
		Upserts: []json.RawMessage{raw(`{"id":8,"status":"open"}`)}})
	requireError(t, err, store.ErrLeaseExpired)
	if snapshot(t, s).Revision != 0 {
		t.Fatal("expired write changed revision")
	}
	_, err = s.Item(8)
	requireError(t, err, store.ErrNotFound)
	_, err = s.Operation("expires-during-write")
	requireError(t, err, store.ErrNotFound)
	page, err := s.ListSummaries(store.ListOptions{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal("expired write leaked summary")
	}
	changes, err := s.Changes(0, 100)
	if err != nil || len(changes.Changes) != 0 {
		t.Fatal("expired write leaked history")
	}
	result, err := s.Update(store.UpdateRequest{RequestID: "after-expiry", Creates: []json.RawMessage{raw(`{"title":"fresh"}`)}})
	if err != nil || !reflect.DeepEqual(result.CreatedIDs, []int64{1}) {
		t.Fatalf("rolled back allocator = %+v, %v", result, err)
	}
}

func TestRejectedCommitDoesNotLeavePartialWrites(t *testing.T) {
	s, _, _ := openStore(t)
	lease := acquire(t, s)
	if _, err := s.Commit(store.CommitRequest{RequestID: "seed", LeaseID: lease.ID, Upserts: []json.RawMessage{raw(`{"id":3,"title":"before"}`)}}); err != nil {
		t.Fatal(err)
	}
	before := snapshot(t, s)
	tests := []struct {
		name string
		req  store.CommitRequest
		want error
	}{
		{"stale", store.CommitRequest{BaseRevision: 0, Upserts: []json.RawMessage{raw(`{"id":3,"title":"stale"}`)}}, store.ErrConflict},
		{"duplicate", store.CommitRequest{BaseRevision: 1, Upserts: []json.RawMessage{raw(`{"id":4}`), raw(`{"id":4,"title":"duplicate"}`)}}, store.ErrConflict},
		{"delete-overlap", store.CommitRequest{BaseRevision: 1, Upserts: []json.RawMessage{raw(`{"id":3}`)}, DeleteIDs: []int64{3}}, store.ErrConflict},
		{"duplicate-delete", store.CommitRequest{BaseRevision: 1, DeleteIDs: []int64{3, 3}}, store.ErrConflict},
		{"missing-delete", store.CommitRequest{BaseRevision: 1, Upserts: []json.RawMessage{raw(`{"id":4}`)}, DeleteIDs: []int64{77}}, store.ErrNotFound},
		{"meta-with-items", store.CommitRequest{BaseRevision: 1, Meta: raw(`{"items":[]}`)}, store.ErrInvalid},
		{"null-meta", store.CommitRequest{BaseRevision: 1, Meta: raw(`null`)}, store.ErrInvalid},
		{"duplicate-key", store.CommitRequest{BaseRevision: 1, Upserts: []json.RawMessage{raw(`{"id":4,"child":{"x":1,"x":2}}`)}}, store.ErrInvalid},
		{"invalid-id", store.CommitRequest{BaseRevision: 1, Upserts: []json.RawMessage{raw(`{"id":4.0}`)}}, store.ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.req.RequestID, tc.req.LeaseID = tc.name, lease.ID
			_, err := s.Commit(tc.req)
			requireError(t, err, tc.want)
			if got := snapshot(t, s); !reflect.DeepEqual(got, before) {
				t.Fatal("rejected mutation changed snapshot")
			}
			_, err = s.Operation(tc.name)
			requireError(t, err, store.ErrNotFound)
		})
	}
	_, err := s.Commit(store.CommitRequest{RequestID: "corrected", LeaseID: lease.ID, BaseRevision: 1, Upserts: []json.RawMessage{raw(`{"id":3,"title":"after"}`)}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMetadataReplacementAndAllocationHighWater(t *testing.T) {
	s, _, _ := openStore(t)
	_, err := s.Update(store.UpdateRequest{RequestID: "seed", Upserts: []json.RawMessage{raw(`{"id":40}`)}, Meta: raw(`{"next_id":100,"unknown":{"enabled":null},"Unknown":false}`)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Update(store.UpdateRequest{RequestID: "delete", BaseRevision: 1, DeleteIDs: []int64{40}})
	if err != nil {
		t.Fatal(err)
	}
	state := snapshot(t, s)
	requireJSON(t, state.Document, raw(`{"items":[],"next_id":100,"unknown":{"enabled":null},"Unknown":false}`))
	created, err := s.Update(store.UpdateRequest{RequestID: "create", BaseRevision: 2, Meta: raw(`{"next_id":2,"replacement":[true,null,9e50]}`), Creates: []json.RawMessage{raw(`{"title":null,"unindexed":{"next":false}}`)}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(created.CreatedIDs, []int64{100}) {
		t.Fatalf("allocation reused a retired ID: %+v", created)
	}
	requireJSON(t, snapshot(t, s).Document, raw(`{"items":[{"id":100,"title":null,"unindexed":{"next":false}}],"next_id":101,"replacement":[true,null,9e50]}`))
	changes, err := s.Changes(2, 1)
	if err != nil {
		t.Fatal(err)
	}
	requireJSON(t, changes.Changes[0].Meta, raw(`{"next_id":101,"replacement":[true,null,9e50]}`))
}

func TestRawUnknownCaseNumbersNullAndLargeItem(t *testing.T) {
	s, _, _ := openStore(t)
	lease := acquire(t, s)
	notes := strings.Repeat("fixture text \u65e5\u672c", 100000)
	quoted, _ := json.Marshal(notes)
	item := raw(`{ "id":1,"title":null,"lastJob":{"receipt":null},"lastjob":false,"LastJob":[1e99,-0,900719925474099312345],"nullable":null,"notes":` + string(quoted) + ` }`)
	meta := raw(`{"next_id":2,"Meta":null,"meta":{"x":900719925474099312345,"X":0.000001e-40},"itemsCase":"kept"}`)
	if _, err := s.Commit(store.CommitRequest{RequestID: "large", LeaseID: lease.ID, Upserts: []json.RawMessage{item}, Meta: meta}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Item(1)
	if err != nil || !bytes.Equal(got.Item, item) {
		t.Fatalf("large raw item changed: %v", err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(snapshot(t, s).Document, &root); err != nil {
		t.Fatal(err)
	}
	var wantMeta map[string]json.RawMessage
	if err := json.Unmarshal(meta, &wantMeta); err != nil {
		t.Fatal(err)
	}
	for k, v := range wantMeta {
		if !bytes.Equal(root[k], v) {
			t.Fatalf("root field %s changed", k)
		}
	}
	if _, exists := root["nullable"]; exists {
		t.Fatal("absent root field became present")
	}
	page, err := s.ListSummaries(store.ListOptions{})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("summaries: %v", err)
	}
	encoded, err := json.Marshal(page.Items[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > 100 || bytes.Contains(encoded, []byte("notes")) || string(page.Items[0].Title) != "null" || page.Items[0].Status != nil {
		t.Fatal("summary loses missing/null or includes full notes")
	}
	changes, err := s.Changes(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	requireJSON(t, changes.Changes[0].Upserts[0], item)
}

func TestSummaryFiltersAndPagination(t *testing.T) {
	s, _, _ := openStore(t)
	items := []json.RawMessage{
		raw(`{"id":9,"status":"open","project":"sample","title":"nine","notes":"excluded"}`),
		raw(`{"id":1,"status":"closed","project":"sample"}`),
		raw(`{"id":3,"status":"open","project":"sample"}`),
		raw(`{"id":5,"status":"open","project":"other"}`),
		raw(`{"id":7,"status":null,"project":"sample"}`),
	}
	if _, err := s.Update(store.UpdateRequest{RequestID: "seed", Upserts: items}); err != nil {
		t.Fatal(err)
	}
	first, err := s.ListSummaries(store.ListOptions{Status: "open", Project: "sample", Limit: 1})
	if err != nil || len(first.Items) != 1 || first.Items[0].ID != 3 || first.NextAfter != 3 || !first.HasMore {
		t.Fatalf("first=%+v, %v", first, err)
	}
	second, err := s.ListSummaries(store.ListOptions{Status: "open", Project: "sample", After: first.NextAfter, Limit: 1})
	if err != nil || len(second.Items) != 1 || second.Items[0].ID != 9 || second.NextAfter != 9 || second.HasMore {
		t.Fatalf("second=%+v, %v", second, err)
	}
	empty, err := s.ListSummaries(store.ListOptions{After: math.MaxInt64})
	if err != nil || len(empty.Items) != 0 || empty.NextAfter != math.MaxInt64 || empty.HasMore {
		t.Fatalf("empty=%+v, %v", empty, err)
	}
	_, err = s.ListSummaries(store.ListOptions{After: -1})
	requireError(t, err, store.ErrInvalid)
	_, err = s.ListSummaries(store.ListOptions{Limit: store.MaxLimit + 1})
	requireError(t, err, store.ErrInvalid)
}

func TestSummarySearchPagination(t *testing.T) {
	s, _, _ := openStore(t)
	// The first 100 summaries do not match the title query. A page-size limit
	// must count matching summaries instead of truncating the cursor scan.
	items := make([]json.RawMessage, 0, 110)
	for id := 1; id <= 100; id++ {
		items = append(items, raw(fmt.Sprintf(`{"id":%d,"title":"routine","status":"open","project":"sample"}`, id)))
	}
	items = append(items,
		raw(`{"id":111,"title":"First NEEDLE task","status":"open","project":"sample"}`),
		raw(`{"id":120,"title":"needle closed","status":"closed","project":"sample"}`),
		raw(`{"id":130,"title":"needle other project","status":"open","project":"other"}`),
		raw(`{"id":145,"title":"second Needle task","status":"open","project":"sample"}`),
		raw(`{"id":180,"title":{"text":"needle"},"status":"open","project":"sample"}`),
		raw(`{"id":181,"title":null,"status":"open","project":"sample"}`),
		raw(`{"id":190,"title":"Ticket 181","status":"open","project":"sample"}`),
		raw(`{"id":199,"title":"third needle task","status":"open","project":"sample"}`),
		raw(`{"id":230,"title":"needle later closed","status":"closed","project":"sample"}`),
		raw(`{"id":240,"title":"needle later project","status":"open","project":"other"}`),
	)
	if _, err := s.Update(store.UpdateRequest{RequestID: "search-seed", Upserts: items}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query string
		ids   []int64
	}{
		{query: "nEeDlE", ids: []int64{111, 145, 199}},
		{query: "181", ids: []int64{181, 190}},
		{query: "no match", ids: nil},
	} {
		t.Run(tc.query, func(t *testing.T) {
			after := int64(0)
			for i, wantID := range tc.ids {
				page, err := s.ListSummaries(store.ListOptions{Search: tc.query, Status: "open", Project: "sample", After: after, Limit: 1})
				if err != nil {
					t.Fatal(err)
				}
				if page.Revision != 1 || len(page.Items) != 1 || page.Items[0].ID != wantID || page.NextAfter != wantID || page.HasMore != (i+1 < len(tc.ids)) {
					t.Fatalf("page %d = %+v, want id=%d hasMore=%v", i, page, wantID, i+1 < len(tc.ids))
				}
				after = page.NextAfter
			}
			last, err := s.ListSummaries(store.ListOptions{Search: tc.query, Status: "open", Project: "sample", After: after, Limit: 1})
			if err != nil || len(last.Items) != 0 || last.HasMore || last.NextAfter != after {
				t.Fatalf("last page = %+v, %v", last, err)
			}
		})
	}
}

func TestChangesPaginationNoGapsAndIncludesEffectiveMeta(t *testing.T) {
	s, _, _ := openStore(t)
	empty, err := s.Changes(0, 1)
	if err != nil || len(empty.Changes) != 0 || empty.NextAfter != 0 || empty.HasMore {
		t.Fatalf("initial=%+v, %v", empty, err)
	}
	for i := 0; i < 7; i++ {
		req := store.UpdateRequest{RequestID: fmt.Sprintf("page-%d", i), BaseRevision: uint64(i), Creates: []json.RawMessage{raw(`{"title":"fixture"}`)}}
		if i == 3 {
			req.Meta = raw(`{"next_id":40,"unknown":null}`)
		}
		if _, err := s.Update(req); err != nil {
			t.Fatal(err)
		}
	}
	after := uint64(0)
	var seen []uint64
	for pageNumber := 0; pageNumber < 10; pageNumber++ {
		page, err := s.Changes(after, 2)
		if err != nil {
			t.Fatal(err)
		}
		if page.Revision != 7 || len(page.Changes) == 0 || len(page.Changes) > 2 {
			t.Fatalf("page=%+v", page)
		}
		for _, change := range page.Changes {
			seen = append(seen, change.Revision)
			var meta map[string]json.RawMessage
			if err := json.Unmarshal(change.Meta, &meta); err != nil {
				t.Fatal(err)
			}
			if meta["next_id"] == nil {
				t.Fatal("allocation metadata omitted from change")
			}
		}
		if page.NextAfter != page.Changes[len(page.Changes)-1].Revision {
			t.Fatal("cursor is not last returned revision")
		}
		after = page.NextAfter
		if !page.HasMore {
			break
		}
	}
	if !reflect.DeepEqual(seen, []uint64{1, 2, 3, 4, 5, 6, 7}) {
		t.Fatalf("revisions=%v", seen)
	}
	last, err := s.Changes(after, 2)
	if err != nil || len(last.Changes) != 0 || last.NextAfter != 7 || last.HasMore {
		t.Fatalf("last=%+v, %v", last, err)
	}
	_, err = s.Changes(math.MaxUint64, 1)
	requireError(t, err, store.ErrConflict)
	_, err = s.Changes(0, -1)
	requireError(t, err, store.ErrInvalid)
}

func TestAssetDigestExactBytesAndImmutableMetadata(t *testing.T) {
	s, _, _ := openStore(t)
	data := []byte("\xef\xbb\xbfline\r\n\x00\xff")
	asset := store.Asset{SHA256: hash(data), OriginalPath: `fixture\spec\MixedCase.txt`, Kind: "Spec", Bytes: data}
	wrong := asset
	wrong.Bytes = []byte("different")
	_, err := s.PutAsset(wrong)
	requireError(t, err, store.ErrInvalid)
	_, err = s.Asset(asset.SHA256)
	requireError(t, err, store.ErrNotFound)
	meta, err := s.PutAsset(asset)
	if err != nil || meta.Bytes != int64(len(data)) || meta.OriginalPath != asset.OriginalPath || meta.Kind != asset.Kind {
		t.Fatalf("metadata=%+v, %v", meta, err)
	}
	if _, err := s.PutAsset(asset); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*store.Asset){func(a *store.Asset) { a.Kind = "spec" }, func(a *store.Asset) { a.Bytes = []byte("different bytes"); a.SHA256 = hash(a.Bytes) }} {
		copy := asset
		change(&copy)
		_, err := s.PutAsset(copy)
		requireError(t, err, store.ErrConflict)
	}
	got, err := s.Asset(asset.SHA256)
	if err != nil || !reflect.DeepEqual(got, asset) {
		t.Fatalf("asset changed: %v", err)
	}
	got.Bytes[0] = 0
	again, err := s.Asset(asset.SHA256)
	if err != nil || !bytes.Equal(again.Bytes, data) {
		t.Fatal("returned asset aliases database memory")
	}
	if snapshot(t, s).Revision != 0 {
		t.Fatal("asset write changed ledger revision")
	}
}

func TestReturnedItemAndSnapshotAreDetached(t *testing.T) {
	s, _, _ := openStore(t)
	if _, err := s.Update(store.UpdateRequest{RequestID: "seed", Upserts: []json.RawMessage{raw(`{"id":1}`)}}); err != nil {
		t.Fatal(err)
	}
	item, err := s.Item(1)
	if err != nil {
		t.Fatal(err)
	}
	item.Item[0] = '!'
	state := snapshot(t, s)
	state.Document[0] = '!'
	again, err := s.Item(1)
	if err != nil {
		t.Fatal(err)
	}
	requireJSON(t, again.Item, raw(`{"id":1}`))
	if !json.Valid(snapshot(t, s).Document) {
		t.Fatal("snapshot aliases database memory")
	}
}
