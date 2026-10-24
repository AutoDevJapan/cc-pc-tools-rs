package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// #400: Given a scoped channel and effective bot permissions, When history-read
// runs, Then retain only the configured owner/bot, without inventing a starter.
func historyChannelFixture(t *testing.T, kind, parentKind int) *monthlyFixture {
	t.Helper()
	f := newMonthlyFixture(t)
	c := map[string]any{"id": "100", "type": kind, "guild_id": monthlyTestGuild, "permission_overwrites": []any{}}
	if kind == 10 || kind == 11 || kind == 12 {
		c["parent_id"], c["owner_id"] = "200", "999"
		f.responses["/channels/200"] = map[string]any{"id": "200", "type": parentKind, "guild_id": monthlyTestGuild, "permission_overwrites": []any{}}
	}
	f.responses["/channels/100"] = c
	f.responses["/channels/100/thread-members/"+monthlyTestBot] = map[string]any{"id": "100", "user_id": monthlyTestBot}
	f.responses["/guilds/"+monthlyTestGuild+"/roles"] = []any{map[string]any{"id": monthlyTestGuild, "permissions": strconv.FormatUint(monthlyView|monthlyHistory|monthlyConnect, 10)}}
	message := func(id, author, content string) map[string]any {
		m := historyMessage(id, author, content)
		m["author"] = map[string]any{"id": author, "bot": author == monthlyTestBot || author == "998"}
		m["message_reference"] = map[string]any{"message_id": "100", "channel_id": "100", "guild_id": monthlyTestGuild}
		m["type"] = 0
		return m
	}
	later := message("905", monthlyTestBot, "later context")
	later["attachments"] = []any{}
	owner := message("904", monthlyTestOwner, "edited owner answer")
	owner["type"] = 19
	owner["attachments"] = []any{map[string]any{"id": "51", "size": 0}}
	f.responses["/channels/100/messages"] = []any{later, owner, message("903", "997", "foreign human"), message("902", "998", "foreign bot"), message("901", monthlyTestBot, "earlier context"), message("900", "997", "older foreign human")}
	f.responses["/channels/100/messages/100"] = message("100", monthlyTestBot, "original task")
	return f
}

func historyChannelArgs(t *testing.T) ([]string, string) {
	t.Helper()
	s := monthlyTestScope("discord-history-read-scope-v1")
	s["todo_id"] = 3387
	s["thread_ids"] = []string{"100"} // v1 spelling remains compatible.
	return monthlyTestArgs(t, s)
}

func TestHistoryReadAdditionalChannels(t *testing.T) {
	for _, tc := range []struct {
		name         string
		kind, parent int
		starter      bool
	}{
		{"text", 0, 0, false}, {"announcement", 5, 0, false},
		{"forum", 11, 15, true}, {"media", 11, 16, true},
		{"voice", 2, 0, false}, {"stage", 13, 0, false},
		{"private", 12, 0, false}, {"public_text_thread", 11, 0, false}, {"announcement_thread", 10, 5, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := historyChannelFixture(t, tc.kind, tc.parent)
			args, output := historyChannelArgs(t)
			var out bytes.Buffer
			if historyReadCLIWith(args, &out, f.load, monthlyPermit) != 0 {
				t.Fatal("readable channel rejected", out.String(), f.paths)
			}
			p, raw := monthlyTestReceipt(t, output)
			if p["channel_type"] != float64(tc.kind) || p["scanned_messages"] != float64(6) || p["last_scanned_id"] != "900" || p["next"].(map[string]any)["before"] != "900" {
				t.Fatal("channel metadata or all-message cursor lost", p)
			}
			messages := p["messages"].([]any)
			ids := []string{}
			for _, m := range messages {
				ids = append(ids, m.(map[string]any)["id"].(string))
			}
			if !reflect.DeepEqual(ids, []string{"905", "904", "901"}) {
				t.Fatal("owner/bot context was lost or third party retained", ids)
			}
			answer := messages[1].(map[string]any)
			if answer["content"] != "edited owner answer" || answer["edited_timestamp"] != "2026-10-18T12:01:00Z" || answer["type"] != float64(19) || answer["message_reference"].(map[string]any)["message_id"] != "100" || answer["author"].(map[string]any)["bot"] != false {
				t.Fatal("body/version/reply/owner identity changed")
			}
			if len(messages[0].(map[string]any)["attachments"].([]any)) != 0 || messages[2].(map[string]any)["attachments"] != nil || len(answer["attachments"].([]any)) != 1 {
				t.Fatal("known empty, known attached and unknown observations confused")
			}
			starterGET := false
			for _, path := range f.paths {
				starterGET = starterGET || path == "GET /channels/100/messages/100"
			}
			if starterGET != tc.starter || (p["starter"] != nil) != tc.starter {
				t.Fatal("invented or missing starter", f.paths)
			}
			for _, excluded := range []string{"foreign human", "foreign bot", "synthetic-token"} {
				if bytes.Contains(raw, []byte(excluded)) || strings.Contains(out.String(), excluded) {
					t.Fatal("private/third-party data escaped")
				}
			}
			if strings.Contains(out.String(), "owner answer") {
				t.Fatal("body printed to stdout")
			}
		})
	}
}

