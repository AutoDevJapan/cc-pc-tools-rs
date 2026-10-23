package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ownerReplyMediaFixture(t *testing.T, waiting ...bool) (*conversationFixture, conversationReply, *Request, *map[string]any) {
	t.Helper()
	f := conversationTest(t, len(waiting) != 0 && waiting[0])
	f.ask(t)
	reply := f.reply(t, 3, "<@400>")
	live := map[string]any{"id": reply.Message, "channel_id": reply.Thread, "author": map[string]any{"id": "300", "bot": false}, "timestamp": reply.Created, "edited_timestamp": "", "content": reply.Body, "type": 0, "attachments": []any{map[string]any{"id": "900", "size": 64, "filename": "private-name.png", "url": "https://cdn.discordapp.com/never-record-this?signature=private"}}}
	server(t, f.r, func(w http.ResponseWriter, r *http.Request) { respond(w, live) })
	proof, err := f.r.conversationProof(context.Background(), f.q)
	if err != nil {
		t.Fatal(err)
	}
	req, err := f.r.captureConversationReply(context.Background(), f.q, proof, reply)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.r.deliverConversationReply(context.Background(), f.q, reply, req); err != nil {
		t.Fatal(err)
	}
	return f, reply, req, &live
}

func TestOwnerReplyMediaReferenceAndFixedScopeReachOriginalNote(t *testing.T) {
	f, reply, req, _ := ownerReplyMediaFixture(t)
	source := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+".json")
	before, _ := os.ReadFile(source)
	row, _ := f.r.todo(42)
	if err := f.r.deliverOwnerReplyMediaObservation(context.Background(), f.q, reply, req); err != nil {
		t.Fatal(err)
	}
	after, _ := f.r.todo(42)
	if !strings.Contains(after.Note, "conversation-owner-media:") || !strings.Contains(after.Note, "添付") || after.Status != row.Status || !strings.HasPrefix(after.Note, row.Note) {
		t.Fatalf("owner image reference missing or original task changed: %s", after.Note)
	}
	unchanged, _ := os.ReadFile(source)
	if string(before) != string(unchanged) {
		t.Fatal("canonical source was rewritten")
	}
	files, _ := filepath.Glob(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "owner-media", "*.scope.private.json"))
	if len(files) != 1 {
		t.Fatalf("image scope count %d", len(files))
	}
	var scope historyAttachmentScope
	if readJSON(files[0], 1<<20, &scope) != nil || scope.Attachment != "900" || scope.Message != reply.Message || scope.ContentSHA != bytesHash([]byte(reply.Body)) || scope.Created != reply.Created || scope.Size != 64 || len(scope.Threads) != 1 || scope.Threads[0] != reply.Thread || !historyAttachmentScopeValid(scope, f.r.Now()) {
		t.Fatalf("scope is not bound to the owner reply: %+v", scope)
	}
	paths, _ := filepath.Glob(filepath.Join(filepath.Dir(files[0]), "*.json"))
	for _, path := range paths {
		b, _ := os.ReadFile(path)
		if strings.Contains(string(b), "private-name") || strings.Contains(string(b), "signature=") {
			t.Fatal("attachment name or signed URL leaked")
		}
	}
	if err := f.r.deliverOwnerReplyMediaObservation(context.Background(), f.q, reply, req); err != nil {
		t.Fatal(err)
	}
	again, _ := f.r.todo(42)
	if again.Note != after.Note {
		t.Fatal("retry duplicated the note")
	}
}

