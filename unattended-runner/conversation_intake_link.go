package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Ordinary intake and canonical conversation delivery have different owners.
// Only source-link may establish the second capture; ordinary processing keeps
// using State.Requests and therefore cannot steal or complete the origin task.
func (r *Runner) conversationRequest(id string) *Request {
	if req := r.State.ConversationCaptures[id]; req != nil {
		return req // A different conversation is a conflict, never a fallback.
	}
	return r.State.Requests[id]
}

func (r *Runner) conversationRequests() map[string]*Request {
	all := make(map[string]*Request, len(r.State.Requests)+len(r.State.ConversationCaptures))
	for id, req := range r.State.Requests {
		all[id] = req
	}
	for id, req := range r.State.ConversationCaptures {
		all[id] = req
	}
	return all
}

type conversationIntakeLinkReceipt struct {
	Domain      string `json:"domain"`
	Request     string `json:"request_id"`
	Origin      uint64 `json:"origin_todo_id"`
	Intake      uint64 `json:"intake_todo_id"`
	Actor       uint64 `json:"actor_todo_id"`
	Job         string `json:"job_id"`
	Revision    string `json:"revision"`
	Message     string `json:"message_id"`
	QuestionSHA string `json:"question_receipt_sha256"`
	RawSHA      string `json:"original_source_sha256"`
	OrdinarySHA string `json:"ordinary_source_sha256"`
	IntakeSHA   string `json:"intake_snapshot_sha256"`
	CaptureSHA  string `json:"capture_sha256"`
}

func linkStopped(raw json.RawMessage) bool {
	var row map[string]json.RawMessage
	if json.Unmarshal(raw, &row) != nil {
		return true
	}
	for _, key := range []string{"owner_stopped", "paused", "stopped", "cancel_requested", "owner_postponement_recorded"} {
		if v := row[key]; len(v) != 0 && string(v) != "null" && string(v) != "false" {
			return true
		}
	}
	if v := row["stop_kind"]; len(v) != 0 && string(v) != "null" && string(v) != `""` {
		return true
	}
	return false
}

func sameLinkTime(a, b string) bool {
	x, e1 := time.Parse(time.RFC3339Nano, a)
	y, e2 := time.Parse(time.RFC3339Nano, b)
	return e1 == nil && e2 == nil && x.Equal(y)
}

func linkAssigned(body string, q ConversationQuestion, reply string) bool {
	for _, id := range []string{fmt.Sprint(q.Todo), q.RequestID, q.Message, reply} {
		// Decimal IDs cannot match inside another decimal ID. Request names
		// cannot match inside a different request identifier either.
		boundary := `[^0-9]`
		if id == q.RequestID {
			boundary = `[^A-Za-z0-9_-]`
		}
		if !regexp.MustCompile(`(^|` + boundary + `)` + regexp.QuoteMeta(id) + `($|` + boundary + `)`).MatchString(body) {
			return false
		}
	}
	return true
}

func (r *Runner) linkActor(row *TodoRow, q ConversationQuestion, opts conversationCLIOptions) bool {
	jobID := os.Getenv("CC_JOB_ID")
	if !validJob(jobID) || row.Status != "doing" || (row.Scheduler.Job != jobID && row.Worker.Job != jobID) || linkStopped(row.conversationState) {
		return false
	}
	root := r.JobsPath
	if root == "" {
		root = "D:/CC/Tools/codex-jobs/jobs"
	}
	var job struct {
		ID        string `json:"id"`
		Todo      uint64 `json:"todo_id"`
		Status    string `json:"status"`
		Cwd       string `json:"cwd"`
		Cancelled bool   `json:"cancel_requested"`
	}
	if readJSON(filepath.Join(root, jobID, "job.json"), 1<<20, &job) != nil || job.ID != jobID || job.Todo != row.ID || job.Status != "running" || job.Cancelled || !filepath.IsAbs(job.Cwd) || !strings.EqualFold(filepath.Clean(job.Cwd), filepath.Clean(row.Scheduler.Worktree)) {
		return false
	}
	owned := false
	for _, raw := range row.Jobs {
		var id string
		if json.Unmarshal(raw, &id) == nil && id == jobID {
			owned = true
		}
	}
	var spec string
	if !owned || json.Unmarshal(row.Spec, &spec) != nil || !filepath.IsAbs(spec) {
		return false
	}
	b, err := boundedRegular(spec)
	prompt, e2 := boundedRegular(filepath.Join(root, jobID, "prompt.md"))
	if err != nil || e2 != nil {
		return false
	}
	if linkAssigned(string(b), q, opts.Message) && linkAssigned(string(prompt), q, opts.Message) {
		return true
	}
	path, _, verified := r.ordinaryAssignment(q, opts.Message, row)
	return verified && opts.IntakeTodo == row.ID && ordinarySourceAssigned(string(b), path, opts.Message, q.Thread) && ordinarySourceAssigned(string(prompt), path, opts.Message, q.Thread)
}