// Positive controls above use the same loader and HTTP fixture as the denials.
// Each permission/routing denial must precede every message-body GET.
func TestHistoryReadChannelDenialsBeforeBodies(t *testing.T) {
	for _, mode := range []string{"unknown_permissions", "missing_role", "view", "history", "voice_connect", "stage_connect", "private_nonmember", "private_403", "foreign_guild", "foreign_parent_guild", "wrong_parent_id", "wrong_parent_type", "private_forum", "announcement_text_parent", "unsupported_channel", "identity", "roles_403"} {
		t.Run(mode, func(t *testing.T) {
			kind, parent := 11, 15
			if strings.HasPrefix(mode, "private_") {
				kind, parent = 12, 0
			}
			if mode == "voice_connect" {
				kind = 2
			}
			if mode == "stage_connect" {
				kind = 13
			}
			if mode == "announcement_text_parent" {
				kind, parent = 10, 0
			}
			f := historyChannelFixture(t, kind, parent)
			c := f.responses["/channels/100"].(map[string]any)
			p := f.responses["/channels/200"].(map[string]any)
			roles := "/guilds/" + monthlyTestGuild + "/roles"
			switch mode {
			case "unknown_permissions":
				delete(p, "permission_overwrites")
			case "missing_role":
				f.responses[roles] = []any{}
			case "view":
				f.responses[roles] = []any{map[string]any{"id": monthlyTestGuild, "permissions": strconv.FormatUint(monthlyHistory, 10)}}
			case "history":
				p["permission_overwrites"] = []any{map[string]any{"id": monthlyTestBot, "type": 1, "allow": "0", "deny": strconv.FormatUint(monthlyHistory, 10)}}
			case "voice_connect", "stage_connect":
				f.responses[roles] = []any{map[string]any{"id": monthlyTestGuild, "permissions": "66560"}}
			case "private_nonmember":
				f.responses["/channels/100/thread-members/"+monthlyTestBot] = map[string]any{"id": "100", "user_id": "999"}
			case "private_403":
				f.status["/channels/100/thread-members/"+monthlyTestBot] = 403
			case "foreign_guild":
				c["guild_id"] = "999"
			case "foreign_parent_guild":
				p["guild_id"] = "999"
			case "wrong_parent_id":
				p["id"] = "201"
			case "wrong_parent_type":
				p["type"] = 2
			case "private_forum":
				p["type"] = 15
			case "unsupported_channel":
				c["type"] = 15
			case "identity":
				f.responses["/users/@me"] = map[string]any{"id": "999", "bot": true}
			case "roles_403":
				f.status[roles] = 403
			}
			args, output := historyChannelArgs(t)
			var out bytes.Buffer
			if historyReadCLIWith(args, &out, f.load, monthlyPermit) == 0 {
				t.Fatal("denied source accepted")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("denial published a receipt")
			}
			for _, path := range f.paths {
				if strings.Contains(path, "/messages") {
					t.Fatal("body GET preceded qualification", f.paths)
				}
			}
		})
	}
}

