package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Fresh material validation authorizes one fixed send. The signed admission
// then binds its verified sender receipt; later answers do not reopen that
// material or extend the grant to another question.
type taskQuestionAdmission struct {
	Domain       string               `json:"domain"`
	Registration taskQuestionReceipt  `json:"registration"`
	Evidence     taskQuestionEvidence `json:"evidence"`
	Validated    time.Time            `json:"validated_at"`
	Question     string               `json:"question_sha256,omitempty"`
}

func taskQuestionAdmissionName(request string, posted bool) string {
	kind := "-send.json"
	if posted {
		kind = "-admitted.json"
	}
	return bytesHash([]byte(request)) + kind
}

func taskQuestionAdmittedIdentity(q ConversationQuestion) string {
	q.Phase, q.ReplyCursor = "", ""
	return canonicalHash(q)
}

func (r taskQuestionRegistry) stageAdmission(e taskQuestionEvidence) error {
	var registration taskQuestionReceipt
	if r.readSigned(taskQuestionName(e.Request), &registration) != nil {
		return errTaskQuestion
	}
	a := taskQuestionAdmission{Domain: "owner-task-question-admission-v1", Registration: registration, Evidence: e, Validated: r.Now()}
	var old taskQuestionAdmission
	name := taskQuestionAdmissionName(e.Request, false)
	if r.readSigned(name, &old) == nil {
		a.Validated = old.Validated
		if canonicalHash(a) != canonicalHash(old) {
			return errTaskQuestion
		}
	}
	return r.writeSigned(name, a)
}

func (r *Runner) taskQuestionRegistry() taskQuestionRegistry {
	if r.TaskQuestion != nil {
		return *r.TaskQuestion
	}
	return taskQuestionProduction()
}

func (r *Runner) recordTaskQuestionAdmission(ctx context.Context, q ConversationQuestion, proof *conversationProof) error {
	registry := r.taskQuestionRegistry()
	var a taskQuestionAdmission
	var registration taskQuestionReceipt
	if registry.readSigned(taskQuestionAdmissionName(q.RequestID, false), &a) != nil ||
		registry.readSigned(taskQuestionName(q.RequestID), &registration) != nil || a.Registration != registration ||
		a.Domain != "owner-task-question-admission-v1" || a.Question != "" || a.Validated.Before(a.Evidence.Created) || !a.Validated.Before(a.Evidence.Expires) {
		return errTaskQuestion
	}
	// This runs only after the ordinary sender verified the exact image hashes
	// and its read-only API proof. Never mint an admission from a retained index.
	created, err := time.Parse(time.RFC3339Nano, proof.Created)
	if err != nil || created.Before(a.Evidence.Created) || !created.Before(a.Evidence.Expires) || created.After(r.Now()) ||
		q.RequestID != a.Evidence.Request || q.Todo != a.Evidence.Scope.Todo || q.QuestionSHA != a.Evidence.Scope.Body ||
		len(q.AttachmentHashes) != 1 || q.Wait != a.Evidence.Wait || q.Actor != nil || q.RecoveredIntake != 0 ||
		canonicalHash(proof.OriginalDecision) != a.Evidence.Scope.Decision || taskQuestionDecisionImage(proof.OriginalDecision) != q.AttachmentHashes[0] ||
		q.OriginThread != a.Evidence.Origin || q.Title != a.Evidence.Title || !registry.Dst(a.Evidence.Scope) || ctx.Err() != nil {
		return errTaskQuestion
	}
	a.Question = taskQuestionAdmittedIdentity(q)
	return registry.writeSigned(taskQuestionAdmissionName(q.RequestID, true), a)
}

func (r taskQuestionRegistry) admitted(ctx context.Context, input ConversationAskInput, registration taskQuestionReceipt, state taskQuestionState) (taskQuestionEvidence, error) {
	zero := taskQuestionEvidence{}
	q := input.heldQuestion
	var a taskQuestionAdmission
	if q == nil || r.readSigned(taskQuestionAdmissionName(input.RequestID, true), &a) != nil ||
		a.Domain != "owner-task-question-admission-v1" || a.Registration != registration || a.Question != taskQuestionAdmittedIdentity(*q) ||
		a.Validated.Before(a.Evidence.Created) || !a.Validated.Before(a.Evidence.Expires) || a.Validated.After(r.Now()) {
		return zero, errTaskQuestion
	}
	e := a.Evidence
	// Retain revocation and original-source checks. Only fresh-material/clock
	// validation is replaced by the immutable, already admitted question.
	b, err := r.guarded(ctx, registration.Evidence, 128<<10)
	if err != nil || bytesHash(b) != registration.SHA || !r.Dst(e.Scope) || !r.Repo(ctx, e.Repository) || !r.Root(ctx, e.Root) {
		return zero, errTaskQuestion
	}
	current, err := r.Task(ctx, e.Scope.Todo)
	if err != nil || canonicalHash(current) != canonicalHash(state) {
		return zero, errTaskQuestion
	}
	bound, err := r.questionState(e, q, &registration, current)
	if err != nil || canonicalHash(bound) != registration.State ||
		(!bound.eligible(q, e.Scope.Owner) && !(q.Phase == "posted" && bound.eligible(nil, e.Scope.Owner))) || taskQuestionNative(e, bound, r.Records) != nil {
		return zero, errTaskQuestion
	}
	latest, err := r.Task(ctx, e.Scope.Todo)
	if err != nil || canonicalHash(latest) != canonicalHash(current) || ctx.Err() != nil {
		return zero, errTaskQuestion
	}
	return e, nil
}

