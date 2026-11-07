package httpapi

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"autodevjapan/shared-todo-cloud/internal/store"
)

const testAdmin = "owner-admin-test-012345678901234567890123456789"
const testSync = "machine-sync-test-012345678901234567890123456789"
const testOrigin = "https://tasks.example.test"

func testServer(t *testing.T) *Server {
	t.Helper()
	ledger, err := store.Open(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	var rows []json.RawMessage
	for i := 1; i <= 201; i++ {
		title := fmt.Sprintf("task %03d", i)
		if i == 201 {
			title = `<script>alert("secret task")</script> needle`
		}
		rows = append(rows, raw(map[string]any{"id": i, "title": title, "status": "todo", "project": "Example", "note": "private detail", "lastJob": "A", "lastjob": "B", "future": map[string]any{"null": nil, "value": true}}))
	}
	document := raw(map[string]any{"items": rows, "next_id": 202, "projects": map[string]any{"Example": map[string]any{"path": "fictional"}}, "recovery": map[string]any{"preserve": true}})
	if err = ledger.ImportChunk("fixture", 0, document); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(document)
	if _, err = ledger.Finalize("fixture", store.FinalizeRequest{SHA256: hex.EncodeToString(digest[:]), ChunkCount: 1, Bytes: int64(len(document))}); err != nil {
		t.Fatal(err)
	}
	s, err := New(ledger, Config{AdminToken: testAdmin, SyncToken: testSync, PublicURL: testOrigin, SecureCookies: true})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func perform(s *Server, method, path, body, token string, cookie *http.Cookie, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, testOrigin+path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}
func authenticated(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	w := perform(s, "POST", "/api/v1/session", "", testAdmin, nil, "")
	if w.Code != 200 {
		t.Fatalf("session: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count %d", len(cookies))
	}
	c := cookies[0]
	if !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode {
		t.Fatal("session cookie protection missing")
	}
	return c
}
func TestAnonymousCannotReadTaskData(t *testing.T) {
	s := testServer(t)
	for _, path := range []string{"/api/v1/state", "/api/v1/items", "/api/v1/items/201", "/api/v1/changes", "/api/v1/assets/resolve?path=private"} {
		w := perform(s, "GET", path, "", "", nil, "")
		if w.Code != 401 || strings.Contains(w.Body.String(), "secret task") || strings.Contains(w.Body.String(), "private detail") {
			t.Fatalf("unauthorized response on %s", path)
		}
	}
	w := perform(s, "GET", "/?task=201", "", "", nil, "")
	if w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Fatal("dashboard requires session")
	}
	if perform(s, "POST", "/api/v1/session", "", testSync, nil, "").Code != 401 {
		t.Fatal("sync credential granted owner browser session")
	}
}
func TestGlobalSearchAndHTMLAreSafe(t *testing.T) {
	s := testServer(t)
	cookie := authenticated(t, s)
	w := perform(s, "GET", "/?q=needle&task=201", "", "", cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "201") || !strings.Contains(w.Body.String(), "needle") {
		t.Fatalf("late search result: %d", w.Code)
	}
	if strings.Contains(w.Body.String(), `<script>`) || !strings.Contains(w.Body.String(), "&lt;script&gt;") {
		t.Fatal("task text was not escaped")
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'none'") {
		t.Fatal("missing CSP")
	}
	w = perform(s, "GET", "/api/v1/items?q=needle", "", testSync, nil, "")
	var result store.SummaryPage
	if json.Unmarshal(w.Body.Bytes(), &result) != nil || len(result.Items) != 1 || result.Items[0].ID != 201 {
		t.Fatal("API search must precede pagination")
	}
}
func TestMutationRequiresCSRFAndCurrentRevision(t *testing.T) {
	s := testServer(t)
	cookie := authenticated(t, s)
	snap, _ := s.Store.Snapshot()
	form := url.Values{"csrf": {s.csrf(cookie.Value)}, "request_id": {"web-edit"}, "revision": {fmt.Sprint(snap.Revision)}, "id": {"1"}, "title": {"changed"}, "note": {"new note"}, "status": {"todo"}}
	for _, bad := range []struct{ origin, csrf string }{{"https://elsewhere.example", form.Get("csrf")}, {testOrigin, "wrong"}, {"", form.Get("csrf")}} {
		copy := url.Values{}
		for k, v := range form {
			copy[k] = append([]string(nil), v...)
		}
		copy.Set("csrf", bad.csrf)
		if w := perform(s, "POST", "/tasks", copy.Encode(), "", cookie, bad.origin); w.Code != 403 {
			t.Fatalf("invalid CSRF accepted: %d", w.Code)
		}
	}
	w := perform(s, "POST", "/tasks", form.Encode(), "", cookie, testOrigin)
	if w.Code != 303 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	item, _ := s.Store.Item(1)
	row := object(item.Item)
	if text(row["title"]) != "changed" || text(row["lastJob"]) != "A" || text(row["lastjob"]) != "B" || !bytes.Contains(row["future"], []byte("true")) {
		t.Fatal("change or preserved fields differ")
	}
	form.Set("request_id", "stale-edit")
	form.Set("title", "lost update")
	if perform(s, "POST", "/tasks", form.Encode(), "", cookie, testOrigin).Code != 409 {
		t.Fatal("stale form overwrote newer change")
	}
}
func TestLeasePreventsWebWriteAndDuplicateCommitIsStable(t *testing.T) {
	s := testServer(t)
	cookie := authenticated(t, s)
	snap, _ := s.Store.Snapshot()
	lease, err := s.Store.Acquire("pc", "pc-acquire")
	if err != nil {
		t.Fatal(err)
	}
	form := url.Values{"csrf": {s.csrf(cookie.Value)}, "request_id": {"web-create"}, "revision": {fmt.Sprint(snap.Revision)}, "project": {"Example"}, "title": {"new task"}}
	if perform(s, "POST", "/tasks", form.Encode(), "", cookie, testOrigin).Code != 423 {
		t.Fatal("web write ignored active PC lease")
	}
	item, _ := s.Store.Item(1)
	changed := object(item.Item)
	changed["note"] = raw("PC update")
	q := store.CommitRequest{RequestID: "pc-commit", LeaseID: lease.ID, BaseRevision: snap.Revision, Upserts: []json.RawMessage{raw(changed)}}
	encoded, _ := json.Marshal(q)
	w := perform(s, "POST", "/api/v1/commit", string(encoded), testSync, nil, "")
	if w.Code != 200 {
		t.Fatalf("commit %d %s", w.Code, w.Body.String())
	}
	w = perform(s, "POST", "/api/v1/commit", string(encoded), testSync, nil, "")
	var receipt store.CommitResult
	if json.Unmarshal(w.Body.Bytes(), &receipt) != nil || !receipt.Duplicate || receipt.Revision != snap.Revision+1 {
		t.Fatal("ambiguous retry duplicated commit")
	}
}
func TestSessionTamperExpiryAndOrigin(t *testing.T) {
	s := testServer(t)
	cookie := authenticated(t, s)
	for _, value := range []string{cookie.Value + "x", "not-a-session"} {
		c := *cookie
		c.Value = value
		if perform(s, "GET", "/", "", "", &c, "").Code != 303 {
			t.Fatal("tampered session accepted")
		}
	}
	s.now = func() time.Time { return time.Now().Add(8 * 24 * time.Hour) }
	if perform(s, "GET", "/", "", "", cookie, "").Code != 303 {
		t.Fatal("expired session accepted")
	}
	if perform(s, "POST", "/login", url.Values{"token": {testAdmin}}.Encode(), "", nil, "https://elsewhere.example").Code != 401 {
		t.Fatal("cross-origin login accepted")
	}
}
func TestSnapshotCompressionAndAssetAliases(t *testing.T) {
	s := testServer(t)
	r := httptest.NewRequest("GET", testOrigin+"/api/v1/state", nil)
	r.Header.Set("Authorization", "Bearer "+testSync)
	r.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 200 || w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("snapshot compression unavailable")
	}
	gz, err := gzip.NewReader(w.Body)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	b, err := io.ReadAll(gz)
	if err != nil {
		t.Fatal(err)
	}
	var snap store.Snapshot
	if json.Unmarshal(b, &snap) != nil || !bytes.Contains(snap.Document, []byte("private detail")) {
		t.Fatal("compressed snapshot is incomplete")
	}
	asset := []byte("original note history")
	digest := sha256.Sum256(asset)
	hash := hex.EncodeToString(digest[:])
	for _, path := range []string{"original-a.json", "original-b.json"} {
		if perform(s, "PUT", "/api/v1/assets/"+hash+"?path="+path+"&kind=history", string(asset), testSync, nil, "").Code != 200 {
			t.Fatal("asset upload")
		}
		response := perform(s, "GET", "/api/v1/assets/resolve?path="+path, "", testSync, nil, "")
		var meta store.AssetMetadata
		if json.Unmarshal(response.Body.Bytes(), &meta) != nil || meta.SHA256 != hash || meta.OriginalPath != path {
			t.Fatal("asset path alias lost")
		}
	}
	response := perform(s, "GET", "/api/v1/assets/"+hash, "", testSync, nil, "")
	if !bytes.Equal(response.Body.Bytes(), asset) {
		t.Fatal("asset bytes changed")
	}
}
func TestHealthFailsAfterStorageClosed(t *testing.T) {
	s := testServer(t)
	_ = s.Store.Close()
	if perform(s, "GET", "/healthz", "", "", nil, "").Code != 503 {
		t.Fatal("unhealthy storage reported healthy")
	}
}