func TestOwnerReplyMediaRejectsForeignOrChangedSource(t *testing.T) {
	for _, mode := range []string{"owner", "channel", "body", "time", "edited", "kind", "duplicate", "negative", "too_many"} {
		t.Run(mode, func(t *testing.T) {
			f, reply, req, live := ownerReplyMediaFixture(t)
			before, _ := f.r.todo(42)
			switch mode {
			case "owner":
				(*live)["author"] = map[string]any{"id": "999"}
			case "channel":
				(*live)["channel_id"] = "999"
			case "body":
				(*live)["content"] = "changed"
			case "time":
				(*live)["timestamp"] = "2026-10-13T00:00:00Z"
			case "edited":
				(*live)["edited_timestamp"] = "2026-10-13T01:00:00Z"
			case "kind":
				(*live)["type"] = 7
			case "duplicate":
				(*live)["attachments"] = []any{map[string]any{"id": "900", "size": 64}, map[string]any{"id": "900", "size": 64}}
			case "negative":
				(*live)["attachments"] = []any{map[string]any{"id": "900", "size": -1}}
			case "too_many":
				var a []any
				for i := 0; i < 11; i++ {
					a = append(a, map[string]any{"id": fmt.Sprint(900 + i), "size": 64})
				}
				(*live)["attachments"] = a
			}
			if err := f.r.deliverOwnerReplyMediaObservation(context.Background(), f.q, reply, req); err == nil {
				t.Fatal("changed/invalid media source accepted")
			}
			after, _ := f.r.todo(42)
			if before.Note != after.Note || before.Status != after.Status {
				t.Fatal("invalid source changed original task")
			}
		})
	}
}

func TestOwnerReplyMediaRemovalAndUnknownAreDistinct(t *testing.T) {
	f, reply, req, live := ownerReplyMediaFixture(t)
	if err := f.r.deliverOwnerReplyMediaObservation(context.Background(), f.q, reply, req); err != nil {
		t.Fatal(err)
	}
	first, _ := f.r.todo(42)
	delete(*live, "attachments")
	if err := f.r.deliverOwnerReplyMediaObservation(context.Background(), f.q, reply, req); err != nil {
		t.Fatal(err)
	}
	unknown, _ := f.r.todo(42)
	if unknown.Note != first.Note {
		t.Fatal("unknown metadata was asserted empty")
	}
	(*live)["attachments"] = []any{}
	if err := f.r.deliverOwnerReplyMediaObservation(context.Background(), f.q, reply, req); err != nil {
		t.Fatal(err)
	}
	after, _ := f.r.todo(42)
	if after.Note == first.Note || !strings.Contains(after.Note, "現在の添付は0件") {
		t.Fatal("removal not delivered")
	}
}

func TestOwnerReplyMediaFailedNoteRetriesWithoutLosingOriginal(t *testing.T) {
	f, reply, req, _ := ownerReplyMediaFixture(t)
	originalRun := f.r.Run
	fail := true
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "edit" && fail {
			fail = false
			return "", errors.New("conversation_note_changed")
		}
		return originalRun(ctx, path, args...)
	}
	before, _ := f.r.todo(42)
	if err := f.r.deliverOwnerReplyMediaObservation(context.Background(), f.q, reply, req); err == nil {
		t.Fatal("failed note reported successful")
	}
	after, _ := f.r.todo(42)
	if before.Note != after.Note {
		t.Fatal("failed write changed note")
	}
	latest := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "owner-media", reply.Message+".latest.private.json")
	if _, err := os.Stat(latest); !os.IsNotExist(err) {
		t.Fatal("failed note advanced latest observation")
	}
	if err := f.r.deliverOwnerReplyMediaObservation(context.Background(), f.q, reply, req); err != nil {
		t.Fatal(err)
	}
	after, _ = f.r.todo(42)
	if !strings.HasPrefix(after.Note, before.Note) || strings.Count(after.Note, "conversation-owner-media:") != 1 {
		t.Fatal("retry lost or duplicated original")
	}
}

func TestOwnerReplyMediaNormalTickDeliversMetadata(t *testing.T) {
	f, reply, _, _ := ownerReplyMediaFixture(t)
	health := &Health{}
	f.r.processConversations(context.Background(), health)
	row, err := f.r.todo(42)
	if err != nil || !strings.Contains(row.Note, "conversation-owner-media:") || !strings.Contains(row.Note, reply.Message) {
		t.Fatalf("normal tick omitted owner media: %v, errors=%v", err, health.Errors)
	}
	if f.close != 0 {
		t.Fatal("media reference was treated as application/close")
	}
	index, err := f.r.loadConversations()
	if err != nil || index.Questions[f.q.RequestID].Phase != "answered" || containsString(health.Errors, "conversation_owner_media_deferred") || containsString(health.Errors, "conversation_delivery_deferred") {
		t.Fatalf("normal delivery phase/health differs: %v %v", err, health.Errors)
	}
}

