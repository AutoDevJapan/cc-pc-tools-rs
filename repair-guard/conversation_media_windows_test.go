package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

func TestConversationOwnerMediaProvenanceMismatch(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*conversationMediaFixture)
	}{
		{"domain", func(m *conversationMediaFixture) { m.observation.Domain = "other" }},
		{"source", func(m *conversationMediaFixture) { m.observation.Source = "html" }},
		{"request-generation", func(m *conversationMediaFixture) { m.observation.Request = "another-generation" }},
		{"sender-generation", func(m *conversationMediaFixture) { m.observation.Sender = "another-sender" }},
		{"todo", func(m *conversationMediaFixture) { m.observation.Todo = 2 }},
		{"guild", func(m *conversationMediaFixture) { m.observation.Guild = "999" }},
		{"thread", func(m *conversationMediaFixture) { m.observation.Thread = "999" }},
		{"question", func(m *conversationMediaFixture) { m.observation.Question = "999" }},
		{"reply", func(m *conversationMediaFixture) { m.observation.Message = "999" }},
		{"owner", func(m *conversationMediaFixture) { m.observation.Owner = "999" }},
		{"created", func(m *conversationMediaFixture) { m.observation.Created = "2026-10-18T01:00:04Z" }},
		{"edited", func(m *conversationMediaFixture) { m.observation.Edited = "2026-10-18T01:00:04Z" }},
		{"content-sha", func(m *conversationMediaFixture) { m.observation.ContentSHA = strings.Repeat("a", 64) }},
		{"question-sha", func(m *conversationMediaFixture) { m.observation.QuestionSHA = strings.Repeat("a", 64) }},
		{"source-sha", func(m *conversationMediaFixture) { m.observation.CanonicalSHA = strings.Repeat("a", 64) }},
		{"unknown-attachments", func(m *conversationMediaFixture) { m.observation.Attachments = nil }},
		{"unknown-size", func(m *conversationMediaFixture) { m.observation.Attachments[0].Size = nil }},
		{"negative-size", func(m *conversationMediaFixture) { *m.observation.Attachments[0].Size = -1 }},
		{"attachment-id", func(m *conversationMediaFixture) { m.observation.Attachments[0].ID = "0900" }},
		{"attachment-id-overflow", func(m *conversationMediaFixture) { m.observation.Attachments[0].ID = "18446744073709551616" }},
		{"duplicate-attachment", func(m *conversationMediaFixture) {
			m.observation.Attachments = append(m.observation.Attachments, m.observation.Attachments[0])
		}},
		{"attachment-order", func(m *conversationMediaFixture) {
			m.observation.Attachments = append(m.observation.Attachments, mediaAttachmentFixture{ID: "800", Size: m.observation.Attachments[0].Size})
		}},
		{"too-many-attachments", func(m *conversationMediaFixture) {
			for i := 0; i < 10; i++ {
				m.observation.Attachments = append(m.observation.Attachments, mediaAttachmentFixture{ID: strings.Repeat("9", i+4), Size: m.observation.Attachments[0].Size})
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newConversationMediaFixture(t)
			tc.mutate(m)
			// Keep the original filename/reply present so this case reaches the
			// observation's message binding, rather than a missing 999.json.
			m.publishNamed("202", m.scope.Attachment)
			for _, p := range []string{m.observationPath, m.scopePath} {
				if conversationAllowed(p, m.f.a) {
					t.Fatal("mismatched observation accepted")
				}
			}
		})
	}
}

func TestConversationOwnerMediaScopeMismatch(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*mediaScopeFixture)
	}{
		{"domain", func(s *mediaScopeFixture) { s.Domain = "discord-history-read-scope-v1" }},
		{"todo", func(s *mediaScopeFixture) { s.Todo = 2 }},
		{"guild", func(s *mediaScopeFixture) { s.Guild = "999" }},
		{"owner", func(s *mediaScopeFixture) { s.Owner = "999" }},
		{"thread", func(s *mediaScopeFixture) { s.Threads = []string{"999"} }},
		{"extra-thread", func(s *mediaScopeFixture) { s.Threads = append(s.Threads, "201") }},
		{"message", func(s *mediaScopeFixture) { s.Message = "999" }},
		{"attachment", func(s *mediaScopeFixture) { s.Attachment = "901" }},
		{"content-sha", func(s *mediaScopeFixture) { s.ContentSHA = strings.Repeat("a", 64) }},
		{"created", func(s *mediaScopeFixture) { s.Created = "2026-10-18T01:00:04Z" }},
		{"edited", func(s *mediaScopeFixture) { s.Edited = "2026-10-18T01:00:04Z" }},
		{"size", func(s *mediaScopeFixture) { s.Size++ }},
		{"since", func(s *mediaScopeFixture) { s.Since = "2026-10-18T01:00:02Z" }},
		{"until", func(s *mediaScopeFixture) { s.Until = "2026-10-18T01:00:04Z" }},
		{"future", func(s *mediaScopeFixture) { s.Until = "2999-01-01T00:00:00Z" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newConversationMediaFixture(t)
			tc.mutate(&m.scope)
			if tc.name == "attachment" {
				m.observation.Attachments = append(m.observation.Attachments, mediaAttachmentFixture{ID: "901", Size: m.observation.Attachments[0].Size})
			}
			// Both attachments exist: select 900 by filename but put 901 inside.
			m.publishNamed(m.observation.Message, "900")
			if conversationAllowed(m.scopePath, m.f.a) {
				t.Fatal("mismatched scope accepted")
			}
		})
	}
}

