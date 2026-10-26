package main

// A qualified source reference is not a destination permission. This command
// never sends, opens a lock, updates a receipt, or reads the legacy ledger.
import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"path/filepath"
	"strings"
)

type conversationOriginContract struct{ RequestID, SenderID, Origin, InputSHA string }

var fixedConversationOrigin = conversationOriginContract{
	RequestID: "normal-ask-9620ba9f12adf45e1a01ee67010dafe616750231a1625ffcca0c7ed470a6a862",
	SenderID:  "conversation-v1-c5fd3cd83bfb64d7b04e0701b4e178ad",
	Origin:    "1556320676015640587",
	InputSHA:  "486a3210dca7e8110ff2cf3fb94ff0c1b2bd06ba99a344a6a8be54f800b7803c",
}

type conversationOriginReferenceProof struct {
	Domain      string `json:"domain"`
	Verified    bool   `json:"verified"`
	NormalID    string `json:"normal_request_id"`
	SenderID    string `json:"sender_request_id"`
	Todo        uint64 `json:"todo_id"`
	Actor       uint64 `json:"actor_todo_id"`
	Job         string `json:"actor_job_id"`
	Origin      string `json:"origin_thread_id"`
	InputSHA    string `json:"input_sha256"`
	BodySHA     string `json:"body_sha256"`
	DecisionSHA string `json:"decision_sha256"`
	Title       string `json:"title"`
	SourceSHA   string `json:"succession_sha256"`
	Guild       string `json:"guild_id"`
	Forum       string `json:"forum_id"`
	Owner       string `json:"owner_id"`
	Bot         string `json:"bot_id"`
}

func (r *Runner) conversationOriginReference(ctx context.Context, requestID string, bound conversationOriginContract) (conversationOriginReferenceProof, error) {
	var zero conversationOriginReferenceProof
	fail := errors.New("conversation source reference unverified")
	if requestID != bound.RequestID {
		return zero, fail
	}
	ix, err := r.loadConversations()
	if err != nil {
		return zero, fail
	}
	q, ok := ix.Questions[requestID]
	if !ok || q.RequestID != bound.RequestID || q.SenderID != bound.SenderID || q.OriginThread != bound.Origin || q.InputSHA != bound.InputSHA || q.Phase != "reserved" || q.Todo != 1171 || q.Actor == nil || q.Actor.Todo != 3277 || q.Project != "GitDo" || len(q.Attachments) != 0 || len(q.AttachmentHashes) != 0 {
		return zero, fail
	}
	if _, err = r.conversationActorRow(ctx, q); err != nil {
		return zero, fail
	}
	if _, err = r.conversationActorProof(q); err != nil {
		return zero, fail
	}
	dir := filepath.Join(r.Config.Home, "conversations", requestID)
	if conversationPathSHA(q.BodyFile) != conversationPathSHA(filepath.Join(dir, "question.txt")) || conversationPathSHA(q.DecisionFile) != conversationPathSHA(filepath.Join(dir, "decision.json")) {
		return zero, fail
	}
	body, err := boundedRegular(q.BodyFile)
	if err != nil {
		return zero, fail
	}
	decision, err := boundedRegular(q.DecisionFile)
	if err != nil {
		return zero, fail
	}
	var identity map[string]json.RawMessage
	var todo uint64
	if json.Unmarshal(decision, &identity) != nil || json.Unmarshal(identity["todo_id"], &todo) != nil || todo != 1171 || canonicalHash([]any{q.Todo, q.OriginThread, bytesHash(body), canonicalHash(json.RawMessage(decision)), q.Title, q.Wait}) != bound.InputSHA {
		return zero, fail
	}
	return conversationOriginReferenceProof{Domain: "discord-conversation-origin-reference-v1", Verified: true, NormalID: q.RequestID, SenderID: q.SenderID, Todo: q.Todo, Actor: q.Actor.Todo, Job: q.Actor.Job, Origin: q.OriginThread, InputSHA: q.InputSHA, BodySHA: bytesHash([]byte(strings.TrimPrefix(string(body), "\ufeff"))), DecisionSHA: canonicalHash(json.RawMessage(decision)), Title: q.Title, SourceSHA: q.Actor.SourceSHA, Guild: r.State.Destination.Guild, Forum: r.State.Destination.Forum, Owner: r.State.Destination.Owner, Bot: r.State.Destination.Bot}, nil
}

func conversationOriginReferenceCLI(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("check-conversation-origin-reference", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var requestID string
	conversationStringFlag(fs, "request-id", &requestID)
	if fs.Parse(args) != nil || fs.NArg() != 0 || requestID != fixedConversationOrigin.RequestID {
		_, _ = io.WriteString(out, "conversation source reference unverified\n")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), conversationCLIDeadline)
	defer cancel()
	backend, release, err := loadConversationStatusRunner(conversationDefaultConfig)
	if err == nil {
		defer release()
		if runner, ok := backend.(*Runner); ok {
			if proof, err := runner.conversationOriginReference(ctx, requestID, fixedConversationOrigin); err == nil {
				_ = json.NewEncoder(out).Encode(proof)
				return 0
			}
		}
	}
	_, _ = io.WriteString(out, "conversation source reference unverified\n")
	return 1
}

// This finite protocol is read only for a failed canonical recovery-ask child.
// Arbitrary stdout/stderr and unknown codes never become public diagnostics.
func conversationSenderFailure(raw string) error {
	var failure struct {
		Domain   string `json:"domain"`
		Delivery string `json:"delivery"`
		Stage    string `json:"failure_stage"`
	}
	if json.Unmarshal([]byte(raw), &failure) == nil && failure.Domain == "new-owner-recovery-messages" && failure.Delivery == "not_verified" {
		switch failure.Stage {
		case "origin_scope", "source_scope", "busy", "network", "rate_limited", "operation_pending":
			return errors.New("conversation sender " + failure.Stage)
		}
	}
	return nil
}
