package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const conversationCloseDomain = "discord-conversation-close-v1"

var errConversationClose = errors.New("conversation close verification deferred")
var errConversationCloseState = errors.New("conversation close record invalid or unavailable")
var errConversationCloseSource = errors.New("conversation close source association differs")
var errConversationCloseApplied = errors.New("conversation implementation receipt invalid or unavailable")

type conversationCloseStamp struct {
	ReplySHA               string `json:"reply_sha256"`
	SourceSHA              string `json:"source_receipt_sha256,omitempty"`
	AppliedSHA             string `json:"applied_receipt_sha256,omitempty"`
	DirectionSHA           string `json:"direction_receipt_sha256,omitempty"`
	DirectionInputSHA      string `json:"direction_input_sha256,omitempty"`
	ImplementationSHA      string `json:"implementation_receipt_sha256,omitempty"`
	ImplementationInputSHA string `json:"implementation_input_sha256,omitempty"`
}

type conversationCloseHistory struct {
	Phase    string                            `json:"phase"`
	ProofSHA string                            `json:"proof_sha256"`
	Replies  map[string]conversationCloseStamp `json:"replies"`
	At       string                            `json:"at"`
}

// Only hashes and receipt identities belong here; owner text stays in the
// existing private source receipts. A successful earlier close is never erased.
type conversationCloseRecord struct {
	Domain        string                            `json:"domain"`
	Identity      string                            `json:"identity_sha256"`
	RequestID     string                            `json:"request_id"`
	Thread        string                            `json:"thread_id"`
	Phase         string                            `json:"phase"`
	ProofSHA      string                            `json:"proof_sha256"`
	Replies       map[string]conversationCloseStamp `json:"replies"`
	Created       string                            `json:"created_at"`
	Updated       string                            `json:"updated_at"`
	PatchAttempts uint64                            `json:"patch_attempts"`
	VerifiedScope string                            `json:"verified_scope_sha256,omitempty"`
	VerifiedAt    string                            `json:"verified_at,omitempty"`
	History       []conversationCloseHistory        `json:"history,omitempty"`
}

type conversationCloseThread struct {
	ID       string `json:"id"`
	Parent   string `json:"parent_id"`
	Guild    string `json:"guild_id"`
	Owner    string `json:"owner_id"`
	Type     int    `json:"type"`
	Metadata struct {
		Archived *bool `json:"archived"`
	} `json:"thread_metadata"`
}

type conversationCloseApplication struct {
	Kind         string `json:"kind,omitempty"`
	Source       string `json:"source"`
	RequestID    string `json:"request_id"`
	OriginTodo   uint64 `json:"origin_todo_id"`
	Todo         uint64 `json:"todo_id"`
	Thread       string `json:"thread_id"`
	Question     string `json:"question_message_id"`
	Message      string `json:"message_id"`
	Author       string `json:"author_id"`
	Job          string `json:"job_id"`
	Revision     string `json:"revision"`
	AppliedAt    string `json:"applied_at"`
	Completion   string `json:"completion"`
	ReplySHA     string `json:"reply_sha256"`
	SourceSHA    string `json:"source_receipt_sha256"`
	Verification struct {
		Execution string `json:"execution"`
		Origin    string `json:"origin_saved"`
		Identity  string `json:"identity"`
	} `json:"verification"`
}

func conversationCloseRead(path string, ceiling int64) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, true, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > ceiling {
		return nil, false, errConversationCloseState
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false, errConversationCloseState
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, ceiling+1))
	if err != nil || int64(len(b)) > ceiling {
		return nil, false, errConversationCloseState
	}
	return b, false, nil
}

func (r *Runner) conversationCloseIdentity(q ConversationQuestion) string {
	return canonicalHash([]any{q.RequestID, q.SenderID, q.Todo, q.Project, q.Thread, q.Message, q.Progress, q.OriginThread, q.Fingerprint, q.DecisionSHA, q.QuestionSHA, q.ReceiptSHA, r.State.Destination})
}

