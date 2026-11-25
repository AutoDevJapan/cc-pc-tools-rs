//go:build windows

package transport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestCapacityStatusRustUsesVerifiedCacheWithoutWriterSession(t *testing.T) {
	cli, bridge := os.Getenv("TODO_CAPACITY_TEST_CLI"), os.Getenv("TODO_CAPACITY_TEST_BRIDGE")
	if cli == "" || bridge == "" {
		t.Skip("fresh Rust CLI and Go bridge fixture binaries not supplied")
	}
	state := strings.Repeat("a", 64)
	const revision = uint64(9007199254740993)
	local := []byte("\xef\xbb\xbf{\"items\":[{\"id\":7,\"status\":\"todo\",\"capacity_decision\":{\"key\":\"capacity-decision-v1\",\"active_generation\":\"fixture-current\"}},{\"id\":8,\"title\":\"fixture-private-item\"}]}\n")
	meta := string(wire(cacheState{Revision: revision, StateID: state, SHA256: digest(local), Bytes: int64(len(local))}))
	for _, fixture := range []struct {
		name         string
		remote       Version
		metadata     string
		marker       string
		mutate       bool
		status       int
		wantCalls    int32
		wantError    string
		omitCache    bool
	}{
		{name: "valid", remote: Version{Revision: revision, StateID: state}, metadata: meta, wantCalls: 1},
		{
			name: "missing cache", remote: Version{Revision: revision, StateID: state}, metadata: meta,
			omitCache: true, wantCalls: 1, wantError: "todo_remote_cache_unavailable",
		},
		{
			name: "newer revision", remote: Version{Revision: revision + 1, StateID: state}, metadata: meta, wantCalls: 1,
			wantError: "todo_remote_cache_revision_mismatch",
		},
		{
			name: "same revision but different state", remote: Version{Revision: revision, StateID: strings.Repeat("b", 64)},
			metadata: meta, wantCalls: 1, wantError: "todo_remote_cache_revision_mismatch",
		},
		{
			name: "legacy sidecar", metadata: "{\"revision\":9007199254740993}", wantCalls: 0,
			wantError: "todo_remote_cache_revision_mismatch",
		},
		{
			name: "invalid sidecar type", metadata: strings.Replace(meta, "9007199254740993", "\"9007199254740993\"", 1), wantCalls: 0,
			wantError: "todo_remote_cache_revision_mismatch",
		},
		{
			name: "missing sidecar", wantCalls: 0, wantError: "todo_remote_cache_unavailable",
		},
		{
			name: "pending", metadata: meta, marker: "todo.json.remote-pending", wantCalls: 0,
			wantError: "todo_remote_cache_revision_mismatch",
		},
		{
			name: "invalidated", metadata: meta, marker: "todo.json.remote-invalid", wantCalls: 0,
			wantError: "todo_remote_cache_revision_mismatch",
		},
		{
			name: "mismatching bytes with equal size", remote: Version{Revision: revision, StateID: state}, metadata: meta,
			mutate: true, wantCalls: 1, wantError: "todo_remote_cache_revision_mismatch",
		},
		{
			name: "unauthorized", status: 401, metadata: meta, wantCalls: 1, wantError: "todo_remote_http_401",
		},
		{
			name: "old server", status: 404, metadata: meta, wantCalls: 1, wantError: "todo_remote_http_404",
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			home := t.TempDir()
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Method != http.MethodGet || r.URL.RequestURI() != "/api/v1/version" {
					t.Error("unexpected operation while reading capacity")
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				// The Rust caller must hold the exact writer lock through the GET.
				peer, openErr := os.OpenFile(filepath.Join(home, "todo.lock"), os.O_RDWR, 0)
				if openErr != nil {
					t.Error("could not open the fixture lock:", openErr)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				lockErr := lockFile(peer)
				if lockErr == nil {
					unlockFile(peer)
				}
				peer.Close()
				if !errors.Is(lockErr, windows.ERROR_LOCK_VIOLATION) && !errors.Is(lockErr, windows.ERROR_IO_PENDING) {
					t.Error("version GET did not hold the writer byte lock:", lockErr)
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
				if fixture.status != 0 {
					w.WriteHeader(fixture.status)
					_, _ = w.Write([]byte("fixture-private-response"))
					return
				}
				_ = json.NewEncoder(w).Encode(fixture.remote)
			}))
			t.Cleanup(server.Close)
			cache := filepath.Join(home, "todo.json")
			credentials := filepath.Join(home, "fixture.dpapi")
			cfg := Config{BaseURL: server.URL, SyncToken: "fixture-token", ClientID: "fixture", CachePath: cache}
			if err := SaveConfig(credentials, cfg); err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{
				"todo.json":        local,
				"todo.lock":        {},
				"todo.remote.json": wire(map[string]string{"bridge": bridge, "credentials": credentials}),
			}
			if fixture.metadata != "" {
				files["todo.json.remote-revision"] = []byte(fixture.metadata)
			}
			if fixture.marker != "" {
				files[fixture.marker] = []byte("owned marker")
			}
			if fixture.mutate {
				files["todo.json"] = bytes.Replace(local, []byte("fixture-current"), []byte("fixture-changed"), 1)
			}
			if fixture.omitCache {
				delete(files, "todo.json")
			}
			for name, body := range files {
				if err := os.WriteFile(filepath.Join(home, name), body, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := capacityFixtureFiles(t, home)
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, cli, "capacity-status")
			hideCommand(cmd)
			cmd.Env = append(os.Environ(), "CC_TODO_HOME="+home, "CC_TODO_JOBS="+home)
			cmd.WaitDelay = 5 * time.Second
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if fixture.wantError == "" {
				var projection map[string]json.RawMessage
				if err != nil || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &projection) != nil ||
					string(projection["registered"]) != "true" || string(projection["todo_id"]) != "7" ||
					string(projection["active_generation"]) != "\"fixture-current\"" ||
					string(projection["launch_policy"]) != "\"ordinary_scheduler\"" ||
					bytes.Contains(stdout.Bytes(), []byte("fixture-private-item")) {
					t.Fatalf("unexpected capacity projection: exit=%v; stdout=%q; stderr=%q", err, stdout.String(), stderr.String())
				}
			} else if err == nil || stdout.Len() != 0 || !strings.Contains(stderr.String(), fixture.wantError) ||
				strings.Contains(stderr.String(), "fixture-private-response") {
				t.Fatalf("inspection did not fail closed: exit=%v; stdout=%q; stderr=%q", err, stdout.String(), stderr.String())
			}
			if calls.Load() != fixture.wantCalls || !reflect.DeepEqual(capacityFixtureFiles(t, home), before) {
				t.Fatal("capacity read used extra HTTP operations or changed the store")
			}
			lock, err := os.OpenFile(filepath.Join(home, "todo.lock"), os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			if err := lockFile(lock); err != nil {
				t.Fatal("capacity child did not release the shared byte lock:", err)
			}
			unlockFile(lock)
		})
	}
}
