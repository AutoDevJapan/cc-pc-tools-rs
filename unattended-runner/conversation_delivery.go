package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type conversationAnswerReceipt struct {
	Domain      string `json:"domain"`
	RequestID   string `json:"request_id"`
	Todo        string `json:"todo_id"`
	Thread      string `json:"thread_id"`
	Question    string `json:"question_message_id"`
	Fingerprint string `json:"question_fingerprint"`
	QuestionSHA string `json:"question_receipt_sha256"`
	Message     string `json:"message_id"`
	Author      string `json:"author_id"`
	Source      string `json:"source"`
	ReplySHA    string `json:"reply_sha256"`
	Acquired    string `json:"acquired_at"`
	ReceiptSHA  string `json:"receipt_file_sha256"`
}

func (r *Runner) captureConversationReply(ctx context.Context, q ConversationQuestion, proof *conversationProof, reply conversationReply) (*Request, error) {
	// Read the exact live owner post as well as the sender's question proof. This
	// preserves edit/reference metadata which the sender's reply projection omits.
	var live Message
	if err := r.get(ctx, "/channels/"+q.Thread+"/messages/"+reply.Message, &live); err != nil {
		return nil, err
	}
	if live.ID != reply.Message || live.Channel != q.Thread || live.Author.ID != r.State.Destination.Owner || live.Author.Bot || live.Timestamp != reply.Created || live.Content != reply.Body || (live.Reference.Channel != "" && live.Reference.Channel != q.Thread) || (live.Reference.Guild != "" && live.Reference.Guild != r.State.Destination.Guild) {
		return nil, errors.New("conversation live owner source differs")
	}
	if safeText(reply.Body, r.Token) != reply.Body {
		return nil, errors.New("conversation owner receipt contains credential material")
	}
	acquiredAt := r.Now()
	created, timeErr := time.Parse(time.RFC3339Nano, live.Timestamp)
	if timeErr != nil || created.After(acquiredAt) {
		// Do not freeze an acquisition before Discord's creation timestamp.
		// The next bounded tick can acquire it after the local clock catches up.
		return nil, errors.New("conversation owner source clock deferred")
	}
	if live.Edited != "" {
		edited, err := time.Parse(time.RFC3339Nano, live.Edited)
		if err != nil || edited.Before(created) || edited.After(acquiredAt) {
			return nil, errors.New("conversation owner edit clock deferred")
		}
	}
	old := r.State.Requests[reply.Message]
	if old == nil || (old.Conversation == q.RequestID && old.Todo == 0 && old.Phase == "conversation_capture_pending") {
		path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".json")
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			b, readErr := boundedRegular(path)
			var saved Request
			var savedProof conversationProof
			if readErr != nil || json.Unmarshal(b, &saved) != nil || json.Unmarshal([]byte(saved.Context), &savedProof) != nil || saved.RecordVersion != 2 || saved.Source != "discord-api-v10" || saved.Conversation != q.RequestID || saved.Thread != q.Thread || saved.Guild != r.State.Destination.Guild || saved.Author != reply.Author || saved.Message != reply.Message || saved.Created != reply.Created || saved.Key != requestKey(q.Thread, reply.Message) || (saved.Todo != 0 && saved.Todo != q.Todo) || savedProof.RequestID != q.SenderID || savedProof.Thread != q.Thread || savedProof.Question != q.Message || savedProof.Owner != r.State.Destination.Owner || savedProof.Source != "discord-api-v10" || canonicalHash(savedProof.Decision) != q.DecisionSHA || bytesHash([]byte(savedProof.Body)) != q.QuestionSHA {
				return nil, errors.New("conversation saved owner source differs")
			}
			// Restore the original private source without overwriting its bytes or
			// trusting an intake phase as canonical delivery/application evidence.
			saved.Phase, saved.Reported = "conversation_reply_saved", ""
			// Every delivery writes this version's marker before mutating the
			// ToDo. A prior version's input is not an attempt for a captured edit.
			saved.ConversationSourceRecovered = false
			notePath := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".note.txt")
			if _, err := os.Lstat(notePath); !os.IsNotExist(err) {
				priorNote, err := boundedRegular(notePath)
				if err != nil {
					return nil, errors.New("conversation prior delivery evidence unavailable")
				}
				saved.ConversationSourceRecovered = hasNoteMarker(string(priorNote), conversationReplyMarker(q, &saved))
			}
			old = &saved
			r.State.Requests[reply.Message] = old
		}
	}
	if old != nil && (old.Thread != q.Thread || old.Author != reply.Author || old.Message != reply.Message || (old.Todo != 0 && old.Conversation != q.RequestID)) {
		return nil, errors.New("conversation existing receipt association differs")
	}
	if old != nil && old.Conversation == q.RequestID && (old.Text != reply.Body || old.Edited != live.Edited) {
		// Preserve the previous delivery, then hand the actual edited owner text
		// to the same task. Edits never inherit a prior implementation receipt.
		previous, _ := json.Marshal(old)
		path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "edits", reply.Message+"-"+bytesHash(previous)[:16]+".json")
		if err := writeConversationOnce(path, previous); err != nil {
			return nil, err
		}
		old.PriorVersions = append(old.PriorVersions, old.Text)
		old.PriorEdits = append(old.PriorEdits, EditVersion{Edited: old.Edited, SHA256: bytesHash([]byte(old.Text))})
		old.Text = reply.Body
		old.Edited = live.Edited
		old.ProgressThread = q.Progress
		old.Acquired = acquiredAt.UTC().Format(time.RFC3339Nano)
		contextBytes, _ := json.Marshal(proof)
		old.Context = string(contextBytes)
		old.Phase = "conversation_reply_saved"
		old.ConversationSourceRecovered = false
		old.ConversationRecoveryNote = ""
		old.Reported = ""
		if err := publish(filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".json"), old); err != nil {
			return nil, err
		}
		if err := r.save(); err != nil {
			return nil, err
		}
		return old, nil
	}
	if old != nil && old.Conversation == q.RequestID && old.Phase != "conversation_capture_pending" {
		return old, nil
	}
	contextBytes, _ := json.Marshal(proof)
	req := &Request{Thread: q.Thread, Guild: r.State.Destination.Guild, RecordVersion: 2, Message: reply.Message, Author: reply.Author, Created: reply.Created, Edited: live.Edited, ReplyTo: live.Reference.Message, ReplyChannel: live.Reference.Channel, ReplyGuild: live.Reference.Guild, Acquired: acquiredAt.UTC().Format(time.RFC3339Nano), Source: "discord-api-v10", Text: reply.Body, Context: string(contextBytes), Key: requestKey(q.Thread, reply.Message), Phase: "conversation_reply_saved", ProgressThread: q.Progress, Conversation: q.RequestID}
	path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".json")
	if err := publish(path, req); err != nil {
		return nil, err
	}
	r.State.Requests[reply.Message] = req
	if err := r.save(); err != nil {
		return nil, err
	}
	return req, nil
}

