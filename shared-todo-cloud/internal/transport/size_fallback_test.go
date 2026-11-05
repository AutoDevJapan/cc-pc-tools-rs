package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// Generate actual oversized JSON history without allocating a giant fixture
// string. Unknown Content-Length still exercises the bounded streaming reader.
type fixtureBytes struct{ remaining int64 }

func (r *fixtureBytes) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	for i := 0; i < n; i++ {
		p[i] = 'x'
	}
	r.remaining -= int64(n)
	return n, nil
}

func oversizedHistory(first, count int, itemBytes int64, final uint64, id string) (io.Reader, int64) {
	var readers []io.Reader
	var length int64
	text := func(b []byte) { readers = append(readers, bytes.NewReader(b)); length += int64(len(b)) }
	text([]byte(fmt.Sprintf(`{"revision":%d,"stateId":%q,"changes":[`, final, id)))
	for i := 0; i < count; i++ {
		rev := first + i
		if i != 0 {
			text([]byte(","))
		}
		text([]byte(fmt.Sprintf(`{"revision":%d,"stateId":"fixture-state-%d","parentStateId":"fixture-state-%d","upserts":[{"id":2,"padding":"`, rev, rev, rev-1)))
		readers = append(readers, &fixtureBytes{remaining: itemBytes})
		length += itemBytes
		text([]byte(`"}],"deleteIds":[],"meta":{"next_id":3}}`))
	}
	text([]byte(fmt.Sprintf(`],"nextAfter":%d,"hasMore":false}`, final)))
	return io.MultiReader(readers...), length
}

func response(body io.Reader, length int64) *http.Response {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(body), ContentLength: length}
}

func TestLargeChangesUseSamePinnedLeaseSnapshotAndKeepCacheOnFailure(t *testing.T) {
	for _, tc := range []struct {
		name                                                              string
		single, chunked, partial, failSnapshot, wrongState, wrongRevision bool
	}{
		{name: "eighty-megabyte-history"},
		{name: "single-record-chunked", single: true, chunked: true},
		{name: "partial-delta-snapshot-failure", partial: true, failSnapshot: true},
		{name: "snapshot-state-mismatch", wrongState: true},
		{name: "snapshot-revision-mismatch", wrongRevision: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l, c := newLedger(t)
			path := cachePath(t)
			initial := begin(t, c, path)
			_ = initial.Close()
			original, metadata := read(t, path), read(t, path+".remote-revision")
			final := uint64(81)
			if tc.single {
				final = 3
			}
			if tc.partial {
				final = 82
			}
			finalID := fmt.Sprintf("fixture-state-%d", final)
			l.mu.Lock()
			l.rev, l.stateID = final, finalID
			l.doc.items[2] = json.RawMessage(`{"id":2,"status":"done","snapshotOnly":null}`)
			l.mu.Unlock()
			changeReads, stateReads := 0, 0
			var changeLease, snapshotLease string
			c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				switch r.URL.Path {
				case "/api/v1/changes":
					changeReads++
					l.mu.Lock()
					changeLease = l.lease.ID
					l.mu.Unlock()
					if tc.partial && changeReads == 1 {
						return response(bytes.NewReader(wire(Changes{Revision: final, StateID: finalID, NextAfter: 2, HasMore: true, Changes: []Change{{Revision: 2, StateID: "fixture-state-2", ParentStateID: "fixture-state-1", Upserts: []json.RawMessage{json.RawMessage(`{"id":2,"status":"staged-only"}`)}}}})), -1), nil
					}
					count, first, padding := 80, 2, int64(1<<20)
					if tc.single {
						count = 1
						padding = maxWireBytes + 1
					}
					if tc.partial {
						first = 3
					}
					body, length := oversizedHistory(first, count, padding, final, finalID)
					if length <= maxWireBytes {
						t.Error("history fixture is not oversized")
					}
					if tc.chunked {
						length = -1
					}
					return response(body, length), nil
				case "/api/v1/state":
					stateReads++
					l.mu.Lock()
					snapshotLease = l.lease.ID
					l.mu.Unlock()
					if tc.failSnapshot {
						return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil))}, nil
					}
					if tc.wrongState || tc.wrongRevision {
						id, rev := finalID, final
						if tc.wrongState {
							id = "unrelated-snapshot-state"
						}
						if tc.wrongRevision {
							rev++
						}
						return response(bytes.NewReader(wire(Snapshot{Revision: rev, StateID: id, Document: json.RawMessage(fixture)})), -1), nil
					}
				}
				return http.DefaultTransport.RoundTrip(r)
			})
			s, err := Begin(context.Background(), c, "fixture-pc", path)
			if tc.failSnapshot || tc.wrongState || tc.wrongRevision {
				if err == nil {
					_ = s.Close()
					t.Fatal("failed/unpinned snapshot was accepted")
				}
				if !bytes.Equal(original, read(t, path)) || !bytes.Equal(metadata, read(t, path+".remote-revision")) {
					t.Fatal("failed snapshot installed staged deltas or changed cache metadata")
				}
			} else {
				if err != nil {
					t.Fatal("oversized changes did not recover through snapshot:", err)
				}
				if s.Revision() != final || s.StateID() != finalID || !bytes.Contains(read(t, path), []byte(`"snapshotOnly":null`)) {
					t.Fatal("pinned snapshot was not installed")
				}
				_ = s.Close()
			}
			if stateReads != 1 || changeReads != 1 && !(tc.partial && changeReads == 2) || changeLease == "" || snapshotLease != changeLease {
				t.Fatal("fallback did not use one snapshot under the original lease")
			}
			if !bytes.Equal(original, read(t, path+".foreign-"+digest(original))) {
				t.Fatal("valid original branch was not preserved")
			}
			l.mu.Lock()
			acquires := l.acquires
			l.mu.Unlock()
			if acquires != 2 {
				t.Fatal("fallback acquired another lease")
			}
		})
	}
}