func TestAllocationsStaySeparateAndDepartmentPaginationIsComplete(t *testing.T) {
	s := testServer(t)
	cookie := authenticated(t, s)
	before, _ := s.Store.Snapshot()
	var assignments []map[string]any
	for i := 1; i <= 201; i++ {
		department := "情報システム部"
		if i == 201 {
			department = "ゲーム部"
		}
		assignments = append(assignments, map[string]any{"taskId": i, "department": department, "displayName": fmt.Sprintf("担当%d", i), "state": "assigned"})
	}
	put := func(previous string) *httptest.ResponseRecorder {
		return perform(s, "PUT", "/api/v1/allocations", string(raw(map[string]any{"document": map[string]any{"assignments": assignments}, "previousSHA256": previous})), testSync, nil, "")
	}
	if w := put(""); w.Code != 200 {
		t.Fatalf("allocation registry %d %s", w.Code, w.Body.String())
	}
	w := perform(s, "GET", "/?department="+url.QueryEscape("ゲーム部"), "", "", cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "担当201") || strings.Contains(w.Body.String(), "担当001") {
		t.Fatal("department filtering lost a task after the first page")
	}
	registry, _ := s.Store.Registry("task-allocations")
	assignments[0]["state"] = "queued"
	if put("").Code != 409 {
		t.Fatal("stale registry update accepted")
	}
	if put(registry.SHA256).Code != 200 {
		t.Fatal("current registry update rejected")
	}
	after, _ := s.Store.Snapshot()
	if before.Revision != after.Revision || !bytes.Equal(before.Document, after.Document) {
		t.Fatal("auxiliary registry changed the original task document")
	}
}

