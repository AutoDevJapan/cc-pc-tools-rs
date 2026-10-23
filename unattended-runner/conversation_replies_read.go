package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func conversationRepliesOutput(ctx context.Context, path string, guard func(context.Context, string) bool) bool {
	if !historyPlainParent(path) || !strings.HasSuffix(path, ".private.json") || strings.Contains(filepath.Base(path), ":") || !guard(ctx, filepath.Dir(path)) {
		return false
	}
	_, err := os.Lstat(path)
	return os.IsNotExist(err)
}

// This is a fresh API inspection, not a canonical per-reply application source.
// Only its new private output is written; the existing cache is left intact.
func (r *Runner) conversationRepliesSnapshot(ctx context.Context, requestID, output string) (map[string]any, error) {
	bad := errors.New("conversation inspection unavailable")
	if !conversationID.MatchString(requestID) {
		return nil, bad
	}
	index, err := r.loadConversations()
	if err != nil {
		return nil, bad
	}
	q, ok := index.Questions[requestID]
	if !ok || q.Phase == "reserved" {
		return nil, bad
	}
	questionSHA := canonicalHash(q)
	proof, err := r.conversationProof(ctx, q)
	if err != nil || ctx.Err() != nil || len(proof.Replies) > 100 || safeText(proof.Body, r.Token) != proof.Body {
		return nil, bad
	}
	versions := make([]map[string]any, 0, len(proof.Replies))
	sources := make([]map[string]any, 0, len(proof.Replies))
	ordinary, other, unknown := 0, 0, 0
	for _, reply := range proof.Replies {
		if safeText(reply.Body, r.Token) != reply.Body {
			return nil, bad
		}
		var live historyReadMessage
		if r.get(ctx, "/channels/"+q.Thread+"/messages/"+reply.Message, &live) != nil || !historyMessageMetadataValid(live.Message, q.Thread, r) || live.ID != reply.Message || live.Author.ID != reply.Author || live.Content != reply.Body || (live.Reference.Channel != "" && live.Reference.Channel != q.Thread) || (live.Reference.Guild != "" && live.Reference.Guild != r.State.Destination.Guild) {
			return nil, bad
		}
		created, err := time.Parse(time.RFC3339Nano, reply.Created)
		if err != nil || !mustHistoryTime(live.Timestamp).Equal(created) {
			return nil, bad
		}
		if live.Type == nil {
			unknown++
		} else if *live.Type == 0 || *live.Type == 19 {
			ordinary++
		} else {
			other++
		}
		version := map[string]any{"message_id": reply.Message, "timestamp": live.Timestamp, "edited_timestamp": live.Edited, "message_type": live.Type, "content_sha256": bytesHash([]byte(live.Content))}
		// An inspection file's SHA is never a canonical application source SHA.
		// Expose the latter only when the actual delivered capture also matches
		// this fresh question/reply/edit. An absent capture remains unverified.
		if stamp, ready, err := r.conversationReplySourceMessage(ctx, q, proof, reply, &live.Message); err == nil && ready {
			version["source_receipt_sha256"] = stamp.SourceSHA
			sources = append(sources, map[string]any{"message_id": reply.Message, "reply_sha256": stamp.ReplySHA, "source_receipt_sha256": stamp.SourceSHA})
		}
		versions = append(versions, version)
	}
	current, err := r.loadConversations()
	if err != nil || canonicalHash(current.Questions[requestID]) != questionSHA {
		return nil, bad
	}
	inspection := map[string]any{"request_id": q.RequestID, "todo_id": q.Todo, "question_phase": q.Phase, "question_snapshot_sha256": questionSHA, "acquired_at": r.Now().UTC().Format(time.RFC3339Nano), "delivery": "inspection_only", "application": "not_inferred", "snapshot_consistency": "question_rechecked_independent_files", "owner_reply_versions": versions}
	receipt := struct {
		*conversationProof
		Inspection map[string]any `json:"inspection"`
	}{proof, inspection}
	b, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil || len(b) > 4<<20 || safeText(string(b), r.Token) != string(b) || ctx.Err() != nil || writeHistoryReceipt(output, b) != nil {
		return nil, bad
	}
	return map[string]any{"domain": conversationDomain, "source": "discord-api-v10", "read_only": true, "request_id": q.RequestID, "todo_id": q.Todo, "replies_verified": len(proof.Replies), "owner_messages": ordinary, "owner_other_types": other, "owner_unknown_types": unknown, "question_phase": q.Phase, "question_snapshot_sha256": questionSHA, "receipt_sha256": bytesHash(b), "delivery": "inspection_only", "application": "not_inferred", "verified_source_receipts": sources}, nil
}
