package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type conversationFixture struct {
	t                                                  *testing.T
	a                                                  conversationAccess
	root, dir, replyPath                               string
	cfg, index, q, db, envelope, ledger, record, state object
	rootCalls                                          int
}

func conversationClone(v object) object {
	b, _ := json.Marshal(v)
	m, ok := conversationJSON(b)
	if !ok {
		panic("invalid fixture")
	}
	return m
}
func newConversationFixture(t *testing.T) *conversationFixture {
	t.Helper()
	return newConversationFixtureAt(t, t.TempDir())
}
func newConversationFixtureAt(t *testing.T, base string) *conversationFixture {
	t.Helper()
	// CI service TEMP may use 8.3 names. The production guard deliberately
	// requires physical spelling, so only the synthetic fixture resolves it.
	base, err := physical(base)
	if err != nil {
		t.Fatal("fixture physical base unavailable")
	}
	home := filepath.Join(base, "operational")
	root := filepath.Join(base, "source")
	for _, p := range []string{home, root} {
		if os.MkdirAll(p, 0700) != nil {
			t.Fatal("fixture directory unavailable")
		}
	}
	f := &conversationFixture{t: t, root: root}
	f.a = conversationAccess{home: home, notify: filepath.Join(base, "notify.json"), todo: filepath.Join(base, "todo.json"), sender: filepath.Join(base, "sender.json"), at: time.Date(2026, 10, 18, 1, 0, 10, 0, time.UTC), protected: func(string) bool { return false }}
	f.a.rootAllowed = func(path string) bool { f.rootCalls++; return same(path, root) }
	f.cfg = object{"guild_id": "100", "owner_user_id": "101", "application_id": "102", "task_channel_id": "103"}
	request := "normal-ask-" + strings.Repeat("a", 64)
	sender := "conversation-v1-" + strings.Repeat("b", 32)
	f.dir = filepath.Join(home, "conversations", request)
	f.replyPath = filepath.Join(f.dir, "replies", "202.json")
	decision := object{"todo_id": json.Number("1"), "decision": "direction", "scope": "fixture <scope & scope>", "resources": []any{}}
	body := "fixture question <question & scope>"
	f.envelope = object{"domain": "discord-conversation-question-v1", "request_id": sender, "todo_id": "1", "guild_id": "100", "owner_id": "101", "bot_id": "102", "thread_id": "200", "question_message_id": "200", "progress_thread_id": "201", "question_fingerprint": strings.Repeat("c", 64), "created_at": "2026-10-18T01:00:02Z"}
	raw, _ := json.Marshal(f.envelope)
	f.q = object{"request_id": request, "sender_request_id": sender, "todo_id": json.Number("1"), "project": "fixture-tools", "origin_thread_id": "150", "input_sha256": hashBytes([]byte("original input")), "body_file": filepath.Join(f.dir, "question.txt"), "decision_file": filepath.Join(f.dir, "decision.json"), "title": "fixture", "wait_requested": true, "phase": "waiting", "created_at": "2026-10-18T01:00:01Z", "thread_id": "200", "question_message_id": "200", "progress_thread_id": "201", "question_fingerprint": strings.Repeat("c", 64), "decision_identity_sha256": conversationCanonical(decision), "question_body_sha256": hashBytes([]byte(body)), "question_receipt_file": filepath.Join(f.dir, "question.json"), "question_receipt_sha256": hashBytes(raw)}
	f.index = object{"domain": "discord-conversation-v1", "since": "2026-10-18T01:00:00Z", "destination": conversationClone(f.cfg), "questions": object{request: f.q}}
	f.db = object{"projects": object{"fixture-tools": root}, "items": []any{object{"id": json.Number("1"), "project": "fixture-tools", "status": "block"}}}
	f.ledger = object{"domain": "new-owner-recovery-messages", "version": json.Number("1"), "events": []any{
		object{"event": "intent", "request": object{"request_id": sender, "fingerprint": strings.Repeat("c", 64), "body": body, "notify": true, "destination": object{"guild": "100", "forum": "103", "owner": "101", "request_forum": "104"}, "operation": object{"kind": "ask", "todo_id": json.Number("1"), "origin_thread": "150", "decision_identity": decision, "decision_original": decision}}},
		object{"event": "bot", "request_id": sender, "author_id": "102"},
		object{"event": "verified", "request_id": sender, "phase": "question:0", "receipt": object{"thread_id": "200", "message_id": "200", "author_id": "102", "timestamp": "2026-10-18T01:00:02Z", "flags": json.Number("4"), "owner_mentions": json.Number("1")}},
		object{"event": "verified", "request_id": sender, "phase": "progress", "receipt": object{"thread_id": "201", "message_id": "201", "author_id": "102", "timestamp": "2026-10-18T01:00:02Z", "flags": json.Number("4100"), "owner_mentions": json.Number("0")}},
	}}
	context := object{"domain": "new-owner-recovery-messages", "read_only": true, "source": "discord-api-v10", "request_id": sender, "thread_id": "200", "owner_id": "101", "question_message_id": "200", "question_timestamp": "2026-10-18T01:00:02Z", "progress_thread_id": "201", "original_question": body, "decision_identity": decision, "owner_replies": []any{object{"request_id": sender, "todo_id": json.Number("1"), "question_thread_id": "200", "question_message_id": "200", "author_id": "101", "message_id": "202", "timestamp": "2026-10-18T01:00:03Z", "body": "fixture owner reply", "decision_identity": decision}}}
	contextBytes, _ := json.Marshal(context)
	f.record = object{"thread_id": "200", "guild_id": "100", "record_version": json.Number("2"), "message_id": "202", "author_id": "101", "created_at": "2026-10-18T01:00:03Z", "acquired_at": "2026-10-18T01:00:04Z", "source": "discord-api-v10", "text": "fixture owner reply", "question_context": string(contextBytes), "key": hashBytes([]byte("unattended-owner-v1\x00200\x00202"))[:32], "todo": json.Number("0"), "phase": "conversation_reply_saved", "progress_thread_id": "201", "conversation_request_id": request}
	f.state = object{"domain": "unattended-owner-v1", "since": "2026-10-18T01:00:00Z", "destination": conversationClone(f.cfg), "requests": object{"202": conversationClone(f.record)}}
	f.publish()
	return f
}

