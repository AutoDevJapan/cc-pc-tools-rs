package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// The signature pins the original generation/recipient. Only the same canonical
// question wait (or its answer receipt) may change the task's eligible phase.
func (s taskQuestionState) eligible(q *ConversationQuestion, owner string) bool {
	if s.retired {
		return q != nil
	}
	var row TodoRow
	var generation uint64
	if json.Unmarshal(s.current, &row) != nil || json.Unmarshal(s.Generation, &generation) != nil || generation == 0 || generation != s.Worker.Generation {
		return false
	}
	noWait := len(row.QuestionWait) == 0 || string(row.QuestionWait) == "null"
	if q == nil {
		return noWait && (row.Status == "doing" || row.Status == "todo")
	}
	if !q.Wait || q.Todo != s.ID || q.Project != s.Project || q.Actor != nil || q.deliveryActor != nil || q.originTodo != 0 || q.RecoveredIntake != 0 {
		return false
	}
	if noWait {
		if row.Status != "doing" && row.Status != "todo" {
			return false
		}
		for _, raw := range row.QuestionAnswers {
			var record struct {
				Domain string                    `json:"domain"`
				Wait   json.RawMessage           `json:"wait"`
				Answer conversationAnswerReceipt `json:"answer"`
			}
			if json.Unmarshal(raw, &record) == nil && record.Domain == "todo-owner-question-answer-receipt-v1" &&
				record.Answer.RequestID == q.SenderID && record.Answer.Todo == fmt.Sprint(q.Todo) && record.Answer.Thread == q.Thread &&
				record.Answer.Question == q.Message && record.Answer.QuestionSHA == q.ReceiptSHA && record.Answer.Fingerprint == q.Fingerprint &&
				record.Answer.Source == "discord-api-v10" && record.Answer.Author == owner && taskQuestionWaitMatches(s.current, record.Wait, *q) {
				return true
			}
		}
		return false
	}
	row.conversationState = s.current
	return taskQuestionWaitMatches(s.current, row.QuestionWait, *q) && conversationWaitUnchanged(&row, *q)
}

func taskQuestionWaitMatches(current, raw json.RawMessage, q ConversationQuestion) bool {
	var wait struct {
		Domain   string                      `json:"domain"`
		Question conversationQuestionReceipt `json:"question"`
		Origin   json.RawMessage             `json:"origin"`
	}
	if json.Unmarshal(raw, &wait) != nil || wait.Domain != "todo-owner-question-wait-v1" || len(wait.Origin) == 0 || string(wait.Origin) == "null" {
		return false
	}
	question, _ := json.MarshalIndent(wait.Question, "", "  ")
	row := TodoRow{conversationState: current, QuestionWait: raw}
	return bytesHash(question) == q.ReceiptSHA && conversationWaitOwnerUnchanged(&row)
}

// The caller cannot nominate a task/recipient by supplying a user-looking JSON
// file. Reuse the retained index, immutable question receipt, sender API proof,
// saved source validation and exact live owner post used by the ordinary flow.
func (r *Runner) taskQuestionReplyInput(ctx context.Context, q ConversationQuestion, req *Request) (ConversationAskInput, error) {
	if req == nil || r.conversationRequest(req.Message) != req {
		return ConversationAskInput{}, errTaskQuestion
	}
	input, proof, err := r.taskQuestionRetainedInput(ctx, q)
	if err != nil {
		return input, err
	}
	matched := false
	for _, reply := range proof.Replies {
		if reply.Message != req.Message {
			continue
		}
		_, ready, err := r.conversationReplySource(ctx, q, proof, reply)
		if err != nil || !ready || matched {
			return ConversationAskInput{}, errTaskQuestion
		}
		matched = true
	}
	if !matched {
		return ConversationAskInput{}, errTaskQuestion
	}
	return input, nil
}

