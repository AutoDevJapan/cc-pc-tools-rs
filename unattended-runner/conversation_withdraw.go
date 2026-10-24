package main

// Withdrawal is an operator correction, never an owner reply or implementation.
// The caller holds runner.lock. All artifacts live in the new conversation home.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

const conversationWithdrawalDomain = "discord-conversation-withdrawal-v1"
const todoWithdrawalDomain = "todo-question-withdrawal-receipt-v1"

var errConversationWithdraw = errors.New("conversation withdrawal deferred")
var errConversationWithdrawReply = errors.New("conversation owner reply requires normal delivery")

type conversationWithdrawal struct {
	Domain    string               `json:"domain"`
	Question  ConversationQuestion `json:"question"`
	ReasonSHA string               `json:"reason_sha256"`
	OriginSHA string               `json:"expected_state_sha256"`
	ProofSHA  string               `json:"proof_sha256"`
	Phase     string               `json:"phase"`
}

func (r *Runner) withdrawalRow(id uint64) (json.RawMessage, error) {
	var db struct {
		Items []json.RawMessage `json:"items"`
	}
	if readJSON(r.TodoPath, maxTodo, &db) != nil {
		return nil, errConversationWithdraw
	}
	var found json.RawMessage
	for _, raw := range db.Items {
		var row struct {
			ID uint64 `json:"id"`
		}
		if json.Unmarshal(raw, &row) != nil {
			return nil, errConversationWithdraw
		}
		if row.ID == id {
			if found != nil {
				return nil, errConversationWithdraw
			}
			found = raw
		}
	}
	if found == nil {
		return nil, errConversationWithdraw
	}
	return found, nil
}

// Only the exact canonical Rust receipt proves persistence. Subprocess output,
// a different request's receipt, or a technical rejection cannot prove release.
func (r *Runner) withdrawalRecorded(q ConversationQuestion, record *conversationWithdrawal) (bool, error) {
	raw, err := r.withdrawalRow(q.Todo)
	if err != nil {
		return false, err
	}
	var row struct {
		Project  string `json:"project"`
		Receipts []struct {
			Domain    string          `json:"domain"`
			Reason    string          `json:"reason"`
			ReasonSHA string          `json:"reason_sha256"`
			OriginSHA string          `json:"expected_state_sha256"`
			Origin    json.RawMessage `json:"origin"`
			Applied   string          `json:"applied_at"`
			Wait      struct {
				SHA      string                      `json:"question_receipt_sha256"`
				Question conversationQuestionReceipt `json:"question"`
			} `json:"wait"`
		} `json:"owner_question_withdrawal_receipts"`
	}
	if json.Unmarshal(raw, &row) != nil || row.Project != q.Project {
		return false, errConversationWithdraw
	}
	b, err := boundedRegular(q.ReceiptFile)
	var expected conversationQuestionReceipt
	if err != nil || bytesHash(b) != q.ReceiptSHA || json.Unmarshal(b, &expected) != nil {
		return false, errConversationWithdraw
	}
	originBytes, err := boundedRegular(filepath.Join(r.Config.Home, "conversations", q.RequestID, "withdrawal-origin.json"))
	var origin struct {
		Generation json.RawMessage `json:"resume_generation"`
		Worker     json.RawMessage `json:"worker_receipt"`
		Scheduler  struct {
			Job string `json:"job"`
			Key string `json:"key"`
		} `json:"scheduler"`
	}
	if err != nil || bytesHash(originBytes) != record.OriginSHA || json.Unmarshal(originBytes, &origin) != nil {
		return false, errConversationWithdraw
	}
	originSHA := canonicalHash(map[string]any{"resume_generation": origin.Generation, "worker_receipt": origin.Worker, "job": origin.Scheduler.Job, "launch_key": origin.Scheduler.Key})
	matched := 0
	for _, receipt := range row.Receipts {
		if receipt.Wait.Question.RequestID != q.SenderID {
			continue
		}
		applied, timeErr := time.Parse(time.RFC3339Nano, receipt.Applied)
		created, createErr := time.Parse(time.RFC3339Nano, expected.Created)
		if receipt.Domain != todoWithdrawalDomain || receipt.ReasonSHA != record.ReasonSHA || bytesHash([]byte(receipt.Reason)) != record.ReasonSHA || receipt.OriginSHA != record.OriginSHA || canonicalHash(receipt.Origin) != originSHA || timeErr != nil || createErr != nil || applied.Before(created) || applied.After(r.Now()) || receipt.Wait.SHA != q.ReceiptSHA || receipt.Wait.Question != expected {
			return false, errConversationWithdraw
		}
		matched++
	}
	if matched > 1 {
		return false, errConversationWithdraw
	}
	return matched == 1, nil
}

