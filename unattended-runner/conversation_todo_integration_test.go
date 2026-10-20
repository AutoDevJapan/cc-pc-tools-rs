package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func continuedNative(t *testing.T) *conversationFixture {
	t.Helper()
	f := conversationNativeTest(t)
	f.mutateTodo(t, func(row map[string]any) { row["resume_generation"] = 1 })
	f.ask(t)
	f.mutateTodo(t, func(row map[string]any) {
		row["status"] = "doing"
		row["resume_generation"] = 2
		row["worker_receipt"] = map[string]any{"job": nil, "generation": 2}
		row["note"] = "新しい直接指示を優先。元の全依頼と途中成果を保持する。"
	})
	return f
}

func TestConversationActualCanonicalContinuedDeliveryPreservesOriginalTask(t *testing.T) {
	f := continuedNative(t)
	reply := f.reply(t, 8, "a")
	before, err := os.ReadFile(f.r.TodoPath)
	if err != nil {
		t.Fatal(err)
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 {
		t.Fatalf("continued owner reply not delivered: %v", health.Errors)
	}
	row, err := f.r.todo(42)
	if err != nil || row.Status != "doing" || !strings.HasPrefix(row.Note, "新しい直接指示を優先。") || !strings.Contains(row.Note, "conversation-owner-reply:") || !conversationWaitRecorded(row, f.q) || len(row.QuestionAnswers) != 0 {
		t.Fatal("continued delivery released a hold or lost the original instructions")
	}
	if req := f.r.State.Requests[reply.Message]; req == nil || req.Todo != 42 || req.Phase != "conversation_reply_delivered" {
		t.Fatal("continued delivery not linked to the original task")
	}
	var original, after map[string]any
	if json.Unmarshal(before, &original) != nil || readJSON(f.r.TodoPath, maxTodo, &after) != nil {
		t.Fatal("readback unavailable")
	}
	afterRow := after["items"].([]any)[0].(map[string]any)
	if len(afterRow["owner_question_delivery_receipts"].([]any)) != 1 {
		t.Fatal("durable delivery receipt absent")
	}
	for _, key := range []string{"note", "updated", "owner_question_delivery_receipts"} {
		delete(original["items"].([]any)[0].(map[string]any), key)
		delete(afterRow, key)
	}
	instruction, ok := afterRow["scheduler"].(map[string]any)["owner_instruction"].(string)
	if !ok || !strings.Contains(instruction, "conversation-owner-reply:") {
		t.Fatal("the next worker has no persistent answer reference")
	}
	delete(afterRow, "scheduler") // continuedNative starts without a scheduler object.
	if canonicalHash(original) != canonicalHash(after) {
		t.Fatal("delivery changed original task/owner/holds/work")
	}
	if f.posts != 1 || f.close != 0 {
		t.Fatal("delivery resent a question or invented implementation")
	}
}

func TestConversationActualCanonicalContinuedDeliverySurvivesNoteReplacementAndFutureDelay(t *testing.T) {
	f := continuedNative(t)
	f.mutateTodo(t, func(row map[string]any) {
		row["scheduler"] = map[string]any{"owner_instruction": "現在の直接指示", "not_before": float64(10_000_000_000)}
	})
	f.reply(t, 8, "a")
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 {
		t.Fatalf("time-only hold prevented delivery: %v", health.Errors)
	}
	f.mutateTodo(t, func(row map[string]any) { row["note"] = "通常の担当終了後のメモ" })
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("missing readback")
	}
	row := db["items"].([]any)[0].(map[string]any)
	scheduler := row["scheduler"].(map[string]any)
	if !strings.HasPrefix(scheduler["owner_instruction"].(string), "現在の直接指示\n\n") || !strings.Contains(scheduler["owner_instruction"].(string), "conversation-owner-reply:") || scheduler["not_before"] != float64(10_000_000_000) {
		t.Fatal("persistent instructions or original time hold changed")
	}
	before, _ := os.ReadFile(f.r.TodoPath)
	f.r.processConversations(context.Background(), &Health{})
	after, _ := os.ReadFile(f.r.TodoPath)
	if string(before) != string(after) || f.close != 0 || f.posts != 1 {
		t.Fatal("delivered answer was lost, duplicated or treated as implemented")
	}
}

func TestConversationActualCanonicalDifferentGenerationBlockIsRejectedBeforeNoteEdit(t *testing.T) {
	f := conversationNativeTest(t)
	f.mutateTodo(t, func(row map[string]any) { row["resume_generation"] = 1 })
	f.ask(t)
	f.mutateTodo(t, func(row map[string]any) { row["resume_generation"] = 2 })
	f.reply(t, 8, "a")
	before, _ := os.ReadFile(f.r.TodoPath)
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	after, _ := os.ReadFile(f.r.TodoPath)
	if len(health.Errors) == 0 || string(before) != string(after) {
		t.Fatal("a later blocked generation was edited before canonical refusal")
	}
}