func TestHistoryIncludesArchivesAndSpecsKeepOriginalBytes(t *testing.T) {
	s := testServer(t)
	cookie := authenticated(t, s)
	archive := raw(map[string]any{"domain": "cc-todo-note-history-v1", "todo_id": 1, "entries": []any{map[string]any{"updated": "2025-01-01", "note": "archived original <script>"}}})
	hash := sha256.Sum256(archive)
	digest := hex.EncodeToString(hash[:])
	if _, err := s.Store.PutAsset(store.Asset{SHA256: digest, OriginalPath: "original-history.json", Kind: "note-history", Bytes: archive}); err != nil {
		t.Fatal(err)
	}
	spec := []byte{0xef, 0xbb, 0xbf, 'a', '\r', '\n', 'b'}
	hash = sha256.Sum256(spec)
	specHash := hex.EncodeToString(hash[:])
	if _, err := s.Store.PutAsset(store.Asset{SHA256: specHash, OriginalPath: "original-spec.txt", Kind: "spec", Bytes: spec}); err != nil {
		t.Fatal(err)
	}
	item, _ := s.Store.Item(1)
	m := object(item.Item)
	m["note_history_archives"] = raw([]any{map[string]any{"sha256": digest, "todo_id": 1}})
	m["note_history"] = raw([]any{map[string]any{"updated": "2026-01-01", "note": "inline original"}})
	m["spec"] = raw("original-spec.txt")
	if _, err := s.Store.Update(store.UpdateRequest{RequestID: "history-fixture", BaseRevision: item.Revision, Upserts: []json.RawMessage{raw(m)}}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/tasks/1/history", "/tasks/1/spec"} {
		if perform(s, "GET", path, "", "", nil, "").Code != 303 {
			t.Fatal("unauthenticated original asset was exposed")
		}
	}
	w := perform(s, "GET", "/tasks/1/history", "", "", cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "archived original &lt;script&gt;") || !strings.Contains(w.Body.String(), "inline original") || !strings.Contains(w.Body.String(), "2025-01-01") {
		t.Fatal("full history omitted archived notes or dates")
	}
	w = perform(s, "GET", "/tasks/1/spec", "", "", cookie, "")
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), spec) {
		t.Fatal("specification bytes changed")
	}
}

