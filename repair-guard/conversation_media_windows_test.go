package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// These wire types mirror the normal producer, independently of qualification.
type mediaObservationFixture struct {
	Domain       string                   `json:"domain"`
	Source       string                   `json:"source"`
	Request      string                   `json:"request_id"`
	Sender       string                   `json:"sender_request_id"`
	Todo         uint64                   `json:"todo_id"`
	Guild        string                   `json:"guild_id"`
	Thread       string                   `json:"thread_id"`
	Question     string                   `json:"question_message_id"`
	Message      string                   `json:"message_id"`
	Owner        string                   `json:"author_id"`
	Created      string                   `json:"created_at"`
	Edited       string                   `json:"edited_timestamp"`
	ContentSHA   string                   `json:"content_sha256"`
	QuestionSHA  string                   `json:"question_content_sha256"`
	CanonicalSHA string                   `json:"canonical_source_receipt_sha256"`
	Attachments  []mediaAttachmentFixture `json:"attachments"`
}
type mediaAttachmentFixture struct {
	ID   string `json:"id"`
	Size *int64 `json:"size"`
}
type mediaScopeFixture struct {
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
type conversationMediaFixture struct {
	f                          *conversationFixture
	observation                mediaObservationFixture
	scope                      mediaScopeFixture
	observationPath, scopePath string
}

func newConversationMediaFixture(t *testing.T) *conversationMediaFixture {
	t.Helper()
	f := newConversationFixture(t)
	raw, err := os.ReadFile(f.replyPath)
	if err != nil {
		t.Fatal("canonical fixture unavailable")
	}
	size := int64(64)
	m := &conversationMediaFixture{f: f}
	m.observation = mediaObservationFixture{Domain: "discord-owner-reply-media-v1", Source: "discord-api-v10", Request: str(f.q["request_id"]), Sender: str(f.q["sender_request_id"]), Todo: 1, Guild: "100", Thread: "200", Question: "200", Message: "202", Owner: "101", Created: str(f.record["created_at"]), ContentSHA: hashBytes([]byte(str(f.record["text"]))), QuestionSHA: str(f.q["question_body_sha256"]), CanonicalSHA: hashBytes(raw), Attachments: []mediaAttachmentFixture{{ID: "900", Size: &size}}}
	created, _ := time.Parse(time.RFC3339Nano, m.observation.Created)
	m.scope = mediaScopeFixture{Domain: "discord-owner-image-read-scope-v1", Todo: 1, Guild: "100", Owner: "101", Since: m.observation.Created, Until: created.Add(time.Millisecond).Format(time.RFC3339Nano), Threads: []string{"200"}, Message: "202", Attachment: "900", ContentSHA: m.observation.ContentSHA, Created: m.observation.Created, Size: size}
	m.publish()
	return m
}
func (m *conversationMediaFixture) publish() {
	m.f.t.Helper()
	raw, err := json.MarshalIndent(m.observation, "", "  ")
	if err != nil {
		m.f.t.Fatal("observation fixture invalid")
	}
	dir := filepath.Join(m.f.dir, "owner-media")
	if os.MkdirAll(dir, 0700) != nil {
		m.f.t.Fatal("media fixture directory unavailable")
	}
	stem := m.observation.Message + "-" + hashBytes(raw)
	m.observationPath = filepath.Join(dir, stem+".private.json")
	m.scopePath = filepath.Join(dir, stem+"-"+m.scope.Attachment+".scope.private.json")
	if os.WriteFile(m.observationPath, raw, 0600) != nil {
		m.f.t.Fatal("observation fixture write failed")
	}
	m.f.put(m.scopePath, m.scope)
}
func TestConversationOwnerMediaQualifiedOriginalAndScope(t *testing.T) {
	m := newConversationMediaFixture(t)
	before, _ := os.ReadFile(m.f.replyPath)
	for _, path := range []string{filepath.Join(m.f.dir, "question.json"), m.f.replyPath, m.observationPath, m.scopePath} {
		t.Run(filepath.Base(path), func(t *testing.T) {
			if !conversationAllowed(path, m.f.a) {
				t.Fatal("normal record rejected")
			}
		})
	}
	if m.f.rootCalls != 4 {
		t.Fatal("original source qualification was bypassed")
	}
	after, _ := os.ReadFile(m.f.replyPath)
	if string(before) != string(after) {
		t.Fatal("canonical source changed")
	}
	if obj(m.f.db["items"].([]any)[0])["status"] != "block" {
		t.Fatal("original stop changed")
	}
}
