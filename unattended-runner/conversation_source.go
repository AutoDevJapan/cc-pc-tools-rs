package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"time"
)

// Generations belong to the same canonical source namespace. The original
// message.json is never overwritten by this purpose-bound acquisition.
func (r *Runner) conversationSourcePath(q ConversationQuestion, id string) (string, error) {
	req := r.conversationRequest(id)
	if req != nil && req.ConversationSourceGeneration != "" {
		if !shaPattern.MatchString(req.ConversationSourceGeneration) {
			return "", errConversationCloseSource
		}
		return filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", id+".sources", req.ConversationSourceGeneration+".json"), nil
	}
	return filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", id+".json"), nil
}
func (r *Runner) conversationSourceReacquire(ctx context.Context, q ConversationQuestion, proof *conversationProof, reply conversationReply) (map[string]any, error) {
	if stamp, ready, err := r.conversationReplySource(ctx, q, proof, reply); err == nil && ready {
		return map[string]any{"domain": "discord-conversation-source-generation-v1", "reacquired": false, "canonical_current": true, "source_receipt_sha256": stamp.SourceSHA, "reply_sha256": stamp.ReplySHA}, nil
	}
	var live Message
	if r.get(ctx, "/channels/"+q.Thread+"/messages/"+reply.Message, &live) != nil || live.ID != reply.Message || live.Channel != q.Thread || live.Author.ID != r.State.Destination.Owner || live.Author.Bot || live.Timestamp != reply.Created || live.Content != reply.Body || (live.Reference.Channel != "" && live.Reference.Channel != q.Thread) || (live.Reference.Guild != "" && live.Reference.Guild != r.State.Destination.Guild) {
		return nil, errImplementation
	}
	now := r.Now()
	created, err := time.Parse(time.RFC3339Nano, reply.Created)
	if err != nil || created.After(now) {
		return nil, errImplementation
	}
	if live.Edited != "" {
		at, err := time.Parse(time.RFC3339Nano, live.Edited)
		if err != nil || at.Before(created) || at.After(now) {
			return nil, errImplementation
		}
	}
	old := r.conversationRequest(reply.Message)
	if old != nil && (old.Conversation != q.RequestID || old.Thread != q.Thread || old.Message != reply.Message || old.Author != reply.Author || (old.Todo != 0 && old.Todo != q.Todo)) {
		return nil, errImplementation
	}
	// Record the old intake separately; neither its hash nor an inspection hash
	// is the canonical source hash of this fresh API acquisition.
	priorIntakeSHA := ""
	if old != nil {
		b, _ := json.Marshal(old)
		priorIntakeSHA = bytesHash(b)
		if safeText(string(b), r.Token) != string(b) || writeConversationOnce(filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".sources", "intake-"+bytesHash(b)+".json"), b) != nil {
			return nil, errImplementation
		}
	}
	original := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".json")
	prior, missing, err := conversationCloseRead(original, maxState)
	if err != nil {
		return nil, errImplementation
	}
	body, _ := json.Marshal(proof)
	req := &Request{Thread: q.Thread, Guild: r.State.Destination.Guild, RecordVersion: 2, Message: reply.Message, Author: reply.Author, Created: reply.Created, Edited: live.Edited, ReplyTo: live.Reference.Message, ReplyChannel: live.Reference.Channel, ReplyGuild: live.Reference.Guild, Acquired: now.UTC().Format(time.RFC3339Nano), Source: "discord-api-v10", Text: reply.Body, Context: string(body), Key: requestKey(q.Thread, reply.Message), Phase: "conversation_source_verified", ProgressThread: q.Progress, Conversation: q.RequestID}
	if old != nil {
		req.ConversationIntakeLinkSHA = old.ConversationIntakeLinkSHA
	}
	b, _ := json.Marshal(req)
	if safeText(string(b), r.Token) != string(b) {
		return nil, errImplementation
	}
	sourceSHA := bytesHash(b)
	path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".sources", sourceSHA+".json")
	if writeConversationOnce(path, b) != nil {
		return nil, errImplementation
	}
	req.ConversationSourceGeneration = sourceSHA
	if r.saveConversationCapture(req) != nil {
		return nil, errImplementation
	}
	stamp, ready, err := r.conversationReplySource(ctx, q, proof, reply)
	if err != nil || !ready || stamp.SourceSHA != sourceSHA {
		return nil, errImplementation
	}
	result := map[string]any{"domain": "discord-conversation-source-generation-v1", "reacquired": true, "canonical_current": true, "source_receipt_sha256": sourceSHA, "reply_sha256": stamp.ReplySHA, "original_source_exists": !missing, "original_source_overwritten": false, "delivery_inferred": false, "application_inferred": false}
	if !missing {
		result["retained_original_source_sha256"] = bytesHash(prior)
	}
	if priorIntakeSHA != "" {
		result["retained_intake_sha256"] = priorIntakeSHA
	}
	return result, nil
}
