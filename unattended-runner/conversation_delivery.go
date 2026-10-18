package main

import (
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
	old := r.State.Requests[reply.Message]
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
		old.Acquired = r.Now().UTC().Format(time.RFC3339Nano)
		contextBytes, _ := json.Marshal(proof)
		old.Context = string(contextBytes)
		old.Phase = "conversation_reply_saved"
		old.Reported = ""
		if err := publish(filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".json"), old); err != nil {
			return nil, err
		}
		if err := r.save(); err != nil {
			return nil, err
		}
		return old, nil
	}
	if old != nil && old.Conversation == q.RequestID {
		return old, nil
	}
	contextBytes, _ := json.Marshal(proof)
	req := &Request{Thread: q.Thread, Guild: r.State.Destination.Guild, RecordVersion: 2, Message: reply.Message, Author: reply.Author, Created: reply.Created, Edited: live.Edited, ReplyTo: live.Reference.Message, ReplyChannel: live.Reference.Channel, ReplyGuild: live.Reference.Guild, Acquired: r.Now().UTC().Format(time.RFC3339Nano), Source: "discord-api-v10", Text: reply.Body, Context: string(contextBytes), Key: requestKey(q.Thread, reply.Message), Phase: "conversation_reply_saved", ProgressThread: q.Progress, Conversation: q.RequestID}
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
	path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".json")
	receipt, err := boundedRegular(path)
	if err != nil {
		return err
	}
	marker := "conversation-owner-reply:" + q.SenderID + ":" + reply.Message + ":" + canonicalHash([]string{req.Text, req.Edited})[:16]
	if !hasNoteMarker(row.Note, marker) {
		instructions := fmt.Sprintf("%s\n本人の回答と元の質問: %s\n元ToDo %d。取得元はDiscord正規API、本人IDと元質問・判断範囲を照合済みです。担当は原文と質問の範囲を読んで実作業へ反映し、受領と実反映を分けて記録してください。\n検証後の反映記録は %s へ保存します。必要項目: source=discord-api-v10, request_id=%s, origin_todo_id=%d, todo_id=%d, thread_id=%s, question_message_id=%s, message_id=%s, author_id=%s, job_id（実際の担当）, revision（40桁）, applied_at（RFC3339）, completion（反映した内容）, verification（execution/origin_saved/identityの各確認結果を文字列で）。追加費用など元質問を超える許可に拡大しないでください。\n", marker, path, q.Todo, filepath.Join(r.Config.Home, "applied", reply.Message+".json"), q.SenderID, q.Todo, q.Todo, q.Thread, q.Message, reply.Message, reply.Author)
		note := strings.TrimRight(row.Note, "\r\n") + "\n\n" + instructions
		if len(note) > 1<<20 {
			return errors.New("conversation delivery note limit exceeded")
		}
		noteFile := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".note.txt")
		if err = writeText(noteFile, note); err != nil {
			return err
		}
		if _, err = r.Run(ctx, conversationTodo, "edit", fmt.Sprint(q.Todo), "--note-file", noteFile); err != nil {
			return errors.New("conversation normal task delivery deferred")
		}
		row, err = r.todo(q.Todo)
		if err != nil || !hasNoteMarker(row.Note, marker) {
			return errors.New("conversation task delivery readback deferred")
		}
	}
	if q.Wait && row.Status == "block" {
		answer := conversationAnswerReceipt{Domain: "discord-conversation-answer-v1", RequestID: q.SenderID, Todo: fmt.Sprint(q.Todo), Thread: q.Thread, Question: q.Message, Fingerprint: q.Fingerprint, QuestionSHA: q.ReceiptSHA, Message: reply.Message, Author: reply.Author, Source: "discord-api-v10", ReplySHA: canonicalHash(reply), Acquired: req.Acquired, ReceiptSHA: bytesHash(receipt)}
		b, _ := json.MarshalIndent(answer, "", "  ")
		answerFile := filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".ready.json")
		if err = writeConversationOnce(answerFile, b); err != nil {
			return err
		}
		if _, err = r.Run(ctx, conversationTodo, "question-answer", fmt.Sprint(q.Todo), "--answer-file", answerFile); err != nil {
			return errors.New("conversation exact question wait release deferred")
		}
	}
	// No done/start/launch is issued here. Only the canonical question-answer
	// command may release the exact new wait, under the shared ToDo lock.
	req.Todo = q.Todo
	req.Phase = "conversation_reply_delivered"
	return r.save()
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
	path := filepath.Join(r.Config.Home, "applied", reply.Message+".json")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil || len(b) > 128<<10 {
		return false, errors.New("conversation applied receipt unavailable")
	}
	var value map[string]json.RawMessage
	if json.Unmarshal(b, &value) != nil {
		return false, errors.New("conversation applied receipt invalid")
	}
	text := func(key string) string { var v string; _ = json.Unmarshal(value[key], &v); return v }
	num := func(key string) uint64 { var v uint64; _ = json.Unmarshal(value[key], &v); return v }
	var verification map[string]string
	if json.Unmarshal(value["verification"], &verification) != nil || verification["execution"] == "" || verification["origin_saved"] == "" || verification["identity"] == "" {
		return false, errors.New("conversation implementation verification missing")
	}
	at, e := time.Parse(time.RFC3339Nano, text("applied_at"))
	acquired, _ := time.Parse(time.RFC3339Nano, req.Acquired)
	if e != nil || at.Before(acquired) || at.After(r.Now().Add(time.Minute)) || text("source") != "discord-api-v10" || text("request_id") != q.SenderID || num("origin_todo_id") != q.Todo || num("todo_id") != q.Todo || text("thread_id") != q.Thread || text("question_message_id") != q.Message || text("message_id") != reply.Message || text("author_id") != r.State.Destination.Owner || !regexpRevision(text("revision")) || strings.TrimSpace(text("completion")) == "" {
		return false, errors.New("conversation applied association differs")
	}
	row, err := r.todo(q.Todo)
	if err != nil {
		return false, err
	}
	owned := false
	for _, job := range row.Jobs {
		var id string
		if json.Unmarshal(job, &id) == nil && id == text("job_id") && validJob(id) {
			owned = true
		}
	}
	if !owned {
		return false, errors.New("conversation applied worker ownership unavailable")
	}
	if len(proof.Replies) != 1 {
		return false, errors.New("conversation additional replies remain independently tracked")
	}
	manifest := map[string]any{"request_id": q.SenderID, "thread_id": q.Thread, "question_message_id": q.Message, "origin_todo_id": q.Todo, "todo_id": q.Todo, "message_id": reply.Message, "author_id": reply.Author, "job_id": text("job_id"), "revision": text("revision"), "question_fingerprint": q.Fingerprint, "reply_sha256": canonicalHash(reply), "receipt_sha256": bytesHash(b), "verification_sha256": canonicalHash(json.RawMessage(value["verification"])), "completion": text("completion")}
	manifestPath := filepath.Join(r.Config.Home, "conversations", q.RequestID, "close.json")
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	if err = writeConversationOnce(manifestPath, mb); err != nil {
		return false, err
	}
	raw, err := r.Run(ctx, conversationSender, "recovery-close", "--manifest", manifestPath)
	if err != nil {
		return false, errors.New("conversation question close verification deferred")
	}
	var outcome struct {
		Phase   string `json:"phase"`
		Outcome string `json:"outcome"`
		Thread  string `json:"thread_id"`
	}
	if json.Unmarshal([]byte(raw), &outcome) != nil || outcome.Thread != q.Thread || (outcome.Outcome != "archived" && outcome.Outcome != "already_archived" && outcome.Outcome != "recovered_after_patch") {
		return false, errors.New("conversation question close receipt differs")
	}
	req.Phase = "conversation_reply_applied"
	if err = r.save(); err != nil {
		return false, err
	}
	return true, nil
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
		if q.Phase == "reserved" || q.Phase == "closed" {
			continue
		}
		index.Cursor = id
		if publish(r.conversationPath(), index) != nil {
			health.Errors = append(health.Errors, "conversation_cursor_deferred")
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
			q.Phase = "answered"
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
			stage := "conversation_delivered"
			if applied {
				stage = "conversation_applied"
				q.Phase = "applied"
				index.Questions[id] = q
				if publish(r.conversationPath(), index) != nil {
					health.Errors = append(health.Errors, "conversation_stage_deferred")
					return
				}
			}
			if err = r.conversationProgress(ctx, q, req, stage); err != nil {
				health.Errors = append(health.Errors, "conversation_progress_deferred")
			}
			if applied && req.Reported == "conversation_applied" {
				q.Phase = "closed"
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