func (r *Runner) deliverConversationReply(ctx context.Context, q ConversationQuestion, reply conversationReply, req *Request) error {
	if req.Phase == "conversation_reply_delivered" || req.Phase == "conversation_reply_applied" {
		return nil
	}
	row, err := r.conversationTask(ctx, q.Todo)
	if err != nil {
		return err
	}
	if row.Project != q.Project {
		return errors.New("conversation original task project differs")
	}
	scopeFile, err := r.conversationScopeFile(row, q)
	if err != nil {
		return err
	}
	path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".json")
	receipt, err := boundedRegular(path)
	if err != nil {
		return err
	}
	marker := conversationReplyMarker(q, req)
	if q.Wait {
		// Every waiting reply, including a second reply, gets an immutable input
		// before any edit. Reacquisition must not silently replace that version.
		answer, b, answerFile, reacquired, err := r.conversationAnswerInput(q, reply, req, receipt)
		if err != nil {
			return err
		}
		if reacquired {
			// This survives both a lost successful answer response and another
			// intake loss. Delivery proof cannot prove application of a different
			// source SHA, even if a canonical answer was already committed.
			req.ConversationRecoveryNote = "再取得で受領元が置換済み。メモの旧受領SHAを現在版の反映証拠にしない。実反映と質問の締めは未確認。現在のsource_receipt_sha256=" + bytesHash(receipt)
		}
		if conversationDeliveryRecorded(row, q, answer, b) || conversationAnswerRecorded(row, q, answer, b) || (reacquired && conversationQuestionAnswered(row, q) && hasNoteMarker(row.Note, marker)) {
			req.Todo, req.Phase = q.Todo, "conversation_reply_delivered"
			return r.save()
		}
		if reacquired {
			if !hasNoteMarker(row.Note, marker) || !conversationWaitUnchanged(row, q) || !conversationWaitOwnerUnchanged(row) {
				return errors.New("conversation reacquisition delivery proof unavailable")
			}
			// A note alone is a partial delivery. Complete only its exact original
			// wait with the saved input; the canonical lock rejects any later hold.
			if _, err = r.Run(ctx, conversationTodo, "question-answer", fmt.Sprint(q.Todo), "--answer-file", answerFile, "--scope-file", scopeFile); err != nil {
				return errors.New("conversation reacquired partial answer deferred")
			}
			current, err := r.todo(q.Todo)
			if err != nil || current.Project != q.Project || !conversationAnswerRecorded(current, q, answer, b) {
				return errors.New("conversation reacquired answer readback deferred")
			}
			req.Todo, req.Phase = q.Todo, "conversation_reply_delivered"
			return r.save()
		}
	}
	if req.ConversationSourceRecovered && !hasNoteMarker(row.Note, marker) && (!q.Wait || conversationQuestionAnswered(row, q)) {
		return errors.New("conversation recovered source delivery proof unavailable")
	}
	if q.Wait && conversationContinuedWait(row, q) {
		return r.deliverContinuedConversationReply(ctx, row, q, reply, req, receipt, marker)
	}
	if q.Wait && !conversationQuestionAnswered(row, q) && !conversationWaitOwnerUnchanged(row) {
		return errors.New("conversation original question generation differs")
	}
	// Never append to an overwritten question hold or manufacture its missing
	// marker. Only an intact canonical wait or its durable answer can proceed.
	imported := q.Wait && row.Status == "block" && conversationWaitRecorded(row, q) && row.Note == "ジョブの結果に判断が必要です。"
	if q.Wait && !conversationQuestionAnswered(row, q) && !conversationWaitUnchanged(row, q) && !imported {
		return errors.New("conversation original question wait differs")
	}
	if !hasNoteMarker(row.Note, marker) {
		instructions := r.conversationReplyInstructions(q, reply, req, receipt, marker)
		note := strings.TrimRight(row.Note, "\r\n") + "\n\n" + instructions
		if len(note) > 1<<20 {
			return errors.New("conversation delivery note limit exceeded")
		}
		noteFile := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".note.txt")
		if err = writeText(noteFile, note); err != nil {
			return err
		}
		if imported {
			// Rust checks the exact row, original owner/result generation and all
			// holds under one lock. Never reconstruct a missing question marker
			// with edit, or expose a runnable task before its reply is durable.
			expectedFile, err := r.conversationSnapshotFile(row, q, reply.Message)
			if err != nil {
				return err
			}
			answer, b, answerFile, err := r.prepareConversationAnswer(q, reply, req, receipt)
			if err != nil {
				return err
			}
			if _, err = r.Run(ctx, conversationTodo, "question-answer", fmt.Sprint(q.Todo), "--answer-file", answerFile, "--delivery-note-file", noteFile, "--expected-state-file", expectedFile, "--scope-file", scopeFile); err != nil {
				return errors.New("conversation imported answer delivery deferred")
			}
			row, err = r.todo(q.Todo)
			if err != nil || row.Project != q.Project || !hasNoteMarker(row.Note, marker) || !conversationAnswerRecorded(row, q, answer, b) {
				return errors.New("conversation imported answer readback deferred")
			}
			req.Todo, req.Phase = q.Todo, "conversation_reply_delivered"
			return r.save()
		}
		// Compare under the canonical ToDo lock: a concurrent ordinary block
		// must not be overwritten with this snapshot's question-wait marker.
		expectedFile, err := r.conversationSnapshotFile(row, q, reply.Message)
		if err != nil {
			return err
		}
		if _, err = r.Run(ctx, conversationTodo, "edit", fmt.Sprint(q.Todo), "--note-file", noteFile, "--expected-note-sha256", bytesHash([]byte(row.Note)), "--scope-file", scopeFile, "--expected-state-file", expectedFile); err != nil {
			return errors.New("conversation normal task delivery deferred")
		}
		row, err = r.conversationTask(ctx, q.Todo)
		if err != nil || row.Project != q.Project || !hasNoteMarker(row.Note, marker) {
			return errors.New("conversation task delivery readback deferred")
		}
	}
	// The reply reference must be durable before the task becomes runnable.
	// Recheck after the edit; question-answer remains the final locked authority.
	// A lost successful response is reconciled without releasing a later block.
	if q.Wait && !conversationQuestionAnswered(row, q) {
		if !conversationWaitUnchanged(row, q) || !conversationWaitOwnerUnchanged(row) {
			return errors.New("conversation original question wait differs")
		}
		answer, b, answerFile, err := r.prepareConversationAnswer(q, reply, req, receipt)
		if err != nil {
			return err
		}
		if _, err = r.Run(ctx, conversationTodo, "question-answer", fmt.Sprint(q.Todo), "--answer-file", answerFile, "--scope-file", scopeFile); err != nil {
			return errors.New("conversation exact question wait release deferred")
		}
		row, err = r.todo(q.Todo)
		if err != nil || row.Project != q.Project || !hasNoteMarker(row.Note, marker) || !conversationAnswerRecorded(row, q, answer, b) {
			return errors.New("conversation question answer readback deferred")
		}
	}
	// No done/start/launch is issued here. Only the canonical question-answer
	// command may release the exact new wait, under the shared ToDo lock.
	req.Todo = q.Todo
	req.Phase = "conversation_reply_delivered"
	return r.save()
}

