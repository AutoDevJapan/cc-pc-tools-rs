package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type ownerAckFixture struct {
	r                   *Runner
	source              Message
	posted              []Message
	posts               int
	lostResponse        bool
	foreignGuild        bool
	sourceType          *int
	postStatus          int
	postsWithoutAttempt int
	locked              bool
	redirect            string
	discoveryFailure    bool
	botIdentity         string
	channelOverride     *Thread
	historyOverride     *[]Message
}

func newOwnerAckFixture(t *testing.T) *ownerAckFixture {
	t.Helper()
	r, _ := fixture(t)
	cutoff, _ := time.Parse(time.RFC3339Nano, ownerAcknowledgementSince)
	now := cutoff.Add(time.Minute)
	r.Now = func() time.Time { return now }
	r.OwnerAcknowledgementsActive = true
	return attachOwnerAckFixture(t, r, msg(cutoff.Add(time.Second), "500", "300", "新しい依頼です。"))
}

func attachOwnerAckFixture(t *testing.T, r *Runner, source Message) *ownerAckFixture {
	t.Helper()
	typ := 0
	f := &ownerAckFixture{r: r, source: source, sourceType: &typ}
	r.State.Threads[source.Channel] = Thread{ID: source.Channel, Guild: "100", Parent: requestForum, Owner: "300", Type: 11}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if q.Header.Get("Authorization") != "Bot fixture-token" {
			t.Error("missing existing bot authentication")
		}
		switch {
		case q.Method == "GET" && q.URL.Path == "/users/@me":
			id := "400"
			if f.botIdentity != "" {
				id = f.botIdentity
			}
			respond(w, map[string]any{"id": id, "bot": true})
		case f.discoveryFailure && q.Method == "GET" && (q.URL.Path == "/channels/200" || q.URL.Path == "/channels/"+requestForum):
			w.WriteHeader(503)
		case q.Method == "GET" && q.URL.Path == "/channels/"+f.source.Channel:
			guild := "100"
			if f.foreignGuild {
				guild = "999"
			}
			channel := r.State.Threads[f.source.Channel]
			if f.channelOverride != nil {
				channel = *f.channelOverride
			}
			channel.Guild = guild
			channel.Metadata.Locked = f.locked
			respond(w, channel)
		case q.Method == "GET" && q.URL.Path == "/channels/"+f.source.Channel+"/messages/"+f.source.ID:
			respond(w, historyReadMessage{Message: f.source, Type: f.sourceType})
		case q.Method == "GET" && q.URL.Path == "/channels/"+f.source.Channel+"/messages":
			if f.historyOverride != nil {
				respond(w, *f.historyOverride)
				return
			}
			respond(w, append(append([]Message(nil), f.posted...), f.source))
		case q.Method == "GET" && strings.HasPrefix(q.URL.Path, "/channels/"+f.source.Channel+"/messages/"):
			for _, m := range f.posted {
				if strings.HasSuffix(q.URL.Path, "/"+m.ID) {
					respond(w, m)
					return
				}
			}
			w.WriteHeader(404)
		case q.Method == "POST" && q.URL.Path == "/channels/"+f.source.Channel+"/messages":
			f.posts++
			saved, err := r.loadOwnerAcknowledgements()
			pending := false
			if err == nil {
				for _, a := range saved.Receipts {
					if a.Message == f.source.ID && a.Attempted != "" {
						pending = true
					}
				}
			}
			if !pending {
				f.postsWithoutAttempt++
			}
			if f.redirect != "" {
				http.Redirect(w, q, f.redirect, http.StatusTemporaryRedirect)
				return
			}
			if f.postStatus != 0 {
				w.WriteHeader(f.postStatus)
				if f.postStatus == 429 {
					_, _ = w.Write([]byte(`{"retry_after":2}`))
				}
				return
			}
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
			if ref["message_id"] != f.source.ID || ref["channel_id"] != f.source.Channel || ref["guild_id"] != "100" {
				t.Error("not the original owner thread")
			}
			at := r.Now()
			created, _ := acknowledgementTime(f.source.Timestamp)
			edited, _ := acknowledgementTime(f.source.Edited)
			if created.After(at) {
				at = created
			}
			if edited.After(at) {
				at = edited
			}
			m := msg(at.Add(time.Duration(f.posts)*time.Second), f.source.Channel, "400", "受け取りました。")
			m.Author.Bot = true
			m.Flags = 4100
			m.Reference.Message = f.source.ID
			m.Reference.Channel = f.source.Channel
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
	f.r.ownerAcknowledgementCache = nil
	f.r.ownerAcknowledgementLoaded = false
	f.r.ownerAcknowledgementLoadError = nil
	f.r.ownerAcknowledgementBotVerified = false
	h := Health{}
	f.r.processOwnerAcknowledgements(context.Background(), &h)
	return h
}

func TestOwnerAcknowledgementOneReplySurvivesReloadAndTwoCapturePaths(t *testing.T) {
	f := newOwnerAckFixture(t)
	f.queue(t)
	f.queue(t)
	h := f.tick()
	if f.posts != 1 || f.postsWithoutAttempt != 0 || h.OwnerAcknowledgements != 1 || len(h.Errors) != 0 {
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
	now := f.r.Now().Add(time.Minute)
	f.r.Now = func() time.Time { return now }
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
		now := f.r.Now().Add(6 * time.Minute)
		f.r.Now = func() time.Time { return now }
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
	if f.posts != 0 {
		t.Fatal("scan itself consumed the discovery deadline by posting")
	}
	f.tick()
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

func TestOwnerAcknowledgementRateLimitRetriesOnlyAfterDeclaredDelay(t *testing.T) {
	f := newOwnerAckFixture(t)
	f.postStatus = 429
	f.queue(t)
	f.tick()
	s, _ := f.r.loadOwnerAcknowledgements()
	for _, a := range s.Receipts {
		if a.Attempted != "" || a.NextTry == "" || a.Terminal != "" {
			t.Fatal("definite rate limit became uncertain", a)
		}
	}
	f.tick()
	if f.posts != 1 {
		t.Fatal("rate limit retried before delay")
	}
	now := f.r.Now().Add(3 * time.Second)
	f.r.Now = func() time.Time { return now }
	f.postStatus = 0
	h := f.tick()
	if f.posts != 2 || len(f.posted) != 1 || h.OwnerAcknowledgements != 1 {
		t.Fatal("rate-limited version did not recover exactly once", f.posts, h)
	}
}

func TestOwnerAcknowledgementDefiniteRejectionIsReportedOnce(t *testing.T) {
	f := newOwnerAckFixture(t)
	f.postStatus = 403
	f.queue(t)
	first := f.tick()
	second := f.tick()
	if f.posts != 1 || len(first.Errors) == 0 || len(second.Errors) != 0 {
		t.Fatal("definite refusal retried or spammed health", f.posts, first, second)
	}
	s, _ := f.r.loadOwnerAcknowledgements()
	for _, a := range s.Receipts {
		if a.Verified != "" || a.Terminal != "post_rejected_403" || !a.Reported {
			t.Fatal("refusal was not retained", a)
		}
	}
}

func TestOwnerAcknowledgementDoesNotFollowPostRedirect(t *testing.T) {
	f := newOwnerAckFixture(t)
	forwarded := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded++; w.WriteHeader(200) }))
	defer other.Close()
	f.redirect = other.URL
	f.queue(t)
	f.tick()
	if f.posts != 1 || forwarded != 0 {
		t.Fatal("authenticated POST was redirected", f.posts, forwarded)
	}
}

