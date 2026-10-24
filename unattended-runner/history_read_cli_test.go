package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func historyFixture(t *testing.T, change func(string, any) any) (func(context.Context, historyReadScope) (*Runner, error), *[]string) {
	t.Helper()
	paths := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		paths = append(paths, req.Method+" "+req.URL.RequestURI())
		if req.Method != "GET" || req.Header.Get("Authorization") != "Bot fixture-token" {
			t.Error("not the authorized read-only bot request")
		}
		var response any
		switch req.URL.Path {
		case "/users/@me":
			response = map[string]any{"id": "30", "bot": true}
		case "/channels/100":
			response = map[string]any{"id": "100", "guild_id": "10", "parent_id": "40", "owner_id": "30", "type": 11}
		case "/channels/40", "/channels/" + requestForum:
			response = map[string]any{"id": strings.TrimPrefix(req.URL.Path, "/channels/"), "guild_id": "10", "type": 15, "permission_overwrites": []any{}}
		case "/channels/41":
			response = map[string]any{"id": "41", "guild_id": "10", "type": 2, "permission_overwrites": []any{}}
		case "/guilds/10/roles":
			response = []any{map[string]any{"id": "10", "permissions": "66560"}}
		case "/guilds/10/members/30":
			response = map[string]any{"user": map[string]any{"id": "30", "bot": true}, "roles": []any{}}
		case "/channels/100/messages/100":
			response = historyMessage("100", "30", "original question")
		case "/channels/100/messages":
			switch req.URL.Query().Get("before") {
			case "901":
				response = []any{historyMessage("900", "20", "owner answer"), historyMessage("899", "30", "bot context")}
			case "899":
				response = []any{}
			default:
				messages := []any{}
				for id := 1000; id >= 901; id-- {
					messages = append(messages, historyMessage(fmt.Sprint(id), "99", "unrelated human content"))
				}
				response = messages
			}
		default:
			t.Errorf("unexpected source %s", req.URL.Path)
			http.NotFound(w, req)
			return
		}
		if change != nil {
			response = change(req.URL.Path, response)
		}
		if status, ok := response.(int); ok {
			w.WriteHeader(status)
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)
	return func(_ context.Context, scope historyReadScope) (*Runner, error) {
		return &Runner{State: &State{Destination: Destination{Guild: "10", Owner: "20", Bot: "30", Forum: "40"}}, Client: server.Client(), Base: server.URL, Token: "fixture-token", Now: func() time.Time { return time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC) }}, nil
	}, &paths
}

func TestHistoryReadEmptyPageRequiresReadableStarter(t *testing.T) {
	for _, mode := range []string{"empty", "denied", "rate_limited"} {
		t.Run(mode, func(t *testing.T) {
			load, _ := historyFixture(t, func(path string, v any) any {
				if path == "/channels/100/messages/100" && mode == "denied" {
					return 403
				}
				if path == "/channels/100/messages" {
					if mode == "rate_limited" {
						return 429
					}
					return []any{}
				}
				return v
			})
			args, output := historyArgs(t)
			var stdout bytes.Buffer
			code := historyReadCLIWith(args, &stdout, load, func(context.Context, string) bool { return true })
			if mode == "empty" {
				var result map[string]any
				_ = json.Unmarshal(stdout.Bytes(), &result)
				if code != 0 || result["complete"] != true {
					t.Fatal("verified empty history did not finish")
				}
			} else {
				if code == 0 {
					t.Fatal("permission/API failure accepted as no replies")
				}
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("failure advanced receipt")
				}
			}
		})
	}
}