// Keep the complete retained decision binding separate from sender identity.
func taskQuestionInputMatches(input ConversationAskInput, e taskQuestionEvidence) bool {
	return e.Request == input.RequestID && e.Scope.Todo == input.Todo && e.Origin == input.OriginThread && e.Title == input.Title && e.Wait == input.Wait && input.ActorTodo == 0 && input.ActorJob == ""
}

func (r *Runner) taskQuestionSendBefore(input ConversationAskInput) (string, error) {
	registry := r.taskQuestionRegistry()
	var staged taskQuestionAdmission
	var registration taskQuestionReceipt
	if registry.readSigned(taskQuestionAdmissionName(input.RequestID, false), &staged) != nil || registry.readSigned(taskQuestionName(input.RequestID), &registration) != nil ||
		staged.Registration != registration || !taskQuestionInputMatches(input, staged.Evidence) || !registry.Now().Before(staged.Evidence.Expires) {
		return "", errTaskQuestion
	}
	return staged.Evidence.Expires.UTC().Format(time.RFC3339Nano), nil
}

// The admission is the durable authority; index publication is a recoverable
// projection. Never call the sender to repair this already posted question.
func (r *Runner) recoverTaskQuestionIndex(ctx context.Context, input ConversationAskInput) error {
	if !conversationMediaNormal.MatchString(input.RequestID) {
		return nil
	}
	index, err := r.loadConversations()
	if err != nil {
		return errTaskQuestion
	}
	reserved, ok := index.Questions[input.RequestID]
	if !ok || reserved.Phase != "reserved" {
		return nil
	}
	registry := r.taskQuestionRegistry()
	if _, err := os.Lstat(filepath.Join(filepath.FromSlash(registry.Keys.Home), taskQuestionAdmissionName(input.RequestID, true))); os.IsNotExist(err) {
		return nil
	}
	var admission taskQuestionAdmission
	if registry.readSigned(taskQuestionAdmissionName(input.RequestID, true), &admission) != nil || admission.Domain != "owner-task-question-admission-v1" || !shaPattern.MatchString(admission.Question) || !taskQuestionInputMatches(input, admission.Evidence) ||
		reserved.Actor != nil || reserved.RecoveredIntake != 0 || !reserved.Wait || reserved.SenderID != "conversation-v1-"+bytesHash([]byte(reserved.RequestID))[:32] {
		return errTaskQuestion
	}
	if reserved.Thread != "" || reserved.Message != "" || reserved.Progress != "" || reserved.Fingerprint != "" || reserved.QuestionSHA != "" || reserved.DecisionSHA != "" || reserved.ReceiptSHA != "" || reserved.ReceiptFile != "" {
		return errTaskQuestion
	}
	if _, _, err := conversationQuestionRetryImages(index, input); err != nil {
		return errTaskQuestion
	}
	dir := filepath.Join(r.Config.Home, "conversations", reserved.RequestID)
	if reserved.BodyFile != filepath.Join(dir, "question.txt") || reserved.DecisionFile != filepath.Join(dir, "decision.json") {
		return errTaskQuestion
	}
	q := reserved
	q.ReceiptFile = filepath.Join(dir, "question.json")
	b, err := registry.Read(q.ReceiptFile, 128<<10)
	var receipt conversationQuestionReceipt
	if err != nil || historyStrictJSON(b, &receipt) != nil {
		return errTaskQuestion
	}
	q.ReceiptSHA = bytesHash(b)
	q.Thread, q.Message, q.Progress, q.Fingerprint = receipt.Thread, receipt.Message, receipt.Progress, receipt.Fingerprint
	want := conversationQuestionReceipt{Domain: "discord-conversation-question-v1", RequestID: q.SenderID, Todo: fmt.Sprint(q.Todo), Guild: r.State.Destination.Guild, Owner: r.State.Destination.Owner, Bot: r.State.Destination.Bot, Thread: q.Thread, Message: q.Message, Progress: q.Progress, Fingerprint: q.Fingerprint, Created: receipt.Created}
	if receipt != want || q.Thread != q.Message || !shaPattern.MatchString(q.Fingerprint) {
		return errTaskQuestion
	}
	for _, id := range []string{q.Thread, q.Progress} {
		if _, ok := number(id); !ok {
			return errTaskQuestion
		}
	}
	// Discovery is read-only and request-bound by the existing sender. The full
	// proof is checked again after matching the immutable signed question hash.
	raw, err := r.Run(ctx, conversationSender, "recovery-replies", "--thread", q.Thread, "--message", q.Message, "--request-id", q.SenderID, "--todo-id", fmt.Sprint(q.Todo), "--read-only")
	var found conversationProof
	if err != nil || historyStrictJSON([]byte(raw), &found) != nil {
		return errTaskQuestion
	}
	q.QuestionSHA, q.DecisionSHA = bytesHash([]byte(found.Body)), canonicalHash(found.Decision)
	q.Phase = "posted"
	if admission.Question != taskQuestionAdmittedIdentity(q) {
		return errTaskQuestion
	}
	if _, _, err = r.taskQuestionRetainedProof(ctx, q); err != nil {
		return errTaskQuestion
	}
	state, err := registry.Task(ctx, q.Todo)
	if err != nil {
		return errTaskQuestion
	}
	checked := input
	checked.heldQuestion = &q
	if err = r.verifyTaskQuestion(ctx, checked, state); err != nil {
		return errTaskQuestion
	}
	latest, err := r.loadConversations()
	if err != nil || canonicalHash(latest.Questions[q.RequestID]) != canonicalHash(reserved) {
		return errTaskQuestion
	}
	latest.Questions[q.RequestID] = q
	return publish(r.conversationPath(), latest)
}
