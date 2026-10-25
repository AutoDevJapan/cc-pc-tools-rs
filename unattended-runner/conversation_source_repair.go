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

const conversationRepairDomain = "discord-finished-intake-source-repair-v1"

type conversationRepairJob struct {
	ID        string `json:"id"`
	Todo      uint64 `json:"todo_id"`
	Project   string `json:"project"`
	Root      string `json:"registered_cwd"`
	Cwd       string `json:"cwd"`
	Key       string `json:"launch_key"`
	Status    string `json:"status"`
	Exit      *int   `json:"exit_code"`
	Started   string `json:"started_at"`
	Finished  string `json:"finished_at"`
	Cancelled bool   `json:"cancel_requested"`
}

type conversationRepairAudit struct {
	Domain           string                `json:"domain"`
	QuestionIdentity string                `json:"question_identity_sha256"`
	Sender           string                `json:"sender_request_id"`
	BodySHA          string                `json:"body_sha256"`
	OriginSHA        string                `json:"origin_snapshot_sha256"`
	ActorSHA         string                `json:"actor_snapshot_sha256"`
	IntakeSHA        string                `json:"intake_task_snapshot_sha256"`
	IntakeJob        conversationRepairJob `json:"finished_intake_job"`
	SpecSHA          string                `json:"intake_spec_sha256"`
	PromptSHA        string                `json:"intake_prompt_sha256"`
	OriginalSize     int64                 `json:"original_source_size"`
	OriginalModified string                `json:"original_source_modified_at"`
}