func TestHistoryReadAttachmentObservations(t *testing.T) {
	for _, mode := range []string{"missing", "null", "empty", "attached"} {
		t.Run(mode, func(t *testing.T) {
			load, paths := historyFixture(t, func(path string, value any) any {
				decorate := func(message map[string]any) map[string]any {
					switch mode {
					case "null":
						message["attachments"] = nil
					case "empty":
						message["attachments"] = []any{}
					case "attached":
						message["attachments"] = []any{
							map[string]any{"id": "51", "size": 123, "filename": "private-label", "description": "private-description", "content_type": "private-type", "url": "https://cdn.invalid/?token=fixture-token", "proxy_url": "https://proxy.invalid/fixture-token"},
							map[string]any{"id": "52", "size": 0},
						}
					}
					return message
				}
				switch path {
				case "/channels/100/messages/100":
					return decorate(value.(map[string]any))
				case "/channels/100/messages":
					return []any{decorate(historyMessage("900", "20", "<@30>"))}
				}
				return value
			})
			args, output := historyArgs(t)
			var stdout bytes.Buffer
			if code := historyReadCLIWith(args, &stdout, load, func(context.Context, string) bool { return true }); code != 0 {
				t.Fatalf("attachment observation failed: %d %s", code, stdout.String())
			}
			raw, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			var page map[string]any
			if err := json.Unmarshal(raw, &page); err != nil {
				t.Fatal(err)
			}
			messages := page["messages"].([]any)
			if len(messages) != 1 || messages[0].(map[string]any)["content"] != "<@30>" {
				t.Fatal("mention-only owner reply was lost")
			}
			for _, message := range []map[string]any{page["starter"].(map[string]any), messages[0].(map[string]any)} {
				observed, present := message["attachments"]
				if !present {
					t.Fatal("attachment observation missing from receipt")
				}
				if mode == "missing" || mode == "null" {
					if observed != nil {
						t.Fatal("unknown attachments were reported as an array")
					}
					continue
				}
				attachments, ok := observed.([]any)
				if !ok {
					t.Fatal("known attachment array was lost")
				}
				if mode == "empty" {
					if len(attachments) != 0 {
						t.Fatal("empty API array changed")
					}
					continue
				}
				if len(attachments) != 2 {
					t.Fatal("attachment count changed")
				}
				for i, expected := range []struct {
					id   string
					size float64
				}{{"51", 123}, {"52", 0}} {
					attachment := attachments[i].(map[string]any)
					if len(attachment) != 2 || attachment["id"] != expected.id || attachment["size"] != expected.size {
						t.Fatal("attachment ID/size changed or unnecessary fields retained")
					}
				}
			}
			for _, forbidden := range []string{"private-label", "private-description", "private-type", "cdn.invalid", "proxy.invalid", "fixture-token"} {
				if strings.Contains(string(raw), forbidden) || strings.Contains(stdout.String(), forbidden) {
					t.Fatal("attachment labels or URLs were exposed")
				}
			}
			if len(*paths) != 7 {
				t.Fatalf("history unexpectedly performed extra requests: %v", *paths)
			}
		})
	}
}

func TestHistoryReadRejectsInvalidRetainedAttachments(t *testing.T) {
	for _, target := range []string{"starter", "reply"} {
		for _, mode := range []string{"missing_id", "invalid_id", "missing_size", "negative_size"} {
			t.Run(target+"/"+mode, func(t *testing.T) {
				load, _ := historyFixture(t, func(path string, value any) any {
					attachment := map[string]any{"id": "51", "size": 123}
					switch mode {
					case "missing_id":
						delete(attachment, "id")
					case "invalid_id":
						attachment["id"] = "not-an-id"
					case "missing_size":
						delete(attachment, "size")
					case "negative_size":
						attachment["size"] = -1
					}
					if target == "starter" && path == "/channels/100/messages/100" {
						value.(map[string]any)["attachments"] = []any{attachment}
					}
					if path == "/channels/100/messages" {
						message := historyMessage("900", "20", "<@30>")
						if target == "reply" {
							message["attachments"] = []any{attachment}
						}
						return []any{message}
					}
					return value
				})
				args, output := historyArgs(t)
				var stdout bytes.Buffer
				if historyReadCLIWith(args, &stdout, load, func(context.Context, string) bool { return true }) == 0 {
					t.Fatal("invalid retained attachment accepted")
				}
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("invalid metadata published a success receipt")
				}
			})
		}
	}
}

