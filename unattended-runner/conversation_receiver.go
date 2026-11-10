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

type conversationReceiverHold struct {
	Request string `json:"request_id"`
	Todo    uint64 `json:"todo_id"`
	Message string `json:"message_id"`
	Reason  string `json:"reason"`
}

type conversationReceiverCounts struct {
	Processed     int
	Audited       int
	Recovered     int
	SourceUnknown int
	MediaDeferred int
	Holds         []conversationReceiverHold
}

// This projection reports only work performed by the receiver, with explicit
// zeros. General discovery/scheduler health is deliberately not serialized.
type conversationReceiverHealth struct {
	Mode            string                     `json:"mode"`
	Started         string                     `json:"started_at"`
	Finished        string                     `json:"finished_at"`
	Questions       int                        `json:"questions"`
	Attempted       int                        `json:"attempted"`
	Succeeded       int                        `json:"succeeded"`
	Deferred        int                        `json:"deferred"`
	Processed       int                        `json:"processed"`
	Audited         int                        `json:"audited"`
	Recovered       int                        `json:"recovered"`
	KnownHold       int                        `json:"known_hold"`
	AlreadyAnswered int                        `json:"already_answered"`
	SourceUnknown   int                        `json:"source_unknown"`
	MediaDeferred   int                        `json:"media_deferred"`
	Holds           []conversationReceiverHold `json:"holds"`
	Errors          []string                   `json:"errors"`
}

func receiverHealth(h Health) conversationReceiverHealth {
	c := h.receiver
	if c == nil {
		c = &conversationReceiverCounts{}
	}
	holds := append([]conversationReceiverHold{}, c.Holds...)
	return conversationReceiverHealth{Mode: h.Mode, Started: h.Started, Finished: h.Finished,
		Questions: h.ConversationWaiting, Attempted: h.ConversationChecked,
		Succeeded: max(0, h.ConversationWaiting-h.ConversationPollDeferred), Deferred: h.ConversationPollDeferred,
		Processed: c.Processed, Audited: c.Audited, Recovered: c.Recovered, KnownHold: len(holds),
		AlreadyAnswered: h.ReceiverAlreadyAnswered, SourceUnknown: c.SourceUnknown,
		MediaDeferred: c.MediaDeferred, Holds: holds, Errors: append([]string{}, h.Errors...)}
}

func receiverDelivered(req *Request) bool {
	switch req.Phase {
	case "conversation_reply_delivered", "conversation_reply_applied", "conversation_direction_recorded", "conversation_implementation_recorded":
		return true
	}
	return false
}

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
	report := receiverHealth(h)
	if publish(filepath.Join(r.Config.Home, "conversation-receiver-health.json"), report) != nil {
		fmt.Fprintln(out, "conversation receiver health unavailable")
		return 1
	}
	if json.NewEncoder(out).Encode(report) != nil {
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
	h.receiver = &conversationReceiverCounts{}
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
func (r *Runner) conversationReceiverDelivery(ctx context.Context, q ConversationQuestion, proof *conversationProof, reply conversationReply, req *Request, h *Health) (bool, bool, error) {
	row, err := r.conversationDeliveryTask(ctx, q, req)
	if err != nil || row.Project != q.Project {
		h.ReceiverDeliveryHeld++
		return false, false, errors.New("receiver task scope unverified")
	}
	canonicalBefore := q.Actor == nil && !receiverDelivered(req) && r.conversationReceiverAnswered(ctx, row, q, proof, reply, req)
	if req.Phase == "conversation_source_verified" && canonicalBefore {
		h.ReceiverAlreadyAnswered++
		return false, true, nil
	}
	if row.Status == "recovery_unverified" {
		h.ReceiverDeliveryHeld++
		return false, false, errors.New("receiver task recovery unverified")
	}
	if row.Status == "done" || conversationActorHeld(row.conversationState) {
		// Holding an explicitly stopped/completed task is a normal outcome,
		// not a recurring technical failure. Still verify its current source:
		// an unknown or changed acquisition must not inherit a healthy skip.
		_, ready, err := r.conversationReplySource(ctx, q, proof, reply)
		if err != nil || !ready {
			h.ReceiverDeliveryHeld++
			h.receiver.SourceUnknown++
			return false, false, errors.New("receiver held source unverified")
		}
		h.ReceiverDeliveryHeld++
		reason := "task_hold"
		if row.Status == "done" {
			reason = "task_done"
		}
		h.receiver.Holds = append(h.receiver.Holds, conversationReceiverHold{Request: q.RequestID, Todo: row.ID, Message: reply.Message, Reason: reason})
		return false, canonicalBefore, nil
	}
	if req.Phase == "conversation_source_verified" && req.Todo == 0 {
		h.ReceiverDeliveryHeld++
		h.receiver.SourceUnknown++
		return false, false, errors.New("receiver retains source without task mutation")
	}
	return true, canonicalBefore, nil
}

func (r *Runner) conversationReceiverAnswered(ctx context.Context, row *TodoRow, q ConversationQuestion, proof *conversationProof, reply conversationReply, req *Request) bool {
	stamp, ready, err := r.conversationReplySource(ctx, q, proof, reply)
	if err != nil || !ready {
		return false
	}
	expected := conversationAnswerReceipt{Domain: "discord-conversation-answer-v1", RequestID: q.SenderID, Todo: fmt.Sprint(q.Todo), Thread: q.Thread, Question: q.Message, Fingerprint: q.Fingerprint, QuestionSHA: q.ReceiptSHA, Message: reply.Message, Author: reply.Author, Source: "discord-api-v10", ReplySHA: stamp.ReplySHA, Acquired: req.Acquired, ReceiptSHA: stamp.SourceSHA}
	// Follow-up answers use the existing delivery-only receipt namespace.
	// Reuse its exact envelope/byte-hash check, just as delivery recovery does.
	input, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		return false
	}
	if conversationDeliveryRecorded(row, q, expected, input) {
		return true
	}
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