func conversationReplyMarker(q ConversationQuestion, req *Request) string {
	return "conversation-owner-reply:" + q.SenderID + ":" + req.Message + ":" + canonicalHash([]string{req.Text, req.Edited})[:16]
}

func (r *Runner) conversationReplyInstructions(q ConversationQuestion, reply conversationReply, req *Request, receipt []byte, marker string) string {
	path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".json")
	instructions := fmt.Sprintf("%s\n本人の回答と元の質問: %s\n元ToDo %d。取得元はDiscord正規API、本人IDと元質問・判断範囲を照合済みです。担当は原文と質問の範囲を読んで実作業へ反映し、受領と実反映を分けて記録してください。\n検証後の反映記録は %s へ保存します。必要項目: source=discord-api-v10, request_id=%s, origin_todo_id=%d, todo_id=%d, thread_id=%s, question_message_id=%s, message_id=%s, author_id=%s, job_id（実際の担当）, revision（40桁）, applied_at（RFC3339）, completion（反映した内容）, verification（execution/origin_saved/identityの各確認結果を文字列で）。追加費用など元質問を超える許可に拡大しないでください。\n", marker, path, q.Todo, filepath.Join(r.Config.Home, "applied", reply.Message+".json"), q.SenderID, q.Todo, q.Todo, q.Thread, q.Message, reply.Message, reply.Author)
	instructions += fmt.Sprintf("返信版の照合: reply_sha256=%s, source_receipt_sha256=%s を反映記録へ含めてください。現在の私的原文を読み返してから記録し、編集前の証跡を新しい本文の反映証拠にしないでください。\n", canonicalHash(reply), bytesHash(receipt))
	instructions += fmt.Sprintf("元回答時刻=%s、編集時刻=%s、取得時刻=%s。現在の直接指示・停止・依存・元仕様と許可範囲を保持し、古い回答で新しい指示を上書きしないでください。\n", req.Created, req.Edited, req.Acquired)
	return instructions
}

