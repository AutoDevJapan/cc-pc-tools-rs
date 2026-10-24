package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type conversationCloseFixture struct {
	mu                                     sync.Mutex
	r                                      *Runner
	q                                      ConversationQuestion
	proof                                  conversationProof
	live                                   map[string]Message
	archived                               bool
	thread                                 map[string]any
	patches, posts, gets, proofReads       int
	requestErrors                          []string
	onPatch                                func()
	onProof                                func(int)
	onThread                               func(int)
	threadGets                             int
	losePatch, failAfterPatch, ignorePatch bool
	patchStatus                            int
	redirect                               string
	redirectOnlyPatch                      bool
}

func conversationCloseTest(t *testing.T) *conversationCloseFixture {
	t.Helper()
	r, start := fixture(t)
	r.Now = func() time.Time { return start.Add(2 * time.Minute) }
	question := snowflake(start.Add(2 * time.Second))
	decision := json.RawMessage(`{"todo_id":42,"decision":"direction","scope":{"specification":"fixture"}}`)
	f := &conversationCloseFixture{r: r, live: map[string]Message{}, patchStatus: 200}
	f.q = ConversationQuestion{RequestID: "close-question", SenderID: "conversation-v1-close", Todo: 42, Project: "cc-pc-tools-rs", OriginThread: "510", Thread: question, Message: question, Progress: "610", Fingerprint: strings.Repeat("a", 64), ReceiptSHA: strings.Repeat("b", 64), DecisionSHA: canonicalHash(decision), QuestionSHA: bytesHash([]byte("aかbを選んでください。"))}
	f.proof = conversationProof{Domain: "new-owner-recovery-messages", ReadOnly: true, Source: "discord-api-v10", RequestID: f.q.SenderID, Thread: question, Owner: r.State.Destination.Owner, Question: question, Created: start.Add(2 * time.Second).Format(time.RFC3339Nano), Progress: f.q.Progress, Body: "aかbを選んでください。", Decision: decision, Replies: []conversationReply{}}
	f.thread = map[string]any{"id": question, "guild_id": r.State.Destination.Guild, "parent_id": r.State.Destination.Forum, "owner_id": r.State.Destination.Bot, "type": 11}
	r.JobsPath = filepath.Join(r.Config.Home, "test-jobs")
	if publish(filepath.Join(r.JobsPath, "worker-42", "job.json"), map[string]any{"id": "worker-42", "todo_id": 42, "cwd": r.Config.Home}) != nil {
		t.Fatal("job fixture unavailable")
	}
	if publish(r.TodoPath, map[string]any{"items": []any{map[string]any{"id": 42, "project": "cc-pc-tools-rs", "status": "doing", "jobs": []string{"worker-42"}, "scheduler": map[string]any{"worktree": r.Config.Home}}}}) != nil {
		t.Fatal("could not create private task fixture")
	}
	for _, name := range []string{"state.json", "recovery-messages.json", "recovery-close.json"} {
		if os.WriteFile(filepath.Join(r.Config.Home, name), []byte("legacy-sentinel"), 0600) != nil {
			t.Fatal("could not create private preservation fixture")
		}
	}
	r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if path != conversationSender || len(args) == 0 || args[0] != "recovery-replies" {
			return "", errors.New("unexpected legacy mutation or subprocess")
		}
		f.mu.Lock()
		f.proofReads++
		count, hook := f.proofReads, f.onProof
		f.mu.Unlock()
		if hook != nil {
			hook(count)
		}
		f.mu.Lock()
		b, err := json.Marshal(f.proof)
		f.mu.Unlock()
		return string(b), err
	}
	s := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(s.Close)
	r.Base = s.URL
	r.Client = s.Client()
	return f
}