func TestConversationFixtureNormalizesShortWindowsTempBase(t *testing.T) {
	base := filepath.Join(t.TempDir(), "fixture directory with a long Windows name")
	if os.MkdirAll(base, 0700) != nil {
		t.Fatal("fixture directory unavailable")
	}
	input, err := syscall.UTF16PtrFromString(base)
	if err != nil {
		t.Fatal("fixture path conversion failed")
	}
	buffer := make([]uint16, 32768)
	n, err := syscall.GetShortPathName(input, &buffer[0], uint32(len(buffer)))
	if err != nil || n == 0 || n >= uint32(len(buffer)) {
		t.Fatal("fixture short path unavailable")
	}
	short := syscall.UTF16ToString(buffer[:n])
	expected, err := physical(short)
	if err != nil {
		t.Fatal("fixture physical path unavailable")
	}
	f := newConversationFixtureAt(t, short)
	if !same(filepath.Dir(f.a.home), expected) || !conversationAllowed(f.replyPath, f.a) {
		t.Fatal("Windows short temporary path was not normalized for the fixture")
	}
}
func (f *conversationFixture) put(path string, v any) {
	f.t.Helper()
	if os.MkdirAll(filepath.Dir(path), 0700) != nil {
		f.t.Fatal("fixture directory unavailable")
	}
	b, e := json.Marshal(v)
	if e != nil || os.WriteFile(path, b, 0600) != nil {
		f.t.Fatal("fixture write failed")
	}
}
func (f *conversationFixture) publish() {
	f.put(f.a.notify, f.cfg)
	f.put(f.a.todo, f.db)
	f.put(f.a.sender, f.ledger)
	f.put(filepath.Join(f.a.home, "conversation-questions.json"), f.index)
	f.put(filepath.Join(f.dir, "question.json"), f.envelope)
	f.put(f.replyPath, f.record)
	f.put(filepath.Join(f.a.home, "unattended-state.json"), f.state)
}
func (f *conversationFixture) contextEdit(edit func(object)) {
	v, ok := conversationJSON([]byte(str(f.record["question_context"])))
	if !ok {
		f.t.Fatal("fixture context invalid")
	}
	edit(v)
	b, _ := json.Marshal(v)
	f.record["question_context"] = string(b)
}
func (f *conversationFixture) stateMatch() {
	obj(f.state["requests"])["202"] = conversationClone(f.record)
}

