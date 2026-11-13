package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"autodevjapan/shared-todo-cloud/internal/httpapi"
	"autodevjapan/shared-todo-cloud/internal/store"
)

func TestCurrentNoteProjectionRejectsMalformedResponses(t *testing.T) {
	state := strings.Repeat("a", 64)
	note := "試験😀\n" + strings.Repeat("long", 3000)
	valid := fmt.Sprintf(`{"id":7,"note":%q,"noteSha256":%q,"revision":5,"stateId":%q}`, note, digest([]byte(note)), state)
	tests := []struct {
		name, body string
		ok         bool
	}{
		{"valid", valid, true},
		{"wrong-id", strings.Replace(valid, `"id":7`, `"id":8`, 1), false},
		{"wrong-digest", strings.Replace(valid, digest([]byte(note)), strings.Repeat("0", 64), 1), false},
		{"missing-note", fmt.Sprintf(`{"id":7,"noteSha256":%q,"revision":5,"stateId":%q}`, digest(nil), state), false},
		{"null-note", strings.Replace(valid, fmt.Sprintf(`"note":%q`, note), `"note":null`, 1), false},
		{"duplicate", strings.Replace(valid, `"id":7`, `"id":7,"id":7`, 1), false},
		{"unknown", strings.Replace(valid, `"id":7`, `"id":7,"other":"secret"`, 1), false},
		{"state-empty", strings.Replace(valid, state, "", 1), false},
		{"trailing", valid + ` {}`, false},
		{"invalid-utf8", strings.Replace(valid, "試験", string([]byte{0xff}), 1), false},
		{"unpaired-surrogate", fmt.Sprintf(`{"id":7,"note":"\ud800","noteSha256":%q,"revision":5,"stateId":%q}`, digest([]byte("�")), state), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/items/7/note" || r.Header.Get("Authorization") != "Bearer synthetic-token" {
					t.Fatal("unexpected route or missing existing authorization")
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "synthetic-token")
			if err != nil {
				t.Fatal(err)
			}
			got, err := client.currentNote(context.Background(), 7)
			if tc.ok {
				if err != nil || got.Note != note || got.StateID != state || got.Revision != 5 {
					t.Fatal("valid long projection failed")
				}
			} else if err == nil {
				t.Fatal("invalid projection was accepted")
			}
			if calls != 1 {
				t.Fatal("projection did not use exactly one request")
			}
		})
	}
}

