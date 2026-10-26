package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type conversationExplicitWaitReceipt struct {
	WaitSHA   string `json:"wait_receipt_sha256"`
	OriginSHA string `json:"origin_sha256"`
}

type conversationWaitIntent struct {
	Domain   string          `json:"domain"`
	Identity string          `json:"question_identity_sha256"`
	Origin   json.RawMessage `json:"origin"`
}

var errConversationWait = errors.New("conversation explicit wait deferred")

func conversationOwnerIdentity(row *TodoRow) json.RawMessage {
	var v struct {
		Generation json.RawMessage `json:"resume_generation"`
		Worker     json.RawMessage `json:"worker_receipt"`
		Scheduler  struct {
			Job json.RawMessage `json:"job"`
			Key json.RawMessage `json:"key"`
		} `json:"scheduler"`
	}
	if json.Unmarshal(row.conversationState, &v) != nil {
		return nil
	}
	b, _ := json.Marshal(map[string]json.RawMessage{"resume_generation": v.Generation, "worker_receipt": v.Worker, "job": v.Scheduler.Job, "key": v.Scheduler.Key})
	return b
}

// Compare every question field, not merely a reused request ID or owner ID.
func (r *Runner) conversationExactWait(raw json.RawMessage, q ConversationQuestion) bool {
	var wait struct {
		Domain   string          `json:"domain"`
		SHA      string          `json:"question_receipt_sha256"`
		Question json.RawMessage `json:"question"`
		Origin   json.RawMessage `json:"origin"`
	}
	b, err := boundedRegular(q.ReceiptFile)
	var expected conversationQuestionReceipt
	return err == nil && bytesHash(b) == q.ReceiptSHA && json.Unmarshal(b, &expected) == nil &&
		json.Unmarshal(raw, &wait) == nil && wait.Domain == "todo-owner-question-wait-v1" && wait.SHA == q.ReceiptSHA &&
		canonicalHash(wait.Question) == canonicalHash(json.RawMessage(b)) && len(wait.Origin) != 0 && string(wait.Origin) != "null"
}

func (r *Runner) conversationExactAnswer(row *TodoRow, q ConversationQuestion) bool {
	for _, raw := range row.QuestionAnswers {
		var receipt struct {
			Domain string                    `json:"domain"`
			Wait   json.RawMessage           `json:"wait"`
			Answer conversationAnswerReceipt `json:"answer"`
		}
		a := &receipt.Answer
		if json.Unmarshal(raw, &receipt) == nil && receipt.Domain == "todo-owner-question-answer-receipt-v1" && r.conversationExactWait(receipt.Wait, q) &&
			a.Domain == "discord-conversation-answer-v1" && a.RequestID == q.SenderID && a.Todo == fmt.Sprint(q.Todo) && a.Thread == q.Thread && a.Question == q.Message && a.Fingerprint == q.Fingerprint && a.QuestionSHA == q.ReceiptSHA && a.Author == r.State.Destination.Owner && a.Source == "discord-api-v10" {
			return true
		}
	}
	return false
}

// The canonical current wait is the authority, even if a process died before
// saving the explicit-wait hint. This never treats Wait=false as permission to
// release another question, owner, generation, dependency, or stop.
func (r *Runner) conversationReplyUsesWait(row *TodoRow, q ConversationQuestion) (bool, error) {
	if q.Wait {
		return true, nil
	}
	if r.conversationExactAnswer(row, q) {
		return true, nil
	}
	if len(row.QuestionWait) == 0 || string(row.QuestionWait) == "null" {
		return false, nil
	}
	if !conversationWaitRecorded(row, q) {
		var other struct {
			SHA      string `json:"question_receipt_sha256"`
			Question struct {
				Request string `json:"request_id"`
			} `json:"question"`
		}
		if json.Unmarshal(row.QuestionWait, &other) != nil || other.SHA == q.ReceiptSHA || other.Question.Request == q.SenderID {
			return false, errConversationWait
		}
		// Another real question's wait is retained. This independent reply
		// still belongs in the original task's note, without any wait release.
		return false, nil
	}
	if !r.conversationExactWait(row.QuestionWait, q) {
		return false, errConversationWait
	}
	var wait struct {
		Origin json.RawMessage `json:"origin"`
	}
	_ = json.Unmarshal(row.QuestionWait, &wait)
	if q.ExplicitWait != nil && q.ExplicitWait.OriginSHA != canonicalHash(wait.Origin) {
		return false, errConversationWait
	}
	path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "wait-intent.json")
	if b, err := boundedRegular(path); err == nil {
		var intent conversationWaitIntent
		if json.Unmarshal(b, &intent) != nil || intent.Domain != "discord-conversation-explicit-wait-v1" || intent.Identity != r.conversationCloseIdentity(q) || canonicalHash(intent.Origin) != canonicalHash(wait.Origin) {
			return false, errConversationWait
		}
	} else if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
		return false, errConversationWait
	}
	return true, nil
}

