package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type ownerAckFixture struct {
	r            *Runner
	source       Message
	posted       []Message
	posts        int
	lostResponse bool
	foreignGuild bool
}

func newOwnerAckFixture(t *testing.T) *ownerAckFixture {
	t.Helper()
	r, _ := fixture(t)
	cutoff, _ := time.Parse(time.RFC3339Nano, ownerAcknowledgementSince)
	now := cutoff.Add(time.Minute)
	r.Now = func() time.Time { return now }
	r.OwnerAcknowledgementsActive = true
	f := &ownerAckFixture{r: r, source: msg(cutoff.Add(time.Second), "500", "300", "新しい依頼です。")}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if q.Header.Get("Authorization") != "Bot fixture-token" {
			t.Error("missing existing bot authentication")
		}
		switch {
		case q.Method == "GET" && q.URL.Path == "/channels/500":
			guild := "100"
			if f.foreignGuild {
				guild = "999"
			}
			respond(w, Thread{ID: "500", Guild: guild, Type: 11})
		case q.Method == "GET" && q.URL.Path == "/channels/500/messages/"+f.source.ID:
			respond(w, f.source)
		case q.Method == "GET" && q.URL.Path == "/channels/500/messages":
			respond(w, append(append([]Message(nil), f.posted...), f.source))
		case q.Method == "GET" && strings.HasPrefix(q.URL.Path, "/channels/500/messages/"):
			for _, m := range f.posted {
				if strings.HasSuffix(q.URL.Path, "/"+m.ID) {
					respond(w, m)
					return
				}
			}
			w.WriteHeader(404)
		case q.Method == "POST" && q.URL.Path == "/channels/500/messages":
			f.posts++
			var body map[string]any
			if json.NewDecoder(q.Body).Decode(&body) != nil {
				t.Error("invalid POST")
			}
			mentions, _ := body["allowed_mentions"].(map[string]any)
			if body["content"] != "受け取りました。" || body["flags"] != float64(4100) || body["enforce_nonce"] != true || body["tts"] != false || mentions["replied_user"] != false {
				t.Errorf("unexpected owner notification or text: %#v", body)
			}
			for _, name := range []string{"parse", "users", "roles"} {
				if values, ok := mentions[name].([]any); !ok || len(values) != 0 {
					t.Error("mention enabled", name)
				}
			}
			ref, _ := body["message_reference"].(map[string]any)
			if ref["message_id"] != f.source.ID || ref["channel_id"] != "500" || ref["guild_id"] != "100" {
				t.Error("not the original owner thread")
			}
			m := msg(now.Add(time.Duration(f.posts)*time.Second), "500", "400", "受け取りました。")
			m.Author.Bot = true
			m.Flags = 4100
			m.Reference.Message = f.source.ID
			m.Reference.Channel = "500"
			f.posted = append(f.posted, m)
			if f.lostResponse {
				w.WriteHeader(502)
				return
			}
			respond(w, m)
		default:
			t.Errorf("unexpected route %s %s", q.Method, q.URL.Path)
			w.WriteHeader(404)
		}
	})
	return f
}

func (f *ownerAckFixture) queue(t *testing.T) {
	t.Helper()
	if err := f.r.queueOwnerAcknowledgement(f.source); err != nil {
		t.Fatal(err)
	}
}
func (f *ownerAckFixture) tick() Health {
	f.r.OwnerAcknowledgementAttempts = 0
	h := Health{}
	f.r.processOwnerAcknowledgements(context.Background(), &h)
	return h
}

func TestOwnerAcknowledgementOneReplySurvivesReloadAndTwoCapturePaths(t *testing.T) {
	f := newOwnerAckFixture(t)
	f.queue(t)
	f.queue(t)
	h := f.tick()
	if f.posts != 1 || h.OwnerAcknowledgements != 1 || len(h.Errors) != 0 {
		t.Fatal("first receipt not verified", f.posts, h)
	}
	s, err := f.r.loadOwnerAcknowledgements()
	if err != nil || len(s.Receipts) != 1 {
		t.Fatal("duplicate receipt versions", err)
	}
	for _, a := range s.Receipts {
		if a.Reply == "" || a.Verified == "" || a.BodySHA != bytesHash([]byte(f.source.Content)) || a.Edited != "" {
			t.Fatal("identity/hash/reply proof missing", a)
		}
	}
	clone := *f.r
	f.r = &clone
	f.queue(t)
	f.tick()
	if f.posts != 1 {
		t.Fatal("same version was posted after restart")
	}
	b, err := os.ReadFile(f.r.ownerAcknowledgementPath())
	if err != nil || strings.Contains(string(b), f.source.Content) || strings.Contains(string(b), "fixture-token") {
		t.Fatal("source body or credentials were stored")
	}
}

