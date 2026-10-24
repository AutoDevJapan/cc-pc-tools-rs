package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const directionKind = "specification_direction"

var errDirection = errors.New("direction specification recording deferred")

type directionCheckMode int

const (
	directionWrite directionCheckMode = iota
	directionVerify
	directionVerifyNote
	directionCheckHolds
)

// This is API-attested input to the canonical ToDo writer, never an applied JSON.
// Owner text stays private. The public CLI returns only stage/receipt metadata.
type conversationDirectionInput struct {
	Domain           string `json:"domain"`
	Kind             string `json:"kind"`
	Source           string `json:"source"`
	RequestID        string `json:"request_id"`
	SenderID         string `json:"sender_request_id"`
	OriginTodo       string `json:"origin_todo_id"`
	ActorTodo        string `json:"actor_todo_id"`
	Job              string `json:"job_id"`
	Project          string `json:"project"`
	Revision         string `json:"revision"`
	Thread           string `json:"thread_id"`
	Question         string `json:"question_message_id"`
	Message          string `json:"message_id"`
	Author           string `json:"author_id"`
	Fingerprint      string `json:"question_fingerprint"`
	QuestionSHA      string `json:"question_receipt_sha256"`
	DecisionSHA      string `json:"decision_identity_sha256"`
	ReplySHA         string `json:"reply_sha256"`
	SourceSHA        string `json:"source_receipt_sha256"`
	OriginalQuestion string `json:"original_question"`
	QuestionCreated  string `json:"question_created_at"`
	OwnerReply       string `json:"owner_reply"`
	Created          string `json:"created_at"`
	Edited           string `json:"edited_at"`
	Acquired         string `json:"acquired_at"`
}
type conversationDirectionReceipt struct {
	Domain   string                     `json:"domain"`
	Kind     string                     `json:"kind"`
	Input    conversationDirectionInput `json:"input"`
	InputSHA string                     `json:"input_sha256"`
}
type conversationDirectionResult struct {
	Domain     string `json:"domain"`
	Kind       string `json:"kind"`
	Recorded   bool   `json:"recorded"`
	Changed    bool   `json:"changed"`
	InputSHA   string `json:"input_sha256"`
	ReceiptSHA string `json:"receipt_sha256"`
}

func directionQuestion(proof *conversationProof) bool {
	var decision struct {
		Decision string          `json:"decision"`
		Scope    json.RawMessage `json:"scope"`
	}
	return proof != nil && json.Unmarshal(proof.Decision, &decision) == nil && decision.Decision == "direction" &&
		len(decision.Scope) > 0 && !bytes.Equal(decision.Scope, []byte("null"))
}
func directionInput(q ConversationQuestion, proof *conversationProof, reply conversationReply, req *Request, sourceSHA string, actor uint64, job, revision string) conversationDirectionInput {
	return conversationDirectionInput{Domain: "discord-conversation-direction-v1", Kind: directionKind, Source: "discord-api-v10",
		RequestID: q.RequestID, SenderID: q.SenderID, OriginTodo: fmt.Sprint(q.Todo), ActorTodo: fmt.Sprint(actor), Job: job, Project: q.Project, Revision: strings.ToLower(revision),
		Thread: q.Thread, Question: q.Message, Message: reply.Message, Author: reply.Author, Fingerprint: q.Fingerprint, QuestionSHA: q.ReceiptSHA,
		DecisionSHA: q.DecisionSHA, ReplySHA: canonicalHash(reply), SourceSHA: sourceSHA, OriginalQuestion: proof.Body, QuestionCreated: proof.Created,
		OwnerReply: reply.Body, Created: req.Created, Edited: req.Edited, Acquired: req.Acquired}
}
func directionFile(r *Runner, q ConversationQuestion, id, sha string) string {
	return filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", id+"-"+sha+".direction.json")
}
func readDirection(path string) (conversationDirectionInput, []byte, error) {
	var input conversationDirectionInput
	b, missing, err := conversationCloseRead(path, 1<<20)
	if missing || err != nil {
		return input, nil, errDirection
	}
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		return input, nil, errDirection
	}
	return input, b, nil
}
func findDirection(row *TodoRow, q ConversationQuestion, message, source string) (*conversationDirectionReceipt, error) {
	var found *conversationDirectionReceipt
	for i := range row.DirectionRecords {
		receipt := &row.DirectionRecords[i]
		if receipt.Input.RequestID == q.RequestID && receipt.Input.Message == message && receipt.Input.SourceSHA == source {
			if found != nil || receipt.Domain != "todo-direction-record-v1" || receipt.Kind != directionKind || !shaPattern.MatchString(receipt.InputSHA) {
				return nil, errDirection
			}
			found = receipt
		}
	}
	return found, nil
}

