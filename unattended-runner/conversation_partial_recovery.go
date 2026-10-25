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
		if q.Thread != req.Thread {
			return nil, errImplementation
		}
		if _, err := r.conversationProof(ctx, q); err != nil {
			return nil, err
		}
		return map[string]any{"recovered": true, "reused": true, "request_id": q.RequestID, "origin_resumed": false}, nil
	}
	origin, err := r.conversationTask(ctx, q.Todo)
	if err != nil || origin.Project != q.Project {
		return nil, errImplementation
	}
	intake, err := r.conversationTask(ctx, req.Todo)
	if err != nil {
		return nil, errImplementation
	}
	q.Thread, q.Message = req.Thread, req.Thread
	raw, err := r.Run(ctx, conversationSender, "recovery-replies", "--thread", q.Thread, "--message", q.Message, "--request-id", q.SenderID, "--todo-id", fmt.Sprint(q.Todo), "--read-only")
	if err != nil {
		return nil, errImplementation
	}
	var found struct {
		conversationProof
		Fingerprint string `json:"question_fingerprint"`
	}
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
	if _, ok := number(q.Progress); !ok || q.Progress == q.Thread {
		return nil, errImplementation
	}
	// Reuse the ordinary full question/owner/reply/version verification. The
	// second read also prevents a stale discovery result from becoming a receipt.
	proof, err := r.conversationProof(ctx, q)
	if err != nil {
		return nil, err
	}
	matched := false
	for _, reply := range proof.Replies {
		if reply.Message == req.Message && reply.Body == req.Text && sameLinkTime(reply.Created, req.Created) {
			version, known := proof.ReplyVersions[reply.Message]
			edited := ""
			if version.Edited != nil {
				edited = *version.Edited
			}
			matched = known && edited == req.Edited
		}
	}
	if !matched {
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
	if writeConversationOnce(q.ReceiptFile, b) != nil {
		return nil, errImplementation
	}
	q.Phase = "posted" // Do not register a new question wait over an existing hold.
	index.Questions[q.RequestID] = q
	if publish(r.conversationPath(), index) != nil {
		return nil, errImplementation
	}
	return map[string]any{"recovered": true, "request_id": q.RequestID, "question_thread_id": q.Thread, "origin_resumed": false, "new_question_sent": false}, nil
}

// An ordinary intake's real source is a narrowly scoped assignment, not a
// blanket cross-project permission. Both registered roots remain guarded.
func (r *Runner) ordinaryAssignment(q ConversationQuestion, message string, row *TodoRow) (string, string, bool) {
	old := r.State.Requests[message]
	if old == nil || row == nil || old.Todo != row.ID || old.Todo == q.Todo || old.Conversation != "" || (old.Phase != "registered" && old.Phase != "reported") || old.Source != "discord-api-v10" || old.RecordVersion != 2 || old.Author != r.State.Destination.Owner || old.Guild != r.State.Destination.Guild || old.Thread != q.Thread || old.Key != requestKey(q.Thread, message) || !strings.Contains("\n"+row.Note+"\n", "\nunattended-owner-receipt:"+old.Key+"\n") {
		return "", "", false
	}
	path := filepath.Join(r.Config.Home, "requests", message+".json")
	b, err := boundedRegular(path)
	var saved Request
	var context conversationProof
	if err != nil || json.Unmarshal(b, &saved) != nil || ordinaryVersion(&saved) != ordinaryVersion(old) || saved.Key != old.Key || json.Unmarshal([]byte(saved.Context), &context) != nil || context.Source != "discord-api-v10" || !context.ReadOnly || context.Owner != old.Author || context.Thread != q.Thread || context.Question != q.Message || context.RequestID != q.SenderID || canonicalHash(context.Decision) != q.DecisionSHA || bytesHash([]byte(context.Body)) != q.QuestionSHA {
		return "", "", false
	}
	for _, reply := range context.Replies {
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
	return regexp.MustCompile(pattern).MatchString(normalize(body)) && strings.Contains(body, message) && strings.Contains(body, thread)
}
