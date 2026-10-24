package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type conversationFixture struct {
	r             *Runner
	input         ConversationAskInput
	question      string
	created       string
	decision      json.RawMessage
	replies       []conversationReply
	commands      []string
	posts         int
	close         int
	lost          bool
	deny          bool
	q             ConversationQuestion
	answerFailure bool
	waitFailure   bool
	edited        map[string]string
	archived      bool
}

func conversationTest(t *testing.T, wait bool) *conversationFixture {
	t.Helper()
	r, c := fixture(t)
	f := &conversationFixture{r: r, question: snowflake(c.Add(2 * time.Second)), created: c.Add(2 * time.Second).Format(time.RFC3339Nano), decision: json.RawMessage(`{"action":"choose","todo_id":42}`)}
	f.input = ConversationAskInput{RequestID: "current-question", Todo: 42, OriginThread: "510", BodyFile: filepath.Join(r.Config.Home, "input.txt"), DecisionFile: filepath.Join(r.Config.Home, "input.json"), Title: "確認", Wait: wait}
	if err := os.WriteFile(f.input.BodyFile, []byte("aかbを選んでください。"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.input.DecisionFile, f.decision, 0600); err != nil {
		t.Fatal(err)
	}
	if err := publish(r.TodoPath, map[string]any{"projects": map[string]string{"cc-pc-tools-rs": r.Config.Home}, "items": []any{map[string]any{"id": 42, "project": "cc-pc-tools-rs", "status": "doing", "note": "以前の作業を保持", "spec": "unchanged-original-spec", "jobs": []string{"worker-42"}}}}); err != nil {
		t.Fatal(err)
	}
	r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if len(args) == 0 {
			return "", errors.New("missing command")
		}
		f.commands = append(f.commands, args[0])
		if strings.Contains(path, "scrub391") {
			if f.deny {
				return `{"client_resource":true}`, nil
			}
			return `{"client_resource":false,"classification_unverified":false}`, nil
		}
		switch args[0] {
		case "recovery-ask":
			if f.posts == 0 {
				f.posts++
			}
			if f.lost {
				f.lost = false
				return "", errors.New("pipe result lost after posted")
			}
			value := map[string]any{"domain": "new-owner-recovery-messages", "delivery": "verified", "request_id": "conversation-v1-" + bytesHash([]byte(f.input.RequestID))[:32], "thread_id": f.question, "message_id": f.question, "progress_thread_id": "610", "original_fingerprint": strings.Repeat("a", 64), "original_question": "aかbを選んでください。", "decision": f.decision}
			b, _ := json.Marshal(value)
			return string(b), nil
		case "recovery-replies":
			var sender string
			for i := 1; i+1 < len(args); i++ {
				if args[i] == "--request-id" {
					sender = args[i+1]
				}
			}
			versions := map[string]conversationReplyVersion{}
			for _, reply := range f.replies {
				v := conversationReplyVersion{}
				if edited, ok := f.edited[reply.Message]; ok {
					v.Edited = &edited
				}
				versions[reply.Message] = v
			}
			b, _ := json.Marshal(conversationProof{Domain: "new-owner-recovery-messages", ReadOnly: true, Source: "discord-api-v10", RequestID: sender, Thread: f.question, Owner: r.State.Destination.Owner, Question: f.question, Created: f.created, Progress: "610", Body: "aかbを選んでください。", Decision: f.decision, Replies: f.replies, ReplyVersions: versions})
			return string(b), nil
		case "question-wait":
			if f.waitFailure {
				f.waitFailure = false
				return "", errors.New("canonical wait lock busy")
			}
			b, err := os.ReadFile(args[3])
			if err != nil {
				return "", err
			}
			var question map[string]any
			if json.Unmarshal(b, &question) != nil {
				return "", errors.New("invalid fixture question")
			}
			f.mutateTodo(t, func(row map[string]any) {
				row["status"] = "block"
				row["owner_question_wait"] = map[string]any{"question": question, "question_receipt_sha256": bytesHash(b)}
				row["note"] = row["note"].(string) + fmt.Sprintf("\n[discord-conversation-question-v1 request_id=%s sha256=%s]", question["request_id"], bytesHash(b))
			})
			return "", nil
		case "edit":
			b, err := os.ReadFile(args[3])
			if err != nil {
				return "", err
			}
			row, err := r.todo(42)
			if err != nil || len(args) != 10 || args[4] != "--expected-note-sha256" || bytesHash([]byte(row.Note)) != args[5] || args[6] != "--scope-file" || args[8] != "--expected-state-file" {
				return "", errors.New("conversation_note_changed")
			}
			f.mutateTodo(t, func(row map[string]any) { row["note"] = string(b) })
			return "", nil
		case "question-answer":
			if f.answerFailure {
				f.answerFailure = false
				return "", errors.New("canonical answer failed before apply")
			}
			var proof conversationAnswerReceipt
			if readJSON(args[3], 1<<20, &proof) != nil || proof.Todo != "42" || proof.Message == "" || proof.Author != r.State.Destination.Owner || proof.QuestionSHA != f.q.ReceiptSHA {
				return "", errors.New("wrong answer association")
			}
			if argValue(args, "--delivery-note-file") != "" {
				data, err := os.ReadFile(argValue(args, "--delivery-note-file"))
				if err != nil {
					return "", err
				}
				var expected map[string]any
				if readJSON(argValue(args, "--expected-state-file"), maxTodo, &expected) != nil {
					return "", errors.New("missing delivery snapshot")
				}
				var db map[string]any
				if readJSON(r.TodoPath, maxTodo, &db) != nil {
					return "", errors.New("missing fixture task")
				}
				current := db["items"].([]any)[0].(map[string]any)
				if canonicalHash(current) != canonicalHash(expected) || !strings.HasPrefix(string(data), current["note"].(string)+"\n\n") {
					return "", errors.New("conversation_note_changed")
				}
				f.mutateTodo(t, func(row map[string]any) {
					wait := row["owner_question_wait"]
					if wait == nil {
						wait = row["owner_question_answer_receipts"].([]any)[0].(map[string]any)["wait"]
					}
					var saved []any
					if row["owner_question_delivery_receipts"] != nil {
						saved = row["owner_question_delivery_receipts"].([]any)
					}
					b, _ := os.ReadFile(args[3])
					row["owner_question_delivery_receipts"] = append(saved, map[string]any{"domain": "todo-owner-question-delivery-receipt-v1", "answer": proof, "answer_file_sha256": bytesHash(b), "wait": wait})
					row["note"] = string(data)
					if row["scheduler"] == nil {
						row["scheduler"] = map[string]any{}
					}
					row["scheduler"].(map[string]any)["owner_instruction"] = string(data)
				})
				return "", nil
			}
			f.mutateTodo(t, func(row map[string]any) {
				wait := row["owner_question_wait"]
				marker := fmt.Sprintf("\n[discord-conversation-question-v1 request_id=%s sha256=%s]", proof.RequestID, proof.QuestionSHA)
				row["note"] = strings.Replace(row["note"].(string), marker, "", 1)
				row["status"] = "todo"
				delete(row, "owner_question_wait")
				b, _ := os.ReadFile(args[3])
				row["owner_question_answer_receipts"] = []any{map[string]any{"answer": proof, "answer_file_sha256": bytesHash(b), "wait": wait}}
				if row["scheduler"] == nil {
					row["scheduler"] = map[string]any{}
				}
				row["scheduler"].(map[string]any)["owner_instruction"] = row["note"]
			})
			return "", nil
		case "recovery-log":
			b, _ := json.Marshal(map[string]any{"domain": "new-owner-recovery-messages", "delivery": "verified", "thread_id": "610", "message_id": "710", "url": "https://discord.com/channels/100/610", "reused": false})
			return string(b), nil
		case "recovery-close":
			f.close++
			b, _ := json.Marshal(map[string]any{"thread_id": f.question, "outcome": "archived"})
			return string(b), nil
		default:
			return "", fmt.Errorf("unexpected command %s", args[0])
		}
	}
	server(t, r, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/channels/"+f.question {
			if req.Method == http.MethodPatch {
				f.close++
				f.archived = true
			}
			respond(w, map[string]any{"id": f.question, "guild_id": "100", "parent_id": "200", "owner_id": "400", "type": 11, "thread_metadata": map[string]any{"archived": f.archived}})
			return
		}
		for _, reply := range f.replies {
			if req.URL.Path == "/channels/"+f.question+"/messages/"+reply.Message {
				m := msg(c.Add(10*time.Second), f.question, r.State.Destination.Owner, reply.Body)
				m.ID = reply.Message
				m.Timestamp = reply.Created
				m.Edited = f.edited[reply.Message]
				respond(w, m)
				return
			}
		}
		t.Errorf("unexpected request %s", req.URL.Path)
		w.WriteHeader(404)
	})
	return f
}

