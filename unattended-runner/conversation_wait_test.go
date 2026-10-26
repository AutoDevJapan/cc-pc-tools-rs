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
	"testing"
	"time"
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
					answers, _ := row["owner_question_answer_receipts"].([]any)
					for _, raw := range answers {
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

func TestConversationExplicitWaitLostIndexPublicationStillGetsMinuteDelivery(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		originalIndex, _ := os.ReadFile(f.r.conversationPath())
		if _, err := f.r.conversationWait(context.Background(), f.q.RequestID, false); err != nil {
			t.Fatal(err)
		}
		// Simulate restart with the last durable index from before its publish,
		// while keeping the separately committed canonical wait and intent.
		if err := os.WriteFile(f.r.conversationPath(), originalIndex, 0600); err != nil {
			t.Fatal(err)
		}
		f.reply(t, 8, "answer after index-save interruption")
		health := Health{}
		f.r.processConversations(context.Background(), &health)
		row, _ := f.r.todo(42)
		if len(health.Errors) != 0 || health.ConversationChecked != 1 || row.Status != "todo" || len(row.QuestionAnswers) != 1 {
			t.Fatalf("committed wait missed minute processing: %v", health.Errors)
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
		if result, err := f.r.conversationWait(context.Background(), f.q.RequestID, false); err != nil || result["outcome"] != "waiting" {
			t.Fatalf("precommit failure froze an obsolete owner: %v %v", result, err)
		}
	})
}

func TestConversationExplicitWaitLaterRepliesEditsAndLostAnswerResponse(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
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
	})
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

func TestConversationExplicitWaitReplyToAnotherQuestionKeepsCurrentWait(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		ctx := context.Background()
		first := f.q
		stamp, _ := time.Parse(time.RFC3339Nano, f.created)
		f.created = stamp.Add(time.Second).Format(time.RFC3339Nano)
		f.question = snowflake(stamp.Add(time.Second))
		f.input.RequestID = "another-independent-question"
		f.decision = json.RawMessage(`{"action":"second-question","todo_id":42}`)
		if err := os.WriteFile(f.input.DecisionFile, f.decision, 0600); err != nil {
			t.Fatal(err)
		}
		f.ask(t)
		// Both real synthetic questions were posted while work was runnable.
		// Register A through the actual locked CLI before B's answer arrives.
		row, _ := f.r.conversationTask(ctx, 42)
		scope, _ := f.r.conversationScopeFile(row, first)
		snapshot, _ := f.r.conversationSnapshotFile(row, first, "second-question")
		if _, err := f.r.Run(ctx, conversationTodo, "question-wait", "42", "--question-file", first.ReceiptFile, "--scope-file", scope, "--expected-state-file", snapshot); err != nil {
			t.Fatal(err)
		}
		row, _ = f.r.todo(42)
		wait := canonicalHash(row.QuestionWait)
		reply := f.reply(t, 8, "answer for independent question B")
		result, err := f.r.conversationWait(ctx, f.q.RequestID, false)
		row, _ = f.r.todo(42)
		if err != nil || result["outcome"] != "owner_reply_delivered" || row.Status != "block" || canonicalHash(row.QuestionWait) != wait || len(row.QuestionAnswers) != 0 || !strings.Contains(row.Note, reply.Body) {
			t.Fatalf("independent answer was lost or current wait changed: %v %v", result, err)
		}
	})
}