func conversationContinuedWait(row *TodoRow, q ConversationQuestion) bool {
	var wait struct {
		Origin struct {
			Generation *uint64 `json:"resume_generation"`
		} `json:"origin"`
	}
	var current *uint64
	return (row.Status == "doing" || row.Status == "todo") && conversationWaitRecorded(row, q) &&
		json.Unmarshal(row.QuestionWait, &wait) == nil && wait.Origin.Generation != nil && json.Unmarshal(row.ResumeGeneration, &current) == nil && current != nil && *current > *wait.Origin.Generation
}

func conversationWaitOwnerUnchanged(row *TodoRow) bool {
	var wait struct {
		Origin json.RawMessage `json:"origin"`
	}
	if json.Unmarshal(row.QuestionWait, &wait) != nil {
		return false
	}
	if len(wait.Origin) == 0 {
		return true
	} // Existing legacy waits keep their original contract.
	var current struct {
		Generation json.RawMessage `json:"resume_generation"`
		Worker     json.RawMessage `json:"worker_receipt"`
		Scheduler  struct {
			Job json.RawMessage `json:"job"`
			Key json.RawMessage `json:"key"`
		} `json:"scheduler"`
	}
	if json.Unmarshal(row.conversationState, &current) != nil {
		return false
	}
	return canonicalHash(wait.Origin) == canonicalHash(map[string]json.RawMessage{"resume_generation": current.Generation, "worker_receipt": current.Worker, "job": current.Scheduler.Job, "key": current.Scheduler.Key})
}