func (r *Runner) conversationCloseProofHash(q ConversationQuestion, proof *conversationProof) (string, error) {
	if proof == nil || proof.Domain != "new-owner-recovery-messages" || !proof.ReadOnly || proof.Source != "discord-api-v10" || proof.RequestID != q.SenderID || proof.Thread != q.Thread || proof.Question != q.Message || proof.Progress != q.Progress || proof.Owner != r.State.Destination.Owner || canonicalHash(proof.Decision) != q.DecisionSHA || bytesHash([]byte(proof.Body)) != q.QuestionSHA {
		return "", errConversationCloseSource
	}
	created, err := time.Parse(time.RFC3339Nano, proof.Created)
	if err != nil || created.After(r.Now().Add(time.Minute)) {
		return "", errConversationCloseSource
	}
	seen := make(map[string]bool)
	question, _ := number(q.Message)
	copy := *proof
	// Optional polling metadata is not part of the original close-proof identity.
	// Edit/source hashes and exact live posts are verified independently below.
	copy.ReplyVersions = nil
	copy.Fingerprint = "" // Optional sender metadata was absent in earlier proof hashes.
	copy.Replies = append([]conversationReply{}, proof.Replies...)
	for _, reply := range copy.Replies {
		id, valid := number(reply.Message)
		at, err := time.Parse(time.RFC3339Nano, reply.Created)
		if !valid || id <= question || seen[reply.Message] || err != nil || at.Before(created) || at.After(r.Now().Add(time.Minute)) || reply.Author != r.State.Destination.Owner || reply.RequestID != q.SenderID || reply.Todo != q.Todo || reply.Thread != q.Thread || reply.Question != q.Message || canonicalHash(reply.Decision) != q.DecisionSHA {
			return "", errConversationCloseSource
		}
		seen[reply.Message] = true
	}
	sort.Slice(copy.Replies, func(i, j int) bool {
		a, _ := number(copy.Replies[i].Message)
		b, _ := number(copy.Replies[j].Message)
		return a < b
	})
	return canonicalHash(copy), nil
}

func (r *Runner) conversationCloseLoad(q ConversationQuestion, path string) (*conversationCloseRecord, error) {
	b, missing, err := conversationCloseRead(path, maxState)
	if err != nil {
		return nil, errConversationCloseState
	}
	if missing {
		now := r.Now().UTC().Format(time.RFC3339Nano)
		return &conversationCloseRecord{Domain: conversationCloseDomain, Identity: r.conversationCloseIdentity(q), RequestID: q.RequestID, Thread: q.Thread, Phase: "pending", Replies: map[string]conversationCloseStamp{}, Created: now, Updated: now}, nil
	}
	var record conversationCloseRecord
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil || decoder.Decode(new(any)) != io.EOF || record.Domain != conversationCloseDomain || record.Identity != r.conversationCloseIdentity(q) || record.RequestID != q.RequestID || record.Thread != q.Thread || record.Replies == nil || !shaPattern.MatchString(record.ProofSHA) {
		return nil, errConversationCloseState
	}
	switch record.Phase {
	case "pending", "patch_pending", "verified", "reopened":
	default:
		return nil, errConversationCloseState
	}
	created, e1 := time.Parse(time.RFC3339Nano, record.Created)
	updated, e2 := time.Parse(time.RFC3339Nano, record.Updated)
	if e1 != nil || e2 != nil || updated.Before(created) || updated.After(r.Now()) {
		return nil, errConversationCloseState
	}
	if record.VerifiedAt != "" {
		verified, err := time.Parse(time.RFC3339Nano, record.VerifiedAt)
		if err != nil || verified.Before(created) || verified.After(updated) || !shaPattern.MatchString(record.VerifiedScope) {
			return nil, errConversationCloseState
		}
	} else if record.VerifiedScope != "" || record.Phase == "verified" || record.Phase == "reopened" {
		return nil, errConversationCloseState
	}
	for id, stamp := range record.Replies {
		if (stamp.ImplementationSHA != "" || stamp.ImplementationInputSHA != "") && (!shaPattern.MatchString(stamp.ImplementationSHA) || !shaPattern.MatchString(stamp.ImplementationInputSHA)) {
			return nil, errConversationCloseState
		}
		if _, ok := number(id); !ok || !shaPattern.MatchString(stamp.ReplySHA) || (stamp.SourceSHA != "" && !shaPattern.MatchString(stamp.SourceSHA)) || (stamp.AppliedSHA != "" && !shaPattern.MatchString(stamp.AppliedSHA)) || (stamp.DirectionSHA != "" && (!shaPattern.MatchString(stamp.DirectionSHA) || !shaPattern.MatchString(stamp.DirectionInputSHA))) {
			return nil, errConversationCloseState
		}
	}
	return &record, nil
}