// Both guarded row snapshots are passed to Rust. The guard check itself is not
// an exemption: Rust binds project/root and both exact rows under todo.lock.
func (r *Runner) directionCommand(ctx context.Context, q ConversationQuestion, input conversationDirectionInput, b []byte, mode directionCheckMode) (conversationDirectionResult, error) {
	var zero conversationDirectionResult
	actorID, ok := number(input.ActorTodo)
	if !ok || !validJob(input.Job) {
		return zero, errDirection
	}
	args := []string{"direction-record", "--input-file", directionFile(r, q, input.Message, bytesHash(b))}
	for _, selected := range []struct {
		name string
		id   uint64
	}{{"origin", q.Todo}, {"actor", actorID}} {
		row, err := r.conversationTask(ctx, selected.id)
		if err != nil || row.Project != q.Project || len(row.conversationState) == 0 {
			return zero, errDirection
		}
		base := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", input.Message+"-direction-"+selected.name)
		scope, _ := json.Marshal(row.conversationScope)
		if writeText(base+"-scope.json", string(scope)) != nil || writeText(base+"-state.json", string(row.conversationState)) != nil {
			return zero, errDirection
		}
		args = append(args, "--"+selected.name+"-scope-file", base+"-scope.json", "--"+selected.name+"-state-file", base+"-state.json")
	}
	if mode != directionWrite {
		args = append(args, "--verify-only")
	}
	if mode == directionVerifyNote {
		args = append(args, "--verify-note")
	}
	if mode == directionCheckHolds {
		args = append(args, "--check-holds")
	}
	raw, err := r.Run(ctx, conversationTodo, args...)
	var result conversationDirectionResult
	if err != nil || json.Unmarshal([]byte(raw), &result) != nil || result.Domain != "todo-direction-record-v1" || result.Kind != directionKind ||
		!result.Recorded || result.InputSHA != bytesHash(b) || !shaPattern.MatchString(result.ReceiptSHA) || (mode != directionWrite && result.Changed) {
		return zero, errDirection
	}
	return result, nil
}

func (r *Runner) conversationDirectionCheck(ctx context.Context, q ConversationQuestion, message, inputSHA string, mode directionCheckMode) (conversationDirectionResult, error) {
	var zero conversationDirectionResult
	if !shaPattern.MatchString(inputSHA) {
		return zero, errDirection
	}
	input, b, err := readDirection(directionFile(r, q, message, inputSHA))
	if err != nil || bytesHash(b) != inputSHA || input.RequestID != q.RequestID || input.Message != message {
		return zero, errDirection
	}
	return r.directionCommand(ctx, q, input, b, mode)
}
func (r *Runner) conversationDirectionApplied(ctx context.Context, q ConversationQuestion, proof *conversationProof, reply conversationReply, row *TodoRow, stamp conversationCloseStamp) (conversationCloseStamp, bool, error) {
	record, err := findDirection(row, q, reply.Message, stamp.SourceSHA)
	if err != nil || record == nil {
		return stamp, false, err
	}
	if !directionQuestion(proof) {
		return stamp, false, errDirection
	}
	input, b, err := readDirection(directionFile(r, q, reply.Message, record.InputSHA))
	actor, ok := number(input.ActorTodo)
	req := r.State.Requests[reply.Message]
	if err != nil || !ok || req == nil || input != record.Input || bytesHash(b) != record.InputSHA ||
		input != directionInput(q, proof, reply, req, stamp.SourceSHA, actor, input.Job, input.Revision) {
		return stamp, false, errDirection
	}
	result, err := r.directionCommand(ctx, q, input, b, directionVerify)
	if err != nil {
		return stamp, false, err
	}
	stamp.DirectionSHA, stamp.DirectionInputSHA = result.ReceiptSHA, result.InputSHA
	return stamp, true, nil
}