func TestConversationExactNewQuestionAndReplyQualify(t *testing.T) {
	f := newConversationFixture(t)
	for _, phase := range []string{"posted", "waiting", "answered", "applied", "closed"} {
		f.q["phase"] = phase
		f.publish()
		for _, path := range []string{filepath.Join(f.dir, "question.json"), f.replyPath} {
			if !conversationAllowed(path, f.a) {
				t.Fatal("verified new record rejected")
			}
		}
	}
	if f.rootCalls != 10 {
		t.Fatal("original resource qualification was bypassed")
	}
	// The saved Request predates normal delivery; mutable delivery state may advance.
	r := obj(obj(f.state["requests"])["202"])
	r["todo"] = json.Number("1")
	r["phase"] = "conversation_reply_delivered"
	f.publish()
	if !conversationAllowed(f.replyPath, f.a) {
		t.Fatal("settled delivery state rejected")
	}
}

func TestConversationProvenanceMismatchRemainsProtected(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*conversationFixture)
	}{
		{"unknown-index-domain", func(f *conversationFixture) { f.index["domain"] = "legacy" }},
		{"index-owner", func(f *conversationFixture) { obj(f.index["destination"])["owner_user_id"] = "999" }},
		{"config-bot", func(f *conversationFixture) { f.cfg["application_id"] = "999" }},
		{"reserved", func(f *conversationFixture) { f.q["phase"] = "reserved" }},
		{"missing-project", func(f *conversationFixture) { delete(f.q, "project") }},
		{"fabricated-project", func(f *conversationFixture) { f.q["project"] = "unregistered" }},
		{"project-association", func(f *conversationFixture) { obj(f.db["items"].([]any)[0])["project"] = "other" }},
		{"duplicate-todo", func(f *conversationFixture) {
			f.db["items"] = append(f.db["items"].([]any), conversationClone(obj(f.db["items"].([]any)[0])))
		}},
		{"question-hash", func(f *conversationFixture) { f.q["question_receipt_sha256"] = strings.Repeat("d", 64) }},
		{"question-owner", func(f *conversationFixture) {
			f.envelope["owner_id"] = "999"
			raw, _ := json.Marshal(f.envelope)
			f.q["question_receipt_sha256"] = hashBytes(raw)
		}},
		{"question-todo-number", func(f *conversationFixture) {
			f.envelope["todo_id"] = json.Number("1")
			raw, _ := json.Marshal(f.envelope)
			f.q["question_receipt_sha256"] = hashBytes(raw)
		}},
		{"question-path", func(f *conversationFixture) { f.q["question_receipt_file"] = filepath.Join(f.a.home, "question.json") }},
		{"unknown-sender-domain", func(f *conversationFixture) { f.ledger["domain"] = "lost-ledger" }},
		{"sender-source", func(f *conversationFixture) {
			obj(obj(f.ledger["events"].([]any)[0])["request"])["operation"] = object{"kind": "log"}
		}},
		{"sender-bot", func(f *conversationFixture) { obj(f.ledger["events"].([]any)[1])["author_id"] = "999" }},
		{"sender-question", func(f *conversationFixture) { obj(obj(f.ledger["events"].([]any)[2])["receipt"])["message_id"] = "999" }},
		{"sender-progress", func(f *conversationFixture) { obj(obj(f.ledger["events"].([]any)[3])["receipt"])["author_id"] = "999" }},
		{"sender-origin", func(f *conversationFixture) {
			obj(obj(obj(f.ledger["events"].([]any)[0])["request"])["operation"])["origin_thread"] = "999"
		}},
		{"sender-body", func(f *conversationFixture) { obj(obj(f.ledger["events"].([]any)[0])["request"])["body"] = "changed" }},
		{"source-not-api", func(f *conversationFixture) { f.record["source"] = "html" }},
		{"foreign-owner", func(f *conversationFixture) { f.record["author_id"] = "999"; f.stateMatch() }},
		{"foreign-conversation", func(f *conversationFixture) { f.record["conversation_request_id"] = "other"; f.stateMatch() }},
		{"missing-state-request", func(f *conversationFixture) { f.state["requests"] = object{} }},
		{"pending-state-text", func(f *conversationFixture) { obj(obj(f.state["requests"])["202"])["text"] = "old" }},
		{"pending-state-context", func(f *conversationFixture) { obj(obj(f.state["requests"])["202"])["question_context"] = "old" }},
		{"pending-state-edit", func(f *conversationFixture) {
			obj(obj(f.state["requests"])["202"])["edited_at"] = "2026-10-18T01:00:04Z"
		}},
		{"invalid-todo", func(f *conversationFixture) { f.record["todo"] = "bad"; f.stateMatch() }},
		{"wrong-todo", func(f *conversationFixture) { f.record["todo"] = json.Number("2"); f.stateMatch() }},
		{"context-original", func(f *conversationFixture) {
			f.contextEdit(func(c object) { c["original_question"] = "changed" })
			f.stateMatch()
		}},
		{"context-scope", func(f *conversationFixture) {
			f.contextEdit(func(c object) { obj(c["decision_identity"])["scope"] = "other" })
			f.stateMatch()
		}},
		{"context-reply", func(f *conversationFixture) {
			f.contextEdit(func(c object) { obj(c["owner_replies"].([]any)[0])["body"] = "changed" })
			f.stateMatch()
		}},
		{"context-foreign-owner", func(f *conversationFixture) {
			f.contextEdit(func(c object) { obj(c["owner_replies"].([]any)[0])["author_id"] = "999" })
			f.stateMatch()
		}},
		{"future", func(f *conversationFixture) { f.record["acquired_at"] = "2999-01-01T00:00:00Z"; f.stateMatch() }},
		{"unknown-record-field", func(f *conversationFixture) { f.record["unknown"] = "unknown"; f.stateMatch() }},
		{"unknown-q-field", func(f *conversationFixture) { f.q["unknown"] = "unknown" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newConversationFixture(t)
			c.mutate(f)
			f.publish()
			if conversationAllowed(f.replyPath, f.a) {
				t.Fatal("unverified record accepted")
			}
		})
	}
}

