package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

var errImplementation = errors.New("implementation recording deferred")

// The input is immutable. Only the canonical ToDo writer appends a receipt,
// while holding both exact snapshots under the one existing todo.lock.
type conversationImplementationInput struct {
	conversationDirectionInput
	Completion string `json:"completion"`
	Execution  string `json:"execution"`
	Origin     string `json:"origin_saved"`
	Identity   string `json:"identity"`
	AppliedAt  string `json:"applied_at"`
}
type conversationImplementationReceipt struct {
	Domain   string                          `json:"domain"`
	Kind     string                          `json:"kind"`
	Input    conversationImplementationInput `json:"input"`
	InputSHA string                          `json:"input_sha256"`
}
type conversationImplementationEvidence struct {
	Completion string `json:"completion"`
	Execution  string `json:"execution"`
	Origin     string `json:"origin_saved"`
	Identity   string `json:"identity"`
	AppliedAt  string `json:"applied_at"`
}

func implementationFile(r *Runner, q ConversationQuestion, id, sha string) string {
	return filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", id+"-"+sha+".implementation.json")
}
func readImplementation(path string) (conversationImplementationInput, []byte, error) {
	var input conversationImplementationInput
	b, missing, err := conversationCloseRead(path, 1<<20)
	if missing || err != nil {
		return input, nil, errImplementation
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&input) != nil || d.Decode(new(any)) != io.EOF {
		return input, nil, errImplementation
	}
	return input, b, nil
}
func findImplementation(row *TodoRow, q ConversationQuestion, id, source string) (*conversationImplementationReceipt, error) {
	var found *conversationImplementationReceipt
	for i := range row.ImplementationRecords {
		receipt := &row.ImplementationRecords[i]
		if receipt.Input.RequestID == q.RequestID && receipt.Input.Message == id && receipt.Input.SourceSHA == source {
			if found != nil || receipt.Domain != "todo-implementation-record-v1" || receipt.Kind != "implementation" || !shaPattern.MatchString(receipt.InputSHA) {
				return nil, errImplementation
			}
			found = receipt
		}
	}
	return found, nil
}
func (r *Runner) implementationCommand(ctx context.Context, q ConversationQuestion, input conversationImplementationInput, b []byte, mode directionCheckMode) (conversationDirectionResult, error) {
	var result conversationDirectionResult
	actor, ok := number(input.ActorTodo)
	if !ok || actor == q.Todo || !validJob(input.Job) {
		return result, errImplementation
	}
	args := []string{"implementation-record", "--input-file", implementationFile(r, q, input.Message, bytesHash(b))}
	for _, selected := range []struct {
		name string
		id   uint64
	}{{"origin", q.Todo}, {"actor", actor}} {
		row, err := r.conversationTask(ctx, selected.id)
		if err != nil || len(row.conversationState) == 0 {
			return result, errImplementation
		}
		if selected.name == "actor" && row.Project == q.Project && (input.IntakeSourceFile != "" || input.IntakeSourceSHA != "") {
			return result, errImplementation
		}
		if row.Project != q.Project {
			path, sha, ok := r.ordinaryAssignment(q, input.Message, row)
			if selected.name != "actor" || !ok || input.IntakeSourceFile != path || input.IntakeSourceSHA != sha {
				return result, errImplementation
			}
		}
		base := implementationFile(r, q, input.Message, bytesHash(b)) + "-" + selected.name
		scope, _ := json.Marshal(row.conversationScope)
		if writeText(base+"-scope.json", string(scope)) != nil || writeText(base+"-state.json", string(row.conversationState)) != nil {
			return result, errImplementation
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
	if err != nil || json.Unmarshal([]byte(raw), &result) != nil || result.Domain != "todo-implementation-record-v1" || result.Kind != "implementation" || !result.Recorded || result.InputSHA != bytesHash(b) || !shaPattern.MatchString(result.ReceiptSHA) || (mode != directionWrite && result.Changed) {
		return result, errImplementation
	}
	return result, nil
}
func (r *Runner) implementationCheck(ctx context.Context, q ConversationQuestion, id, sha string, mode directionCheckMode) (conversationDirectionResult, error) {
	var zero conversationDirectionResult
	if !shaPattern.MatchString(sha) {
		return zero, errImplementation
	}
	input, b, err := readImplementation(implementationFile(r, q, id, sha))
	if err != nil || bytesHash(b) != sha || input.RequestID != q.RequestID || input.Message != id {
		return zero, errImplementation
	}
	return r.implementationCommand(ctx, q, input, b, mode)
}
func (r *Runner) conversationImplementationApplied(ctx context.Context, q ConversationQuestion, proof *conversationProof, reply conversationReply, receipt *conversationImplementationReceipt, stamp conversationCloseStamp) (conversationCloseStamp, bool, error) {
	input, b, err := readImplementation(implementationFile(r, q, reply.Message, receipt.InputSHA))
	actor, ok := number(input.ActorTodo)
	req := r.conversationRequest(reply.Message)
	if err != nil || !ok || req == nil || input != receipt.Input || bytesHash(b) != receipt.InputSHA {
		return stamp, false, errImplementation
	}
	expected := directionInput(q, proof, reply, req, stamp.SourceSHA, actor, input.Job, input.Revision)
	expected.Domain, expected.Kind = "discord-conversation-implementation-v1", "implementation"
	if input.IntakeSourceFile != "" {
		row, err := r.conversationTask(ctx, actor)
		if err != nil {
			return stamp, false, errImplementation
		}
		path, sha, ok := r.ordinaryAssignment(q, reply.Message, row)
		if !ok {
			return stamp, false, errImplementation
		}
		expected.IntakeSourceFile, expected.IntakeSourceSHA = path, sha
	}
	if input.conversationDirectionInput != expected {
		return stamp, false, errImplementation
	}
	result, err := r.implementationCommand(ctx, q, input, b, directionVerify)
	if err != nil {
		return stamp, false, err
	}
	stamp.ImplementationSHA, stamp.ImplementationInputSHA = result.ReceiptSHA, result.InputSHA
	return stamp, true, nil
}
func (r *Runner) implementationQuestion(ctx context.Context, opts conversationCLIOptions) (ConversationQuestion, *conversationProof, *conversationReply, error) {
	index, err := r.loadConversations()
	if err != nil {
		return ConversationQuestion{}, nil, nil, errImplementation
	}
	q, ok := index.Questions[opts.RequestID]
	if !ok || q.Phase == "reserved" {
		return q, nil, nil, errImplementation
	}
	proof, err := r.conversationProof(ctx, q)
	if err != nil {
		return q, nil, nil, errImplementation
	}
	if _, err = r.conversationCloseProofHash(q, proof); err != nil {
		return q, nil, nil, errImplementation
	}
	for i := range proof.Replies {
		if proof.Replies[i].Message == opts.Message {
			return q, proof, &proof.Replies[i], nil
		}
	}
	if opts.Command == "close" {
		return q, proof, nil, nil
	}
	return q, nil, nil, errImplementation
}
func (r *Runner) conversationImplementationCommand(ctx context.Context, opts conversationCLIOptions) (map[string]any, error) {
	if opts.Command == "recover-question" {
		return r.recoverConversationQuestion(ctx, opts)
	}
	q, proof, reply, err := r.implementationQuestion(ctx, opts)
	if err != nil {
		return nil, err
	}
	if opts.Command == "source-link" {
		return r.conversationIntakeLink(ctx, opts, q, proof, *reply)
	}
	if opts.Command == "source-reacquire" {
		return r.conversationSourceReacquire(ctx, q, proof, *reply)
	}
	if opts.Command == "close" {
		closed, err := r.closeConversationAuthorized(ctx, q, proof, true)
		if err != nil {
			return nil, err
		}
		if closed {
			latest, err := r.loadConversations()
			if err != nil || r.conversationCloseIdentity(latest.Questions[q.RequestID]) != r.conversationCloseIdentity(q) {
				return nil, errImplementation
			}
			current := latest.Questions[q.RequestID]
			current.Phase = "closed"
			latest.Questions[q.RequestID] = current
			if publish(r.conversationPath(), latest) != nil {
				return nil, errImplementation
			}
		}
		return map[string]any{"domain": conversationCloseDomain, "request_id": q.RequestID, "question_closed_get_verified": closed, "origin_resumed": false}, nil
	}
	stamp, ready, err := r.conversationReplySource(ctx, q, proof, *reply)
	if err != nil || !ready {
		return nil, errImplementation
	}
	if opts.Command == "source-verify" {
		return map[string]any{"domain": "discord-conversation-source-verification-v1", "request_id": q.RequestID, "todo_id": q.Todo, "message_id": reply.Message, "source_receipt_sha256": stamp.SourceSHA, "reply_sha256": stamp.ReplySHA, "canonical_current": true, "delivery_inferred": false, "application_inferred": false}, nil
	}
	if opts.Command != "implementation-record" || opts.SourceSHA != stamp.SourceSHA || opts.Todo == q.Todo {
		return nil, errImplementation
	}
	// Reject private inputs before reading their contents if the normal guard
	// cannot confirm their scope. This is never a raw applied-file exemption.
	raw, err := r.Run(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", opts.EvidenceFile)
	var guard struct {
		Client     *bool `json:"client_resource"`
		Unverified *bool `json:"classification_unverified"`
	}
	if err != nil || json.Unmarshal([]byte(raw), &guard) != nil || guard.Client == nil || *guard.Client || guard.Unverified == nil || *guard.Unverified {
		return nil, errImplementation
	}
	b, err := boundedRegular(opts.EvidenceFile)
	if err != nil {
		return nil, errImplementation
	}
	var evidence conversationImplementationEvidence
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&evidence) != nil || d.Decode(new(any)) != io.EOF {
		return nil, errImplementation
	}
	for _, s := range []string{evidence.Completion, evidence.Execution, evidence.Origin, evidence.Identity, evidence.AppliedAt} {
		if strings.TrimSpace(s) == "" {
			return nil, errImplementation
		}
	}
	row, err := r.conversationTask(ctx, q.Todo)
	if err != nil || row.Project != q.Project {
		return nil, errImplementation
	}
	existing, err := findImplementation(row, q, reply.Message, stamp.SourceSHA)
	if err != nil {
		return nil, err
	}
	input := conversationImplementationInput{Completion: evidence.Completion, Execution: evidence.Execution, Origin: evidence.Origin, Identity: evidence.Identity, AppliedAt: evidence.AppliedAt}
	job := os.Getenv("CC_JOB_ID")
	if existing != nil {
		job = existing.Input.Job
	}
	if !validJob(job) {
		return nil, errImplementation
	}
	input.conversationDirectionInput = directionInput(q, proof, *reply, r.conversationRequest(reply.Message), stamp.SourceSHA, opts.Todo, job, opts.Revision)
	input.Domain, input.Kind = "discord-conversation-implementation-v1", "implementation"
	actorRow, err := r.conversationTask(ctx, opts.Todo)
	if err != nil {
		return nil, errImplementation
	}
	if actorRow.Project != q.Project {
		path, sha, ok := r.ordinaryAssignment(q, reply.Message, actorRow)
		if !ok {
			return nil, errImplementation
		}
		input.IntakeSourceFile, input.IntakeSourceSHA = path, sha
	}
	b, _ = json.Marshal(input)
	if existing != nil && (input != existing.Input || bytesHash(b) != existing.InputSHA) {
		return nil, errImplementation
	}
	if safeText(string(b), r.Token) != string(b) || writeConversationOnce(implementationFile(r, q, reply.Message, bytesHash(b)), b) != nil {
		return nil, errImplementation
	}
	result, err := r.implementationCommand(ctx, q, input, b, directionWrite)
	if err != nil {
		return nil, err
	}
	readback, err := r.implementationCheck(ctx, q, reply.Message, result.InputSHA, directionVerifyNote)
	if err != nil || readback.ReceiptSHA != result.ReceiptSHA {
		return nil, errImplementation
	}
	fresh, err := r.conversationProof(ctx, q)
	if err != nil {
		return nil, errImplementation
	}
	var current *conversationReply
	for i := range fresh.Replies {
		if fresh.Replies[i].Message == reply.Message {
			current = &fresh.Replies[i]
		}
	}
	if current == nil || canonicalHash(*current) != stamp.ReplySHA {
		return nil, errImplementation
	}
	again, ready, err := r.conversationReplySource(ctx, q, fresh, *current)
	if err != nil || !ready || again != stamp {
		return nil, errImplementation
	}
	req := r.conversationRequest(reply.Message)
	prior := req.Phase
	req.Phase = "conversation_implementation_recorded"
	if r.save() != nil {
		req.Phase = prior
		return nil, errImplementation
	}
	return map[string]any{"domain": "todo-implementation-record-v1", "kind": "implementation", "recorded": true, "changed": result.Changed, "input_sha256": result.InputSHA, "receipt_sha256": result.ReceiptSHA, "source_receipt_sha256": stamp.SourceSHA, "actor_todo_id": opts.Todo, "job_id": job, "origin_resumed": false, "question_closed_get_verified": false}, nil
}

// Historical receipts remain readable only with a real job's original identity.
// A jobs-array string can never create execution evidence by itself.
func (r *Runner) legacyImplementationJob(row *TodoRow, applied conversationCloseApplication) bool {
	root := r.JobsPath
	if root == "" {
		root = "D:/CC/Tools/codex-jobs/jobs"
	}
	var job struct {
		ID   string `json:"id"`
		Todo uint64 `json:"todo_id"`
		Cwd  string `json:"cwd"`
	}
	if !validJob(applied.Job) || readJSON(filepath.Join(root, applied.Job, "job.json"), 1<<20, &job) != nil || job.ID != applied.Job || job.Todo != row.ID || !filepath.IsAbs(job.Cwd) {
		return false
	}
	if row.Scheduler.Worktree == "" {
		return false
	}
	a, b := filepath.Clean(job.Cwd), filepath.Clean(row.Scheduler.Worktree)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
