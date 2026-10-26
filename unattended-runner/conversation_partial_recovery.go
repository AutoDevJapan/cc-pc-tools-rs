package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Recovery reads only an already posted, sender-attested question. A pending
// linking POST is neither resent nor treated as proof that the question is absent.
func (r *Runner) recoverConversationQuestion(ctx context.Context, opts conversationCLIOptions) (map[string]any, error) {
	index, err := r.loadConversations()
	if err != nil {
		return nil, err
	}
	q, ok := index.Questions[opts.RequestID]
	req := r.State.Requests[opts.Message]
	if !ok || req == nil || req.Source != "discord-api-v10" || req.Author != r.State.Destination.Owner || req.Guild != r.State.Destination.Guild || req.Conversation != "" || req.Todo == 0 || req.Key != requestKey(req.Thread, req.Message) {
		return nil, errImplementation
	}
	if q.Phase != "reserved" {
		if q.Thread != req.Thread || q.Phase == "withdrawn" || q.RecoveredIntake != req.Todo {
			return nil, errImplementation
		}
		row, err := r.conversationTask(ctx, req.Todo)
		if err != nil {
			return nil, errImplementation
		}
		if _, _, verified := r.ordinaryAssignment(q, req.Message, row); !verified {
			return nil, errImplementation
		}
		proof, err := r.conversationProof(ctx, q)
		if err != nil {
			return nil, err
		}
		if !recoveryReplyMatches(proof, req) {
			return nil, errImplementation
		}
		return map[string]any{"recovered": true, "reused": true, "phase": q.Phase, "recovered_intake_todo_id": q.RecoveredIntake, "request_id": q.RequestID, "origin_resumed": false}, nil
	}
	origin, err := r.conversationTask(ctx, q.Todo)
	if err != nil || origin.Project != q.Project {
		return nil, errImplementation
	}
	intake, err := r.conversationTask(ctx, req.Todo)
	if err != nil || intake.Project == origin.Project {
		return nil, errImplementation
	}
	q.Thread, q.Message = req.Thread, req.Thread
	raw, err := r.Run(ctx, conversationSender, "recovery-replies", "--thread", q.Thread, "--message", q.Message, "--request-id", q.SenderID, "--todo-id", fmt.Sprint(q.Todo), "--read-only")
	if err != nil {
		return nil, errImplementation
	}
	var found conversationProof
	if json.Unmarshal([]byte(raw), &found) != nil || !shaPattern.MatchString(found.Fingerprint) {
		return nil, errImplementation
	}
	body, e1 := boundedRegular(q.BodyFile)
	decision, e2 := boundedRegular(q.DecisionFile)
	originalDecision := found.OriginalDecision
	if len(originalDecision) == 0 || strings.TrimSpace(string(originalDecision)) == "null" {
		originalDecision = found.Decision
	}
	if e1 != nil || e2 != nil || string(body) != found.Body || canonicalHash(json.RawMessage(decision)) != canonicalHash(originalDecision) {
		return nil, errImplementation
	}
	q.Progress, q.Fingerprint = found.Progress, found.Fingerprint
	q.DecisionSHA, q.QuestionSHA = canonicalHash(found.Decision), bytesHash(body)
	if _, _, verified := r.ordinaryAssignment(q, req.Message, intake); !verified {
		return nil, errImplementation
	}
	if _, ok := number(q.Progress); !ok || q.Progress == q.Thread {
		return nil, errImplementation
	}
	// Reuse the ordinary full question/owner/reply/version verification. The
	// second read also prevents a stale discovery result from becoming a receipt.
	proof, err := r.conversationProof(ctx, q)
	if err != nil {
		return nil, err
	}
	if !recoveryReplyMatches(proof, req) {
		return nil, errImplementation
	}
	for _, before := range []*TodoRow{origin, intake} {
		after, err := r.conversationTask(ctx, before.ID)
		if err != nil || string(after.conversationState) != string(before.conversationState) {
			return nil, errImplementation
		}
	}
	receipt := conversationQuestionReceipt{Domain: "discord-conversation-question-v1", RequestID: q.SenderID, Todo: fmt.Sprint(q.Todo), Guild: r.State.Destination.Guild, Owner: r.State.Destination.Owner, Bot: r.State.Destination.Bot, Thread: q.Thread, Message: q.Message, Progress: q.Progress, Fingerprint: q.Fingerprint, Created: proof.Created}
	b, _ := json.MarshalIndent(receipt, "", "  ")
	q.ReceiptFile = filepath.Join(r.Config.Home, "conversations", q.RequestID, "question.json")
	q.ReceiptSHA = bytesHash(b)
	if !ordinaryRecoveryEligible(q, origin, intake) {
		return nil, errImplementation
	}
	if writeConversationOnce(q.ReceiptFile, b) != nil {
		return nil, errImplementation
	}
	q.Phase = "posted" // Do not register a new question wait over an existing hold.
	q.RecoveredIntake = req.Todo
	index.Questions[q.RequestID] = q
	if publish(r.conversationPath(), index) != nil {
		return nil, errImplementation
	}
	return map[string]any{"recovered": true, "request_id": q.RequestID, "question_thread_id": q.Thread, "recovered_intake_todo_id": q.RecoveredIntake, "origin_resumed": false, "new_question_sent": false}, nil
}

