package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
)

// Same synthetic Discord question in both the fast runner tests and the actual
// canonical CLI acceptance tests. No production question/body/image is a fixture.
func explicitWaitTest(t *testing.T, native bool) *conversationFixture {
	t.Helper()
	f := conversationTest(t, false)
	if native {
		f = conversationNativeTest(t)
		f.input.Wait = false
	}
	f.ask(t)
	server(t, f.r, func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/channels/"+f.question {
			respond(w, map[string]any{"id": f.question, "guild_id": "100", "parent_id": "200", "owner_id": "400", "type": 11, "thread_metadata": map[string]any{"archived": false}})
			return
		}
		if req.URL.Path == "/channels/"+f.question+"/messages/"+f.question {
			respond(w, map[string]any{"id": f.question, "channel_id": f.question, "timestamp": f.created, "author": map[string]any{"id": "400", "bot": true}})
			return
		}
		for _, reply := range f.replies {
			if req.URL.Path == "/channels/"+f.question+"/messages/"+reply.Message {
				respond(w, map[string]any{"id": reply.Message, "channel_id": f.question, "timestamp": reply.Created, "edited_timestamp": f.edited[reply.Message], "content": reply.Body, "author": map[string]any{"id": reply.Author, "bot": false}})
				return
			}
		}
		t.Errorf("unexpected synthetic API request %s", req.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})
	if native {
		return f
	}
	backend := f.r.Run
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		if path != conversationTodo || len(args) == 0 || args[0] != "question-wait" {
			out, err := backend(ctx, path, args...)
			if path == conversationTodo && args[0] == "question-answer" && err == nil {
				f.mutateTodo(t, func(row map[string]any) {
					for _, raw := range row["owner_question_answer_receipts"].([]any) {
						raw.(map[string]any)["domain"] = "todo-owner-question-answer-receipt-v1"
					}
				})
			}
			return out, err
		}
		row, err := f.r.conversationTask(ctx, 42)
		if err != nil {
			return "", err
		}
		var expected json.RawMessage
		if readJSON(argValue(args, "--expected-state-file"), maxTodo, &expected) != nil || canonicalHash(expected) != canonicalHash(row.conversationState) {
			return "", errors.New("synthetic canonical state_changed")
		}
		var raw map[string]json.RawMessage
		_ = json.Unmarshal(row.conversationState, &raw)
		if row.Status != "doing" && row.Status != "todo" || len(row.WaitOn) != 0 || string(raw["owner_stopped"]) == "true" {
			return "", errors.New("synthetic canonical other_hold")
		}
		origin := conversationOwnerIdentity(row)
		out, err := backend(ctx, path, args...)
		if err == nil {
			f.mutateTodo(t, func(row map[string]any) {
				wait := row["owner_question_wait"].(map[string]any)
				wait["domain"], wait["origin"] = "todo-owner-question-wait-v1", origin
			})
		}
		return out, err
	}
	return f
}

func explicitWaitVariants(t *testing.T, run func(*testing.T, *conversationFixture)) {
	t.Helper()
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprintf("native=%t", native), func(t *testing.T) { run(t, explicitWaitTest(t, native)) })
	}
}