func (r *Runner) repairGuard(ctx context.Context, path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	raw, err := r.Run(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", path)
	var p struct {
		Client  *bool `json:"client_resource"`
		Unknown *bool `json:"classification_unverified"`
	}
	return err == nil && json.Unmarshal([]byte(raw), &p) == nil && p.Client != nil && !*p.Client && p.Unknown != nil && !*p.Unknown
}

func (r *Runner) repairJobPath(id string) string {
	root := r.JobsPath
	if root == "" {
		root = "D:/CC/Tools/codex-jobs/jobs"
	}
	return filepath.Join(root, id)
}

func (r *Runner) repairJob(ctx context.Context, row *TodoRow, id string) (conversationRepairJob, error) {
	var job conversationRepairJob
	path := filepath.Join(r.repairJobPath(id), "job.json")
	if !validJob(id) || readJSON(path, 1<<20, &job) != nil || job.ID != id || job.Todo != row.ID || job.Project != row.Project || !filepath.IsAbs(job.Root) || bytesHash([]byte(job.Root)) != row.conversationScope.RootSHA || !filepath.IsAbs(job.Cwd) {
		return job, errImplementation
	}
	return job, nil
}

// A stale scheduler phase is not evidence of a running worker. Conversely, a
// note saying "finished" is not evidence of an exited, normally registered job.
func (r *Runner) repairFinishedIntake(ctx context.Context, row *TodoRow, q ConversationQuestion, message string) (conversationRepairAudit, error) {
	var audit conversationRepairAudit
	if row.Status != "wait" || len(row.WaitOn) != 1 || row.WaitOn[0] != q.Todo || row.Worker.Job != "" || len(row.Jobs) == 0 || len(row.Jobs) > 128 || !validJob(row.Scheduler.Job) {
		return audit, errImplementation
	}
	var raw struct {
		Scheduler struct {
			Key   string `json:"key"`
			Phase string `json:"phase"`
		} `json:"scheduler"`
	}
	if json.Unmarshal(row.conversationState, &raw) != nil || !validJob(raw.Scheduler.Key) || (raw.Scheduler.Phase != "running" && raw.Scheduler.Phase != "finished" && raw.Scheduler.Phase != "succeeded") {
		return audit, errImplementation
	}
	seen := map[string]bool{}
	for i, b := range row.Jobs {
		var id string
		if json.Unmarshal(b, &id) != nil || seen[id] {
			return audit, errImplementation
		}
		seen[id] = true
		job, err := r.repairJob(ctx, row, id)
		start, e1 := time.Parse(time.RFC3339Nano, job.Started)
		end, e2 := time.Parse(time.RFC3339Nano, job.Finished)
		if err != nil || e1 != nil || e2 != nil || end.Before(start) || end.After(r.Now()) || job.Exit == nil || (job.Status != "succeeded" && job.Status != "failed" && job.Status != "cancelled") {
			return audit, errImplementation
		}
		if id == row.Scheduler.Job {
			if i != len(row.Jobs)-1 || job.Status != "succeeded" || *job.Exit != 0 || job.Cancelled || job.Key != raw.Scheduler.Key || !strings.EqualFold(filepath.Clean(job.Cwd), filepath.Clean(row.Scheduler.Worktree)) {
				return audit, errImplementation
			}
			audit.IntakeJob = job
		}
	}
	if audit.IntakeJob.ID == "" {
		return audit, errImplementation
	}
	var spec string
	prompt := filepath.Join(r.repairJobPath(audit.IntakeJob.ID), "prompt.md")
	if json.Unmarshal(row.Spec, &spec) != nil || !r.repairRegisteredSpec(spec) {
		return audit, errImplementation
	}
	b, e1 := boundedRegular(spec)
	p, e2 := boundedRegular(prompt)
	// Use decimal boundaries, not a body/short-answer heuristic.
	for _, text := range []string{string(b), string(p)} {
		// Intake does not know the future question association; only its exact
		// source thread/message are required in the saved assignment.
		if !repairContainsID(text, q.Thread) || !repairContainsID(text, message) {
			return audit, errImplementation
		}
	}
	if e1 != nil || e2 != nil {
		return audit, errImplementation
	}
	audit.SpecSHA, audit.PromptSHA = bytesHash(b), bytesHash(p)
	return audit, nil
}

func repairContainsID(text, id string) bool {
	return regexp.MustCompile(`(^|[^0-9])` + regexp.QuoteMeta(id) + `($|[^0-9])`).MatchString(text)
}

func (r *Runner) repairRegisteredSpec(path string) bool {
	rel, err := filepath.Rel(filepath.Dir(r.TodoPath), path)
	return filepath.IsAbs(path) && err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// Attachment URLs may be freshly signed by Discord. Preserve every original
// metadata field privately, but do not confuse a refreshed URL with an edit.
func repairAttachmentIdentity(raw json.RawMessage) string {
	var items []map[string]json.RawMessage
	if len(raw) == 0 || string(raw) == "null" || json.Unmarshal(raw, &items) != nil || items == nil {
		return ""
	}
	for _, item := range items {
		delete(item, "url")
		delete(item, "proxy_url")
	}
	return canonicalHash(items)
}

func (r *Runner) repairQuestionSource(ctx context.Context, q ConversationQuestion) bool {
	if !q.Wait || q.SenderID != "conversation-v1-"+bytesHash([]byte(q.RequestID))[:32] {
		return false
	}
	// These are the normal question writer's fixed, hash-bound inputs, checked
	// against the independently acquired sender proof; never an intake copy.
	b, err := boundedRegular(q.ReceiptFile)
	var receipt conversationQuestionReceipt
	if err != nil || bytesHash(b) != q.ReceiptSHA || json.Unmarshal(b, &receipt) != nil || receipt.Domain != "discord-conversation-question-v1" || receipt.RequestID != q.SenderID || receipt.Todo != fmt.Sprint(q.Todo) || receipt.Guild != r.State.Destination.Guild || receipt.Owner != r.State.Destination.Owner || receipt.Bot != r.State.Destination.Bot || receipt.Thread != q.Thread || receipt.Message != q.Message || receipt.Progress != q.Progress || receipt.Fingerprint != q.Fingerprint {
		return false
	}
	body, e1 := boundedRegular(q.BodyFile)
	decision, e2 := boundedRegular(q.DecisionFile)
	return e1 == nil && e2 == nil && bytesHash(body) == q.QuestionSHA && canonicalHash(json.RawMessage(decision)) == q.DecisionSHA
}

func (r *Runner) conversationSourceRepair(ctx context.Context, opts conversationCLIOptions, q ConversationQuestion, proof *conversationProof, reply conversationReply) (map[string]any, error) {
	if opts.Todo == q.Todo || opts.IntakeTodo == q.Todo || opts.IntakeTodo == opts.Todo || bytesHash([]byte(reply.Body)) != opts.BodySHA || !r.repairQuestionSource(ctx, q) {
		return nil, errImplementation
	}
	old := r.State.Requests[reply.Message]
	if old == nil || old.Conversation != "" || old.Todo != opts.IntakeTodo || old.Phase != "registered" || old.RecordVersion != 2 || old.Source != "discord-api-v10" || old.Thread != q.Thread || old.Guild != r.State.Destination.Guild || old.Message != reply.Message || old.Author != r.State.Destination.Owner || old.Author != reply.Author || old.Text != reply.Body || !sameLinkTime(old.Created, reply.Created) || old.Key != requestKey(q.Thread, reply.Message) {
		return nil, errImplementation
	}
	rows := make([]*TodoRow, 0, 3)
	for _, id := range []uint64{q.Todo, opts.IntakeTodo, opts.Todo} {
		row, err := r.conversationTask(ctx, id)
		if err != nil || linkStopped(row.conversationState) || row.Status == "recovery_unverified" {
			return nil, errImplementation
		}
		rows = append(rows, row)
	}
	if rows[0].Project != q.Project || rows[1].Project != r.Config.Project || rows[2].Project != r.Config.Project || (!conversationWaitRecorded(rows[0], q) && !conversationQuestionAnswered(rows[0], q)) {
		return nil, errImplementation
	}
	actor := rows[2]
	var spec string
	if json.Unmarshal(actor.Spec, &spec) != nil || !r.repairRegisteredSpec(spec) {
		return nil, errImplementation
	}
	if !r.repairGuard(ctx, actor.Scheduler.Worktree) {
		return nil, errImplementation
	}
	job, err := r.repairJob(ctx, actor, os.Getenv("CC_JOB_ID"))
	cwd, cwdErr := os.Getwd()
	if err != nil || cwdErr != nil || !strings.EqualFold(filepath.Clean(cwd), filepath.Clean(job.Cwd)) || !r.linkActor(actor, q, opts) {
		return nil, errImplementation
	}
	var actorState struct {
		Scheduler struct {
			Key string `json:"key"`
		} `json:"scheduler"`
	}
	if json.Unmarshal(actor.conversationState, &actorState) != nil || job.Key != actorState.Scheduler.Key || !validJob(job.Key) {
		return nil, errImplementation
	}
	head, err := r.Run(ctx, "git", "-C", job.Cwd, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(head) != opts.Revision {
		return nil, errImplementation
	}
	audit, err := r.repairFinishedIntake(ctx, rows[1], q, reply.Message)
	if err != nil {
		return nil, err
	}
	// The original general-intake file can be protection-unverified. Retain it
	// without opening it. This contract's source SHA identifies the registered
	// ordinary version, independently compared to a fresh owner-post GET below.
	originalPath := filepath.Join(r.Config.Home, "requests", reply.Message+".json")
	info, err := os.Lstat(originalPath)
	if err != nil || !info.Mode().IsRegular() || ordinaryVersion(old) != opts.SourceSHA {
		return nil, errImplementation
	}
	audit.OriginalSize, audit.OriginalModified = info.Size(), info.ModTime().UTC().Format(time.RFC3339Nano)
	var live struct {
		Message
		Type        *int            `json:"type"`
		Attachments json.RawMessage `json:"attachments"`
	}
	if r.get(ctx, "/channels/"+q.Thread+"/messages/"+reply.Message, &live) != nil || live.ID != reply.Message || live.Channel != q.Thread || live.Author.ID != reply.Author || live.Author.Bot || live.Timestamp != reply.Created || !sameLinkTime(live.Timestamp, old.Created) || live.Content != reply.Body || live.Edited != old.Edited || live.Reference.Message != old.ReplyTo || live.Reference.Channel != old.ReplyChannel || live.Reference.Guild != old.ReplyGuild || (live.Reference.Channel != "" && live.Reference.Channel != q.Thread) || (live.Reference.Guild != "" && live.Reference.Guild != old.Guild) {
		return nil, errImplementation
	}
	now := r.Now()
	created, e1 := time.Parse(time.RFC3339Nano, live.Timestamp)
	if e1 != nil || created.After(now) {
		return nil, errImplementation
	}
	if live.Edited != "" {
		edited, err := time.Parse(time.RFC3339Nano, live.Edited)
		if err != nil || edited.Before(created) || edited.After(now) {
			return nil, errImplementation
		}
	}
	version, ok := proof.ReplyVersions[reply.Message]
	if !ok || (version.Edited == nil && live.Edited != "") || (version.Edited != nil && *version.Edited != live.Edited) {
		return nil, errImplementation
	}
	var attachments *[]historyReadAttachment
	if json.Unmarshal(live.Attachments, &attachments) != nil || attachments == nil || live.Type == nil || (*live.Type != 0 && *live.Type != 19) || len(*attachments) > 10 || repairAttachmentIdentity(live.Attachments) == "" || !historyAttachmentsValid(historyReadMessage{Type: live.Type, Attachments: attachments}) || (old.MessageType != nil && canonicalHash(old.MessageType) != canonicalHash(live.Type)) || (old.Attachments != nil && canonicalHash(old.Attachments) != canonicalHash(attachments)) {
		return nil, errImplementation
	}
	seenAttachments := map[string]bool{}
	for _, a := range *attachments {
		if seenAttachments[a.ID] {
			return nil, errImplementation
		}
		seenAttachments[a.ID] = true
	}
	body, _ := json.Marshal(proof)
	capture := &Request{Thread: q.Thread, Guild: old.Guild, RecordVersion: 2, Message: reply.Message, Author: reply.Author, Created: reply.Created, Edited: live.Edited, ReplyTo: live.Reference.Message, ReplyChannel: live.Reference.Channel, ReplyGuild: live.Reference.Guild, Acquired: now.UTC().Format(time.RFC3339Nano), Source: "discord-api-v10", Text: reply.Body, Context: string(body), Key: old.Key, Phase: "conversation_reply_saved", ProgressThread: q.Progress, Conversation: q.RequestID, MessageType: live.Type, Attachments: attachments, ConversationAttachmentMetadata: live.Attachments}
	dir := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies")
	path := filepath.Join(dir, reply.Message+".json")
	prior, missing, err := conversationCloseRead(path, maxState)
	if err != nil {
		return nil, errImplementation
	}
	if !missing {
		var recovered Request
		var priorProof conversationProof
		if json.Unmarshal(prior, &recovered) != nil || recovered.Conversation != q.RequestID || recovered.Todo != 0 || recovered.RecordVersion != 2 || recovered.Source != capture.Source || recovered.Phase != capture.Phase || ordinaryVersion(&recovered) != ordinaryVersion(capture) || recovered.Key != capture.Key || repairAttachmentIdentity(recovered.ConversationAttachmentMetadata) != repairAttachmentIdentity(capture.ConversationAttachmentMetadata) || json.Unmarshal([]byte(recovered.Context), &priorProof) != nil {
			return nil, errImplementation
		}
		if _, err := r.conversationCloseProofHash(q, &priorProof); err != nil {
			return nil, errImplementation
		}
		capture = &recovered
	}
	b, _ := json.Marshal(capture)
	if safeText(string(b), r.Token) != string(b) {
		return nil, errImplementation
	}
	audit.Domain, audit.QuestionIdentity, audit.Sender, audit.BodySHA = conversationRepairDomain, r.conversationCloseIdentity(q), q.SenderID, opts.BodySHA
	audit.OriginSHA, audit.IntakeSHA, audit.ActorSHA = bytesHash(rows[0].conversationState), bytesHash(rows[1].conversationState), bytesHash(actor.conversationState)
	auditPath := filepath.Join(dir, reply.Message+".source-repair.json")
	auditBytes, auditMissing, err := conversationCloseRead(auditPath, 1<<20)
	if err != nil {
		return nil, errImplementation
	}
	if !auditMissing {
		var recovered conversationRepairAudit
		if json.Unmarshal(auditBytes, &recovered) != nil || recovered.Domain != audit.Domain || recovered.QuestionIdentity != audit.QuestionIdentity || recovered.Sender != audit.Sender || recovered.BodySHA != audit.BodySHA || canonicalHash(recovered.IntakeJob) != canonicalHash(audit.IntakeJob) || recovered.SpecSHA != audit.SpecSHA || recovered.PromptSHA != audit.PromptSHA || recovered.OriginalSize != audit.OriginalSize || recovered.OriginalModified != audit.OriginalModified {
			return nil, errImplementation
		}
		audit = recovered
	}
	auditBytes, _ = json.Marshal(audit)
	snapshot, _ := json.Marshal(old)
	link := conversationIntakeLinkReceipt{Domain: conversationRepairDomain, Request: q.RequestID, Origin: q.Todo, Intake: opts.IntakeTodo, Actor: opts.Todo, Job: job.ID, Revision: opts.Revision, Message: reply.Message, QuestionSHA: q.ReceiptSHA, OrdinarySHA: ordinaryVersion(old), IntakeSHA: bytesHash(snapshot), CaptureSHA: bytesHash(b), RepairSHA: bytesHash(auditBytes)}
	linkBytes, _ := json.Marshal(link)
	linkPath := filepath.Join(dir, reply.Message+".intake-link.json")
	firstBytes, firstMissing, err := conversationCloseRead(linkPath, 1<<20)
	if err != nil {
		return nil, errImplementation
	}
	if !firstMissing {
		var first conversationIntakeLinkReceipt
		if json.Unmarshal(firstBytes, &first) != nil {
			return nil, errImplementation
		}
		expected := link
		expected.Job = first.Job
		if canonicalHash(first) != canonicalHash(expected) || !r.repairPriorActor(ctx, actor, first.Job, job.ID) {
			return nil, errImplementation
		}
		// The first writer's audit survives a crash. A newly qualified regular
		// job may finish the same input, but cannot rewrite the first writer.
		link, linkBytes = first, firstBytes
	}
	if previous := r.State.ConversationCaptures[reply.Message]; previous != nil && (previous.Conversation != q.RequestID || previous.ConversationIntakeLinkSHA != bytesHash(linkBytes) || ordinaryVersion(previous) != ordinaryVersion(capture) || previous.Context != capture.Context || previous.Acquired != capture.Acquired) {
		return nil, errImplementation
	}
	for _, before := range rows {
		after, err := r.conversationTask(ctx, before.ID)
		if err != nil || string(before.conversationState) != string(after.conversationState) {
			return nil, errImplementation
		}
	}
	finished, err := r.repairFinishedIntake(ctx, rows[1], q, reply.Message)
	if err != nil || canonicalHash(finished.IntakeJob) != canonicalHash(audit.IntakeJob) || finished.SpecSHA != audit.SpecSHA || finished.PromptSHA != audit.PromptSHA {
		return nil, errImplementation
	}
	currentActor, err := r.repairJob(ctx, actor, job.ID)
	if err != nil || canonicalHash(currentActor) != canonicalHash(job) || !r.linkActor(actor, q, opts) {
		return nil, errImplementation
	}
	// Canonical and audit become durable before a second capture is visible to
	// the ordinary receiver. None of these operations replace an existing file.
	for _, file := range []struct {
		path string
		data []byte
	}{{path, b}, {auditPath, auditBytes}, {filepath.Join(dir, reply.Message+".intake-"+link.IntakeSHA+".json"), snapshot}, {filepath.Join(dir, reply.Message+".intake-capture-"+link.CaptureSHA+".json"), b}, {linkPath, linkBytes}} {
		if safeText(string(file.data), r.Token) != string(file.data) || writeConversationOnce(file.path, file.data) != nil {
			return nil, errImplementation
		}
	}
	if r.State.ConversationCaptures == nil {
		r.State.ConversationCaptures = map[string]*Request{}
	}
	if previous := r.State.ConversationCaptures[reply.Message]; previous != nil {
		if previous.Conversation != q.RequestID || previous.ConversationIntakeLinkSHA != bytesHash(linkBytes) || ordinaryVersion(previous) != ordinaryVersion(capture) || previous.Context != capture.Context || previous.Acquired != capture.Acquired || canonicalHash(previous.ConversationAttachmentMetadata) != canonicalHash(capture.ConversationAttachmentMetadata) {
			return nil, errImplementation
		}
		capture = previous
	} else {
		capture.ConversationIntakeLinkSHA = bytesHash(linkBytes)
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
	if err := r.deliverConversationReply(ctx, q, reply, capture); err != nil {
		return nil, err
	}
	return map[string]any{"domain": conversationRepairDomain, "linked": true, "canonical_current": true, "request_id": q.RequestID, "message_id": reply.Message, "origin_todo_id": q.Todo, "intake_todo_id": opts.IntakeTodo, "actor_todo_id": opts.Todo, "actor_job_id": job.ID, "association_writer_job_id": link.Job, "ordinary_intake_retained": true, "source_receipt_sha256": stamp.SourceSHA, "reply_sha256": stamp.ReplySHA, "repair_audit_sha256": link.RepairSHA, "application_inferred": false}, nil
}

func (r *Runner) repairPriorActor(ctx context.Context, row *TodoRow, first, current string) bool {
	if first == current {
		return true
	}
	listed := false
	for _, raw := range row.Jobs {
		var id string
		if json.Unmarshal(raw, &id) == nil && id == first {
			listed = true
		}
	}
	if !listed {
		return false
	}
	job, err := r.repairJob(ctx, row, first)
	at, timeErr := time.Parse(time.RFC3339Nano, job.Finished)
	if timeErr != nil && job.Status == "lost" {
		// The normal scheduler's mark_lost writes a basic numeric UTC offset
		// (%z), whereas the job runner writes RFC3339. Neither is a local time.
		at, timeErr = time.Parse("2006-01-02T15:04:05-0700", job.Finished)
	}
	return err == nil && timeErr == nil && !at.After(r.Now()) && (job.Status == "succeeded" || job.Status == "failed" || job.Status == "lost" || job.Status == "cancelled")
}

func (r *Runner) conversationRepairRetained(q ConversationQuestion, req *Request, link conversationIntakeLinkReceipt) bool {
	b, err := boundedRegular(filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", req.Message+".source-repair.json"))
	var audit conversationRepairAudit
	if err != nil || !shaPattern.MatchString(link.RepairSHA) || bytesHash(b) != link.RepairSHA || json.Unmarshal(b, &audit) != nil || audit.Domain != conversationRepairDomain || audit.QuestionIdentity != r.conversationCloseIdentity(q) || audit.Sender != q.SenderID || audit.BodySHA != bytesHash([]byte(req.Text)) || audit.IntakeJob.Todo != link.Intake || audit.IntakeJob.Status != "succeeded" || audit.IntakeJob.Exit == nil || *audit.IntakeJob.Exit != 0 || repairAttachmentIdentity(req.ConversationAttachmentMetadata) == "" {
		return false
	}
	info, err := os.Lstat(filepath.Join(r.Config.Home, "requests", req.Message+".json"))
	return err == nil && info.Mode().IsRegular() && info.Size() == audit.OriginalSize && info.ModTime().UTC().Format(time.RFC3339Nano) == audit.OriginalModified
}

func (r *Runner) repairLiveMetadata(ctx context.Context, q ConversationQuestion, req *Request) bool {
	var live struct {
		Message
		Type        *int            `json:"type"`
		Attachments json.RawMessage `json:"attachments"`
	}
	if r.get(ctx, "/channels/"+q.Thread+"/messages/"+req.Message, &live) != nil || live.ID != req.Message || live.Channel != req.Thread || live.Author.ID != req.Author || live.Author.Bot || live.Timestamp != req.Created || live.Content != req.Text || live.Edited != req.Edited || live.Reference.Message != req.ReplyTo || live.Reference.Channel != req.ReplyChannel || live.Reference.Guild != req.ReplyGuild || canonicalHash(live.Type) != canonicalHash(req.MessageType) {
		return false
	}
	return repairAttachmentIdentity(live.Attachments) != "" && repairAttachmentIdentity(live.Attachments) == repairAttachmentIdentity(req.ConversationAttachmentMetadata)
}