// Caller holds runner.lock, except a check-only inspection which never writes.
func (r *Runner) conversationWait(ctx context.Context, requestID string, checkOnly bool) (map[string]any, error) {
	index, err := r.loadConversations()
	if err != nil {
		return nil, err
	}
	if alias, ok := index.Aliases[requestID]; ok {
		requestID = alias.Target
	}
	q, exists := index.Questions[requestID]
	if !exists || !conversationID.MatchString(requestID) || q.Phase == "reserved" || q.Phase == "withdrawn" || q.Phase == "closed" || q.Phase == "applied" {
		return nil, errConversationWait
	}
	// The finite resumed-recipient contract owns its separate question/wait
	// envelope. This original-task operation must never park its origin row.
	if q.Actor != nil {
		return nil, errConversationWait
	}
	row, err := r.conversationTask(ctx, q.Todo)
	if err != nil || row.Project != q.Project {
		return nil, errConversationWait
	}
	proof, err := r.conversationProof(ctx, q) // sender checks current body, decision and saved attachment metadata
	if err != nil {
		return nil, err
	}
	b, err := boundedRegular(q.ReceiptFile)
	expected := conversationQuestionReceipt{Domain: "discord-conversation-question-v1", RequestID: q.SenderID, Todo: fmt.Sprint(q.Todo), Guild: r.State.Destination.Guild, Owner: r.State.Destination.Owner, Bot: r.State.Destination.Bot, Thread: q.Thread, Message: q.Message, Progress: q.Progress, Fingerprint: q.Fingerprint, Created: proof.Created}
	var saved conversationQuestionReceipt
	if err != nil || bytesHash(b) != q.ReceiptSHA || json.Unmarshal(b, &saved) != nil || saved != expected {
		return nil, errConversationWait
	}
	var live Message
	if r.get(ctx, "/channels/"+q.Thread+"/messages/"+q.Message, &live) != nil || live.ID != q.Message || live.Channel != q.Thread || live.Author.ID != expected.Bot || !live.Author.Bot || live.Timestamp != proof.Created {
		return nil, errConversationWait
	}
	if _, err = r.conversationCloseThreadGET(ctx, q); err != nil {
		return nil, err
	}
	result := conversationQuestionPublic(q)
	result["check_only"], result["task_status"], result["replies_verified"] = checkOnly, row.Status, len(proof.Replies)
	if len(proof.Replies) != 0 {
		result["outcome"] = "owner_reply_requires_delivery"
		if checkOnly {
			return result, nil
		}
		// Deliver before considering a wait. Never hide an already observed
		// answer behind a newly created unanswered hold.
		health := Health{}
		r.processConversationReplies(ctx, index, requestID, q, proof, &health)
		if len(health.Errors) != 0 || ctx.Err() != nil {
			return nil, errConversationWait
		}
		result["outcome"], result["phase"] = "owner_reply_delivered", index.Questions[requestID].Phase
		return result, nil
	}
	for _, req := range r.conversationRequests() {
		if req != nil && req.Conversation == q.RequestID {
			return nil, errConversationWait // saved/deleted/edited source needs reconciliation
		}
	}
	if conversationQuestionAnswered(row, q) {
		result["outcome"] = "already_answered"
		return result, nil
	}
	if q.Phase == "answered" {
		result["outcome"] = "answered_source_requires_reconciliation"
		return result, nil
	}
	active := r.conversationExactWait(row.QuestionWait, q) && conversationWaitUnchanged(row, q) && conversationWaitOwnerUnchanged(row)
	if active {
		if _, err := r.conversationReplyUsesWait(row, q); err != nil {
			return nil, err
		}
	}
	if !active && (row.Status != "todo" && row.Status != "doing" || len(row.QuestionWait) != 0 && string(row.QuestionWait) != "null") {
		result["outcome"] = "task_held"
		return result, nil
	}
	if checkOnly {
		result["outcome"] = "registration_requires_locked_check"
		if active {
			result["outcome"] = "waiting"
			result["wait_receipt_sha256"] = canonicalHash(row.QuestionWait)
		}
		return result, nil
	}
	intent := conversationWaitIntent{Domain: "discord-conversation-explicit-wait-v1", Identity: r.conversationCloseIdentity(q), Origin: conversationOwnerIdentity(row)}
	if len(intent.Origin) == 0 {
		return nil, errConversationWait
	}
	intentBytes, _ := json.Marshal(intent)
	if !active {
		scope, err := r.conversationScopeFile(row, q)
		if err != nil {
			return nil, err
		}
		// One CAS input under runner.lock, with the canonical store's row
		// ceiling rather than the small immutable question-envelope ceiling.
		path := filepath.Join(r.Config.Home, "conversations", requestID, "wait-state.json")
		if err = writeText(path, string(row.conversationState)); err != nil {
			return nil, err
		}
		_, _ = r.Run(ctx, conversationTodo, "question-wait", fmt.Sprint(q.Todo), "--question-file", q.ReceiptFile, "--scope-file", scope, "--expected-state-file", path)
		// Read back even after a lost successful subprocess response.
		current, readErr := r.conversationTask(ctx, q.Todo)
		if readErr != nil || current.Project != q.Project || !r.conversationExactWait(current.QuestionWait, q) || !conversationWaitUnchanged(current, q) || !conversationWaitOwnerUnchanged(current) || canonicalHash(conversationOwnerIdentity(current)) != canonicalHash(intent.Origin) {
			return nil, errConversationWait
		}
		row = current
	}
	// A rejected/precommit attempt must not freeze the question to that
	// worker forever. Only a verified canonical wait creates this intent.
	if err = writeConversationOnce(filepath.Join(r.Config.Home, "conversations", requestID, "wait-intent.json"), intentBytes); err != nil {
		result["intent_save"] = "deferred"
	}
	q.Phase = "waiting"
	q.ExplicitWait = &conversationExplicitWaitReceipt{WaitSHA: canonicalHash(row.QuestionWait), OriginSHA: canonicalHash(intent.Origin)}
	index.Questions[requestID] = q
	result["phase"], result["outcome"], result["task_status"], result["wait_receipt_sha256"] = q.Phase, "waiting", row.Status, q.ExplicitWait.WaitSHA
	if err = publish(r.conversationPath(), index); err != nil {
		result["index_save"] = "deferred"
		return result, nil // minute polling also reads the committed canonical wait
	}
	// A reply may have arrived between the first API read and the locked
	// registration. The durable waiting phase also keeps minute polling live
	// if this second API read or its delivery cannot finish in this call.
	fresh, err := r.conversationProof(ctx, q)
	if err != nil {
		result["reply_check"] = "deferred"
		return result, nil
	}
	if len(fresh.Replies) != 0 {
		health := Health{}
		r.processConversationReplies(ctx, index, requestID, q, fresh, &health)
		if len(health.Errors) != 0 || ctx.Err() != nil {
			result["reply_delivery"] = "deferred"
			return result, nil
		}
		result["outcome"], result["phase"] = "owner_reply_delivered", index.Questions[requestID].Phase
		if current, err := r.todo(q.Todo); err == nil {
			result["task_status"] = current.Status
		}
	}
	return result, nil
}
