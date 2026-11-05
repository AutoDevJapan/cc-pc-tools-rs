package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func activateFixture(t *testing.T, c *Client, path string) Config {
	t.Helper()
	cfg := Config{BaseURL: c.base, SyncToken: "fixture-token", ClientID: "fixture-pc", CachePath: path}
	credentials := filepath.Join(filepath.Dir(path), "fixture.dpapi")
	if err := SaveConfig(credentials, cfg); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(filepath.Dir(path), "todo.remote.json"), wire(map[string]string{"bridge": filepath.Join(filepath.Dir(path), "fixture.exe"), "credentials": credentials})); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestUnchangedWatchPollDoesNotAcquireLeaseOrOpenDocument(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	_ = s.Close()
	cfg := activateFixture(t, c, path)
	f, err := os.OpenFile(filepath.Join(filepath.Dir(path), "todo.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err = lockFile(f); err != nil {
		t.Fatal(err)
	}
	defer unlockFile(f)
	allowRead := denyFixtureDocumentRead(t, path)
	defer allowRead()
	before := read(t, path+".remote-revision")
	for i := 0; i < 3; i++ {
		if err = syncIfChanged(context.Background(), cfg, c); err != nil {
			t.Fatal("unchanged poll touched locked document or writer lock:", err)
		}
	}
	if !bytes.Equal(before, read(t, path+".remote-revision")) {
		t.Fatal("unchanged poll rewrote metadata")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.versionReads != 3 || l.acquires != 1 || l.stateReads != 1 || l.changeReads != 0 {
		t.Fatal("unchanged watcher did more than cheap version requests")
	}
}

func TestBeginReusesSameStateRawBytesWithoutParsingOrSerializing(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	id := s.StateID()
	_ = s.Close()
	raw := append([]byte{0xef, 0xbb, 0xbf}, []byte(" \r\n"+fixture+"\r\n ")...)
	if err := saveCache(path, raw, 1, id); err != nil {
		t.Fatal(err)
	}
	before := read(t, path+".remote-revision")
	s = begin(t, c, path)
	if s.base != nil || !bytes.Equal(s.canonical, raw) || !bytes.Equal(read(t, path), raw) || !bytes.Equal(read(t, path+".remote-revision"), before) {
		t.Fatal("same-state begin parsed, reformatted or rewrote cache")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stateReads != 1 || l.changeReads != 0 {
		t.Fatal("same state fetched document again")
	}
}

func TestChangedVersionPollSynchronizesOnlyWhenNeeded(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	_ = s.Close()
	cfg := activateFixture(t, c, path)
	l.mu.Lock()
	change := Change{Revision: 2, ParentStateID: l.stateID, StateID: "fixture-state-2", Upserts: []json.RawMessage{json.RawMessage(`{"id":2,"status":"done","unknown":null}`)}}
	_ = l.doc.apply(change)
	l.history = append(l.history, change)
	l.rev = 2
	l.stateID = change.StateID
	l.mu.Unlock()
	if err := syncIfChanged(context.Background(), cfg, c); err != nil {
		t.Fatal(err)
	}
	if err := syncIfChanged(context.Background(), cfg, c); err != nil {
		t.Fatal(err)
	}
	state, err := readCacheState(path)
	if err != nil || state.Revision != 2 || state.StateID != change.StateID {
		t.Fatal("changed version not installed")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.versionReads != 2 || l.acquires != 2 || l.changeReads != 1 || l.stateReads != 1 {
		t.Fatal("changed poll did not use one delta sync")
	}
}

func TestSameRevisionDifferentStateFetchesFreshSnapshot(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	oldID := s.StateID()
	_ = s.Close()
	l.mu.Lock()
	delete(l.doc.items, 2)
	l.doc.order = []int64{1}
	l.doc.items[1] = json.RawMessage(`{"id":1,"branch":"replacement","lastJob":null}`)
	l.stateID = "other-ledger-at-one"
	l.mu.Unlock()
	s = begin(t, c, path)
	if s.Revision() != 1 || s.StateID() == oldID || bytes.Contains(read(t, path), []byte(`"id":2`)) || !bytes.Contains(read(t, path), []byte(`"branch":"replacement"`)) {
		t.Fatal("equal revision mixed unrelated state")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stateReads != 2 || l.changeReads != 0 {
		t.Fatal("same revision mismatch did not refresh snapshot")
	}
}

func TestHigherRevisionForkCannotApplyUnrelatedParentChanges(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	_ = s.Close()
	l.mu.Lock()
	l.doc.items = map[int64]json.RawMessage{8: json.RawMessage(`{"id":8,"branch":"fork"}`)}
	l.doc.order = []int64{8}
	l.doc.meta = map[string]json.RawMessage{"next_id": json.RawMessage("9")}
	l.rev = 3
	l.stateID = "branch-b-3"
	l.history = []Change{{Revision: 2, ParentStateID: "branch-b-1", StateID: "branch-b-2", Upserts: []json.RawMessage{json.RawMessage(`{"id":8,"branch":"fork"}`)}}, {Revision: 3, ParentStateID: "branch-b-2", StateID: "branch-b-3"}}
	l.mu.Unlock()
	s = begin(t, c, path)
	if s.Revision() != 3 || s.StateID() != "branch-b-3" || bytes.Contains(read(t, path), []byte(`"id":1`)) || !bytes.Contains(read(t, path), []byte(`"id":8`)) {
		t.Fatal("fork changes were overlaid on prior cache")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.changeReads != 1 || l.stateReads != 2 {
		t.Fatal("parent mismatch did not discard staged deltas")
	}
}

func TestRollbackToLowerRevisionUsesAuthoritativeSnapshot(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	l.mu.Lock()
	l.rev = 4
	l.stateID = "branch-a-4"
	l.mu.Unlock()
	s := begin(t, c, path)
	_ = s.Close()
	l.mu.Lock()
	l.rev = 1
	l.stateID = "restored-1"
	delete(l.doc.items, 2)
	l.doc.order = []int64{1}
	l.mu.Unlock()
	s = begin(t, c, path)
	if s.Revision() != 1 || s.StateID() != "restored-1" || bytes.Contains(read(t, path), []byte(`"id":2`)) {
		t.Fatal("rollback retained future cache")
	}
}

func TestLegacyCacheWithoutStateIdentityIsNeverTrustedAsSameVersion(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	if err := saveCache(path, []byte(`{"next_id":10,"items":[{"id":9,"unrelated":true}]}`), 1); err != nil {
		t.Fatal(err)
	}
	s := begin(t, c, path)
	state, err := readCacheState(path)
	if err != nil || state.StateID != s.StateID() || state.StateID == "" || bytes.Contains(read(t, path), []byte(`"unrelated"`)) {
		t.Fatal("old sidecar was upgraded without fresh data")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stateReads != 1 || l.changeReads != 0 {
		t.Fatal("old cache used unverified diffs")
	}
}

func TestStateMismatchThenSnapshotNetworkFailurePreservesOriginal(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	_ = s.Close()
	original, metadata := read(t, path), read(t, path+".remote-revision")
	l.mu.Lock()
	l.stateID = "replacement-state"
	l.mu.Unlock()
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/state" {
			return nil, errLineage
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	if _, err := Begin(context.Background(), c, "fixture-pc", path); err == nil {
		t.Fatal("network failure accepted")
	}
	if !bytes.Equal(read(t, path), original) || !bytes.Equal(read(t, path+".remote-revision"), metadata) {
		t.Fatal("failed identity refresh replaced cache")
	}
}

func TestLeaseServerWallClockSkewDoesNotSetLocalDeadline(t *testing.T) {
	for _, skew := range []time.Duration{-48 * time.Hour, 48 * time.Hour} {
		t.Run(skew.String(), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "acquire") || strings.HasSuffix(r.URL.Path, "renew") {
					_ = json.NewEncoder(w).Encode(Lease{ID: "fixture-lease", Revision: 1, StateID: "fixture-state", TTLMillis: 90000, ExpiresAt: time.Now().Add(skew)})
					return
				}
				w.WriteHeader(404)
			}))
			defer server.Close()
			c, err := NewClient(server.URL, "fixture-token")
			if err != nil {
				t.Fatal(err)
			}
			lease, err := c.acquire(context.Background(), "fixture-pc")
			if err != nil {
				t.Fatal("server/PC clock skew rejected lease:", err)
			}
			renewed, err := c.renew(context.Background(), lease.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range []Lease{lease, renewed} {
				remaining := time.Until(value.deadline)
				if remaining < 80*time.Second || remaining > 90*time.Second {
					t.Fatal("deadline came from server UTC rather than local elapsed time")
				}
			}
		})
	}
}

func TestLeaseDeadlineAccountsForRoundTripAndLegacyTTL(t *testing.T) {
	lease := Lease{ID: "fixture", TTLMillis: 90000, ExpiresAt: time.Now().Add(48 * time.Hour)}
	if err := lease.startDeadline(time.Now().Add(-20 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if remaining := time.Until(lease.deadline); remaining > 71*time.Second || remaining < 69*time.Second {
		t.Fatal("network transit extended lease")
	}
	lease.TTLMillis = 0
	if err := lease.startDeadline(time.Now()); err != nil {
		t.Fatal("v1 fixed lease TTL lost compatibility")
	}
	for _, ttl := range []int64{-1, 90001} {
		lease.TTLMillis = ttl
		if lease.startDeadline(time.Now()) == nil {
			t.Fatal("invalid lease TTL accepted")
		}
	}
}

func TestLockedLeaseRetriesSameRequestWithinCallerDeadline(t *testing.T) {
	l, c := newLedger(t)
	l.acquireLockedCount = 1
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	lease, err := c.acquire(ctx, "fixture-pc")
	if err != nil || lease.ID == "" {
		t.Fatal("short old lease did not recover")
	}
	l.mu.Lock()
	if len(l.acquireBodies) != 2 || !bytes.Equal(l.acquireBodies[0], l.acquireBodies[1]) {
		t.Fatal("lease retry changed request ID")
	}
	l.mu.Unlock()
	_ = c.release(context.Background(), lease.ID)
	l.mu.Lock()
	l.acquireLockedCount = 1000
	l.mu.Unlock()
	ctx, cancel = context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	if _, err = c.acquire(ctx, "fixture-pc"); err == nil {
		t.Fatal("locked lease ignored caller deadline")
	}
	if time.Since(started) > time.Second {
		t.Fatal("bounded lease retry exceeded caller budget")
	}
}