func TestConversationUnknownFilesAreNotAHomeAllowlist(t *testing.T) {
	f := newConversationFixture(t)
	for _, path := range []string{f.a.home, f.dir, filepath.Join(f.a.home, "conversation-questions.json"), filepath.Join(f.dir, "question.txt"), filepath.Join(f.dir, "decision.json"), filepath.Join(f.dir, "latest-replies.json"), filepath.Join(f.dir, "replies", "202.ready.json"), filepath.Join(f.dir, "replies", "202.note.txt"), filepath.Join(f.dir, "replies", "0202.json"), filepath.Join(f.dir, "replies", "999.json"), filepath.Join(f.a.home, "conversations-other", str(f.q["request_id"]), "question.json")} {
		if conversationAllowed(path, f.a) {
			t.Fatal("unknown target accepted")
		}
	}
}

func TestConversationProtectedSourceAndRecursiveRootNeverQualify(t *testing.T) {
	for _, nested := range []bool{false, true} {
		f := newConversationFixture(t)
		calls := 0
		f.a.rootAllowed = func(string) bool { calls++; return false }
		if nested {
			obj(f.db["projects"])["fixture-tools"] = f.dir
			f.publish()
		}
		if conversationAllowed(f.replyPath, f.a) {
			t.Fatal("protected or recursive root accepted")
		}
		if nested && calls != 0 {
			t.Fatal("recursive resource qualification invoked")
		}
		if !nested && calls != 1 {
			t.Fatal("existing source qualification not consulted")
		}
	}
	f := newConversationFixture(t)
	f.a.protected = func(path string) bool { return same(path, f.replyPath) }
	if conversationAllowed(f.replyPath, f.a) {
		t.Fatal("private exclusion bypassed")
	}
}