func TestOwnerAcknowledgementSmallClockSkewDoesNotLoseCapture(t *testing.T) {
	f := newOwnerAckFixture(t)
	f.source.Timestamp = f.r.Now().Add(10 * time.Second).Format(time.RFC3339Nano)
	f.queue(t)
	h := f.tick()
	if f.posts != 1 || h.OwnerAcknowledgements != 1 {
		t.Fatal("permitted clock skew lost acknowledgement", f.posts, h)
	}
}

func TestOwnerAcknowledgementQueueFailureDoesNotAdvanceIntoPermanentLoss(t *testing.T) {
	f := newOwnerAckFixture(t)
	f.r.ownerAcknowledgementLoaded = true
	f.r.ownerAcknowledgementLoadError = errors.New("fixture journal busy")
	n, err := f.r.scan(context.Background(), f.r.State.Threads["500"])
	if err != nil || n != 1 || f.r.State.Requests[f.source.ID] == nil || len(f.r.ownerAcknowledgementQueueErrors) != 1 {
		t.Fatal("saved source was classified as failed history", n, err)
	}
	f.r.ownerAcknowledgementLoaded = false
	f.r.ownerAcknowledgementLoadError = nil
	h := Health{}
	f.r.recoverOwnerAcknowledgementQueue(context.Background(), &h)
	f.tick()
	if f.posts != 1 {
		t.Fatal("durable capture was not recovered after unchanged watermark", h)
	}
}