func (f *conversationFixture) mutateTodo(t *testing.T, change func(map[string]any)) {
	t.Helper()
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("todo fixture unavailable")
	}
	change(db["items"].([]any)[0].(map[string]any))
	if err := publish(f.r.TodoPath, db); err != nil {
		t.Fatal(err)
	}
}

func (f *conversationFixture) ask(t *testing.T) ConversationQuestion {
	t.Helper()
	q, err := f.r.conversationAsk(context.Background(), f.input)
	if err != nil {
		t.Fatal(err)
	}
	f.q = q
	return q
}

func (f *conversationFixture) reply(t *testing.T, seconds int, text string) conversationReply {
	t.Helper()
	stamp, _ := time.Parse(time.RFC3339Nano, f.created)
	stamp = stamp.Add(time.Duration(seconds) * time.Second)
	reply := conversationReply{RequestID: f.q.SenderID, Todo: 42, Thread: f.question, Question: f.question, Author: f.r.State.Destination.Owner, Message: snowflake(stamp), Created: stamp.Format(time.RFC3339Nano), Body: text, Decision: f.decision}
	f.replies = append(f.replies, reply)
	return reply
}

func TestConversationNewQuestionDoesNotRequireLostLegacyState(t *testing.T) {
	f := conversationTest(t, true)
	legacy := filepath.Join(filepath.Dir(f.r.Config.Home), "state.json")
	before, _ := os.ReadFile(legacy)
	q := f.ask(t)
	if q.Phase != "waiting" || f.posts != 1 || q.Todo != 42 || q.Thread != f.question || q.Progress != "610" || q.ReceiptSHA == "" {
		t.Fatalf("question not verified: %+v", q)
	}
	row, err := f.r.todo(42)
	if err != nil || row.Status != "block" || !strings.HasPrefix(row.Note, "以前の作業を保持\n") || !conversationWaitUnchanged(row, q) || string(row.Spec) != `"unchanged-original-spec"` || len(row.Jobs) != 1 {
		t.Fatal("original task changed beyond wait")
	}
	after, _ := os.ReadFile(legacy)
	if string(before) != string(after) {
		t.Fatal("legacy state modified")
	}
	public, _ := json.Marshal(q)
	if strings.Contains(string(public), "aかb") || strings.Contains(string(public), "fixture-token") {
		t.Fatal("private question leaked")
	}
}