func (r *Runner) conversationDirectionRecord(ctx context.Context, opts conversationCLIOptions) (map[string]any, error) {
	index, err := r.loadConversations()
	if err != nil {
		return nil, errDirection
	}
	q, ok := index.Questions[opts.RequestID]
	if !ok || q.Phase == "reserved" {
		return nil, errDirection
	}
	proof, err := r.conversationProof(ctx, q)
	if err != nil || !directionQuestion(proof) {
		return nil, errDirection
	}
	if _, err = r.conversationCloseProofHash(q, proof); err != nil {
		return nil, errDirection
	}
	var selected *conversationReply
	for i := range proof.Replies {
		if proof.Replies[i].Message == opts.Message {
			selected = &proof.Replies[i]
		}
	}
	if selected == nil {
		return nil, errDirection
	}
	stamp, ready, err := r.conversationReplySource(ctx, q, proof, *selected)
	if err != nil || !ready || stamp.SourceSHA != opts.SourceSHA {
		return nil, errDirection
	}
	// Canonical saved sources are sufficient for separate implementations only.
	// Preserve direction-record's original requirement of actual delivery.
	req := r.State.Requests[opts.Message]
	if req == nil || req.Todo != q.Todo || req.Phase == "conversation_reply_saved" || req.Phase == "conversation_source_verified" {
		return nil, errDirection
	}
	row, err := r.conversationTask(ctx, q.Todo)
	if err != nil || row.Project != q.Project {
		return nil, errDirection
	}
	existing, err := findDirection(row, q, opts.Message, stamp.SourceSHA)
	if err != nil {
		return nil, errDirection
	}
	var result conversationDirectionResult
	if existing != nil {
		if existing.Input.ActorTodo != strconv.FormatUint(opts.Todo, 10) {
			return nil, errDirection
		}
		verified, applied, err := r.conversationDirectionApplied(ctx, q, proof, *selected, row, stamp)
		if err != nil || !applied {
			return nil, errDirection
		}
		result = conversationDirectionResult{Domain: "todo-direction-record-v1", Kind: directionKind, Recorded: true,
			InputSHA: verified.DirectionInputSHA, ReceiptSHA: verified.DirectionSHA}
	} else {
		job := os.Getenv("CC_JOB_ID")
		if !validJob(job) {
			return nil, errDirection
		}
		input := directionInput(q, proof, *selected, r.State.Requests[opts.Message], stamp.SourceSHA, opts.Todo, job, opts.Revision)
		b, _ := json.Marshal(input)
		if safeText(string(b), r.Token) != string(b) || writeConversationOnce(directionFile(r, q, opts.Message, bytesHash(b)), b) != nil {
			return nil, errDirection
		}
		result, err = r.directionCommand(ctx, q, input, b, directionWrite)
		if err != nil {
			return nil, errDirection
		}
		readback, err := r.conversationDirectionCheck(ctx, q, opts.Message, result.InputSHA, directionVerifyNote)
		if err != nil || readback.ReceiptSHA != result.ReceiptSHA {
			return nil, errDirection
		}
	}
	// Check the source again after the locked write. An edit never inherits the
	// old recording, even if Discord changed while Rust was persisting it.
	again, ready, err := r.conversationReplySource(ctx, q, proof, *selected)
	if err != nil || !ready || again.SourceSHA != stamp.SourceSHA {
		return nil, errDirection
	}
	req = r.State.Requests[opts.Message]
	previous := req.Phase
	req.Phase = "conversation_direction_recorded"
	if r.save() != nil {
		req.Phase = previous
		return nil, errDirection
	}
	if err = r.conversationProgress(ctx, q, req, "conversation_direction_recorded"); err != nil {
		return nil, errDirection
	}
	closed, err := r.closeConversation(ctx, q, proof)
	if err != nil {
		return nil, errDirection
	}
	if closed {
		// Progress posting saves Logs in this same index. Retain its verified
		// message/notification receipt rather than republishing the earlier copy.
		latest, err := r.loadConversations()
		if err != nil {
			return nil, errDirection
		}
		current, ok := latest.Questions[q.RequestID]
		if !ok || r.conversationCloseIdentity(current) != r.conversationCloseIdentity(q) {
			return nil, errDirection
		}
		current.Phase = "closed"
		latest.Questions[q.RequestID] = current
		if publish(r.conversationPath(), latest) != nil {
			return nil, errDirection
		}
	}
	return map[string]any{"domain": "todo-direction-record-v1", "kind": directionKind, "recorded": true, "changed": result.Changed,
		"input_sha256": result.InputSHA, "receipt_sha256": result.ReceiptSHA, "question_closed_get_verified": closed,
		"functional_implementation": "incomplete", "image_approval": false, "origin_todo_complete": false}, nil
}
