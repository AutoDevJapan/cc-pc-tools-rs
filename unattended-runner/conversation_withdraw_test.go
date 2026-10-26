package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type withdrawalFixture struct {
	f                                                                                   *conversationFixture
	mu                                                                                  sync.Mutex
	reason                                                                              string
	patches, logs, commands, proofReads                                                 int
	archived, ignorePatch, losePatch, failLog, loseCommand, denyCommand, pretendCommand bool
	foreignScope, edited                                                                string
	onProof                                                                             func(int)
	onCommand, onPatch                                                                  func()
	allowAnswer                                                                         bool
}

func withdrawalTest(t *testing.T, native bool) *withdrawalFixture {
	return withdrawalTestWithWait(t, native, true)
}

func withdrawalTestWithWait(t *testing.T, native, askWait bool) *withdrawalFixture {
	t.Helper()
	var f *conversationFixture
	if native {
		f = conversationNativeTest(t)
	} else {
		f = conversationTest(t, true)
	}
	f.input.Wait = askWait
	f.mutateTodo(t, func(row map[string]any) {
		row["resume_generation"] = 7
		row["worker_receipt"] = map[string]any{"job": "worker-42", "generation": 7}
		row["scheduler"] = map[string]any{"job": "worker-42", "key": "key-42", "phase": "running", "worktree": "saved-worktree", "branch": "saved-branch"}
	})
	f.ask(t)
	w := &withdrawalFixture{f: f, reason: filepath.Join(f.r.Config.Home, "reason.txt")}
	if os.WriteFile(w.reason, []byte("既存許可のある通常読取を再確認したため"), 0600) != nil {
		t.Fatal("reason fixture unavailable")
	}
	if native {
		job := filepath.Join(f.r.Config.Home, "isolated-jobs", "worker-42", "job.json")
		if publish(job, map[string]any{"id": "worker-42", "todo_id": 42, "launch_key": "key-42", "status": "succeeded", "created_at": "2023-11-15T07:13:20+09:00"}) != nil {
			t.Fatal("job fixture unavailable")
		}
	}
	backend := f.r.Run
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		switch args[0] {
		case "recovery-replies":
			w.proofReads++
			if w.onProof != nil {
				w.onProof(w.proofReads)
			}
		case "question-withdraw":
			w.commands++
			if w.onCommand != nil {
				w.onCommand()
			}
			if w.denyCommand {
				return "", errors.New("existing technical rejection")
			}
			if w.pretendCommand {
				return `{"withdrawn":true}`, nil
			}
			if !native {
				if len(args) != 10 || args[2] != "--question-file" || args[4] != "--reason-file" || args[6] != "--expected-state-file" || args[8] != "--scope-file" {
					t.Fatal("canonical command contract differs")
				}
				origin, err := boundedRegularUpTo(args[7], maxTodo)
				current, readErr := f.r.withdrawalRow(42)
				if err != nil || readErr != nil || canonicalHash(json.RawMessage(origin)) != canonicalHash(json.RawMessage(current)) {
					return "", errors.New("conversation_state_changed")
				}
				reason, err := boundedRegular(args[5])
				if err != nil {
					return "", err
				}
				f.mutateTodo(t, func(row map[string]any) {
					wait := row["owner_question_wait"]
					row["owner_question_withdrawal_receipts"] = []any{map[string]any{"domain": todoWithdrawalDomain, "reason": string(reason), "reason_sha256": bytesHash(reason), "expected_state_sha256": bytesHash(origin), "wait": wait,
						"origin": map[string]any{"resume_generation": row["resume_generation"], "worker_receipt": row["worker_receipt"], "job": "worker-42", "launch_key": "key-42"}, "applied_at": f.r.Now().UTC().Format(time.RFC3339Nano)}}
					delete(row, "owner_question_wait")
					row["status"] = "todo"
					marker := fmt.Sprintf("\n[discord-conversation-question-v1 request_id=%s sha256=%s]", f.q.SenderID, f.q.ReceiptSHA)
					row["note"] = strings.Replace(row["note"].(string), marker, "", 1)
				})
				if w.loseCommand {
					w.loseCommand = false
					return "", errors.New("result lost after commit")
				}
				return "{}", nil
			}
		case "question-wait":
			if !native && !askWait {
				row, err := f.r.conversationTask(ctx, f.input.Todo)
				if err != nil {
					return "", err
				}
				out, err := backend(ctx, path, args...)
				if err == nil {
					f.mutateTodo(t, func(row map[string]any) {
						wait := row["owner_question_wait"].(map[string]any)
						wait["domain"] = "todo-owner-question-wait-v1"
					})
					origin := conversationOwnerIdentity(row)
					f.mutateTodo(t, func(row map[string]any) { row["owner_question_wait"].(map[string]any)["origin"] = origin })
				}
				return out, err
			}
		case "recovery-log":
			if w.commands == 0 {
				t.Fatal("progress preceded canonical persistence")
			}
			for _, arg := range args {
				if arg == "--notify" {
					t.Fatal("withdrawal requested a notification")
				}
			}
			if args[len(args)-2] != "--thread" || args[len(args)-1] != f.q.Progress {
				t.Fatal("progress went to a question or another scope")
			}
			if w.logs == 0 {
				w.logs++
			}
			if w.failLog {
				w.failLog = false
				return "", errors.New("log result lost")
			}
		case "question-answer":
			if !w.allowAnswer {
				t.Fatal("withdrawal manufactured an answer")
			}
		case "recovery-ask", "start", "done":
			t.Fatal("withdrawal manufactured an answer or launched/completed the task")
		}
		return backend(ctx, path, args...)
	}
	server(t, f.r, func(out http.ResponseWriter, req *http.Request) {
		w.mu.Lock()
		defer w.mu.Unlock()
		if req.Header.Get("Authorization") != "Bot fixture-token" {
			t.Error("credential placement differs")
		}
		if req.URL.Path == "/channels/"+f.q.Thread && req.Method == http.MethodPatch {
			w.patches++
			var record conversationWithdrawal
			if readJSON(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, "withdrawal-state.json"), 128<<10, &record) != nil || record.Phase != "patch_pending" {
				t.Error("archive intent not durable")
			}
			if !w.ignorePatch {
				w.archived = true
			}
			if w.onPatch != nil {
				w.onPatch()
			}
			if w.losePatch {
				w.losePatch = false
				connection, _, err := out.(http.Hijacker).Hijack()
				if err == nil {
					_ = connection.Close()
				}
				return
			}
			respond(out, map[string]any{"archive_response": true})
			return
		}
		if (req.URL.Path == "/channels/"+f.q.Thread || req.URL.Path == "/channels/"+f.q.Progress) && req.Method == http.MethodGet {
			thread := strings.TrimPrefix(req.URL.Path, "/channels/")
			owner := f.r.State.Destination.Bot
			if w.foreignScope != "" {
				owner = w.foreignScope
			}
			respond(out, map[string]any{"id": thread, "guild_id": "100", "parent_id": "200", "owner_id": owner, "type": 11, "thread_metadata": map[string]any{"archived": w.archived}})
			return
		}
		if req.URL.Path == "/channels/"+f.q.Thread+"/messages/"+f.q.Message && req.Method == http.MethodGet {
			message := Message{ID: f.q.Message, Channel: f.q.Thread, Timestamp: f.created, Content: "bot question", Edited: w.edited}
			message.Author.ID = "400"
			message.Author.Bot = true
			respond(out, message)
			return
		}
		for _, reply := range f.replies {
			if req.URL.Path == "/channels/"+f.q.Thread+"/messages/"+reply.Message {
				message := Message{ID: reply.Message, Channel: f.q.Thread, Timestamp: reply.Created, Content: reply.Body}
				message.Author.ID = reply.Author
				respond(out, message)
				return
			}
		}
		t.Error("unexpected withdrawal HTTP operation")
		out.WriteHeader(404)
	})
	return w
}

