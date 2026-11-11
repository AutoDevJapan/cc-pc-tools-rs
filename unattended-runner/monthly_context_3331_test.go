package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMonthlyContextKeepsConfiguredConversationAndOwnerCounts(t *testing.T) {
	for _, tc := range []struct{ kind, parent int }{{0, 0}, {11, 15}, {11, 0}, {12, 0}} {
		f := historyChannelFixture(t, tc.kind, tc.parent)
		s := monthlyTestScope("discord-monthly-history-read-scope-v1")
		s["channel_ids"], s["include_bot_context"] = []string{"100"}, true
		args, path := monthlyTestArgs(t, s)
		var out bytes.Buffer
		if monthlyReadCLIWith(args, &out, f.load, monthlyPermit) != 0 {
			t.Fatal(out.String())
		}
		p, raw := monthlyTestReceipt(t, path)
		ms := p["messages"].([]any)
		if len(ms) != 3 || ms[0].(map[string]any)["id"] != "905" || ms[1].(map[string]any)["id"] != "904" || ms[2].(map[string]any)["id"] != "901" {
			t.Fatal("lost context")
		}
		owner := ms[1].(map[string]any)
		if owner["type"] != float64(19) || owner["edited_timestamp"] != "2026-10-18T12:01:00Z" || owner["message_reference"].(map[string]any)["message_id"] != "100" {
			t.Fatal("version/reference changed")
		}
		if (p["starter"] != nil) != (tc.parent == 15) || p["include_bot_context"] != true {
			t.Fatal("starter/selection lost")
		}
		var summary map[string]any
		if json.Unmarshal(out.Bytes(), &summary) != nil || summary["owner_messages"] != float64(1) || summary["bot_messages"] != float64(2) {
			t.Fatal("wrong actor counts", out.String())
		}
		for _, bad := range []string{"foreign human", "foreign bot", "synthetic-token"} {
			if bytes.Contains(raw, []byte(bad)) || strings.Contains(out.String(), bad) {
				t.Fatal("foreign body escaped")
			}
		}
		// A failed continuation must neither alter the prior receipt nor publish.
		old := append([]byte(nil), raw...)
		f.status["/channels/100/messages"] = 403
		next := filepath.Join(filepath.Dir(path), "next.private.json")
		out.Reset()
		if monthlyReadCLIWith([]string{"--scope", args[1], "--output", next, "--cursor", path}, &out, f.load, monthlyPermit) == 0 {
			t.Fatal("failed page accepted")
		}
		b, _ := os.ReadFile(path)
		if !bytes.Equal(b, old) {
			t.Fatal("prior changed")
		}
		if _, e := os.Stat(next); !os.IsNotExist(e) {
			t.Fatal("failed page published")
		}
	}
}

func TestMonthlyContextRefusesForeignBotSpoofAndSecret(t *testing.T) {
	for _, mode := range []string{"spoof", "secret", "configuration", "guard"} {
		t.Run(mode, func(t *testing.T) {
			f := historyChannelFixture(t, 0, 0)
			ms := f.responses["/channels/100/messages"].([]any)
			bot := ms[0].(map[string]any)
			if mode == "spoof" {
				bot["author"] = map[string]any{"id": monthlyTestBot, "bot": false}
			}
			if mode == "secret" {
				bot["content"] = "synthetic-token"
			}
			load := f.load
			if mode == "configuration" {
				load = func(ctx context.Context, s historyReadScope) (*Runner, error) {
					r, e := f.load(ctx, s)
					r.State.Destination.Bot = "998"
					return r, e
				}
			}
			s := monthlyTestScope("discord-monthly-history-read-scope-v1")
			s["channel_ids"] = []string{"100"}
			s["include_bot_context"] = true
			args, path := monthlyTestArgs(t, s)
			guard := monthlyPermit
			if mode == "guard" {
				guard = func(context.Context, string) bool { return false }
			}
			var out bytes.Buffer
			if monthlyReadCLIWith(args, &out, load, guard) == 0 {
				t.Fatal("invalid context accepted")
			}
			if _, e := os.Stat(path); !os.IsNotExist(e) {
				t.Fatal("invalid page published")
			}
		})
	}
}