func (r *Runner) conversationCloseSave(path string, record *conversationCloseRecord, phase, proofSHA string, replies map[string]conversationCloseStamp) error {
	now := r.Now().UTC().Format(time.RFC3339Nano)
	if record.ProofSHA != "" && (record.Phase != phase || record.ProofSHA != proofSHA || canonicalHash(record.Replies) != canonicalHash(replies)) {
		record.History = append(record.History, conversationCloseHistory{Phase: record.Phase, ProofSHA: record.ProofSHA, Replies: record.Replies, At: record.Updated})
	}
	record.Phase, record.ProofSHA, record.Replies, record.Updated = phase, proofSHA, replies, now
	if publish(path, record) != nil {
		return errConversationCloseState
	}
	return nil
}

func (r *Runner) conversationReplySource(ctx context.Context, q ConversationQuestion, current *conversationProof, reply conversationReply) (conversationCloseStamp, bool, error) {
	return r.conversationReplySourceMessage(ctx, q, current, reply, nil)
}

func (r *Runner) conversationReplySourceMessage(ctx context.Context, q ConversationQuestion, current *conversationProof, reply conversationReply, observed *Message) (conversationCloseStamp, bool, error) {
	stamp := conversationCloseStamp{ReplySHA: canonicalHash(reply)}
	req := r.conversationRequest(reply.Message)
	if req == nil {
		return stamp, false, nil
	}
	if !r.conversationIntakeRetained(q, req) {
		return stamp, false, errConversationCloseSource
	}
	if req.Phase != "conversation_reply_saved" && req.Phase != "conversation_reply_delivered" && req.Phase != "conversation_reply_applied" && req.Phase != "conversation_direction_recorded" && req.Phase != "conversation_implementation_recorded" && req.Phase != "conversation_source_verified" {
		return stamp, false, nil
	}
	if req.Conversation != q.RequestID || (req.Todo != 0 && req.Todo != q.Todo) || req.Source != "discord-api-v10" || req.RecordVersion != 2 || req.Guild != r.State.Destination.Guild || req.Thread != q.Thread || req.Message != reply.Message || req.Author != r.State.Destination.Owner || req.Created != reply.Created || req.Key != requestKey(q.Thread, reply.Message) {
		return stamp, false, errConversationCloseSource
	}
	if req.Text != reply.Body {
		return stamp, false, nil
	}
	acquired, err := time.Parse(time.RFC3339Nano, req.Acquired)
	created, createErr := time.Parse(time.RFC3339Nano, req.Created)
	if err != nil || createErr != nil || acquired.Before(created) || acquired.After(r.Now()) {
		return stamp, false, errConversationCloseSource
	}
	if req.Edited != "" {
		edited, err := time.Parse(time.RFC3339Nano, req.Edited)
		if err != nil || edited.Before(created) || edited.After(acquired) {
			return stamp, false, errConversationCloseSource
		}
	}
	sourcePath, err := r.conversationSourcePath(q, reply.Message)
	if err != nil {
		return stamp, false, err
	}
	source, missing, err := conversationCloseRead(sourcePath, maxState)
	if missing {
		return stamp, false, nil
	}
	if err != nil {
		return stamp, false, errConversationCloseSource
	}
	stamp.SourceSHA = bytesHash(source)
	if req.ConversationSourceGeneration != "" && req.ConversationSourceGeneration != stamp.SourceSHA {
		return stamp, false, errConversationCloseSource
	}
	var saved Request
	if json.Unmarshal(source, &saved) != nil || saved.Conversation != req.Conversation || (saved.Todo != 0 && saved.Todo != q.Todo) || saved.Source != req.Source || saved.RecordVersion != req.RecordVersion || saved.Guild != req.Guild || saved.Thread != req.Thread || saved.Message != req.Message || saved.Author != req.Author || saved.Created != req.Created || saved.Edited != req.Edited || saved.Text != req.Text || saved.Acquired != req.Acquired || saved.Key != req.Key || saved.Context != req.Context || saved.ReplyTo != req.ReplyTo || saved.ReplyChannel != req.ReplyChannel || saved.ReplyGuild != req.ReplyGuild {
		return stamp, false, errConversationCloseSource
	}
	var contextProof conversationProof
	if json.Unmarshal([]byte(req.Context), &contextProof) != nil {
		return stamp, false, errConversationCloseSource
	}
	if _, err := r.conversationCloseProofHash(q, &contextProof); err != nil {
		return stamp, false, errConversationCloseSource
	}
	if canonicalHash(contextProof.OriginalDecision) != canonicalHash(current.OriginalDecision) {
		return stamp, false, errConversationCloseSource
	}
	matched := false
	for _, prior := range contextProof.Replies {
		if prior.Message == reply.Message && canonicalHash(prior) == stamp.ReplySHA {
			matched = true
		}
	}
	if !matched {
		return stamp, false, nil
	}
	var live Message
	if observed != nil {
		live = *observed
	} else if r.get(ctx, "/channels/"+q.Thread+"/messages/"+reply.Message, &live) != nil {
		return stamp, false, errConversationClose
	}
	if live.ID != reply.Message || live.Channel != q.Thread || live.Author.ID != req.Author || live.Author.Bot || live.Timestamp != reply.Created || live.Reference.Message != req.ReplyTo || live.Reference.Channel != req.ReplyChannel || live.Reference.Guild != req.ReplyGuild || (live.Reference.Channel != "" && live.Reference.Channel != q.Thread) || (live.Reference.Guild != "" && live.Reference.Guild != r.State.Destination.Guild) {
		return stamp, false, errConversationCloseSource
	}
	if live.Content != reply.Body || live.Edited != req.Edited {
		return stamp, false, nil
	}
	return stamp, true, nil
}