func TestConversationLostPostResultRetriesSameIdentityWithoutNewQuestion(t *testing.T) {
	f := conversationTest(t, false)
	f.lost = true
	if _, err := f.r.conversationAsk(context.Background(), f.input); err == nil {
		t.Fatal("lost result reported as verified")
	}
	index, err := f.r.loadConversations()
	if err != nil || index.Questions[f.input.RequestID].Phase != "reserved" {
		t.Fatal("intent not retained")
	}
	q := f.ask(t)
	if q.Phase != "posted" || f.posts != 1 {
		t.Fatal("POST result loss duplicated question")
	}
	count := len(f.commands)
	f.ask(t)
	if len(f.commands) != count+1 {
		t.Fatal("verified retry sent again")
	} // only current task guard runs
	changed := f.input
	changed.Title = "different"
	if _, err = f.r.conversationAsk(context.Background(), changed); err == nil || f.posts != 1 {
		t.Fatal("changed retry accepted")
	}
}

func TestConversationReplyDeliveredToOriginalTaskAndReleasedOnlyByCanonicalCommand(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	f.r.reportConversations(context.Background(), &health)
	if len(health.Errors) != 0 || health.ConversationReplies != 1 {
		t.Fatalf("reply delivery %v", health.Errors)
	}
	req := f.r.State.Requests[reply.Message]
	if req == nil || req.Todo != 42 || req.Conversation != f.q.RequestID || req.Phase != "conversation_reply_delivered" || req.Reported != "conversation_delivered" {
		t.Fatal("owner answer was not delivered to original task")
	}
	row, err := f.r.todo(42)
	if err != nil || row.Status != "todo" || !strings.Contains(row.Note, "以前の作業を保持") || !strings.Contains(row.Note, "conversation-owner-reply:") {
		t.Fatal("normal task reference missing")
	}
	for _, cmd := range f.commands {
		if cmd == "add" || cmd == "start" || cmd == "done" || cmd == "run" {
			t.Fatal("answer started duplicate work or inferred completion")
		}
	}
	if f.close != 0 {
		t.Fatal("unapplied answer closed")
	}
	f.r.NoticeAttempted = false
	f.r.processConversations(context.Background(), &Health{})
	answers := 0
	for _, cmd := range f.commands {
		if cmd == "question-answer" {
			answers++
		}
	}
	if answers != 1 {
		t.Fatal("answer released again")
	}
}

func TestConversationForeignOwnerOrChangedDecisionDoesNotDeliver(t *testing.T) {
	for _, change := range []string{"owner", "decision", "question", "todo"} {
		t.Run(change, func(t *testing.T) {
			f := conversationTest(t, true)
			f.ask(t)
			f.reply(t, 8, "a")
			before, _ := f.r.todo(42)
			switch change {
			case "owner":
				f.replies[0].Author = "999"
			case "decision":
				f.replies[0].Decision = json.RawMessage(`{"action":"different"}`)
			case "question":
				f.replies[0].Question = "999"
			case "todo":
				f.replies[0].Todo = 999
			}
			health := Health{}
			f.r.processConversations(context.Background(), &health)
			f.r.reportConversations(context.Background(), &health)
			if len(health.Errors) == 0 {
				t.Fatal("wrong reply accepted")
			}
			row, _ := f.r.todo(42)
			if row.Status != "block" || row.Note != before.Note {
				t.Fatal("invalid source changed original task")
			}
		})
	}
}

func TestConversationMultipleRepliesAllReachOriginalTaskAndAreNotInferredApplied(t *testing.T) {
	f := conversationTest(t, false)
	f.ask(t)
	first := f.reply(t, 8, "a")
	second := f.reply(t, 9, "補足です")
	for i := 0; i < 2; i++ {
		f.r.NoticeAttempted = false
		health := Health{}
		f.r.processConversations(context.Background(), &health)
		f.r.reportConversations(context.Background(), &health)
		if len(health.Errors) != 0 {
			t.Fatal(health.Errors)
		}
	}
	for _, reply := range []conversationReply{first, second} {
		if f.r.State.Requests[reply.Message] == nil || f.r.State.Requests[reply.Message].Todo != 42 {
			t.Fatal("later reply starved")
		}
	}
	if f.close != 0 {
		t.Fatal("multiple unapplied replies closed")
	}
}