func (f *conversationCloseFixture) serve(w http.ResponseWriter, request *http.Request) {
	f.mu.Lock()
	if request.Header.Get("Authorization") != "Bot fixture-token" || request.Header.Get("User-Agent") != "DiscordBot (https://github.com/AutoDevJapan/cc-pc-tools-rs, 0.1.0)" || request.URL.RawQuery != "" {
		f.requestErrors = append(f.requestErrors, "request credential placement or user agent differs")
	}
	if request.Method == http.MethodPost {
		f.posts++
	}
	if request.Method == http.MethodGet {
		f.gets++
	}
	if f.redirect != "" && (!f.redirectOnlyPatch || request.Method == http.MethodPatch) {
		if request.Method == http.MethodPatch {
			f.patches++
		}
		to := f.redirect
		f.mu.Unlock()
		http.Redirect(w, request, to, http.StatusTemporaryRedirect)
		return
	}
	if request.URL.Path == "/channels/"+f.q.Thread && request.Method == http.MethodPatch {
		f.patches++
		b, err := io.ReadAll(request.Body)
		if err != nil || string(b) != `{"archived":true}` || request.Header.Get("Content-Type") != "application/json" {
			f.requestErrors = append(f.requestErrors, "archive PATCH changed another field")
		}
		var record conversationCloseRecord
		if readJSON(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "close-state.json"), maxState, &record) != nil || record.Phase != "patch_pending" {
			f.requestErrors = append(f.requestErrors, "archive intent was not durable before PATCH")
		}
		if !f.ignorePatch {
			f.archived = true
		}
		hook, lose, status := f.onPatch, f.losePatch, f.patchStatus
		f.losePatch = false
		f.mu.Unlock()
		if hook != nil {
			hook()
		}
		if lose {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = connection.Close()
			}
			return
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"api_only":"not archive verification"}`)
		return
	}
	if request.URL.Path == "/channels/"+f.q.Thread && request.Method == http.MethodGet {
		f.threadGets++
		hook, count := f.onThread, f.threadGets
		if f.failAfterPatch && f.patches > 0 {
			f.failAfterPatch = false
			f.mu.Unlock()
			w.WriteHeader(503)
			return
		}
		f.mu.Unlock()
		if hook != nil {
			hook(count)
		}
		f.mu.Lock()
		thread := map[string]any{}
		for key, value := range f.thread {
			thread[key] = value
		}
		thread["thread_metadata"] = map[string]any{"archived": f.archived}
		b, _ := json.Marshal(thread)
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
		return
	}
	for id, message := range f.live {
		if request.URL.Path == "/channels/"+f.q.Thread+"/messages/"+id && request.Method == http.MethodGet {
			b, _ := json.Marshal(message)
			f.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(b)
			return
		}
	}
	f.requestErrors = append(f.requestErrors, "unexpected HTTP operation")
	f.mu.Unlock()
	w.WriteHeader(404)
}

func (f *conversationCloseFixture) current() *conversationProof {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, _ := json.Marshal(f.proof)
	var proof conversationProof
	_ = json.Unmarshal(b, &proof)
	return &proof
}

func (f *conversationCloseFixture) addReply(t *testing.T, text string, applied bool) conversationReply {
	t.Helper()
	f.mu.Lock()
	created, _ := time.Parse(time.RFC3339Nano, f.proof.Created)
	created = created.Add(time.Duration(8+len(f.proof.Replies)) * time.Second)
	reply := conversationReply{RequestID: f.q.SenderID, Todo: f.q.Todo, Thread: f.q.Thread, Question: f.q.Message, Author: f.r.State.Destination.Owner, Message: snowflake(created), Created: created.Format(time.RFC3339Nano), Body: text, Decision: f.proof.Decision}
	f.proof.Replies = append(f.proof.Replies, reply)
	message := msg(created, f.q.Thread, reply.Author, text)
	message.ID = reply.Message
	f.live[reply.Message] = message
	f.mu.Unlock()
	f.capture(t, reply.Message)
	if applied {
		f.apply(t, reply.Message)
	}
	return reply
}

func (f *conversationCloseFixture) capture(t *testing.T, id string) {
	t.Helper()
	proof := f.current()
	var reply conversationReply
	for _, value := range proof.Replies {
		if value.Message == id {
			reply = value
		}
	}
	f.mu.Lock()
	live := f.live[id]
	f.mu.Unlock()
	contextBytes, _ := json.Marshal(proof)
	req := &Request{Thread: f.q.Thread, Guild: f.r.State.Destination.Guild, RecordVersion: 2, Message: id, Author: reply.Author, Created: reply.Created, Edited: live.Edited, Acquired: f.r.Now().Add(-5 * time.Second).Format(time.RFC3339Nano), Source: "discord-api-v10", Text: reply.Body, Context: string(contextBytes), Key: requestKey(f.q.Thread, id), Phase: "conversation_reply_saved", ProgressThread: f.q.Progress, Conversation: f.q.RequestID}
	if publish(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", id+".json"), req) != nil {
		t.Fatal("could not save private source fixture")
	}
	req.Todo = f.q.Todo
	req.Phase = "conversation_reply_delivered"
	f.mu.Lock()
	f.r.State.Requests[id] = req
	f.mu.Unlock()
}

func (f *conversationCloseFixture) apply(t *testing.T, id string) {
	t.Helper()
	var reply conversationReply
	for _, value := range f.current().Replies {
		if value.Message == id {
			reply = value
		}
	}
	source, err := os.ReadFile(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", id+".json"))
	if err != nil {
		t.Fatal("private source fixture unavailable")
	}
	application := map[string]any{"source": "discord-api-v10", "request_id": f.q.SenderID, "origin_todo_id": f.q.Todo, "todo_id": f.q.Todo, "thread_id": f.q.Thread, "question_message_id": f.q.Message, "message_id": id, "author_id": reply.Author, "job_id": "worker-42", "revision": strings.Repeat("e", 40), "applied_at": f.r.Now().Format(time.RFC3339Nano), "completion": "設定を反映して読み返しました。", "reply_sha256": canonicalHash(reply), "source_receipt_sha256": bytesHash(source), "verification": map[string]string{"execution": "readback passed", "origin_saved": "original scope preserved", "identity": "API and owner matched"}}
	if publish(filepath.Join(f.r.Config.Home, "applied", id+".json"), application) != nil {
		t.Fatal("could not save private implementation fixture")
	}
}

func (f *conversationCloseFixture) edit(id, text string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for index, reply := range f.proof.Replies {
		if reply.Message == id {
			f.proof.Replies[index].Body = text
		}
	}
	message := f.live[id]
	message.Content = text
	message.Edited = f.r.Now().Add(-10 * time.Second).Format(time.RFC3339Nano)
	f.live[id] = message
}

func (f *conversationCloseFixture) record(t *testing.T) conversationCloseRecord {
	t.Helper()
	var record conversationCloseRecord
	if readJSON(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "close-state.json"), maxState, &record) != nil {
		t.Fatal("close receipt unavailable")
	}
	return record
}

func (f *conversationCloseFixture) check(t *testing.T, patches int) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.patches != patches || f.posts != 0 || len(f.requestErrors) != 0 {
		t.Fatal("archive HTTP count, fields, authorization or pre-PATCH persistence differ")
	}
	for _, name := range []string{"state.json", "recovery-messages.json", "recovery-close.json"} {
		b, err := os.ReadFile(filepath.Join(f.r.Config.Home, name))
		if err != nil || string(b) != "legacy-sentinel" {
			t.Fatal("legacy source was modified")
		}
	}
}

func TestConversationCloseSingleAndMultipleRepliesRequireEveryCurrentImplementation(t *testing.T) {
	for _, count := range []int{1, 2} {
		t.Run("all_replies", func(t *testing.T) {
			f := conversationCloseTest(t)
			for i := 0; i < count; i++ {
				f.addReply(t, "現在の回答です", true)
			}
			closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
			if err != nil || !closed {
				t.Fatal("current implementation receipts did not close the question")
			}
			record := f.record(t)
			if record.Domain != conversationCloseDomain || record.Phase != "verified" || len(record.Replies) != count || record.VerifiedAt == "" || record.VerifiedScope == "" {
				t.Fatal("verified close receipt lost its version scope")
			}
			for _, reply := range f.current().Replies {
				if f.r.State.Requests[reply.Message].Phase != "conversation_reply_applied" {
					t.Fatal("individual applied reply was not saved")
				}
			}
			closed, err = f.r.closeConversation(context.Background(), f.q, f.current())
			if err != nil || !closed {
				t.Fatal("verified unchanged archive did not reconcile")
			}
			f.check(t, 1)
		})
	}
}

func TestConversationClosePendingReplyStillSavesOtherIndividualApplications(t *testing.T) {
	f := conversationCloseTest(t)
	first := f.addReply(t, "最初の回答", false)
	second := f.addReply(t, "追加の回答", true)
	closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
	if err != nil || closed || f.r.State.Requests[first.Message].Phase != "conversation_reply_delivered" || f.r.State.Requests[second.Message].Phase != "conversation_reply_applied" {
		t.Fatal("pending reply was closed or valid individual application was lost")
	}
	if f.record(t).Phase != "pending" {
		t.Fatal("pending work was not retained")
	}
	f.apply(t, first.Message)
	closed, err = f.r.closeConversation(context.Background(), f.q, f.current())
	if err != nil || !closed {
		t.Fatal("second application did not unblock closing")
	}
	f.check(t, 1)
}

func TestConversationCloseEditedReplyNeedsBothNewVersionHashes(t *testing.T) {
	f := conversationCloseTest(t)
	reply := f.addReply(t, "元の回答", true)
	f.edit(reply.Message, "編集した回答")
	f.capture(t, reply.Message)
	closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
	if err != nil || closed {
		t.Fatal("old implementation was reused for an edited reply")
	}
	f.check(t, 0)
	f.apply(t, reply.Message)
	closed, err = f.r.closeConversation(context.Background(), f.q, f.current())
	if err != nil || !closed {
		t.Fatal("current edited reply with both hashes could not close")
	}
	f.check(t, 1)
}

func TestConversationCloseLostPatchOrReadbackRecoversByGETWithoutAnotherPatch(t *testing.T) {
	for _, mode := range []string{"response_lost", "readback_unavailable"} {
		t.Run(mode, func(t *testing.T) {
			f := conversationCloseTest(t)
			f.addReply(t, "現在の回答", true)
			f.losePatch = mode == "response_lost"
			f.failAfterPatch = mode == "readback_unavailable"
			closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
			if closed || err == nil || f.record(t).Phase != "patch_pending" {
				t.Fatal("uncertain archive was reported verified or lost its intent")
			}
			closed, err = f.r.closeConversation(context.Background(), f.q, f.current())
			if err != nil || !closed || f.record(t).Phase != "verified" {
				t.Fatal("GET did not recover the archived question")
			}
			f.check(t, 1)
		})
	}
}

func TestConversationCloseSuccessResponseAloneDoesNotProveArchive(t *testing.T) {
	f := conversationCloseTest(t)
	f.addReply(t, "現在の回答", true)
	f.ignorePatch = true
	closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
	if err != nil || closed || f.record(t).Phase != "pending" {
		t.Fatal("PATCH success was mistaken for an observed archive")
	}
	f.check(t, 1)
}

func TestConversationCloseVerifiedManualReopenIsNeverPatchedAgain(t *testing.T) {
	f := conversationCloseTest(t)
	f.addReply(t, "現在の回答", true)
	if closed, err := f.r.closeConversation(context.Background(), f.q, f.current()); err != nil || !closed {
		t.Fatal("initial close failed")
	}
	f.mu.Lock()
	f.archived = false
	f.mu.Unlock()
	for i := 0; i < 2; i++ {
		closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
		if err != nil || closed || f.record(t).Phase != "reopened" {
			t.Fatal("manual reopen was silently closed again")
		}
	}
	f.check(t, 1)
}

func TestConversationCloseReplyOrImplementationRaceStaysPending(t *testing.T) {
	for _, mode := range []string{"new_before_patch", "new_after_patch", "edit_after_patch", "applied_after_patch"} {
		t.Run(mode, func(t *testing.T) {
			f := conversationCloseTest(t)
			reply := f.addReply(t, "現在の回答", true)
			add := func() { f.addReply(t, "追加の回答", false) }
			switch mode {
			case "new_before_patch":
				f.onProof = func(count int) {
					if count == 1 {
						add()
					}
				}
			case "new_after_patch":
				f.onPatch = add
			case "edit_after_patch":
				f.onPatch = func() { f.edit(reply.Message, "編集された回答") }
			case "applied_after_patch":
				f.onPatch = func() {
					path := filepath.Join(f.r.Config.Home, "applied", reply.Message+".json")
					b, _ := os.ReadFile(path)
					_ = os.WriteFile(path, append(b, '\n'), 0600)
				}
			}
			closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
			if err != nil || closed || f.record(t).Phase != "pending" {
				t.Fatal("changed owner reply set, edit or applied file was treated as the verified scope")
			}
			patches := 1
			if mode == "new_before_patch" {
				patches = 0
			}
			f.check(t, patches)
		})
	}
}

func TestConversationCloseForeignOwnerOrThreadIsRejected(t *testing.T) {
	for _, field := range []string{"reply_owner", "live_owner", "thread_owner", "guild", "parent", "type"} {
		t.Run(field, func(t *testing.T) {
			f := conversationCloseTest(t)
			reply := f.addReply(t, "現在の回答", true)
			switch field {
			case "reply_owner":
				f.proof.Replies[0].Author = "999"
			case "live_owner":
				live := f.live[reply.Message]
				live.Author.ID = "999"
				f.live[reply.Message] = live
			case "thread_owner":
				f.thread["owner_id"] = "999"
			case "guild":
				f.thread["guild_id"] = "999"
			case "parent":
				f.thread["parent_id"] = "999"
			case "type":
				f.thread["type"] = 12
			}
			closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
			if err == nil || closed {
				t.Fatal("foreign owner or thread association was accepted")
			}
			f.check(t, 0)
		})
	}
}

func TestConversationCloseRedirectNeverForwardsCredentialOrPatch(t *testing.T) {
	f := conversationCloseTest(t)
	f.addReply(t, "現在の回答", true)
	var mu sync.Mutex
	redirected := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mu.Lock()
		redirected++
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer other.Close()
	f.redirect = other.URL
	closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
	if closed || err == nil {
		t.Fatal("redirected source was accepted")
	}
	mu.Lock()
	count := redirected
	mu.Unlock()
	if count != 0 || f.r.Client.CheckRedirect != nil {
		t.Fatal("redirect escaped the clone or changed the caller client")
	}
	f.check(t, 0)
}

func TestConversationCloseMalformedExistingReceiptIsNeverInitialized(t *testing.T) {
	for _, data := range []string{"{", "null", "{}", `{"domain":"wrong"}`} {
		t.Run("malformed", func(t *testing.T) {
			f := conversationCloseTest(t)
			f.addReply(t, "現在の回答", true)
			path := filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "close-state.json")
			if os.WriteFile(path, []byte(data), 0600) != nil {
				t.Fatal("could not save malformed private close fixture")
			}
			closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
			b, _ := os.ReadFile(path)
			if err == nil || closed || string(b) != data {
				t.Fatal("malformed existing close state was reinitialized")
			}
			f.check(t, 0)
		})
	}
}

func (f *conversationCloseFixture) changeApplication(t *testing.T, id string, change func(map[string]any)) {
	t.Helper()
	path := filepath.Join(f.r.Config.Home, "applied", id+".json")
	var value map[string]any
	if readJSON(path, 128<<10, &value) != nil {
		t.Fatal("private implementation fixture unavailable")
	}
	change(value)
	if publish(path, value) != nil {
		t.Fatal("private implementation fixture could not be changed")
	}
}

func TestConversationCloseOldOrInvalidImplementationNeverCountsAsApplied(t *testing.T) {
	for _, field := range []string{"reply_sha256", "source_receipt_sha256", "job_id", "revision", "applied_before_capture", "applied_in_future", "source", "request_id", "origin_todo_id", "todo_id", "thread_id", "question_message_id", "message_id", "author_id", "completion", "verification"} {
		t.Run(field, func(t *testing.T) {
			f := conversationCloseTest(t)
			reply := f.addReply(t, "現在の回答", true)
			f.changeApplication(t, reply.Message, func(value map[string]any) {
				switch field {
				case "reply_sha256", "source_receipt_sha256":
					delete(value, field)
				case "origin_todo_id", "todo_id":
					value[field] = 99
				case "revision":
					value[field] = strings.Repeat("g", 40)
				case "completion":
					value[field] = " "
				case "verification":
					value[field] = map[string]string{"execution": "readback", "origin_saved": "kept", "identity": " "}
				case "applied_before_capture":
					value["applied_at"] = f.r.Now().Add(-time.Minute).Format(time.RFC3339Nano)
				case "applied_in_future":
					value["applied_at"] = f.r.Now().Add(time.Nanosecond).Format(time.RFC3339Nano)
				default:
					value[field] = "foreign-receipt"
				}
			})
			closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
			if closed || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
				t.Fatal("old or invalid implementation receipt was accepted")
			}
			if field == "reply_sha256" || field == "source_receipt_sha256" {
				if err != nil || f.record(t).Phase != "pending" {
					t.Fatal("old schema was not retained as pending implementation")
				}
			} else if err == nil {
				t.Fatal("foreign or invalid implementation evidence was not rejected")
			}
			f.check(t, 0)
		})
	}
}

func TestConversationCloseCaptureOrMissingArtifactsRemainPending(t *testing.T) {
	for _, mode := range []string{"request_missing", "capture_only", "source_missing", "applied_missing"} {
		t.Run(mode, func(t *testing.T) {
			f := conversationCloseTest(t)
			reply := f.addReply(t, "現在の回答", true)
			switch mode {
			case "request_missing":
				delete(f.r.State.Requests, reply.Message)
			case "capture_only":
				f.r.State.Requests[reply.Message].Phase = "conversation_reply_saved"
			case "source_missing":
				if os.Remove(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+".json")) != nil {
					t.Fatal("private source removal failed")
				}
			case "applied_missing":
				if os.Remove(filepath.Join(f.r.Config.Home, "applied", reply.Message+".json")) != nil {
					t.Fatal("private implementation removal failed")
				}
			}
			closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
			if err != nil || closed || f.record(t).Phase != "pending" {
				t.Fatal("capture or missing evidence was treated as applied")
			}
			f.check(t, 0)
		})
	}
}

func TestConversationCloseSameTextEditStillRequiresNewSourceReceiptHash(t *testing.T) {
	f := conversationCloseTest(t)
	reply := f.addReply(t, "現在の回答", true)
	f.edit(reply.Message, reply.Body)
	f.capture(t, reply.Message)
	closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
	if err != nil || closed {
		t.Fatal("same-text edit reused an earlier source receipt")
	}
	f.check(t, 0)
	f.apply(t, reply.Message)
	closed, err = f.r.closeConversation(context.Background(), f.q, f.current())
	if err != nil || !closed {
		t.Fatal("new source metadata with current application could not close")
	}
	f.check(t, 1)
}

func TestConversationCloseAppliedFileChangedImmediatelyBeforeOrAfterPatchStaysPending(t *testing.T) {
	for _, when := range []int{1, 3} {
		t.Run("changed_private_hash", func(t *testing.T) {
			f := conversationCloseTest(t)
			reply := f.addReply(t, "現在の回答", true)
			f.onThread = func(count int) {
				if count == when {
					path := filepath.Join(f.r.Config.Home, "applied", reply.Message+".json")
					b, _ := os.ReadFile(path)
					_ = os.WriteFile(path, append(b, '\n'), 0600)
				}
			}
			closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
			if err != nil || closed || f.record(t).Phase != "pending" {
				t.Fatal("changed private implementation bytes were accepted across archive verification")
			}
			patches := 0
			if when == 3 {
				patches = 1
			}
			f.check(t, patches)
		})
	}
}

func TestConversationClosePhaseSaveFailureDoesNotLeaveUnsavedApplicationInMemory(t *testing.T) {
	f := conversationCloseTest(t)
	reply := f.addReply(t, "現在の回答", true)
	statePath := filepath.Join(f.r.Config.Home, "unattended-state.json")
	if os.Mkdir(statePath, 0700) != nil {
		t.Fatal("could not create failing state target")
	}
	closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
	if err == nil || closed || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
		t.Fatal("failed state persistence left an applied phase in memory")
	}
	f.check(t, 0)
	if os.Remove(statePath) != nil {
		t.Fatal("could not restore fixture state target")
	}
	closed, err = f.r.closeConversation(context.Background(), f.q, f.current())
	if err != nil || !closed {
		t.Fatal("state persistence failure could not recover")
	}
	f.check(t, 1)
}

func TestConversationClosePatchRedirectIsNotFollowed(t *testing.T) {
	f := conversationCloseTest(t)
	f.addReply(t, "現在の回答", true)
	var mu sync.Mutex
	forwarded := 0
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		mu.Lock()
		forwarded++
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer other.Close()
	f.redirect, f.redirectOnlyPatch = other.URL, true
	closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
	mu.Lock()
	count := forwarded
	mu.Unlock()
	if err == nil || closed || count != 0 || f.record(t).Phase != "patch_pending" {
		t.Fatal("archive redirect was followed or reported verified")
	}
	f.check(t, 1)
}

func TestConversationCloseAlreadyArchivedFreshScopeRequiresNoPatch(t *testing.T) {
	f := conversationCloseTest(t)
	f.addReply(t, "現在の回答", true)
	f.archived = true
	closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
	if err != nil || !closed {
		t.Fatal("GET-confirmed archive was not verified")
	}
	additional := f.addReply(t, "追加の回答", false)
	closed, err = f.r.closeConversation(context.Background(), f.q, f.current())
	if err != nil || closed {
		t.Fatal("additional reply inherited a previous applied scope")
	}
	f.apply(t, additional.Message)
	closed, err = f.r.closeConversation(context.Background(), f.q, f.current())
	if err != nil || !closed {
		t.Fatal("fresh applied scope in an already archived thread remained stuck")
	}
	f.check(t, 0)
}

func TestConversationCloseCanceledContextDoesNotPatchOrLeakDiagnostics(t *testing.T) {
	f := conversationCloseTest(t)
	f.addReply(t, "現在の回答", true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	closed, err := f.r.closeConversation(ctx, f.q, f.current())
	if closed || err == nil || strings.Contains(err.Error(), f.r.Token) || strings.Contains(err.Error(), f.r.Config.Home) || strings.Contains(err.Error(), f.r.Base) {
		t.Fatal("canceled verification leaked diagnostics or succeeded")
	}
	f.check(t, 0)
}

func TestConversationCloseRequiresExactOriginalProject(t *testing.T) {
	for _, project := range []string{"", "other-project", "owner"} {
		t.Run("project_binding", func(t *testing.T) {
			f := conversationCloseTest(t)
			f.addReply(t, "現在の回答", true)
			f.q.Project = project
			closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
			if err == nil || closed {
				t.Fatal("missing or different project was allowed to close")
			}
			f.check(t, 0)
		})
	}
}

func TestConversationCloseOriginalDecisionChangeCannotReuseSourceOrPrePatchScope(t *testing.T) {
	for _, afterPatch := range []bool{false, true} {
		t.Run("original_decision", func(t *testing.T) {
			f := conversationCloseTest(t)
			f.proof.OriginalDecision = json.RawMessage(`{"todo_id":42,"scope":{"specification":"original"}}`)
			f.addReply(t, "現在の回答", true)
			change := func() {
				f.mu.Lock()
				f.proof.OriginalDecision = json.RawMessage(`{"todo_id":42,"scope":{"specification":"different"}}`)
				f.mu.Unlock()
			}
			if afterPatch {
				f.onPatch = change
			} else {
				change()
			}
			closed, err := f.r.closeConversation(context.Background(), f.q, f.current())
			if closed || err == nil {
				t.Fatal("different original decision reused the captured source scope")
			}
			patches := 0
			if afterPatch {
				patches = 1
			}
			f.check(t, patches)
		})
	}
}