func (r *Runner) conversationCloseReply(ctx context.Context, q ConversationQuestion, current *conversationProof, reply conversationReply, row *TodoRow) (conversationCloseStamp, bool, error) {
	stamp, ready, err := r.conversationReplySource(ctx, q, current, reply)
	if err != nil || !ready {
		return stamp, false, err
	}
	req := r.conversationRequest(reply.Message)
	if found, err := findImplementation(row, q, reply.Message, stamp.SourceSHA); err != nil {
		return stamp, false, err
	} else if found != nil {
		return r.conversationImplementationApplied(ctx, q, current, reply, found, stamp)
	}
	// Saved sources are verifiable, but never stand for delivery or application.
	if req.Phase == "conversation_reply_saved" || req.Phase == "conversation_source_verified" || req.Todo == 0 {
		return stamp, false, nil
	}
	b, missing, err := conversationCloseRead(filepath.Join(r.Config.Home, "applied", reply.Message+".json"), 128<<10)
	if missing {
		return r.conversationDirectionApplied(ctx, q, current, reply, row, stamp)
	}
	if err != nil {
		return stamp, false, errConversationCloseApplied
	}
	stamp.AppliedSHA = bytesHash(b)
	var applied conversationCloseApplication
	if json.Unmarshal(b, &applied) != nil || (applied.Kind != "" && applied.Kind != "implementation") {
		return stamp, false, errConversationCloseApplied
	}
	// An old receipt is pending even if its revision/job once passed validation.
	if applied.ReplySHA != stamp.ReplySHA || applied.SourceSHA != stamp.SourceSHA {
		return stamp, false, nil
	}
	acquired, _ := time.Parse(time.RFC3339Nano, req.Acquired)
	at, err := time.Parse(time.RFC3339Nano, applied.AppliedAt)
	if err != nil || at.Before(acquired) || at.After(r.Now()) || applied.Source != "discord-api-v10" || applied.RequestID != q.SenderID || applied.OriginTodo != q.Todo || applied.Todo != q.Todo || applied.Thread != q.Thread || applied.Question != q.Message || applied.Message != reply.Message || applied.Author != req.Author || len(applied.Revision) != 40 || strings.Trim(applied.Revision, "0123456789abcdefABCDEF") != "" || strings.TrimSpace(applied.Completion) == "" || strings.TrimSpace(applied.Verification.Execution) == "" || strings.TrimSpace(applied.Verification.Origin) == "" || strings.TrimSpace(applied.Verification.Identity) == "" {
		return stamp, false, errConversationCloseApplied
	}
	for _, raw := range row.Jobs {
		var job string
		if json.Unmarshal(raw, &job) == nil && job == applied.Job && validJob(job) {
			if !r.legacyImplementationJob(row, applied) {
				return stamp, false, errConversationCloseApplied
			}
			return stamp, true, nil
		}
	}
	return stamp, false, errConversationCloseApplied
}