func (r *Runner) withdrawalProof(ctx context.Context, q ConversationQuestion) (string, bool, error) {
	proof, err := r.conversationProof(ctx, q)
	if err != nil {
		return "", false, err
	}
	if len(proof.Replies) != 0 {
		return "", true, nil
	}
	for _, req := range r.conversationRequests() {
		if req != nil && (req.Conversation == q.RequestID || (req.Thread == q.Thread && req.Author == r.State.Destination.Owner)) {
			// Even an already captured or edited reply cannot be reclassified as
			// an unanswered question if it disappeared from the current projection.
			return "", true, nil
		}
	}
	var live Message
	if r.get(ctx, "/channels/"+q.Thread+"/messages/"+q.Message, &live) != nil || live.ID != q.Message || live.Channel != q.Thread || live.Author.ID != r.State.Destination.Bot || !live.Author.Bot || live.Timestamp != proof.Created {
		return "", false, errConversationWithdraw
	}
	if _, err = r.conversationCloseThreadGET(ctx, q); err != nil {
		return "", false, err
	}
	progress := q
	progress.Thread = q.Progress
	if _, err = r.conversationCloseThreadGET(ctx, progress); err != nil {
		return "", false, err
	}
	return canonicalHash([]any{proof, live}), false, nil
}

func (r *Runner) conversationWithdraw(ctx context.Context, requestID, reasonFile string) (map[string]any, error) {
	if !conversationID.MatchString(requestID) {
		return nil, errConversationWithdraw
	}
	index, err := r.loadConversations()
	if err != nil {
		return nil, err
	}
	q, exists := index.Questions[requestID]
	if _, alias := index.Aliases[requestID]; alias || !exists || q.RequestID != requestID || q.Thread == q.Progress {
		return nil, errConversationWithdraw
	}
	for key, other := range index.Questions {
		if key != requestID && (other.SenderID == q.SenderID || other.Thread == q.Thread) {
			return nil, errConversationWithdraw
		}
	}
	reason, err := boundedRegular(reasonFile)
	if err != nil || len(reason) > 16<<10 || !utf8.Valid(reason) || strings.TrimSpace(string(reason)) == "" || bytes.IndexByte(reason, 0) >= 0 || safeText(string(reason), r.Token) != string(reason) {
		return nil, errConversationWithdraw
	}
	dir := filepath.Join(r.Config.Home, "conversations", q.RequestID)
	statePath, originPath, savedReason := filepath.Join(dir, "withdrawal-state.json"), filepath.Join(dir, "withdrawal-origin.json"), filepath.Join(dir, "withdrawal-reason.txt")
	// Use a bounded no-redirect client for every identity read and archive PATCH.
	worker := *r
	client := http.Client{Timeout: 6 * time.Second}
	if r.Client != nil {
		client = *r.Client
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if client.Timeout <= 0 || client.Timeout > 6*time.Second {
		client.Timeout = 6 * time.Second
	}
	worker.Client = &client

	data, missing, err := conversationCloseRead(statePath, 128<<10)
	if err != nil {
		return nil, errConversationWithdraw
	}
	record := &conversationWithdrawal{}
	if !missing {
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(record) != nil || decoder.Decode(&struct{}{}) != io.EOF || record.Domain != conversationWithdrawalDomain || record.Question.RequestID != requestID || record.ReasonSHA != bytesHash(reason) || !shaPattern.MatchString(record.OriginSHA) || !shaPattern.MatchString(record.ProofSHA) {
			return nil, errConversationWithdraw
		}
		// Phase and Wait are delivery state; the original identity stays immutable.
		original, current := record.Question, q
		original.Phase, current.Phase = "", ""
		original.Wait, current.Wait = false, false
		original.ReplyCursor, current.ReplyCursor = "", ""
		if !reflect.DeepEqual(original, current) {
			return nil, errConversationWithdraw
		}
		switch record.Phase {
		case "prepared", "saved", "notified", "patch_pending", "archived":
		default:
			return nil, errConversationWithdraw
		}
	} else if !q.Wait || q.Phase != "waiting" {
		return nil, errConversationWithdraw
	}
	validateFiles := func() error {
		for path, expected := range map[string]string{originPath: record.OriginSHA, savedReason: record.ReasonSHA, q.ReceiptFile: q.ReceiptSHA} {
			b, err := boundedRegular(path)
			if err != nil || bytesHash(b) != expected {
				return errConversationWithdraw
			}
		}
		return nil
	}
	savePhase := func(phase string) error { record.Phase = phase; return publish(statePath, record) }
	rememberSaved := func() error {
		if record.Phase == "prepared" && savePhase("saved") != nil {
			return errConversationWithdraw
		}
		// Preserve the original wait=true in the immutable intent and canonical
		// receipt. Future real replies use ordinary delivery to the same task.
		if q.Wait {
			q.Wait, q.Phase = false, "withdrawn"
			index.Questions[q.RequestID] = q
			if publish(r.conversationPath(), index) != nil {
				return errConversationWithdraw
			}
		}
		return nil
	}
	row, err := worker.conversationTask(ctx, q.Todo)
	if err != nil || row.Project != q.Project {
		return nil, errConversationWithdraw
	}
	scopeFile, err := worker.conversationScopeFile(row, q)
	if err != nil {
		return nil, errConversationWithdraw
	}
	proofSHA, replied, err := worker.withdrawalProof(ctx, q)
	if err != nil {
		return nil, err
	}
	if replied {
		// A lost canonical response may precede this real reply. Reconcile the
		// saved withdrawal first, so normal delivery cannot try to answer a
		// wait which was already retired. No reply/application receipt is made.
		if !missing {
			if err = validateFiles(); err != nil {
				return nil, err
			}
			saved, err := r.withdrawalRecorded(q, record)
			if err != nil {
				return nil, err
			}
			if saved && rememberSaved() != nil {
				return nil, errConversationWithdraw
			}
			if !saved && record.Phase != "prepared" {
				return nil, errConversationWithdraw
			}
		}
		return map[string]any{"request_id": requestID, "withdrawn": false, "owner_reply_pending": true}, nil
	}
	if missing {
		// Do not freeze an obsolete row while the first Discord proof is in
		// flight. Recheck the real guard and capture the immutable intent now;
		// later changes still fail the canonical locked comparison.
		row, err = worker.conversationTask(ctx, q.Todo)
		if err != nil || row.Project != q.Project {
			return nil, errConversationWithdraw
		}
		scopeFile, err = worker.conversationScopeFile(row, q)
		if err != nil {
			return nil, errConversationWithdraw
		}
		b, err := boundedRegular(q.ReceiptFile)
		var receipt conversationQuestionReceipt
		// Rust is the locked authority for note provenance, including the exact
		// same-owner scheduler import which replaces the original marker line.
		if err != nil || bytesHash(b) != q.ReceiptSHA || json.Unmarshal(b, &receipt) != nil || receipt.Domain != "discord-conversation-question-v1" || receipt.RequestID != q.SenderID || receipt.Todo != fmt.Sprint(q.Todo) || receipt.Guild != r.State.Destination.Guild || receipt.Owner != r.State.Destination.Owner || receipt.Bot != r.State.Destination.Bot || receipt.Thread != q.Thread || receipt.Message != q.Message || receipt.Progress != q.Progress || receipt.Fingerprint != q.Fingerprint || row.Status != "block" || !conversationWaitRecorded(row, q) {
			return nil, errConversationWithdraw
		}
		origin := row.conversationState
		if writeConversationOnce(originPath, origin) != nil || writeConversationOnce(savedReason, reason) != nil {
			return nil, errConversationWithdraw
		}
		record = &conversationWithdrawal{Domain: conversationWithdrawalDomain, Question: q, ReasonSHA: bytesHash(reason), OriginSHA: bytesHash(origin), ProofSHA: proofSHA, Phase: "prepared"}
		if publish(statePath, record) != nil {
			return nil, errConversationWithdraw
		}
	}
	if err = validateFiles(); err != nil {
		return nil, err
	}
	if proofSHA != record.ProofSHA {
		return nil, errConversationWithdraw
	}
	refresh := func() error {
		sha, replied, err := worker.withdrawalProof(ctx, q)
		if err != nil {
			return err
		}
		if replied {
			return errConversationWithdrawReply
		}
		if sha != record.ProofSHA {
			return errConversationWithdraw
		}
		return nil
	}
	if err = refresh(); err != nil {
		return nil, err
	}
	saved, err := r.withdrawalRecorded(q, record)
	if err != nil {
		return nil, err
	}
	if !saved {
		if record.Phase != "prepared" {
			return nil, errConversationWithdraw
		}
		if _, err = r.Run(ctx, conversationTodo, "question-withdraw", fmt.Sprint(q.Todo), "--question-file", q.ReceiptFile, "--reason-file", savedReason, "--expected-state-file", originPath, "--scope-file", scopeFile); err != nil {
			return nil, errConversationWithdraw
		}
		saved, err = r.withdrawalRecorded(q, record)
		if err != nil || !saved {
			return nil, errConversationWithdraw
		}
	}
	if err = rememberSaved(); err != nil {
		return nil, err
	}
	if err = refresh(); err != nil {
		return nil, err
	}
	if record.Phase == "saved" {
		body := []byte("既存の依頼と許可で実行できる通常作業の再確認だったため、質問を取り下げました。本人回答としては記録せず、元の作業を続けます。履行の完了は別途確認します。\n理由: " + string(reason))
		bodyPath := filepath.Join(dir, "withdrawal-progress.txt")
		if writeConversationOnce(bodyPath, body) != nil {
			return nil, errConversationWithdraw
		}
		logID := "withdraw-" + bytesHash([]byte(q.RequestID))[:32]
		if _, err = worker.conversationLog(ctx, logID, "質問の取り下げ", bodyPath, q.Progress, false, nil); err != nil {
			return nil, err
		}
		if savePhase("notified") != nil {
			return nil, errConversationWithdraw
		}
	}
	if err = refresh(); err != nil {
		return nil, err
	}
	archived, err := worker.conversationCloseThreadGET(ctx, q)
	if err != nil {
		return nil, err
	}
	if record.Phase == "archived" && !archived {
		return nil, errConversationWithdraw
	}
	if !archived {
		if savePhase("patch_pending") != nil {
			return nil, errConversationWithdraw
		}
		if err = worker.conversationClosePATCH(ctx, q); err != nil {
			return nil, err
		}
	}
	archived, err = worker.conversationCloseThreadGET(ctx, q)
	if err != nil || !archived {
		return nil, errConversationWithdraw
	}
	if err = refresh(); err != nil {
		return nil, err
	}
	if savePhase("archived") != nil {
		return nil, errConversationWithdraw
	}
	return map[string]any{"domain": conversationWithdrawalDomain, "request_id": q.RequestID, "todo_id": q.Todo, "withdrawn": true, "answered": false, "archived": true, "fulfilled": false}, nil
}