func TestConversationAppliedReceiptClosesOnlyAfterImplementationAndWorkerIdentity(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	f.r.processConversations(context.Background(), &Health{})
	f.r.Now = func() time.Time {
		stamp, _ := time.Parse(time.RFC3339Nano, f.created)
		return stamp.Add(2 * time.Minute)
	}
	application := map[string]any{"source": "discord-api-v10", "request_id": f.q.SenderID, "origin_todo_id": 42, "todo_id": 42, "thread_id": f.question, "question_message_id": f.question, "message_id": reply.Message, "author_id": "300", "job_id": "foreign-worker", "revision": strings.Repeat("e", 40), "applied_at": f.r.Now().Format(time.RFC3339Nano), "completion": "設定を反映し読み返しました。", "verification": map[string]string{"execution": "actual readback", "origin_saved": "original question kept", "identity": "configured owner/API/source matched"}}
	source, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+".json"))
	application["reply_sha256"] = canonicalHash(reply)
	application["source_receipt_sha256"] = bytesHash(source)
	if err := publish(filepath.Join(f.r.Config.Home, "applied", reply.Message+".json"), application); err != nil {
		t.Fatal(err)
	}
	health := Health{}
	f.r.NoticeAttempted = false
	f.r.processConversations(context.Background(), &health)
	f.r.reportConversations(context.Background(), &health)
	if len(health.Errors) == 0 || f.close != 0 {
		t.Fatal("foreign worker inferred applied")
	}
	application["job_id"] = "worker-42"
	f.r.JobsPath = filepath.Join(f.r.Config.Home, "test-jobs")
	if publish(filepath.Join(f.r.JobsPath, "worker-42", "job.json"), map[string]any{"id": "worker-42", "todo_id": 42, "cwd": f.r.Config.Home}) != nil {
		t.Fatal("job fixture")
	}
	f.mutateTodo(t, func(row map[string]any) { row["scheduler"] = map[string]any{"worktree": f.r.Config.Home} })
	if err := publish(filepath.Join(f.r.Config.Home, "applied", reply.Message+".json"), application); err != nil {
		t.Fatal(err)
	}
	health = Health{}
	f.r.NoticeAttempted = false
	f.r.processConversations(context.Background(), &health)
	f.r.reportConversations(context.Background(), &health)
	if len(health.Errors) != 0 || f.close != 1 {
		t.Fatalf("valid application not closed: %v", health.Errors)
	}
	index, _ := f.r.loadConversations()
	if index.Questions[f.input.RequestID].Phase != "closed" {
		t.Fatal("closed stage not persisted")
	}
	if f.r.State.Requests[reply.Message].Phase != "conversation_reply_applied" {
		t.Fatal("applied stage not retained")
	}
}

