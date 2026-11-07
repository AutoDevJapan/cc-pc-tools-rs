package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"autodevjapan/shared-todo-cloud/internal/store"
)

func TestHTTPStoreWireAndCanonicalNextID(t *testing.T) {
	ledger, err := store.Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	// A test-only adapter isolates transport/store wire compatibility from the
	// independently developed browser/admin HTTP handlers.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+strings.Repeat("s", 40) {
			w.WriteHeader(401)
			return
		}
		var value any
		var callErr error
		switch r.URL.Path {
		case "/api/v1/state":
			value, callErr = ledger.Snapshot()
		case "/api/v1/lease/acquire":
			var q struct {
				ClientID  string `json:"clientId"`
				RequestID string `json:"requestId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&q)
			value, callErr = ledger.Acquire(q.ClientID, q.RequestID)
		case "/api/v1/lease/renew", "/api/v1/lease/release":
			var q struct {
				LeaseID string `json:"leaseId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&q)
			if strings.HasSuffix(r.URL.Path, "renew") {
				value, callErr = ledger.Renew(q.LeaseID)
			} else {
				callErr = ledger.Release(q.LeaseID)
				value = map[string]bool{"ok": callErr == nil}
			}
		case "/api/v1/changes":
			after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			value, callErr = ledger.Changes(after, limit)
		case "/api/v1/commit":
			var q store.CommitRequest
			_ = json.NewDecoder(r.Body).Decode(&q)
			value, callErr = ledger.Commit(q)
		default:
			w.WriteHeader(404)
			return
		}
		if callErr != nil {
			w.WriteHeader(409)
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	}))
	t.Cleanup(server.Close)
	c, err := NewClient(server.URL, strings.Repeat("s", 40))
	if err != nil {
		t.Fatal(err)
	}
	path := cachePath(t)
	s := begin(t, c, path)
	// Cloud enforces the high-water mark even when an old domain path submits
	// lower metadata. The cache must use the accepted change, not that candidate.
	next := []byte(`{"next_id":1,"unknown":{"float":1.2500,"absent":null},"items":[{"id":17,"lastJob":"A","lastjob":null,"spec":"C:/fixture/specs/17.md"}]}`)
	result, err := s.Commit(context.Background(), next)
	if err != nil {
		t.Fatal(err)
	}
	if result.Revision != 1 {
		t.Fatal("wrong revision")
	}
	var cache map[string]json.RawMessage
	if json.Unmarshal(read(t, path), &cache) != nil || string(cache["next_id"]) != "18" {
		t.Fatal("canonical high-water mark was not applied")
	}
	snap, err := ledger.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var canonical map[string]json.RawMessage
	if json.Unmarshal(snap.Document, &canonical) != nil {
		t.Fatal("invalid snapshot")
	}
	if !rawEqual(wire(cache), wire(canonical)) {
		t.Fatal("server and cache differ")
	}
	if !bytes.Contains(read(t, path), []byte(`"float":1.2500`)) {
		t.Fatal("numeric spelling lost")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	// A normal web update is visible through changes, without another snapshot.
	_, err = ledger.Update(store.UpdateRequest{RequestID: "fixture-web-edit", BaseRevision: 1, Upserts: []json.RawMessage{json.RawMessage(`{"id":17,"lastJob":"B","lastjob":null}`)}})
	if err != nil {
		t.Fatal(err)
	}
	s = begin(t, c, path)
	if s.Revision() != 2 || !bytes.Contains(read(t, path), []byte(`"lastJob":"B"`)) {
		t.Fatal("web delta not applied")
	}
}