// This read supports ask retries after the exact wait was committed. A retained
// question alone never authorizes an answer mutation; that also needs its source.
func (r *Runner) taskQuestionRetainedInput(ctx context.Context, q ConversationQuestion) (ConversationAskInput, *conversationProof, error) {
	input := ConversationAskInput{}
	bad := func() (ConversationAskInput, *conversationProof, error) { return input, nil, errTaskQuestion }
	if !conversationMediaNormal.MatchString(q.RequestID) || q.SenderID != "conversation-v1-"+bytesHash([]byte(q.RequestID))[:32] ||
		q.Actor != nil || q.deliveryActor != nil || q.originTodo != 0 || !q.Wait || q.RecoveredIntake != 0 || len(q.Attachments) != 1 || len(q.AttachmentHashes) != 1 ||
		(q.Phase != "posted" && q.Phase != "waiting" && q.Phase != "answered") {
		return bad()
	}
	index, err := r.loadConversations()
	if err != nil || canonicalHash(index.Questions[q.RequestID]) != canonicalHash(q) {
		return bad()
	}
	dir := filepath.Join(r.Config.Home, "conversations", q.RequestID)
	if q.BodyFile != filepath.Join(dir, "question.txt") || q.DecisionFile != filepath.Join(dir, "decision.json") || q.ReceiptFile != filepath.Join(dir, "question.json") {
		return bad()
	}
	b, err := generatedQuestionRead(q.ReceiptFile, 128<<10)
	var receipt conversationQuestionReceipt
	if err != nil || bytesHash(b) != q.ReceiptSHA || historyStrictJSON(b, &receipt) != nil {
		return bad()
	}
	want := conversationQuestionReceipt{Domain: "discord-conversation-question-v1", RequestID: q.SenderID, Todo: fmt.Sprint(q.Todo), Guild: r.State.Destination.Guild, Owner: r.State.Destination.Owner, Bot: r.State.Destination.Bot, Thread: q.Thread, Message: q.Message, Progress: q.Progress, Fingerprint: q.Fingerprint, Created: receipt.Created}
	created, err := time.Parse(time.RFC3339Nano, receipt.Created)
	if receipt != want || err != nil || created.After(r.Now()) {
		return bad()
	}
	proof, err := r.conversationProof(ctx, q)
	if err != nil || proof.Fingerprint != q.Fingerprint || proof.Created != receipt.Created {
		return bad()
	}
	body, err := generatedQuestionRead(q.BodyFile, 128<<10)
	if err != nil || bytesHash(body) != q.QuestionSHA {
		return bad()
	}
	decision, err := generatedQuestionRead(q.DecisionFile, 128<<10)
	if err != nil || canonicalHash(json.RawMessage(decision)) != canonicalHash(proof.OriginalDecision) || taskQuestionDecisionImage(decision) != q.AttachmentHashes[0] {
		return bad()
	}
	// Decision identity normalizes resource order; input identity and image
	// qualification bind the full original decision, not that projection.
	identity := []any{q.Todo, q.OriginThread, bytesHash(body), canonicalHash(json.RawMessage(decision)), q.Title, q.Wait, q.AttachmentHashes}
	if q.InputSHA != canonicalHash(identity) {
		return bad()
	}
	input = ConversationAskInput{RequestID: q.RequestID, Todo: q.Todo, OriginThread: q.OriginThread, Title: q.Title, Wait: q.Wait, BodyFile: q.BodyFile, DecisionFile: q.DecisionFile, Attachments: q.Attachments, heldQuestion: &q}
	return input, proof, nil
}

// Persist the complete answer as this recipient's pending instruction before
// releasing the same wait. Both operations use the existing locked canonical
// CLI; no launch, new sender, generation change or general asset permission.
func (r *Runner) deliverTaskQuestionReply(ctx context.Context, row *TodoRow, q ConversationQuestion, reply conversationReply, req *Request, receipt []byte, marker string) error {
	answer, b, answerFile, reacquired, err := r.conversationAnswerInput(q, reply, req, receipt)
	if err != nil {
		return err
	}
	if conversationAnswerRecorded(row, q, answer, b) {
		req.Todo, req.Phase = q.Todo, "conversation_reply_delivered"
		return r.save()
	}
	if conversationQuestionAnswered(row, q) {
		return r.deliverContinuedConversationReply(ctx, row, q, reply, req, receipt, marker)
	}
	if !conversationWaitUnchanged(row, q) || !conversationWaitOwnerUnchanged(row) {
		return errTaskQuestion
	}
	ready, err := conversationPreservedReplyReady(row, q, answer, b)
	if err != nil {
		return err
	}
	if !ready {
		if reacquired {
			return errTaskQuestion
		}
		row, err = r.recordConversationReplyKeepingWait(ctx, row, q, reply, req, receipt, marker, true)
		if err != nil {
			return err
		}
	}
	// Revalidate source/registration, expiry, stops and original generation after
	// the durable append. Never mark the intake delivered before wait release.
	row, err = r.conversationDeliveryTask(ctx, q, req)
	if err != nil {
		return err
	}
	if ready, err = conversationPreservedReplyReady(row, q, answer, b); err != nil || !ready {
		return errTaskQuestion
	}
	scope, err := r.conversationScopeFile(row, q)
	if err != nil {
		return err
	}
	registry := taskQuestionProduction()
	if r.TaskQuestion != nil {
		registry = *r.TaskQuestion
	}
	if err := registry.recordRelease(row, q, answer, b); err != nil {
		return err
	}
	if _, err = r.Run(ctx, conversationTodo, "question-answer", fmt.Sprint(q.Todo), "--answer-file", answerFile, "--scope-file", scope); err != nil {
		return err
	}
	current, err := r.todo(q.Todo)
	if err != nil || current.Project != q.Project || !conversationAnswerRecorded(current, q, answer, b) ||
		!hasNoteMarker(current.Note, marker) || !hasNoteMarker(current.conversationInstruction(), marker) || !strings.Contains(current.conversationInstruction(), reply.Body) {
		return errTaskQuestion
	}
	req.Todo, req.Phase = q.Todo, "conversation_reply_delivered"
	return r.save()
}
