package main

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

func inventoryFixture(t *testing.T, mode string) (func(context.Context, historyReadScope) (*Runner, error), *int) {
	t.Helper()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls++
		if req.Method != "GET" || req.Header.Get("Authorization") != "Bot fixture-token" {
			t.Error("inventory must use authorized GET only")
		}
		thread := func(id, owner, stamp string) map[string]any {
			return map[string]any{"id": id, "guild_id": "10", "parent_id": "40", "owner_id": owner, "type": 11, "name": "private title must be discarded", "thread_metadata": map[string]any{"archive_timestamp": stamp}}
		}
		var value any
		switch req.URL.Path {
		case "/users/@me":
			value = map[string]any{"id": "30", "bot": true}
		case "/channels/40":
			value = map[string]any{"id": "40", "guild_id": "10", "type": 15}
			if mode == "foreign_forum" {
				value.(map[string]any)["guild_id"] = "11"
			}
		case "/channels/40/threads/archived/public":
			if req.URL.Query().Get("before") == "" {
				value = map[string]any{"threads": []any{thread("102", "30", "2026-10-19T12:00:00Z"), thread("101", "99", "2026-10-18T12:00:00Z")}, "has_more": true}
			} else {
				value = map[string]any{"threads": []any{thread("100", "20", "2026-10-17T12:00:00Z")}, "has_more": false}
			}
			if mode == "empty_more" {
				value = map[string]any{"threads": []any{}, "has_more": true}
			}
			if mode == "denied" {
				w.WriteHeader(403)
				return
			}
		case "/guilds/10/threads/active":
			value = map[string]any{"threads": []any{thread("102", "30", "2026-10-19T12:00:00Z")}}
		default:
			t.Errorf("unexpected inventory HTTP %s", req.URL.Path)
			http.NotFound(w, req)
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	}))
	t.Cleanup(server.Close)
	return func(context.Context, historyReadScope) (*Runner, error) {
		return &Runner{State: &State{Destination: Destination{Guild: "10", Owner: "20", Bot: "30", Forum: "40"}}, Client: server.Client(), Base: server.URL, Token: "fixture-token", Now: func() time.Time { return time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC) }}, nil
	}, &calls
}

func inventoryArgs(t *testing.T, mode string) ([]string, string) {
	t.Helper()
	readArgs, output := historyArgs(t)
	scope := historyInventoryScope{Domain: "discord-history-inventory-scope-v1", Todo: 3110, Guild: "10", Owner: "20", Since: "2026-10-13T00:00:00Z", Until: "2026-10-20T00:00:00Z", Forum: "task", Mode: mode}
	b, _ := json.Marshal(scope)
	if err := os.WriteFile(readArgs[1], b, 0600); err != nil {
		t.Fatal(err)
	}
	return readArgs, output
}

func TestHistoryInventoryEnumeratesMetadataWithoutBodiesOrLiveState(t *testing.T) {
	for _, mode := range []string{"active", "archived"} {
		t.Run(mode, func(t *testing.T) {
			load, calls := inventoryFixture(t, "")
			args, output := inventoryArgs(t, mode)
			guard := func(context.Context, string) bool { return true }
			var out bytes.Buffer
			if historyInventoryCLIWith(args, &out, load, guard) != 0 {
				t.Fatalf("first page failed: %s", out.String())
			}
			b, _ := os.ReadFile(output)
			if strings.Contains(string(b), "private title") || strings.Contains(string(b), "fixture-token") {
				t.Fatal("title or credential escaped")
			}
			var first historyInventoryPage
			if json.Unmarshal(b, &first) != nil || len(first.Threads) != 1 || first.Threads[0].ID != "102" || first.Complete != (mode == "active") {
				t.Fatal("partial/filtered coverage misreported")
			}
			if mode == "archived" {
				next := filepath.Join(filepath.Dir(output), "second.private.json")
				if historyInventoryCLIWith([]string{"--scope", args[1], "--output", next, "--cursor", output}, &out, load, guard) != 0 {
					t.Fatal("archive continuation failed")
				}
				b, _ = os.ReadFile(next)
				var second historyInventoryPage
				if json.Unmarshal(b, &second) != nil || !second.Complete || len(second.Threads) != 1 || second.Threads[0].ID != "100" {
					t.Fatal("terminal archive page omitted owner thread")
				}
				if *calls != 6 {
					t.Fatal("unexpected HTTP calls")
				}
			} else if *calls != 3 {
				t.Fatal("unexpected active HTTP calls")
			}
		})
	}
}

func TestHistoryInventoryFailureDoesNotCreateSuccessReceipt(t *testing.T) {
	for _, mode := range []string{"foreign_forum", "empty_more", "denied", "guard", "scope"} {
		t.Run(mode, func(t *testing.T) {
			load, calls := inventoryFixture(t, mode)
			args, output := inventoryArgs(t, "archived")
			if mode == "scope" {
				args = append(args, "--scope", args[1])
			}
			var out bytes.Buffer
			if historyInventoryCLIWith(args, &out, load, func(context.Context, string) bool { return mode != "guard" }) == 0 {
				t.Fatal("invalid inventory succeeded")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("invalid inventory wrote success receipt")
			}
			if (mode == "guard" || mode == "scope") && *calls != 0 {
				t.Fatal("invalid input reached API")
			}
		})
	}
}