func TestConversationExplicitWaitContinuedDeliveryKeepsNewGeneration(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		f.mutateTodo(t, func(row map[string]any) { row["resume_generation"] = 1 })
		if _, err := f.r.conversationWait(context.Background(), f.q.RequestID, false); err != nil {
			t.Fatal(err)
		}
		f.mutateTodo(t, func(row map[string]any) {
			row["status"], row["resume_generation"] = "doing", 2
			row["worker_receipt"] = map[string]any{"job": nil, "generation": 2}
			row["note"] = "New direct instruction and current work remain."
		})
		reply := f.reply(t, 8, "owner answer retained across continued work")
		health := Health{}
		f.r.processConversations(context.Background(), &health)
		row, _ := f.r.todo(42)
		if len(health.Errors) != 0 || row.Status != "doing" || len(row.QuestionAnswers) != 0 || len(row.QuestionDeliveries) != 1 || !strings.Contains(row.Note, reply.Body) || string(row.ResumeGeneration) != "2" || !conversationWaitRecorded(row, f.q) {
			t.Fatalf("continued answer lost or new generation released: %v", health.Errors)
		}
	})
}

func TestConversationExplicitWaitLargeSnapshotCanRetryBeforeCommit(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		f.mutateTodo(t, func(row map[string]any) {
			row["note"] = strings.Repeat("n", 700<<10)
			row["note_history"] = []any{strings.Repeat("h", 500<<10)}
		})
		backend := f.r.Run
		calls := 0
		f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
			if path == conversationTodo && args[0] == "question-wait" {
				calls++
				if calls == 1 {
					return "", errors.New("synthetic deadline before canonical commit")
				}
			}
			return backend(ctx, path, args...)
		}
		if _, err := f.r.conversationWait(context.Background(), f.q.RequestID, false); err == nil {
			t.Fatal("precommit failure was reported successful")
		}
		result, err := f.r.conversationWait(context.Background(), f.q.RequestID, false)
		if err != nil || result["outcome"] != "waiting" || calls != 2 {
			t.Fatalf("large row retry stuck before commit: %v %v, calls=%d", result, err, calls)
		}
	})
}

func TestConversationExplicitWaitPostcommitReplyCheckReportsDurableWait(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		backend := f.r.Run
		reads := 0
		f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
			if args[0] == "recovery-replies" {
				reads++
				if reads == 2 {
					return "", errors.New("synthetic postcommit API timeout")
				}
			}
			return backend(ctx, path, args...)
		}
		result, err := f.r.conversationWait(context.Background(), f.q.RequestID, false)
		row, _ := f.r.todo(42)
		if err != nil || result["outcome"] != "waiting" || result["reply_check"] != "deferred" || row.Status != "block" || !conversationWaitRecorded(row, f.q) {
			t.Fatalf("committed wait was reported lost: %v %v", result, err)
		}
		f.reply(t, 8, "answer after temporary API failure")
		health := Health{}
		f.r.processConversations(context.Background(), &health)
		row, _ = f.r.todo(42)
		if len(health.Errors) != 0 || row.Status != "todo" || len(row.QuestionAnswers) != 1 {
			t.Fatalf("durable wait did not resume delivery: %v", health.Errors)
		}
	})
}

func TestConversationExplicitWaitInspectionAndAliasCreateNoArtifacts(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		index, _ := f.r.loadConversations()
		index.Aliases["same-posted-question-alias"] = conversationAlias{Target: f.q.RequestID, InputSHA: f.q.InputSHA}
		if err := publish(f.r.conversationPath(), index); err != nil {
			t.Fatal(err)
		}
		snapshot := func() map[string]string {
			files := map[string]string{}
			err := filepath.WalkDir(f.r.Config.Home, func(path string, entry os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !entry.IsDir() {
					b, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					files[path] = bytesHash(b)
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			return files
		}
		for _, answered := range []bool{false, true} {
			if answered {
				f.reply(t, 8, "owner answer inspected without writes")
			}
			before := snapshot()
			result, err := f.r.conversationWait(context.Background(), "same-posted-question-alias", true)
			if err != nil || result["request_id"] != f.q.RequestID || canonicalHash(before) != canonicalHash(snapshot()) {
				t.Fatalf("inspection created/changed artifacts or lost alias: %v %v", result, err)
			}
		}
	})
}

func TestConversationExplicitWaitWithdrawalPreservesAskAndReplays(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprintf("native=%t", native), func(t *testing.T) {
			w := withdrawalTestWithWait(t, native, false)
			f := w.f
			f.mutateTodo(t, func(row map[string]any) { row["note_history"] = []any{strings.Repeat("h", 1100<<10)} })
			if _, err := f.r.conversationWait(context.Background(), f.q.RequestID, false); err != nil {
				t.Fatal(err)
			}
			for attempt := 0; attempt < 2; attempt++ {
				result, err := f.r.conversationWithdraw(context.Background(), f.q.RequestID, w.reason)
				if err != nil || result["archived"] != true || result["withdrawn"] != true {
					t.Fatalf("explicit wait withdrawal failed: %v %v", result, err)
				}
			}
			index, _ := f.r.loadConversations()
			q := index.Questions[f.q.RequestID]
			row, _ := f.r.todo(42)
			if q.Wait || q.InputSHA != f.q.InputSHA || q.Phase != "withdrawn" || row.Status != "todo" || len(row.QuestionAnswers) != 0 || w.patches != 1 {
				t.Fatal("withdrawal changed ask input, manufactured answer or repeated archive")
			}
		})
	}
}