func TestHistoryReadAdditionalContinuation(t *testing.T) {
	f := historyChannelFixture(t, 0, 0)
	args, first := historyChannelArgs(t)
	var out bytes.Buffer
	if historyReadCLIWith(args, &out, f.load, monthlyPermit) != 0 {
		t.Fatal("first channel page failed")
	}
	raw, _ := os.ReadFile(first)
	var original map[string]any
	_ = json.Unmarshal(raw, &original)
	for _, mode := range []string{"valid", "type", "supported_type", "starter", "parent", "next", "scope", "bot"} {
		t.Run(mode, func(t *testing.T) {
			var prior map[string]any
			_ = json.Unmarshal(raw, &prior)
			switch mode {
			case "type":
				prior["channel_type"] = 15
			case "supported_type":
				prior["channel_type"] = 2
			case "starter":
				prior["starter"] = original["messages"].([]any)[0]
			case "parent":
				prior["parent_id"] = "200"
			case "next":
				prior["next"].(map[string]any)["before"] = "899"
			case "scope":
				prior["scope_sha256"] = "changed"
			case "bot":
				prior["bot_id"] = "999"
			}
			b, _ := json.Marshal(prior)
			cursor := filepath.Join(filepath.Dir(first), mode+"-cursor.private.json")
			if err := os.WriteFile(cursor, b, 0600); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(filepath.Dir(first), mode+"-next.private.json")
			f.responses["/channels/100/messages"] = []any{}
			calls := len(f.paths)
			code := historyReadCLIWith([]string{"--scope", args[1], "--output", output, "--cursor", cursor}, &out, f.load, monthlyPermit)
			if mode == "valid" {
				if code != 0 {
					t.Fatal("valid channel continuation failed", out.String())
				}
				p, _ := monthlyTestReceipt(t, output)
				if p["complete"] != true || p["starter"] != nil {
					t.Fatal("empty terminal page changed")
				}
			} else {
				if code == 0 {
					t.Fatal("tampered continuation accepted")
				}
				for _, path := range f.paths[calls:] {
					if strings.Contains(path, "/messages") {
						t.Fatal("tampered continuation reached body GET")
					}
				}
			}
		})
	}
}

func TestHistoryReadAdditionalAPIFailuresAndIdentity(t *testing.T) {
	for _, mode := range []string{"empty", "history_403", "starter_403", "foreign_starter", "owner_as_bot", "configured_bot_as_human"} {
		t.Run(mode, func(t *testing.T) {
			kind, parent := 0, 0
			if mode == "starter_403" || mode == "foreign_starter" {
				kind, parent = 11, 15
			}
			f := historyChannelFixture(t, kind, parent)
			switch mode {
			case "empty":
				f.responses["/channels/100/messages"] = []any{}
			case "history_403":
				f.status["/channels/100/messages"] = 403
			case "starter_403":
				f.status["/channels/100/messages/100"] = 403
			case "foreign_starter":
				m := f.responses["/channels/100/messages/100"].(map[string]any)
				m["author"] = map[string]any{"id": "997", "bot": false}
				m["content"] = "authorization: excluded third party"
			case "owner_as_bot":
				f.responses["/channels/100/messages"].([]any)[1].(map[string]any)["author"].(map[string]any)["bot"] = true
			case "configured_bot_as_human":
				f.responses["/channels/100/messages"].([]any)[0].(map[string]any)["author"].(map[string]any)["bot"] = false
			}
			args, output := historyChannelArgs(t)
			var out bytes.Buffer
			code := historyReadCLIWith(args, &out, f.load, monthlyPermit)
			if mode == "empty" || mode == "foreign_starter" {
				if code != 0 {
					t.Fatal("qualified read rejected", out.String())
				}
				p, raw := monthlyTestReceipt(t, output)
				if p["starter"] != nil || bytes.Contains(raw, []byte("excluded third party")) {
					t.Fatal("third-party or invented starter persisted")
				}
				if mode == "empty" && (p["complete"] != true || p["scanned_messages"] != float64(0)) {
					t.Fatal("qualified empty page did not finish")
				}
			} else {
				if code == 0 {
					t.Fatal("API denial/spoof accepted as success")
				}
				if _, err := os.Stat(output); !os.IsNotExist(err) {
					t.Fatal("failure published a success receipt")
				}
			}
		})
	}
}