func TestOwnerReplyMediaNormalTickForWaitingAnswer(t *testing.T) {
	f, _, _, _ := ownerReplyMediaFixture(t, true)
	before, _ := f.r.todo(42)
	if before.Status != "todo" {
		t.Fatal("fixture did not exercise the canonical question-answer release")
	}
	health := &Health{}
	f.r.processConversations(context.Background(), health)
	after, _ := f.r.todo(42)
	if after.Status != before.Status || !strings.HasPrefix(after.Note, before.Note) || !strings.Contains(after.Note, "conversation-owner-media:") || containsString(health.Errors, "conversation_owner_media_deferred") || containsString(health.Errors, "conversation_delivery_deferred") {
		t.Fatalf("waiting-answer metadata path altered or lost canonical delivery: %v", health.Errors)
	}
}

func TestOwnerReplyMediaEmptyWithoutPriorObservationDoesNotInventImage(t *testing.T) {
	f, reply, req, live := ownerReplyMediaFixture(t)
	(*live)["attachments"] = []any{}
	before, _ := f.r.todo(42)
	if err := f.r.deliverOwnerReplyMediaObservation(context.Background(), f.q, reply, req); err != nil {
		t.Fatal(err)
	}
	after, _ := f.r.todo(42)
	if before.Note != after.Note {
		t.Fatal("empty attachment observation invented an image answer")
	}
}

func TestOwnerReplyMediaSecondReadChangesDoNotDeliver(t *testing.T) {
	f, reply, req, live := ownerReplyMediaFixture(t)
	calls := 0
	server(t, f.r, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			(*live)["attachments"] = []any{}
		}
		respond(w, *live)
	})
	before, _ := f.r.todo(42)
	if err := f.r.deliverOwnerReplyMediaObservation(context.Background(), f.q, reply, req); err == nil {
		t.Fatal("mid-flight media change accepted")
	}
	after, _ := f.r.todo(42)
	if before.Note != after.Note || calls != 2 {
		t.Fatal("second read did not guard actual delivery")
	}
}

func TestOwnerReplyMediaDeliveredObservationDoesNotReplayIntoReplacementHold(t *testing.T) {
	f, reply, req, _ := ownerReplyMediaFixture(t)
	if err := f.r.deliverOwnerReplyMediaObservation(context.Background(), f.q, reply, req); err != nil {
		t.Fatal(err)
	}
	f.mutateTodo(t, func(row map[string]any) {
		row["note"] = "ジョブの結果に判断が必要です。"
		row["status"] = "block"
	})
	before, _ := f.r.todo(42)
	if err := f.r.deliverOwnerReplyMediaObservation(context.Background(), f.q, reply, req); err != nil {
		t.Fatal(err)
	}
	after, _ := f.r.todo(42)
	if after.Note != before.Note || after.Status != before.Status {
		t.Fatal("delivered old media replayed into a different question hold")
	}
}

func TestOwnerReplyMediaMissingBodyAnchorDoesNotBackfill(t *testing.T) {
	f, reply, req, _ := ownerReplyMediaFixture(t)
	f.mutateTodo(t, func(row map[string]any) { row["note"] = "元の回答とは別の依頼を維持" })
	before, _ := f.r.todo(42)
	if err := f.r.deliverOwnerReplyMediaObservation(context.Background(), f.q, reply, req); err != nil {
		t.Fatal(err)
	}
	after, _ := f.r.todo(42)
	if after.Note != before.Note {
		t.Fatal("unrelated note received an old answer supplement")
	}
	latest := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "owner-media", reply.Message+".latest.private.json")
	if _, err := os.Stat(latest); !os.IsNotExist(err) {
		t.Fatal("missing anchor created a delivery success pointer")
	}
}

