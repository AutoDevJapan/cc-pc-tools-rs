package main

// A recipient binding does not alter the sender's original question identity.
// This finite contract is the already verified 3638/3650 -> 3277 -> 1171 chain.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type conversationActor struct {
	Todo          uint64 `json:"todo_id"`
	Job           string `json:"job_id"`
	OriginSpecSHA string `json:"origin_spec_sha256"`
	SpecSHA       string `json:"spec_sha256"`
	RootSHA       string `json:"registered_path_sha256"`
	SourceSHA     string `json:"succession_sha256"`
}

func conversationRecipient(q ConversationQuestion) uint64 {
	if q.Actor != nil {
		return q.Actor.Todo
	}
	return q.Todo
}

func conversationOrigin(q ConversationQuestion) uint64 {
	if q.originTodo != 0 {
		return q.originTodo
	}
	return q.Todo
}

func sameConversationActor(a, b *conversationActor) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func conversationPathSHA(path string) string {
	return bytesHash([]byte(strings.ToLower(strings.TrimRight(strings.ReplaceAll(path, `\`, "/"), "/"))))
}

func conversationActorHeld(raw json.RawMessage) bool {
	var row map[string]any
	if json.Unmarshal(raw, &row) != nil {
		return true
	}
	for _, key := range []string{"stopped", "cancel_requested", "owner_stopped", "paused", "owner_postponement_recorded", "stop_kind", "decision_wait", "wait_ready", "resume_condition", "external_resume_condition"} {
		if v := row[key]; v != nil && v != false && v != "" {
			return true
		}
	}
	if scheduler, ok := row["scheduler"].(map[string]any); ok {
		for _, key := range []string{"owner_stop_job", "pending_cancel", "question_close_pending"} {
			if v := scheduler[key]; v != nil && v != false && v != "" {
				return true
			}
		}
	}
	return false
}

func (r *Runner) conversationActorForAsk(ctx context.Context, input ConversationAskInput, origin *TodoRow) (*conversationActor, error) {
	if input.ActorTodo == 0 && input.ActorJob == "" {
		return nil, nil
	}
	if input.ActorTodo == 0 || !validJob(input.ActorJob) {
		return nil, errors.New("conversation actor unverified")
	}
	if input.ActorTodo == input.Todo {
		return nil, nil // Existing callers may explicitly identify the original worker.
	}
	index, err := r.loadConversations()
	if err != nil {
		return nil, err
	}
	var saved *conversationActor
	if q, exists := index.Questions[input.RequestID]; exists {
		saved = q.Actor
	}
	if saved != nil && saved.Job != input.ActorJob {
		current, err := r.verifyConversationActor(ctx, input.Todo, input.ActorTodo, input.ActorJob, origin, nil)
		if err != nil {
			return nil, err
		}
		current.Job = saved.Job
		if !sameConversationActor(current, saved) {
			return nil, errors.New("conversation actor differs")
		}
		return r.verifyConversationActor(ctx, input.Todo, input.ActorTodo, saved.Job, origin, saved)
	}
	return r.verifyConversationActor(ctx, input.Todo, input.ActorTodo, input.ActorJob, origin, saved)
}

func (r *Runner) verifyConversationActor(ctx context.Context, originID, actorID uint64, jobID string, origin *TodoRow, saved *conversationActor) (*conversationActor, error) {
	fail := errors.New("conversation actor unverified")
	if originID != 1171 || actorID != 3277 || origin.ID != originID || origin.Project != "GitDo" || origin.Status == "done" || conversationActorHeld(origin.conversationState) || !validJob(jobID) {
		return nil, fail
	}
	actor, err := r.conversationTask(ctx, actorID)
	if err != nil || actor.Project != origin.Project || actor.conversationRoot != origin.conversationRoot || conversationActorHeld(actor.conversationState) {
		return nil, fail
	}
	if actor.Status != "doing" && actor.Status != "todo" && actor.Status != "wait" && !(actor.Status == "block" && len(actor.QuestionWait) != 0 && string(actor.QuestionWait) != "null" && saved != nil) {
		return nil, fail
	}
	// The original job remains a provenance anchor after normal scheduler resume.
	// Only an already stored binding may use that historical job on a retry.
	known := false
	for _, raw := range actor.Jobs {
		var id string
		if json.Unmarshal(raw, &id) == nil && id == jobID {
			known = true
		}
	}
	if !known || (actor.Scheduler.Job != jobID && (saved == nil || saved.Job != jobID || saved.Todo != actorID)) {
		return nil, fail
	}
	jobs := r.JobsPath
	if jobs == "" {
		jobs = "D:/CC/Tools/codex-jobs/jobs"
	}
	var job struct {
		ID      string `json:"id"`
		Todo    uint64 `json:"todo_id"`
		Cwd     string `json:"cwd"`
		Status  string `json:"status"`
		Stopped bool   `json:"stopped"`
		Cancel  bool   `json:"cancel_requested"`
		Reason  string `json:"cancel_reason"`
	}
	if readJSON(filepath.Join(jobs, jobID, "job.json"), 1<<20, &job) != nil || job.ID != jobID || job.Todo != actorID || !filepath.IsAbs(job.Cwd) || actor.Scheduler.Worktree == "" || conversationPathSHA(job.Cwd) != conversationPathSHA(actor.Scheduler.Worktree) || job.Stopped || job.Cancel || job.Reason == "manual" || job.Reason == "person" || job.Status == "cancelled" || job.Status == "canceled" {
		return nil, fail
	}
	raw, err := r.Run(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--verify-current-succession", fmt.Sprint(actorID))
	var proof struct {
		Domain      string `json:"domain"`
		Verified    bool   `json:"verified"`
		Origin      uint64 `json:"origin_todo_id"`
		Actor       uint64 `json:"actor_todo_id"`
		Todo        uint64 `json:"todo_id"`
		Project     string `json:"project"`
		RootSHA     string `json:"registered_path_sha256"`
		SpecPathSHA string `json:"spec_path_sha256"`
		SpecSHA     string `json:"spec_sha256"`
		Registry    *bool  `json:"original_registry_recovered"`
		Kind        string `json:"source_kind"`
		Session     string `json:"source_session"`
		EventSHA    string `json:"source_event_sha256"`
		At          string `json:"source_at"`
	}
	var actorSpec, originSpec string
	if err != nil || json.Unmarshal([]byte(raw), &proof) != nil || proof.Domain != "current-product-succession-v1" || !proof.Verified || proof.Origin != 3638 || proof.Actor != 3650 || proof.Todo != actorID || proof.Project != actor.Project || proof.Registry == nil || *proof.Registry || proof.RootSHA != conversationPathSHA(actor.conversationRoot) || proof.Kind != "native_user" || proof.Session != "01a16c0c-18a9-7570-ab4e-9522ffa8d6d0" || proof.EventSHA != "e0cff4ef85745ffb93ddcc36d1fb9626c4c098967406d8875a672d58c1684465" || proof.At != "2026-10-24T11:26:20.672Z" || json.Unmarshal(actor.Spec, &actorSpec) != nil || json.Unmarshal(origin.Spec, &originSpec) != nil || !filepath.IsAbs(actorSpec) || !filepath.IsAbs(originSpec) || proof.SpecPathSHA != conversationPathSHA(actorSpec) {
		return nil, fail
	}
	// No spec is opened before the official finite verifier succeeds.
	a, err := boundedRegular(actorSpec)
	if err != nil || bytesHash(a) != proof.SpecSHA {
		return nil, fail
	}
	o, err := boundedRegular(originSpec)
	if err != nil {
		return nil, fail
	}
	bound := &conversationActor{Todo: actorID, Job: jobID, OriginSpecSHA: bytesHash(o), SpecSHA: bytesHash(a), RootSHA: proof.RootSHA, SourceSHA: canonicalHash(proof)}
	if saved != nil && !sameConversationActor(bound, saved) {
		return nil, fail
	}
	return bound, nil
}

func (r *Runner) conversationActorRow(ctx context.Context, q ConversationQuestion) (*TodoRow, error) {
	origin, err := r.conversationTask(ctx, q.Todo)
	if err != nil {
		return nil, err
	}
	if q.Actor == nil {
		return nil, errors.New("conversation actor unverified")
	}
	before, err := r.conversationTask(ctx, q.Actor.Todo)
	if err != nil {
		return nil, err
	}
	if _, err = r.verifyConversationActor(ctx, q.Todo, q.Actor.Todo, q.Actor.Job, origin, q.Actor); err != nil {
		return nil, err
	}
	row, err := r.conversationTask(ctx, q.Actor.Todo)
	if err != nil || canonicalHash(json.RawMessage(row.conversationState)) != canonicalHash(json.RawMessage(before.conversationState)) {
		return nil, errors.New("conversation actor unverified")
	}
	row.conversationOriginState, row.conversationActorBinding = origin.conversationState, q.Actor
	return row, nil
}

func (r *Runner) conversationDeliveryTask(ctx context.Context, q ConversationQuestion) (*TodoRow, error) {
	if q.deliveryActor != nil {
		q.Todo, q.Actor = q.originTodo, q.deliveryActor
		return r.conversationActorRow(ctx, q)
	}
	return r.conversationTask(ctx, q.Todo)
}

// This is a local recipient envelope, not a replacement of the API's origin
// receipt. The immutable q.Actor binds it to the retained original envelope.
func (r *Runner) conversationActorQuestion(q ConversationQuestion) (ConversationQuestion, error) {
	b, err := boundedRegular(q.ReceiptFile)
	var receipt conversationQuestionReceipt
	if err != nil || bytesHash(b) != q.ReceiptSHA || json.Unmarshal(b, &receipt) != nil || receipt.Todo != fmt.Sprint(q.Todo) || receipt.RequestID != q.SenderID || receipt.Thread != q.Thread || receipt.Fingerprint != q.Fingerprint {
		return q, errors.New("conversation actor receipt differs")
	}
	receipt.Todo = fmt.Sprint(q.Actor.Todo)
	b, _ = json.MarshalIndent(receipt, "", "  ")
	q.originTodo = q.Todo
	q.Todo = q.Actor.Todo
	q.deliveryActor = q.Actor
	q.Actor = nil
	q.ReceiptFile = filepath.Join(r.Config.Home, "conversations", q.RequestID, "actor-question.json")
	q.ReceiptSHA = bytesHash(b)
	return q, writeConversationOnce(q.ReceiptFile, b)
}

func (r *Runner) conversationActorWait(ctx context.Context, index *conversationIndex, q ConversationQuestion) (ConversationQuestion, error) {
	row, err := r.conversationActorRow(ctx, q)
	if err != nil {
		return q, err
	}
	var held struct {
		WorkOwnerWaitOn []uint64 `json:"work_owner_wait_on"`
	}
	if json.Unmarshal(row.conversationState, &held) != nil {
		return q, errors.New("conversation actor unverified")
	}
	pending := row.Status == "wait"
	for _, id := range append(append([]uint64{}, row.WaitOn...), held.WorkOwnerWaitOn...) {
		dependency, err := r.todo(id)
		if err != nil || id == row.ID {
			return q, errors.New("conversation actor unverified")
		}
		pending = pending || dependency.Status != "done"
	}
	if pending {
		q.WaitDeferred = true
		index.Questions[q.RequestID] = q
		return q, publish(r.conversationPath(), index)
	}
	delivery, err := r.conversationActorQuestion(q)
	if err != nil {
		return q, err
	}
	scope, err := r.conversationScopeFile(row, delivery)
	if err != nil {
		return q, err
	}
	if _, err = r.Run(ctx, conversationTodo, "question-wait", fmt.Sprint(delivery.Todo), "--question-file", delivery.ReceiptFile, "--scope-file", scope); err != nil {
		return q, errors.New("conversation posted; task wait recording pending")
	}
	row, err = r.todo(delivery.Todo)
	if err != nil || !conversationWaitUnchanged(row, delivery) {
		return q, errors.New("conversation task wait verification pending")
	}
	q.Phase, q.WaitDeferred = "waiting", false
	index.Questions[q.RequestID] = q
	return q, publish(r.conversationPath(), index)
}

func (r *Runner) deliverConversationActorReply(ctx context.Context, q ConversationQuestion, reply conversationReply, req *Request) error {
	row, err := r.conversationActorRow(ctx, q)
	if err != nil {
		return err
	}
	delivery, err := r.conversationActorQuestion(q)
	if err != nil {
		return err
	}
	if conversationWaitUnchanged(row, delivery) || conversationQuestionAnswered(row, delivery) {
		// Keep the original question identity in the instructions and source
		// while the canonical wait transaction uses the recipient envelope.
		return r.deliverConversationReply(ctx, delivery, reply, req)
	}
	// An unrelated owner/question hold must never be overwritten. A dependency
	// wait is metadata-only delivery; its status, jobs and dependencies survive.
	if row.Status != "todo" && row.Status != "doing" && row.Status != "wait" {
		return errors.New("conversation actor held")
	}
	marker := conversationReplyMarker(q, req)
	if !hasNoteMarker(row.Note, marker) {
		path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".json")
		b, err := boundedRegular(path)
		if err != nil {
			return err
		}
		note := row.Note + "\n\n" + r.conversationReplyInstructions(q, reply, req, b, marker)
		if len(note) > 1<<20 {
			return errors.New("conversation delivery note limit exceeded")
		}
		notePath := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".note.txt")
		if err = writeText(notePath, note); err != nil {
			return err
		}
		scope, err := r.conversationScopeFile(row, delivery)
		if err != nil {
			return err
		}
		expected, err := r.conversationSnapshotFile(row, delivery, reply.Message)
		if err != nil {
			return err
		}
		if _, err = r.Run(ctx, conversationTodo, "edit", fmt.Sprint(delivery.Todo), "--note-file", notePath, "--expected-note-sha256", bytesHash([]byte(row.Note)), "--scope-file", scope, "--expected-state-file", expected); err != nil {
			return errors.New("conversation normal task delivery deferred")
		}
		row, err = r.todo(delivery.Todo)
		if err != nil || !hasNoteMarker(row.Note, marker) || !hasNoteMarker(row.Scheduler.OwnerInstruction, marker) {
			return errors.New("conversation task delivery readback deferred")
		}
	}
	if !hasNoteMarker(row.Scheduler.OwnerInstruction, marker) {
		return errors.New("conversation task prompt delivery unverified")
	}
	req.Todo, req.Phase = delivery.Todo, "conversation_reply_delivered"
	return r.save()
}

// Only fixed stages are public. Never echo arbitrary lower-level diagnostics.
func conversationFailureStage(err error) string {
	if err == nil {
		return "deadline"
	}
	switch err.Error() {
	case "conversation actor unverified", "conversation actor differs", "conversation actor receipt differs", "conversation actor held":
		return "actor_scope"
	case "conversation task scope unavailable", "conversation task scope unverified", "conversation original project differs":
		return "task_scope"
	case "conversation original task unavailable", "conversation task is already held", "conversation reserved task is held":
		return "original_task"
	case "conversation retry input differs", "conversation alias retry input differs", "conversation decision task differs":
		return "input_mismatch"
	case "conversation send pending; retry the same request":
		return "send_pending"
	case "conversation send verification pending", "conversation reply verification deferred":
		return "verification_pending"
	case "conversation posted; task wait recording pending", "conversation task wait verification pending":
		return "wait_pending"
	default:
		return "operation_pending"
	}
}
