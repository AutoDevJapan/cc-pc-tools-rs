package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type attachmentTestTransport func(*http.Request) (*http.Response, error)

func (f attachmentTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type attachmentTestCalls struct {
	loads, api, media int
	badAuth           bool
	changed           bool
}

func attachmentFixture(t *testing.T, mode string) (func(context.Context, historyReadScope) (*Runner, error), *attachmentTestCalls, []byte) {
	t.Helper()
	image, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVQIHWP4z8DwHwAFgAI/ScLbtAAAAABJRU5ErkJggg==")
	if err != nil {
		t.Fatal(err)
	}
	calls := &attachmentTestCalls{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.api++
		if req.Method != "GET" || req.Header.Get("Authorization") != "Bot fixture-token" {
			calls.badAuth = true
		}
		var response any
		switch req.URL.Path {
		case "/users/@me":
			id := "30"
			if mode == "wrong_bot" {
				id = "31"
			}
			response = map[string]any{"id": id, "bot": true}
		case "/channels/100":
			thread := map[string]any{"id": "100", "guild_id": "10", "parent_id": "40", "owner_id": "30", "type": 11}
			if mode == "wrong_guild" {
				thread["guild_id"] = "11"
			}
			if mode == "wrong_forum" {
				thread["parent_id"] = "41"
			}
			response = thread
		case "/channels/100/messages/100":
			response = historyMessage("100", "30", "original question")
		case "/channels/100/messages/900":
			message := historyMessage("900", "20", "<@30>")
			message["edited_timestamp"] = nil
			message["type"] = 0
			if mode == "wrong_owner" {
				message["author"] = map[string]any{"id": "99", "bot": false}
			}
			if mode == "changed_body" || calls.changed {
				message["content"] = "different owner reply version"
			}
			if mode == "changed_edit" {
				message["edited_timestamp"] = "2026-10-18T12:02:00Z"
			}
			url := "https://cdn.discordapp.com/attachments/100/51/private-label.png?hm=fixture-token"
			switch mode {
			case "wrong_host":
				url = "https://cdn.discordapp.com.invalid/attachments/100/51/private-label.png"
			case "wrong_path":
				url = "https://cdn.discordapp.com/attachments/101/51/private-label.png"
			case "http_url":
				url = "http://cdn.discordapp.com/attachments/100/51/private-label.png"
			case "userinfo":
				url = "https://user@cdn.discordapp.com/attachments/100/51/private-label.png"
			}
			attachment := map[string]any{"id": "51", "size": len(image), "content_type": "image/png", "filename": "private-label.png", "url": url}
			if mode == "wrong_attachment" {
				attachment["id"] = "52"
			}
			message["attachments"] = []any{attachment}
			if mode == "duplicate_attachment" {
				message["attachments"] = []any{attachment, attachment}
			}
			response = message
		default:
			http.NotFound(w, req)
			return
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)
	return func(_ context.Context, scope historyReadScope) (*Runner, error) {
		calls.loads++
		client := server.Client()
		transport := client.Transport
		if transport == nil {
			transport = http.DefaultTransport
		}
		client.Transport = attachmentTestTransport(func(req *http.Request) (*http.Response, error) {
			if req.URL.Hostname() != "cdn.discordapp.com" {
				return transport.RoundTrip(req)
			}
			calls.media++
			if req.Method != "GET" || req.Header.Get("Authorization") != "" || req.Header.Get("Cookie") != "" {
				calls.badAuth = true
			}
			body := image
			status := http.StatusOK
			header := http.Header{"Content-Type": {"image/png"}}
			switch mode {
			case "redirect":
				status = http.StatusFound
				header.Set("Location", "https://cdn.discordapp.com/attachments/100/51/redirected.png")
			case "short":
				body = image[:len(image)-1]
			case "oversize":
				body = append(append([]byte{}, image...), 0)
			case "not_image":
				body = bytes.Repeat([]byte("x"), len(image))
			case "changed_after_download":
				calls.changed = true
			}
			return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req}, nil
		})
		return &Runner{State: &State{Destination: Destination{Guild: "10", Owner: "20", Bot: "30", Forum: "40"}}, Client: client, Base: server.URL, Token: "fixture-token", Now: func() time.Time { return time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC) }}, nil
	}, calls, image
}