func (r *Runner) conversationIntakeLink(ctx context.Context, opts conversationCLIOptions, q ConversationQuestion, proof *conversationProof, reply conversationReply) (map[string]any, error) {
	old := r.State.Requests[reply.Message]
	if old == nil || old.Conversation != "" || old.Todo != opts.IntakeTodo || old.Todo == q.Todo || opts.Todo == q.Todo || old.Phase != "registered" || old.RecordVersion != 2 || old.Source != "discord-api-v10" ||
		old.Thread != q.Thread || old.Guild != r.State.Destination.Guild || old.Message != reply.Message || old.Author != r.State.Destination.Owner || old.Author != reply.Author || old.Text != reply.Body || !sameLinkTime(old.Created, reply.Created) || old.Key != requestKey(q.Thread, reply.Message) {
		return nil, errImplementation
	}
	original, err := boundedRegular(filepath.Join(r.Config.Home, "requests", reply.Message+".json"))
	var saved Request
	if err != nil || bytesHash(original) != opts.SourceSHA || json.Unmarshal(original, &saved) != nil || saved.Conversation != "" || saved.Source != old.Source || saved.RecordVersion != 2 || ordinaryVersion(&saved) != ordinaryVersion(old) {
		return nil, errImplementation
	}
	rows := make([]*TodoRow, 0, 3)
	for _, id := range []uint64{q.Todo, opts.IntakeTodo, opts.Todo} {
		row, err := r.conversationTask(ctx, id)
		if err != nil || (id == q.Todo && row.Project != q.Project) || linkStopped(row.conversationState) || row.Status == "done" || row.Status == "recovery_unverified" {
			return nil, errImplementation
		}
		rows = append(rows, row)
	}
	if !r.linkActor(rows[2], q, opts) {
		return nil, errImplementation
	}
	crossProject := rows[1].Project != q.Project || rows[2].Project != q.Project
	if crossProject {
		if _, _, ok := r.ordinaryAssignment(q, opts.Message, rows[2]); !ok || opts.IntakeTodo != opts.Todo {
			return nil, errImplementation
		}
	}
	var live struct {
		Message
		Type        *int                     `json:"type"`
		Attachments *[]historyReadAttachment `json:"attachments"`
	}
	if r.get(ctx, "/channels/"+q.Thread+"/messages/"+reply.Message, &live) != nil || live.ID != old.Message || live.Channel != old.Thread || live.Author.ID != old.Author || live.Author.Bot || !sameLinkTime(live.Timestamp, old.Created) || live.Content != old.Text || live.Edited != old.Edited || live.Reference.Message != old.ReplyTo || live.Reference.Channel != old.ReplyChannel || live.Reference.Guild != old.ReplyGuild {
		return nil, errImplementation
	}
	if old.MessageType != nil && canonicalHash(old.MessageType) != canonicalHash(live.Type) || old.Attachments != nil && canonicalHash(old.Attachments) != canonicalHash(live.Attachments) {
		return nil, errImplementation
	}
	// Recheck all three guarded rows after remote acquisition. Capture and later
	// canonical writes must not clear a stop or borrow a newer actor assignment.
	for _, before := range rows {
		after, err := r.conversationTask(ctx, before.ID)
		if err != nil || string(before.conversationState) != string(after.conversationState) {
			return nil, errImplementation
		}
	}
	dir := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies")
	path := filepath.Join(dir, reply.Message+".json")
	body, _ := json.Marshal(proof)
	capture := &Request{Thread: q.Thread, Guild: old.Guild, RecordVersion: 2, Message: reply.Message, Author: reply.Author, Created: reply.Created, Edited: live.Edited,
		ReplyTo: live.Reference.Message, ReplyChannel: live.Reference.Channel, ReplyGuild: live.Reference.Guild, Acquired: r.Now().UTC().Format(time.RFC3339Nano), Source: "discord-api-v10", Text: reply.Body,
		Context: string(body), Key: old.Key, Phase: "conversation_reply_saved", ProgressThread: q.Progress, Conversation: q.RequestID}
	prior, missing, err := conversationCloseRead(path, maxState)
	if err != nil {
		return nil, errImplementation
	}
	if !missing {
		var recovered Request
		var priorProof conversationProof
		if json.Unmarshal(prior, &recovered) != nil || recovered.Conversation != q.RequestID || recovered.Todo != 0 || recovered.Phase != "conversation_reply_saved" || recovered.Source != capture.Source || recovered.RecordVersion != 2 || recovered.Thread != capture.Thread || recovered.Guild != capture.Guild || recovered.Message != capture.Message || recovered.Author != capture.Author || recovered.Created != capture.Created || recovered.Edited != capture.Edited || recovered.Text != capture.Text || recovered.Key != capture.Key || recovered.ReplyTo != capture.ReplyTo || recovered.ReplyChannel != capture.ReplyChannel || recovered.ReplyGuild != capture.ReplyGuild || json.Unmarshal([]byte(recovered.Context), &priorProof) != nil {
			return nil, errImplementation
		}
		if _, err := r.conversationCloseProofHash(q, &priorProof); err != nil {
			return nil, errImplementation
		}
		capture = &recovered
	}
	b, _ := json.Marshal(capture)
	if safeText(string(b), r.Token) != string(b) || writeConversationOnce(path, b) != nil {
		return nil, errImplementation
	}
	snapshot, _ := json.Marshal(old)
	link := conversationIntakeLinkReceipt{Domain: "discord-conversation-intake-link-v1", Request: q.RequestID, Origin: q.Todo, Intake: opts.IntakeTodo, Actor: opts.Todo, Job: os.Getenv("CC_JOB_ID"), Revision: opts.Revision, Message: reply.Message, QuestionSHA: q.ReceiptSHA, RawSHA: opts.SourceSHA, OrdinarySHA: ordinaryVersion(old), IntakeSHA: bytesHash(snapshot), CaptureSHA: bytesHash(b)}
	linkBytes, _ := json.Marshal(link)
	if safeText(string(snapshot), r.Token) != string(snapshot) || writeConversationOnce(filepath.Join(dir, reply.Message+".intake-"+link.IntakeSHA+".json"), snapshot) != nil || writeConversationOnce(filepath.Join(dir, reply.Message+".intake-capture-"+link.CaptureSHA+".json"), b) != nil || writeConversationOnce(filepath.Join(dir, reply.Message+".intake-link.json"), linkBytes) != nil {
		return nil, errImplementation
	}
	capture.ConversationIntakeLinkSHA = bytesHash(linkBytes)
	if r.State.ConversationCaptures == nil {
		r.State.ConversationCaptures = map[string]*Request{}
	}
	previous, exists := r.State.ConversationCaptures[reply.Message]
	if exists {
		if previous == nil || previous.Conversation != q.RequestID {
			return nil, errImplementation
		}
		capture = previous
	} else {
		r.State.ConversationCaptures[reply.Message] = capture
		if err := r.save(); err != nil {
			delete(r.State.ConversationCaptures, reply.Message)
			return nil, err
		}
	}
	stamp, ready, err := r.conversationReplySource(ctx, q, proof, reply)
	if err != nil || !ready {
		return nil, errImplementation
	}
	// A source bound to its actual ordinary intake is ready for the explicit
	// implementation writer. Do not replace the original technical wait with a
	// question wait or resume its worker as an incidental effect of linking.
	if !crossProject {
		if err := r.deliverConversationReply(ctx, q, reply, capture); err != nil {
			return nil, err
		}
	}
	return map[string]any{"domain": link.Domain, "linked": true, "request_id": q.RequestID, "ordinary_intake_retained": true, "original_source_sha256": link.RawSHA, "ordinary_source_sha256": link.OrdinarySHA, "source_receipt_sha256": stamp.SourceSHA, "intake_todo_id": opts.IntakeTodo, "origin_todo_id": q.Todo, "actor_todo_id": opts.Todo, "application_inferred": false}, nil
}