func TestConversationExplicitWaitRoundTripAndRetryKeepsAskImmutable(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		ctx := context.Background()
		original, _ := os.ReadFile(f.q.ReceiptFile)
		for attempt := 0; attempt < 2; attempt++ {
			result, err := f.r.conversationWait(ctx, f.q.RequestID, false)
			if err != nil || result["outcome"] != "waiting" || result["wait_requested"] != false {
				t.Fatalf("explicit wait failed: %v %v", result, err)
			}
		}
		if strings.Count(strings.Join(f.commands, ","), "question-wait") > 1 {
			t.Fatal("retry registered another wait")
		}
		reply := f.reply(t, 8, "a: synthetic complete answer")
		health := Health{}
		f.r.processConversations(ctx, &health)
		row, err := f.r.todo(42)
		if err != nil || len(health.Errors) != 0 || health.ConversationChecked != 1 || row.Status != "todo" || len(row.QuestionAnswers) != 1 || !strings.Contains(row.Note, reply.Body) || len(row.Jobs) != 1 {
			t.Fatalf("exact wait did not resume original task: %v %v", err, health.Errors)
		}
		if len(row.QuestionWait) != 0 && string(row.QuestionWait) != "null" {
			t.Fatal("wait survived answer")
		}
		result, err := f.r.conversationWait(ctx, f.q.RequestID, false)
		if err != nil || result["outcome"] != "owner_reply_delivered" {
			t.Fatalf("answered retry failed: %v %v", result, err)
		}
		index, _ := f.r.loadConversations()
		q := index.Questions[f.q.RequestID]
		current, _ := os.ReadFile(q.ReceiptFile)
		if q.Wait || q.InputSHA != f.q.InputSHA || q.RequestID != f.q.RequestID || string(current) != string(original) || q.ExplicitWait == nil || f.posts != 1 {
			t.Fatal("explicit wait rewrote ask identity or reposted question")
		}
	})
}

func TestConversationExplicitWaitOwnerReplyPrecedesRegistration(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		reply := f.reply(t, 8, "b: answer arrived while independent work continued")
		result, err := f.r.conversationWait(context.Background(), f.q.RequestID, false)
		row, _ := f.r.todo(42)
		if err != nil || result["outcome"] != "owner_reply_delivered" || row.Status != "doing" || !strings.Contains(row.Note, reply.Body) || strings.Contains(strings.Join(f.commands, ","), "question-wait") {
			t.Fatalf("early answer hidden by wait: %v %v", result, err)
		}
	})
}

func TestConversationExplicitWaitInspectionAndHeldTaskDoNotMutate(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		f.mutateTodo(t, func(row map[string]any) { row["status"], row["wait_on"] = "wait", []int{99} })
		before, _ := os.ReadFile(f.r.TodoPath)
		indexBefore, _ := os.ReadFile(f.r.conversationPath())
		for _, check := range []bool{true, false} {
			result, err := f.r.conversationWait(context.Background(), f.q.RequestID, check)
			if err != nil || result["outcome"] != "task_held" {
				t.Fatalf("held task was not preserved: %v %v", result, err)
			}
		}
		after, _ := os.ReadFile(f.r.TodoPath)
		indexAfter, _ := os.ReadFile(f.r.conversationPath())
		if string(before) != string(after) || string(indexBefore) != string(indexAfter) || strings.Contains(strings.Join(f.commands, ","), "question-wait") {
			t.Fatal("inspection/held task wrote state")
		}
	})
}

func TestConversationExplicitWaitLostSuccessfulResponseReconcilesReceipt(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		backend := f.r.Run
		calls := 0
		f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
			out, err := backend(ctx, path, args...)
			if path == conversationTodo && args[0] == "question-wait" {
				calls++
				if err == nil {
					return "", errors.New("synthetic response lost after commit")
				}
			}
			return out, err
		}
		for attempt := 0; attempt < 2; attempt++ {
			result, err := f.r.conversationWait(context.Background(), f.q.RequestID, false)
			if err != nil || result["outcome"] != "waiting" {
				t.Fatalf("durable wait was lost: %v %v", result, err)
			}
		}
		if calls != 1 || f.posts != 1 {
			t.Fatal("successful wait retried its mutation or question")
		}
	})
}

func TestConversationExplicitWaitConcurrentOwnerChangeRejectsCAS(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		backend := f.r.Run
		f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
			if path == conversationTodo && args[0] == "question-wait" {
				f.mutateTodo(t, func(row map[string]any) { row["resume_generation"] = 99 })
			}
			return backend(ctx, path, args...)
		}
		if _, err := f.r.conversationWait(context.Background(), f.q.RequestID, false); err == nil {
			t.Fatal("concurrent new generation was parked")
		}
		row, _ := f.r.todo(42)
		if row.Status != "doing" || len(row.QuestionWait) != 0 {
			t.Fatal("CAS rejection mutated current task")
		}
		f.r.Run = backend
		if _, err := f.r.conversationWait(context.Background(), f.q.RequestID, false); err == nil {
			t.Fatal("retry silently rebound the old wait intent to another generation")
		}
	})
}