func TestOversizedChunkedResponseHasSpecificSizeError(t *testing.T) {
	_, c := newLedger(t)
	c.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return response(&fixtureBytes{remaining: 65}, -1), nil })
	var out any
	err := c.callLimit(context.Background(), "GET", "/fixture", nil, &out, 64)
	if !errors.Is(err, errResponseTooLarge) || err.Error() != "todo_remote_response_too_large" {
		t.Fatal("wire reader lost its size-error classification")
	}
}

func TestConfirmedOversizedChangeSnapshotPreservesH1JournalSemantics(t *testing.T) {
	for _, failSnapshot := range []bool{false, true} {
		t.Run(fmt.Sprintf("snapshot-failure-%t", failSnapshot), func(t *testing.T) {
			l, c := newLedger(t)
			path := cachePath(t)
			s := begin(t, c, path)
			original, metadata := read(t, path), read(t, path+".remote-revision")
			c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/api/v1/changes" {
					body, length := oversizedHistory(2, 1, maxWireBytes+1, 2, "fixture-state-2")
					return response(body, length), nil
				}
				if r.URL.Path == "/api/v1/state" && failSnapshot {
					return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil))}, nil
				}
				return http.DefaultTransport.RoundTrip(r)
			})
			receipt, err := s.Commit(context.Background(), changed(t, original))
			if err != nil || receipt.Revision != 2 || receipt.StateID != "fixture-state-2" {
				t.Fatal("confirmed domain commit was turned into a retryable failure")
			}
			pending, err := readPending(path)
			if err != nil {
				t.Fatal(err)
			}
			if failSnapshot {
				if pending == nil || pending.Confirmed == nil || pending.Confirmed.StateID != receipt.StateID || !needsFresh(path) {
					t.Fatal("failed snapshot lost durable confirmed journal")
				}
				if !bytes.Equal(original, read(t, path)) || !bytes.Equal(metadata, read(t, path+".remote-revision")) {
					t.Fatal("failed committed snapshot overwrote old cache")
				}
				_ = s.Close()
				c.http.Transport = nil
				s = begin(t, c, path)
				if s.Revision() != 2 || s.StateID() != receipt.StateID || needsFresh(path) {
					t.Fatal("next begin did not repair confirmed cache")
				}
			} else if pending != nil || needsFresh(path) || s.StateID() != receipt.StateID || !bytes.Contains(read(t, path), []byte(`"status":"doing"`)) {
				t.Fatal("successful committed snapshot did not finish cache/journal refresh")
			}
			l.mu.Lock()
			commits := l.commits
			l.mu.Unlock()
			if commits != 1 {
				t.Fatal("cache refresh or recovery replayed the domain edit")
			}
		})
	}
}

func TestRollbackSnapshotPreservesValidBranchAndAbortsIfBackupFails(t *testing.T) {
	for _, backupConflict := range []bool{false, true} {
		t.Run(fmt.Sprintf("backup-conflict-%t", backupConflict), func(t *testing.T) {
			l, c := newLedger(t)
			l.mu.Lock()
			l.rev = 3
			l.stateID = "branch-a-3"
			l.doc.items[2] = json.RawMessage(`{"id":2,"ownerData":"fixture","transferredResults":[null,false]}`)
			l.mu.Unlock()
			path := cachePath(t)
			s := begin(t, c, path)
			_ = s.Close()
			original, metadata := read(t, path), read(t, path+".remote-revision")
			target := path + ".foreign-" + digest(original)
			if backupConflict && os.WriteFile(target, []byte("conflicting fixture backup"), 0600) != nil {
				t.Fatal("backup failure fixture failed")
			}
			l.mu.Lock()
			l.rev = 1
			l.stateID = "restored-1"
			delete(l.doc.items, 2)
			l.doc.order = []int64{1}
			l.mu.Unlock()
			s, err := Begin(context.Background(), c, "fixture-pc", path)
			if backupConflict {
				if err == nil {
					_ = s.Close()
					t.Fatal("backup failure did not abort snapshot replacement")
				}
				if !bytes.Equal(original, read(t, path)) || !bytes.Equal(metadata, read(t, path+".remote-revision")) {
					t.Fatal("backup failure changed cache or version metadata")
				}
				l.mu.Lock()
				stateReads := l.stateReads
				l.mu.Unlock()
				if stateReads != 1 {
					t.Fatal("snapshot was fetched before preserving the valid cache")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if s.Revision() != 1 || s.StateID() != "restored-1" || !bytes.Equal(original, read(t, target)) || bytes.Contains(read(t, path), []byte(`"ownerData"`)) {
					t.Fatal("rollback discarded the only valid newer branch")
				}
				_ = s.Close()
			}
		})
	}
}

func TestSameVersionAndRegularChangesDoNotCopyValidCache(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	_ = s.Close()
	s = begin(t, c, path)
	_ = s.Close()
	l.mu.Lock()
	change := Change{Revision: 2, StateID: "fixture-state-2", ParentStateID: l.stateID, Upserts: []json.RawMessage{json.RawMessage(`{"id":2,"status":"done"}`)}}
	_ = l.doc.apply(change)
	l.rev, l.stateID = 2, change.StateID
	l.history = append(l.history, change)
	l.mu.Unlock()
	s = begin(t, c, path)
	_ = s.Close()
	backups, err := filepath.Glob(path + ".foreign-*")
	if err != nil || len(backups) != 0 {
		t.Fatal("ordinary cache reuse or regular delta sync copied the cache")
	}
}