func TestOwnerAcknowledgementStaleCaptureQueuesActualEditedVersion(t *testing.T) {
	f := newOwnerAckFixture(t)
	f.queue(t)
	f.source.Content = "受信後の現在版"
	f.tick()
	h := f.tick()
	if f.posts != 1 || h.OwnerAcknowledgements != 1 {
		t.Fatal("edit was dropped as permanently stale", f.posts, h)
	}
}

func TestOwnerAcknowledgementUnreplyableMessageTypesAreNotPosted(t *testing.T) {
	for _, kind := range []int{6, 7, 21} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			f := newOwnerAckFixture(t)
			f.sourceType = &kind
			f.queue(t)
			f.tick()
			f.tick()
			if f.posts != 0 {
				t.Fatal("system message acknowledged")
			}
		})
	}
}

func TestOwnerAcknowledgementLimitedBudgetDoesNotReserveUnsentPost(t *testing.T) {
	f := newOwnerAckFixture(t)
	f.queue(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	f.r.processOwnerAcknowledgements(ctx, &Health{})
	s, _ := f.r.loadOwnerAcknowledgements()
	for _, a := range s.Receipts {
		if a.Attempted != "" || a.Reply != "" {
			t.Fatal("insufficient time reserved a phantom send", a)
		}
	}
	if f.posts != 0 {
		t.Fatal("POST started without its finite verification allowance")
	}
}

func TestOwnerAcknowledgementVerifiedArchivesStillDeduplicate(t *testing.T) {
	f := newOwnerAckFixture(t)
	s, err := f.r.loadOwnerAcknowledgements()
	if err != nil {
		t.Fatal(err)
	}
	messages := map[string]Message{}
	for i := 0; i < ownerAcknowledgementRetained+3; i++ {
		m := f.source
		m.ID = fmt.Sprint(uint64(1566113376763904000) + uint64(i))
		m.Content = "保存済みの本人投稿"
		a := ownerAcknowledgement{Guild: "100", Thread: m.Channel, Message: m.ID, Author: m.Author.ID, Created: m.Timestamp, BodySHA: bytesHash([]byte(m.Content)), Queued: f.r.Now().Format(time.RFC3339Nano), Reply: "700", Verified: f.r.Now().Format(time.RFC3339Nano)}
		s.Receipts[a.version()] = a
		messages[a.version()] = m
	}
	if err := f.r.saveOwnerAcknowledgements(s); err != nil {
		t.Fatal(err)
	}
	archived, err := filepath.Glob(filepath.Join(f.r.Config.Home, "owner-acknowledgements", "*.json"))
	if err != nil || len(archived) != 3 {
		t.Fatal("verified receipts were not archived", len(archived), err)
	}
	key := strings.TrimSuffix(filepath.Base(archived[0]), ".json")
	if err := f.r.queueOwnerAcknowledgement(messages[key]); err != nil {
		t.Fatal(err)
	}
	s, err = f.r.loadOwnerAcknowledgements()
	if err != nil || len(s.Receipts) != ownerAcknowledgementRetained {
		t.Fatal("archived version was queued again", len(s.Receipts), err)
	}
}

func TestOwnerAcknowledgementSerializedLimitMatchesReader(t *testing.T) {
	f := newOwnerAckFixture(t)
	s, _ := f.r.loadOwnerAcknowledgements()
	a := ownerAcknowledgement{Guild: "100", Thread: "500", Message: f.source.ID, Author: "300", Created: f.source.Timestamp, BodySHA: bytesHash([]byte(f.source.Content))}
	s.Receipts[a.version()] = a
	compact, _ := json.Marshal(s)
	indented, _ := json.MarshalIndent(s, "", "  ")
	a.Queued = strings.Repeat(" ", ownerAcknowledgementLimit-len(compact)-(len(indented)-len(compact))/2)
	s.Receipts[a.version()] = a
	compact, _ = json.Marshal(s)
	indented, _ = json.MarshalIndent(s, "", "  ")
	if len(compact) >= ownerAcknowledgementLimit || len(indented) <= ownerAcknowledgementLimit {
		t.Fatal("invalid size-boundary fixture")
	}
	if f.r.saveOwnerAcknowledgements(s) == nil {
		t.Fatal("indented file larger than reader limit was written")
	}
	if _, err := os.Stat(f.r.ownerAcknowledgementPath()); !os.IsNotExist(err) {
		t.Fatal("oversized index was committed")
	}
}

func TestOwnerAcknowledgementConversationPollAndEditedReply(t *testing.T) {
	f := conversationTest(t, true)
	q := f.ask(t)
	cutoff, _ := acknowledgementTime(ownerAcknowledgementSince)
	now := cutoff.Add(time.Minute)
	f.r.Now = func() time.Time { return now }
	f.r.OwnerAcknowledgementsActive = true
	m := msg(cutoff.Add(time.Second), q.Thread, "300", "aで進めてください")
	ack := attachOwnerAckFixture(t, f.r, m)
	f.replies = []conversationReply{{RequestID: q.SenderID, Todo: q.Todo, Thread: q.Thread, Question: q.Message, Author: "300", Message: m.ID, Created: m.Timestamp, Body: m.Content, Decision: f.decision}}
	for range 2 {
		h := Health{}
		f.r.processConversations(context.Background(), &h)
		if len(h.Errors) != 0 {
			t.Fatal("conversation delivery changed", h)
		}
	}
	if ack.posts != 1 {
		t.Fatal("first reply or re-poll acknowledgement count", ack.posts)
	}
	edited := now.Add(time.Minute)
	now = edited.Add(time.Minute)
	f.edited = map[string]string{m.ID: edited.Format(time.RFC3339Nano)}
	f.replies[0].Body = "bへ変更してください"
	ack.source.Content = f.replies[0].Body
	ack.source.Edited = f.edited[m.ID]
	f.r.OwnerAcknowledgementAttempts = 0
	h := Health{}
	f.r.processConversations(context.Background(), &h)
	if ack.posts != 2 || len(h.Errors) != 0 {
		t.Fatal("edited reply not acknowledged exactly once", ack.posts, h)
	}
}

func TestOwnerAcknowledgementRecoveredQuestionRunsFromExistingTick(t *testing.T) {
	f := conversationTest(t, true)
	q := f.ask(t)
	cutoff, _ := acknowledgementTime(ownerAcknowledgementSince)
	f.r.Now = func() time.Time { return cutoff.Add(time.Minute) }
	m := msg(cutoff.Add(time.Second), q.Thread, "300", "新しい回答")
	ack := attachOwnerAckFixture(t, f.r, m)
	ack.discoveryFailure = true
	f.replies = []conversationReply{{RequestID: q.SenderID, Todo: q.Todo, Thread: q.Thread, Question: q.Message, Author: "300", Message: m.ID, Created: m.Timestamp, Body: m.Content, Decision: f.decision}}
	index, err := f.r.loadConversations()
	if err != nil {
		t.Fatal(err)
	}
	q.RecoveredIntake = 42
	index.Questions[q.RequestID] = q
	if err := publish(f.r.conversationPath(), index); err != nil {
		t.Fatal(err)
	}
	h := f.r.tick(context.Background())
	if ack.posts != 1 || h.OwnerAcknowledgements != 1 || f.r.OwnerAcknowledgementsActive {
		t.Fatal("existing tick did not acknowledge recovered question", ack.posts, h)
	}
}

func TestOwnerAcknowledgementAttemptsStayBoundedPerTick(t *testing.T) {
	f := newOwnerAckFixture(t)
	for i := 0; i < 3; i++ {
		m := f.source
		m.Content += fmt.Sprint(i)
		if err := f.r.queueOwnerAcknowledgement(m); err != nil {
			t.Fatal(err)
		}
	}
	f.tick()
	s, _ := f.r.loadOwnerAcknowledgements()
	checked := 0
	for _, a := range s.Receipts {
		if a.Checked != "" {
			checked++
		}
	}
	if f.r.OwnerAcknowledgementAttempts != 2 || checked != 2 || f.posts > 2 {
		t.Fatal("one tick exceeded finite acknowledgement budget", f.r.OwnerAcknowledgementAttempts, checked, f.posts)
	}
}

func TestOwnerAcknowledgementConfiguredQuestionDoesNotNeedPriorDiscovery(t *testing.T) {
	f := newOwnerAckFixture(t)
	delete(f.r.State.Threads, "500")
	f.channelOverride = &Thread{ID: "500", Guild: "100", Parent: "200", Owner: "400", Type: 11}
	f.queue(t)
	f.tick()
	if f.posts != 1 {
		t.Fatal("configured bot question could not receive its first reply")
	}
}

func TestOwnerAcknowledgementThreadOwnershipChangeIsNotAuthorized(t *testing.T) {
	f := newOwnerAckFixture(t)
	f.channelOverride = &Thread{ID: "500", Guild: "100", Parent: "999", Owner: "999", Type: 11}
	f.queue(t)
	f.tick()
	if f.posts != 0 {
		t.Fatal("changed thread scope was accepted")
	}
}

func TestOwnerAcknowledgementPartialHistoryCannotAuthorizePost(t *testing.T) {
	f := newOwnerAckFixture(t)
	rows := []Message{}
	f.historyOverride = &rows
	f.queue(t)
	f.tick()
	if f.posts != 0 {
		t.Fatal("partial history authorized a new POST")
	}
	s, _ := f.r.loadOwnerAcknowledgements()
	for _, a := range s.Receipts {
		if a.Terminal != "history_coverage_unconfirmed" {
			t.Fatal("missing history coverage not recorded", a)
		}
	}
}

func TestOwnerAcknowledgementBatchRetainsValidSourcesAfterInvalidMetadata(t *testing.T) {
	f := newOwnerAckFixture(t)
	bad := f.source
	bad.ID = "900"
	bad.Timestamp = "bad-time"
	if f.r.queueOwnerAcknowledgements([]Message{bad, f.source}) == nil {
		t.Fatal("malformed source was not surfaced")
	}
	h := f.tick()
	if f.posts != 1 || h.OwnerAcknowledgements != 1 {
		t.Fatal("bad first source starved valid later capture", f.posts, h)
	}
}

func TestOwnerAcknowledgementDoesNotUseDifferentBotIdentity(t *testing.T) {
	f := newOwnerAckFixture(t)
	f.botIdentity = "999"
	f.queue(t)
	h := f.tick()
	if f.posts != 0 || len(h.Errors) == 0 {
		t.Fatal("wrong bot identity was permitted", f.posts, h)
	}
}