func (r *Runner) conversationCloseSnapshot(ctx context.Context, q ConversationQuestion, proof *conversationProof, mark bool) (map[string]conversationCloseStamp, bool, error) {
	stamps := make(map[string]conversationCloseStamp)
	row, err := r.todo(q.Todo)
	if err != nil || row.ID != q.Todo || row.Project == "owner" || strings.TrimSpace(q.Project) == "" || row.Project != q.Project {
		return stamps, false, errConversationCloseApplied
	}
	all, dirty := len(proof.Replies) != 0, false
	previous := make(map[string]string)
	var firstErr error
	for _, reply := range proof.Replies {
		stamp, applied, err := r.conversationCloseReply(ctx, q, proof, reply, row)
		stamps[reply.Message] = stamp
		if err != nil && firstErr == nil {
			firstErr = err
		}
		if !applied {
			all = false
		} else if mark {
			phase := "conversation_reply_applied"
			if stamp.ImplementationSHA != "" {
				phase = "conversation_implementation_recorded"
			} else if stamp.DirectionSHA != "" {
				phase = "conversation_direction_recorded"
			}
			if r.conversationRequest(reply.Message).Phase == phase {
				continue
			}
			previous[reply.Message] = r.conversationRequest(reply.Message).Phase
			r.conversationRequest(reply.Message).Phase = phase
			dirty = true
		}
	}
	if dirty && r.save() != nil {
		for id, phase := range previous {
			r.conversationRequest(id).Phase = phase
		}
		return stamps, false, errConversationCloseState
	}
	return stamps, all && firstErr == nil, firstErr
}

func (r *Runner) conversationCloseThreadGET(ctx context.Context, q ConversationQuestion) (bool, error) {
	var thread conversationCloseThread
	if r.get(ctx, "/channels/"+q.Thread, &thread) != nil {
		return false, errConversationClose
	}
	d := r.State.Destination
	if thread.ID != q.Thread || thread.Guild != d.Guild || thread.Parent != d.Forum || thread.Owner != d.Bot || thread.Type != 11 || thread.Metadata.Archived == nil {
		return false, errConversationCloseSource
	}
	return *thread.Metadata.Archived, nil
}

