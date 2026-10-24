package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func questionImageFixture(t *testing.T, mode string) ([]string, string, func(context.Context, historyReadScope) (*Runner, error), *int) {
	t.Helper()
	f := newMonthlyFixture(t)
	f.responses["/channels/100"] = map[string]any{"id": "100", "type": 11, "guild_id": monthlyTestGuild, "parent_id": "1554276307997692004", "owner_id": monthlyTestBot}
	message := func(id, author, body, stamp string) map[string]any {
		m := historyMessage(id, author, body)
		m["author"] = map[string]any{"id": author, "bot": author == monthlyTestBot}
		m["timestamp"] = stamp
		m["edited_timestamp"] = nil
		m["type"] = 0
		return m
	}
	f.responses["/channels/100/messages/100"] = message("100", monthlyTestBot, "original task", "2026-10-18T11:00:00Z")
	image, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==")
	q := message("900", monthlyTestBot, "original options", "2026-10-18T12:00:00Z")
	q["attachments"] = []any{map[string]any{"id": "51", "size": len(image), "content_type": "image/png", "url": "https://cdn.discordapp.com/attachments/100/51/fixture.png?hm=synthetic-token"}}
	reply := message("901", monthlyTestOwner, "a", "2026-10-18T12:02:00Z")
	reply["message_reference"] = map[string]any{"message_id": "900", "channel_id": "100", "guild_id": monthlyTestGuild}
	if mode == "foreign_actor" {
		q["author"] = map[string]any{"id": "998", "bot": true}
	}
	if mode == "spoof" {
		q["author"] = map[string]any{"id": monthlyTestBot, "bot": false}
	}
	if mode == "late_edit" {
		q["edited_timestamp"] = "2026-10-18T12:03:00Z"
	}
	if mode == "wrong_reply" {
		reply["author"] = map[string]any{"id": "998", "bot": false}
	}
	if mode == "different_question" {
		reply["message_reference"] = map[string]any{"message_id": "899", "channel_id": "100", "guild_id": monthlyTestGuild}
	}
	if mode == "unreferenced" {
		delete(reply, "message_reference")
	}
	f.responses["/channels/100/messages/900"], f.responses["/channels/100/messages/901"] = q, reply
	s := monthlyTestScope("discord-bot-question-image-read-scope-v1")
	s["thread_ids"] = []string{"100"}
	s["message_id"] = "900"
	s["attachment_id"] = "51"
	s["expected_content_sha256"] = bytesHash([]byte("original options"))
	s["expected_created_at"] = "2026-10-18T12:00:00Z"
	s["expected_edited_timestamp"] = ""
	s["expected_size"] = len(image)
	s["owner_reply_id"] = "901"
	s["expected_owner_reply_content_sha256"] = bytesHash([]byte("a"))
	s["expected_owner_reply_created_at"] = "2026-10-18T12:02:00Z"
	s["expected_owner_reply_edited_timestamp"] = ""
	if mode == "late_edit" {
		s["expected_edited_timestamp"] = "2026-10-18T12:03:00Z"
	}
	if mode == "owner_impersonation" {
		s["owner_id"] = monthlyTestBot
	}
	if mode == "foreign_todo" {
		s["todo_id"] = 3387
	}
	if mode == "missing_reply_version" {
		delete(s, "expected_owner_reply_content_sha256")
	}
	if mode == "wrong_question_version" {
		s["expected_content_sha256"] = strings.Repeat("a", 64)
	}
	args, path := monthlyTestArgs(t, s)
	path = filepath.Join(filepath.Dir(path), "question.private.image")
	args[3] = path
	media := new(int)
	load := func(ctx context.Context, scope historyReadScope) (*Runner, error) {
		r, e := f.load(ctx, scope)
		transport := r.Client.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		r.Client.Transport = attachmentTestTransport(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "cdn.discordapp.com" {
				return transport.RoundTrip(req)
			}
			*media++
			if req.Method != "GET" || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
				t.Error("CDN credentials leaked")
			}
			if mode == "changed_question" {
				q["content"] = "revised options"
			}
			if mode == "changed_reply" {
				reply["content"] = "b"
			}
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, ContentLength: int64(len(image)), Body: io.NopCloser(bytes.NewReader(image)), Request: req}, nil
		})
		return r, e
	}
	return args, path, load, media
}

func TestHistoryAttachmentExactBotQuestionAndReply(t *testing.T) {
	for _, mode := range []string{"ok", "unreferenced"} {
		t.Run(mode, func(t *testing.T) {
			args, path, load, media := questionImageFixture(t, mode)
			var out bytes.Buffer
			if historyAttachmentCLIWith(args, &out, load, monthlyPermit) != 0 {
				t.Fatal(out.String())
			}
			data, e := os.ReadFile(path)
			var receipt map[string]any
			if e != nil || len(data) == 0 || json.Unmarshal(out.Bytes(), &receipt) != nil || receipt["domain"] != "discord-bot-question-image-read-receipt-v1" || receipt["author_id"] != monthlyTestBot || receipt["owner_reply_id"] != "901" || receipt["image_sha256"] != bytesHash(data) || *media != 1 {
				t.Fatal("incorrect provenance", out.String())
			}
			for _, bad := range []string{"original options", "synthetic-token", "https://", "fixture.png"} {
				if strings.Contains(out.String(), bad) {
					t.Fatal("private source escaped")
				}
			}
		})
	}
}

func TestHistoryAttachmentBotQuestionRejectsActorsVersionsAndRecheck(t *testing.T) {
	for _, mode := range []string{"foreign_actor", "spoof", "late_edit", "wrong_reply", "different_question", "owner_impersonation", "foreign_todo", "missing_reply_version", "wrong_question_version", "changed_question", "changed_reply"} {
		t.Run(mode, func(t *testing.T) {
			args, path, load, media := questionImageFixture(t, mode)
			var out bytes.Buffer
			if historyAttachmentCLIWith(args, &out, load, monthlyPermit) == 0 {
				t.Fatal("invalid question accepted")
			}
			if _, e := os.Stat(path); !os.IsNotExist(e) {
				t.Fatal("unverified image published")
			}
			want := 0
			if strings.HasPrefix(mode, "changed_") {
				want = 1
			}
			if *media != want {
				t.Fatal("wrong request stage", out.String())
			}
		})
	}
}