func TestOwnerAcknowledgementNewEditGetsOneNewReceipt(t *testing.T) {
	f := newOwnerAckFixture(t)
	f.queue(t)
	f.tick()
	edited := f.r.Now().Add(10 * time.Second)
	f.r.Now = func() time.Time { return edited.Add(time.Minute) }
	f.source.Edited = edited.Format(time.RFC3339Nano)
	f.source.Content = "依頼を編集しました。"
	f.queue(t)
	f.tick()
	f.queue(t)
	f.tick()
	s, err := f.r.loadOwnerAcknowledgements()
	if err != nil || f.posts != 2 || len(s.Receipts) != 2 {
		t.Fatal("edit version conflated or repeated", f.posts, s, err)
	}
}

func TestOwnerAcknowledgementExistingReceiptSuppressesNewPost(t *testing.T) {
	f := newOwnerAckFixture(t)
	m := msg(f.r.Now(), "500", "400", "【PCの道具】受け取りました。今の作業に反映します。")
	m.Author.Bot = true
	m.Reference.Message = f.source.ID
	f.posted = []Message{m}
	f.queue(t)
	f.tick()
	s, _ := f.r.loadOwnerAcknowledgements()
	if f.posts != 0 {
		t.Fatal("existing acknowledgement duplicated")
	}
	for _, a := range s.Receipts {
		if a.Reply != m.ID || !a.Reused || a.Verified == "" {
			t.Fatal("existing receipt not linked", a)
		}
	}
}

func TestOwnerAcknowledgementLostResponseIsGetOnlyUntilFound(t *testing.T) {
	f := newOwnerAckFixture(t)
	f.lostResponse = true
	f.queue(t)
	if h := f.tick(); len(h.Errors) == 0 || f.posts != 1 {
		t.Fatal("lost response falsely verified", h, f.posts)
	}
	h := f.tick()
	if f.posts != 1 || h.OwnerAcknowledgements != 1 || len(h.Errors) != 0 {
		t.Fatal("lost response caused another POST", h, f.posts)
	}
}

func TestOwnerAcknowledgementUncertainMissingPostNeverResubmits(t *testing.T) {
	f := newOwnerAckFixture(t)
	f.lostResponse = true
	f.queue(t)
	f.tick()
	f.posted = nil
	for range 2 {
		if h := f.tick(); len(h.Errors) == 0 {
			t.Fatal("unconfirmed delivery marked complete")
		}
	}
	if f.posts != 1 {
		t.Fatal("uncertain POST repeated")
	}
}

func TestOwnerAcknowledgementDoesNotBackfillOrAcknowledgeOtherAuthors(t *testing.T) {
	for _, kind := range []string{"historical", "other_author", "bot", "inactive"} {
		t.Run(kind, func(t *testing.T) {
			f := newOwnerAckFixture(t)
			switch kind {
			case "historical":
				old, _ := time.Parse(time.RFC3339Nano, ownerAcknowledgementSince)
				f.source.Timestamp = old.Add(-time.Second).Format(time.RFC3339Nano)
			case "other_author":
				f.source.Author.ID = "999"
			case "bot":
				f.source.Author.Bot = true
			case "inactive":
				f.r.OwnerAcknowledgementsActive = false
			}
			f.queue(t)
			f.tick()
			if f.posts != 0 {
				t.Fatal("out-of-scope acknowledgement")
			}
		})
	}
}

func TestOwnerAcknowledgementStaleVersionAndForeignGuildNeverPost(t *testing.T) {
	for _, kind := range []string{"source_edit", "foreign_guild"} {
		t.Run(kind, func(t *testing.T) {
			f := newOwnerAckFixture(t)
			f.queue(t)
			if kind == "source_edit" {
				f.source.Content = "受信後に変わった本文"
			} else {
				f.foreignGuild = true
			}
			f.tick()
			if f.posts != 0 {
				t.Fatal("unverified source was posted")
			}
		})
	}
}

func TestOwnerAcknowledgementCorruptJournalIsNotReplaced(t *testing.T) {
	f := newOwnerAckFixture(t)
	if err := os.WriteFile(f.r.ownerAcknowledgementPath(), []byte("{broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if f.r.queueOwnerAcknowledgement(f.source) == nil {
		t.Fatal("invalid existing journal ignored")
	}
	f.tick()
	b, _ := os.ReadFile(f.r.ownerAcknowledgementPath())
	if f.posts != 0 || string(b) != "{broken" {
		t.Fatal("corrupt journal reset or posted")
	}
}

func TestOwnerAcknowledgementScanQueuesBeforeTaskAdmission(t *testing.T) {
	f := newOwnerAckFixture(t)
	if n, err := f.r.scan(context.Background(), Thread{ID: "500", Guild: "100"}); err != nil || n != 1 {
		t.Fatal("new source was not captured", n, err)
	}
	if f.posts != 1 || f.r.State.Requests[f.source.ID].Todo != 0 {
		t.Fatal("receipt waited for task registration", f.posts)
	}
	if n, err := f.r.scan(context.Background(), Thread{ID: "500", Guild: "100"}); err != nil || n != 0 {
		t.Fatal("repeat observation failed", n, err)
	}
	if f.posts != 1 {
		t.Fatal("repeat scan duplicated receipt")
	}
}
