package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) (*Runner, time.Time) {
	t.Helper()
	cutoff := time.Date(2026, 10, 13, 0, 59, 25, 0, time.UTC)
	home := t.TempDir()
	r := &Runner{Config: Config{Home: home, Project: "cc-pc-tools-rs"}, State: &State{Domain: "unattended-owner-v1", Since: cutoff.Format(time.RFC3339), Destination: Destination{Guild: "100", Forum: "200", Owner: "300", Bot: "400"}, Threads: map[string]Thread{}, Scans: map[string]*Scan{}, Requests: map[string]*Request{}}, Client: &http.Client{Timeout: time.Second}, Token: "fixture-token", Now: func() time.Time { return cutoff.Add(time.Minute) }, TodoPath: filepath.Join(home, "todo.json")}
	if err := os.WriteFile(r.TodoPath, []byte(`{"items":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	return r, cutoff
}
func msg(t time.Time, thread, author, text string) Message {
	m := Message{ID: snowflake(t), Channel: thread, Content: text, Timestamp: t.Format(time.RFC3339Nano)}
	m.Author.ID = author
	return m
}
func knownLegacyThreadForTest(r *Runner, id string) {
	r.State.Threads[id] = Thread{ID: id, Guild: r.State.Destination.Guild, Parent: requestForum, Owner: r.State.Destination.Owner, Type: 11}
}
func server(t *testing.T, r *Runner, handler http.HandlerFunc) {
	t.Helper()
	s := httptest.NewServer(handler)
	t.Cleanup(s.Close)
	r.Base = s.URL
}
func respond(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func TestCaptureOnlyNewOwnerAndDeduplicateAcrossObservations(t *testing.T) {
	r, c := fixture(t)
	fresh := msg(c.Add(time.Second), "500", "300", "監視を修復して")
	rows := []Message{msg(c.Add(-time.Second), "500", "300", "old"), msg(c.Add(2*time.Second), "500", "999", "ignore"), fresh}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if q.Header.Get("Authorization") != "Bot fixture-token" {
			t.Error("authorization missing")
		}
		respond(w, rows)
	})
	count, e := r.scan(context.Background(), Thread{ID: "500"})
	if e != nil || count != 1 || len(r.State.Requests) != 1 {
		t.Fatalf("capture %d %v", count, e)
	}
	count, e = r.scan(context.Background(), Thread{ID: "500"})
	if e != nil || count != 0 || len(r.State.Requests) != 1 {
		t.Fatalf("duplicate %d %v", count, e)
	}
}
func TestBotEvenWithOwnerIdentityIsNotOwnerInstruction(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "do this")
	m.Author.Bot = true
	server(t, r, func(w http.ResponseWriter, q *http.Request) { respond(w, []Message{m}) })
	n, e := r.scan(context.Background(), Thread{ID: "500"})
	if e != nil || n != 0 {
		t.Fatalf("bot capture %d %v", n, e)
	}
}
func TestShortAnswerDoesNotUsePrecedingProgressAsQuestion(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "<@400> a")
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		respond(w, []Message{msg(c, "500", "400", "working"), m})
	})
	_, e := r.scan(context.Background(), Thread{ID: "500"})
	if e != nil {
		t.Fatal(e)
	}
	if r.State.Requests[m.ID].Phase != "context_pending" || r.State.Requests[m.ID].Context != "" {
		t.Fatal("guessed answer context")
	}
}
func TestForeignReplyReferenceIsNotRoutedAsAnOwnerInstruction(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "reply")
	m.Reference.Channel = "999"
	m.Reference.Message = "700"
	m.Reference.Guild = "999"
	server(t, r, func(w http.ResponseWriter, q *http.Request) { respond(w, []Message{m}) })
	n, err := r.scan(context.Background(), Thread{ID: "500"})
	if err != nil || n != 1 {
		t.Fatalf("owner reply receipt not saved: %d %v", n, err)
	}
	request := r.State.Requests[m.ID]
	if request.Phase != "reference_mismatch" || request.ReplyTo != "700" || request.Thread != "500" {
		t.Fatal("foreign reply target was lost or promoted")
	}
}
func TestForeignChannelDoesNotAdvanceCursor(t *testing.T) {
	r, c := fixture(t)
	r.State.Scans["500"] = &Scan{After: snowflake(c)}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		respond(w, []Message{msg(c.Add(time.Second), "999", "300", "wrong")})
	})
	_, e := r.scan(context.Background(), Thread{ID: "500"})
	if e == nil || r.State.Scans["500"].After != snowflake(c) {
		t.Fatal("foreign channel accepted")
	}
}
func TestMultiPageHistoryKeepsHighWaterUntilCoverageComplete(t *testing.T) {
	r, c := fixture(t)
	r.State.Scans["500"] = &Scan{After: snowflake(c)}
	var pages int
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		pages++
		if pages == 1 {
			rows := []Message{}
			for i := 100; i > 0; i-- {
				rows = append(rows, msg(c.Add(time.Duration(i)*time.Second), "500", "300", fmt.Sprintf("request %d", i)))
			}
			respond(w, rows)
			return
		}
		if q.URL.Query().Get("before") == "" {
			t.Error("backfill cursor missing")
		}
		respond(w, []Message{msg(c.Add(-time.Millisecond), "500", "300", "old")})
	})
	_, e := r.scan(context.Background(), Thread{ID: "500"})
	if e != nil {
		t.Fatal(e)
	}
	if r.State.Scans["500"].After != snowflake(c) || r.State.Scans["500"].Before == "" {
		t.Fatal("advanced before full coverage")
	}
	_, e = r.scan(context.Background(), Thread{ID: "500"})
	if e != nil || r.State.Scans["500"].After != snowflake(c.Add(100*time.Second)) || r.State.Scans["500"].Before != "" || len(r.State.Requests) != 100 {
		t.Fatalf("coverage %v", e)
	}
}
func TestCredentialIsNotPersisted(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "password=example-value fixture-token")
	server(t, r, func(w http.ResponseWriter, q *http.Request) { respond(w, []Message{m}) })
	_, e := r.scan(context.Background(), Thread{ID: "500"})
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(filepath.Join(r.Config.Home, "unattended-state.json"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), "fixture-token") || strings.Contains(string(raw), "example-value") {
		t.Fatal("credential persisted")
	}
}
func TestRejectModifiedOwnerMessageBeforeRegistration(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "original")
	request := &Request{Thread: "500", Guild: "100", RecordVersion: 2, Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: "original", Key: requestKey("500", m.ID)}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			m.Content = "edited"
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Type: 11, Guild: "100", Parent: requestForum, Owner: "300"})
		}
	})
	if r.validateRequest(context.Background(), request) == nil {
		t.Fatal("modified message accepted")
	}
}
func TestRejectForgedStoredOwner(t *testing.T) {
	r, c := fixture(t)
	req := &Request{Thread: "500", Message: snowflake(c.Add(time.Second)), Author: "999", Created: c.Add(time.Second).Format(time.RFC3339), Source: "discord-api-v10", Key: requestKey("500", snowflake(c.Add(time.Second)))}
	server(t, r, func(w http.ResponseWriter, q *http.Request) { t.Error("rejected identity reached API") })
	if r.validateRequest(context.Background(), req) == nil {
		t.Fatal("forged author accepted")
	}
}
func TestCrashAfterRegistrationRecoversSameTodo(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "fix")
	req := &Request{Thread: "500", Guild: "100", RecordVersion: 2, Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: m.Content, Key: requestKey("500", m.ID)}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Type: 11, Guild: "100", Parent: requestForum, Owner: "300"})
		}
	})
	body := fmt.Sprintf(`{"items":[{"id":123,"project":"cc-pc-tools-rs","note":"unattended-owner-receipt:%s","status":"todo"}]}`, req.Key)
	_ = os.WriteFile(r.TodoPath, []byte(body), 0600)
	r.Run = func(context.Context, string, ...string) (string, error) {
		t.Error("duplicate registration command")
		return "", nil
	}
	if e := r.register(context.Background(), req); e != nil || req.Todo != 123 {
		t.Fatalf("receipt recovery %v", e)
	}
}
func TestDuplicateReceiptIsNotChosenArbitrarily(t *testing.T) {
	r, _ := fixture(t)
	_ = os.WriteFile(r.TodoPath, []byte(`{"items":[{"id":1,"project":"cc-pc-tools-rs","note":"unattended-owner-receipt:x"},{"id":2,"project":"cc-pc-tools-rs","note":"unattended-owner-receipt:x"}]}`), 0600)
	if _, e := r.existingTodo("x"); e == nil {
		t.Fatal("ambiguous receipt chosen")
	}
}
func TestSchedulerBusyOrErrorIsNotSuccessful(t *testing.T) {
	for _, raw := range []string{`{"busy":true}`, `{"repair_mode":true,"error":"measurement_failed"}`, `not-json`, `{"repair_mode":false}`} {
		if schedulerOK(raw) {
			t.Fatalf("false success %s", raw)
		}
	}
	if !schedulerOK("diagnostic\n{\"repair_mode\":true,\"error\":null}") {
		t.Fatal("valid report rejected")
	}
}
func TestStatePublishDoesNotOverwriteOnSizeFailure(t *testing.T) {
	r, _ := fixture(t)
	path := filepath.Join(r.Config.Home, "saved.json")
	if e := publish(path, map[string]int{"v": 1}); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(path)
	if publish(path, strings.Repeat("x", maxState+1)) == nil {
		t.Fatal("size accepted")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("old state changed on failure")
	}
}
func TestWindowLockSerializesAndReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	release, e := acquire(path)
	if e != nil {
		t.Fatal(e)
	}
	if second, e := acquire(path); e == nil {
		second()
		release()
		t.Fatal("duplicate lock accepted")
	}
	release()
	again, e := acquire(path)
	if e != nil {
		t.Fatal(e)
	}
	again()
}
func TestSnowflakeNumericOrdering(t *testing.T) {
	_, c := fixture(t)
	a, _ := strconv.ParseUint(snowflake(c), 10, 64)
	b, _ := strconv.ParseUint(snowflake(c.Add(time.Millisecond)), 10, 64)
	if b <= a {
		t.Fatal("cutover identity order differs")
	}
}
func TestQuestionUsesExactCanonicalPhaseAndReply(t *testing.T) {
	for _, match := range []bool{true, false} {
		r, c := fixture(t)
		m := msg(c.Add(time.Second), "500", "300", "a")
		req := &Request{Thread: "500", Guild: "100", RecordVersion: 2, Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: m.Content, Key: requestKey("500", m.ID), Phase: "context_pending"}
		ledger := map[string]any{"domain": "new-owner-recovery-messages", "events": []any{map[string]any{"event": "verified", "request_id": "fixture-q", "phase": "question:0", "receipt": map[string]any{"thread_id": "500", "message_id": "600", "author_id": "400"}}}}
		if e := publish(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), ledger); e != nil {
			t.Fatal(e)
		}
		r.Run = func(context.Context, string, ...string) (string, error) {
			mid := req.Message
			if !match {
				mid = "777"
			}
			body, _ := json.Marshal(map[string]any{"domain": "new-owner-recovery-messages", "source": "discord-api-v10", "thread_id": "500", "owner_id": "300", "question_message_id": "600", "owner_replies": []any{map[string]any{"message_id": mid, "author_id": "300", "question_thread_id": "500", "question_message_id": "600", "body": "a"}}})
			return string(body), nil
		}
		e := r.questionContext(context.Background(), req)
		if (e == nil) != match {
			t.Fatalf("reply association %v %v", match, e)
		}
		if (req.Phase == "captured") != match {
			t.Fatal("wrong reply promoted")
		}
	}
}
func TestEditedFirstRequestDoesNotStarveSecond(t *testing.T) {
	r, c := fixture(t)
	first := msg(c.Add(time.Second), "500", "300", "old")
	second := msg(c.Add(2*time.Second), "501", "300", "new")
	second.Channel = second.ID // A new owner forum starter, not an unbound followup.
	for _, m := range []Message{first, second} {
		r.State.Requests[m.ID] = &Request{Thread: m.Channel, Guild: "100", RecordVersion: 2, Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: m.Content, Key: requestKey(m.Channel, m.ID), Phase: "captured"}
	}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			if strings.Contains(q.URL.Path, "/500/") {
				first.Content = "edited"
				respond(w, first)
			} else {
				respond(w, second)
			}
		} else {
			tid := strings.TrimPrefix(q.URL.Path, "/channels/")
			respond(w, Thread{ID: tid, Type: 11, Guild: "100", Parent: requestForum, Owner: "300"})
		}
	})
	calls := 0
	r.Run = func(context.Context, string, ...string) (string, error) { calls++; return "added 123", nil }
	var h Health
	r.admit(context.Background(), &h)
	if calls != 1 || h.Registered != 1 || r.State.Requests[second.ID].Todo != 123 {
		t.Fatal("second request starved")
	}
	a := r.State.Requests[first.ID]
	if a.Text != "edited" || len(a.PriorVersions) != 1 || a.PriorVersions[0] != "old" {
		t.Fatal("owner edit/evidence lost")
	}
}

func TestVerifiedProgressReplyCreatesNewGenerationForCompletedOrigin(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "次の指示")
	link := ProgressLink{ProgressOrigin: ProgressOrigin{RequestID: "owner-discord-progress-2665-20261015-initial", Guild: "100", Thread: "500", Message: "600", Todo: 10, Job: "original-job"}, Author: "400", Created: c.Format(time.RFC3339Nano)}
	r.ProgressLinks = map[string]ProgressLink{"500": link}
	r.BlockedProgressThreads = map[string]bool{}
	_ = os.WriteFile(r.TodoPath, []byte(`{"items":[{"id":10,"project":"cc-pc-tools-rs","status":"done","jobs":["original-job"],"title":"original"}]}`), 0600)
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Type: 11, Guild: "100", Parent: "200", Owner: "400"})
		}
	})
	adds := 0
	r.Run = func(_ context.Context, _ string, args ...string) (string, error) {
		adds++
		if len(args) == 0 || args[0] != "add" {
			t.Fatalf("unexpected command: %v", args)
		}
		note := "unattended-owner-receipt:" + requestKey("500", m.ID) + "\n" + originGenerationMarker(link.ProgressOrigin)
		var noteArg string
		for i := 0; i+1 < len(args); i++ {
			if args[i] == "--note" {
				noteArg = args[i+1]
			}
		}
		if noteArg != note {
			t.Fatal("new generation receipt did not retain its source identity")
		}
		_ = os.WriteFile(r.TodoPath, []byte(fmt.Sprintf(`{"items":[{"id":10,"project":"cc-pc-tools-rs","status":"done","jobs":["original-job"]},{"id":11,"project":"cc-pc-tools-rs","status":"todo","note":%q}]}`, note)), 0600)
		return "added 11", nil
	}
	request := &Request{Thread: "500", Guild: "100", RecordVersion: 2, Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: m.Content, Key: requestKey("500", m.ID), Phase: "captured"}
	if err := r.register(context.Background(), request); err != nil || request.Todo != 11 || request.Phase != "registered" || request.Origin == nil || request.Origin.Todo != 10 {
		t.Fatalf("new generation not registered: todo=%d phase=%s err=%v", request.Todo, request.Phase, err)
	}
	if adds != 1 {
		t.Fatal("new instruction did not create exactly one generation")
	}
	spec, err := os.ReadFile(filepath.Join(r.Config.Home, "requests", m.ID+".md"))
	if err != nil || !strings.Contains(string(spec), "元ToDo 10") || !strings.Contains(string(spec), "original-job") || !strings.Contains(string(spec), "新しい作業世代") {
		t.Fatal("new generation lost the exact original task context")
	}
	if err := r.register(context.Background(), request); err != nil || request.Todo != 11 || adds != 1 {
		t.Fatal("retry duplicated the new task generation")
	}
}

func TestVerifiedProgressReplySteersOnlyItsActiveOrigin(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "追加指示")
	r.ProgressLinks = map[string]ProgressLink{"500": {ProgressOrigin: ProgressOrigin{RequestID: "exact", Guild: "100", Thread: "500", Message: "600", Todo: 10, Job: "original-job"}, Author: "400", Created: c.Format(time.RFC3339Nano)}}
	r.BlockedProgressThreads = map[string]bool{}
	_ = os.WriteFile(r.TodoPath, []byte(`{"items":[{"id":10,"project":"cc-pc-tools-rs","status":"doing","jobs":["original-job"]}]}`), 0600)
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Type: 11, Guild: "100", Parent: "200", Owner: "400"})
		}
	})
	r.Run = func(context.Context, string, ...string) (string, error) {
		t.Fatal("active task must not be started a second time")
		return "", nil
	}
	request := &Request{Thread: "500", Guild: "100", RecordVersion: 2, Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: m.Content, Key: requestKey("500", m.ID), Phase: "captured"}
	if err := r.register(context.Background(), request); err != nil || request.Todo != 10 || request.Phase != "steering_saved" {
		t.Fatalf("active task was not steered: todo=%d phase=%s err=%v", request.Todo, request.Phase, err)
	}
	if _, err := os.Stat(filepath.Join(r.Config.Home, "steering", "10-"+m.ID+".json")); err != nil {
		t.Fatal("durable steering record missing")
	}
}

func TestLaterReplyReusesTheActiveGenerationForTheSameOrigin(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "さらに補足")
	origin := ProgressOrigin{RequestID: "exact-origin", Guild: "100", Thread: "500", Message: "600", Todo: 10, Job: "original-job"}
	r.ProgressLinks = map[string]ProgressLink{"500": {ProgressOrigin: origin, Author: "400", Created: c.Format(time.RFC3339Nano)}}
	r.BlockedProgressThreads = map[string]bool{}
	note := originGenerationMarker(origin)
	_ = os.WriteFile(r.TodoPath, []byte(fmt.Sprintf(`{"items":[{"id":10,"project":"cc-pc-tools-rs","status":"done","jobs":["original-job"]},{"id":11,"project":"cc-pc-tools-rs","status":"doing","jobs":["generation-job"],"note":%q}]}`, note)), 0600)
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Type: 11, Guild: "100", Parent: "200", Owner: "400"})
		}
	})
	r.Run = func(context.Context, string, ...string) (string, error) {
		t.Fatal("a second task generation was launched for the same source")
		return "", nil
	}
	request := &Request{Thread: "500", Guild: "100", RecordVersion: 2, Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: m.Content, Key: requestKey("500", m.ID), Phase: "captured"}
	if err := r.register(context.Background(), request); err != nil || request.Todo != 11 || request.Phase != "steering_saved" || request.Origin == nil || request.Origin.Todo != 10 {
		t.Fatalf("later reply did not reuse the active generation: todo=%d phase=%s err=%v", request.Todo, request.Phase, err)
	}
	if _, err := os.Stat(filepath.Join(r.Config.Home, "steering", "11-"+m.ID+".json")); err != nil {
		t.Fatal("later reply was not durably routed to the existing generation")
	}
}

func TestLinkedShortProgressReplyIsLiteralNotAQuestionAnswer(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "a")
	r.ProgressLinks = map[string]ProgressLink{"500": {ProgressOrigin: ProgressOrigin{RequestID: "exact", Guild: "100", Thread: "500", Message: "600", Todo: 10, Job: "original-job"}, Author: "400", Created: c.Format(time.RFC3339Nano)}}
	r.BlockedProgressThreads = map[string]bool{}
	request := &Request{Thread: "500", Guild: "100", RecordVersion: 2, Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: "a", Key: requestKey("500", m.ID), Phase: "context_pending"}
	r.State.Requests[m.ID] = request
	_ = os.WriteFile(r.TodoPath, []byte(`{"items":[{"id":10,"project":"cc-pc-tools-rs","status":"done","jobs":["original-job"]}]}`), 0600)
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Type: 11, Guild: "100", Parent: "200", Owner: "400"})
		}
	})
	added := false
	r.Run = func(context.Context, string, ...string) (string, error) { added = true; return "added 11", nil }
	var health Health
	r.admit(context.Background(), &health)
	if !added || request.Phase != "registered" || request.Todo != 11 || !strings.Contains(request.Context, "non-question progress thread") {
		t.Fatal("short reply on progress thread was misclassified or stranded")
	}
}

func TestProgressLinkScopeMustMatchConfiguredServerAndExactReceipt(t *testing.T) {
	for _, change := range []func(*ProgressLink){
		func(link *ProgressLink) { link.Guild = "999" },
		func(link *ProgressLink) { link.Message = "601" },
		func(link *ProgressLink) { link.RequestID = "other" },
	} {
		r, c := fixture(t)
		r.Config.Home = filepath.Join(t.TempDir(), "unattended")
		_ = os.MkdirAll(r.Config.Home, 0700)
		_ = os.WriteFile(r.TodoPath, []byte(`{"items":[{"id":10,"project":"cc-pc-tools-rs","status":"done","scheduler":{"job":"original-job"}}]}`), 0600)
		link := ProgressLink{ProgressOrigin: ProgressOrigin{RequestID: "exact", Guild: "100", Thread: "500", Message: "600", Todo: 10, Job: "original-job"}, Author: "400", Created: c.Format(time.RFC3339Nano)}
		change(&link)
		if link.Guild == "100" {
			ledger := map[string]any{"domain": "new-owner-recovery-messages", "events": []any{map[string]any{"event": "verified", "request_id": "exact", "phase": "log:0", "receipt": map[string]any{"thread_id": "500", "message_id": "600", "author_id": "400", "timestamp": c.Format(time.RFC3339Nano), "flags": 4096}}}}
			_ = publish(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), ledger)
		} else {
			_ = publish(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), map[string]any{"domain": "new-owner-recovery-messages", "events": []any{}})
		}
		server(t, r, func(w http.ResponseWriter, q *http.Request) {
			if strings.Contains(q.URL.Path, "/messages/") {
				m := Message{ID: "600", Channel: "500", Timestamp: c.Format(time.RFC3339Nano)}
				m.Author.ID, m.Author.Bot = "400", true
				respond(w, m)
			} else {
				respond(w, Thread{ID: "500", Type: 11, Guild: "100", Parent: "200", Owner: "400"})
			}
		})
		if _, accepted := r.verifyProgressLink(context.Background(), link); accepted {
			t.Fatal("mismatched progress association accepted")
		}
	}
}

func TestProgressSourceEditTimeAndPreviousVersionArePreserved(t *testing.T) {
	r, c := fixture(t)
	r.Config.Home = filepath.Join(t.TempDir(), "unattended")
	_ = os.MkdirAll(r.Config.Home, 0700)
	_ = os.WriteFile(r.TodoPath, []byte(`{"items":[{"id":10,"project":"cc-pc-tools-rs","status":"done","scheduler":{"job":"original-job"}}]}`), 0600)
	created := c.Format(time.RFC3339Nano)
	ledger := map[string]any{"domain": "new-owner-recovery-messages", "events": []any{map[string]any{"event": "verified", "request_id": "exact", "phase": "log:0", "receipt": map[string]any{"thread_id": "500", "message_id": "600", "author_id": "400", "timestamp": created, "flags": 4096}}}}
	_ = publish(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), ledger)
	link := ProgressLink{ProgressOrigin: ProgressOrigin{RequestID: "exact", Guild: "100", Thread: "500", Message: "600", Todo: 10, Job: "original-job"}, Author: "400", Created: created, ContentHash: "old-hash"}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			m := Message{ID: "600", Channel: "500", Timestamp: created, Edited: c.Add(time.Minute).Format(time.RFC3339Nano), Content: "edited progress"}
			m.Author.ID, m.Author.Bot = "400", true
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Type: 11, Guild: "100", Parent: "200", Owner: "400"})
		}
	})
	updated, accepted := r.verifyProgressLink(context.Background(), link)
	if !accepted || updated.Edited == "" || len(updated.PriorEdits) != 1 {
		t.Fatal("edited source post was rejected or its previous version was lost")
	}
	sum := sha256.Sum256([]byte("edited progress"))
	if updated.ContentHash != hex.EncodeToString(sum[:]) || updated.PriorEdits[0].SHA256 != "old-hash" {
		t.Fatal("source content hash history differs")
	}
}

func TestProgressLinkLoadsOnlyAfterReceiptAndLiveDiscordVerification(t *testing.T) {
	r, c := fixture(t)
	r.Config.Home = filepath.Join(t.TempDir(), "unattended")
	_ = os.MkdirAll(r.Config.Home, 0700)
	_ = os.WriteFile(r.TodoPath, []byte(`{"items":[{"id":10,"project":"cc-pc-tools-rs","status":"done","jobs":["original-job"]}]}`), 0600)
	created := c.Format(time.RFC3339Nano)
	link := ProgressLink{ProgressOrigin: ProgressOrigin{RequestID: "exact", Guild: "100", Thread: "500", Message: "600", Todo: 10, Job: "original-job"}, Author: "400", Created: created}
	if err := publish(filepath.Join(r.Config.Home, "progress-links.json"), progressLinkFile{Domain: progressLinksDomain, Links: []ProgressLink{link}}); err != nil {
		t.Fatal(err)
	}
	ledger := map[string]any{"domain": "new-owner-recovery-messages", "events": []any{map[string]any{"event": "verified", "request_id": "exact", "phase": "log:0", "receipt": map[string]any{"thread_id": "500", "message_id": "600", "author_id": "400", "timestamp": created, "flags": 4096}}}}
	_ = publish(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), ledger)
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			m := Message{ID: "600", Channel: "500", Timestamp: created, Content: "progress"}
			m.Author.ID, m.Author.Bot = "400", true
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Type: 11, Guild: "100", Parent: "200", Owner: "400"})
		}
	})
	r.loadProgressLinks(context.Background(), map[string]bool{"500": true})
	if r.ProgressLinks["500"].Todo != 10 || r.State.ProgressLinks["500"].Message != "600" || r.BlockedProgressThreads["500"] {
		t.Fatal("verified progress relation was not loaded into receiver state")
	}
}

func TestCorruptProgressLinkRegistryBlocksKnownBotThreads(t *testing.T) {
	r, _ := fixture(t)
	r.Config.Home = filepath.Join(t.TempDir(), "unattended")
	_ = os.MkdirAll(r.Config.Home, 0700)
	_ = os.WriteFile(filepath.Join(r.Config.Home, "progress-links.json"), []byte("{"), 0600)
	r.loadProgressLinks(context.Background(), map[string]bool{"500": true, "501": true})
	for _, thread := range []string{"500", "501"} {
		if _, ok := r.linkOrigin(&Request{Thread: thread}); ok {
			t.Fatalf("corrupt registry allowed unrelated intake from thread %s", thread)
		}
	}
	if !r.ProgressLinkRegistryInvalid {
		t.Fatal("corrupt registry was not surfaced")
	}
}

func TestMismatchedProgressLinkBlocksKnownThreadNotNamedByBadEntry(t *testing.T) {
	r, _ := fixture(t)
	r.Config.Home = filepath.Join(t.TempDir(), "unattended")
	_ = os.MkdirAll(r.Config.Home, 0700)
	bad := ProgressLink{ProgressOrigin: ProgressOrigin{RequestID: "wrong-origin", Guild: "999", Thread: "501", Message: "501", Todo: 10, Job: "wrong-job"}, Author: "400", Created: "2026-10-15T00:00:00Z"}
	_ = publish(filepath.Join(r.Config.Home, "progress-links.json"), progressLinkFile{Domain: progressLinksDomain, Links: []ProgressLink{bad}})
	r.loadProgressLinks(context.Background(), map[string]bool{"500": true})
	for _, thread := range []string{"500", "501"} {
		if _, ok := r.linkOrigin(&Request{Thread: thread}); ok {
			t.Fatalf("mismatched link left thread %s open for unrelated intake", thread)
		}
	}
	if !r.ProgressLinkRegistryInvalid {
		t.Fatal("mismatched progress registry was not surfaced")
	}
}

func TestIntakeCursorAdvancesPastBlockedOrigins(t *testing.T) {
	r, c := fixture(t)
	messages := make([]Message, 5)
	for i := range messages {
		thread := fmt.Sprintf("blocked-%d", i+1)
		if i == 4 {
			thread = snowflake(c.Add(time.Duration(i+1) * time.Second))
		}
		messages[i] = msg(c.Add(time.Duration(i+1)*time.Second), thread, "300", fmt.Sprintf("request %d", i+1))
		r.State.Requests[messages[i].ID] = &Request{Thread: thread, Guild: "100", RecordVersion: 2, Message: messages[i].ID, Author: "300", Created: messages[i].Timestamp, Source: "discord-api-v10", Text: messages[i].Content, Key: requestKey(thread, messages[i].ID), Phase: "captured"}
		if i < 4 {
			if r.BlockedProgressThreads == nil {
				r.BlockedProgressThreads = map[string]bool{}
			}
			r.BlockedProgressThreads[thread] = true
		} else {
			knownLegacyThreadForTest(r, thread)
		}
	}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			respond(w, messages[4])
		} else {
			respond(w, Thread{ID: messages[4].Channel, Type: 11, Guild: "100", Parent: requestForum, Owner: "300"})
		}
	})
	adds := 0
	r.Run = func(context.Context, string, ...string) (string, error) { adds++; return "added 99", nil }
	var health Health
	r.admit(context.Background(), &health)
	if adds != 0 || r.State.IntakeCursor != messages[3].ID {
		t.Fatal("blocked messages did not advance and persist the intake cursor")
	}
	r.admit(context.Background(), &health)
	if adds != 1 || r.State.Requests[messages[4].ID].Todo != 99 {
		t.Fatal("later valid request remained starved behind blocked origins")
	}
}

func TestBlockedOriginReplyIsReevaluatedAfterTaskUnblocks(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "解除後に届ける指示")
	origin := ProgressOrigin{RequestID: "exact-origin", Guild: "100", Thread: "500", Message: "600", Todo: 10, Job: "original-job"}
	request := &Request{Thread: "500", Guild: "100", RecordVersion: 2, Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: m.Content, Key: requestKey("500", m.ID), Phase: "captured", ProgressThread: "500", Origin: &origin}
	r.State.Requests[m.ID] = request
	_ = os.WriteFile(r.TodoPath, []byte(`{"items":[{"id":10,"project":"cc-pc-tools-rs","status":"block","jobs":["original-job"]}]}`), 0600)
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Type: 11, Guild: "100", Parent: "200", Owner: "400"})
		}
	})
	r.Run = func(context.Context, string, ...string) (string, error) { return "added 11", nil }
	var health Health
	if err := r.registerOrigin(context.Background(), request, origin); err != nil || request.Phase != "origin_blocked" || request.Todo != 10 {
		t.Fatalf("blocked origin was not retained: phase=%s todo=%d err=%v", request.Phase, request.Todo, err)
	}
	request.Todo = 0 // legacy origin_blocked receipts had no destination ToDo.
	r.recheckOriginBlocked(context.Background(), &health)
	if request.Phase != "origin_blocked" || request.Todo != 10 {
		t.Fatal("legacy blocked reply bypassed the still-active owner stop")
	}
	_ = os.WriteFile(r.TodoPath, []byte(`{"items":[{"id":10,"project":"cc-pc-tools-rs","status":"done","jobs":["original-job"]}]}`), 0600)
	r.recheckOriginBlocked(context.Background(), &health)
	r.admit(context.Background(), &health)
	if request.Phase != "registered" || request.Todo != 11 {
		t.Fatalf("saved owner reply was not re-evaluated after the block cleared: phase=%s todo=%d", request.Phase, request.Todo)
	}
}

func TestLegacyOwnerReplyMetadataMigratesOnlyAfterCanonicalAPIIdentity(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "legacy receipt")
	m.Reference.Message, m.Reference.Channel, m.Reference.Guild = "600", "500", "100"
	request := &Request{Thread: "500", Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: m.Content, Key: requestKey("500", m.ID)}
	r.State.Requests[m.ID] = request
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Type: 11, Guild: "100", Parent: requestForum, Owner: "300"})
		}
	})
	if err := r.validateRequest(context.Background(), request); err == nil || request.RecordVersion != 2 || request.Guild != "100" || request.ReplyTo != "600" || request.ReplyChannel != "500" || request.ReplyGuild != "100" {
		t.Fatal("legacy metadata was not filled from the authenticated API message")
	}
	if err := r.validateRequest(context.Background(), request); err != nil {
		t.Fatalf("migrated owner reply could not be re-evaluated: %v", err)
	}
}

func TestOwnerEditTimestampChangeIsSavedWithOriginalIdentity(t *testing.T) {
	r, c := fixture(t)
	m := msg(c.Add(time.Second), "500", "300", "same text")
	m.Edited = c.Add(2 * time.Second).Format(time.RFC3339Nano)
	request := &Request{Thread: "500", Guild: "100", RecordVersion: 2, Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: m.Content, Key: requestKey("500", m.ID)}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Type: 11, Guild: "100", Parent: requestForum, Owner: "300"})
		}
	})
	if err := r.validateRequest(context.Background(), request); err == nil || request.Edited != m.Edited || len(request.PriorEdits) != 1 || request.PriorEdits[0].Edited != "" {
		t.Fatal("owner edit timestamp was not preserved as a deferred update")
	}
}
func TestBlockedThreadPreservesOriginalTaskForFollowup(t *testing.T) {
	r, c := fixture(t)
	old := &Request{Thread: "500", Message: "600", Todo: 10, Phase: "registered"}
	r.State.Requests[old.Message] = old
	_ = os.WriteFile(r.TodoPath, []byte(`{"items":[{"id":10,"project":"cc-pc-tools-rs","status":"block","jobs":[],"spec":false}]}`), 0600)
	m := msg(c.Add(time.Second), "500", "300", "new direction")
	req := &Request{Thread: "500", Guild: "100", RecordVersion: 2, Message: m.ID, Author: "300", Created: m.Timestamp, Source: "discord-api-v10", Text: m.Content, Key: requestKey("500", m.ID), Phase: "captured"}
	server(t, r, func(w http.ResponseWriter, q *http.Request) {
		if strings.Contains(q.URL.Path, "/messages/") {
			respond(w, m)
		} else {
			respond(w, Thread{ID: "500", Type: 11, Guild: "100", Parent: requestForum, Owner: "300"})
		}
	})
	r.Run = func(context.Context, string, ...string) (string, error) { return "added 11", nil }
	if e := r.register(context.Background(), req); e != nil || req.Todo != 10 || req.Phase != "steering_saved" {
		t.Fatalf("blocked delivery %v", e)
	}
}
func TestUnappliedSteeringRetainsBlockedOriginalForVersionedDelivery(t *testing.T) {
	r, _ := fixture(t)
	req := &Request{Thread: "500", Message: "600", Todo: 10, Phase: "steering_saved"}
	r.State.Requests[req.Message] = req
	_ = os.WriteFile(r.TodoPath, []byte(`{"items":[{"id":10,"project":"cc-pc-tools-rs","status":"block","jobs":[],"spec":false}]}`), 0600)
	if e := r.results(context.Background()); e != nil {
		t.Fatal(e)
	}
	if req.Todo != 10 || req.Phase != "steering_saved" || req.PendingOwner != "OMB" {
		t.Fatal("unapplied steering lost its original task or recovery owner")
	}
}
func TestLostSenderResponseRequiresExactSilentReceipt(t *testing.T) {
	for _, valid := range []bool{true, false} {
		r, _ := fixture(t)
		ledger := map[string]any{"domain": "new-owner-recovery-messages", "events": []any{map[string]any{"event": "verified", "request_id": "exact", "phase": "log:0", "receipt": map[string]any{"thread_id": "500", "message_id": "600", "author_id": "400", "flags": 4096}}}}
		if e := publish(filepath.Join(filepath.Dir(r.Config.Home), "recovery-messages.json"), ledger); e != nil {
			t.Fatal(e)
		}
		server(t, r, func(w http.ResponseWriter, q *http.Request) {
			if strings.Contains(q.URL.Path, "messages") {
				body := "expected\nreceipt marker"
				if !valid {
					body = "other"
				}
				m := Message{ID: "600", Channel: "500", Content: body, Flags: 4096}
				m.Author.ID = "400"
				respond(w, m)
			} else {
				respond(w, Thread{ID: "500", Type: 11, Guild: "100", Parent: "200", Owner: "400"})
			}
		})
		thread, e := r.recoverStatusReceipt(context.Background(), "exact", "expected")
		if (e == nil) != valid || (valid && thread != "500") {
			t.Fatalf("receipt verification %v", e)
		}
	}
}

func TestPullRequestLinksRequireCompleteOwnedGitHubURL(t *testing.T) {
	valid := "https://github.com/AutoDevJapan/cc-pc-tools-rs/pull/144"
	for _, candidate := range []string{
		"https://example.invalid/" + valid,
		"https://github.com.example.invalid/AutoDevJapan/repo/pull/1",
		"https://github.com@evil.invalid/AutoDevJapan/repo/pull/1",
		"https://evil.invalid/?target=" + valid,
		"https://evil.invalid/(" + valid + ")",
		valid + "suffix", valid + "/extra", valid + "?token=private", valid + "#fragment",
		"http://github.com/AutoDevJapan/repo/pull/1",
		"https://github.com/other-owner/repo/pull/1",
		"https://github.com/AutoDevJapan/../pull/1",
		"https://github.com/AutoDevJapan/%72epo/pull/1",
	} {
		if got := pullRequestLinks(candidate, 5); len(got) != 0 {
			t.Fatalf("untrusted URL was extracted: %q", candidate)
		}
	}
	body := "成果: [PR](" + valid + "). " + valid + " https://github.com/BM1314/another/pull/2"
	if got := pullRequestLinks(body, 5); len(got) != 2 || got[0] != valid {
		t.Fatalf("legitimate result links were lost or duplicated: %v", got)
	}
	if got := pullRequestLinks(body, 1); len(got) != 1 {
		t.Fatal("result link limit was not respected")
	}
}