// This path repairs a standalone ordinary intake, never an origin's live or
// already consumed question wait. Keep this admission rule shared by discovery
// and linking so periodic processing cannot suppress normal answer delivery.
func ordinaryRecoveryEligible(q ConversationQuestion, origin, intake *TodoRow) bool {
	return origin != nil && intake != nil && origin.Project == q.Project && intake.Project != q.Project && intake.ID != q.Todo &&
		(q.RecoveredIntake == 0 || q.RecoveredIntake == intake.ID) &&
		(len(origin.QuestionWait) == 0 || string(origin.QuestionWait) == "null") &&
		!conversationWaitRecorded(origin, q) && !conversationQuestionAnswered(origin, q)
}

func recoveryReplyMatches(proof *conversationProof, req *Request) bool {
	for _, reply := range proof.Replies {
		if reply.Message == req.Message && reply.Body == req.Text && sameLinkTime(reply.Created, req.Created) {
			version, known := proof.ReplyVersions[reply.Message]
			edited := ""
			if version.Edited != nil {
				edited = *version.Edited
			}
			return known && edited == req.Edited
		}
	}
	return false
}

// An ordinary intake's real source is a narrowly scoped assignment, not a
// blanket cross-project permission. Both registered roots remain guarded.
func (r *Runner) ordinaryAssignment(q ConversationQuestion, message string, row *TodoRow) (string, string, bool) {
	old := r.State.Requests[message]
	if old == nil || row == nil || old.Todo != row.ID || old.Todo == q.Todo || old.Conversation != "" || (old.Phase != "registered" && old.Phase != "reported") || old.Source != "discord-api-v10" || old.RecordVersion != 2 || old.Author != r.State.Destination.Owner || old.Guild != r.State.Destination.Guild || old.Thread != q.Thread || old.Key != requestKey(q.Thread, message) || !hasNoteMarker(row.Note, "unattended-owner-receipt:"+old.Key) {
		return "", "", false
	}
	path := filepath.Join(r.Config.Home, "requests", message+".json")
	b, err := boundedRegular(path)
	var saved Request
	var sourceContext conversationProof
	if err != nil || json.Unmarshal(b, &saved) != nil || ordinaryVersion(&saved) != ordinaryVersion(old) || saved.Key != old.Key || json.Unmarshal([]byte(saved.Context), &sourceContext) != nil || sourceContext.Source != "discord-api-v10" || !sourceContext.ReadOnly || sourceContext.Owner != old.Author || sourceContext.Thread != q.Thread || sourceContext.Question != q.Message || sourceContext.RequestID != q.SenderID || canonicalHash(sourceContext.Decision) != q.DecisionSHA || bytesHash([]byte(sourceContext.Body)) != q.QuestionSHA {
		return "", "", false
	}
	if sourceContext.Fingerprint != "" && sourceContext.Fingerprint != q.Fingerprint {
		return "", "", false
	}
	for _, reply := range sourceContext.Replies {
		if reply.Message == message && reply.Todo == q.Todo && reply.Author == old.Author && reply.Thread == q.Thread && reply.Question == q.Message && reply.Body == old.Text && sameLinkTime(reply.Created, old.Created) {
			return path, bytesHash(b), true
		}
	}
	return "", "", false
}

func ordinarySourceAssigned(body, path, message, thread string) bool {
	normalize := func(s string) string { return strings.ToLower(strings.ReplaceAll(s, `\`, "/")) }
	// The full source path must be a token, not a prefix of a backup filename.
	pattern := "(^|[\\s\"'`(])" + regexp.QuoteMeta(normalize(path)) + "($|[\\s\"'`),;。])"
	idToken := func(id string) bool {
		return regexp.MustCompile(`(^|[^0-9])` + regexp.QuoteMeta(id) + `($|[^0-9])`).MatchString(strings.ReplaceAll(normalize(body), normalize(path), ""))
	}
	return regexp.MustCompile(pattern).MatchString(normalize(body)) && idToken(message) && idToken(thread)
}
