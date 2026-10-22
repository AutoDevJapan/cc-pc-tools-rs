package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type ownerReplyMediaObservation struct {
	Domain       string                  `json:"domain"`
	Source       string                  `json:"source"`
	Request      string                  `json:"request_id"`
	Sender       string                  `json:"sender_request_id"`
	Todo         uint64                  `json:"todo_id"`
	Guild        string                  `json:"guild_id"`
	Thread       string                  `json:"thread_id"`
	Question     string                  `json:"question_message_id"`
	Message      string                  `json:"message_id"`
	Owner        string                  `json:"author_id"`
	Created      string                  `json:"created_at"`
	Edited       string                  `json:"edited_timestamp"`
	ContentSHA   string                  `json:"content_sha256"`
	QuestionSHA  string                  `json:"question_content_sha256"`
	CanonicalSHA string                  `json:"canonical_source_receipt_sha256"`
	Attachments  []historyReadAttachment `json:"attachments"`
}

func (r *Runner) ownerReplyMedia(ctx context.Context, q ConversationQuestion, reply conversationReply, req *Request) (ownerReplyMediaObservation, bool, error) {
	var live historyReadMessage
	var record ownerReplyMediaObservation
	if req == nil || req.Todo != q.Todo || req.Source != "discord-api-v10" || req.Conversation != q.RequestID || req.Thread != q.Thread || req.Message != reply.Message || req.Author != r.State.Destination.Owner || reply.Author != req.Author || reply.Body != req.Text || reply.Created != req.Created {
		return record, false, errors.New("owner media association differs")
	}
	if err := r.get(ctx, "/channels/"+q.Thread+"/messages/"+reply.Message, &live); err != nil {
		return record, false, errors.New("owner media read deferred")
	}
	if live.ID != req.Message || live.Channel != req.Thread || live.Author.ID != req.Author || live.Author.Bot || live.Content != req.Text || live.Timestamp != req.Created || live.Edited != req.Edited || (live.Reference.Channel != "" && live.Reference.Channel != q.Thread) || (live.Reference.Guild != "" && live.Reference.Guild != r.State.Destination.Guild) {
		return record, false, errors.New("owner media current version differs")
	}
	if live.Attachments == nil {
		return record, false, nil
	}
	if live.Type == nil || (*live.Type != 0 && *live.Type != 19) || !historyAttachmentsValid(live) || len(*live.Attachments) > 10 {
		return record, false, errors.New("owner media metadata invalid")
	}
	attachments := make([]historyReadAttachment, 0, len(*live.Attachments))
	seen := map[string]bool{}
	for _, a := range *live.Attachments {
		if seen[a.ID] {
			return record, false, errors.New("owner media duplicate identity")
		}
		seen[a.ID] = true
		size := *a.Size
		attachments = append(attachments, historyReadAttachment{ID: a.ID, Size: &size})
	}
	sort.Slice(attachments, func(i, j int) bool {
		a, _ := number(attachments[i].ID)
		b, _ := number(attachments[j].ID)
		return a < b
	})
	canonical, err := boundedRegular(filepath.Join(r.Config.Home, "conversations", q.RequestID, "replies", reply.Message+".json"))
	var original Request
	if err != nil || json.Unmarshal(canonical, &original) != nil || original.Message != req.Message || original.Thread != req.Thread || original.Guild != r.State.Destination.Guild || original.Conversation != q.RequestID || original.Author != req.Author || original.Text != req.Text || original.Created != req.Created || original.Edited != req.Edited || original.Source != "discord-api-v10" {
		return record, false, errors.New("owner media canonical source differs")
	}
	record = ownerReplyMediaObservation{Domain: "discord-owner-reply-media-v1", Source: "discord-api-v10", Request: q.RequestID, Sender: q.SenderID, Todo: q.Todo, Guild: r.State.Destination.Guild, Thread: q.Thread, Question: q.Message, Message: req.Message, Owner: req.Author, Created: req.Created, Edited: req.Edited, ContentSHA: bytesHash([]byte(req.Text)), QuestionSHA: q.QuestionSHA, CanonicalSHA: bytesHash(canonical), Attachments: attachments}
	return record, true, nil
}