// Every later verification retains the origin of this exceptional association.
// The immutable original may predate an edit; the usual live/source checks still
// decide whether the currently selected conversation capture is current.
func (r *Runner) conversationIntakeRetained(q ConversationQuestion, req *Request) bool {
	if r.State.ConversationCaptures[req.Message] == nil {
		return req.ConversationIntakeLinkSHA == ""
	}
	if !shaPattern.MatchString(req.ConversationIntakeLinkSHA) {
		return false
	}
	dir := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies")
	b, err := boundedRegular(filepath.Join(dir, req.Message+".intake-link.json"))
	var link conversationIntakeLinkReceipt
	if err != nil || bytesHash(b) != req.ConversationIntakeLinkSHA || json.Unmarshal(b, &link) != nil || link.Domain != "discord-conversation-intake-link-v1" || link.Request != q.RequestID || link.Origin != q.Todo || link.Message != req.Message || link.QuestionSHA != q.ReceiptSHA || link.Intake == 0 || link.Actor == 0 || !validJob(link.Job) || !regexpRevision(link.Revision) {
		return false
	}
	for _, sha := range []string{link.RawSHA, link.OrdinarySHA, link.IntakeSHA, link.CaptureSHA} {
		if !shaPattern.MatchString(sha) {
			return false
		}
	}
	original, err := boundedRegular(filepath.Join(r.Config.Home, "requests", req.Message+".json"))
	if err != nil || bytesHash(original) != link.RawSHA {
		return false
	}
	snapshot, err := boundedRegular(filepath.Join(dir, req.Message+".intake-"+link.IntakeSHA+".json"))
	var intake Request
	if err != nil || bytesHash(snapshot) != link.IntakeSHA || json.Unmarshal(snapshot, &intake) != nil || intake.Todo != link.Intake || intake.Conversation != "" || ordinaryVersion(&intake) != link.OrdinarySHA {
		return false
	}
	initial, err := boundedRegular(filepath.Join(dir, req.Message+".intake-capture-"+link.CaptureSHA+".json"))
	if err != nil || bytesHash(initial) != link.CaptureSHA {
		return false
	}
	current := r.State.Requests[req.Message]
	return current != nil && current.Conversation == "" && current.Todo == link.Intake && current.Thread == q.Thread && current.Message == req.Message && current.Author == req.Author
}