func TestConversationUnrelatedGenerationShapeDoesNotBreakOriginalTaskRead(t *testing.T) {
	f := conversationTest(t, false)
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("missing fixture")
	}
	db["items"] = append(db["items"].([]any), map[string]any{"id": 99, "project": "cc-pc-tools-rs", "status": "doing", "resume_generation": "unverified"})
	if err := publish(f.r.TodoPath, db); err != nil {
		t.Fatal(err)
	}
	if row, err := f.r.todo(42); err != nil || row.ID != 42 {
		t.Fatal("unrelated malformed generation prevented original task read")
	}
}

func TestConversationActualCanonicalAllDeliveryPathsKeepGuardedProjectAndRoot(t *testing.T) {
	for _, pathKind := range []string{"normal", "imported", "continued"} {
		for _, change := range []string{"project", "root"} {
			t.Run(pathKind+"/"+change, func(t *testing.T) {
				var f *conversationFixture
				if pathKind == "imported" {
					w := withdrawalTest(t, true)
					w.allowAnswer = true
					f = w.f
					f.mutateTodo(t, func(row map[string]any) {
						row["note"] = "ジョブの結果に判断が必要です。"
						row["scheduler"].(map[string]any)["phase"] = "finished"
						row["processed_results"] = map[string]any{"worker-42": map[string]any{"generation": 7, "reason": "block"}}
					})
				} else if pathKind == "continued" {
					f = continuedNative(t)
				} else {
					f = conversationNativeTest(t)
					f.ask(t)
				}
				f.r.NoticeAttempted = true
				f.reply(t, 8, "a")
				backend := f.r.Run
				var changed []byte
				f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
					if path == conversationTodo && (args[0] == "edit" || args[0] == "question-answer") && changed == nil {
						var db map[string]any
						if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
							t.Fatal("missing store")
						}
						if change == "project" {
							db["projects"].(map[string]any)["other-project"] = f.r.Config.Home
							db["items"].([]any)[0].(map[string]any)["project"] = "other-project"
						} else {
							db["projects"].(map[string]any)["cc-pc-tools-rs"] = filepath.Join(f.r.Config.Home, "other-root")
						}
						if err := publish(f.r.TodoPath, db); err != nil {
							t.Fatal(err)
						}
						changed, _ = os.ReadFile(f.r.TodoPath)
					}
					return backend(ctx, path, args...)
				}
				health := Health{}
				f.r.processConversations(context.Background(), &health)
				after, _ := os.ReadFile(f.r.TodoPath)
				if changed == nil || len(health.Errors) == 0 || string(changed) != string(after) {
					t.Fatal("guarded project/root change was mutated before refusal")
				}
			})
		}
	}
}

func TestConversationActualCanonicalWaitAndWithdrawalKeepGuardedRoot(t *testing.T) {
	for _, kind := range []string{"wait", "withdraw"} {
		t.Run(kind, func(t *testing.T) {
			var f *conversationFixture
			var w *withdrawalFixture
			if kind == "wait" {
				f = conversationNativeTest(t)
			} else {
				w = withdrawalTest(t, true)
				f = w.f
			}
			backend := f.r.Run
			var changed []byte
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if path == conversationTodo && args[0] == "question-"+kind {
					var db map[string]any
					if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
						t.Fatal("missing fixture")
					}
					db["projects"].(map[string]any)["cc-pc-tools-rs"] = filepath.Join(f.r.Config.Home, "different-root")
					if err := publish(f.r.TodoPath, db); err != nil {
						t.Fatal(err)
					}
					changed, _ = os.ReadFile(f.r.TodoPath)
				}
				return backend(ctx, path, args...)
			}
			var err error
			if kind == "wait" {
				_, err = f.r.conversationAsk(context.Background(), f.input)
			} else {
				_, err = w.run()
			}
			after, _ := os.ReadFile(f.r.TodoPath)
			if changed == nil || err == nil || string(changed) != string(after) {
				t.Fatal("scope change allowed wait or withdrawal mutation")
			}
		})
	}
}

func TestConversationActualCanonicalSameGenerationNewOwnerIsNotEdited(t *testing.T) {
	f := conversationNativeTest(t)
	f.mutateTodo(t, func(row map[string]any) { row["resume_generation"] = 1 })
	f.ask(t)
	f.mutateTodo(t, func(row map[string]any) { row["worker_receipt"] = map[string]any{"job": "new-worker", "generation": 1} })
	f.reply(t, 8, "a")
	before, _ := os.ReadFile(f.r.TodoPath)
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	after, _ := os.ReadFile(f.r.TodoPath)
	if len(health.Errors) == 0 || string(before) != string(after) {
		t.Fatal("same-generation replacement owner was edited")
	}
}