func TestConversationMalformedDuplicateOversizedAndLinkedRecordsStayProtected(t *testing.T) {
	for _, name := range []string{"index", "state", "envelope", "sender"} {
		t.Run(name, func(t *testing.T) {
			f := newConversationFixture(t)
			p := map[string]string{"index": filepath.Join(f.a.home, "conversation-questions.json"), "state": filepath.Join(f.a.home, "unattended-state.json"), "envelope": filepath.Join(f.dir, "question.json"), "sender": f.a.sender}[name]
			if os.WriteFile(p, []byte("{"), 0600) != nil {
				t.Fatal("fixture write failed")
			}
			if conversationAllowed(f.replyPath, f.a) {
				t.Fatal("corrupt dependency accepted")
			}
		})
	}
	for _, raw := range []string{`{"domain":"a","domain":"b"}`, `{"domain":"a","\u0064omain":"b"}`, `{} {}`, `null`} {
		if _, ok := conversationJSON([]byte(raw)); ok {
			t.Fatal("ambiguous JSON accepted")
		}
	}
	f := newConversationFixture(t)
	if os.WriteFile(f.replyPath, bytesOfSize(1<<20+1), 0600) != nil {
		t.Fatal("fixture write failed")
	}
	if conversationAllowed(f.replyPath, f.a) {
		t.Fatal("oversized record accepted")
	}
	f = newConversationFixture(t)
	real := filepath.Join(f.dir, "actual.json")
	if os.Rename(f.replyPath, real) != nil {
		t.Fatal("fixture move failed")
	}
	if os.Symlink(real, f.replyPath) != nil {
		t.Fatal("fixture symlink unavailable")
	}
	if conversationAllowed(f.replyPath, f.a) {
		t.Fatal("symlink record accepted")
	}
	f = newConversationFixture(t)
	actual := filepath.Join(f.dir, "real-replies")
	if os.Rename(filepath.Dir(f.replyPath), actual) != nil {
		t.Fatal("fixture move failed")
	}
	if os.Symlink(actual, filepath.Dir(f.replyPath)) != nil {
		t.Fatal("fixture symlink unavailable")
	}
	if conversationAllowed(f.replyPath, f.a) {
		t.Fatal("linked parent accepted")
	}
}
func bytesOfSize(n int) []byte { return []byte(strings.Repeat(" ", n)) }

func TestConversationEditedReplyWaitsForMatchingSavedState(t *testing.T) {
	f := newConversationFixture(t)
	f.record["text"] = "edited owner reply"
	f.record["edited_at"] = "2026-10-18T01:00:05Z"
	f.record["acquired_at"] = "2026-10-18T01:00:06Z"
	f.contextEdit(func(c object) { obj(c["owner_replies"].([]any)[0])["body"] = "edited owner reply" })
	f.publish()
	if conversationAllowed(f.replyPath, f.a) {
		t.Fatal("reply published before saved state was accepted")
	}
	f.stateMatch()
	f.publish()
	if !conversationAllowed(f.replyPath, f.a) {
		t.Fatal("settled exact edited reply rejected")
	}
}

func TestConversationChangedIndexDuringQualificationIsNotPermission(t *testing.T) {
	f := newConversationFixture(t)
	f.a.rootAllowed = func(root string) bool {
		f.q["phase"] = "answered"
		f.put(filepath.Join(f.a.home, "conversation-questions.json"), f.index)
		return same(root, f.root)
	}
	if conversationAllowed(f.replyPath, f.a) {
		t.Fatal("changed provenance snapshot accepted")
	}
	f.a.rootAllowed = func(root string) bool { return same(root, f.root) }
	if !conversationAllowed(f.replyPath, f.a) {
		t.Fatal("settled provenance snapshot rejected")
	}
}

func TestConversationLegacyExclusionStillWinsForSourceAndEvidence(t *testing.T) {
	for _, which := range []string{"source", "sender", "notify", "state", "question"} {
		t.Run(which, func(t *testing.T) {
			f := newConversationFixture(t)
			excluded := map[string]string{"source": f.root, "sender": f.a.sender, "notify": f.a.notify, "state": filepath.Join(f.a.home, "unattended-state.json"), "question": filepath.Join(f.dir, "question.json")}[which]
			f.a.protected = func(path string) bool { return same(path, excluded) }
			if conversationAllowed(f.replyPath, f.a) {
				t.Fatal("existing private exclusion bypassed")
			}
		})
	}
}