func TestUnchangedProgressIsNotAnExplicitResumeProgress(t *testing.T) {
	s := testServer(t)
	cookie := authenticated(t, s)
	item, _ := s.Store.Item(1)
	m := object(item.Item)
	m["status"] = raw("done")
	m["progress"] = raw(100)
	if _, err := s.Store.Update(store.UpdateRequest{RequestID: "done-fixture", BaseRevision: item.Revision, Upserts: []json.RawMessage{raw(m)}}); err != nil {
		t.Fatal(err)
	}
	item, _ = s.Store.Item(1)
	form := url.Values{"csrf": {s.csrf(cookie.Value)}, "request_id": {"resume-fixture"}, "revision": {fmt.Sprint(item.Revision)}, "id": {"1"}, "title": {"task 001"}, "status": {"doing"}, "note": {"private detail"}, "progress": {"100"}}
	w := perform(s, "POST", "/tasks", form.Encode(), "", cookie, testOrigin)
	if w.Code != 303 {
		t.Fatalf("resume form %d %s", w.Code, w.Body.String())
	}
	item, _ = s.Store.Item(1)
	m = object(item.Item)
	if text(m["status"]) != "doing" || len(m["progress"]) != 0 {
		t.Fatal("untouched completed progress persisted into a fresh start")
	}
}

func TestUnrelatedUpdateDoesNotDiscardFormAndConflictsPreserveDraft(t *testing.T) {
	s := testServer(t)
	cookie := authenticated(t, s)
	item, _ := s.Store.Item(1)
	form := url.Values{"csrf": {s.csrf(cookie.Value)}, "request_id": {"merge-web"}, "revision": {fmt.Sprint(item.Revision)}, "id": {"1"}, "item_fingerprint": {itemFingerprint(item.Item)}, "title": {"my changed title"}, "note": {"my typed draft"}, "status": {"todo"}}
	other, _ := s.Store.Item(2)
	row := object(other.Item)
	row["title"] = raw("unrelated update")
	if _, err := s.Store.Update(store.UpdateRequest{RequestID: "other", BaseRevision: other.Revision, Upserts: []json.RawMessage{raw(row)}}); err != nil {
		t.Fatal(err)
	}
	w := perform(s, "POST", "/tasks", form.Encode(), "", cookie, testOrigin)
	if w.Code != 303 {
		t.Fatalf("unrelated update blocked save: %d %s", w.Code, w.Body.String())
	}
	form.Set("request_id", "conflicting-web")
	w = perform(s, "POST", "/tasks", form.Encode(), "", cookie, testOrigin)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "my typed draft") {
		t.Fatal("conflicting update discarded typed draft")
	}
	form.Set("request_id", "merge-web")
	w = perform(s, "POST", "/tasks", form.Encode(), "", cookie, testOrigin)
	if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "notice=") {
		t.Fatal("same form receipt was not recognized")
	}
}

func TestOutOfRangePaginationIsRejectedBeforeIntegerNarrowing(t *testing.T) {
	s := testServer(t)
	cookie := authenticated(t, s)
	for _, path := range []string{"/api/v1/items?limit=9223372036854775807", "/api/v1/changes?limit=9223372036854775807", "/api/v1/items?limit=184467440737095516160"} {
		if w := perform(s, "GET", path, "", testSync, nil, ""); w.Code != 400 {
			t.Fatalf("oversized limit accepted on %s", path)
		}
	}
	if w := perform(s, "GET", "/tasks/1/history?after=184467440737095516160", "", "", cookie, ""); w.Code != 400 {
		t.Fatal("oversized history offset accepted")
	}
}
