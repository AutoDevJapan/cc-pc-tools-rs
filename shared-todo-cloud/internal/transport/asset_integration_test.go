package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"autodevjapan/shared-todo-cloud/internal/assetio"
	"autodevjapan/shared-todo-cloud/internal/httpapi"
	"autodevjapan/shared-todo-cloud/internal/store"
)

func assetFixture(t *testing.T, wrap func(*store.Store, http.Handler) http.Handler) (*store.Store, *Client, *Session, string) {
	t.Helper()
	ledger, err := store.Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	_, err = ledger.Update(store.UpdateRequest{RequestID: "fixture-seed", Upserts: []json.RawMessage{json.RawMessage(`{"id":1,"lastJob":null,"lastjob":false,"spec":"specs/missing-legacy.md","unknown":9007199254740993}`)}})
	if err != nil {
		t.Fatal(err)
	}
	api, err := httpapi.New(ledger, httpapi.Config{AdminToken: strings.Repeat("a", 40), SyncToken: strings.Repeat("s", 40)})
	if err != nil {
		t.Fatal(err)
	}
	var handler http.Handler = api
	if wrap != nil {
		handler = wrap(ledger, api)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c, err := NewClient(server.URL, strings.Repeat("s", 40))
	if err != nil {
		t.Fatal(err)
	}
	path := cachePath(t)
	s := begin(t, c, path)
	return ledger, c, s, filepath.Dir(path)
}
func newAssetCandidate(t *testing.T, s *Session, home string) ([]byte, string, []byte, string, []byte) {
	t.Helper()
	specAlias := "specs/new #+漢字.md"
	spec := []byte{0xef, 0xbb, 0xbf, 'a', '\r', '\n', 0xff}
	if os.MkdirAll(filepath.Join(home, "specs"), 0700) != nil || os.WriteFile(filepath.Join(home, filepath.FromSlash(specAlias)), spec, 0600) != nil {
		t.Fatal("fixture spec")
	}
	archive := []byte(`{"domain":"` + assetio.HistoryDomain + `","todo_id":1,"entries":[{"note":"fictional history","unknown":null}],"Unknown":9007199254740993}`)
	file := "1-" + digest(archive) + ".json"
	archivePath := filepath.Join(home, "note-history-v1", file)
	if os.MkdirAll(filepath.Dir(archivePath), 0700) != nil || os.WriteFile(archivePath, archive, 0600) != nil {
		t.Fatal("fixture archive")
	}
	d, err := parseDocument(s.canonical)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]json.RawMessage
	_ = json.Unmarshal(d.items[1], &row)
	row["spec"] = wire(specAlias)
	row["spec_history"] = json.RawMessage(`["specs/missing-legacy.md"]`)
	row["note_history_archives"] = wire([]any{map[string]any{"domain": assetio.HistoryDomain, "todo_id": 1, "file": file, "sha256": digest(archive), "bytes": len(archive), "entries": 1}})
	d.items[1] = wire(row)
	candidate, err := d.bytes()
	if err != nil {
		t.Fatal(err)
	}
	return candidate, specAlias, spec, archivePath, archive
}
func TestNewSpecAndArchiveAreUploadedBeforeRootReferenceCommit(t *testing.T) {
	var specAlias, archivePath string
	var spec, archive []byte
	checked := false
	ledger, _, s, home := assetFixture(t, func(ledger *store.Store, next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/commit" {
				for path, raw := range map[string][]byte{specAlias: spec, archivePath: archive} {
					asset, err := ledger.ResolveAsset(path)
					if err != nil || asset.Bytes != int64(len(raw)) || asset.SHA256 != digest(raw) {
						t.Error("root POST preceded complete asset aliases")
					}
				}
				checked = true
			}
			next.ServeHTTP(w, r)
		})
	})
	candidate, a, b, c, d := newAssetCandidate(t, s, home)
	specAlias, spec, archivePath, archive = a, b, c, d
	if _, err := s.Commit(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	if !checked {
		t.Fatal("root commit was not observed")
	}
	snap, err := ledger.Snapshot()
	if err != nil || snap.Revision != 2 || !bytes.Contains(snap.Document, []byte(`"lastJob":null`)) || !bytes.Contains(snap.Document, []byte(`"lastjob":false`)) {
		t.Fatal("asset hook changed raw root schema")
	}
	for path, raw := range map[string][]byte{specAlias: spec, filepath.Join(home, filepath.FromSlash(specAlias)): spec, archivePath: archive} {
		asset, err := ledger.ResolveAsset(path)
		if err != nil || asset.Bytes != int64(len(raw)) || asset.OriginalPath != path {
			t.Fatal("asset digest/path/bytes did not survive HTTP upload")
		}
		blob, err := ledger.Asset(asset.SHA256)
		if err != nil || !bytes.Equal(blob.Bytes, raw) {
			t.Fatal("uploaded raw bytes changed")
		}
	}
}