func conversationDeliveryRecorded(row *TodoRow, q ConversationQuestion, answer conversationAnswerReceipt, b []byte) bool {
	matched := 0
	for _, raw := range row.QuestionDeliveries {
		var record struct {
			Domain string                    `json:"domain"`
			SHA    string                    `json:"answer_file_sha256"`
			Answer conversationAnswerReceipt `json:"answer"`
			Wait   struct {
				SHA      string `json:"question_receipt_sha256"`
				Question struct {
					Request string `json:"request_id"`
				} `json:"question"`
			} `json:"wait"`
		}
		if json.Unmarshal(raw, &record) == nil && record.Domain == "todo-owner-question-delivery-receipt-v1" && record.SHA == bytesHash(b) && record.Answer == answer && record.Wait.SHA == q.ReceiptSHA && record.Wait.Question.Request == q.SenderID {
			matched++
		}
	}
	return matched == 1
}

func (r *Runner) deliverContinuedConversationReply(ctx context.Context, row *TodoRow, q ConversationQuestion, reply conversationReply, req *Request, receipt []byte, marker string) error {
	note := row.Note + "\n\n" + r.conversationReplyInstructions(q, reply, req, receipt, marker)
	if len(note) > 1<<20 {
		return errors.New("conversation delivery note limit exceeded")
	}
	dir := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies")
	noteFile := filepath.Join(dir, reply.Message+".note.txt")
	if err := writeText(noteFile, note); err != nil {
		return err
	}
	scopeFile, err := r.conversationScopeFile(row, q)
	if err != nil {
		return err
	}
	expectedFile, err := r.conversationSnapshotFile(row, q, reply.Message)
	if err != nil {
		return err
	}
	answer, b, answerFile, err := r.prepareConversationAnswer(q, reply, req, receipt)
	if err != nil {
		return err
	}
	if _, err := r.Run(ctx, conversationTodo, "question-answer", fmt.Sprint(q.Todo), "--answer-file", answerFile, "--delivery-only", "--delivery-note-file", noteFile, "--expected-state-file", expectedFile, "--scope-file", scopeFile); err != nil {
		return errors.New("conversation continued answer delivery deferred")
	}
	current, err := r.todo(q.Todo)
	if err != nil || current.Project != q.Project || !conversationDeliveryRecorded(current, q, answer, b) {
		return errors.New("conversation continued answer readback deferred")
	}
	req.Todo, req.Phase = q.Todo, "conversation_reply_delivered"
	return r.save()
}

// Keep construction separate from the immutable input and its recovery checks.
func conversationAnswerBytes(q ConversationQuestion, reply conversationReply, req *Request, receipt []byte) (conversationAnswerReceipt, []byte) {
	answer := conversationAnswerReceipt{Domain: "discord-conversation-answer-v1", RequestID: q.SenderID, Todo: fmt.Sprint(q.Todo), Thread: q.Thread, Question: q.Message, Fingerprint: q.Fingerprint, QuestionSHA: q.ReceiptSHA, Message: reply.Message, Author: reply.Author, Source: "discord-api-v10", ReplySHA: canonicalHash(reply), Acquired: req.Acquired, ReceiptSHA: bytesHash(receipt)}
	b, _ := json.MarshalIndent(answer, "", "  ")
	return answer, b
}

func (r *Runner) prepareConversationAnswer(q ConversationQuestion, reply conversationReply, req *Request, receipt []byte) (conversationAnswerReceipt, []byte, string, error) {
	answer, b, path, reacquired, err := r.conversationAnswerInput(q, reply, req, receipt)
	if err == nil && reacquired {
		err = errors.New("conversation reacquisition requires delivery proof")
	}
	return answer, b, path, err
}