func (w *withdrawalFixture) run() (map[string]any, error) {
	return w.f.r.conversationWithdraw(context.Background(), w.f.q.RequestID, w.reason)
}
func (w *withdrawalFixture) patchCount() int { w.mu.Lock(); defer w.mu.Unlock(); return w.patches }

func TestConversationWithdrawPersistsBeforeOneQuietNoticeAndArchive(t *testing.T) {
	w := withdrawalTest(t, false)
	result, err := w.run()
	if err != nil || result["withdrawn"] != true || result["answered"] != false || result["fulfilled"] != false {
		t.Fatalf("withdrawal not verified: %v", err)
	}
	index, err := w.f.r.loadConversations()
	if err != nil || index.Questions[w.f.q.RequestID].Wait || index.Questions[w.f.q.RequestID].Phase != "withdrawn" {
		t.Fatal("retired wait not stored separately")
	}
	for i := 0; i < 2; i++ {
		if _, err = w.run(); err != nil {
			t.Fatal(err)
		}
	}
	if w.commands != 1 || w.logs != 1 || w.patchCount() != 1 || len(w.f.r.State.Requests) != 0 {
		t.Fatal("retry duplicated work or manufactured an owner receipt")
	}
	row, _ := w.f.r.todo(42)
	if row.Status != "todo" || len(row.QuestionAnswers) != 0 || row.Note != "以前の作業を保持" {
		t.Fatal("question release lost original work")
	}
	if err = os.WriteFile(w.reason, []byte("different reason"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = w.run(); err == nil {
		t.Fatal("changed retry accepted")
	}
}

func TestConversationWithdrawLostResultsResumeWithoutDuplicateEffects(t *testing.T) {
	for _, stage := range []string{"canonical", "notice", "patch"} {
		t.Run(stage, func(t *testing.T) {
			w := withdrawalTest(t, false)
			w.loseCommand = stage == "canonical"
			w.failLog = stage == "notice"
			w.losePatch = stage == "patch"
			if _, err := w.run(); err == nil {
				t.Fatal("lost result reported as success")
			}
			if _, err := w.run(); err != nil {
				t.Fatal(err)
			}
			if w.commands != 1 || w.logs != 1 || w.patchCount() != 1 {
				t.Fatal("resume duplicated a canonical release, notice or archive")
			}
		})
	}
}

func TestConversationWithdrawLostCanonicalResponseThenRealReplyReturnsToNormalDelivery(t *testing.T) {
	w := withdrawalTest(t, false)
	w.loseCommand = true
	if _, err := w.run(); err == nil {
		t.Fatal("lost canonical response reported success")
	}
	reply := w.f.reply(t, 8, "既存の依頼を続けて")
	result, err := w.run()
	if err != nil || result["withdrawn"] != false || result["owner_reply_pending"] != true {
		t.Fatal("reply not returned to normal delivery")
	}
	health := Health{}
	w.f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 || w.f.r.State.Requests[reply.Message] == nil || w.f.r.State.Requests[reply.Message].Todo != 42 {
		t.Fatalf("lost-response recovery stranded the real reply: %v", health.Errors)
	}
	row, _ := w.f.r.todo(42)
	if len(row.QuestionAnswers) != 0 || w.commands != 1 || w.logs != 0 || w.patchCount() != 0 {
		t.Fatal("reply recovery created an answer receipt, notification or archive")
	}
}

func TestConversationWithdrawInvalidCanonicalReceiptsAndManualReopenNeverVerify(t *testing.T) {
	for _, kind := range []string{"reason", "origin", "question", "duplicate", "reopened"} {
		t.Run(kind, func(t *testing.T) {
			w := withdrawalTest(t, false)
			if _, err := w.run(); err != nil {
				t.Fatal(err)
			}
			if kind == "reopened" {
				w.mu.Lock()
				w.archived = false
				w.mu.Unlock()
			} else {
				w.f.mutateTodo(t, func(row map[string]any) {
					receipts := row["owner_question_withdrawal_receipts"].([]any)
					receipt := receipts[0].(map[string]any)
					switch kind {
					case "reason":
						receipt["reason"] = "changed"
					case "origin":
						receipt["origin"].(map[string]any)["job"] = "another-worker"
					case "question":
						receipt["wait"].(map[string]any)["question"].(map[string]any)["thread_id"] = "999"
					case "duplicate":
						row["owner_question_withdrawal_receipts"] = append(receipts, receipt)
					}
				})
			}
			if _, err := w.run(); err == nil {
				t.Fatal("invalid receipt or manual reopen verified")
			}
			if w.commands != 1 || w.logs != 1 || w.patchCount() != 1 {
				t.Fatal("invalid replay retried side effects")
			}
		})
	}
}

func TestConversationWithdrawOwnerReplyWinsBeforeReleaseAndRacesNeverReportSuccess(t *testing.T) {
	for _, read := range []int{1, 2, 3, 4, 5} {
		t.Run(fmt.Sprint(read), func(t *testing.T) {
			w := withdrawalTest(t, false)
			w.onProof = func(n int) {
				if n == read {
					w.f.reply(t, 8, "既存の許可で進めて")
				}
			}
			result, err := w.run()
			if err == nil && result["withdrawn"] == true {
				t.Fatal("new owner reply counted as withdrawal success")
			}
			if read <= 2 && w.commands != 0 {
				t.Fatal("owner reply did not win before canonical release")
			}
			if read < 5 && w.patchCount() != 0 {
				t.Fatal("question with a reply was archived")
			}
		})
	}
}

func TestConversationWithdrawActualCanonicalCapturesSnapshotAfterFirstProof(t *testing.T) {
	w := withdrawalTest(t, true)
	w.onProof = func(n int) {
		if n == 1 {
			w.f.mutateTodo(t, func(row map[string]any) { row["ordinary_progress"] = "saved while checking Discord" })
		}
	}
	result, err := w.run()
	if err != nil || result["withdrawn"] != true || w.commands != 1 {
		t.Fatal("an ordinary update during the first proof poisoned the immutable withdrawal intent")
	}
	var db map[string]any
	if readJSON(w.f.r.TodoPath, maxTodo, &db) != nil || db["items"].([]any)[0].(map[string]any)["ordinary_progress"] != "saved while checking Discord" {
		t.Fatal("withdrawal lost the ordinary update")
	}
}

func TestConversationWithdrawAliasesScopeEditsAndTechnicalRejectionsStayPending(t *testing.T) {
	for _, kind := range []string{"alias", "shadow", "scope", "question-edit", "deny", "pretend", "cas", "archive-response"} {
		t.Run(kind, func(t *testing.T) {
			w := withdrawalTest(t, false)
			switch kind {
			case "alias", "shadow":
				index, _ := w.f.r.loadConversations()
				if kind == "alias" {
					index.Aliases[w.f.q.RequestID] = conversationAlias{Target: "other", InputSHA: w.f.q.InputSHA}
					other := w.f.q
					other.RequestID = "other"
					other.BodyFile = filepath.Join(w.f.r.Config.Home, "conversations", "other", "question.txt")
					other.DecisionFile = filepath.Join(w.f.r.Config.Home, "conversations", "other", "decision.json")
					other.ReceiptFile = filepath.Join(w.f.r.Config.Home, "conversations", "other", "question.json")
					index.Questions["other"] = other
				} else {
					other := w.f.q
					other.RequestID = "other"
					other.BodyFile = filepath.Join(w.f.r.Config.Home, "conversations", "other", "question.txt")
					other.DecisionFile = filepath.Join(w.f.r.Config.Home, "conversations", "other", "decision.json")
					other.ReceiptFile = filepath.Join(w.f.r.Config.Home, "conversations", "other", "question.json")
					index.Questions["other"] = other
				}
				if publish(w.f.r.conversationPath(), index) != nil {
					t.Fatal("index fixture unavailable")
				}
			case "scope":
				w.foreignScope = "999"
			case "question-edit":
				w.onProof = func(n int) {
					if n == 2 {
						w.mu.Lock()
						w.edited = "2026-10-13T01:00:00Z"
						w.mu.Unlock()
					}
				}
			case "deny":
				w.denyCommand = true
			case "pretend":
				w.pretendCommand = true
			case "cas":
				w.onCommand = func() { w.f.mutateTodo(t, func(row map[string]any) { row["note"] = "another stop" }) }
			case "archive-response":
				w.ignorePatch = true
			}
			if _, err := w.run(); err == nil {
				t.Fatal("conflict or technical rejection reported success")
			}
			if kind != "archive-response" && w.patchCount() != 0 {
				t.Fatal("conflicting scope was archived")
			}
			if kind == "deny" || kind == "pretend" || kind == "cas" {
				if w.logs != 0 {
					t.Fatal("canonical failure treated as persistence")
				}
			}
		})
	}
}

func TestConversationWithdrawActualCanonicalRustAndLateReplyUsesOriginalTask(t *testing.T) {
	w := withdrawalTest(t, true)
	if _, err := w.run(); err != nil {
		t.Fatal(err)
	}
	row, err := w.f.r.todo(42)
	if err != nil || row.Status != "todo" || row.Scheduler.Job != "" || len(row.QuestionAnswers) != 0 || len(row.Jobs) != 1 {
		t.Fatal("actual canonical withdrawal lost task identity")
	}
	reply := w.f.reply(t, 8, "既存の依頼を進めて")
	health := Health{}
	w.f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 || w.f.r.State.Requests[reply.Message] == nil || w.f.r.State.Requests[reply.Message].Todo != 42 {
		t.Fatalf("late owner reply not delivered to original task: %v", health.Errors)
	}
	row, _ = w.f.r.todo(42)
	if len(row.QuestionAnswers) != 0 {
		t.Fatal("withdrawal became an owner answer")
	}
}

func TestConversationWithdrawActualCanonicalSchedulerImportedBlockNote(t *testing.T) {
	for _, kind := range []string{"exact", "another-note", "appended-stop", "missing-result", "old-generation"} {
		t.Run(kind, func(t *testing.T) {
			w := withdrawalTest(t, true)
			w.f.mutateTodo(t, func(row map[string]any) {
				row["note"] = "ジョブの結果に判断が必要です。"
				row["scheduler"].(map[string]any)["phase"] = "finished"
				row["processed_results"] = map[string]any{"worker-42": map[string]any{"generation": 7, "reason": "block"}}
				switch kind {
				case "another-note":
					row["note"] = "別の本人指示で停止"
				case "appended-stop":
					row["note"] = "ジョブの結果に判断が必要です。\n別の停止"
				case "missing-result":
					delete(row, "processed_results")
				case "old-generation":
					row["processed_results"].(map[string]any)["worker-42"].(map[string]any)["generation"] = 6
				}
			})
			before, _ := os.ReadFile(w.f.r.TodoPath)
			result, err := w.run()
			if kind != "exact" {
				after, _ := os.ReadFile(w.f.r.TodoPath)
				if err == nil || !bytes.Equal(before, after) || w.logs != 0 || w.patchCount() != 0 {
					t.Fatal("unrelated stop was released or reported as withdrawn")
				}
				return
			}
			if err != nil || result["withdrawn"] != true {
				t.Fatalf("reported scheduler import path not supported: %v", err)
			}
			row, _ := w.f.r.todo(42)
			if row.Status != "todo" || row.Note != "以前の作業を保持" || len(row.QuestionAnswers) != 0 || w.logs != 1 || w.patchCount() != 1 {
				t.Fatal("imported-block withdrawal lost work or created an answer")
			}
		})
	}
}

func TestConversationWithdrawActualCanonicalRetiredLegacyWorker(t *testing.T) {
	for _, kind := range []string{"exact", "modern", "foreign-worker", "worker-generation", "missing-result", "running-job", "later-stop", "changed-snapshot"} {
		t.Run(kind, func(t *testing.T) {
			w := withdrawalTest(t, true)
			w.f.mutateTodo(t, func(row map[string]any) {
				wait := row["owner_question_wait"].(map[string]any)
				delete(wait, "origin")
				row["worker_receipt"] = map[string]any{"job": nil, "generation": 7}
				row["note"] = "ジョブの結果に判断が必要です。"
				row["scheduler"].(map[string]any)["phase"] = "finished"
				row["processed_results"] = map[string]any{"worker-42": map[string]any{"generation": 7, "reason": "block"}}
				switch kind {
				case "modern":
					wait["origin"] = map[string]any{"resume_generation": 7, "worker_receipt": row["worker_receipt"], "job": "worker-42", "key": "key-42"}
				case "foreign-worker":
					row["worker_receipt"].(map[string]any)["job"] = "another"
				case "worker-generation":
					row["worker_receipt"].(map[string]any)["generation"] = 6
				case "missing-result":
					delete(row, "processed_results")
				case "later-stop":
					row["owner_stopped"] = true
				}
			})
			if kind == "running-job" {
				job := filepath.Join(w.f.r.Config.Home, "isolated-jobs", "worker-42", "job.json")
				if err := publish(job, map[string]any{"id": "worker-42", "todo_id": 42, "launch_key": "key-42", "status": "running", "created_at": 1700000000}); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "changed-snapshot" {
				w.onCommand = func() { w.f.mutateTodo(t, func(row map[string]any) { row["note"] = "後から届いた停止" }) }
			}
			before, _ := os.ReadFile(w.f.r.TodoPath)
			result, err := w.run()
			if kind != "exact" {
				after, _ := os.ReadFile(w.f.r.TodoPath)
				if err == nil || (kind != "changed-snapshot" && !bytes.Equal(before, after)) || w.logs != 0 || w.patchCount() != 0 {
					t.Fatal("unproven null worker was released or reported")
				}
				return
			}
			if err != nil || result["withdrawn"] != true || w.logs != 1 || w.patchCount() != 1 || w.proofReads < 2 {
				t.Fatalf("legacy withdrawal, API recheck, progress or archive not verified: %v", err)
			}
			row, _ := w.f.r.todo(42)
			if row.Status != "todo" || row.Note != "以前の作業を保持" || row.Scheduler.Job != "" || len(row.QuestionAnswers) != 0 {
				t.Fatal("legacy withdrawal lost original work or invented an answer")
			}
			if _, err = w.run(); err != nil || w.commands != 1 || w.logs != 1 || w.patchCount() != 1 {
				t.Fatal("replay duplicated withdrawal, progress or archive")
			}
			reply := w.f.reply(t, 8, "既存の依頼を進めて")
			health := Health{}
			w.f.r.processConversations(context.Background(), &health)
			if len(health.Errors) != 0 || w.f.r.State.Requests[reply.Message] == nil || w.f.r.State.Requests[reply.Message].Todo != 42 {
				t.Fatalf("late owner reply lost after legacy withdrawal: %v", health.Errors)
			}
		})
	}
}

func TestConversationActualCanonicalOwnerReplyBeforeRetiredLegacyWithdrawal(t *testing.T) {
	w := withdrawalTest(t, true)
	w.allowAnswer = true
	w.f.mutateTodo(t, func(row map[string]any) {
		delete(row["owner_question_wait"].(map[string]any), "origin")
		row["worker_receipt"] = map[string]any{"job": nil, "generation": 7}
		row["note"] = "ジョブの結果に判断が必要です。"
		row["scheduler"].(map[string]any)["phase"] = "finished"
		row["processed_results"] = map[string]any{"worker-42": map[string]any{"generation": 7, "reason": "block"}}
	})
	reply := w.f.reply(t, 8, "既存の依頼を進めて")
	result, err := w.run()
	if err != nil || result["owner_reply_pending"] != true || w.commands != 0 || w.logs != 0 || w.patchCount() != 0 {
		t.Fatal("real owner reply did not stop withdrawal")
	}
	health := Health{}
	w.f.r.NoticeAttempted = true
	w.f.r.processConversations(context.Background(), &health)
	row, _ := w.f.r.todo(42)
	if len(health.Errors) != 0 || row.Status != "todo" || len(row.QuestionAnswers) != 1 || !hasNoteMarker(row.Note, "conversation-owner-reply:"+w.f.q.SenderID+":"+reply.Message+":"+canonicalHash([]string{reply.Body, ""})[:16]) {
		t.Fatalf("real owner reply was saved but could not release machine-imported wait: %v", health.Errors)
	}
}

func TestConversationActualCanonicalImportedAnswerLostResponseAndNewStop(t *testing.T) {
	for _, kind := range []string{"lost-response", "new-stop"} {
		t.Run(kind, func(t *testing.T) {
			w := withdrawalTest(t, true)
			w.allowAnswer = true
			w.f.r.NoticeAttempted = true
			w.f.mutateTodo(t, func(row map[string]any) {
				delete(row["owner_question_wait"].(map[string]any), "origin")
				row["worker_receipt"] = map[string]any{"job": nil, "generation": 7}
				row["note"] = "ジョブの結果に判断が必要です。"
				row["scheduler"].(map[string]any)["phase"] = "finished"
				row["processed_results"] = map[string]any{"worker-42": map[string]any{"generation": 7, "reason": "block"}}
			})
			reply := w.f.reply(t, 8, "既存の依頼を進めて")
			backend, intercepted := w.f.r.Run, false
			w.f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if args[0] == "question-answer" && !intercepted {
					intercepted = true
					if kind == "new-stop" {
						w.f.mutateTodo(t, func(row map[string]any) { row["note"] = "後から届いた停止" })
					} else {
						if _, err := backend(ctx, path, args...); err != nil {
							t.Fatal(err)
						}
						return "", errors.New("committed response lost")
					}
				}
				return backend(ctx, path, args...)
			}
			health := Health{}
			w.f.r.processConversations(context.Background(), &health)
			if !intercepted || len(health.Errors) == 0 {
				t.Fatal("did not exercise uncertain delivery")
			}
			health = Health{}
			w.f.r.processConversations(context.Background(), &health)
			row, _ := w.f.r.todo(42)
			if kind == "new-stop" {
				if row.Status != "block" || row.Note != "後から届いた停止" || len(row.QuestionAnswers) != 0 || w.f.r.State.Requests[reply.Message].Todo != 0 {
					t.Fatal("a later hold was lost or reported as delivered")
				}
			} else if len(health.Errors) != 0 || row.Status != "todo" || len(row.QuestionAnswers) != 1 || w.f.r.State.Requests[reply.Message].Todo != 42 {
				t.Fatalf("lost successful response did not reconcile exactly once: %v", health.Errors)
			}
		})
	}
}

type withdrawalCLIBackend struct {
	*conversationCLIFake
	reason string
}

func (b *withdrawalCLIBackend) conversationWithdraw(ctx context.Context, id, reason string) (map[string]any, error) {
	b.command, b.ctx, b.request, b.reason = "withdraw", ctx, id, reason
	return b.result, b.err
}

func TestConversationWithdrawCLIContractAndPrivateErrors(t *testing.T) {
	for _, args := range [][]string{{"withdraw"}, {"withdraw", "--request-id=q"}, {"withdraw", "--reason-file=private"}, {"withdraw", "--request-id=q", "--reason-file=private", "--notify"}, {"withdraw", "--request-id=q", "--reason-file=private", "--reason-file=other"}} {
		loaded := false
		var out bytes.Buffer
		code := conversationCLIWithLoader(args, &out, func(string) (conversationCLIBackend, func(), error) { loaded = true; return nil, nil, nil })
		if code != 1 || loaded || out.String() != "invalid conversation arguments\n" {
			t.Fatal("invalid withdrawal reached state or leaked private arguments")
		}
	}
	backend := &withdrawalCLIBackend{conversationCLIFake: &conversationCLIFake{result: map[string]any{"withdrawn": true, "answered": false}}}
	var out bytes.Buffer
	code := conversationCLIWithLoader([]string{"withdraw", "--request-id=q", "--reason-file=private"}, &out, func(string) (conversationCLIBackend, func(), error) { return backend, func() {}, nil })
	if code != 0 || backend.command != "withdraw" || backend.request != "q" || backend.reason != "private" || strings.Contains(out.String(), "private") {
		t.Fatal("withdrawal CLI contract differs")
	}
}