func (r *Runner) conversationClosePATCH(ctx context.Context, q ConversationQuestion) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, strings.TrimRight(r.Base, "/")+"/channels/"+q.Thread, strings.NewReader(`{"archived":true}`))
	if err != nil {
		return errConversationClose
	}
	req.Header.Set("Authorization", "Bot "+r.Token)
	req.Header.Set("User-Agent", "DiscordBot (https://github.com/AutoDevJapan/cc-pc-tools-rs, 0.1.0)")
	req.Header.Set("Content-Type", "application/json")
	response, err := r.Client.Do(req)
	if err != nil {
		return errConversationClose
	}
	defer response.Body.Close()
	b, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil || len(b) > 4<<20 || response.StatusCode != http.StatusOK {
		return errConversationClose
	}
	return nil
}

func (r *Runner) conversationCloseFilesUnchanged(ctx context.Context, q ConversationQuestion, stamps map[string]conversationCloseStamp, checkHolds bool) (bool, error) {
	for id, stamp := range stamps {
		if stamp.ImplementationSHA != "" {
			mode := directionVerify
			if checkHolds {
				mode = directionCheckHolds
			}
			result, err := r.implementationCheck(ctx, q, id, stamp.ImplementationInputSHA, mode)
			if err != nil {
				if checkHolds {
					return false, nil
				}
				return false, err
			}
			if result.ReceiptSHA != stamp.ImplementationSHA {
				return false, nil
			}
		} else if stamp.DirectionSHA != "" {
			mode := directionVerify
			if checkHolds {
				mode = directionCheckHolds
			}
			result, err := r.conversationDirectionCheck(ctx, q, id, stamp.DirectionInputSHA, mode)
			if err != nil {
				if checkHolds {
					return false, nil
				} // First close remains pending, including a new hold.
				// A failed observation does not revoke the same GET-verified scope.
				return false, err
			}
			if result.ReceiptSHA != stamp.DirectionSHA {
				return false, nil
			}
		} else {
			applied, missing, err := conversationCloseRead(filepath.Join(r.Config.Home, "applied", id+".json"), 128<<10)
			if err != nil {
				return false, errConversationCloseState
			}
			if missing || bytesHash(applied) != stamp.AppliedSHA {
				return false, nil
			}
		}
		path, err := r.conversationSourcePath(q, id)
		if err != nil {
			return false, err
		}
		source, missing, err := conversationCloseRead(path, maxState)
		if err != nil {
			return false, errConversationCloseState
		}
		if missing || bytesHash(source) != stamp.SourceSHA {
			return false, nil
		}
	}
	return true, nil
}

// The caller holds runner.lock. Every reply is independently applied before an
// archive intent is saved; only a GET-confirmed unchanged archive returns true.
func (r *Runner) closeConversation(ctx context.Context, q ConversationQuestion, proof *conversationProof) (bool, error) {
	return r.closeConversationAuthorized(ctx, q, proof, false)
}