func TestConversationActualCanonicalGuardedProjectCannotChangeBeforeDeliverySnapshot(t *testing.T) {
	f := continuedNative(t)
	var db map[string]any
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil {
		t.Fatal("missing fixture")
	}
	db["projects"].(map[string]any)["other-project"] = f.r.Config.Home
	if err := publish(f.r.TodoPath, db); err != nil {
		t.Fatal(err)
	}
	f.reply(t, 8, "a")
	backend := f.r.Run
	var afterProjectChange []byte
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		out, err := backend(ctx, path, args...)
		if strings.Contains(path, "scrub391") && err == nil {
			f.mutateTodo(t, func(row map[string]any) { row["project"] = "other-project" })
			afterProjectChange, _ = os.ReadFile(f.r.TodoPath)
		}
		return out, err
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	after, _ := os.ReadFile(f.r.TodoPath)
	if afterProjectChange == nil || len(health.Errors) == 0 || string(afterProjectChange) != string(after) {
		t.Fatal("delivery mutated a different project after the original project guard")
	}
}

func TestConversationActualCanonicalContinuedDeliveryRejectsRacesAndOtherHolds(t *testing.T) {
	for _, change := range []string{"cas-note", "cas-generation", "new-question", "missing-origin", "stop", "dependency", "guard"} {
		t.Run(change, func(t *testing.T) {
			f := continuedNative(t)
			reply := f.reply(t, 8, "a")
			if !strings.HasPrefix(change, "cas-") {
				f.mutateTodo(t, func(row map[string]any) {
					switch change {
					case "new-question":
						row["owner_question_wait"].(map[string]any)["question"].(map[string]any)["request_id"] = "another-question"
					case "missing-origin":
						delete(row["owner_question_wait"].(map[string]any), "origin")
					case "stop":
						row["owner_stopped"] = true
					case "dependency":
						row["wait_on"] = []any{99}
					case "guard":
						f.deny = true
					}
				})
			}
			var expected []byte
			backend := f.r.Run
			f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
				if path == conversationTodo && args[0] == "question-answer" && strings.HasPrefix(change, "cas-") {
					f.mutateTodo(t, func(row map[string]any) {
						if change == "cas-note" {
							row["note"] = "後から届いた直接指示"
						} else {
							row["resume_generation"] = 3
						}
					})
					expected, _ = os.ReadFile(f.r.TodoPath)
				}
				return backend(ctx, path, args...)
			}
			before, _ := os.ReadFile(f.r.TodoPath)
			health := Health{}
			f.r.processConversations(context.Background(), &health)
			after, _ := os.ReadFile(f.r.TodoPath)
			if expected == nil {
				expected = before
			}
			if len(health.Errors) == 0 || string(after) != string(expected) {
				t.Fatal("rejected delivery changed task or lost the intervening instruction")
			}
			if req := f.r.State.Requests[reply.Message]; req == nil || req.Phase != "conversation_reply_saved" || req.Todo != 0 {
				t.Fatal("rejection reported a delivery")
			}
			if f.posts != 1 || f.close != 0 {
				t.Fatal("rejection resent a question or closed an unapplied answer")
			}
		})
	}
}

func TestConversationActualCanonicalContinuedDeliveryRecoversLostResultAndSeparatesEdits(t *testing.T) {
	f := continuedNative(t)
	reply := f.reply(t, 8, "a")
	backend := f.r.Run
	deliveries := 0
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		out, err := backend(ctx, path, args...)
		if path == conversationTodo && args[0] == "question-answer" && err == nil {
			deliveries++
			return "", errors.New("lost response after committed delivery")
		}
		return out, err
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) == 0 || f.r.State.Requests[reply.Message].Todo != 0 {
		t.Fatal("lost response was acknowledged before readback")
	}
	f.mutateTodo(t, func(row map[string]any) {
		row["status"] = "block"
		row["owner_stopped"] = true
		row["note"] = "新しい停止と直接指示"
		row["scheduler"].(map[string]any)["owner_instruction"] = "新しい担当への直接指示"
	})
	before, _ := os.ReadFile(f.r.TodoPath)
	f.r.processConversations(context.Background(), &Health{})
	after, _ := os.ReadFile(f.r.TodoPath)
	if string(before) != string(after) || deliveries != 1 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
		t.Fatal("recovery changed a new stop or lost the committed delivery")
	}
	// A text-preserving edit changes the actual saved source bytes. It must
	// never inherit the first delivery or implementation result.
	f.edited = map[string]string{reply.Message: time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano)}
	f.r.processConversations(context.Background(), &Health{})
	if req := f.r.State.Requests[reply.Message]; req.Phase != "conversation_reply_saved" {
		t.Fatal("edited answer inherited a stopped generation's delivery")
	}
	f.r.Run = backend
	f.mutateTodo(t, func(row map[string]any) { row["status"] = "doing"; delete(row, "owner_stopped") })
	health = Health{}
	f.r.processConversations(context.Background(), &health)
	var db struct {
		Items []struct {
			Records []json.RawMessage `json:"owner_question_delivery_receipts"`
		} `json:"items"`
	}
	if readJSON(f.r.TodoPath, maxTodo, &db) != nil || len(health.Errors) != 0 || len(db.Items[0].Records) != 2 || f.r.State.Requests[reply.Message].Phase != "conversation_reply_delivered" {
		t.Fatal("current edit did not receive its own durable delivery")
	}
	if f.close != 0 {
		t.Fatal("delivery claimed an implementation")
	}
}