func TestConversationOwnerMediaKnownRemovalAndSizeContract(t *testing.T) {
	for _, size := range []int64{0, 20<<20 + 1} {
		t.Run(strconv.FormatInt(size, 10), func(t *testing.T) {
			m := newConversationMediaFixture(t)
			*m.observation.Attachments[0].Size = size
			m.scope.Size = size
			m.publish()
			if !conversationAllowed(m.observationPath, m.f.a) || conversationAllowed(m.scopePath, m.f.a) {
				t.Fatal("observation confused with eligible image scope")
			}
		})
	}
	m := newConversationMediaFixture(t)
	m.observation.Attachments = []mediaAttachmentFixture{}
	m.publish()
	if !conversationAllowed(m.observationPath, m.f.a) || conversationAllowed(m.scopePath, m.f.a) {
		t.Fatal("known removal invented an image")
	}
}

func TestConversationOwnerMediaCanonicalAndProtectionStillRequired(t *testing.T) {
	for _, which := range []string{"canonical-missing", "canonical-bytes", "pending-version", "reserved", "root", "observation", "scope", "canonical", "question", "notify", "sender", "state"} {
		t.Run(which, func(t *testing.T) {
			m := newConversationMediaFixture(t)
			switch which {
			case "canonical-missing":
				if os.Rename(m.f.replyPath, m.f.replyPath+".retained") != nil {
					t.Fatal("fixture rename failed")
				}
			case "canonical-bytes":
				raw, _ := os.ReadFile(m.f.replyPath)
				if os.WriteFile(m.f.replyPath, append(raw, '\n'), 0600) != nil {
					t.Fatal("fixture write failed")
				}
			case "pending-version":
				obj(obj(m.f.state["requests"])["202"])["text"] = "later owner version"
				m.f.publish()
			case "reserved":
				m.f.q["phase"] = "reserved"
				m.f.publish()
			case "root":
				m.f.a.rootAllowed = func(string) bool { return false }
			default:
				p := map[string]string{"observation": m.observationPath, "scope": m.scopePath, "canonical": m.f.replyPath, "question": filepath.Join(m.f.dir, "question.json"), "notify": m.f.a.notify, "sender": m.f.a.sender, "state": filepath.Join(m.f.a.home, "unattended-state.json")}[which]
				// Represents both original client and owner-card exclusions; no real data.
				m.f.a.protected = func(path string) bool { return same(path, p) }
			}
			if conversationAllowed(m.scopePath, m.f.a) {
				t.Fatal("original source/protection requirement bypassed")
			}
		})
	}
}

func TestConversationOwnerMediaUnknownAndLinkedFiles(t *testing.T) {
	m := newConversationMediaFixture(t)
	stem := strings.TrimSuffix(filepath.Base(m.observationPath), ".private.json")
	for _, name := range []string{"202.latest.private.json", stem + ".note.txt", stem + ".expected.json", "unknown.json", "202-" + strings.Repeat("a", 64) + ".private.json", "202-" + strings.Repeat("a", 64) + "-900.scope.private.json"} {
		p := filepath.Join(filepath.Dir(m.scopePath), name)
		m.f.put(p, m.scope)
		if conversationAllowed(p, m.f.a) {
			t.Fatal("unknown or unbound file accepted")
		}
	}
	for _, which := range []string{"scope", "observation", "directory"} {
		t.Run(which, func(t *testing.T) {
			m := newConversationMediaFixture(t)
			p := m.scopePath
			if which == "observation" {
				p = m.observationPath
			}
			if which == "directory" {
				p = filepath.Dir(p)
			}
			retained := p + ".retained"
			if os.Rename(p, retained) != nil || os.Symlink(retained, p) != nil {
				t.Fatal("linked fixture unavailable")
			}
			if conversationAllowed(m.scopePath, m.f.a) {
				t.Fatal("linked evidence accepted")
			}
		})
	}
}

func TestConversationOwnerMediaOverwriteAndConcurrentChange(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		t.Run(map[bool]string{false: "overwrite", true: "concurrent"}[concurrent], func(t *testing.T) {
			m := newConversationMediaFixture(t)
			raw, _ := os.ReadFile(m.observationPath)
			change := func() {
				if os.WriteFile(m.observationPath, append(raw, '\n'), 0600) != nil {
					t.Fatal("fixture change failed")
				}
			}
			if concurrent {
				changed := false
				scopeChecks := 0
				m.f.a.protected = func(path string) bool {
					if same(path, m.scopePath) {
						scopeChecks++
					}
					// First check is fast membership, before any read. The
					// second is scope read, after the observation was parsed.
					if !changed && scopeChecks == 2 {
						changed = true
						change()
					}
					return false
				}
			} else {
				change()
			}
			if conversationAllowed(m.scopePath, m.f.a) {
				t.Fatal("overwritten observation accepted")
			}
		})
	}
}

