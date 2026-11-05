package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const fixture = `{"next_id":3,"projects":{"demo":"C:/fixture"},"unknown":{"value":9007199254740993,"small":1.2300e-7,"null":null},"items":[{"id":1,"lastJob":"UPPER","lastjob":null,"spec":"C:/fixture/original.md","generation":3,"receipt":{"accepted":true},"unknown":0.10000000000000001},{"id":2,"status":"todo"}]}`

type fakeLedger struct {
	mu         sync.Mutex
	doc        *document
	rev        uint64
	stateID    string
	lease      Lease
	history    []Change
	operations map[string]struct {
		body    []byte
		receipt Receipt
	}
	stateReads, changeReads, releases, commits int
	versionReads, acquires                     int
	acquireLockedCount                         int
	acquireBodies                              [][]byte
	commitStatus                               int
	changeFailAt                               int
	dropReply                                  bool
	dropAllReplies                             bool
	operationStatus                            int
	renewed                                    chan struct{}
}

func newLedger(t *testing.T) (*fakeLedger, *Client) {
	t.Helper()
	doc, err := parseDocument([]byte(fixture))
	if err != nil {
		t.Fatal(err)
	}
	l := &fakeLedger{doc: doc, rev: 1, stateID: "fixture-state-1", operations: make(map[string]struct {
		body    []byte
		receipt Receipt
	})}
	server := httptest.NewServer(l)
	t.Cleanup(server.Close)
	client, err := NewClient(server.URL, "fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	return l, client
}
func (l *fakeLedger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer fixture-token" {
		w.WriteHeader(401)
		return
	}
	out := func(v any) { w.Header().Set("Content-Type", "application/json"); _ = json.NewEncoder(w).Encode(v) }
	switch r.URL.Path {
	case "/api/v1/version":
		l.versionReads++
		out(Version{Revision: l.rev, StateID: l.stateID})
	case "/api/v1/lease/acquire":
		l.acquires++
		request, _ := io.ReadAll(r.Body)
		l.acquireBodies = append(l.acquireBodies, request)
		if l.acquires <= l.acquireLockedCount {
			w.WriteHeader(423)
			return
		}
		if l.lease.ID != "" && l.lease.ExpiresAt.After(time.Now()) {
			w.WriteHeader(423)
			return
		}
		l.lease = Lease{ID: fmt.Sprintf("fixture-lease-%d", l.acquires), Revision: l.rev, StateID: l.stateID, TTLMillis: 90000, ExpiresAt: time.Now().Add(90 * time.Second)}
		out(l.lease)
	case "/api/v1/lease/renew":
		if l.renewed != nil {
			select {
			case l.renewed <- struct{}{}:
			default:
			}
		}
		if !l.lease.ExpiresAt.After(time.Now()) {
			w.WriteHeader(409)
			return
		}
		l.lease.Revision = l.rev
		l.lease.StateID = l.stateID
		l.lease.ExpiresAt = time.Now().Add(90 * time.Second)
		out(l.lease)
	case "/api/v1/lease/release":
		l.releases++
		l.lease.ID = ""
		out(map[string]bool{"ok": true})
	case "/api/v1/state":
		l.stateReads++
		b, _ := l.doc.bytes()
		out(Snapshot{Revision: l.rev, StateID: l.stateID, Document: b})
	case "/api/v1/changes":
		l.changeReads++
		if l.changeFailAt == l.changeReads {
			w.WriteHeader(503)
			return
		}
		var after uint64
		_, _ = fmt.Sscan(r.URL.Query().Get("after"), &after)
		page := Changes{Revision: l.rev, StateID: l.stateID, NextAfter: after}
		for _, change := range l.history {
			if change.Revision > after {
				page.Changes = append(page.Changes, change)
				page.NextAfter = change.Revision
				break
			}
		}
		page.HasMore = page.NextAfter < l.rev
		out(page)
	case "/api/v1/commit":
		b, _ := io.ReadAll(r.Body)
		var commit Commit
		if json.Unmarshal(b, &commit) != nil {
			w.WriteHeader(400)
			return
		}
		if saved, ok := l.operations[commit.RequestID]; ok {
			if !bytes.Equal(saved.body, b) {
				w.WriteHeader(409)
				return
			}
			saved.receipt.Duplicate = true
			if l.dropAllReplies {
				conn, _, e := w.(http.Hijacker).Hijack()
				if e == nil {
					_ = conn.Close()
				}
				return
			}
			out(saved.receipt)
			return
		}
		if l.commitStatus != 0 {
			w.WriteHeader(l.commitStatus)
			return
		}
		if commit.BaseRevision != l.rev || (commit.BaseStateID != "" && commit.BaseStateID != l.stateID) || commit.LeaseID != l.lease.ID || !l.lease.ExpiresAt.After(time.Now()) {
			w.WriteHeader(409)
			return
		}
		change := Change{Revision: l.rev + 1, ParentStateID: l.stateID, StateID: fmt.Sprintf("fixture-state-%d", l.rev+1), Upserts: commit.Upserts, DeleteIDs: commit.DeleteIDs, Meta: commit.Meta}
		if l.doc.apply(change) != nil {
			w.WriteHeader(400)
			return
		}
		l.commits++
		l.rev++
		l.stateID = change.StateID
		l.history = append(l.history, change)
		receipt := Receipt{Revision: l.rev, StateID: l.stateID, Upserts: len(commit.Upserts), Deleted: len(commit.DeleteIDs)}
		l.operations[commit.RequestID] = struct {
			body    []byte
			receipt Receipt
		}{append([]byte(nil), b...), receipt}
		if l.dropReply || l.dropAllReplies {
			l.dropReply = false
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
			return
		}
		out(receipt)
	default:
		if strings.HasPrefix(r.URL.Path, "/api/v1/operations/") {
			if l.operationStatus != 0 {
				w.WriteHeader(l.operationStatus)
				return
			}
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/operations/")
			if saved, ok := l.operations[id]; ok {
				out(saved.receipt)
				return
			}
		}
		w.WriteHeader(404)
	}
}
func cachePath(t *testing.T) string { t.Helper(); return filepath.Join(t.TempDir(), "todo.json") }
func begin(t *testing.T, c *Client, path string) *Session {
	t.Helper()
	s, err := Begin(context.Background(), c, "fixture-pc", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func changed(t *testing.T, b []byte) []byte {
	t.Helper()
	d, err := parseDocument(b)
	if err != nil {
		t.Fatal(err)
	}
	d.items[2] = json.RawMessage(`{"id":2,"status":"doing","new":null}`)
	out, err := d.bytes()
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestSnapshotDeltaCASPreservesCompleteSchemaAndNumberSpelling(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	before := read(t, path)
	receipt, err := s.Commit(context.Background(), changed(t, before))
	if err != nil || receipt.Revision != 2 {
		t.Fatalf("commit: %v %+v", err, receipt)
	}
	b := read(t, path)
	for _, fragment := range []string{`9007199254740993`, `1.2300e-7`, `0.10000000000000001`, `"lastJob":"UPPER"`, `"lastjob":null`, `"spec":"C:/fixture/original.md"`, `"generation":3`, `"receipt":{"accepted":true}`, `"next_id":3`} {
		if !bytes.Contains(b, []byte(fragment)) {
			t.Fatalf("lost schema fragment %s", fragment)
		}
	}
	if bytes.Contains(b, []byte(`"missing"`)) {
		t.Fatal("inserted absent field")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	l.rev++
	change := Change{Revision: l.rev, ParentStateID: l.stateID, StateID: "fixture-state-3", Upserts: []json.RawMessage{json.RawMessage(`{"id":2,"status":"done"}`)}}
	l.stateID = change.StateID
	l.history = append(l.history, change)
	_ = l.doc.apply(change)
	l.mu.Unlock()
	s = begin(t, c, path)
	if s.Revision() != 3 || !bytes.Contains(read(t, path), []byte(`"status":"done"`)) {
		t.Fatal("delta was not applied")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stateReads != 1 || l.changeReads != 2 || l.commits != 1 || len(l.history[0].Upserts) != 1 || len(l.history[0].Meta) != 0 {
		t.Fatal("unexpected full read or nonminimal commit")
	}
}

func TestIdempotentResendAndDifferentContentConflict(t *testing.T) {
	l, c := newLedger(t)
	s := begin(t, c, cachePath(t))
	commit := Commit{RequestID: "fixture-operation", LeaseID: s.lease.ID, BaseRevision: 1, Upserts: []json.RawMessage{json.RawMessage(`{"id":2,"status":"doing"}`)}, DeleteIDs: []int64{}}
	a, err := c.commit(context.Background(), commit)
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.commit(context.Background(), commit)
	if err != nil || b.Revision != a.Revision || !b.Duplicate {
		t.Fatalf("resend: %v %+v", err, b)
	}
	commit.Upserts = []json.RawMessage{json.RawMessage(`{"id":2,"status":"done"}`)}
	if _, err = c.commit(context.Background(), commit); err == nil {
		t.Fatal("different contents reused request ID")
	}
	if l.commits != 1 {
		t.Fatal("duplicated transaction")
	}
}
func TestLostCommitReplyResolvesOperationBeforeSavingCache(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	l.mu.Lock()
	l.dropReply = true
	l.mu.Unlock()
	if _, err := s.Commit(context.Background(), changed(t, read(t, path))); err != nil {
		t.Fatal(err)
	}
	d, rev, err := loadCache(path)
	if err != nil || rev != 2 || !bytes.Contains(d.items[2], []byte(`doing`)) {
		t.Fatal("unconfirmed cache")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.commits != 1 {
		t.Fatal("replayed mutation")
	}
}
func TestConflictAndNetworkFailureKeepBothCacheFilesUnchanged(t *testing.T) {
	for _, status := range []int{409, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			l, c := newLedger(t)
			path := cachePath(t)
			s := begin(t, c, path)
			original, revision := read(t, path), read(t, path+".remote-revision")
			l.mu.Lock()
			l.commitStatus = status
			l.mu.Unlock()
			if _, err := s.Commit(context.Background(), changed(t, original)); err == nil {
				t.Fatal("failed commit accepted")
			}
			if !bytes.Equal(read(t, path), original) || !bytes.Equal(read(t, path+".remote-revision"), revision) {
				t.Fatal("cache changed after failed commit")
			}
		})
	}
}
func TestFailureHalfwayThroughPagedDeltaDoesNotChangeCache(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	_ = s.Close()
	original, revision := read(t, path), read(t, path+".remote-revision")
	l.mu.Lock()
	l.rev = 3
	l.stateID = "fixture-state-3"
	l.history = []Change{{Revision: 2, ParentStateID: "fixture-state-1", StateID: "fixture-state-2", DeleteIDs: []int64{2}}, {Revision: 3, ParentStateID: "fixture-state-2", StateID: "fixture-state-3", Meta: json.RawMessage(`{"next_id":3}`)}}
	l.changeFailAt = 2
	l.mu.Unlock()
	if _, err := Begin(context.Background(), c, "fixture-pc", path); err == nil {
		t.Fatal("partial pull accepted")
	}
	if !bytes.Equal(read(t, path), original) || !bytes.Equal(read(t, path+".remote-revision"), revision) {
		t.Fatal("partial delta reached cache")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lease.ID != "" || l.releases != 2 {
		t.Fatal("failed begin leaked lease")
	}
}
func TestExpiredLeaseCannotCommit(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	b := read(t, path)
	s.mu.Lock()
	s.lease.deadline = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if _, err := s.Commit(context.Background(), changed(t, b)); err == nil {
		t.Fatal("expired lease saved")
	}
	if !bytes.Equal(read(t, path), b) {
		t.Fatal("expired lease changed cache")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.commits != 0 {
		t.Fatal("expired lease sent mutation")
	}
}
func TestAtomicReplacementReadersNeverSeePartialJSON(t *testing.T) {
	path := cachePath(t)
	if err := atomicWrite(path, []byte(fixture)); err != nil {
		t.Fatal(err)
	}
	stop := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		for {
			select {
			case <-stop:
				result <- nil
				return
			default:
			}
			b, err := readFileLimit(path, MaxDocumentBytes)
			if err != nil {
				result <- err
				return
			}
			if !json.Valid(b) {
				result <- fmt.Errorf("partial JSON")
				return
			}
		}
	}()
	for i := 0; i < 30; i++ {
		if err := atomicWrite(path, []byte(fixture)); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	// Failure to replace a directory cannot truncate the existing target.
	target := filepath.Join(t.TempDir(), "directory")
	_ = os.Mkdir(target, 0700)
	if atomicWrite(target, []byte(fixture)) == nil {
		t.Fatal("replacement should fail")
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Fatal("failed atomic save changed target")
	}
}
func TestDigestMismatchUsesFreshSnapshotNotStaleRevision(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	_ = s.Close()
	if err := os.WriteFile(path, changed(t, read(t, path)), 0600); err != nil {
		t.Fatal(err)
	}
	s = begin(t, c, path)
	if s.Revision() != 1 || !bytes.Contains(read(t, path), []byte(`"status":"todo"`)) {
		t.Fatal("unversioned edit was trusted")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stateReads != 2 {
		t.Fatal("did not repair crash boundary")
	}
}
func TestServerLeaseFailureAndEOFRelease(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	cfg := Config{BaseURL: c.base, SyncToken: "fixture-token", ClientID: "fixture-pc", CachePath: path}
	configPath := filepath.Join(filepath.Dir(path), "fixture.dpapi")
	if err := SaveConfig(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(filepath.Dir(path), "todo.remote.json"), wire(map[string]string{"bridge": filepath.Join(filepath.Dir(path), "fixture.exe"), "credentials": configPath})); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := Serve(context.Background(), cfg, strings.NewReader(string(wire(bridgeRequest{Op: "begin", CachePath: path}))+"\n"), &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"ok":true`) || strings.Contains(out.String(), cfg.SyncToken) {
		t.Fatal("unsafe control reply")
	}
	l.mu.Lock()
	released := l.releases
	l.mu.Unlock()
	if released != 1 {
		t.Fatal("EOF failed to release")
	}
	s := begin(t, c, path)
	original := read(t, path)
	l.mu.Lock()
	l.lease.ExpiresAt = time.Now().Add(-time.Second)
	l.mu.Unlock()
	if _, err = s.Commit(context.Background(), changed(t, original)); err == nil {
		t.Fatal("server-expired lease accepted")
	}
	if !bytes.Equal(read(t, path), original) {
		t.Fatal("failed renew changed cache")
	}
}
func TestConfigProtectedAtRestAndSecretsNeverReachErrors(t *testing.T) {
	_, c := newLedger(t)
	cfg := Config{BaseURL: c.base, SyncToken: "fixture-token", ClientID: "fixture-pc", CachePath: cachePath(t)}
	path := filepath.Join(t.TempDir(), "credentials")
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	got, err := LoadConfig(path)
	if err != nil || got != cfg {
		t.Fatal("protected configuration did not round trip")
	}
	if err := os.WriteFile(path, []byte("invalid fixture secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(path); err == nil || strings.Contains(err.Error(), "fixture secret") {
		t.Fatal("invalid configuration leaked")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNetworkDisconnectPreservesCacheAndDoesNotLeakTransportErrors(t *testing.T) {
	_, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	original, revision := read(t, path), read(t, path+".remote-revision")
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("fixture-secret-network-error") })
	_, err := s.Commit(context.Background(), changed(t, original))
	if err == nil || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatal("unsafe network error")
	}
	if !bytes.Equal(read(t, path), original) || !bytes.Equal(read(t, path+".remote-revision"), revision) {
		t.Fatal("network error changed cache")
	}
}

func TestLeaseRenewsWhileDomainClosureRunsAndCloseStopsRenewal(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	renewed := make(chan struct{}, 8)
	l.renewed = renewed
	s, err := beginSession(context.Background(), c, "fixture-pc", path, 5*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	for i := 0; i < 2; i++ {
		select {
		case <-renewed:
		case <-time.After(3 * time.Second):
			t.Fatal("lease not renewed during closure")
		}
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.lease.ID != "" {
		t.Fatal("close leaked lease")
	}
}