func TestConversationExplicitWaitCLIDeadlineAfterCommitKeepsReceipt(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		backend := f.r.Run
		reads := 0
		f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
			if args[0] == "recovery-replies" {
				reads++
				if reads == 2 {
					<-ctx.Done() // deadline only after the actual canonical commit/readback
					return "", ctx.Err()
				}
			}
			return backend(ctx, path, args...)
		}
		var out bytes.Buffer
		code := conversationCLIWithTimeout([]string{"wait", "--request-id=" + f.q.RequestID}, &out,
			func(string) (conversationCLIBackend, func(), error) { return f.r, func() {}, nil },
			func(context.Context, string) bool { return true }, 2*time.Second)
		var ack map[string]any
		row, _ := f.r.todo(42)
		if code != 0 || json.Unmarshal(out.Bytes(), &ack) != nil || ack["deadline"] != "after_wait_commit" || ack["reply_check"] != "deferred" || ack["outcome"] != "waiting" || row.Status != "block" || !conversationWaitRecorded(row, f.q) {
			t.Fatalf("CLI hid the durable wait after deadline: code=%d output=%s", code, out.String())
		}
	})
}

func TestConversationExplicitWaitNeverParksTheOriginOfAnotherRecipient(t *testing.T) {
	f := actorTest(t, false)
	f.input.Wait = false
	f.ask(t)
	before, _ := os.ReadFile(f.r.TodoPath)
	indexBefore, _ := os.ReadFile(f.r.conversationPath())
	for _, checkOnly := range []bool{false, true} {
		if _, err := f.r.conversationWait(context.Background(), f.q.RequestID, checkOnly); err == nil {
			t.Fatal("original-task wait accepted a question bound to a different recipient")
		}
	}
	after, _ := os.ReadFile(f.r.TodoPath)
	indexAfter, _ := os.ReadFile(f.r.conversationPath())
	if string(before) != string(after) || string(indexBefore) != string(indexAfter) {
		t.Fatal("the existing recipient contract was changed")
	}
	for _, name := range []string{"wait-state.json", "wait-intent.json"} {
		if _, err := os.Lstat(filepath.Join(f.r.Config.Home, "conversations", f.q.RequestID, name)); !os.IsNotExist(err) {
			t.Fatal("original-task wait created a recipient artifact")
		}
	}
	var out bytes.Buffer
	code := conversationCLIWithLoader([]string{"wait", "--request-id=" + f.q.RequestID, "--check-only"}, &out,
		func(string) (conversationCLIBackend, func(), error) { return f.r, func() {}, nil })
	if code != 1 || out.String() != "conversation unavailable: actor_scope\n" {
		t.Fatal("recipient mismatch lost its fixed diagnostic stage")
	}
}

