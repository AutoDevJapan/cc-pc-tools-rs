package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func existingStatusAnswerFixture(t *testing.T) (*progressFixture, *Request) {
	t.Helper()
	f := progressTestFixture(t)
	f.mu.Lock()
	f.source.Content = "<@400>\n今って作業してるの？"
	f.mu.Unlock()
	configureStateRequestForTest(f, 2705)
	answer := Message{ID: "1560865966252888147", Channel: f.source.Channel, Content: "PRIVATE_EXISTING_STATUS_ANSWER", Timestamp: f.now.UTC().Format(time.RFC3339Nano), Flags: 4096}
	answer.Author.ID = "400"
	answer.Author.Bot = true
	answer.Reference.Message = f.source.ID
	answer.Reference.Channel = f.source.Channel
	answer.Reference.Guild = "100"
	f.mu.Lock()
	f.messages[answer.Channel+"/"+answer.ID] = answer
	f.mu.Unlock()
	receipt := map[string]any{"domain": "owner-request-status-response-v1", "todo": 2705, "thread_id": f.source.Channel, "source_message_id": f.source.ID, "owner_id": "300", "source_body_sha256": bytesHash([]byte(f.source.Content)), "body_sha256": bytesHash([]byte(answer.Content)), "stage": "verified", "message_id": answer.ID}
	bytes, _ := json.Marshal(receipt)
	path := filepath.Join(t.TempDir(), "existing-answer.private.json")
	if err := os.WriteFile(path, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	c := &f.r.Config.ProductReports[0]
	c.Source.Purpose = "status_question"
	c.Source.AnswerMessage = answer.ID
	c.Source.AnswerReceipt = path
	c.Source.AnswerReceiptSHA = bytesHash(bytes)
	return f, f.r.State.Requests[f.source.ID]
}

func TestExisting2705StatusAnswerReportsOnlyQuestionCompletionWithoutResending(t *testing.T) {
	f, req := existingStatusAnswerFixture(t)
	var h Health
	f.r.reportProducts(context.Background(), &h)
	if f.sends != 1 || h.ProductReports != 1 {
		t.Fatal("existing verified answer did not produce its completion record", h)
	}
	f.mu.Lock()
	report := f.messages["801/901"]
	old := f.messages[f.source.Channel+"/1560865966252888147"]
	f.mu.Unlock()
	if !strings.Contains(report.Content, "当該質問への回答の完了記録") || !strings.Contains(report.Content, "/1560865966252888147") || strings.Contains(report.Content, old.Content) || old.Content != "PRIVATE_EXISTING_STATUS_ANSWER" {
		t.Fatal("status answer was resent or promoted to product completion")
	}
	if req.Todo != 2705 {
		t.Fatal("status answer was applied to another product")
	}
	f.r.NoticeAttempted = false
	f.rows[0]["status"] = "done"
	f.saveRows()
	f.now = f.now.Add(2 * time.Hour)
	f.r.reportProducts(context.Background(), &h)
	_ = f.r.results(context.Background())
	if f.sends != 1 {
		t.Fatal("already completed status answer was sent again")
	}
}

func TestAlreadyAnsweredStatusQueryDoesNotDuplicateSourceReplyEvenAfterRecapture(t *testing.T) {
	f, req := existingStatusAnswerFixture(t)
	req.Phase = "status_query"
	req.Todo = 0
	if err := f.r.answerStatusQuery(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if f.sends != 0 || req.Phase != "status_answered" || req.Todo != 2705 {
		t.Fatal("known external status reply was sent again or assigned elsewhere")
	}
	// A missing/changed proof is a verification wait, never permission to resend.
	req.Phase = "status_query"
	req.Todo = 0
	f.r.Config.ProductReports[0].Source.AnswerReceiptSHA = strings.Repeat("0", 64)
	if err := f.r.answerStatusQuery(context.Background(), req); err == nil || f.sends != 0 {
		t.Fatal("unavailable prior proof caused a duplicate answer")
	}
}

func TestExistingAnswerIsNotForgottenWhenAnotherReportConfigurationIsInvalid(t *testing.T) {
	f, req := existingStatusAnswerFixture(t)
	req.Phase, req.Todo = "status_query", 0
	f.r.Config.ProductReports = append(f.r.Config.ProductReports, ProductReportConfig{Todo: 0})
	if err := f.r.answerStatusQuery(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if f.sends != 0 || req.Phase != "status_answered" || req.Todo != 2705 {
		t.Fatal("unrelated invalid configuration caused duplicate or lost answer")
	}
}

func TestInvalidOrAmbiguousExistingAnswerPlanNeverFallsThroughToResend(t *testing.T) {
	for _, ambiguous := range []bool{false, true} {
		f, req := existingStatusAnswerFixture(t)
		req.Phase, req.Todo = "status_query", 0
		if ambiguous {
			f.r.Config.ProductReports = append(f.r.Config.ProductReports, f.r.Config.ProductReports[0])
		} else {
			f.r.Config.ProductReports[0].Source.Kind = "invalid-source-kind"
		}
		if err := f.r.answerStatusQuery(context.Background(), req); err == nil || f.sends != 0 {
			t.Fatal("invalid existing answer was treated as permission to resend")
		}
	}
}

func TestPureNewStatusQuestionBypassesProductGuardAndUsesSilentIdempotentSender(t *testing.T) {
	f := progressTestFixture(t)
	f.mu.Lock()
	f.source.Content = "<@!400>\n今って作業してるの？"
	f.mu.Unlock()
	f.r.Config.ProductReports = nil
	s := f.source
	req := &Request{Thread: s.Channel, Guild: "100", RecordVersion: 2, Message: s.ID, Author: "300", Created: s.Timestamp, Source: "discord-api-v10", Text: s.Content, Key: requestKey(s.Channel, s.ID), Phase: "status_query"}
	f.r.State.Requests[req.Message] = req
	f.loseResponse = true
	if err := f.r.answerStatusQuery(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if req.Todo != 0 || req.Phase != "status_answered" || f.sends != 1 {
		t.Fatal("status question waited for product execution or registered a task")
	}
	f.r.NoticeAttempted = false
	if err := f.r.results(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.sends != 1 {
		t.Fatal("idempotent status query sent a second effect")
	}
}

func TestPureStatusClassifierNeverAcceptsExecutionMoneyOrOldShortAnswer(t *testing.T) {
	for _, body := range []string{"a", "b", "今って作業してるの？有料プランを購入して", "今って作業してるの？ゲームを起動して", "作業を開始して", "quoted: 今って作業してるの？"} {
		if isPureStatusQuestion(body, "400") {
			t.Fatal("status question classification expanded an instruction or approval")
		}
	}
}

func TestStatusQuestionAllowsOnlyOneConfiguredBotMentionPrefix(t *testing.T) {
	for _, body := range []string{"今って作業してるの？", "<@400>\n今って作業してるの？", "  <@!400> 今って作業してるの？\n"} {
		if !isPureStatusQuestion(body, "400") {
			t.Fatal("configured bot status question was not recognized")
		}
	}
	for _, body := range []string{"<@401>\n今って作業してるの？", "<@400><@400>今って作業してるの？", "<@400>今って作業してるの？すべて止めて", "prefix <@400>今って作業してるの？"} {
		if isPureStatusQuestion(body, "400") {
			t.Fatal("another recipient or additional instruction became a status question")
		}
	}
}
