package transport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestConfirmedCommitCachePullFailureIsSuccessAndNextBeginRepairs(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	before := read(t, path)
	l.mu.Lock()
	l.changeFailAt = 1
	l.mu.Unlock()
	receipt, err := s.Commit(context.Background(), changed(t, before))
	if err != nil || receipt.Revision != 2 {
		t.Fatal("confirmed commit was exposed as a failed domain write")
	}
	if !bytes.Equal(read(t, path), before) || !needsFresh(path) {
		t.Fatal("failed cache pull installed candidate or lost invalidation")
	}
	p, err := readPending(path)
	if err != nil || p == nil || p.Confirmed == nil || p.Confirmed.Revision != 2 {
		t.Fatal("confirmed receipt not durable")
	}
	_ = s.Close()
	s = begin(t, c, path)
	if s.Revision() != 2 || needsFresh(path) || !bytes.Contains(read(t, path), []byte(`"status":"doing"`)) {
		t.Fatal("next begin did not reconcile and fresh-pull")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.commits != 1 || l.stateReads != 2 {
		t.Fatal("repair replayed domain mutation or trusted stale cache")
	}
}

func TestConfirmedCommitCacheSaveFailureIsSuccessAndPreservesTornBytes(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	oldMeta := read(t, path+".remote-revision")
	if os.Remove(path+".remote-revision") != nil || os.Mkdir(path+".remote-revision", 0700) != nil {
		t.Fatal("fixture failure setup")
	}
	receipt, err := s.Commit(context.Background(), changed(t, read(t, path)))
	if err != nil || receipt.Revision != 2 || !needsFresh(path) {
		t.Fatal("confirmed cache save failure became retryable domain failure")
	}
	torn := read(t, path)
	_ = s.Close()
	if os.Remove(path+".remote-revision") != nil || os.WriteFile(path+".remote-revision", oldMeta, 0600) != nil {
		t.Fatal("fixture repair setup")
	}
	_ = begin(t, c, path)
	if !bytes.Equal(read(t, path+".foreign-"+digest(torn)), torn) {
		t.Fatal("torn cache bytes were overwritten without preservation")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.commits != 1 {
		t.Fatal("cache repair duplicated confirmed operation")
	}
}

func TestPendingIsFlushedBeforeSendAndNotAppliedOutcomeIsArchived(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	original := read(t, path)
	candidate := changed(t, original)
	var sent [][]byte
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/commit" {
			p, err := readPending(path)
			if err != nil || p == nil {
				t.Error("POST happened before private journal")
			}
			body, _ := io.ReadAll(r.Body)
			sent = append(sent, body)
			if p != nil && (p.CandidateSHA256 != digest(candidate) || p.BaseRevision != 1 || p.BaseStateID != "fixture-state-1" || !bytes.Equal(p.Body, body)) {
				t.Error("journal does not bind exact sent request")
			}
			return nil, errors.New("fixture connection lost before server")
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	_, err := s.Commit(context.Background(), candidate)
	var unknown *CommitUnconfirmed
	if !errors.As(err, &unknown) || len(sent) != 3 || !bytes.Equal(sent[0], sent[1]) || !bytes.Equal(sent[1], sent[2]) {
		t.Fatal("unconfirmed send lost type or changed request body")
	}
	p, err := readPending(path)
	if err != nil || p == nil {
		t.Fatal("unconfirmed journal missing")
	}
	private := read(t, pendingPath(path))
	if runtime.GOOS == "windows" && bytes.Contains(private, []byte(`"requestId"`)) {
		t.Fatal("pending body is not owner protected")
	}
	if bytes.Contains(read(t, path+".remote-events.jsonl"), []byte("fixture-token")) {
		t.Fatal("event leaked credentials")
	}
	if !bytes.Equal(read(t, path), original) {
		t.Fatal("unconfirmed send wrote cache")
	}
	_ = s.Close()
	c.http.Transport = nil
	s = begin(t, c, path)
	if s.Revision() != 1 || needsFresh(path) || !bytes.Equal(read(t, path+".remote-not-applied-"+p.RequestID), private) {
		t.Fatal("new lease did not resolve 404 as non-application while preserving private recovery body")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.commits != 0 || l.acquires != 2 {
		t.Fatal("pending resolution applied a domain update")
	}
}

func TestPendingAppliedOutcomeResolvesBeforeAnotherMutation(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	l.mu.Lock()
	l.dropAllReplies = true
	l.operationStatus = 503
	l.mu.Unlock()
	_, err := s.Commit(context.Background(), changed(t, read(t, path)))
	var unknown *CommitUnconfirmed
	if !errors.As(err, &unknown) {
		t.Fatal("lost applied result was not distinguished")
	}
	_ = s.Close()
	l.mu.Lock()
	l.dropAllReplies = false
	l.operationStatus = 0
	l.mu.Unlock()
	s = begin(t, c, path)
	if s.Revision() != 2 || needsFresh(path) || !bytes.Contains(read(t, path), []byte(`"status":"doing"`)) {
		t.Fatal("applied result was not resolved before new domain work")
	}
	if _, err = s.Commit(context.Background(), changed(t, read(t, path))); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.commits != 1 {
		t.Fatal("recovery reapplied the old domain operation")
	}
}

func TestPendingNamespaceMismatchBlocksBeforeNetwork(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	l.mu.Lock()
	l.commitStatus = 503
	l.mu.Unlock()
	_, _ = s.Commit(context.Background(), changed(t, read(t, path)))
	_ = s.Close()
	other, err := NewClient("http://127.0.0.1:1", "different-fixture-token")
	if err != nil {
		t.Fatal(err)
	}
	called := false
	other.http.Transport = roundTripFunc(func(*http.Request) (*http.Response, error) { called = true; return nil, errors.New("must not call") })
	_, err = Begin(context.Background(), other, "fixture-pc", path)
	var unknown *CommitUnconfirmed
	if !errors.As(err, &unknown) || called {
		t.Fatal("pending request was used against another upstream")
	}
}

func TestForeignAndBrokenCacheBytesArePreservedBeforeFreshPull(t *testing.T) {
	for _, modified := range [][]byte{[]byte(`{"items":[{"id":9,"note":"foreign fixture"}],"next_id":10}`), []byte("broken crash bytes\x00\r\n")} {
		t.Run(digest(modified)[:8], func(t *testing.T) {
			_, c := newLedger(t)
			path := cachePath(t)
			s := begin(t, c, path)
			_ = s.Close()
			if os.WriteFile(path, modified, 0600) != nil {
				t.Fatal("fixture write")
			}
			_ = begin(t, c, path)
			if !bytes.Equal(read(t, path+".foreign-"+digest(modified)), modified) {
				t.Fatal("unverified bytes were silently replaced")
			}
			events := read(t, path+".remote-events.jsonl")
			if !bytes.Contains(events, []byte("cache_unverified_bytes_preserved")) || bytes.Contains(events, []byte("foreign fixture")) {
				t.Fatal("preservation event lost or included task body")
			}
		})
	}
}

func TestForeignBackupConflictFailsWithoutReplacingOriginal(t *testing.T) {
	_, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	_ = s.Close()
	modified := []byte("unique foreign bytes")
	if os.WriteFile(path, modified, 0600) != nil || os.WriteFile(path+".foreign-"+digest(modified), []byte("conflicting backup"), 0600) != nil {
		t.Fatal("fixture write")
	}
	if _, err := Begin(context.Background(), c, "fixture-pc", path); err == nil {
		t.Fatal("conflicting preservation target accepted")
	}
	if !bytes.Equal(read(t, path), modified) {
		t.Fatal("unpreserved bytes were overwritten")
	}
}

func TestOversizedUnverifiedCacheIsStreamPreservedBeforeReplacement(t *testing.T) {
	_, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	_ = s.Close()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(MaxDocumentBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	hashFile := func(path string) string {
		t.Helper()
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		h := sha256.New()
		if _, err = io.Copy(h, f); err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(h.Sum(nil))
	}
	sha := hashFile(path)
	_ = begin(t, c, path)
	backup := path + ".foreign-" + sha
	info, err := os.Stat(backup)
	if err != nil || info.Size() != MaxDocumentBytes+1 || hashFile(backup) != sha {
		t.Fatal("reader ceiling discarded oversized foreign bytes")
	}
}

func TestConfirmedOutcomeWinsWhenSharedCommitDeadlineExpiresDuringPull(t *testing.T) {
	_, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	var deadline time.Time
	c.http.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/changes" {
			deadline, _ = r.Context().Deadline()
			<-r.Context().Done()
			return nil, r.Context().Err()
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	receipt, err := s.Commit(ctx, changed(t, read(t, path)))
	if err != nil || receipt.Revision != 2 || deadline.IsZero() || deadline.After(started.Add(60*time.Second)) {
		t.Fatal("confirmed result was hidden by a later timeout or operation exceeded shared budget")
	}
	if !needsFresh(path) {
		t.Fatal("timeout did not leave durable resync state")
	}
}

func TestPendingResolutionFailureNeverPermitsAnotherDomainWrite(t *testing.T) {
	l, c := newLedger(t)
	path := cachePath(t)
	s := begin(t, c, path)
	l.mu.Lock()
	l.dropAllReplies = true
	l.operationStatus = 503
	l.mu.Unlock()
	_, _ = s.Commit(context.Background(), changed(t, read(t, path)))
	_ = s.Close()
	l.mu.Lock()
	l.dropAllReplies = false
	l.mu.Unlock()
	_, err := Begin(context.Background(), c, "fixture-pc", path)
	var unknown *CommitUnconfirmed
	if !errors.As(err, &unknown) {
		t.Fatal("unresolved operation became ordinary retryable failure")
	}
	if _, err = readPending(path); err != nil {
		t.Fatal("resolution failure lost pending journal")
	}
	if _, err = os.Stat(filepath.Join(filepath.Dir(path), "todo.json")); err != nil {
		t.Fatal("cache lost")
	}
}