func TestConversationCorruptIndexOrProtectedScopeNeverInitializesOrPosts(t *testing.T) {
	f := conversationTest(t, false)
	f.deny = true
	if _, err := f.r.conversationAsk(context.Background(), f.input); err == nil || f.posts != 0 {
		t.Fatal("unverified task used")
	}
	f.deny = false
	data := []byte(`{"domain":"wrong"}`)
	if err := os.WriteFile(f.r.conversationPath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.r.conversationAsk(context.Background(), f.input); err == nil || f.posts != 0 {
		t.Fatal("corrupt index replaced")
	}
	after, _ := os.ReadFile(f.r.conversationPath())
	if string(after) != string(data) {
		t.Fatal("corrupt evidence overwritten")
	}
}

func TestConversationCanonicalHashMatchesSortedUnicodeJSONWithoutHTMLEscaping(t *testing.T) {
	want := bytesHash([]byte(`{"a":"日本語<&>","z":1}`))
	if got := canonicalHash(json.RawMessage(`{"z":1,"a":"日本語<&>"}`)); got != want {
		t.Fatal("cross-language evidence digest differs")
	}
}

func TestConversationLogsReuseTopicAndOriginalRetryDestination(t *testing.T) {
	f := conversationTest(t, false)
	commands := map[string][]string{}
	posts := map[string]string{}
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if args[0] != "recovery-log" {
			return "", errors.New("unexpected operation")
		}
		id := args[2]
		destination := ""
		for i := 1; i+1 < len(args); i++ {
			if args[i] == "--thread" {
				destination = args[i+1]
			}
		}
		if previous, exists := commands[id]; exists {
			if strings.Join(previous, "\x00") != strings.Join(args, "\x00") {
				t.Error("same request changed sender destination")
			}
		} else {
			commands[id] = append([]string(nil), args...)
			posts[id] = fmt.Sprint(700 + len(posts))
			if len(posts) == 1 && destination != "" {
				t.Error("first log should create topic")
			}
			if len(posts) == 2 && destination != "610" {
				t.Error("second log should reuse topic")
			}
		}
		b, _ := json.Marshal(map[string]any{"domain": "new-owner-recovery-messages", "delivery": "verified", "thread_id": "610", "message_id": posts[id]})
		return string(b), nil
	}
	first := filepath.Join(f.r.Config.Home, "first.txt")
	second := filepath.Join(f.r.Config.Home, "second.txt")
	if os.WriteFile(first, []byte("最初の進捗"), 0600) != nil || os.WriteFile(second, []byte("次の進捗"), 0600) != nil {
		t.Fatal("fixture write")
	}
	for _, call := range []struct{ id, file string }{{"first", first}, {"second", second}, {"first", first}} {
		if _, err := f.r.conversationLog(context.Background(), call.id, "同じ製品", call.file, "", false, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(posts) != 2 {
		t.Fatal("retry created another post")
	}
	if _, err := f.r.conversationLog(context.Background(), "first", "同じ製品", second, "", false, nil); err == nil {
		t.Fatal("changed retry reused request")
	}
}

func TestConversationUnavailableQuestionDoesNotStarveLaterQuestion(t *testing.T) {
	f := conversationTest(t, false)
	q := f.ask(t)
	f.reply(t, 8, "a")
	index, err := f.r.loadConversations()
	if err != nil {
		t.Fatal(err)
	}
	bad := q
	bad.RequestID = "a-unavailable"
	bad.SenderID = "unavailable"
	bad.Thread = "999"
	bad.Message = "999"
	bad.Progress = "998"
	root := filepath.Join(f.r.Config.Home, "conversations", bad.RequestID)
	bad.BodyFile = filepath.Join(root, "question.txt")
	bad.DecisionFile = filepath.Join(root, "decision.json")
	bad.ReceiptFile = filepath.Join(root, "question.json")
	index.Questions[bad.RequestID] = bad
	if publish(f.r.conversationPath(), index) != nil {
		t.Fatal("index fixture")
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	f.r.reportConversations(context.Background(), &health)
	if len(health.Errors) == 0 {
		t.Fatal("unavailable question unexpectedly verified")
	}
	health = Health{}
	f.r.NoticeAttempted = false
	f.r.processConversations(context.Background(), &health)
	f.r.reportConversations(context.Background(), &health)
	if len(health.Errors) != 0 || health.ConversationReplies != 1 {
		t.Fatalf("later question starved: %v", health.Errors)
	}
}

func TestConversationCanonicalSenderDeduplicatesChangedRequestIDAndTitle(t *testing.T) {
	f := conversationTest(t, false)
	canonical := f.ask(t)
	alias := f.input
	alias.RequestID = "same-decision-new-worker"
	alias.Title = "言い換えた同じ質問"
	q, err := f.r.conversationAsk(context.Background(), alias)
	if err != nil || q.RequestID != canonical.RequestID {
		t.Fatalf("canonical sender not reconciled: %v", err)
	}
	index, err := f.r.loadConversations()
	if err != nil || len(index.Questions) != 1 || index.Aliases[alias.RequestID].Target != canonical.RequestID || index.Aliases[alias.RequestID].Intent == nil {
		t.Fatal("canonical alias or original intent lost")
	}
	q, err = f.r.conversationAsk(context.Background(), alias)
	if err != nil || q.RequestID != canonical.RequestID || f.posts != 1 {
		t.Fatal("canonical alias retry sent a different question")
	}
	f.reply(t, 8, "a")
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 || health.ConversationReplies != 1 {
		t.Fatalf("canonical reply failed: %v", health.Errors)
	}
}

func TestConversationReservedSendAutomaticallyResumesWithoutNewIdentity(t *testing.T) {
	f := conversationTest(t, false)
	f.lost = true
	if _, err := f.r.conversationAsk(context.Background(), f.input); err == nil {
		t.Fatal("expected lost sender result")
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	index, err := f.r.loadConversations()
	if err != nil || len(health.Errors) != 0 || index.Questions[f.input.RequestID].Phase != "posted" || f.posts != 1 {
		t.Fatal("reserved send did not resume the original intent")
	}
}

func TestConversationNewQuestionDoesNotSendFromAnAlreadyHeldTask(t *testing.T) {
	f := conversationTest(t, true)
	f.mutateTodo(t, func(row map[string]any) { row["status"] = "block" })
	if _, err := f.r.conversationAsk(context.Background(), f.input); err == nil || f.posts != 0 {
		t.Fatal("held task generated a new question")
	}
}

func TestConversationScanPendingEditCompletesCanonicalProgressMetadata(t *testing.T) {
	f := conversationTest(t, false)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	f.r.State.Requests[reply.Message] = &Request{Thread: reply.Thread, Guild: "100", RecordVersion: 2, Message: reply.Message, Author: reply.Author, Created: reply.Created, Acquired: f.r.Now().Format(time.RFC3339Nano), Source: "discord-api-v10", Text: "a", Key: requestKey(reply.Thread, reply.Message), Conversation: f.q.RequestID, Phase: "conversation_capture_pending"}
	f.replies[0].Body = "b"
	f.edited = map[string]string{reply.Message: f.r.Now().Format(time.RFC3339Nano)}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 {
		t.Fatal(health.Errors)
	}
	var receipt Request
	if readJSON(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", reply.Message+".json"), 1<<20, &receipt) != nil || receipt.ProgressThread != f.q.Progress || receipt.Text != "b" || len(receipt.PriorVersions) != 1 {
		t.Fatal("scan/edit lost metadata or previous text")
	}
}

func TestConversationPostedWaitIsRetriedWithSameReceipt(t *testing.T) {
	f := conversationTest(t, true)
	f.waitFailure = true
	if _, err := f.r.conversationAsk(context.Background(), f.input); err == nil {
		t.Fatal("expected wait failure")
	}
	index, _ := f.r.loadConversations()
	if index.Questions[f.input.RequestID].Phase != "posted" {
		t.Fatal("posted state not retained")
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	index, _ = f.r.loadConversations()
	row, _ := f.r.todo(42)
	if len(health.Errors) != 0 || index.Questions[f.input.RequestID].Phase != "waiting" || row.Status != "block" || f.posts != 1 {
		t.Fatal("posted wait did not resume without another question")
	}
}

func TestConversationProjectChangeStopsBeforeAnyDeliveryOrWait(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("fixture")
	}
	db["projects"].(map[string]any)["other-project"] = f.r.Config.Home
	db["items"].([]any)[0].(map[string]any)["project"] = "other-project"
	if publish(f.r.TodoPath, db) != nil {
		t.Fatal("fixture")
	}
	commands := len(f.commands)
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) == 0 {
		t.Fatal("project change accepted")
	}
	for _, command := range f.commands[commands:] {
		if command == "edit" || command == "question-wait" || command == "question-answer" {
			t.Fatal("foreign project changed")
		}
	}
	if _, err := f.r.conversationAsk(context.Background(), f.input); err == nil {
		t.Fatal("existing ask ignored changed project")
	}
	if req := f.r.State.Requests[reply.Message]; req != nil && req.Todo != 0 {
		t.Fatal("foreign project bound")
	}
}

func TestConversationClosedWithPendingNoticeStillDeliversAdditionalOwnerReply(t *testing.T) {
	f := conversationTest(t, false)
	f.ask(t)
	first := f.reply(t, 8, "a")
	f.r.processConversations(context.Background(), &Health{})
	f.r.Now = func() time.Time { at, _ := time.Parse(time.RFC3339Nano, f.created); return at.Add(2 * time.Minute) }
	source, _ := os.ReadFile(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", first.Message+".json"))
	application := map[string]any{"source": "discord-api-v10", "request_id": f.q.SenderID, "origin_todo_id": 42, "todo_id": 42, "thread_id": f.question, "question_message_id": f.question, "message_id": first.Message, "author_id": "300", "job_id": "worker-42", "revision": strings.Repeat("e", 40), "applied_at": f.r.Now().Format(time.RFC3339Nano), "completion": "実反映", "reply_sha256": canonicalHash(first), "source_receipt_sha256": bytesHash(source), "verification": map[string]string{"execution": "readback", "origin_saved": "original retained", "identity": "owner/API matched"}}
	f.r.JobsPath = filepath.Join(f.r.Config.Home, "test-jobs")
	if publish(filepath.Join(f.r.JobsPath, "worker-42", "job.json"), map[string]any{"id": "worker-42", "todo_id": 42, "cwd": f.r.Config.Home}) != nil {
		t.Fatal("job fixture")
	}
	f.mutateTodo(t, func(row map[string]any) { row["scheduler"] = map[string]any{"worktree": f.r.Config.Home} })
	if publish(filepath.Join(f.r.Config.Home, "applied", first.Message+".json"), application) != nil {
		t.Fatal("fixture")
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 || !f.archived {
		t.Fatal("initial close failed", health.Errors)
	}
	second := f.reply(t, 9, "追加です")
	f.archived = false
	f.r.State.Requests[second.Message] = &Request{Thread: second.Thread, Guild: "100", RecordVersion: 2, Message: second.Message, Author: second.Author, Created: second.Created, Acquired: f.r.Now().Format(time.RFC3339Nano), Source: "discord-api-v10", Text: second.Body, Key: requestKey(second.Thread, second.Message), Conversation: f.q.RequestID, Phase: "conversation_capture_pending"}
	f.r.reportConversations(context.Background(), &Health{})
	health = Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 || f.r.State.Requests[second.Message].Todo != 42 || f.r.State.Requests[second.Message].Phase != "conversation_reply_delivered" || f.archived {
		t.Fatal("late owner reply was dropped or manually reopened question was rearchived", health.Errors)
	}
}

func TestConversationEditedReplyAfterReadyFailureUsesSeparateImmutableVersion(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	f.answerFailure = true
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) == 0 {
		t.Fatal("expected canonical failure")
	}
	files, err := filepath.Glob(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", "*.ready.json"))
	if err != nil || len(files) != 1 {
		t.Fatal("initial ready version absent")
	}
	first, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	f.replies[0].Body = "b"
	f.edited = map[string]string{reply.Message: f.r.Now().Format(time.RFC3339Nano)}
	health = Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 {
		t.Fatalf("edited answer stuck: %v", health.Errors)
	}
	files, err = filepath.Glob(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "replies", "*.ready.json"))
	if err != nil || len(files) != 2 {
		t.Fatal("distinct ready versions not retained")
	}
	retained := false
	for _, file := range files {
		b, _ := os.ReadFile(file)
		if string(b) == string(first) {
			retained = true
		}
	}
	if !retained || f.r.State.Requests[reply.Message].Text != "b" || len(f.r.State.Requests[reply.Message].PriorVersions) != 1 {
		t.Fatal("edited owner history lost")
	}
	row, _ := f.r.todo(42)
	if row.Status != "todo" {
		t.Fatal("new answer did not release exact wait")
	}
}

func TestConversationParentReferenceIsDurableBeforeCanonicalRelease(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	f.reply(t, 8, "a")
	backend := f.r.Run
	order := []string{}
	stagedBeforeRelease := false
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path == conversationTodo && (args[0] == "question-answer" || args[0] == "edit") {
			order = append(order, args[0])
			if args[0] == "question-answer" {
				row, err := f.r.todo(42)
				stagedBeforeRelease = err == nil && conversationWaitUnchanged(row, f.q) && strings.Contains(row.Note, "conversation-owner-reply:")
			}
		}
		return backend(ctx, path, args...)
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 || !stagedBeforeRelease || strings.Join(order, ",") != "edit,question-answer" {
		t.Fatal("canonical wait released before the owner reply reference was durable")
	}
}

func TestConversationFailedParentNoteKeepsCanonicalWait(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	f.reply(t, 8, "a")
	before, err := f.r.todo(42)
	if err != nil {
		t.Fatal("parent fixture unavailable")
	}
	backend := f.r.Run
	answers := 0
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path == conversationTodo && args[0] == "edit" {
			return "", errors.New("parent note save failed")
		}
		if path == conversationTodo && args[0] == "question-answer" {
			answers++
		}
		return backend(ctx, path, args...)
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	after, err := f.r.todo(42)
	if err != nil || len(health.Errors) == 0 || after.Status != "block" || after.Note != before.Note || answers != 0 {
		t.Fatal("failed parent delivery released or changed the question wait")
	}
	f.r.Run = backend
	health = Health{}
	f.r.processConversations(context.Background(), &health)
	after, err = f.r.todo(42)
	if err != nil || len(health.Errors) != 0 || after.Status != "todo" || len(after.QuestionAnswers) != 1 || !strings.Contains(after.Note, "conversation-owner-reply:") {
		t.Fatal("parent delivery did not resume after its note save recovered")
	}
}

func TestConversationOverwrittenQuestionHoldIsNotRewrittenOrAcknowledged(t *testing.T) {
	for _, change := range []string{"missing", "duplicate", "inline", "suffix", "status", "wait-sha"} {
		t.Run(change, func(t *testing.T) {
			f := conversationTest(t, true)
			f.ask(t)
			reply := f.reply(t, 8, "a")
			f.mutateTodo(t, func(row map[string]any) {
				note := row["note"].(string)
				marker := note[strings.LastIndex(note, "\n")+1:]
				switch change {
				case "missing":
					row["note"] = "別の停止条件を保持"
				case "duplicate":
					row["note"] = note + "\n" + marker
				case "inline":
					row["note"] = "prefix" + marker
				case "suffix":
					row["note"] = note + "suffix"
				case "status":
					row["status"] = "doing"
				case "wait-sha":
					row["owner_question_wait"].(map[string]any)["question_receipt_sha256"] = strings.Repeat("f", 64)
				}
			})
			before, _ := f.r.todo(42)
			health := Health{}
			f.r.processConversations(context.Background(), &health)
			after, err := f.r.todo(42)
			if err != nil || len(health.Errors) == 0 || after.Status != before.Status || after.Note != before.Note || len(after.QuestionAnswers) != 0 {
				t.Fatal("changed question hold was rewritten or released")
			}
			if req := f.r.State.Requests[reply.Message]; req == nil || req.Phase != "conversation_reply_saved" || req.Todo != 0 {
				t.Fatal("changed question hold was reported as delivered")
			}
			for _, command := range f.commands {
				if command == "edit" || command == "question-answer" {
					t.Fatal("changed question hold reached a mutation command")
				}
			}
		})
	}
}

func TestConversationDifferentBlockIsNotRewrittenIntoAQuestionWait(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	f.reply(t, 8, "a")
	f.mutateTodo(t, func(row map[string]any) {
		delete(row, "owner_question_wait")
		row["note"] = "別の停止条件を保持"
	})
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	row, err := f.r.todo(42)
	if err != nil || len(health.Errors) == 0 || row.Status != "block" || row.Note != "別の停止条件を保持" {
		t.Fatal("unrelated block was rewritten or released")
	}
	for _, command := range f.commands {
		if command == "edit" || command == "question-answer" {
			t.Fatal("unrelated block reached a mutation command")
		}
	}
}

func TestConversationConcurrentOrdinaryBlockCannotBeRestoredAsQuestionWait(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	backend := f.r.Run
	competed := false
	answers := 0
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path == conversationTodo && args[0] == "edit" {
			competed = true
			// Match the canonical Block command: status/note/updated change,
			// without any dedicated stopped flag for clear() to observe.
			f.mutateTodo(t, func(row map[string]any) {
				row["status"] = "block"
				row["note"] = "データ到着まで停止"
			})
		}
		if path == conversationTodo && args[0] == "question-answer" {
			answers++
		}
		return backend(ctx, path, args...)
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	row, err := f.r.todo(42)
	if !competed || err != nil || len(health.Errors) == 0 || row.Status != "block" || row.Note != "データ到着まで停止" || answers != 0 || len(row.QuestionAnswers) != 0 || !conversationWaitRecorded(row, f.q) {
		t.Fatal("concurrent data hold was overwritten or released")
	}
	if req := f.r.State.Requests[reply.Message]; req == nil || req.Phase != "conversation_reply_saved" || req.Todo != 0 {
		t.Fatal("concurrent hold was reported as delivered")
	}
}

func TestConversationLostSuccessfulAnswerResultReconcilesWithoutReleasingTwice(t *testing.T) {
	f := conversationTest(t, true)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	backend := f.r.Run
	answers := 0
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		output, err := backend(ctx, path, args...)
		if path == conversationTodo && args[0] == "question-answer" && err == nil {
			answers++
			return "", errors.New("canonical result lost after durable transition")
		}
		return output, err
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) == 0 {
		t.Fatal("lost answer result was reported as delivered")
	}
	f.mutateTodo(t, func(row map[string]any) {
		row["status"] = "block"
		row["note"] = row["note"].(string) + "\n別の停止条件を保持"
	})
	f.r.processConversations(context.Background(), &Health{})
	row, err := f.r.todo(42)
	if err != nil || row.Status != "block" || !strings.HasSuffix(row.Note, "別の停止条件を保持") || answers != 1 || len(row.QuestionAnswers) != 1 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
		t.Fatal("reconciliation released twice or lost the original task delivery")
	}
}

func TestConversationNormalScanTagsQuestionBeforeOrdinaryAdmission(t *testing.T) {
	f := conversationTest(t, false)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	m := Message{ID: reply.Message, Channel: reply.Thread, Content: reply.Body, Timestamp: reply.Created}
	m.Author.ID = reply.Author
	server(t, f.r, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/channels/"+f.question+"/messages" {
			respond(w, []Message{m})
			return
		}
		if r.URL.Path == "/channels/"+f.question+"/messages/"+reply.Message {
			respond(w, m)
			return
		}
		t.Errorf("unexpected route %s", r.URL.Path)
		w.WriteHeader(404)
	})
	count, err := f.r.scan(context.Background(), Thread{ID: f.question, Owner: "400", Parent: "200", Guild: "100", Type: 11})
	if err != nil || count != 1 {
		t.Fatal("scan failed", err)
	}
	req := f.r.State.Requests[reply.Message]
	if req.Conversation != f.q.RequestID || req.Phase != "conversation_capture_pending" {
		t.Fatal("normal scan lost canonical question routing")
	}
	health := Health{}
	f.r.admit(context.Background(), &health)
	if req.Todo != 0 || health.Registered != 0 {
		t.Fatal("ordinary admission registered answer as another task")
	}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 || f.r.State.Requests[reply.Message].Todo != 42 {
		t.Fatalf("original delivery blocked: %v", health.Errors)
	}
}