func TestHistoryReadDuplicateScopeKeysRejectedBeforeLoading(t *testing.T) {
	load, paths := historyFixture(t, nil)
	args, _ := historyArgs(t)
	raw, err := os.ReadFile(args[1])
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"guild_id":"10"`), []byte(`"guild_id":"11","guild_id":"10"`), 1)
	if err = os.WriteFile(args[1], raw, 0600); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	if historyReadCLIWith(args, &stdout, load, func(context.Context, string) bool { return true }) == 0 || len(*paths) != 0 {
		t.Fatal("ambiguous scope reached Discord")
	}
}

func historyMessage(id, author, content string) map[string]any {
	return map[string]any{"id": id, "channel_id": "100", "author": map[string]any{"id": author, "bot": author == "30"}, "content": content, "timestamp": "2026-10-18T12:00:00Z", "edited_timestamp": "2026-10-18T12:01:00Z", "message_reference": map[string]any{"message_id": "100", "channel_id": "100", "guild_id": "10"}}
}

func historyArgs(t *testing.T) ([]string, string) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	scope := historyReadScope{Domain: "discord-history-read-scope-v1", Todo: 3110, Guild: "10", Owner: "20", Since: "2026-10-17T00:00:00Z", Until: "2026-10-19T00:00:00Z", Threads: []string{"100"}}
	b, _ := json.Marshal(scope)
	input := filepath.Join(dir, "scope.private.json")
	if err := os.WriteFile(input, b, 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "page1.private.json")
	return []string{"--scope", input, "--output", output}, output
}

func TestHistoryReadCursorRetrievesExactOwnerAndContext(t *testing.T) {
	load, paths := historyFixture(t, nil)
	args, output := historyArgs(t)
	guard := func(context.Context, string) bool { return true }
	for page := 1; page <= 3; page++ {
		var stdout bytes.Buffer
		if code := historyReadCLIWith(args, &stdout, load, guard); code != 0 {
			t.Fatalf("page %d failed: %d %s", page, code, stdout.String())
		}
		b, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		var receipt struct {
			Complete bool            `json:"complete"`
			Messages []Message       `json:"messages"`
			Source   string          `json:"source"`
			Next     json.RawMessage `json:"next"`
		}
		if json.Unmarshal(b, &receipt) != nil || receipt.Source != "discord-api-v10" {
			t.Fatal("not a source receipt")
		}
		if strings.Contains(string(b), "fixture-token") || strings.Contains(string(b), "unrelated human content") || strings.Contains(stdout.String(), "owner answer") {
			t.Fatal("private/foreign text escaped")
		}
		if page == 2 {
			if len(receipt.Messages) != 2 || receipt.Messages[0].ID != "900" || receipt.Messages[0].Author.ID != "20" || receipt.Messages[0].Edited == "" || receipt.Messages[0].Reference.Message != "100" || receipt.Messages[1].Author.ID != "30" {
				t.Fatal("owner/context/edits/reference lost")
			}
		}
		if receipt.Complete != (page == 3) {
			t.Fatal("partial page mistaken for completion")
		}
		if page < 3 {
			prior := output
			output = filepath.Join(filepath.Dir(output), fmt.Sprintf("page%d.private.json", page+1))
			args = []string{"--scope", args[1], "--output", output, "--cursor", prior}
		}
	}
	if len(*paths) != 21 {
		t.Fatalf("expected identity, channel, parent, roles, member, starter, page GET per call: %v", *paths)
	}
}

func TestHistoryReadRejectsWrongIdentityAndPageBeforeWriting(t *testing.T) {
	for _, mode := range []string{"bot", "guild", "forum", "owner", "starter", "channel", "order", "timestamp", "secret"} {
		t.Run(mode, func(t *testing.T) {
			load, _ := historyFixture(t, func(path string, value any) any {
				switch {
				case path == "/users/@me" && mode == "bot":
					value.(map[string]any)["id"] = "999"
				case path == "/channels/100":
					m := value.(map[string]any)
					switch mode {
					case "guild":
						m["guild_id"] = "11"
					case "forum":
						m["parent_id"] = "41"
					case "owner":
						m["owner_id"] = "99"
					}
				case path == "/channels/100/messages/100" && mode == "starter":
					value.(map[string]any)["channel_id"] = "101"
				case path == "/channels/100/messages":
					m := value.([]any)[0].(map[string]any)
					switch mode {
					case "channel":
						m["channel_id"] = "101"
					case "order":
						value.([]any)[1] = value.([]any)[0]
					case "timestamp":
						m["timestamp"] = "bad"
					case "secret":
						m["author"] = map[string]any{"id": "20"}
						m["content"] = "fixture-token"
					}
				}
				return value
			})
			args, output := historyArgs(t)
			var stdout bytes.Buffer
			if historyReadCLIWith(args, &stdout, load, func(context.Context, string) bool { return true }) == 0 {
				t.Fatal("invalid source accepted")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("failed source wrote success receipt")
			}
			if strings.Contains(stdout.String(), "fixture-token") {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestHistoryReadScopeCursorGuardAndCollision(t *testing.T) {
	load, paths := historyFixture(t, nil)
	args, output := historyArgs(t)
	var out bytes.Buffer
	guard := func(context.Context, string) bool { return true }
	if historyReadCLIWith(args, &out, load, guard) != 0 {
		t.Fatal("positive control failed")
	}
	before, _ := os.ReadFile(output)
	for _, invalid := range [][]string{append(append([]string{}, args...), "--scope", args[1]), append(append([]string{}, args...), "--unknown=secret"), args} {
		calls := len(*paths)
		out.Reset()
		if historyReadCLIWith(invalid, &out, load, guard) == 0 || len(*paths) != calls {
			t.Fatal("invalid flags or collision reached source")
		}
	}
	after, _ := os.ReadFile(output)
	if !bytes.Equal(before, after) {
		t.Fatal("overwrote receipt")
	}
	newOutput := filepath.Join(filepath.Dir(output), "page2.private.json")
	args[3] = newOutput
	calls := len(*paths)
	if historyReadCLIWith(args, &out, load, func(context.Context, string) bool { return false }) == 0 || len(*paths) != calls {
		t.Fatal("guard rejection reached source")
	}
	scope, _ := os.ReadFile(args[1])
	var value historyReadScope
	_ = json.Unmarshal(scope, &value)
	value.Threads = []string{"100", "100"}
	scope, _ = json.Marshal(value)
	_ = os.WriteFile(args[1], scope, 0600)
	if historyReadCLIWith(args, &out, load, guard) == 0 || len(*paths) != calls {
		t.Fatal("duplicate thread scope reached source")
	}
	value.Threads = []string{"100"}
	value.Since = "2026-10-16T00:00:00Z"
	scope, _ = json.Marshal(value)
	_ = os.WriteFile(args[1], scope, 0600)
	args = append(args, "--cursor", output)
	if historyReadCLIWith(args, &out, load, guard) == 0 || len(*paths) != calls {
		t.Fatal("changed scope reused cursor")
	}
	after, _ = os.ReadFile(output)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("existing receipt mutated")
	}
}

func TestHistoryReadCorruptContinuationFailsBeforeLoading(t *testing.T) {
	for _, field := range []string{"before", "thread", "index", "scanned"} {
		t.Run(field, func(t *testing.T) {
			load, _ := historyFixture(t, nil)
			args, prior := historyArgs(t)
			guard := func(context.Context, string) bool { return true }
			var out bytes.Buffer
			if historyReadCLIWith(args, &out, load, guard) != 0 {
				t.Fatal("positive cursor acquisition failed")
			}
			b, _ := os.ReadFile(prior)
			var page map[string]any
			_ = json.Unmarshal(b, &page)
			switch field {
			case "before":
				page["next"].(map[string]any)["before"] = "101"
			case "thread":
				page["thread_id"] = "101"
			case "index":
				page["cursor"].(map[string]any)["thread_index"] = 1
			case "scanned":
				page["scanned_messages"] = 0
			}
			b, _ = json.Marshal(page)
			if err := os.WriteFile(prior, b, 0600); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(filepath.Dir(prior), "next.private.json")
			loads := 0
			counted := func(ctx context.Context, s historyReadScope) (*Runner, error) {
				loads++
				return load(ctx, s)
			}
			if historyReadCLIWith([]string{"--scope", args[1], "--output", output, "--cursor", prior}, &out, counted, guard) == 0 || loads != 0 {
				t.Fatal("corrupt continuation reached credential loader or succeeded")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("corrupt continuation wrote a success receipt")
			}
		})
	}
}