func (r *Runner) deliverOwnerReplyMediaObservation(ctx context.Context, q ConversationQuestion, reply conversationReply, req *Request) error {
	record, known, err := r.ownerReplyMedia(ctx, q, reply, req)
	if err != nil || !known {
		return err
	}
	dir := filepath.Join(r.Config.Home, "conversations", q.RequestID, "owner-media")
	latestPath := filepath.Join(dir, req.Message+".latest.private.json")
	var previous ownerReplyMediaObservation
	old, readErr := boundedRegular(latestPath)
	if readErr == nil {
		if json.Unmarshal(old, &previous) != nil || previous.Domain != record.Domain || previous.Request != record.Request || previous.Todo != record.Todo || previous.Message != record.Message || previous.Owner != record.Owner || previous.Thread != record.Thread {
			return errors.New("owner media previous association differs")
		}
	} else if _, e := os.Lstat(latestPath); !os.IsNotExist(e) {
		return errors.New("owner media previous observation unavailable")
	}
	if len(record.Attachments) == 0 && readErr != nil {
		return nil
	}
	row, err := r.conversationTask(ctx, q.Todo)
	if err != nil || row.Project != q.Project {
		return errors.New("owner media original task unavailable")
	}
	scopeFile, err := r.conversationScopeFile(row, q)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	sha := bytesHash(data)
	marker := "conversation-owner-media:" + q.SenderID + ":" + req.Message + ":" + sha
	path := filepath.Join(dir, req.Message+"-"+sha+".private.json")
	if err := writeConversationOnce(path, data); err != nil {
		return err
	}
	text := fmt.Sprintf("%s\n本人返信の添付情報: %s\n添付記録SHA=%s、本人=%s、元返信=%s、本文SHA=%s、作成時刻=%s、編集時刻=%s。\n", marker, path, sha, record.Owner, req.Message, record.ContentSHA, req.Created, req.Edited)
	if len(record.Attachments) == 0 {
		text += "現在の添付は0件です。以前の添付参照を現在の回答の画像として流用しないでください。\n"
	} else {
		text += "本文が短くても添付で回答している場合があります。担当は元質問と画像を読み、意味と許可範囲を照合してください。\n"
		created, parseErr := time.Parse(time.RFC3339Nano, req.Created)
		if parseErr != nil {
			return errors.New("owner media timestamp invalid")
		}
		// The scope bytes must remain identical on a retry. A just-created
		// message may defer until this fixed exclusive bound is in the past.
		until := created.Add(time.Millisecond)
		for _, a := range record.Attachments {
			if *a.Size == 0 || *a.Size > 20<<20 {
				text += fmt.Sprintf("添付ID=%s、元サイズ=%d。画像読取のサイズ対象外であり、画像取得済みではありません。\n", a.ID, *a.Size)
				continue
			}
			scope := historyAttachmentScope{historyReadScope: historyReadScope{Domain: "discord-owner-image-read-scope-v1", Todo: q.Todo, Guild: record.Guild, Owner: record.Owner, Since: created.UTC().Format(time.RFC3339Nano), Until: until.UTC().Format(time.RFC3339Nano), Threads: []string{q.Thread}}, Message: req.Message, Attachment: a.ID, ContentSHA: record.ContentSHA, Created: req.Created, Edited: req.Edited, Size: *a.Size}
			if !historyAttachmentScopeValid(scope, r.Now()) {
				return errors.New("owner media image scope invalid")
			}
			scopePath := filepath.Join(dir, req.Message+"-"+sha+"-"+a.ID+".scope.private.json")
			b, _ := json.MarshalIndent(scope, "", "  ")
			if err := writeConversationOnce(scopePath, b); err != nil {
				return err
			}
			text += fmt.Sprintf("添付ID=%s、元サイズ=%d。画像は通常unattended-runner history-attachment --scope %s --output <新しい私的画像パス> で読めます。画像形式・出力資格の確認も必要です。\n", a.ID, *a.Size, scopePath)
		}
	}
	text += "この追記は添付情報の配送だけです。画像内容の実読、実反映、検証、質問終了を示しません。元のcanonical回答/受領SHAと待機・本人停止・担当・依存は保持し、別の画像や操作への承認へ広げないでください。\n"
	current, currentKnown, err := r.ownerReplyMedia(ctx, q, reply, req)
	if err != nil || !currentKnown || canonicalHash(current) != canonicalHash(record) {
		return errors.New("owner media changed before delivery")
	}
	if !hasNoteMarker(row.Note, marker) {
		note := row.Note + "\n\n" + text
		if len(note) > 1<<20 {
			return errors.New("owner media note limit exceeded")
		}
		notePath := filepath.Join(dir, req.Message+"-"+sha+".note.txt")
		if err := writeText(notePath, note); err != nil {
			return err
		}
		expected, err := r.conversationSnapshotFile(row, q, req.Message)
		if err != nil {
			return err
		}
		if _, err := r.Run(ctx, conversationTodo, "edit", fmt.Sprint(q.Todo), "--note-file", notePath, "--expected-note-sha256", bytesHash([]byte(row.Note)), "--scope-file", scopeFile, "--expected-state-file", expected); err != nil {
			return errors.New("owner media note deferred")
		}
		row, err = r.conversationTask(ctx, q.Todo)
		if err != nil || row.Project != q.Project || !hasNoteMarker(row.Note, marker) {
			return errors.New("owner media note readback deferred")
		}
	}
	return publish(latestPath, record)
}
