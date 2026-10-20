package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestHistoryReadFutureFirstThreadRejectedBeforeLoading(t *testing.T) {
	load, _ := historyFixture(t, nil)
	args, output := historyArgs(t)
	loads := 0
	counted := func(ctx context.Context, scope historyReadScope) (*Runner, error) {
		loads++
		if scope.Threads[0] != "100" {
			return nil, errors.New("future scope reached loader")
		}
		return load(ctx, scope)
	}
	guard := func(context.Context, string) bool { return true }
	var out bytes.Buffer
	if historyReadCLIWith(args, &out, counted, guard) != 0 || loads != 1 {
		t.Fatal("loader positive control failed")
	}
	raw, _ := os.ReadFile(args[1])
	var scope historyReadScope
	_ = json.Unmarshal(raw, &scope)
	high, _ := number(historyHighBefore(mustHistoryTime(scope.Until)))
	scope.Threads = []string{strconv.FormatUint(high+1, 10), "100"}
	raw, _ = json.Marshal(scope)
	if err := os.WriteFile(args[1], raw, 0600); err != nil {
		t.Fatal(err)
	}
	args[3] = filepath.Join(filepath.Dir(output), "future.private.json")
	loads = 0
	if historyReadCLIWith(args, &out, counted, guard) == 0 || loads != 0 {
		t.Fatal("future thread reached credential loader")
	}
	if _, err := os.Stat(args[3]); !os.IsNotExist(err) {
		t.Fatal("invalid scope wrote a continuation")
	}
}

func TestHistoryReadChecksSecretsOnlyForRetainedBodies(t *testing.T) {
	for _, mode := range []string{"third_party", "outside_window", "retained_owner"} {
		t.Run(mode, func(t *testing.T) {
			load, _ := historyFixture(t, func(path string, v any) any {
				if path == "/channels/100/messages" {
					m := historyMessage("900", "20", "authorization: fixture-value")
					if mode == "third_party" {
						m["author"] = map[string]any{"id": "99", "bot": false}
					}
					if mode == "outside_window" {
						m["timestamp"] = "2026-10-16T12:00:00Z"
					}
					return []any{m}
				}
				return v
			})
			args, output := historyArgs(t)
			var out bytes.Buffer
			code := historyReadCLIWith(args, &out, load, func(context.Context, string) bool { return true })
			if mode == "retained_owner" {
				if code == 0 {
					t.Fatal("selected credential-bearing body accepted")
				}
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("selected secret persisted")
				}
			} else {
				if code != 0 {
					t.Fatal("discarded body prevented read-only audit")
				}
				b, err := os.ReadFile(output)
				if err != nil || strings.Contains(string(b), "fixture-value") {
					t.Fatal("discarded body persisted")
				}
			}
			if strings.Contains(out.String(), "fixture-value") {
				t.Fatal("body leaked to stdout")
			}
		})
	}
}

func TestHistoryReadRetainsMessageTypeWithoutInventingIt(t *testing.T) {
	for _, kind := range []int{7, 19} {
		t.Run(strconv.Itoa(kind), func(t *testing.T) {
			load, _ := historyFixture(t, func(path string, v any) any {
				if path == "/channels/100/messages" {
					m := historyMessage("900", "20", "owner event")
					m["type"] = kind
					return []any{m}
				}
				return v
			})
			args, output := historyArgs(t)
			var out bytes.Buffer
			if historyReadCLIWith(args, &out, load, func(context.Context, string) bool { return true }) != 0 {
				t.Fatal("typed source failed")
			}
			b, _ := os.ReadFile(output)
			var record struct {
				Messages []map[string]any `json:"messages"`
				Starter  map[string]any   `json:"starter"`
			}
			if json.Unmarshal(b, &record) != nil || len(record.Messages) != 1 || record.Messages[0]["type"] != float64(kind) {
				t.Fatal("API message type lost")
			}
			if _, known := record.Starter["type"]; known {
				t.Fatal("missing source type invented as default")
			}
		})
	}
}

func TestHistoryReadCaseFoldedDuplicateKeysFailBeforeLoading(t *testing.T) {
	for _, alias := range []string{"Guild_ID", "ſince"} {
		t.Run(alias, func(t *testing.T) {
			load, _ := historyFixture(t, nil)
			args, _ := historyArgs(t)
			b, _ := os.ReadFile(args[1])
			b = append([]byte(`{"`+alias+`":"ignored",`), b[1:]...)
			if err := os.WriteFile(args[1], b, 0600); err != nil {
				t.Fatal(err)
			}
			loads := 0
			counted := func(ctx context.Context, s historyReadScope) (*Runner, error) { loads++; return load(ctx, s) }
			var out bytes.Buffer
			if historyReadCLIWith(args, &out, counted, func(context.Context, string) bool { return true }) == 0 || loads != 0 {
				t.Fatal("case-fold duplicate reached loader")
			}
		})
	}
}