func TestCurrentNoteRustBridgeToRealHTTPStoreWithoutSnapshot(t *testing.T) {
	cli, bridge := os.Getenv("TODO_REMOTE_TEST_CLI"), os.Getenv("TODO_REMOTE_TEST_BRIDGE")
	if cli == "" || bridge == "" {
		t.Skip("fresh Rust CLI and Go bridge fixture binaries not supplied")
	}
	ledger, err := store.Open(filepath.Join(t.TempDir(), "fixture.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ledger.Close() })
	note := strings.Repeat("日本語😀\n", 2500)
	selected, _ := json.Marshal(map[string]any{"id": 7, "note": note, "title": "fixture-secret-field"})
	other, _ := json.Marshal(map[string]any{"id": 8, "note": "fixture-secret-other"})
	initial, err := ledger.Update(store.UpdateRequest{RequestID: "synthetic-seed", Upserts: []json.RawMessage{selected, other}})
	if err != nil {
		t.Fatal(err)
	}
	api, err := httpapi.New(ledger, httpapi.Config{AdminToken: strings.Repeat("a", 40), SyncToken: strings.Repeat("s", 40)})
	if err != nil {
		t.Fatal(err)
	}
	var reads, otherCalls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/state" {
			otherCalls.Add(1)
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		if r.URL.Path != "/api/v1/items/7/note" {
			otherCalls.Add(1)
		} else {
			reads.Add(1)
		}
		api.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	home := t.TempDir()
	path := filepath.Join(home, "todo.json")
	before := []byte(`{"items":[{"id":7,"note":"stale-cache-fixture"}],"next_id":8}`)
	if err = os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	credentials := filepath.Join(home, "fixture.dpapi")
	if err = SaveConfig(credentials, Config{BaseURL: server.URL, SyncToken: strings.Repeat("s", 40), ClientID: "fixture-pc", CachePath: path}); err != nil {
		t.Fatal(err)
	}
	marker := wire(map[string]string{"bridge": bridge, "credentials": credentials})
	if err = os.WriteFile(filepath.Join(home, "todo.remote.json"), marker, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(cli, "current-note", "7")
	hideCommand(cmd)
	cmd.Env = append(os.Environ(), "CC_TODO_HOME="+home, "CC_TODO_JOBS="+home)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err = cmd.Run(); err != nil {
		t.Fatalf("CLI failed: %s", stderr.String())
	}
	var got CurrentNote
	if err = json.Unmarshal(stdout.Bytes(), &got); err != nil || got.ID != 7 || got.Note != note || got.NoteSHA256 != digest([]byte(note)) || got.Revision != initial.Revision || got.StateID != initial.StateID {
		t.Fatal("CLI projection mismatched synthetic ledger")
	}
	if bytes.Contains(stdout.Bytes(), []byte("fixture-secret")) || !bytes.Equal(read(t, path), before) || reads.Load() != 1 || otherCalls.Load() != 0 {
		t.Fatal("unexpected private output, cache write, snapshot, or extra HTTP request")
	}
}

func TestCurrentNoteRustBridgeHTTPFailurePreservesCache(t *testing.T) {
	cli, bridge := os.Getenv("TODO_REMOTE_TEST_CLI"), os.Getenv("TODO_REMOTE_TEST_BRIDGE")
	if cli == "" || bridge == "" {
		t.Skip("fresh Rust CLI and Go bridge fixture binaries not supplied")
	}
	for _, status := range []int{401, 403, 404, 413, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls, unexpected atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/api/v1/items/7/note" {
					unexpected.Add(1)
				}
				w.WriteHeader(status)
				_, _ = w.Write([]byte("fixture-private-error-body"))
			}))
			defer server.Close()
			home := t.TempDir()
			path := filepath.Join(home, "todo.json")
			before := []byte(`{"items":[{"id":7,"note":"stale-private-cache"}],"next_id":8}`)
			if err := os.WriteFile(path, before, 0600); err != nil {
				t.Fatal(err)
			}
			credentials := filepath.Join(home, "fixture.dpapi")
			if err := SaveConfig(credentials, Config{BaseURL: server.URL, SyncToken: "fixture-token", ClientID: "fixture-pc", CachePath: path}); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, "todo.remote.json"), wire(map[string]string{"bridge": bridge, "credentials": credentials}), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(cli, "current-note", "7")
			hideCommand(cmd)
			cmd.Env = append(os.Environ(), "CC_TODO_HOME="+home, "CC_TODO_JOBS="+home)
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Run(); err == nil || stdout.Len() != 0 || strings.Contains(stderr.String(), "fixture-private") || strings.Contains(stderr.String(), "stale-private") {
				t.Fatal("failed request exposed content or reported success")
			}
			if !bytes.Equal(read(t, path), before) || calls.Load() != 1 || unexpected.Load() != 0 {
				t.Fatalf("failed read changed cache or attempted a fallback: calls=%d unexpected=%d stderr=%s", calls.Load(), unexpected.Load(), stderr.String())
			}
		})
	}
}

func TestCurrentNoteRustBridgeNetworkFailureHasNoCacheFallback(t *testing.T) {
	cli, bridge := os.Getenv("TODO_REMOTE_TEST_CLI"), os.Getenv("TODO_REMOTE_TEST_BRIDGE")
	if cli == "" || bridge == "" {
		t.Skip("fresh Rust CLI and Go bridge fixture binaries not supplied")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	url := "http://" + listener.Addr().String()
	_ = listener.Close()
	home := t.TempDir()
	path := filepath.Join(home, "todo.json")
	before := []byte(`{"items":[{"id":7,"note":"stale-private-cache"}],"next_id":8}`)
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	credentials := filepath.Join(home, "fixture.dpapi")
	if err := SaveConfig(credentials, Config{BaseURL: url, SyncToken: "fixture-token", ClientID: "fixture-pc", CachePath: path}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "todo.remote.json"), wire(map[string]string{"bridge": bridge, "credentials": credentials}), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(cli, "current-note", "7")
	hideCommand(cmd)
	cmd.Env = append(os.Environ(), "CC_TODO_HOME="+home, "CC_TODO_JOBS="+home)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err == nil || stdout.Len() != 0 || bytes.Contains(stderr.Bytes(), []byte("stale-private")) || !bytes.Equal(read(t, path), before) {
		t.Fatal("network failure exposed or changed the stale cache")
	}
}

func TestCurrentNoteHTTPFailureNeverFallsBackToSnapshot(t *testing.T) {
	for _, status := range []int{401, 403, 404, 413, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/items/7/note" {
					t.Fatal("unexpected snapshot or fallback request")
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			client, err := NewClient(server.URL, "synthetic-token")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := client.currentNote(context.Background(), 7); err == nil || calls != 1 {
				t.Fatal("HTTP failure was not preserved")
			}
		})
	}
}
