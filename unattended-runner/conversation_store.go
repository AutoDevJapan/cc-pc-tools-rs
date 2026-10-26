package main

// The current conversation index is a new operational namespace. The existing
// sender remains responsible for durable POST reservations and API readback.
// Nothing here reconstructs or opens the lost legacy question state.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

const conversationDomain = "discord-conversation-v1"
const conversationSender = "D:/CC/Tools/discord-ask/discord_ask.exe"
const conversationTodo = "D:/CC/Tools/todo/todo.exe"

type ConversationAskInput struct {
	RequestID, OriginThread, BodyFile, DecisionFile, Title string
	Todo                                                   uint64
	ActorTodo                                              uint64
	ActorJob                                               string
	Wait                                                   bool
	Attachments                                            []string
}

// All public fields are receipt metadata. Question and answer text stays in the
// private sender/receipt files, never in command output or health reports.
type ConversationQuestion struct {
	originTodo       uint64 // Only a transient projection for the recipient transaction.
	deliveryActor    *conversationActor
	Actor            *conversationActor `json:"actor,omitempty"`
	WaitDeferred     bool               `json:"wait_deferred,omitempty"`
	ExistingURL      string             `json:"-"`
	RequestID        string             `json:"request_id"`
	SenderID         string             `json:"sender_request_id"`
	Todo             uint64             `json:"todo_id"`
	Project          string             `json:"project"`
	OriginThread     string             `json:"origin_thread_id"`
	InputSHA         string             `json:"input_sha256"`
	BodyFile         string             `json:"body_file"`
	DecisionFile     string             `json:"decision_file"`
	Title            string             `json:"title,omitempty"`
	Wait             bool               `json:"wait_requested"`
	Phase            string             `json:"phase"`
	Created          string             `json:"created_at"`
	Thread           string             `json:"thread_id,omitempty"`
	Message          string             `json:"question_message_id,omitempty"`
	Progress         string             `json:"progress_thread_id,omitempty"`
	Fingerprint      string             `json:"question_fingerprint,omitempty"`
	DecisionSHA      string             `json:"decision_identity_sha256,omitempty"`
	QuestionSHA      string             `json:"question_body_sha256,omitempty"`
	ReceiptFile      string             `json:"question_receipt_file,omitempty"`
	ReceiptSHA       string             `json:"question_receipt_sha256,omitempty"`
	ReplyCursor      string             `json:"reply_cursor,omitempty"`
	Attachments      []string           `json:"attachments,omitempty"`
	AttachmentHashes []string           `json:"attachment_sha256,omitempty"`
}

type conversationIndex struct {
	Domain      string                           `json:"domain"`
	Since       string                           `json:"since"`
	Destination Destination                      `json:"destination"`
	Cursor      string                           `json:"cursor,omitempty"`
	Questions   map[string]ConversationQuestion  `json:"questions"`
	Aliases     map[string]conversationAlias     `json:"aliases,omitempty"`
	Logs        map[string]conversationLogRecord `json:"logs,omitempty"`
	Topics      map[string]string                `json:"topics,omitempty"`
}

type conversationLogRecord struct {
	InputSHA        string `json:"input_sha256"`
	BodyFile        string `json:"body_file"`
	Thread          string `json:"destination_thread_id,omitempty"`
	Message         string `json:"message_id,omitempty"`
	VerifiedThread  string `json:"verified_thread_id,omitempty"`
	EffectiveNotify *bool  `json:"effective_notify,omitempty"`
}

type conversationAlias struct {
	Target      string                `json:"target_request_id"`
	InputSHA    string                `json:"input_sha256"`
	Intent      *ConversationQuestion `json:"original_intent,omitempty"`
	ImagePaths  []string              `json:"image_paths,omitempty"`
	ImageHashes []string              `json:"image_sha256,omitempty"`
}

type conversationQuestionReceipt struct {
	Domain      string `json:"domain"`
	RequestID   string `json:"request_id"`
	Todo        string `json:"todo_id"`
	Guild       string `json:"guild_id"`
	Owner       string `json:"owner_id"`
	Bot         string `json:"bot_id"`
	Thread      string `json:"thread_id"`
	Message     string `json:"question_message_id"`
	Progress    string `json:"progress_thread_id"`
	Fingerprint string `json:"question_fingerprint"`
	Created     string `json:"created_at"`
}

type conversationProof struct {
	Domain           string                              `json:"domain"`
	ReadOnly         bool                                `json:"read_only"`
	Source           string                              `json:"source"`
	RequestID        string                              `json:"request_id"`
	Thread           string                              `json:"thread_id"`
	Owner            string                              `json:"owner_id"`
	Question         string                              `json:"question_message_id"`
	Created          string                              `json:"question_timestamp"`
	Progress         string                              `json:"progress_thread_id"`
	Body             string                              `json:"original_question"`
	Decision         json.RawMessage                     `json:"decision_identity"`
	OriginalDecision json.RawMessage                     `json:"original_decision"`
	Replies          []conversationReply                 `json:"owner_replies"`
	ReplyVersions    map[string]conversationReplyVersion `json:"reply_versions,omitempty"`
}

type conversationReplyVersion struct {
	Edited *string `json:"edited_timestamp"`
}

type conversationReply struct {
	RequestID string          `json:"request_id"`
	Todo      uint64          `json:"todo_id"`
	Thread    string          `json:"question_thread_id"`
	Question  string          `json:"question_message_id"`
	Author    string          `json:"author_id"`
	Message   string          `json:"message_id"`
	Created   string          `json:"timestamp"`
	Body      string          `json:"body"`
	Decision  json.RawMessage `json:"decision_identity"`
}

var conversationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,79}$`)
var shaPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func canonicalHash(value any) string {
	// Decode raw JSON first so object key order is the same across languages.
	b, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	var normalized any
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.UseNumber()
	if decoder.Decode(&normalized) != nil {
		return ""
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if encoder.Encode(normalized) != nil {
		return ""
	}
	return bytesHash(bytes.TrimSuffix(buffer.Bytes(), []byte("\n")))
}

func boundedRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("conversation input unavailable")
	}
	b, err := os.ReadFile(path)
	if err != nil || len(b) > 1<<20 {
		return nil, errors.New("conversation input unavailable")
	}
	return b, nil
}

func writeConversationOnce(path string, data []byte) error {
	if previous, err := boundedRegular(path); err == nil {
		if string(previous) != string(data) {
			return errors.New("conversation immutable input differs")
		}
		return nil
	} else if !os.IsNotExist(err) {
		// boundedRegular deliberately has a fixed public error. Distinguish absent
		// from malformed/unreadable state without treating either as empty.
		if _, statErr := os.Lstat(path); !os.IsNotExist(statErr) {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".conversation-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	// A flushed complete file is linked without overwriting an existing receipt.
	// A crash before this point leaves only an unused temporary file.
	if err = os.Link(f.Name(), path); err == nil {
		return nil
	}
	if previous, readErr := boundedRegular(path); readErr == nil && string(previous) == string(data) {
		return nil
	}
	return err
}

func (r *Runner) conversationPath() string {
	return filepath.Join(r.Config.Home, "conversation-questions.json")
}
func (r *Runner) loadConversations() (*conversationIndex, error) {
	index := &conversationIndex{Domain: conversationDomain, Since: r.Now().UTC().Format(time.RFC3339Nano), Destination: r.State.Destination, Questions: map[string]ConversationQuestion{}}
	info, err := os.Lstat(r.conversationPath())
	if os.IsNotExist(err) {
		return index, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("conversation index unavailable")
	}
	var raw json.RawMessage
	if err = readJSON(r.conversationPath(), maxState, &raw); err != nil {
		return nil, errors.New("conversation index invalid")
	}
	index = &conversationIndex{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(index) != nil {
		return nil, errors.New("conversation index invalid")
	}
	stamp, err := time.Parse(time.RFC3339Nano, index.Since)
	if err != nil || stamp.After(r.Now().Add(time.Minute)) || index.Domain != conversationDomain || index.Destination != r.State.Destination || index.Questions == nil {
		return nil, errors.New("conversation namespace differs")
	}
	for key, q := range index.Questions {
		if q.Actor != nil && (q.Todo != 1171 || q.Project != "GitDo" || q.Actor.Todo != 3277 || !validJob(q.Actor.Job) || !shaPattern.MatchString(q.Actor.SpecSHA) || !shaPattern.MatchString(q.Actor.OriginSpecSHA) || !shaPattern.MatchString(q.Actor.RootSHA) || !shaPattern.MatchString(q.Actor.SourceSHA)) {
			return nil, errors.New("conversation actor unverified")
		}
		if q.WaitDeferred && (q.Actor == nil || !q.Wait) {
			return nil, errors.New("conversation actor unverified")
		}
		if key != q.RequestID || !conversationID.MatchString(key) || q.SenderID == "" || q.Todo == 0 || q.Project == "" || !shaPattern.MatchString(q.InputSHA) || q.Created == "" {
			return nil, errors.New("conversation question invalid")
		}
		dir := filepath.Join(r.Config.Home, "conversations", key)
		if q.BodyFile != filepath.Join(dir, "question.txt") || q.DecisionFile != filepath.Join(dir, "decision.json") {
			return nil, errors.New("conversation private paths differ")
		}
		if !conversationQuestionImageRecord(q) {
			return nil, errors.New("conversation question images invalid")
		}
		if q.Phase != "reserved" {
			if q.Thread != q.Message || !shaPattern.MatchString(q.Fingerprint) || !shaPattern.MatchString(q.DecisionSHA) || !shaPattern.MatchString(q.QuestionSHA) || !shaPattern.MatchString(q.ReceiptSHA) {
				return nil, errors.New("conversation receipt invalid")
			}
			for _, id := range []string{q.Thread, q.Progress} {
				if _, ok := number(id); !ok {
					return nil, errors.New("conversation receipt identity invalid")
				}
			}
			if q.ReceiptFile != filepath.Join(dir, "question.json") {
				return nil, errors.New("conversation receipt path differs")
			}
		}
		switch q.Phase {
		case "reserved", "posted", "waiting", "answered", "applied", "closed", "withdrawn":
		default:
			return nil, errors.New("conversation phase invalid")
		}
	}
	if index.Aliases == nil {
		index.Aliases = map[string]conversationAlias{}
	}
	for key, alias := range index.Aliases {
		target, exists := index.Questions[alias.Target]
		if !conversationID.MatchString(key) || key == alias.Target || !exists || target.RequestID != alias.Target || !shaPattern.MatchString(alias.InputSHA) {
			return nil, errors.New("conversation alias invalid")
		}
	}
	if index.Logs == nil {
		index.Logs = map[string]conversationLogRecord{}
	}
	if index.Topics == nil {
		index.Topics = map[string]string{}
	}
	for key, record := range index.Logs {
		if !conversationID.MatchString(key) || !shaPattern.MatchString(record.InputSHA) || record.BodyFile != filepath.Join(r.Config.Home, "conversation-logs", key+".txt") {
			return nil, errors.New("conversation log record invalid")
		}
	}
	for _, thread := range index.Topics {
		if _, ok := number(thread); !ok {
			return nil, errors.New("conversation topic identity invalid")
		}
	}
	return index, nil
}

func (r *Runner) conversationTask(ctx context.Context, id uint64) (*TodoRow, error) {
	var db struct {
		Projects map[string]string `json:"projects"`
		Items    []json.RawMessage `json:"items"`
	}
	if readJSON(r.TodoPath, maxTodo, &db) != nil {
		return nil, errors.New("conversation task scope unavailable")
	}
	var selected json.RawMessage
	var project string
	for _, raw := range db.Items {
		var header struct {
			ID      uint64 `json:"id"`
			Project string `json:"project"`
		}
		if json.Unmarshal(raw, &header) != nil {
			return nil, errors.New("conversation task unavailable")
		}
		if header.ID == id {
			if selected != nil {
				return nil, errors.New("conversation duplicate task")
			}
			selected, project = raw, header.Project
		}
	}
	root := db.Projects[project]
	if selected == nil || project == "owner" || !filepath.IsAbs(root) {
		return nil, errors.New("conversation task scope unavailable")
	}
	raw, err := r.Run(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", root)
	var proof struct {
		Client     *bool `json:"client_resource"`
		Unverified *bool `json:"classification_unverified"`
	}
	if err != nil || json.Unmarshal([]byte(raw), &proof) != nil || proof.Client == nil || *proof.Client || proof.Unverified == nil || *proof.Unverified {
		return nil, errors.New("conversation task scope unverified")
	}
	var row TodoRow
	if json.Unmarshal(selected, &row) != nil {
		return nil, errors.New("conversation task unavailable")
	}
	row.conversationState = selected
	row.conversationRoot = root
	row.conversationScope = conversationTodoScope{Domain: "discord-conversation-todo-scope-v1", Todo: fmt.Sprint(id), Project: project, RootSHA: bytesHash([]byte(root))}
	return &row, nil
}

func (r *Runner) conversationAsk(ctx context.Context, input ConversationAskInput) (ConversationQuestion, error) {
	var zero ConversationQuestion
	if !conversationID.MatchString(input.RequestID) || input.Todo == 0 {
		return zero, errors.New("conversation identity invalid")
	}
	if _, ok := number(input.OriginThread); !ok {
		return zero, errors.New("conversation origin invalid")
	}
	row, err := r.conversationTask(ctx, input.Todo)
	if err != nil {
		return zero, err
	}
	// Verify a separate recipient before reading the private question inputs.
	actor, err := r.conversationActorForAsk(ctx, input, row)
	if err != nil {
		return zero, err
	}
	if (row.Status == "done" || row.Status == "recovery_unverified") && actor == nil {
		return zero, errors.New("conversation original task unavailable")
	}
	body, err := boundedRegular(input.BodyFile)
	if err != nil || len(body) > 128<<10 || len(strings.TrimSpace(string(body))) == 0 {
		return zero, errors.New("conversation question unavailable")
	}
	decision, err := boundedRegular(input.DecisionFile)
	if err != nil {
		return zero, err
	}
	if len(decision) > 128<<10 {
		return zero, errors.New("conversation decision limit exceeded")
	}
	var decisionValue map[string]json.RawMessage
	if json.Unmarshal(decision, &decisionValue) != nil {
		return zero, errors.New("conversation decision invalid")
	}
	var todo uint64
	if json.Unmarshal(decisionValue["todo_id"], &todo) != nil || todo != input.Todo {
		return zero, errors.New("conversation decision task differs")
	}
	if safeText(string(body), r.Token) != string(body) || safeText(string(decision), r.Token) != string(decision) {
		return zero, errors.New("conversation input contains credential material")
	}
	index, err := r.loadConversations()
	if err != nil {
		return zero, err
	}
	paths, hashes, err := conversationQuestionRetryImages(index, input)
	if err != nil {
		return zero, err
	}
	identity := []any{input.Todo, input.OriginThread, bytesHash(body), canonicalHash(json.RawMessage(decision)), input.Title, input.Wait}
	if len(hashes) != 0 {
		identity = append(identity, hashes)
	}
	inputHash := canonicalHash(identity)
	q, exists := index.Questions[input.RequestID]
	if exists && !sameConversationActor(q.Actor, actor) {
		return zero, errors.New("conversation actor differs")
	}
	if alias, exists := index.Aliases[input.RequestID]; exists {
		if alias.InputSHA != inputHash {
			return zero, errors.New("conversation alias retry input differs")
		}
		canonical := index.Questions[alias.Target]
		if !sameConversationActor(canonical.Actor, actor) {
			return zero, errors.New("conversation actor differs")
		}
		if canonical.Project != row.Project {
			return zero, errors.New("conversation original project differs")
		}
		return canonical, nil
	}
	if exists && q.Project != row.Project {
		return zero, errors.New("conversation original project differs")
	}
	if exists && q.InputSHA != inputHash {
		return zero, errors.New("conversation retry input differs")
	}
	if !exists {
		for _, old := range index.Questions {
			if old.Project != row.Project || old.Todo != input.Todo || old.Phase == "reserved" || old.Phase == "withdrawn" || slices.Equal(old.AttachmentHashes, hashes) {
				continue
			}
			original, readErr := boundedRegular(old.DecisionFile)
			if readErr != nil {
				return zero, readErr
			}
			if canonicalHash(json.RawMessage(original)) != canonicalHash(json.RawMessage(decision)) {
				continue
			}
			if _, proofErr := r.conversationProof(ctx, old); proofErr != nil {
				return zero, proofErr
			}
			old.Phase, old.Wait = "duplicate", false
			old.ExistingURL = "https://discord.com/channels/" + r.State.Destination.Guild + "/" + old.Thread
			return old, nil
		}
	}
	if !exists {
		for _, old := range index.Questions {
			if old.InputSHA == inputHash {
				if !sameConversationActor(old.Actor, actor) {
					return zero, errors.New("conversation actor differs")
				}
				q = old
				exists = true
				if old.RequestID != input.RequestID {
					index.Aliases[input.RequestID] = conversationAlias{Target: old.RequestID, InputSHA: inputHash, ImagePaths: paths, ImageHashes: hashes}
					if err = publish(r.conversationPath(), index); err != nil {
						return zero, err
					}
				}
				break
			}
		}
	}
	if !exists {
		if actor == nil && row.Status != "doing" && row.Status != "todo" {
			return zero, errors.New("conversation task is already held")
		}
		dir := filepath.Join(r.Config.Home, "conversations", input.RequestID)
		q = ConversationQuestion{RequestID: input.RequestID, SenderID: "conversation-v1-" + bytesHash([]byte(input.RequestID))[:32], Todo: input.Todo, Project: row.Project, OriginThread: input.OriginThread, InputSHA: inputHash, BodyFile: filepath.Join(dir, "question.txt"), DecisionFile: filepath.Join(dir, "decision.json"), Title: input.Title, Wait: input.Wait, Phase: "reserved", Created: r.Now().UTC().Format(time.RFC3339Nano)}
		q.Actor = actor
		if actor != nil {
			if len(actor.proof) == 0 {
				return zero, errors.New("conversation actor unverified")
			}
			if err = writeConversationOnce(filepath.Join(dir, "actor-succession.json"), actor.proof); err != nil {
				return zero, err
			}
		}
		q.Attachments, q.AttachmentHashes = paths, hashes
		if err = writeConversationOnce(q.BodyFile, body); err != nil {
			return zero, err
		}
		if err = writeConversationOnce(q.DecisionFile, decision); err != nil {
			return zero, err
		}
		index.Questions[q.RequestID] = q
		if err = publish(r.conversationPath(), index); err != nil {
			return zero, err
		}
	}
	if q.Phase != "reserved" && !(q.Wait && q.Phase == "posted") {
		if q.Project != row.Project {
			return zero, errors.New("conversation original project differs")
		}
		return q, nil
	}
	if q.Project != row.Project {
		return zero, errors.New("conversation original project differs")
	}
	if q.Phase == "reserved" {
		if q.Actor != nil {
			if _, err := r.conversationActorRow(ctx, q); err != nil {
				return zero, err
			}
		}
		if q.Actor == nil && row.Status != "doing" && row.Status != "todo" {
			return zero, errors.New("conversation reserved task is held")
		}
		args := []string{"recovery-ask", "--request-id", q.SenderID, "--todo-id", fmt.Sprint(q.Todo), "--origin-thread", q.OriginThread, "--body-file", q.BodyFile, "--decision-file", q.DecisionFile}
		if q.Actor != nil && q.RequestID == fixedConversationOrigin.RequestID {
			args = append(args, "--conversation-source-request", q.RequestID)
		}
		if q.Title != "" {
			args = append(args, "--title", q.Title)
		}
		for _, path := range q.Attachments {
			args = append(args, "--attach", path)
		}
		for _, sha := range q.AttachmentHashes {
			args = append(args, "--expected-image-sha256", sha)
		}
		raw, callErr := r.Run(ctx, conversationSender, args...)
		if callErr != nil {
			if strings.HasPrefix(callErr.Error(), "conversation sender ") {
				return zero, callErr
			}
			return zero, errors.New("conversation send pending; retry the same request")
		}
		var report struct {
			Domain, Delivery string
			Outcome          string          `json:"outcome"`
			ExistingRequest  string          `json:"existing_request_id"`
			URL              string          `json:"url"`
			ExistingVerified bool            `json:"existing_metadata_verified"`
			Todo             uint64          `json:"todo_id"`
			Request          string          `json:"request_id"`
			Thread           string          `json:"thread_id"`
			Message          string          `json:"message_id"`
			Progress         string          `json:"progress_thread_id"`
			Fingerprint      string          `json:"original_fingerprint"`
			Body             string          `json:"original_question"`
			Decision         json.RawMessage `json:"decision"`
			Images           []string        `json:"image_sha256"`
		}
		decodeErr := json.Unmarshal([]byte(raw), &report)
		if decodeErr == nil && report.Domain == "new-owner-recovery-messages" && report.Outcome == "existing_question_images_differ" && report.Delivery == "not_sent" && report.Request == q.SenderID && report.Todo == q.Todo && report.ExistingRequest != q.SenderID && conversationID.MatchString(report.ExistingRequest) {
			if report.Thread != "" {
				if _, ok := number(report.Thread); !ok || !report.ExistingVerified || report.URL != "https://discord.com/channels/"+r.State.Destination.Guild+"/"+report.Thread {
					return zero, errors.New("conversation duplicate proof invalid")
				}
			} else if report.URL != "" || report.ExistingVerified {
				return zero, errors.New("conversation duplicate proof invalid")
			}
			delete(index.Questions, q.RequestID)
			for id, alias := range index.Aliases {
				if alias.Target == q.RequestID {
					delete(index.Aliases, id)
				}
			}
			if err = publish(r.conversationPath(), index); err != nil {
				return zero, err
			}
			result := q
			for _, old := range index.Questions {
				if old.SenderID == report.ExistingRequest {
					result = old
					break
				}
			}
			result.Phase, result.Wait, result.ExistingURL = "duplicate", false, report.URL
			return result, nil
		}
		if decodeErr != nil || report.Domain != "new-owner-recovery-messages" || report.Delivery != "verified" || report.Thread != report.Message || report.Request == "" || !shaPattern.MatchString(report.Fingerprint) {
			return zero, errors.New("conversation send verification pending")
		}
		if len(report.Images) != len(q.AttachmentHashes) || (len(report.Images) != 0 && canonicalHash(report.Images) != canonicalHash(q.AttachmentHashes)) {
			return zero, errors.New("conversation question image verification pending")
		}
		for _, id := range []string{report.Thread, report.Progress} {
			if _, ok := number(id); !ok {
				return zero, errors.New("conversation destination invalid")
			}
		}
		q.SenderID = report.Request
		q.Thread = report.Thread
		q.Message = report.Message
		q.Progress = report.Progress
		q.Fingerprint = report.Fingerprint
		q.DecisionSHA = canonicalHash(report.Decision)
		q.QuestionSHA = bytesHash([]byte(report.Body))
		for id, canonical := range index.Questions {
			if id == q.RequestID || canonical.Phase == "reserved" {
				continue
			}
			if canonical.SenderID == q.SenderID || canonical.Thread == q.Thread {
				if !sameConversationActor(canonical.Actor, q.Actor) {
					return zero, errors.New("conversation actor differs")
				}
				if canonical.SenderID != q.SenderID || canonical.Thread != q.Thread || canonical.Todo != q.Todo || canonical.Project != q.Project || canonical.Fingerprint != q.Fingerprint || canonical.QuestionSHA != q.QuestionSHA || canonical.DecisionSHA != q.DecisionSHA || !slices.Equal(canonical.AttachmentHashes, report.Images) {
					return zero, errors.New("conversation canonical question differs")
				}
				intent := index.Questions[q.RequestID]
				index.Aliases[q.RequestID] = conversationAlias{Target: canonical.RequestID, InputSHA: q.InputSHA, Intent: &intent, ImagePaths: q.Attachments, ImageHashes: q.AttachmentHashes}
				delete(index.Questions, q.RequestID)
				if err = publish(r.conversationPath(), index); err != nil {
					return zero, err
				}
				return canonical, nil
			}
		}
		proof, err := r.conversationProof(ctx, q)
		if err != nil {
			return zero, err
		}
		receipt := conversationQuestionReceipt{Domain: "discord-conversation-question-v1", RequestID: q.SenderID, Todo: fmt.Sprint(q.Todo), Guild: r.State.Destination.Guild, Owner: r.State.Destination.Owner, Bot: r.State.Destination.Bot, Thread: q.Thread, Message: q.Message, Progress: q.Progress, Fingerprint: q.Fingerprint, Created: proof.Created}
		b, _ := json.MarshalIndent(receipt, "", "  ")
		q.ReceiptFile = filepath.Join(r.Config.Home, "conversations", q.RequestID, "question.json")
		q.ReceiptSHA = bytesHash(b)
		if err = writeConversationOnce(q.ReceiptFile, b); err != nil {
			return zero, err
		}
		q.Phase = "posted"
		index.Questions[q.RequestID] = q
		if err = publish(r.conversationPath(), index); err != nil {
			return zero, err
		}
	}
	if q.Wait && q.Phase == "posted" {
		if q.Actor != nil {
			return r.conversationActorWait(ctx, index, q)
		}
		scopeFile, scopeErr := r.conversationScopeFile(row, q)
		if scopeErr != nil {
			return zero, scopeErr
		}
		if _, err = r.Run(ctx, conversationTodo, "question-wait", fmt.Sprint(q.Todo), "--question-file", q.ReceiptFile, "--scope-file", scopeFile); err != nil {
			return zero, errors.New("conversation posted; task wait recording pending")
		}
		current, err := r.todo(q.Todo)
		if err == nil && current.Project == q.Project && conversationQuestionAnswered(current, q) {
			q.Phase = "answered"
			index.Questions[q.RequestID] = q
			if err = publish(r.conversationPath(), index); err != nil {
				return zero, err
			}
			return q, nil
		}
		if err != nil || current.Status != "block" || !conversationWaitRecorded(current, q) {
			return zero, errors.New("conversation task wait verification pending")
		}
		q.Phase = "waiting"
		index.Questions[q.RequestID] = q
		if err = publish(r.conversationPath(), index); err != nil {
			return zero, err
		}
	}
	return q, nil
}

func conversationWaitRecorded(row *TodoRow, q ConversationQuestion) bool {
	var value struct {
		SHA      string `json:"question_receipt_sha256"`
		Question struct {
			RequestID string `json:"request_id"`
		} `json:"question"`
	}
	return json.Unmarshal(row.QuestionWait, &value) == nil && value.SHA == q.ReceiptSHA && value.Question.RequestID == q.SenderID
}

func conversationAnswerRecorded(row *TodoRow, q ConversationQuestion, answer conversationAnswerReceipt, b []byte) bool {
	matched := 0
	for _, raw := range row.QuestionAnswers {
		var value struct {
			SHA    string                    `json:"answer_file_sha256"`
			Answer conversationAnswerReceipt `json:"answer"`
			Wait   struct {
				SHA string `json:"question_receipt_sha256"`
			} `json:"wait"`
		}
		if json.Unmarshal(raw, &value) == nil && value.SHA == bytesHash(b) && value.Wait.SHA == q.ReceiptSHA && value.Answer == answer {
			matched++
		}
	}
	return matched == 1
}

func conversationQuestionAnswered(row *TodoRow, q ConversationQuestion) bool {
	for _, raw := range row.QuestionAnswers {
		var receipt struct {
			Wait struct {
				SHA      string `json:"question_receipt_sha256"`
				Question struct {
					Request string `json:"request_id"`
				} `json:"question"`
			} `json:"wait"`
			Answer conversationAnswerReceipt `json:"answer"`
		}
		if json.Unmarshal(raw, &receipt) == nil && receipt.Wait.SHA == q.ReceiptSHA && receipt.Wait.Question.Request == q.SenderID && receipt.Answer.RequestID == q.SenderID && receipt.Answer.Todo == fmt.Sprint(q.Todo) && receipt.Answer.Thread == q.Thread && receipt.Answer.Question == q.Message && receipt.Answer.QuestionSHA == q.ReceiptSHA {
			return true
		}
	}
	return false
}

func (r *Runner) conversationProof(ctx context.Context, q ConversationQuestion) (*conversationProof, error) {
	raw, err := r.Run(ctx, conversationSender, "recovery-replies", "--thread", q.Thread, "--message", q.Message, "--request-id", q.SenderID, "--todo-id", fmt.Sprint(q.Todo), "--read-only")
	if err != nil {
		return nil, errors.New("conversation reply verification deferred")
	}
	var proof conversationProof
	if json.Unmarshal([]byte(raw), &proof) != nil || proof.Domain != "new-owner-recovery-messages" || !proof.ReadOnly || proof.Source != "discord-api-v10" || proof.RequestID != q.SenderID || proof.Thread != q.Thread || proof.Owner != r.State.Destination.Owner || proof.Question != q.Message || proof.Progress != q.Progress || canonicalHash(proof.Decision) != q.DecisionSHA || bytesHash([]byte(proof.Body)) != q.QuestionSHA {
		return nil, errors.New("conversation question association differs")
	}
	stamp, err := time.Parse(time.RFC3339Nano, proof.Created)
	if err != nil || stamp.After(r.Now().Add(time.Minute)) {
		return nil, errors.New("conversation question timestamp invalid")
	}
	seen := map[string]bool{}
	for _, reply := range proof.Replies {
		n, valid := number(reply.Message)
		before, _ := number(q.Message)
		at, e := time.Parse(time.RFC3339Nano, reply.Created)
		if !valid || n <= before || seen[reply.Message] || e != nil || at.Before(stamp) || at.After(r.Now().Add(time.Minute)) || reply.RequestID != q.SenderID || reply.Todo != q.Todo || reply.Thread != q.Thread || reply.Question != q.Message || reply.Author != r.State.Destination.Owner || canonicalHash(reply.Decision) != q.DecisionSHA {
			return nil, errors.New("conversation owner reply association differs")
		}
		seen[reply.Message] = true
		if _, ok := proof.ReplyVersions[reply.Message]; !ok {
			// Older senders return the original scope and full body but omit edit
			// metadata. Complete it by the same exact owner-post GET instead of
			// silently treating an unknown version as an unchanged answer.
			var live Message
			if err := r.get(ctx, "/channels/"+q.Thread+"/messages/"+reply.Message, &live); err != nil || live.ID != reply.Message || live.Channel != q.Thread || live.Author.ID != proof.Owner || live.Author.Bot || live.Content != reply.Body || live.Timestamp != reply.Created {
				return nil, errors.New("conversation reply version verification deferred")
			}
			version := conversationReplyVersion{}
			if live.Edited != "" {
				stamp, err := time.Parse(time.RFC3339Nano, live.Edited)
				if err != nil || stamp.Before(at) || stamp.After(r.Now().Add(time.Minute)) {
					return nil, errors.New("conversation reply edit time invalid")
				}
				version.Edited = &live.Edited
			}
			if proof.ReplyVersions == nil {
				proof.ReplyVersions = map[string]conversationReplyVersion{}
			}
			proof.ReplyVersions[reply.Message] = version
		}
	}
	return &proof, nil
}

func (r *Runner) conversationLog(ctx context.Context, requestID, title, bodyFile, thread string, notify bool, attachments []string) (map[string]any, error) {
	return r.conversationLogInner(ctx, requestID, title, bodyFile, thread, notify, attachments, "", false)
}

func (r *Runner) conversationClarification(ctx context.Context, requestID, title, bodyFile, thread, manifest string) (map[string]any, error) {
	if thread == "" || manifest == "" {
		return nil, errors.New("clarification requires an exact original destination and binding")
	}
	return r.conversationLogInner(ctx, requestID, title, bodyFile, thread, false, nil, manifest, false)
}

func (r *Runner) conversationCheckClarification(ctx context.Context, requestID, title, bodyFile, thread, manifest string) (map[string]any, error) {
	if thread == "" || manifest == "" {
		return nil, errors.New("exact clarification input required")
	}
	return r.conversationLogInner(ctx, requestID, title, bodyFile, thread, false, nil, manifest, true)
}

func (r *Runner) conversationLogInner(ctx context.Context, requestID, title, bodyFile, thread string, notify bool, attachments []string, clarification string, checkOnly bool) (map[string]any, error) {
	if checkOnly && clarification == "" {
		return nil, errors.New("clarification purpose required for check-only")
	}
	if !conversationID.MatchString(requestID) || strings.TrimSpace(title) == "" {
		return nil, errors.New("conversation log identity invalid")
	}
	body, err := boundedRegular(bodyFile)
	if err != nil {
		return nil, err
	}
	if len(body) > 128<<10 {
		return nil, errors.New("conversation report limit exceeded")
	}
	attachmentHashes := make([]string, 0, len(attachments))
	for _, attachment := range attachments {
		info, statErr := os.Lstat(attachment)
		if statErr != nil || !info.Mode().IsRegular() || info.Size() > 24<<20 {
			return nil, errors.New("conversation attachment unavailable")
		}
		file, openErr := os.Open(attachment)
		if openErr != nil {
			return nil, errors.New("conversation attachment unavailable")
		}
		data, readErr := io.ReadAll(io.LimitReader(file, (24<<20)+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || len(data) > 24<<20 {
			return nil, errors.New("conversation attachment unavailable")
		}
		attachmentHashes = append(attachmentHashes, bytesHash(data))
	}
	inputHash := canonicalHash([]any{title, bytesHash(body), thread, notify, attachmentHashes})
	index, err := r.loadConversations()
	if err != nil {
		return nil, err
	}
	if index.Logs == nil {
		index.Logs = map[string]conversationLogRecord{}
	}
	if index.Topics == nil {
		index.Topics = map[string]string{}
	}
	record, exists := index.Logs[requestID]
	if exists && record.InputSHA != inputHash {
		return nil, errors.New("conversation log retry input differs")
	}
	// The sender authenticates the original question/current owner reply before
	// this layer saves a new input. Keep original ID/hash semantics on retries.
	var binding []byte
	if clarification != "" {
		if notify || len(attachments) != 0 || thread == "" || exists && record.Thread != thread {
			return nil, errors.New("clarification requires the exact silent text-only destination")
		}
		if _, valid := number(thread); !valid {
			return nil, errors.New("clarification destination invalid")
		}
		if exists {
			saved, readErr := boundedRegular(record.BodyFile)
			if readErr != nil || bytesHash(saved) != bytesHash(body) {
				return nil, errors.New("saved clarification body differs")
			}
		}
		binding, err = boundedRegular(clarification)
		if err != nil || len(binding) > 16<<10 {
			return nil, errors.New("clarification binding unavailable")
		}
		senderID := "conversation-log-" + bytesHash([]byte(requestID))[:32]
		raw, checkErr := r.Run(ctx, conversationSender, "recovery-log", "--request-id", senderID, "--title", title,
			"--body-file", bodyFile, "--thread", thread, "--clarification-file", clarification, "--check-only")
		var checked map[string]any
		if checkErr != nil || json.Unmarshal([]byte(raw), &checked) != nil || checked["domain"] != "new-owner-recovery-messages" || checked["check_only"] != true || checked["delivery"] != "not_sent" || checked["request_id"] != senderID || checked["source_body_sha256"] != bytesHash(body) || checked["binding_sha256"] != bytesHash(binding) {
			return nil, errors.New("clarification verification pending")
		}
		if checkOnly {
			return map[string]any{"domain": conversationDomain, "request_id": requestID, "delivery": "not_sent", "check_only": true,
				"original_intent_retained": true, "source_body_sha256": checked["source_body_sha256"], "body_sha256": checked["body_sha256"], "binding_sha256": checked["binding_sha256"]}, nil
		}
	}
	if !exists {
		destination := thread
		if destination == "" {
			destination = index.Topics[title]
		}
		record = conversationLogRecord{InputSHA: inputHash, BodyFile: filepath.Join(r.Config.Home, "conversation-logs", requestID+".txt"), Thread: destination}
		if err = writeConversationOnce(record.BodyFile, body); err != nil {
			return nil, err
		}
		index.Logs[requestID] = record
		if err = publish(r.conversationPath(), index); err != nil {
			return nil, err
		}
	}
	args := []string{"recovery-log", "--request-id", "conversation-log-" + bytesHash([]byte(requestID))[:32], "--title", title, "--body-file", record.BodyFile}
	if clarification != "" {
		saved, readErr := boundedRegular(record.BodyFile)
		if readErr != nil || bytesHash(saved) != bytesHash(body) {
			return nil, errors.New("saved clarification body differs")
		}
		path := filepath.Join(r.Config.Home, "conversation-clarifications", requestID+"-"+bytesHash(binding)+".json")
		if err = writeConversationOnce(path, binding); err != nil {
			return nil, err
		}
		args = append(args, "--clarification-file", path)
	}
	if record.Thread != "" {
		if _, ok := number(record.Thread); !ok {
			return nil, errors.New("conversation log destination invalid")
		}
		args = append(args, "--thread", record.Thread)
	}
	if notify {
		args = append(args, "--notify")
	}
	for _, attachment := range attachments {
		args = append(args, "--attach", attachment)
	}
	raw, err := r.Run(ctx, conversationSender, args...)
	if err != nil {
		if clarification != "" {
			return nil, errors.New("clarification delivery incomplete; inspect the current owner reply and saved delivery before retrying; keep the original request unchanged")
		}
		return nil, errors.New("conversation log pending; retry the same request")
	}
	var report map[string]any
	if json.Unmarshal([]byte(raw), &report) != nil || report["domain"] != "new-owner-recovery-messages" || report["delivery"] != "verified" {
		return nil, errors.New("conversation log verification pending")
	}
	verifiedThread, _ := report["thread_id"].(string)
	message, _ := report["message_id"].(string)
	if _, ok := number(verifiedThread); !ok {
		return nil, errors.New("conversation log receipt destination invalid")
	}
	if _, ok := number(message); !ok {
		return nil, errors.New("conversation log receipt identity invalid")
	}
	if (record.Thread != "" && record.Thread != verifiedThread) || (record.Message != "" && (record.Message != message || record.VerifiedThread != verifiedThread)) {
		return nil, errors.New("conversation log receipt differs")
	}
	record.VerifiedThread = verifiedThread
	record.Message = message
	if effective, present := report["effective_notify"]; present {
		value, ok := effective.(bool)
		if !ok {
			return nil, errors.New("conversation log notification receipt invalid")
		}
		record.EffectiveNotify = &value
	}
	index.Logs[requestID] = record
	if thread == "" {
		index.Topics[title] = verifiedThread
	}
	if err = publish(r.conversationPath(), index); err != nil {
		return nil, err
	}
	result := map[string]any{"domain": conversationDomain, "delivery": "verified", "request_id": requestID, "thread_id": report["thread_id"], "message_id": report["message_id"], "url": report["url"], "reused": report["reused"]}
	if record.EffectiveNotify != nil {
		result["effective_notify"] = *record.EffectiveNotify
	}
	return result, nil
}

func (r *Runner) conversationReplies(ctx context.Context, requestID string) (map[string]any, error) {
	index, err := r.loadConversations()
	if err != nil {
		return nil, err
	}
	q, ok := index.Questions[requestID]
	if !ok || q.Phase == "reserved" {
		return nil, errors.New("conversation question unavailable")
	}
	proof, err := r.conversationProof(ctx, q)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(r.Config.Home, "conversations", q.RequestID, "latest-replies.json")
	if err = publish(path, proof); err != nil {
		return nil, err
	}
	return map[string]any{"domain": conversationDomain, "request_id": q.RequestID, "todo_id": q.Todo, "replies_verified": len(proof.Replies), "receipt_file": path, "delivery": "inspection_only"}, nil
}

func (r *Runner) conversationStatus() (map[string]any, error) {
	index, err := r.loadConversations()
	if err != nil {
		return nil, err
	}
	phases := map[string]int{}
	questions := []map[string]any{}
	for _, key := range orderedConversationKeys(index) {
		q := index.Questions[key]
		phases[q.Phase]++
		questions = append(questions, conversationQuestionPublic(q))
	}
	replies := []map[string]any{}
	intake := []map[string]any{}
	ids := make([]string, 0, len(r.State.Requests))
	for id := range r.State.Requests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		req := r.State.Requests[id]
		if req == nil || req.Author != r.State.Destination.Owner {
			continue
		}
		intake = append(intake, map[string]any{"message_id": req.Message, "thread_id": req.Thread, "author_id": req.Author, "todo_id": req.Todo, "request_id": req.Conversation, "phase": req.Phase, "source": req.Source, "created_at": req.Created, "edited_at": req.Edited, "acquired_at": req.Acquired, "content_sha256": bytesHash([]byte(req.Text)), "progress_thread_id": req.ProgressThread})
	}
	ids = ids[:0]
	all := r.conversationRequests()
	for id := range all {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		req := all[id]
		if req == nil || req.Author != r.State.Destination.Owner || req.Conversation == "" {
			continue
		}
		q, ok := index.Questions[req.Conversation]
		if !ok || req.Thread != q.Thread || (req.Todo != q.Todo && !(req.Todo == 0 && (req.Phase == "conversation_implementation_recorded" || req.Phase == "conversation_reply_applied" || req.Phase == "conversation_source_verified"))) {
			continue
		}
		replies = append(replies, map[string]any{"message_id": req.Message, "thread_id": req.Thread, "author_id": req.Author, "todo_id": req.Todo, "origin_todo_id": q.Todo, "request_id": req.Conversation, "phase": req.Phase, "source": req.Source, "created_at": req.Created, "edited_at": req.Edited, "acquired_at": req.Acquired})
	}
	return map[string]any{"domain": conversationDomain, "questions": len(index.Questions), "phases": phases, "items": questions, "reply_stages": replies, "intake_stages": intake, "stage_evidence": "saved_state_only", "snapshot_consistency": "independent_saved_files", "legacy_state": "unchanged", "original_registry_recovered": false}, nil
}

func orderedConversationKeys(index *conversationIndex) []string {
	ids := make([]string, 0, len(index.Questions))
	for id := range index.Questions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (r *Runner) conversationForThread(thread string) (string, error) {
	index, err := r.loadConversations()
	if err != nil {
		return "", err
	}
	found := ""
	for id, q := range index.Questions {
		if q.Phase != "reserved" && q.Thread == thread {
			if found != "" {
				return "", errors.New("conversation thread has multiple canonical owners")
			}
			found = id
		}
	}
	return found, nil
}