func TestAssetMetadataMismatchStopsRootAndRetryIsIdempotent(t *testing.T) {
	corrupt := true
	posts := 0
	ledger, _, s, home := assetFixture(t, func(_ *store.Store, next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/commit" {
				posts++
			}
			if r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/api/v1/assets/") && corrupt {
				recorder := httptest.NewRecorder()
				next.ServeHTTP(recorder, r)
				var metadata map[string]json.RawMessage
				_ = json.Unmarshal(recorder.Body.Bytes(), &metadata)
				metadata["bytes"] = json.RawMessage("999999")
				w.WriteHeader(recorder.Code)
				_, _ = w.Write(wire(metadata))
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	original := read(t, filepath.Join(home, "todo.json"))
	candidate, _, _, _, _ := newAssetCandidate(t, s, home)
	if _, err := s.Commit(context.Background(), candidate); err == nil {
		t.Fatal("bad asset metadata allowed root commit")
	}
	if posts != 0 || !bytes.Equal(read(t, filepath.Join(home, "todo.json")), original) {
		t.Fatal("asset failure changed root or cache")
	}
	corrupt = false
	if _, err := s.Commit(context.Background(), candidate); err != nil {
		t.Fatal("identical asset retry failed:", err)
	}
	rev, _, err := ledger.Version()
	if err != nil || rev != 2 || posts != 1 {
		t.Fatal("asset retry duplicated root commit")
	}
}

func TestNewLease404ResolutionRetiresLateOldRequestBeforeNewMutation(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	lateStatus := make(chan int, 1)
	var claimed atomic.Bool
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer unblock()
	ledger, c, s, home := assetFixture(t, func(_ *store.Store, next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/v1/commit" && claimed.CompareAndSwap(false, true) {
				body, _ := io.ReadAll(r.Body)
				close(started)
				<-release
				late := r.Clone(context.Background())
				late.Body = io.NopCloser(bytes.NewReader(body))
				recorder := httptest.NewRecorder()
				next.ServeHTTP(recorder, late)
				lateStatus <- recorder.Code
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	d, err := parseDocument(s.canonical)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]json.RawMessage
	_ = json.Unmarshal(d.items[1], &row)
	row["progress"] = json.RawMessage("42")
	d.items[1] = wire(row)
	candidate, _ := d.bytes()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := s.Commit(ctx, candidate); result <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("late request fixture never entered server")
	}
	cancel()
	err = <-result
	var unknown *CommitUnconfirmed
	if !errors.As(err, &unknown) {
		t.Fatal("late in-flight request was not retained as unconfirmed")
	}
	_ = s.Close()
	next := begin(t, c, filepath.Join(home, "todo.json"))
	unblock()
	select {
	case status := <-lateStatus:
		if status != 423 {
			t.Fatal("old request committed after new lease/404")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("late request did not finish")
	}
	if _, err = next.Commit(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	rev, _, err := ledger.Version()
	if err != nil || rev != 2 {
		t.Fatal("old and new mutations both applied")
	}
}
