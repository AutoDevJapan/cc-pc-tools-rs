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

const monthlyTestGuild = "1552944707464597524"
const monthlyTestOwner = "730822776147148835"
const monthlyTestBot = "1554269962229649478"

type monthlyFixture struct {
	load      func(context.Context, historyReadScope) (*Runner, error)
	paths     []string
	loads     int
	responses map[string]any
	status    map[string]int
}

func newMonthlyFixture(t *testing.T) *monthlyFixture {
	t.Helper()
	f := &monthlyFixture{responses: map[string]any{}, status: map[string]int{}}
	f.responses["/users/@me"] = map[string]any{"id": monthlyTestBot, "bot": true}
	f.responses["/guilds/"+monthlyTestGuild+"/channels"] = []any{
		map[string]any{"id": "100", "type": 0, "guild_id": monthlyTestGuild, "parent_id": "90", "name": "channel-name-must-not-persist"},
		map[string]any{"id": "200", "type": 15, "guild_id": monthlyTestGuild, "parent_id": nil},
	}
	f.responses["/guilds/"+monthlyTestGuild+"/threads/active"] = map[string]any{"threads": []any{
		map[string]any{"id": "400", "type": 11, "guild_id": monthlyTestGuild, "parent_id": "200", "owner_id": "333", "name": "third-party-thread-name"},
		map[string]any{"id": "399", "type": 12, "guild_id": monthlyTestGuild, "parent_id": "100", "owner_id": "333"},
	}}
	for _, id := range []string{"100", "200"} {
		kind := 0
		if id == "200" {
			kind = 15
		}
		f.responses["/channels/"+id] = map[string]any{"id": id, "type": kind, "guild_id": monthlyTestGuild, "permission_overwrites": []any{}}
	}
	f.responses["/channels/400"] = map[string]any{"id": "400", "type": 11, "guild_id": monthlyTestGuild, "parent_id": "200", "owner_id": "333"}
	f.responses["/channels/399"] = map[string]any{"id": "399", "type": 12, "guild_id": monthlyTestGuild, "parent_id": "100", "owner_id": "333"}
	f.responses["/channels/399/thread-members/"+monthlyTestBot] = map[string]any{"id": "399", "user_id": monthlyTestBot}
	f.responses["/guilds/"+monthlyTestGuild+"/roles"] = []any{map[string]any{"id": monthlyTestGuild, "permissions": "66560"}}
	f.responses["/guilds/"+monthlyTestGuild+"/members/"+monthlyTestBot] = map[string]any{"user": map[string]any{"id": monthlyTestBot, "bot": true}, "roles": []string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.paths = append(f.paths, r.Method+" "+r.URL.RequestURI())
		if r.Method != http.MethodGet {
			t.Errorf("unexpected HTTP mutation: %s", r.Method)
			w.WriteHeader(500)
			return
		}
		if code := f.status[r.URL.Path]; code != 0 {
			w.WriteHeader(code)
			return
		}
		value, exists := f.responses[r.URL.Path]
		if !exists {
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(srv.Close)
	f.load = func(context.Context, historyReadScope) (*Runner, error) {
		f.loads++
		return &Runner{State: &State{Destination: Destination{Guild: monthlyTestGuild, Owner: monthlyTestOwner, Bot: monthlyTestBot, Forum: "1554276307997692004"}}, Client: srv.Client(), Base: srv.URL, Token: "synthetic-token", Now: func() time.Time { return time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC) }}, nil
	}
	return f
}

func monthlyTestScope(domain string) map[string]any {
	return map[string]any{"domain": domain, "todo_id": 3327, "guild_id": monthlyTestGuild, "owner_id": monthlyTestOwner, "since": "2026-09-22T00:00:00+09:00", "until": "2026-10-22T16:08:09+09:00"}
}

func monthlyTestArgs(t *testing.T, scope any) ([]string, string) {
	t.Helper()
	dir := t.TempDir()
	b, err := json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "scope.private.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(dir, "result.private.json")
	return []string{"--scope", path, "--output", output}, output
}

func monthlyTestReceipt(t *testing.T, path string) (map[string]any, []byte) {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var p map[string]any
	if e = json.Unmarshal(b, &p); e != nil {
		t.Fatal(e)
	}
	return p, b
}

func monthlyPermit(context.Context, string) bool { return true }

func TestMonthlyInventoryChannelsAndAllActiveCreators(t *testing.T) {
	for _, mode := range []string{"channels", "active"} {
		t.Run(mode, func(t *testing.T) {
			f := newMonthlyFixture(t)
			s := monthlyTestScope("discord-monthly-history-inventory-scope-v1")
			s["mode"] = mode
			args, path := monthlyTestArgs(t, s)
			var out bytes.Buffer
			if code := monthlyInventoryCLIWith(args, &out, f.load, monthlyPermit); code != 0 {
				t.Fatalf("entry failed: %d %s", code, out.String())
			}
			p, b := monthlyTestReceipt(t, path)
			if len(p["channels"].([]any)) != 2 {
				t.Fatal("missing structural candidates")
			}
			for _, bad := range []string{"channel-name-must-not-persist", "third-party-thread-name", "\"content\"", "\"name\"", "synthetic-token"} {
				if bytes.Contains(b, []byte(bad)) || strings.Contains(out.String(), bad) {
					t.Fatal("receipt/stdout retained forbidden data")
				}
			}
			if mode == "active" && p["channels"].([]any)[0].(map[string]any)["parent_id"] != "200" {
				t.Fatal("active parent missing")
			}
		})
	}
}

func TestMonthlyInventoryArchiveCursorKindsAndOldThreads(t *testing.T) {
	for _, mode := range []string{"public-archived", "private-archived", "joined-private-archived"} {
		t.Run(mode, func(t *testing.T) {
			f := newMonthlyFixture(t)
			s := monthlyTestScope("discord-monthly-history-inventory-scope-v1")
			s["mode"] = mode
			parent, kind, endpoint := "200", 11, "/channels/200/threads/archived/public"
			if mode != "public-archived" {
				parent, kind, endpoint = "100", 12, "/channels/100/threads/archived/private"
			}
			if mode == "joined-private-archived" {
				endpoint = "/channels/100/users/@me/threads/archived/private"
			}
			s["channel_id"] = parent
			f.responses[endpoint] = map[string]any{"threads": []any{map[string]any{"id": "400", "type": kind, "parent_id": parent, "guild_id": monthlyTestGuild, "owner_id": "333", "thread_metadata": map[string]any{"archive_timestamp": "2026-09-01T01:00:00Z"}}}, "has_more": true}
			args, path := monthlyTestArgs(t, s)
			var out bytes.Buffer
			if monthlyInventoryCLIWith(args, &out, f.load, monthlyPermit) != 0 {
				t.Fatal("first archive page failed", out.String())
			}
			p, _ := monthlyTestReceipt(t, path)
			want := "2026-09-01T01:00:00Z"
			if mode == "joined-private-archived" {
				want = "400"
			}
			if p["next"] != want || len(p["channels"].([]any)) != 1 {
				t.Fatal("wrong cursor or old thread excluded")
			}
			f.responses[endpoint] = map[string]any{"threads": []any{}, "has_more": false}
			next := filepath.Join(filepath.Dir(path), "next.private.json")
			args[3] = next
			args = append(args, "--cursor", path)
			out.Reset()
			if monthlyInventoryCLIWith(args, &out, f.load, monthlyPermit) != 0 {
				t.Fatal("resume failed", out.String())
			}
			last, _ := monthlyTestReceipt(t, next)
			if last["complete"] != true || last["timestamp_boundary_unresolved"] != (mode != "joined-private-archived") {
				t.Fatal("lost boundary qualification")
			}
			if !strings.Contains(f.paths[len(f.paths)-1], "before=") {
				t.Fatal("cursor not sent")
			}
		})
	}
}

func monthlyMessage(id, channel, author, body string, bot bool) map[string]any {
	return map[string]any{"id": id, "channel_id": channel, "author": map[string]any{"id": author, "bot": bot}, "type": 0, "content": body, "timestamp": "2026-10-01T00:00:00Z", "attachments": []any{}}
}

func TestMonthlyReadOwnerOnlyTextAndThirdPartyThreads(t *testing.T) {
	for _, channel := range []string{"100", "400", "399"} {
		t.Run(channel, func(t *testing.T) {
			f := newMonthlyFixture(t)
			s := monthlyTestScope("discord-monthly-history-read-scope-v1")
			s["channel_ids"] = []string{channel}
			f.responses["/channels/"+channel+"/messages"] = []any{monthlyMessage("800", channel, monthlyTestOwner, "本人の条件", false), monthlyMessage("799", channel, "333", "third-party-secret-text", false), monthlyMessage("798", channel, monthlyTestBot, "bot-context-not-selected", true)}
			args, path := monthlyTestArgs(t, s)
			var out bytes.Buffer
			if monthlyReadCLIWith(args, &out, f.load, monthlyPermit) != 0 {
				t.Fatal("owner history failed", out.String())
			}
			p, b := monthlyTestReceipt(t, path)
			if p["scanned_messages"] != float64(3) || len(p["messages"].([]any)) != 1 {
				t.Fatal("owner selection wrong")
			}
			if bytes.Contains(b, []byte("third-party-secret-text")) || bytes.Contains(b, []byte("bot-context-not-selected")) || strings.Contains(out.String(), "本人の条件") {
				t.Fatal("unselected/stdout body leak")
			}
			f.responses["/channels/"+channel+"/messages"] = []any{}
			next := filepath.Join(filepath.Dir(path), "next.private.json")
			args[3] = next
			args = append(args, "--cursor", path)
			out.Reset()
			if monthlyReadCLIWith(args, &out, f.load, monthlyPermit) != 0 {
				t.Fatal("history continuation failed", out.String())
			}
			terminal, _ := monthlyTestReceipt(t, next)
			if terminal["complete"] != true {
				t.Fatal("permitted terminal page incomplete")
			}
		})
	}
}

func TestMonthlyInvalidAuthorityStopsBeforeLoad(t *testing.T) {
	for _, field := range []string{"todo_id", "guild_id", "owner_id", "since", "until", "domain"} {
		t.Run(field, func(t *testing.T) {
			f := newMonthlyFixture(t)
			s := monthlyTestScope("discord-monthly-history-inventory-scope-v1")
			s["mode"] = "channels"
			s[field] = "different"
			if field == "todo_id" {
				s[field] = 3331
			}
			args, path := monthlyTestArgs(t, s)
			var out bytes.Buffer
			if monthlyInventoryCLIWith(args, &out, f.load, monthlyPermit) == 0 || f.loads != 0 {
				t.Fatal("invalid scope loaded credentials")
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("invalid scope receipt")
			}
		})
	}
}

func TestMonthlyFailuresNeverPublishOrAdvance(t *testing.T) {
	for _, kind := range []string{"403", "404", "history_permission", "overwrite_deny", "duplicate", "secret", "wrong_guild", "private_membership", "guard"} {
		t.Run(kind, func(t *testing.T) {
			f := newMonthlyFixture(t)
			channel := "100"
			if kind == "private_membership" {
				channel = "399"
			}
			s := monthlyTestScope("discord-monthly-history-read-scope-v1")
			s["channel_ids"] = []string{channel}
			endpoint := "/channels/" + channel + "/messages"
			f.responses[endpoint] = []any{monthlyMessage("800", channel, monthlyTestOwner, "selected", false)}
			guard := monthlyPermit
			switch kind {
			case "403":
				f.status[endpoint] = 403
			case "404":
				f.status[endpoint] = 404
			case "history_permission":
				f.responses["/guilds/"+monthlyTestGuild+"/roles"] = []any{map[string]any{"id": monthlyTestGuild, "permissions": "1024"}}
				f.responses[endpoint] = []any{}
			case "overwrite_deny":
				f.responses["/channels/100"].(map[string]any)["permission_overwrites"] = []any{map[string]any{"id": monthlyTestBot, "type": 1, "allow": "0", "deny": "65536"}}
			case "duplicate":
				f.responses[endpoint] = []any{monthlyMessage("800", channel, monthlyTestOwner, "a", false), monthlyMessage("800", channel, monthlyTestOwner, "b", false)}
			case "secret":
				f.responses[endpoint] = []any{monthlyMessage("800", channel, monthlyTestOwner, "password=do-not-store", false)}
			case "wrong_guild":
				f.responses["/channels/100"].(map[string]any)["guild_id"] = "11"
			case "private_membership":
				f.status["/channels/399/thread-members/"+monthlyTestBot] = 404
			case "guard":
				guard = func(context.Context, string) bool { return false }
			}
			args, path := monthlyTestArgs(t, s)
			var out bytes.Buffer
			if monthlyReadCLIWith(args, &out, f.load, guard) == 0 {
				t.Fatal("invalid read succeeded", f.paths)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatal("failure published success receipt")
			}
			if strings.Contains(out.String(), "do-not-store") || strings.Contains(out.String(), "synthetic-token") {
				t.Fatal("failure leaked source")
			}
			if (kind == "403" || kind == "404") && !strings.Contains(out.String(), kind) {
				t.Fatal("missing concrete API failure status")
			}
		})
	}
}

func TestMonthlyCursorFailureKeepsOriginalReceipt(t *testing.T) {
	for _, mode := range []string{"scope_change", "cursor_change", "page_failure"} {
		t.Run(mode, func(t *testing.T) {
			f := newMonthlyFixture(t)
			s := monthlyTestScope("discord-monthly-history-read-scope-v1")
			s["channel_ids"] = []string{"100", "400"}
			f.responses["/channels/100/messages"] = []any{monthlyMessage("800", "100", monthlyTestOwner, "original", false)}
			args, path := monthlyTestArgs(t, s)
			var out bytes.Buffer
			if monthlyReadCLIWith(args, &out, f.load, monthlyPermit) != 0 {
				t.Fatal(out.String())
			}
			_, original := monthlyTestReceipt(t, path)
			cursor := path
			if mode == "scope_change" {
				s["channel_ids"] = []string{"100"}
				b, _ := json.Marshal(s)
				if err := os.WriteFile(args[1], b, 0600); err != nil {
					t.Fatal(err)
				}
			} else if mode == "cursor_change" {
				var altered map[string]any
				if err := json.Unmarshal(original, &altered); err != nil {
					t.Fatal(err)
				}
				altered["next"].(map[string]any)["before"] = "801"
				b, _ := json.Marshal(altered)
				cursor = filepath.Join(filepath.Dir(path), "altered.private.json")
				if err := os.WriteFile(cursor, b, 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				f.status["/channels/100/messages"] = 403
			}
			next := filepath.Join(filepath.Dir(path), "next.private.json")
			args[3] = next
			args = append(args, "--cursor", cursor)
			loads := f.loads
			out.Reset()
			if monthlyReadCLIWith(args, &out, f.load, monthlyPermit) == 0 {
				t.Fatal("bad continuation succeeded")
			}
			if mode != "page_failure" && f.loads != loads {
				t.Fatal("bad cursor loaded credentials")
			}
			if _, err := os.Stat(next); !os.IsNotExist(err) {
				t.Fatal("failed continuation published")
			}
			_, current := monthlyTestReceipt(t, path)
			if !bytes.Equal(original, current) {
				t.Fatal("prior receipt changed")
			}
		})
	}
}

func TestMonthlyArchiveTiesAreAcceptedAndBoundaryIsExplicit(t *testing.T) {
	f := newMonthlyFixture(t)
	s := monthlyTestScope("discord-monthly-history-inventory-scope-v1")
	s["mode"], s["channel_id"] = "public-archived", "200"
	thread := func(id string) map[string]any {
		return map[string]any{"id": id, "type": 11, "parent_id": "200", "thread_metadata": map[string]any{"archive_timestamp": "2026-09-01T01:00:00Z"}}
	}
	f.responses["/channels/200/threads/archived/public"] = map[string]any{"threads": []any{thread("400"), thread("399")}, "has_more": true}
	args, path := monthlyTestArgs(t, s)
	var out bytes.Buffer
	if monthlyInventoryCLIWith(args, &out, f.load, monthlyPermit) != 0 {
		t.Fatal(out.String())
	}
	p, original := monthlyTestReceipt(t, path)
	if p["timestamp_boundary_unresolved"] != true || len(p["channels"].([]any)) != 2 {
		t.Fatal("equal-time page lost coverage qualification")
	}
	// An API returning the exclusive boundary again cannot advance the walk.
	next := filepath.Join(filepath.Dir(path), "next.private.json")
	args[3] = next
	args = append(args, "--cursor", path)
	out.Reset()
	if monthlyInventoryCLIWith(args, &out, f.load, monthlyPermit) == 0 {
		t.Fatal("nonprogressing timestamp page accepted")
	}
	if _, err := os.Stat(next); !os.IsNotExist(err) {
		t.Fatal("invalid archive receipt")
	}
	_, current := monthlyTestReceipt(t, path)
	if !bytes.Equal(original, current) {
		t.Fatal("original archive overwritten")
	}
}

func TestMonthlyRoleOverwritePrecedenceAndThreadType(t *testing.T) {
	f := newMonthlyFixture(t)
	f.responses["/guilds/"+monthlyTestGuild+"/roles"] = []any{map[string]any{"id": monthlyTestGuild, "permissions": "0"}, map[string]any{"id": "501", "permissions": "0"}, map[string]any{"id": "502", "permissions": "0"}}
	f.responses["/guilds/"+monthlyTestGuild+"/members/"+monthlyTestBot] = map[string]any{"user": map[string]any{"id": monthlyTestBot, "bot": true}, "roles": []string{"501", "502"}}
	f.responses["/channels/100"].(map[string]any)["permission_overwrites"] = []any{
		map[string]any{"id": monthlyTestGuild, "type": 0, "allow": "0", "deny": "66560"},
		map[string]any{"id": "501", "type": 0, "allow": "1024", "deny": "65536"},
		map[string]any{"id": "502", "type": 0, "allow": "65536", "deny": "1024"},
	}
	f.responses["/channels/399/messages"] = []any{}
	s := monthlyTestScope("discord-monthly-history-read-scope-v1")
	s["channel_ids"] = []string{"399"}
	args, path := monthlyTestArgs(t, s)
	var out bytes.Buffer
	if monthlyReadCLIWith(args, &out, f.load, monthlyPermit) != 0 {
		t.Fatal(out.String())
	}
	p, _ := monthlyTestReceipt(t, path)
	if p["channel_type"] != float64(12) || p["complete"] != true {
		t.Fatal("parent decode changed selected type")
	}
	found := false
	for _, path := range f.paths {
		if path == "GET /channels/399/thread-members/"+monthlyTestBot {
			found = true
		}
	}
	if !found {
		t.Fatal("private thread membership was not verified")
	}
}