func TestOwnerReplyMediaPersistentFailureDoesNotStopCanonicalApplicationCheck(t *testing.T) {
	f, _, req, _ := ownerReplyMediaFixture(t)
	latest := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "owner-media", req.Message+".latest.private.json")
	if err := os.MkdirAll(filepath.Dir(latest), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(latest, []byte("invalid media receipt"), 0600); err != nil {
		t.Fatal(err)
	}
	// A malformed synthetic application input makes reaching the original
	// application check observable without fabricating an applied success.
	applied := filepath.Join(f.r.Config.Home, "applied", req.Message+".json")
	if err := os.MkdirAll(filepath.Dir(applied), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(applied, []byte("invalid application receipt"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		health := &Health{}
		f.r.processConversations(context.Background(), health)
		index, err := f.r.loadConversations()
		if err != nil {
			t.Fatal(err)
		}
		if index.Questions[f.q.RequestID].Phase != "answered" || !containsString(health.Errors, "conversation_owner_media_deferred") || !containsString(health.Errors, "conversation_application_deferred") || containsString(health.Errors, "conversation_delivery_deferred") {
			t.Fatalf("media failure stopped or misclassified canonical flow: phase=%s errors=%v", index.Questions[f.q.RequestID].Phase, health.Errors)
		}
	}
}

func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

func TestOwnerReplyMediaGeneratedScopeWorksThroughImageCLI(t *testing.T) {
	f, reply, req, live := ownerReplyMediaFixture(t)
	_, _, png := attachmentFixture(t, "ok")
	a := (*live)["attachments"].([]any)[0].(map[string]any)
	a["size"] = len(png)
	a["url"] = fmt.Sprintf("https://cdn.discordapp.com/attachments/%s/900/image.png?signature=not-persisted", f.q.Thread)
	a["content_type"] = "image/png"
	if err := f.r.deliverOwnerReplyMediaObservation(context.Background(), f.q, reply, req); err != nil {
		t.Fatal(err)
	}
	scopes, _ := filepath.Glob(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "owner-media", "*.scope.private.json"))
	if len(scopes) != 1 {
		t.Fatal("missing generated scope")
	}
	server(t, f.r, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/users/@me":
			respond(w, map[string]any{"id": "400", "bot": true})
		case "/channels/" + f.q.Thread:
			respond(w, map[string]any{"id": f.q.Thread, "guild_id": "100", "parent_id": "200", "owner_id": "400", "type": 11})
		case "/channels/" + f.q.Thread + "/messages/" + f.q.Thread:
			stamp, _ := time.Parse(time.RFC3339Nano, f.created)
			m := msg(stamp, f.q.Thread, "400", "synthetic image question")
			m.ID = f.q.Thread
			m.Author.Bot = true
			respond(w, m)
		case "/channels/" + f.q.Thread + "/messages/" + reply.Message:
			respond(w, *live)
		default:
			t.Errorf("unexpected image CLI route %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	mediaCalls, badAuth := 0, false
	f.r.Client = &http.Client{Timeout: time.Second, Transport: attachmentTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "cdn.discordapp.com" {
			mediaCalls++
			badAuth = r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != ""
			return &http.Response{StatusCode: 200, ContentLength: int64(len(png)), Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(png)), Request: r}, nil
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
	root, err := filepath.EvalSymlinks(f.r.Config.Home)
	if err != nil {
		t.Fatal(err)
	}
	scopePath, err := filepath.EvalSymlinks(scopes[0])
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(root, "cli-image.private.image")
	guardCalls := 0
	guard := func(_ context.Context, path string) bool {
		guardCalls++
		rel, err := filepath.Rel(root, path)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	var receipt bytes.Buffer
	loadMatches := false
	code := historyAttachmentCLIWith([]string{"--scope", scopePath, "--output", output}, &receipt, func(_ context.Context, scope historyReadScope) (*Runner, error) {
		loadMatches = scope.Todo == f.q.Todo && len(scope.Threads) == 1 && scope.Threads[0] == f.q.Thread
		return f.r, nil
	}, guard)
	got, err := os.ReadFile(output)
	if code != 0 || err != nil || !bytes.Equal(got, png) || !loadMatches || guardCalls < 2 || mediaCalls != 1 || badAuth {
		t.Fatalf("generated scope failed actual image CLI path: code=%d err=%v receipt=%s", code, err, receipt.String())
	}
}