func (r *Runner) closeConversationAuthorized(ctx context.Context, q ConversationQuestion, proof *conversationProof, implementationClose bool) (bool, error) {
	if r.State == nil || r.Now == nil || !filepath.IsAbs(r.Config.Home) || !conversationID.MatchString(q.RequestID) || !conversationID.MatchString(q.SenderID) || q.Todo == 0 || q.Thread != q.Message {
		return false, errConversationCloseSource
	}
	for _, id := range []string{q.Thread, q.Progress, r.State.Destination.Guild, r.State.Destination.Forum, r.State.Destination.Owner, r.State.Destination.Bot} {
		if _, ok := number(id); !ok {
			return false, errConversationCloseSource
		}
	}
	for _, hash := range []string{q.Fingerprint, q.DecisionSHA, q.QuestionSHA, q.ReceiptSHA} {
		if !shaPattern.MatchString(hash) {
			return false, errConversationCloseSource
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	worker := *r
	client := http.Client{Timeout: 6 * time.Second}
	if r.Client != nil {
		client = *r.Client
	}
	if client.Timeout <= 0 || client.Timeout > 6*time.Second {
		client.Timeout = 6 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	worker.Client = &client
	initialProof, err := worker.conversationCloseProofHash(q, proof)
	if err != nil {
		return false, err
	}
	path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "close-state.json")
	record, err := worker.conversationCloseLoad(q, path)
	if err != nil {
		return false, err
	}
	stamps, all, err := worker.conversationCloseSnapshot(ctx, q, proof, true)
	if err != nil {
		return false, err
	}
	if !all {
		return false, worker.conversationCloseSave(path, record, "pending", initialProof, stamps)
	}
	refresh := func() (string, map[string]conversationCloseStamp, bool, error) {
		fresh, err := worker.conversationProof(ctx, q)
		if err != nil {
			return "", nil, false, errConversationClose
		}
		hash, err := worker.conversationCloseProofHash(q, fresh)
		if err != nil {
			return "", nil, false, err
		}
		snapshot, applied, err := worker.conversationCloseSnapshot(ctx, q, fresh, false)
		return hash, snapshot, applied, err
	}
	freshProof, freshStamps, all, err := refresh()
	if err != nil {
		return false, err
	}
	scope := canonicalHash([]any{initialProof, stamps})
	// The actor explicitly closes after its own explanation/readback. A normal
	// tick must not turn merely recording a separate implementation into close.
	for _, stamp := range stamps {
		if stamp.ImplementationSHA != "" && !implementationClose && record.VerifiedScope != scope {
			return false, worker.conversationCloseSave(path, record, "pending", initialProof, stamps)
		}
	}
	if !all || freshProof != initialProof || canonicalHash(freshStamps) != canonicalHash(stamps) {
		return false, worker.conversationCloseSave(path, record, "pending", freshProof, freshStamps)
	}
	// New holds govern first close of this exact proof/receipt set. Later work
	// cannot undo an already verified archive; current replies/API/record hashes
	// still have to match. A new or edited reply starts a new close verification.
	checkHolds := record.VerifiedAt == "" || record.VerifiedScope != scope
	archived, err := worker.conversationCloseThreadGET(ctx, q)
	if err != nil {
		return false, err
	}
	if record.VerifiedAt != "" {
		if !archived {
			return false, worker.conversationCloseSave(path, record, "reopened", initialProof, stamps)
		}
	}
	unchanged, err := worker.conversationCloseFilesUnchanged(ctx, q, stamps, checkHolds)
	if err != nil {
		return false, err
	}
	if !unchanged {
		return false, worker.conversationCloseSave(path, record, "pending", initialProof, stamps)
	}
	if !archived {
		if ctx.Err() != nil {
			return false, errConversationClose
		}
		record.PatchAttempts++
		if worker.conversationCloseSave(path, record, "patch_pending", initialProof, stamps) != nil {
			return false, errConversationCloseState
		}
		if err := worker.conversationClosePATCH(ctx, q); err != nil {
			return false, err
		}
	}
	archived, err = worker.conversationCloseThreadGET(ctx, q)
	if err != nil {
		return false, err
	}
	if !archived {
		return false, worker.conversationCloseSave(path, record, "pending", initialProof, stamps)
	}
	freshProof, freshStamps, all, err = refresh()
	if err != nil {
		return false, err
	}
	if !all || freshProof != initialProof || canonicalHash(freshStamps) != canonicalHash(stamps) {
		return false, worker.conversationCloseSave(path, record, "pending", freshProof, freshStamps)
	}
	archived, err = worker.conversationCloseThreadGET(ctx, q)
	if err != nil {
		return false, err
	}
	if !archived {
		return false, worker.conversationCloseSave(path, record, "pending", initialProof, stamps)
	}
	unchanged, err = worker.conversationCloseFilesUnchanged(ctx, q, stamps, checkHolds)
	if err != nil {
		return false, err
	}
	if !unchanged {
		return false, worker.conversationCloseSave(path, record, "pending", initialProof, stamps)
	}
	if record.VerifiedAt == "" {
		record.VerifiedAt = r.Now().UTC().Format(time.RFC3339Nano)
	}
	if ctx.Err() != nil {
		return false, errConversationClose
	}
	record.VerifiedScope = scope
	if err := worker.conversationCloseSave(path, record, "verified", initialProof, stamps); err != nil {
		return false, err
	}
	return true, nil
}