// A ready file fixes this text/edit version's original acquisition. A new API
// acquisition can confirm its old delivery, but can never manufacture a new
// edit from an older answer after the intake index and note have been replaced.
func (r *Runner) conversationAnswerInput(q ConversationQuestion, reply conversationReply, req *Request, receipt []byte) (conversationAnswerReceipt, []byte, string, bool, error) {
	answer, b := conversationAnswerBytes(q, reply, req, receipt)
	path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+"-"+canonicalHash([]string{req.Text, req.Edited})[:16]+".ready.json")
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return answer, b, path, false, writeConversationOnce(path, b)
	}
	previous, err := boundedRegular(path)
	if err != nil {
		return answer, nil, path, false, err
	}
	var saved conversationAnswerReceipt
	if json.Unmarshal(previous, &saved) != nil {
		return answer, nil, path, false, errors.New("conversation saved answer invalid")
	}
	expected := answer
	expected.Acquired, expected.ReceiptSHA = saved.Acquired, saved.ReceiptSHA
	expectedBytes, _ := json.MarshalIndent(expected, "", "  ")
	acquired, timeErr := time.Parse(time.RFC3339Nano, saved.Acquired)
	created, createdErr := time.Parse(time.RFC3339Nano, req.Created)
	if !bytes.Equal(previous, expectedBytes) || !shaPattern.MatchString(saved.ReceiptSHA) || timeErr != nil || createdErr != nil || acquired.Before(created) || acquired.After(r.Now().Add(time.Minute)) {
		return answer, nil, path, false, errors.New("conversation saved answer association differs")
	}
	return saved, previous, path, saved != answer, nil
}

func conversationWaitUnchanged(row *TodoRow, q ConversationQuestion) bool {
	if row.Status != "block" || !conversationWaitRecorded(row, q) {
		return false
	}
	marker := fmt.Sprintf("[discord-conversation-question-v1 request_id=%s sha256=%s]", q.SenderID, q.ReceiptSHA)
	if strings.Count(row.Note, marker) != 1 {
		return false
	}
	for _, line := range strings.Split(row.Note, "\n") {
		if line == marker {
			return true
		}
	}
	return false
}

func (r *Runner) conversationProgress(ctx context.Context, q ConversationQuestion, req *Request, phase string) error {
	if req.Reported == phase || r.NoticeAttempted {
		return nil
	}
	verb := "本人の回答を保存し、元の作業へ届けました。担当による実反映はこれから確認します。"
	if phase == "conversation_applied" {
		verb = "本人の回答を担当が反映し、検証結果を確認しました。元作業の残りは共有ToDoで継続します。"
	}
	body := fmt.Sprintf("状態: 継続中\n%s\n元の作業: https://discord.com/channels/%s/%s\n質問: https://discord.com/channels/%s/%s\n共有ToDo: %d", verb, r.State.Destination.Guild, q.OriginThread, r.State.Destination.Guild, q.Thread, q.Todo)
	path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", req.Message+"-"+phase+".txt")
	if err := writeText(path, body); err != nil {
		return err
	}
	r.NoticeAttempted = true
	_, err := r.conversationLog(ctx, "reply-"+bytesHash([]byte(q.RequestID + req.Message + phase))[:32], "本人の回答への対応", path, q.Progress, false, nil)
	if err != nil {
		return err
	}
	req.Reported = phase
	return r.save()
}

func (r *Runner) conversationApplied(ctx context.Context, q ConversationQuestion, proof *conversationProof, reply conversationReply, req *Request) (bool, error) {
	return r.closeConversation(ctx, q, proof)
}

func regexpRevision(revision string) bool {
	if len(revision) != 40 {
		return false
	}
	return strings.Trim(revision, "0123456789abcdefABCDEF") == ""
}

