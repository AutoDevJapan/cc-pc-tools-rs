package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"
)

// One pass under the original runner lock. The existing Windows task supplies
// the minute cadence; this command never runs the general periodic pipeline.
const conversationReceiverBudget = 50 * time.Second

func conversationReceiverCLI(args []string, out io.Writer) int {
	return conversationReceiverCLIWith(args, out, func() (*Runner, func(), error) {
		backend, unlock, err := loadConversationCLIRunner(conversationDefaultConfig)
		if err != nil {
			return nil, nil, err
		}
		r, ok := backend.(*Runner)
		if !ok {
			unlock()
			return nil, nil, errors.New("receiver unavailable")
		}
		return r, unlock, nil
	})
}

func conversationReceiverCLIWith(args []string, out io.Writer, load func() (*Runner, func(), error)) int {
	if len(args) != 0 {
		fmt.Fprintln(out, "usage: unattended-runner conversation-receiver-tick")
		return 2
	}
	r, unlock, err := load()
	if errors.Is(err, errConversationBusy) {
		fmt.Fprintln(out, "busy")
		return 2
	}
	if err != nil {
		fmt.Fprintln(out, "conversation receiver unavailable")
		return 1
	}
	defer unlock()
	ctx, cancel := context.WithTimeout(context.Background(), conversationReceiverBudget)
	defer cancel()
	h := r.conversationReceiverTick(ctx)
	if publish(filepath.Join(r.Config.Home, "conversation-receiver-health.json"), h) != nil {
		fmt.Fprintln(out, "conversation receiver health unavailable")
		return 1
	}
	if json.NewEncoder(out).Encode(h) != nil {
		return 1
	}
	if len(h.Errors) != 0 {
		return 1
	}
	return 0
}

func (r *Runner) conversationReceiverTick(ctx context.Context) Health {
	previous := r.ConversationReceiverOnly
	r.ConversationReceiverOnly = true
	defer func() { r.ConversationReceiverOnly = previous }()
	h := Health{Mode: "discord-conversation-receiver-only", Started: r.Now().UTC().Format(time.RFC3339Nano), Errors: []string{}}
	r.processConversations(ctx, &h)
	if ctx.Err() != nil {
		h.Errors = append(h.Errors, "conversation_receiver_deadline")
	}
	h.Finished = r.Now().UTC().Format(time.RFC3339Nano)
	return h
}

// A received source and permission to wake a task are separate. Do not edit a
// completed/stopped task, or turn a source-only acquisition into a delivery.
// Existing exact answer receipts may prove historical acceptance even for a
// desktop worker which has no codex-jobs execution receipt. This observation
// neither invents an applied receipt nor changes any phase, owner or wait.
func (r *Runner) conversationReceiverDelivery(ctx context.Context, q ConversationQuestion, proof *conversationProof, reply conversationReply, req *Request, h *Health) (bool, error) {
	row, err := r.conversationDeliveryTask(ctx, q)
	if err != nil || row.Project != q.Project {
		h.ReceiverDeliveryHeld++
		return false, errors.New("receiver task scope unverified")
	}
	if q.Actor == nil && req.Phase == "conversation_source_verified" && r.conversationReceiverAnswered(ctx, row, q, proof, reply, req) {
		h.ReceiverAlreadyAnswered++
		return false, nil
	}
	if row.Status == "recovery_unverified" {
		h.ReceiverDeliveryHeld++
		return false, errors.New("receiver task recovery unverified")
	}
	if row.Status == "done" || conversationActorHeld(row.conversationState) {
		// Holding an explicitly stopped/completed task is a normal outcome,
		// not a recurring technical failure. Still verify its current source:
		// an unknown or changed acquisition must not inherit a healthy skip.
		_, ready, err := r.conversationReplySource(ctx, q, proof, reply)
		if err != nil || !ready {
			h.ReceiverDeliveryHeld++
			return false, errors.New("receiver held source unverified")
		}
		h.ReceiverDeliveryHeld++
		return false, nil
	}
	if req.Phase == "conversation_source_verified" && req.Todo == 0 {
		h.ReceiverDeliveryHeld++
		return false, errors.New("receiver retains source without task mutation")
	}
	return true, nil
}

func (r *Runner) conversationReceiverAnswered(ctx context.Context, row *TodoRow, q ConversationQuestion, proof *conversationProof, reply conversationReply, req *Request) bool {
	stamp, ready, err := r.conversationReplySource(ctx, q, proof, reply)
	if err != nil || !ready {
		return false
	}
	expected := conversationAnswerReceipt{Domain: "discord-conversation-answer-v1", RequestID: q.SenderID, Todo: fmt.Sprint(q.Todo), Thread: q.Thread, Question: q.Message, Fingerprint: q.Fingerprint, QuestionSHA: q.ReceiptSHA, Message: reply.Message, Author: reply.Author, Source: "discord-api-v10", ReplySHA: stamp.ReplySHA, Acquired: req.Acquired, ReceiptSHA: stamp.SourceSHA}
	matched := 0
	for _, raw := range row.QuestionAnswers {
		var record struct {
			Domain string                    `json:"domain"`
			Answer conversationAnswerReceipt `json:"answer"`
			SHA    string                    `json:"answer_file_sha256"`
			At     string                    `json:"applied_at"`
			Wait   struct {
				SHA      string `json:"question_receipt_sha256"`
				Question struct {
					Request string `json:"request_id"`
				} `json:"question"`
			} `json:"wait"`
		}
		if json.Unmarshal(raw, &record) != nil || record.Domain != "todo-owner-question-answer-receipt-v1" || record.Answer != expected || !shaPattern.MatchString(record.SHA) || record.Wait.SHA != q.ReceiptSHA || record.Wait.Question.Request != q.SenderID {
			continue
		}
		at, e1 := time.Parse(time.RFC3339Nano, record.At)
		acquired, e2 := time.Parse(time.RFC3339Nano, req.Acquired)
		if e1 == nil && e2 == nil && !at.Before(acquired) && !at.After(r.Now()) {
			matched++
		}
	}
	return matched == 1
}