func TestConversationCanonicalScopeKeepsExactNumbersAndUnescapedHTML(t *testing.T) {
	v, ok := conversationJSON([]byte(`{"z":"<&>","a":9007199254740993}`))
	if !ok || conversationCanonical(v) != hashBytes([]byte(`{"a":9007199254740993,"z":"<&>"}`)) {
		t.Fatal("canonical scope differs from the independent Go receipt contract")
	}
}

func TestConversationCanonicalQuestionCoexistsWithAliasesLogsAndTopics(t *testing.T) {
	f := newConversationFixture(t)
	aliasID := "normal-ask-" + strings.Repeat("d", 64)
	original := conversationClone(f.q)
	original["request_id"] = aliasID
	original["sender_request_id"] = "conversation-v1-" + strings.Repeat("e", 32)
	aliasDir := filepath.Join(f.a.home, "conversations", aliasID)
	original["body_file"] = filepath.Join(aliasDir, "question.txt")
	original["decision_file"] = filepath.Join(aliasDir, "decision.json")
	original["input_sha256"] = hashBytes([]byte("distinct retained original intent"))
	original["phase"] = "reserved"
	for _, key := range []string{"thread_id", "question_message_id", "progress_thread_id", "question_fingerprint", "decision_identity_sha256", "question_body_sha256", "question_receipt_file", "question_receipt_sha256"} {
		delete(original, key)
	}
	f.index["aliases"] = object{aliasID: object{
		"target_request_id": f.q["request_id"], "input_sha256": original["input_sha256"], "original_intent": original,
	}}
	f.index["logs"] = object{"fixture-log": object{
		"input_sha256": hashBytes([]byte("fixture log")), "body_file": filepath.Join(f.a.home, "conversation-logs", "fixture-log.txt"),
		"destination_thread_id": "201", "message_id": "203", "verified_thread_id": "201",
	}}
	f.index["topics"] = object{"fixture-topic": "201"}
	f.publish()
	if len(obj(f.index["questions"])) != 1 {
		t.Fatal("fixture must retain one canonical question")
	}
	for _, path := range []string{filepath.Join(f.dir, "question.json"), f.replyPath} {
		if !conversationAllowed(path, f.a) {
			t.Fatal("canonical record rejected when valid index metadata coexists")
		}
	}
	// Retaining another request's intent grants no access to its directory.
	f.put(filepath.Join(aliasDir, "question.json"), f.envelope)
	f.put(filepath.Join(aliasDir, "replies", "202.json"), f.record)
	for _, path := range []string{filepath.Join(aliasDir, "question.json"), filepath.Join(aliasDir, "replies", "202.json")} {
		if conversationAllowed(path, f.a) {
			t.Fatal("alias intent became resource access authority")
		}
	}
}

func TestConversationMutableDeliveryAndNoticeFieldsPreserveReadableReply(t *testing.T) {
	f := newConversationFixture(t)
	f.state["conversation_notice_turn"] = true
	f.state["conversation_report_cursor"] = "203"
	r := obj(obj(f.state["requests"])["202"])
	before, err := os.ReadFile(f.replyPath)
	if err != nil {
		t.Fatal("fixture read failed")
	}
	for _, phase := range []string{"conversation_reply_saved", "conversation_reply_delivered", "conversation_reply_applied"} {
		for _, reported := range []string{"", "conversation_delivered", "conversation_applied"} {
			r["todo"] = json.Number("1")
			r["phase"] = phase
			r["reported"] = reported
			f.publish()
			if !conversationAllowed(f.replyPath, f.a) {
				t.Fatal("mutable delivery metadata blocked an exact owner reply")
			}
			after, err := os.ReadFile(f.replyPath)
			if err != nil || hashBytes(after) != hashBytes(before) {
				t.Fatal("guard or metadata update changed immutable reply bytes")
			}
		}
	}
	// Controlled provenance still has to agree when mutable delivery fields differ.
	r["origin"] = object{"unexpected": "different controlled provenance"}
	f.publish()
	if conversationAllowed(f.replyPath, f.a) {
		t.Fatal("mutable metadata bypassed controlled origin matching")
	}
}
