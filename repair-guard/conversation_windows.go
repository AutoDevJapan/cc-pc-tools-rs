package main

// This is a reader for two exact new operational records, not a new resource registry.
import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const conversationHome = "D:/CC/Tools/discord-ask/unattended"
const conversationNotify = "C:/Users/makhmeto/.godd/discord-notify.json"

var conversationRequestID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,79}$`)
var conversationSenderID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)
var conversationSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

type conversationAccess struct {
	home, notify, todo, sender string
	rootAllowed                func(string) bool
	protected                  func(string) bool
	at                         time.Time
}

func conversationPathAllowed(path string) bool {
	return conversationAllowed(path, conversationAccess{
		home: conversationHome, notify: conversationNotify, todo: productStore,
		sender:      filepath.Join(filepath.Dir(conversationHome), "recovery-messages.json"),
		rootAllowed: repairOnlyAllowed, protected: legacyMatch, at: time.Now(),
	})
}

// Reject links at every component, including junctions whose target stays inside home.
func conversationPhysical(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	for part := filepath.Clean(path); ; part = filepath.Dir(part) {
		info, err := os.Lstat(part)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok && data.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return false
		}
		if filepath.Dir(part) == part {
			break
		}
	}
	resolved, err := physical(path)
	return err == nil && same(path, resolved)
}

func conversationBytes(path string, limit int64, protected func(string) bool) ([]byte, bool) {
	if protected(path) || !conversationPhysical(path) {
		return nil, false
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() > limit {
		return nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, false
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	after, statErr := os.Lstat(path)
	if err != nil || int64(len(b)) > limit || statErr != nil || !os.SameFile(opened, after) || !conversationPhysical(path) || protected(path) {
		return nil, false
	}
	return b, true
}

// Build one tree with exact numbers and reject duplicate keys before maps erase them.
func conversationJSON(data []byte) (object, bool) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value func(int) (any, bool)
	value = func(depth int) (any, bool) {
		if depth > 64 {
			return nil, false
		}
		token, err := d.Token()
		if err != nil {
			return nil, false
		}
		delim, compound := token.(json.Delim)
		if !compound {
			return token, true
		}
		switch delim {
		case '{':
			out := object{}
			for d.More() {
				key, err := d.Token()
				name, ok := key.(string)
				if err != nil || !ok {
					return nil, false
				}
				if _, exists := out[name]; exists {
					return nil, false
				}
				v, ok := value(depth + 1)
				if !ok {
					return nil, false
				}
				out[name] = v
			}
			end, err := d.Token()
			return out, err == nil && end == json.Delim('}')
		case '[':
			out := []any{}
			for d.More() {
				v, ok := value(depth + 1)
				if !ok {
					return nil, false
				}
				out = append(out, v)
			}
			end, err := d.Token()
			return out, err == nil && end == json.Delim(']')
		}
		return nil, false
	}
	v, ok := value(0)
	if !ok {
		return nil, false
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, false
	}
	m, ok := v.(map[string]any)
	return m, ok && m != nil
}
func conversationNumber(v any) uint64 {
	n, ok := v.(json.Number)
	if !ok {
		return 0
	}
	i, err := strconv.ParseUint(string(n), 10, 64)
	if err != nil {
		return 0
	}
	return i
}
func conversationID(v any) bool {
	s, ok := v.(string)
	if !ok || s == "" || s[0] == '0' {
		return false
	}
	for _, b := range []byte(s) {
		if b < '0' || b > '9' {
			return false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return err == nil && n > 0
}
func conversationCanonical(v any) string {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if e.Encode(v) != nil {
		return ""
	}
	return hashBytes(bytes.TrimSuffix(b.Bytes(), []byte("\n")))
}
func conversationTime(v any, at time.Time) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, str(v))
	return t, err == nil && t.Unix() > 0 && !t.After(at)
}
func conversationKnown(m object, keys string) bool {
	for key := range m {
		if !strings.Contains(" "+keys+" ", " "+key+" ") {
			return false
		}
	}
	return true
}
func conversationDestination(m, cfg object) bool {
	for _, key := range []string{"guild_id", "task_channel_id", "owner_user_id", "application_id"} {
		if !conversationID(cfg[key]) || m[key] != cfg[key] {
			return false
		}
	}
	return cfg["owner_user_id"] != cfg["application_id"]
}

// Dependency paths and source qualification are injectable only in in-process tests.
// Production always uses the fixed paths above and repairOnlyAllowed(root).
func conversationAllowed(path string, a conversationAccess) bool {
	path, err := portable(path)
	if err != nil || !filepath.IsAbs(path) {
		return false
	}
	path = filepath.Clean(path)
	container := filepath.Join(a.home, "conversations")
	if !within(container, path) {
		return false
	}
	rel, err := filepath.Rel(container, path)
	if err != nil {
		return false
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) < 2 || !conversationRequestID.MatchString(parts[0]) {
		return false
	}
	questionFile := len(parts) == 2 && parts[1] == "question.json"
	replyID := ""
	if len(parts) == 3 && parts[1] == "replies" && strings.HasSuffix(parts[2], ".json") {
		replyID = strings.TrimSuffix(parts[2], ".json")
	}
	if !questionFile && !conversationID(replyID) {
		return false
	}
	// Fast membership rejection happens before any private state is read.
	if a.rootAllowed == nil || a.protected == nil || a.protected(path) || !conversationPhysical(path) {
		return false
	}
	type snapshot struct {
		path, sha string
		limit     int64
	}
	var snapshots []snapshot
	read := func(p string, limit int64) (object, string, bool) {
		b, ok := conversationBytes(p, limit, a.protected)
		if !ok {
			return nil, "", false
		}
		m, ok := conversationJSON(b)
		if !ok {
			return nil, "", false
		}
		sha := hashBytes(b)
		snapshots = append(snapshots, snapshot{p, sha, limit})
		return m, sha, true
	}
	cfg, _, ok := read(a.notify, 1<<20)
	if !ok {
		return false
	}
	index, _, ok := read(filepath.Join(a.home, "conversation-questions.json"), 8<<20)
	if !ok || index["domain"] != "discord-conversation-v1" || !conversationDestination(obj(index["destination"]), cfg) {
		return false
	}
	if !conversationKnown(index, "domain since destination cursor questions aliases logs topics") {
		return false
	}
	since, ok := conversationTime(index["since"], a.at)
	if !ok || since.Before(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)) {
		return false
	}
	q := obj(obj(index["questions"])[parts[0]])
	if !conversationKnown(q, "request_id sender_request_id todo_id project origin_thread_id input_sha256 body_file decision_file title wait_requested phase created_at thread_id question_message_id progress_thread_id question_fingerprint decision_identity_sha256 question_body_sha256 question_receipt_file question_receipt_sha256 reply_cursor attachments attachment_sha256") {
		return false
	}
	if q["request_id"] != parts[0] || !conversationSenderID.MatchString(str(q["sender_request_id"])) || conversationNumber(q["todo_id"]) == 0 || str(q["project"]) == "" || strings.EqualFold(str(q["project"]), "owner") {
		return false
	}
	switch q["phase"] {
	case "posted", "waiting", "answered", "applied", "closed":
	default:
		return false
	}
	created, ok := conversationTime(q["created_at"], a.at)
	if !ok || created.Before(since) {
		return false
	}
	if !conversationID(q["origin_thread_id"]) || !conversationID(q["thread_id"]) || !conversationID(q["progress_thread_id"]) || q["thread_id"] != q["question_message_id"] || q["thread_id"] == q["progress_thread_id"] {
		return false
	}
	for _, key := range []string{"input_sha256", "question_fingerprint", "decision_identity_sha256", "question_body_sha256", "question_receipt_sha256"} {
		if !conversationSHA.MatchString(str(q[key])) {
			return false
		}
	}
	dir := filepath.Join(container, parts[0])
	for field, name := range map[string]string{"body_file": "question.txt", "decision_file": "decision.json", "question_receipt_file": "question.json"} {
		if !filepath.IsAbs(str(q[field])) || !same(str(q[field]), filepath.Join(dir, name)) {
			return false
		}
	}
	db, _, ok := read(a.todo, 64<<20)
	if !ok {
		return false
	}
	var row object
	count := 0
	rows, ok := db["items"].([]any)
	if !ok {
		return false
	}
	for _, v := range rows {
		r := obj(v)
		if conversationNumber(r["id"]) == conversationNumber(q["todo_id"]) {
			row = r
			count++
		}
	}
	if count != 1 || row["project"] != q["project"] {
		return false
	}
	root := str(obj(db["projects"])[str(q["project"])])
	// Reject self-authorizing roots before calling allowed, including physical aliases.
	if !filepath.IsAbs(root) || within(a.home, filepath.Clean(root)) || !conversationPhysical(root) || a.protected(root) {
		return false
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() || !a.rootAllowed(root) {
		return false
	}
	question, sha, ok := read(filepath.Join(dir, "question.json"), 1<<20)
	if !ok || sha != q["question_receipt_sha256"] || !conversationQuestion(question, q, cfg, a.at) {
		return false
	}
	ledger, _, ok := read(a.sender, 16<<20)
	if !ok {
		return false
	}
	intent, ok := conversationSender(ledger, q, cfg, question)
	if !ok {
		return false
	}
	// The sender's independently saved original body/scope binds the readable context.
	if hashBytes([]byte(str(intent["body"]))) != q["question_body_sha256"] || conversationCanonical(obj(intent["operation"])["decision_identity"]) != q["decision_identity_sha256"] {
		return false
	}
	if !questionFile {
		record, _, ok := read(path, 1<<20)
		if !ok {
			return false
		}
		state, _, ok := read(filepath.Join(a.home, "unattended-state.json"), 8<<20)
		if !ok || state["domain"] != "unattended-owner-v1" || !conversationDestination(obj(state["destination"]), cfg) || !conversationReply(record, obj(obj(state["requests"])[replyID]), q, cfg, question, replyID, a.at) {
			return false
		}
	}
	// A concurrent private-file/index/state update is not permission. Retry after it settles.
	for _, s := range snapshots {
		b, ok := conversationBytes(s.path, s.limit, a.protected)
		if !ok || hashBytes(b) != s.sha {
			return false
		}
	}
	return !a.protected(path) && !a.protected(root) && conversationPhysical(path) && conversationPhysical(root)
}

func conversationQuestion(receipt, q, cfg object, at time.Time) bool {
	const fields = "domain request_id todo_id guild_id owner_id bot_id thread_id question_message_id progress_thread_id question_fingerprint created_at"
	if len(receipt) != 11 || !conversationKnown(receipt, fields) || receipt["domain"] != "discord-conversation-question-v1" {
		return false
	}
	for _, v := range receipt {
		if _, ok := v.(string); !ok {
			return false
		}
	}
	for _, key := range []string{"todo_id", "guild_id", "owner_id", "bot_id", "thread_id", "question_message_id", "progress_thread_id"} {
		if !conversationID(receipt[key]) {
			return false
		}
	}
	if receipt["todo_id"] != strconv.FormatUint(conversationNumber(q["todo_id"]), 10) || receipt["request_id"] != q["sender_request_id"] || receipt["guild_id"] != cfg["guild_id"] || receipt["owner_id"] != cfg["owner_user_id"] || receipt["bot_id"] != cfg["application_id"] {
		return false
	}
	for _, key := range []string{"thread_id", "question_message_id", "progress_thread_id", "question_fingerprint"} {
		if receipt[key] != q[key] {
			return false
		}
	}
	_, ok := conversationTime(receipt["created_at"], at)
	return ok
}

func conversationSender(ledger, q, cfg, question object) (object, bool) {
	if ledger["domain"] != "new-owner-recovery-messages" || conversationNumber(ledger["version"]) != 1 {
		return nil, false
	}
	events, ok := ledger["events"].([]any)
	if !ok {
		return nil, false
	}
	var intent object
	intents, bots, questions, progress := 0, 0, 0, 0
	for _, raw := range events {
		e := obj(raw)
		if e["event"] == "intent" && obj(e["request"])["request_id"] == q["sender_request_id"] {
			intent = obj(e["request"])
			intents++
		}
		if e["request_id"] != q["sender_request_id"] {
			continue
		}
		switch e["event"] {
		case "bot":
			if e["author_id"] != cfg["application_id"] {
				return nil, false
			}
			bots++
		case "origin_corrected", "origin_repair":
			return nil, false
		case "verified":
			r := obj(e["receipt"])
			if e["phase"] == "question:0" {
				if r["thread_id"] != q["thread_id"] || r["message_id"] != q["question_message_id"] || r["author_id"] != cfg["application_id"] || r["timestamp"] != question["created_at"] {
					return nil, false
				}
				questions++
			} else if e["phase"] == "progress" {
				if r["thread_id"] != q["progress_thread_id"] || !conversationID(r["message_id"]) || r["author_id"] != cfg["application_id"] || conversationNumber(r["flags"])&4096 == 0 || conversationNumber(r["owner_mentions"]) != 0 {
					return nil, false
				}
				progress++
			}
		}
	}
	if intents != 1 || bots != 1 || questions != 1 || progress != 1 || intent["fingerprint"] != q["question_fingerprint"] || intent["notify"] != true {
		return nil, false
	}
	if !conversationQuestionImages(q, intent) {
		return nil, false
	}
	if body, ok := intent["body"].(string); !ok || body == "" {
		return nil, false
	}
	dst, op := obj(intent["destination"]), obj(intent["operation"])
	if dst["guild"] != cfg["guild_id"] || dst["forum"] != cfg["task_channel_id"] || dst["owner"] != cfg["owner_user_id"] || op["kind"] != "ask" || conversationNumber(op["todo_id"]) != conversationNumber(q["todo_id"]) || op["origin_thread"] != q["origin_thread_id"] || conversationNumber(obj(op["decision_identity"])["todo_id"]) != conversationNumber(q["todo_id"]) {
		return nil, false
	}
	return intent, true
}

// Paths here are private retry metadata, never a grant to read those images.
// Successful sender receipts must bind every declared image hash in order.
func conversationQuestionImages(q, intent object) bool {
	paths, hasPaths := q["attachments"].([]any)
	hashes, hasHashes := q["attachment_sha256"].([]any)
	images, hasImages := intent["images"].([]any)
	if q["attachments"] == nil && q["attachment_sha256"] == nil {
		return !hasImages || len(images) == 0
	}
	if !hasPaths || !hasHashes || !hasImages || len(paths) == 0 || len(paths) > 10 || len(paths) != len(hashes) || len(images) != len(hashes) {
		return false
	}
	for i, path := range paths {
		text, ok := path.(string)
		if !ok || !filepath.IsAbs(text) || !conversationSHA.MatchString(str(hashes[i])) || obj(images[i])["sha256"] != hashes[i] {
			return false
		}
	}
	return true
}

func conversationReply(record, saved, q, cfg, question object, message string, at time.Time) bool {
	if len(saved) == 0 || record["source"] != "discord-api-v10" || record["conversation_request_id"] != q["request_id"] || record["message_id"] != message || record["thread_id"] != q["thread_id"] || record["guild_id"] != cfg["guild_id"] || record["author_id"] != cfg["owner_user_id"] || record["progress_thread_id"] != q["progress_thread_id"] || conversationNumber(record["record_version"]) != 2 {
		return false
	}
	if !conversationID(message) {
		return false
	}
	n, _ := strconv.ParseUint(message, 10, 64)
	starter, _ := strconv.ParseUint(str(q["thread_id"]), 10, 64)
	if n <= starter {
		return false
	}
	if !conversationKnown(record, "thread_id guild_id record_version message_id author_id created_at edited_at reply_to_message_id reply_to_channel_id reply_to_guild_id acquired_at source text question_context key todo phase reported progress_thread_id prior_versions prior_edits origin conversation_request_id") {
		return false
	}
	for _, key := range []string{"created_at", "acquired_at", "text", "question_context"} {
		if _, ok := record[key].(string); !ok {
			return false
		}
	}
	key := hashBytes([]byte("unattended-owner-v1\x00" + str(q["thread_id"]) + "\x00" + message))[:32]
	if record["key"] != key {
		return false
	}
	for _, key := range []string{"source", "conversation_request_id", "message_id", "thread_id", "guild_id", "author_id", "progress_thread_id", "record_version", "key", "created_at", "edited_at", "acquired_at", "text", "question_context", "reply_to_message_id", "reply_to_channel_id", "reply_to_guild_id", "prior_versions", "prior_edits", "origin"} {
		if !reflect.DeepEqual(record[key], saved[key]) {
			return false
		}
	}
	for _, r := range []object{record, saved} {
		if r["todo"] != nil {
			todo, ok := r["todo"].(json.Number)
			if !ok {
				return false
			}
			n, err := strconv.ParseUint(string(todo), 10, 64)
			if err != nil || (n != 0 && n != conversationNumber(q["todo_id"])) {
				return false
			}
		}
		switch r["phase"] {
		case "conversation_reply_saved", "conversation_reply_delivered", "conversation_reply_applied":
		default:
			return false
		}
	}
	created, ok := conversationTime(record["created_at"], at)
	if !ok {
		return false
	}
	questionAt, ok := conversationTime(question["created_at"], at)
	if !ok || created.Before(questionAt) {
		return false
	}
	acquired, ok := conversationTime(record["acquired_at"], at)
	if !ok || acquired.Before(created) {
		return false
	}
	if record["edited_at"] != nil {
		edited, ok := conversationTime(record["edited_at"], at)
		if !ok || edited.Before(created) || edited.After(acquired) {
			return false
		}
	}
	if record["reply_to_channel_id"] != nil && record["reply_to_channel_id"] != q["thread_id"] {
		return false
	}
	if record["reply_to_guild_id"] != nil && record["reply_to_guild_id"] != cfg["guild_id"] {
		return false
	}
	context, ok := conversationJSON([]byte(str(record["question_context"])))
	if !ok || context["domain"] != "new-owner-recovery-messages" || context["read_only"] != true || context["source"] != "discord-api-v10" || context["request_id"] != q["sender_request_id"] || context["thread_id"] != q["thread_id"] || context["question_message_id"] != q["question_message_id"] || context["progress_thread_id"] != q["progress_thread_id"] || context["owner_id"] != cfg["owner_user_id"] || context["question_timestamp"] != question["created_at"] || hashBytes([]byte(str(context["original_question"]))) != q["question_body_sha256"] || conversationCanonical(context["decision_identity"]) != q["decision_identity_sha256"] {
		return false
	}
	replies, ok := context["owner_replies"].([]any)
	if !ok {
		return false
	}
	matches := 0
	seen := map[string]bool{}
	for _, raw := range replies {
		r := obj(raw)
		id := str(r["message_id"])
		if !conversationID(id) || seen[id] || r["request_id"] != q["sender_request_id"] || conversationNumber(r["todo_id"]) != conversationNumber(q["todo_id"]) || r["question_thread_id"] != q["thread_id"] || r["question_message_id"] != q["question_message_id"] || r["author_id"] != cfg["owner_user_id"] || conversationCanonical(r["decision_identity"]) != q["decision_identity_sha256"] {
			return false
		}
		seen[id] = true
		when, ok := conversationTime(r["timestamp"], at)
		number, _ := strconv.ParseUint(id, 10, 64)
		if !ok || when.Before(questionAt) || number <= starter {
			return false
		}
		if id == message {
			if r["body"] != record["text"] || r["timestamp"] != record["created_at"] {
				return false
			}
			matches++
		}
	}
	return matches == 1
}