func TestConversationAndOrdinaryReportsTakeAlternateTurns(t *testing.T) {
	f := conversationTest(t, false)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	f.r.processConversations(context.Background(), &Health{})
	stamp, _ := time.Parse(time.RFC3339Nano, f.created)
	message := msg(stamp.Add(time.Second), "500", "300", "進捗を教えて")
	normal := &Request{Thread: "500", Guild: "100", RecordVersion: 2, Message: message.ID, Author: "300", Created: message.Timestamp, Acquired: f.r.Now().Format(time.RFC3339Nano), Source: "discord-api-v10", Text: message.Content, Key: requestKey("500", message.ID), Todo: 42, Phase: "registered"}
	f.r.State.Requests[normal.Message] = normal
	server(t, f.r, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/channels/500":
			respond(w, Thread{ID: "500", Guild: "100", Parent: requestForum, Owner: "300", Type: 11})
		case "/channels/500/messages/" + message.ID:
			respond(w, message)
		default:
			t.Errorf("unexpected report verification %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	backend := f.r.Run
	turns := []string{}
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if args[0] == "recovery-log" {
			if strings.HasPrefix(argValue(args, "--request-id"), "conversation-log-") {
				turns = append(turns, "conversation")
			} else {
				turns = append(turns, "ordinary")
			}
		}
		return backend(ctx, path, args...)
	}
	for i := 0; i < 4; i++ {
		normal.Reported = ""
		f.r.State.Requests[reply.Message].Reported = ""
		f.r.NoticeAttempted = false
		health := Health{}
		f.r.reportUpdates(context.Background(), &health)
		if len(health.Errors) != 0 {
			t.Fatal(health.Errors)
		}
	}
	if strings.Join(turns, ",") != "ordinary,conversation,ordinary,conversation" {
		t.Fatalf("a report category starved: %v", turns)
	}
}