// This optional acceptance check uses an actual freshly built canonical CLI
// against a temporary database. It never selects the production ToDo store.
func conversationNativeTest(t *testing.T) *conversationFixture {
	t.Helper()
	candidate := os.Getenv("CC_CONVERSATION_TODO_BINARY")
	if candidate == "" {
		t.Skip("fresh canonical ToDo binary not supplied")
	}
	if !filepath.IsAbs(candidate) {
		t.Fatal("candidate must be absolute")
	}
	f := conversationTest(t, true)
	f.r.Now = time.Now
	if err := publish(filepath.Join(f.r.Config.Home, "isolated-notify.json"), map[string]any{"guild_id": "100", "owner_user_id": "300", "application_id": "400", "task_channel_id": "200"}); err != nil {
		t.Fatal(err)
	}
	backend := f.r.Run
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path != conversationTodo {
			return backend(ctx, path, args...)
		}
		command := exec.CommandContext(ctx, candidate, args...)
		hide(command)
		command.WaitDelay = time.Second
		command.Env = append(os.Environ(), "CC_TODO_HOME="+f.r.Config.Home, "CC_TODO_JOBS="+filepath.Join(f.r.Config.Home, "isolated-jobs"), "CC_ASK_CONFIG="+filepath.Join(f.r.Config.Home, "isolated-notify.json"))
		out, err := command.Output()
		if err != nil {
			return "", errors.New("isolated canonical command rejected")
		}
		return string(out), nil
	}
	return f
}

func TestConversationActualCanonicalTodoRoundTrip(t *testing.T) {
	f := conversationNativeTest(t)
	f.ask(t)
	row, err := f.r.todo(42)
	if err != nil || row.Status != "block" || !conversationWaitRecorded(row, f.q) {
		t.Fatal("actual wait not recorded")
	}
	reply := f.reply(t, 8, "a")
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	if len(health.Errors) != 0 {
		t.Fatalf("actual answer transition failed: %v", health.Errors)
	}
	row, err = f.r.todo(42)
	if err != nil || row.Status != "todo" || len(row.QuestionAnswers) != 1 || !strings.Contains(row.Note, "以前の作業を保持") || !strings.Contains(row.Note, "conversation-owner-reply:") || strings.Contains(row.Note, "[discord-conversation-question-v1 ") || string(row.Spec) != `"unchanged-original-spec"` || len(row.Jobs) != 1 {
		t.Fatal("actual canonical release lost task state")
	}
	if req := f.r.State.Requests[reply.Message]; req == nil || req.Todo != 42 || req.Phase != "conversation_reply_delivered" {
		t.Fatal("actual answer was not linked to original task")
	}
}

func TestConversationActualCanonicalTodoBlockWinsOverStaleDelivery(t *testing.T) {
	f := conversationNativeTest(t)
	f.ask(t)
	reply := f.reply(t, 8, "a")
	backend := f.r.Run
	competed := false
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path == conversationTodo && args[0] == "edit" && !competed {
			competed = true
			if _, err := backend(ctx, path, "block", "42", "データ到着まで停止"); err != nil {
				return "", err
			}
		}
		return backend(ctx, path, args...)
	}
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	row, err := f.r.todo(42)
	if !competed || err != nil || len(health.Errors) == 0 || row.Status != "block" || row.Note != "データ到着まで停止" || !conversationWaitRecorded(row, f.q) || len(row.QuestionAnswers) != 0 {
		t.Fatal("canonical block was overwritten or released by stale delivery")
	}
	if req := f.r.State.Requests[reply.Message]; req == nil || req.Todo != 0 || req.Phase != "conversation_reply_saved" {
		t.Fatal("canonical rejection was reported as delivered")
	}
}