func TestHistoryReadAdditionalScopeBoundsBeforeLoad(t *testing.T) {
	for _, mode := range []string{"missing_since", "long_window", "channel_limit", "duplicate", "raw_scope_changed"} {
		t.Run(mode, func(t *testing.T) {
			f := historyChannelFixture(t, 0, 0)
			args, output := historyChannelArgs(t)
			raw, _ := os.ReadFile(args[1])
			var s map[string]any
			_ = json.Unmarshal(raw, &s)
			switch mode {
			case "missing_since":
				delete(s, "since")
			case "long_window":
				s["since"] = "2026-09-01T00:00:00Z"
			case "channel_limit":
				s["thread_ids"] = []string{"100", "101", "102", "103", "104", "105", "106", "107", "108", "109", "110"}
			case "duplicate":
				s["thread_ids"] = []string{"100", "100"}
			case "raw_scope_changed":
				var out bytes.Buffer
				if historyReadCLIWith(args, &out, f.load, monthlyPermit) != 0 {
					t.Fatal("raw scope control failed")
				}
				args = append(args, "--cursor", output)
				args[3] = filepath.Join(filepath.Dir(output), "raw-changed.private.json")
			}
			b, _ := json.MarshalIndent(s, "", "  ")
			if err := os.WriteFile(args[1], b, 0600); err != nil {
				t.Fatal(err)
			}
			calls, loads := len(f.paths), f.loads
			var out bytes.Buffer
			if historyReadCLIWith(args, &out, f.load, monthlyPermit) == 0 || f.loads != loads || len(f.paths) != calls {
				t.Fatal("invalid scope reached loader/API")
			}
		})
	}
}

func TestHistoryReadLegacyForumQualification(t *testing.T) {
	for _, parent := range []string{"40", requestForum} {
		for _, mode := range []string{"valid", "creator", "permissions"} {
			t.Run(parent+"/"+mode, func(t *testing.T) {
				load, paths := historyFixture(t, func(path string, value any) any {
					if path == "/channels/100" {
						m := value.(map[string]any)
						m["parent_id"] = parent
						if mode == "creator" {
							m["owner_id"] = "99"
						}
					}
					if path == "/guilds/10/roles" && mode == "permissions" {
						return []any{}
					}
					return value
				})
				args, output := historyArgs(t)
				var out bytes.Buffer
				code := historyReadCLIWith(args, &out, load, func(context.Context, string) bool { return true })
				if mode == "valid" {
					if code != 0 {
						t.Fatal("legacy forum rejected")
					}
					// A genuine v1 receipt has no new structural fields.
					p, _ := monthlyTestReceipt(t, output)
					delete(p, "channel_type")
					delete(p, "parent_id")
					b, _ := json.Marshal(p)
					if err := os.WriteFile(output, b, 0600); err != nil {
						t.Fatal(err)
					}
					next := filepath.Join(filepath.Dir(output), "legacy-next.private.json")
					if historyReadCLIWith([]string{"--scope", args[1], "--output", next, "--cursor", output}, &out, load, monthlyPermit) != 0 {
						t.Fatal("old v1 cursor rejected")
					}
				} else {
					if code == 0 {
						t.Fatal("legacy constraint relaxed")
					}
					for _, path := range *paths {
						if strings.Contains(path, "/messages") {
							t.Fatal("legacy denial read a body")
						}
					}
				}
			})
		}
	}
}