func TestConversationExplicitWaitNativeLaterRepliesEditsAndLostAnswerResponse(t *testing.T) {
	f := explicitWaitTest(t, true)
	ctx := context.Background()
	if _, err := f.r.conversationWait(ctx, f.q.RequestID, false); err != nil {
		t.Fatal(err)
	}
	backend := f.r.Run
	lost := false
	f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
		out, err := backend(ctx, path, args...)
		if path == conversationTodo && args[0] == "question-answer" && err == nil && !lost {
			lost = true
			return "", errors.New("synthetic lost answer response")
		}
		return out, err
	}
	f.reply(t, 8, "a: original answer")
	for attempt := 0; attempt < 2; attempt++ {
		health := Health{}
		f.r.processConversations(ctx, &health)
		if attempt == 1 && len(health.Errors) != 0 {
			t.Fatal(health.Errors)
		}
	}
	f.reply(t, 10, "extra owner instruction")
	f.replies[0].Body = "b: edited owner answer"
	f.edited = map[string]string{f.replies[0].Message: f.replies[1].Created}
	health := Health{}
	f.r.processConversations(ctx, &health)
	row, _ := f.r.todo(42)
	if !lost || len(health.Errors) != 0 || row.Status != "todo" || len(row.QuestionAnswers) != 1 || len(row.QuestionDeliveries) != 2 || !strings.Contains(row.Note, "b: edited owner answer") || !strings.Contains(row.Note, "extra owner instruction") {
		t.Fatalf("later versions were lost or released another wait: %v", health.Errors)
	}
}

func TestConversationExplicitWaitAnswerArrivingDuringRegistrationIsDelivered(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		backend := f.r.Run
		f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
			if path == conversationTodo && args[0] == "question-wait" {
				f.reply(t, 8, "owner answered during CAS")
			}
			return backend(ctx, path, args...)
		}
		result, err := f.r.conversationWait(context.Background(), f.q.RequestID, false)
		row, _ := f.r.todo(42)
		if err != nil || result["outcome"] != "owner_reply_delivered" || row.Status != "todo" || len(row.QuestionAnswers) != 1 {
			t.Fatalf("racing answer left an obsolete hold: %v %v", result, err)
		}
	})
}

func TestConversationExplicitWaitNativeRefusesDifferentQuestionOwnerAndHolds(t *testing.T) {
	for _, change := range []string{"question", "same_owner_other_thread", "generation", "owner", "dependency", "stop", "new_block"} {
		t.Run(change, func(t *testing.T) {
			f := explicitWaitTest(t, true)
			if _, err := f.r.conversationWait(context.Background(), f.q.RequestID, false); err != nil {
				t.Fatal(err)
			}
			f.mutateTodo(t, func(row map[string]any) {
				wait := row["owner_question_wait"].(map[string]any)
				switch change {
				case "question":
					wait["question"].(map[string]any)["request_id"] = "another-question"
				case "same_owner_other_thread":
					wait["question"].(map[string]any)["thread_id"] = "999"
				case "generation":
					row["resume_generation"] = 2
				case "owner":
					row["scheduler"] = map[string]any{"job": "new-worker"}
				case "dependency":
					row["wait_on"] = []int{99}
				case "stop":
					row["owner_stopped"] = true
				case "new_block":
					row["note"] = "another explicit block"
				}
			})
			f.reply(t, 8, "a")
			health := Health{}
			f.r.processConversations(context.Background(), &health)
			row, _ := f.r.todo(42)
			if len(health.Errors) == 0 || row.Status != "block" || len(row.QuestionAnswers) != 0 {
				t.Fatalf("released a different hold: %v", health.Errors)
			}
		})
	}
}