func TestConversationExplicitWaitContinuedAuditKeepsAppliedPhase(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		f.mutateTodo(t, func(row map[string]any) { row["resume_generation"] = 1 })
		if _, err := f.r.conversationWait(context.Background(), f.q.RequestID, false); err != nil {
			t.Fatal(err)
		}
		f.mutateTodo(t, func(row map[string]any) {
			row["status"], row["resume_generation"] = "doing", 2
			row["worker_receipt"] = map[string]any{"job": nil, "generation": 2}
			row["note"] = "Current work in the continued generation."
		})
		reply := f.reply(t, 8, "retained answer in continued work")
		health := Health{}
		f.r.processConversations(context.Background(), &health)
		if len(health.Errors) != 0 {
			t.Fatal(health.Errors)
		}
		request := f.r.conversationRequest(reply.Message)
		request.Phase = "conversation_direction_recorded"
		index, _ := f.r.loadConversations()
		if err := f.r.deliverConversationReply(context.Background(), index.Questions[f.q.RequestID], reply, request); err != nil {
			t.Fatal(err)
		}
		row, _ := f.r.todo(42)
		if request.Phase != "conversation_direction_recorded" || len(row.QuestionDeliveries) != 1 || row.Status != "doing" || string(row.ResumeGeneration) != "2" {
			t.Fatal("audit downgraded application state or repeated delivery")
		}
	})
}

func TestConversationExplicitWaitKeepsCompletedActorDeliveryFastPath(t *testing.T) {
	f := actorTest(t, false)
	f.input.Wait = false
	f.ask(t)
	f.reply(t, 8, "already delivered to the original bound recipient")
	f.replies[0].Todo = f.input.Todo
	health := Health{}
	f.r.processConversations(context.Background(), &health)
	request := f.r.conversationRequest(f.replies[0].Message)
	if len(health.Errors) != 0 || request == nil || request.Phase != "conversation_reply_delivered" {
		t.Fatalf("positive recipient delivery missing: %v", health.Errors)
	}
	f.r.Run = func(context.Context, string, ...string) (string, error) {
		t.Fatal("completed recipient delivery was needlessly readmitted")
		return "", errors.New("unexpected command")
	}
	if err := f.r.deliverConversationReply(context.Background(), f.q, f.replies[0], request); err != nil {
		t.Fatal(err)
	}
}

func TestConversationExplicitWaitCLIDeadlineBeforeReadbackStillAcknowledgesCommit(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		backend := f.r.Run
		f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
			out, err := backend(ctx, path, args...)
			if path == conversationTodo && args[0] == "question-wait" && err == nil {
				<-ctx.Done()
				return "", ctx.Err()
			}
			return out, err
		}
		var out bytes.Buffer
		code := conversationCLIWithTimeout([]string{"wait", "--request-id=" + f.q.RequestID}, &out,
			func(string) (conversationCLIBackend, func(), error) { return f.r, func() {}, nil },
			func(context.Context, string) bool { return true }, 2*time.Second)
		var ack map[string]any
		row, _ := f.r.todo(42)
		if code != 0 || json.Unmarshal(out.Bytes(), &ack) != nil || ack["deadline"] != "after_wait_commit" || ack["outcome"] != "waiting" || row.Status != "block" || !conversationWaitRecorded(row, f.q) {
			t.Fatalf("deadline prevented bounded readback: code=%d output=%s", code, out.String())
		}
	})
}

