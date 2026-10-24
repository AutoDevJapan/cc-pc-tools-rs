package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var conversationMediaObservationName = regexp.MustCompile(`^([1-9][0-9]*)-([0-9a-f]{64})\.private\.json$`)
var conversationMediaScopeName = regexp.MustCompile(`^([1-9][0-9]*)-([0-9a-f]{64})-([1-9][0-9]*)\.scope\.private\.json$`)

type conversationMediaTarget struct {
	message, sha, attachment string
}

func conversationMediaPath(parts []string) (conversationMediaTarget, bool) {
	var target conversationMediaTarget
	if len(parts) != 3 || parts[1] != "owner-media" {
		return target, false
	}
	match := conversationMediaObservationName.FindStringSubmatch(parts[2])
	if match == nil {
		match = conversationMediaScopeName.FindStringSubmatch(parts[2])
	}
	if match == nil || !conversationID(match[1]) {
		return target, false
	}
	target.message, target.sha = match[1], match[2]
	if len(match) == 4 {
		if !conversationID(match[3]) {
			return target, false
		}
		target.attachment = match[3]
	}
	return target, true
}

// Wire types match the existing producer; no attachment URL or filename belongs here.
type conversationMediaObservation struct {
	Domain       string                        `json:"domain"`
	Source       string                        `json:"source"`
	Request      string                        `json:"request_id"`
	Sender       string                        `json:"sender_request_id"`
	Todo         uint64                        `json:"todo_id"`
	Guild        string                        `json:"guild_id"`
	Thread       string                        `json:"thread_id"`
	Question     string                        `json:"question_message_id"`
	Message      string                        `json:"message_id"`
	Owner        string                        `json:"author_id"`
	Created      string                        `json:"created_at"`
	Edited       string                        `json:"edited_timestamp"`
	ContentSHA   string                        `json:"content_sha256"`
	QuestionSHA  string                        `json:"question_content_sha256"`
	CanonicalSHA string                        `json:"canonical_source_receipt_sha256"`
	Attachments  []conversationMediaAttachment `json:"attachments"`
}
type conversationMediaAttachment struct {
	ID   string `json:"id"`
	Size *int64 `json:"size"`
}
type conversationMediaScope struct {
	Domain     string   `json:"domain"`
	Todo       uint64   `json:"todo_id"`
	Guild      string   `json:"guild_id"`
	Owner      string   `json:"owner_id"`
	Since      string   `json:"since"`
	Until      string   `json:"until"`
	Threads    []string `json:"thread_ids"`
	Message    string   `json:"message_id"`
	Attachment string   `json:"attachment_id"`
	ContentSHA string   `json:"expected_content_sha256"`
	Created    string   `json:"expected_created_at"`
	Edited     string   `json:"expected_edited_timestamp"`
	Size       int64    `json:"expected_size"`
}

// The common reader already rejected duplicate keys and retained the original byte
// hash. Decode its same parsed tree strictly; never hash this re-encoding as source.
func conversationMediaTyped(m object, fields string, out any) bool {
	keys := strings.Fields(fields)
	if len(m) != len(keys) {
		return false
	}
	for _, key := range keys {
		if m[key] == nil {
			return false
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return false
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	return d.Decode(out) == nil
}

func conversationMediaAllowed(target conversationMediaTarget, dir string, q, cfg, source object, sourceSHA string, at time.Time, read func(string, int64) (object, string, bool)) bool {
	stem := target.message + "-" + target.sha
	mediaDir := filepath.Join(dir, "owner-media")
	m, sha, ok := read(filepath.Join(mediaDir, stem+".private.json"), 1<<20)
	var observation conversationMediaObservation
	if !ok || sha != target.sha || !conversationMediaTyped(m, "domain source request_id sender_request_id todo_id guild_id thread_id question_message_id message_id author_id created_at edited_timestamp content_sha256 question_content_sha256 canonical_source_receipt_sha256 attachments", &observation) {
		return false
	}
	if observation.Domain != "discord-owner-reply-media-v1" || observation.Source != "discord-api-v10" || observation.Request != q["request_id"] || observation.Sender != q["sender_request_id"] || observation.Todo != conversationNumber(q["todo_id"]) || observation.Guild != cfg["guild_id"] || observation.Owner != cfg["owner_user_id"] || observation.Thread != q["thread_id"] || observation.Question != q["question_message_id"] || observation.Message != target.message || observation.Created != source["created_at"] || observation.Edited != str(source["edited_at"]) || observation.ContentSHA != hashBytes([]byte(str(source["text"]))) || observation.QuestionSHA != q["question_body_sha256"] || observation.CanonicalSHA != sourceSHA || observation.Attachments == nil || len(observation.Attachments) > 10 {
		return false
	}
	var previous uint64
	var selected *conversationMediaAttachment
	for i := range observation.Attachments {
		a := &observation.Attachments[i]
		id, err := strconv.ParseUint(a.ID, 10, 64)
		if err != nil || !conversationID(a.ID) || id <= previous || a.Size == nil || *a.Size < 0 {
			return false
		}
		previous = id
		if a.ID == target.attachment {
			selected = a
		}
	}
	if target.attachment == "" {
		return true
	}
	if selected == nil || *selected.Size <= 0 || *selected.Size > 20<<20 {
		return false
	}
	m, _, ok = read(filepath.Join(mediaDir, stem+"-"+target.attachment+".scope.private.json"), 1<<20)
	var scope conversationMediaScope
	if !ok || !conversationMediaTyped(m, "domain todo_id guild_id owner_id since until thread_ids message_id attachment_id expected_content_sha256 expected_created_at expected_edited_timestamp expected_size", &scope) {
		return false
	}
	created, err := time.Parse(time.RFC3339Nano, observation.Created)
	if err != nil {
		return false
	}
	// Exactly the normal producer's finite, retry-stable exclusive bound. A
	// caller cannot widen the scope or select another observed attachment/size.
	until := created.Add(time.Millisecond)
	return scope.Domain == "discord-owner-image-read-scope-v1" && scope.Todo == observation.Todo && scope.Guild == observation.Guild && scope.Owner == observation.Owner && scope.Since == created.UTC().Format(time.RFC3339Nano) && scope.Until == until.UTC().Format(time.RFC3339Nano) && !until.After(at) && len(scope.Threads) == 1 && scope.Threads[0] == observation.Thread && scope.Message == observation.Message && scope.Attachment == selected.ID && scope.Size == *selected.Size && scope.ContentSHA == observation.ContentSHA && scope.Created == observation.Created && scope.Edited == observation.Edited
}