// One question per existing tick, round-robin even on failure. New questions
// cannot starve behind an unavailable older question or a large history.
func (r *Runner) processConversations(ctx context.Context, health *Health) {
	index, err := r.loadConversations()
	if err != nil {
		health.Errors = append(health.Errors, "conversation_index_deferred")
		return
	}
	ids := orderedConversationKeys(index)
	start := sort.Search(len(ids), func(i int) bool { return ids[i] > index.Cursor })
	for i := 0; i < len(ids); i++ {
		id := ids[(start+i)%len(ids)]
		q := index.Questions[id]
		index.Cursor = id
		if publish(r.conversationPath(), index) != nil {
			health.Errors = append(health.Errors, "conversation_cursor_deferred")
			return
		}
		if q.Phase == "reserved" || (q.Phase == "posted" && q.Wait) {
			_, err := r.conversationAsk(ctx, ConversationAskInput{RequestID: q.RequestID, Todo: q.Todo, OriginThread: q.OriginThread, BodyFile: q.BodyFile, DecisionFile: q.DecisionFile, Title: q.Title, Wait: q.Wait, Attachments: q.Attachments})
			if err != nil {
				health.Errors = append(health.Errors, "conversation_send_pending")
			}
			return
		}
		proof, err := r.conversationProof(ctx, q)
		if err != nil {
			health.Errors = append(health.Errors, "conversation_reply_deferred")
			return
		}
		sort.Slice(proof.Replies, func(i, j int) bool {
			a, _ := number(proof.Replies[i].Message)
			b, _ := number(proof.Replies[j].Message)
			return a < b
		})
		replyCursor, _ := number(q.ReplyCursor)
		replyStart := sort.Search(len(proof.Replies), func(i int) bool { n, _ := number(proof.Replies[i].Message); return n > replyCursor })
		for replyIndex := 0; replyIndex < len(proof.Replies); replyIndex++ {
			reply := proof.Replies[(replyStart+replyIndex)%len(proof.Replies)]
			if ctx.Err() != nil {
				health.Errors = append(health.Errors, "conversation_budget_deferred")
				return
			}
			q.ReplyCursor = reply.Message
			index.Questions[id] = q
			if publish(r.conversationPath(), index) != nil {
				health.Errors = append(health.Errors, "conversation_cursor_deferred")
				return
			}
			req, err := r.captureConversationReply(ctx, q, proof, reply)
			if err == nil {
				err = r.deliverConversationReply(ctx, q, reply, req)
			}
			if err != nil {
				health.Errors = append(health.Errors, "conversation_delivery_deferred")
				return
			}
			wasClosed := q.Phase == "closed"
			if !wasClosed || req.Phase != "conversation_reply_applied" {
				q.Phase = "answered"
			}
			index.Questions[id] = q
			if publish(r.conversationPath(), index) != nil {
				health.Errors = append(health.Errors, "conversation_stage_deferred")
				return
			}
			applied, err := r.conversationApplied(ctx, q, proof, reply, req)
			if err != nil {
				health.Errors = append(health.Errors, "conversation_application_deferred")
				return
			}
			if applied {
				q.Phase = "closed"
				index.Questions[id] = q
				if publish(r.conversationPath(), index) != nil {
					health.Errors = append(health.Errors, "conversation_stage_deferred")
					return
				}
			} else {
				q.Phase = "answered"
				index.Questions[id] = q
				if publish(r.conversationPath(), index) != nil {
					health.Errors = append(health.Errors, "conversation_stage_deferred")
					return
				}
			}
			health.ConversationReplies++
			// Keep the tick finite. Other replies are retained by the sender/API
			// and the same source will be checked on its next turn.
			return
		}
		return
	}
}

func (r *Runner) reportConversations(ctx context.Context, health *Health) {
	if r.NoticeAttempted || ctx.Err() != nil {
		return
	}
	index, err := r.loadConversations()
	if err != nil {
		health.Errors = append(health.Errors, "conversation_index_deferred")
		return
	}
	ids := make([]string, 0)
	for id, req := range r.State.Requests {
		if req != nil && req.Conversation != "" && (req.Phase == "conversation_reply_delivered" || req.Phase == "conversation_reply_applied") {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	start := sort.Search(len(ids), func(i int) bool { return ids[i] > r.State.ConversationReportCursor })
	for i := 0; i < len(ids); i++ {
		id := ids[(start+i)%len(ids)]
		req := r.State.Requests[id]
		q, ok := index.Questions[req.Conversation]
		if !ok {
			continue
		}
		stage := "conversation_delivered"
		if req.Phase == "conversation_reply_applied" {
			stage = "conversation_applied"
		}
		if req.Reported == stage {
			continue
		}
		r.State.ConversationReportCursor = id
		if r.save() != nil {
			health.Errors = append(health.Errors, "conversation_report_cursor_deferred")
			return
		}
		if err = r.conversationProgress(ctx, q, req, stage); err != nil {
			health.Errors = append(health.Errors, "conversation_progress_deferred")
			return
		}
		return
	}
}