func TestConversationExplicitWaitPostAnswerFailureReportsActualTaskState(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		backend := f.r.Run
		statePath := filepath.Join(f.r.Config.Home, "unattended-state.json")
		backup := statePath + ".before"
		blocked := false
		t.Cleanup(func() {
			if blocked {
				if err := os.Remove(statePath); err != nil {
					t.Error(err)
				}
				if err := os.Rename(backup, statePath); err != nil {
					t.Error(err)
				}
			}
		})
		f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
			out, err := backend(ctx, path, args...)
			if path == conversationTodo && args[0] == "question-wait" && err == nil {
				f.reply(t, 8, "answer committed before the runner state save failed")
			}
			if path == conversationTodo && args[0] == "question-answer" && err == nil && !blocked {
				if err := os.Rename(statePath, backup); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(statePath, 0700); err != nil {
					t.Fatal(err)
				}
				blocked = true
			}
			return out, err
		}
		result, err := f.r.conversationWait(context.Background(), f.q.RequestID, false)
		row, _ := f.r.todo(42)
		if err != nil || !blocked || result["outcome"] != "question_answered" || result["task_status"] != "todo" || result["reply_processing"] != "deferred" || !shaPattern.MatchString(fmt.Sprint(result["answer_receipt_sha256"])) || row.Status != "todo" || len(row.QuestionAnswers) != 1 {
			t.Fatalf("old block was reported after committed answer: %v %v", result, err)
		}
	})
}

func TestConversationExplicitWaitRetryDeliversAnswerAndRefreshesStatus(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		if _, err := f.r.conversationWait(context.Background(), f.q.RequestID, false); err != nil {
			t.Fatal(err)
		}
		f.reply(t, 8, "answer before the next minute poll")
		result, err := f.r.conversationWait(context.Background(), f.q.RequestID, false)
		row, _ := f.r.todo(42)
		if err != nil || result["outcome"] != "owner_reply_delivered" || result["task_status"] != "todo" || row.Status != "todo" || len(row.QuestionAnswers) != 1 {
			t.Fatalf("successful answer kept the old block status: %v %v", result, err)
		}
	})
}

func TestConversationExplicitWaitCLIAnswerCommitDeadlineKeepsReceipt(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		backend := f.r.Run
		f.r.Run = func(ctx context.Context, path string, args ...string) (string, error) {
			out, err := backend(ctx, path, args...)
			if path == conversationTodo && err == nil {
				if args[0] == "question-wait" {
					f.reply(t, 8, "answer committed at the CLI deadline")
				}
				if args[0] == "question-answer" {
					<-ctx.Done()
					return "", ctx.Err()
				}
			}
			return out, err
		}
		var out bytes.Buffer
		code := conversationCLIWithTimeout([]string{"wait", "--request-id=" + f.q.RequestID}, &out,
			func(string) (conversationCLIBackend, func(), error) { return f.r, func() {}, nil },
			func(context.Context, string) bool { return true }, 2*time.Second)
		var ack map[string]any
		row, _ := f.r.todo(42)
		if code != 0 || json.Unmarshal(out.Bytes(), &ack) != nil || ack["deadline"] != "after_answer_commit" || ack["outcome"] != "question_answered" || ack["task_status"] != "todo" || ack["reply_processing"] != "deferred" || row.Status != "todo" {
			t.Fatalf("CLI lost its committed answer: code=%d output=%s", code, out.String())
		}
	})
}

func TestConversationExplicitWaitCancellationDoesNotExtendReadback(t *testing.T) {
	explicitWaitVariants(t, func(t *testing.T, f *conversationFixture) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		backend := f.r.Run
		cancelled, extended := false, false
		f.r.Run = func(call context.Context, path string, args ...string) (string, error) {
			if cancelled && call.Err() == nil {
				extended = true
			}
			out, err := backend(call, path, args...)
			if path == conversationTodo && args[0] == "question-wait" && err == nil {
				cancelled = true
				cancel()
			}
			return out, err
		}
		if _, err := f.r.conversationWait(ctx, f.q.RequestID, false); err == nil || !cancelled || extended {
			t.Fatal("explicit cancellation was ignored by the readback")
		}
		row, _ := f.r.todo(42)
		if row.Status != "block" || !conversationWaitRecorded(row, f.q) {
			t.Fatal("cancellation rewrote the already committed wait")
		}
	})
}
