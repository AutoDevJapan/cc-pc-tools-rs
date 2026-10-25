package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
)

// This is a delivery/source recovery stage, never an application receipt.
// The original wait request survives; neither question-wait nor question-answer
// may be synthesized after an already answered auxiliary-link interruption.
type conversationAnsweredRecovery struct {
	Message     string   `json:"message_id"`
	ReplySHA    string   `json:"reply_sha256"`
	RawSHA      string   `json:"ordinary_source_sha256"`
	InputSHA    string   `json:"original_input_sha256"`
	Intake      uint64   `json:"intake_todo_id"`
	Actor       uint64   `json:"actor_todo_id"`
	Unconfirmed []string `json:"auxiliary_links_unconfirmed"`
}

func onlyAuxiliaryLinks(links []string) bool {
	seen := map[string]bool{}
	for _, link := range links {
		if (link != "question-links" && link != "origin-links") || seen[link] {
			return false
		}
		seen[link] = true
	}
	return true
}

func validAnsweredRecovery(q ConversationQuestion) bool {
	p := q.AnsweredRecovery
	if p == nil {
		return q.Phase != "link_recovery"
	}
	_, valid := number(p.Message)
	return valid && p.Actor != 0 && p.Actor != q.Todo && p.Intake != 0 && p.Intake != q.Todo &&
		shaPattern.MatchString(p.ReplySHA) && shaPattern.MatchString(p.RawSHA) && p.InputSHA == q.InputSHA &&
		len(p.Unconfirmed) != 0 && onlyAuxiliaryLinks(p.Unconfirmed) && (q.Phase == "link_recovery" || q.Phase == "closed")
}

func answeredRecoveryProof(q ConversationQuestion, proof *conversationProof) bool {
	p := q.AnsweredRecovery
	if p == nil || !validAnsweredRecovery(q) || proof == nil || !proof.QuestionDelivery || proof.Fingerprint != q.Fingerprint || proof.Origin != q.OriginThread || !slices.Equal(proof.Images, q.AttachmentHashes) ||
		len(proof.Replies) != 1 || proof.Replies[0].Message != p.Message || canonicalHash(proof.Replies[0]) != p.ReplySHA || !onlyAuxiliaryLinks(proof.UnconfirmedLinks) {
		return false
	}
	version, exists := proof.ReplyVersions[p.Message]
	return exists && version.Edited == nil
}

func (r *Runner) conversationRecoverAnswered(ctx context.Context, opts conversationCLIOptions) (map[string]any, error) {
	index, err := r.loadConversations()
	if err != nil {
		return nil, errImplementation
	}
	q, exists := index.Questions[opts.RequestID]
	if !exists || (q.Phase != "reserved" && q.Phase != "link_recovery") || opts.Todo == q.Todo || opts.IntakeTodo == q.Todo {
		return nil, errImplementation
	}
	// Guard and authenticate the real current actor before any repair write.
	for _, id := range []uint64{q.Todo, opts.IntakeTodo, opts.Todo} {
		row, err := r.conversationTask(ctx, id)
		if err != nil || row.Project != q.Project || row.Status == "done" || row.Status == "recovery_unverified" || linkStopped(row.conversationState) {
			return nil, errImplementation
		}
		candidate := q
		candidate.Thread, candidate.Message = opts.Thread, opts.Thread
		if id == opts.Todo && !r.linkActor(row, candidate, opts) {
			return nil, errImplementation
		}
	}
	body, e1 := boundedRegular(q.BodyFile)
	decision, e2 := boundedRegular(q.DecisionFile)
	identity := []any{q.Todo, q.OriginThread, bytesHash(body), canonicalHash(json.RawMessage(decision)), q.Title, q.Wait}
	if len(q.AttachmentHashes) != 0 {
		identity = append(identity, q.AttachmentHashes)
	}
	if e1 != nil || e2 != nil || canonicalHash(identity) != q.InputSHA {
		return nil, errImplementation
	}
	raw, err := r.Run(ctx, conversationSender, "recovery-replies", "--thread", opts.Thread, "--message", opts.Thread, "--request-id", q.SenderID, "--todo-id", fmt.Sprint(q.Todo), "--read-only")
	var proof conversationProof
	if err != nil || json.Unmarshal([]byte(raw), &proof) != nil || !proof.QuestionDelivery || len(proof.Replies) != 1 || proof.Replies[0].Message != opts.Message || proof.Origin != q.OriginThread || !shaPattern.MatchString(proof.Fingerprint) ||
		bytesHash([]byte(proof.Body)) != bytesHash(body) || canonicalHash(proof.OriginalDecision) != canonicalHash(json.RawMessage(decision)) || !slices.Equal(proof.Images, q.AttachmentHashes) {
		return nil, errImplementation
	}
	if q.AnsweredRecovery == nil {
		if proof.SenderComplete || len(proof.UnconfirmedLinks) == 0 || !onlyAuxiliaryLinks(proof.UnconfirmedLinks) {
			return nil, errImplementation
		}
		q.Thread, q.Message, q.Progress = opts.Thread, opts.Thread, proof.Progress
		if _, valid := number(q.Progress); !valid {
			return nil, errImplementation
		}
		q.Fingerprint, q.QuestionSHA, q.DecisionSHA = proof.Fingerprint, bytesHash(body), canonicalHash(proof.Decision)
		q.Phase = "link_recovery"
		q.AnsweredRecovery = &conversationAnsweredRecovery{Message: opts.Message, ReplySHA: canonicalHash(proof.Replies[0]), RawSHA: opts.SourceSHA, InputSHA: q.InputSHA, Intake: opts.IntakeTodo, Actor: opts.Todo, Unconfirmed: proof.UnconfirmedLinks}
	} else if q.Thread != opts.Thread || q.AnsweredRecovery.Actor != opts.Todo || q.AnsweredRecovery.Intake != opts.IntakeTodo || q.AnsweredRecovery.RawSHA != opts.SourceSHA {
		return nil, errImplementation
	}
	if _, err := r.conversationCloseProofHash(q, &proof); err != nil {
		return nil, errImplementation
	}
	receipt := conversationQuestionReceipt{Domain: "discord-conversation-question-v1", RequestID: q.SenderID, Todo: fmt.Sprint(q.Todo), Guild: r.State.Destination.Guild, Owner: r.State.Destination.Owner, Bot: r.State.Destination.Bot, Thread: q.Thread, Message: q.Message, Progress: q.Progress, Fingerprint: q.Fingerprint, Created: proof.Created}
	b, _ := json.MarshalIndent(receipt, "", "  ")
	q.ReceiptFile, q.ReceiptSHA = filepath.Join(r.Config.Home, "conversations", q.RequestID, "question.json"), bytesHash(b)
	if writeConversationOnce(q.ReceiptFile, b) != nil {
		return nil, errImplementation
	}
	// The loader lock protects this one index entry. Leave the reply, task and
	// sender journals intact even when a later source or application check fails.
	linked, err := r.conversationIntakeLink(ctx, opts, q, &proof, proof.Replies[0])
	if err != nil {
		return nil, err
	}
	index.Questions[q.RequestID] = q
	if publish(r.conversationPath(), index) != nil {
		return nil, errImplementation
	}
	linked["domain"], linked["request_id"] = "discord-conversation-answered-link-recovery-v1", q.RequestID
	linked["question_delivery_verified"], linked["auxiliary_links_unconfirmed"] = true, q.AnsweredRecovery.Unconfirmed
	linked["question_closed_get_verified"], linked["application_inferred"], linked["origin_resumed"] = false, false, false
	return linked, nil
}