func attachmentArgs(t *testing.T, size int) ([]string, string) {
	t.Helper()
	args, _ := historyArgs(t)
	raw, err := os.ReadFile(args[1])
	if err != nil {
		t.Fatal(err)
	}
	var scope map[string]any
	if err := json.Unmarshal(raw, &scope); err != nil {
		t.Fatal(err)
	}
	scope["domain"] = "discord-owner-image-read-scope-v1"
	scope["message_id"] = "900"
	scope["attachment_id"] = "51"
	scope["expected_content_sha256"] = bytesHash([]byte("<@30>"))
	scope["expected_created_at"] = "2026-10-18T12:00:00Z"
	scope["expected_edited_timestamp"] = ""
	scope["expected_size"] = size
	raw, _ = json.Marshal(scope)
	if err := os.WriteFile(args[1], raw, 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(filepath.Dir(args[1]), "owner-image.private.png")
	return []string{"--scope", args[1], "--output", output}, output
}

func TestHistoryAttachmentReadsExactOwnerImageWithoutLeakingURLOrAuth(t *testing.T) {
	load, calls, expected := attachmentFixture(t, "ok")
	args, output := attachmentArgs(t, len(expected))
	var stdout bytes.Buffer
	if code := historyAttachmentCLIWith(args, &stdout, load, func(context.Context, string) bool { return true }); code != 0 {
		t.Fatalf("exact owner image unavailable: %d %s", code, stdout.String())
	}
	actual, err := os.ReadFile(output)
	if err != nil || !bytes.Equal(actual, expected) {
		t.Fatal("downloaded image was missing or changed")
	}
	var receipt map[string]any
	if json.Unmarshal(stdout.Bytes(), &receipt) != nil || receipt["message_id"] != "900" || receipt["attachment_id"] != "51" || receipt["image_sha256"] != bytesHash(actual) || receipt["content_sha256"] != bytesHash([]byte("<@30>")) || receipt["media_type"] != "image/png" {
		t.Fatal("image provenance missing or different")
	}
	for _, forbidden := range []string{"fixture-token", "private-label", "https://", "<@30>"} {
		if strings.Contains(stdout.String(), forbidden) || bytes.Contains(actual, []byte(forbidden)) {
			t.Fatal("source text, URL or credential escaped")
		}
	}
	if calls.badAuth || calls.loads != 1 || calls.api != 5 || calls.media != 1 {
		t.Fatalf("unexpected requests or CDN authentication: %+v", calls)
	}
}

func TestHistoryAttachmentRejectsChangedOrUnqualifiedSource(t *testing.T) {
	for _, mode := range []string{"wrong_bot", "wrong_guild", "wrong_forum", "wrong_owner", "changed_body", "changed_edit", "wrong_attachment", "duplicate_attachment", "wrong_host", "wrong_path", "http_url", "userinfo", "redirect", "short", "oversize", "not_image", "changed_after_download"} {
		t.Run(mode, func(t *testing.T) {
			load, calls, image := attachmentFixture(t, mode)
			args, output := attachmentArgs(t, len(image))
			var stdout bytes.Buffer
			if historyAttachmentCLIWith(args, &stdout, load, func(context.Context, string) bool { return true }) == 0 {
				t.Fatal("unqualified image source accepted")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatal("failed source published an image")
			}
			if calls.badAuth || strings.Contains(stdout.String(), "fixture-token") || strings.Contains(stdout.String(), "https://") {
				t.Fatal("rejection leaked credential or URL")
			}
			if mode == "redirect" && calls.media != 1 {
				t.Fatalf("redirect scenario issued %d CDN requests, want one without following", calls.media)
			}
		})
	}
}

func TestHistoryAttachmentRejectsInputBeforeLoading(t *testing.T) {
	for _, mode := range []string{"guard", "collision", "wrong_extension", "duplicate_flag", "missing_hash", "multi_thread", "excess_size"} {
		t.Run(mode, func(t *testing.T) {
			load, calls, image := attachmentFixture(t, "ok")
			args, output := attachmentArgs(t, len(image))
			if mode == "collision" {
				if err := os.WriteFile(output, []byte("existing owner image"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "wrong_extension" {
				args[3] = filepath.Join(filepath.Dir(output), "public.png")
			}
			if mode == "duplicate_flag" {
				args = append(args, "--scope", args[1])
			}
			if mode == "missing_hash" || mode == "multi_thread" || mode == "excess_size" {
				raw, _ := os.ReadFile(args[1])
				var scope map[string]any
				_ = json.Unmarshal(raw, &scope)
				switch mode {
				case "missing_hash":
					delete(scope, "expected_content_sha256")
				case "multi_thread":
					scope["thread_ids"] = []string{"100", "101"}
				case "excess_size":
					scope["expected_size"] = 21 << 20
				}
				raw, _ = json.Marshal(scope)
				if err := os.WriteFile(args[1], raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			var stdout bytes.Buffer
			if historyAttachmentCLIWith(args, &stdout, load, func(context.Context, string) bool { return mode != "guard" }) == 0 || calls.loads != 0 || calls.api != 0 || calls.media != 0 {
				t.Fatal("rejected input reached credential loading or HTTP")
			}
			if mode == "collision" {
				actual, _ := os.ReadFile(output)
				if string(actual) != "existing owner image" {
					t.Fatal("existing owner image replaced")
				}
			}
		})
	}
}