func TestConversationOwnerMediaStrictWireTypes(t *testing.T) {
	for _, which := range []string{"unknown-field", "missing-field", "null-edit", "string-todo", "string-size", "attachment-id-case", "attachment-size-case", "duplicate-key", "trailing-json"} {
		t.Run(which, func(t *testing.T) {
			m := newConversationMediaFixture(t)
			raw, _ := json.Marshal(m.observation)
			v, _ := conversationJSON(raw)
			switch which {
			case "unknown-field":
				v["url"] = "https://example.invalid"
			case "missing-field":
				delete(v, "edited_timestamp")
			case "null-edit":
				v["edited_timestamp"] = nil
			case "string-todo":
				v["todo_id"] = "1"
			case "string-size":
				obj(v["attachments"].([]any)[0])["size"] = "64"
			case "attachment-id-case":
				a := obj(v["attachments"].([]any)[0])
				a["ID"] = a["id"]
				delete(a, "id")
			case "attachment-size-case":
				a := obj(v["attachments"].([]any)[0])
				a["Size"] = a["size"]
				delete(a, "size")
			}
			raw, _ = json.Marshal(v)
			if which == "duplicate-key" {
				raw = append([]byte(`{"domain":"discord-owner-reply-media-v1",`), raw[1:]...)
			}
			if which == "trailing-json" {
				raw = append(raw, []byte(" {}")...)
			}
			stem := "202-" + hashBytes(raw)
			p := filepath.Join(filepath.Dir(m.observationPath), stem+".private.json")
			if os.WriteFile(p, raw, 0600) != nil {
				t.Fatal("fixture write failed")
			}
			if conversationAllowed(p, m.f.a) {
				t.Fatal("ambiguous observation wire type accepted")
			}
		})
	}
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
	m.publishNamed(m.observation.Message, m.scope.Attachment)
}
func (m *conversationMediaFixture) publishNamed(message, attachment string) {
	m.f.t.Helper()
	raw, err := json.MarshalIndent(m.observation, "", "  ")
	if err != nil {
		m.f.t.Fatal("observation fixture invalid")
	}
	dir := filepath.Join(m.f.dir, "owner-media")
	if os.MkdirAll(dir, 0700) != nil {
		m.f.t.Fatal("media fixture directory unavailable")
	}
	stem := message + "-" + hashBytes(raw)
	m.observationPath = filepath.Join(dir, stem+".private.json")
	m.scopePath = filepath.Join(dir, stem+"-"+attachment+".scope.private.json")
	if os.WriteFile(m.observationPath, raw, 0600) != nil {
		m.f.t.Fatal("observation fixture write failed")
	}
	m.f.put(m.scopePath, m.scope)
}

func TestConversationOwnerMediaEditedDiscordTimestamp(t *testing.T) {
	m := newConversationMediaFixture(t)
	created := "2026-10-18T01:00:03.123000+00:00"
	edited := "2026-10-18T01:00:05.456000+00:00"
	m.f.record["created_at"] = created
	m.f.record["edited_at"] = edited
	m.f.record["acquired_at"] = "2026-10-18T01:00:06.123000+00:00"
	m.f.contextEdit(func(c object) { obj(c["owner_replies"].([]any)[0])["timestamp"] = created })
	m.f.stateMatch()
	m.f.publish()
	raw, _ := os.ReadFile(m.f.replyPath)
	m.observation.Created = created
	m.observation.Edited = edited
	m.observation.CanonicalSHA = hashBytes(raw)
	at, _ := time.Parse(time.RFC3339Nano, created)
	m.scope.Created = created
	m.scope.Edited = edited
	m.scope.Since = at.UTC().Format(time.RFC3339Nano)
	m.scope.Until = at.Add(time.Millisecond).UTC().Format(time.RFC3339Nano)
	m.publish()
	if !conversationAllowed(m.observationPath, m.f.a) || !conversationAllowed(m.scopePath, m.f.a) {
		t.Fatal("current edited Discord-format version rejected")
	}
	after, _ := os.ReadFile(m.f.replyPath)
	if string(after) != string(raw) {
		t.Fatal("canonical source changed")
	}
}

func TestConversationOwnerMediaExclusiveBoundMustAlreadyBePast(t *testing.T) {
	m := newConversationMediaFixture(t)
	created, _ := time.Parse(time.RFC3339Nano, m.observation.Created)
	m.f.a.at = created.Add(500 * time.Microsecond)
	m.f.record["acquired_at"] = m.f.a.at.Format(time.RFC3339Nano)
	m.f.stateMatch()
	m.f.publish()
	raw, _ := os.ReadFile(m.f.replyPath)
	m.observation.CanonicalSHA = hashBytes(raw)
	m.publish()
	if !conversationAllowed(m.observationPath, m.f.a) {
		t.Fatal("positive observation version did not qualify")
	}
	if conversationAllowed(m.scopePath, m.f.a) {
		t.Fatal("not-yet-past exclusive bound accepted")
	}
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
