package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const taskQuestionDomain = "owner-task-question-metadata-v1"

type taskQuestionRepository struct {
	ID       uint64 `json:"id"`
	OwnerID  uint64 `json:"owner_id"`
	FullName string `json:"full_name"`
}

type taskQuestionEvidence struct {
	Domain            string                 `json:"domain"`
	Request           string                 `json:"request_id"`
	Origin            string                 `json:"origin_thread_id"`
	Title             string                 `json:"title"`
	Wait              bool                   `json:"wait"`
	Scope             generatedQuestionScope `json:"scope"`
	Repository        taskQuestionRepository `json:"repository"`
	Session           string                 `json:"session"`
	Rollout           string                 `json:"rollout_path"`
	SessionSHA        string                 `json:"session_record_sha256"`
	Root              string                 `json:"task_root"`
	RepositoryRequest taskQuestionTurn       `json:"repository_request"`
	TaskRequest       taskQuestionTurn       `json:"task_request"`
	Creation          nativeQuestionEvent    `json:"task_creation"`
	Continuation      taskQuestionTurn       `json:"continuation"`
	Body              string                 `json:"body_path"`
	Decision          string                 `json:"decision_path"`
	Media             string                 `json:"media_evidence_path"`
	MediaSHA          string                 `json:"media_evidence_sha256"`
	Created           time.Time              `json:"created_at"`
	Expires           time.Time              `json:"expires_at"`
}

// Relevant original metadata, not mutable progress/notes. Status and explicit
// stops are checked separately on every registration and consumption.
type taskQuestionState struct {
	current    json.RawMessage
	ID         uint64          `json:"id"`
	Project    string          `json:"project"`
	Title      string          `json:"title"`
	Root       string          `json:"root"`
	Created    float64         `json:"created_s"`
	Spec       json.RawMessage `json:"spec"`
	Jobs       []string        `json:"jobs"`
	Generation json.RawMessage `json:"resume_generation"`
	Worker     struct {
		Job        string
		Generation uint64
	} `json:"worker_receipt"`
	Scheduler struct {
		Job string
		Key json.RawMessage
	} `json:"scheduler"`
}

type taskQuestionReceipt struct {
	Evidence string `json:"evidence_path"`
	SHA      string `json:"evidence_sha256"`
	State    string `json:"original_metadata_sha256"`
}

type taskQuestionRegistry struct {
	Keys     generatedQuestionRegistry
	Sessions string
	Read     func(string, int64) ([]byte, error)
	Guard    func(context.Context, string) (bool, error)
	Clear    func(string) bool
	Records  func(string, []generatedEventBound) ([]json.RawMessage, error)
	Task     func(context.Context, uint64) (taskQuestionState, error)
	Repo     func(context.Context, taskQuestionRepository) bool
	Dst      func(generatedQuestionScope) bool
	Root     func(context.Context, string) bool
	Media    func(context.Context, string) (generatedQuestionBundle, error)
	Now      func() time.Time
}

func taskQuestionMAC(key, payload []byte) string {
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte(taskQuestionDomain + "\x00"))
	_, _ = h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

func (r taskQuestionRegistry) guarded(ctx context.Context, path string, limit int64) ([]byte, error) {
	ok, err := r.Guard(ctx, path)
	if err != nil || !ok || !r.Clear(path) {
		return nil, errConversationMedia
	}
	return r.Read(path, limit)
}

func (r taskQuestionRegistry) check(ctx context.Context, path string) (taskQuestionEvidence, taskQuestionReceipt, error) {
	return r.checkForQuestion(ctx, path, nil)
}

func (r taskQuestionRegistry) checkForQuestion(ctx context.Context, path string, question *ConversationQuestion) (taskQuestionEvidence, taskQuestionReceipt, error) {
	e, receipt := taskQuestionEvidence{}, taskQuestionReceipt{}
	bad := func(stage string) (taskQuestionEvidence, taskQuestionReceipt, error) {
		return taskQuestionEvidence{}, taskQuestionReceipt{}, generatedFailure(stage, "task_question_metadata_unverified")
	}
	path, err := conversationMediaPath(path)
	if err != nil {
		return bad("task_source_evidence")
	}
	b, err := r.guarded(ctx, path, 128<<10)
	if err != nil || historyStrictJSON(b, &e) != nil {
		return bad("task_source_evidence")
	}
	if e.Domain != taskQuestionDomain || !conversationMediaNormal.MatchString(e.Request) || !strings.HasPrefix(e.Request, "normal-ask-") || e.Scope.Todo == 0 || !generatedUUID.MatchString(e.Session) || !shaPattern.MatchString(e.SessionSHA) || !shaPattern.MatchString(e.Scope.Body) || !shaPattern.MatchString(e.Scope.Decision) || !shaPattern.MatchString(e.MediaSHA) || strings.TrimSpace(e.Title) == "" || !generatedWithin(r.Sessions, e.Rollout) || !strings.HasSuffix(filepath.Base(filepath.FromSlash(e.Rollout)), "-"+e.Session+".jsonl") || !filepath.IsAbs(filepath.FromSlash(e.Root)) || !r.Clear(e.Root) || !r.Clear(e.Rollout) {
		return bad("task_source_contract")
	}
	for _, id := range []string{e.Scope.Owner, e.Scope.Guild, e.Scope.Forum, e.Origin} {
		if _, ok := number(id); !ok {
			return bad("task_source_contract")
		}
	}
	now := r.Now()
	if e.Created.IsZero() || now.Before(e.Created) || !now.Before(e.Expires) || e.Expires.After(e.Created.Add(7*24*time.Hour)) || !r.Dst(e.Scope) {
		return bad("task_source_current")
	}
	state, err := r.Task(ctx, e.Scope.Todo)
	if err != nil || !state.eligible(question, e.Scope.Owner) || !conversationMediaSamePath(filepath.FromSlash(e.Root), filepath.FromSlash(state.Root)) || state.Worker.Job != e.Session || state.Scheduler.Job != e.Session || len(state.Jobs) != 1 || state.Jobs[0] != e.Session || state.Worker.Generation == 0 || !r.Root(ctx, e.Root) {
		return bad("task_source_task")
	}
	if err = taskQuestionNative(e, state, r.Records); err != nil {
		return e, receipt, err
	}
	if !r.Repo(ctx, e.Repository) {
		return bad("task_source_repository")
	}
	body, err := r.guarded(ctx, e.Body, 128<<10)
	if err != nil || bytesHash(body) != e.Scope.Body {
		return bad("task_source_question")
	}
	decision, err := r.guarded(ctx, e.Decision, 128<<10)
	if err != nil || !taskQuestionDecision(decision, e) {
		return bad("task_source_question")
	}
	mediaBytes, err := r.guarded(ctx, e.Media, 512<<10)
	if err != nil || bytesHash(mediaBytes) != e.MediaSHA {
		return bad("task_source_media")
	}
	media, err := r.Media(ctx, e.Media)
	if err != nil || media.Scope != e.Scope || len(media.Images) != 1 || media.Images[0].SHA256 != taskQuestionDecisionImage(decision) {
		return bad("task_source_media")
	}
	var resources struct{ Resources []struct{ Kind, ID string } }
	_ = json.Unmarshal(decision, &resources)
	for _, resource := range resources.Resources {
		if resource.Kind == "image" && resource.ID != filepath.Base(filepath.FromSlash(media.Images[0].Path)) {
			return bad("task_source_media")
		}
	}
	// Validate the same original bytes; the bridge retains the filename's
	// extension in its request identity even for JPEG data named .png.
	image, err := r.Read(media.Images[0].Path, conversationMediaLimit)
	mime, _, magicErr := conversationMediaMagic(image)
	if err != nil || magicErr != nil || bytesHash(image) != media.Images[0].SHA256 || int64(len(image)) != media.Images[0].Size {
		return bad("task_source_media")
	}
	// The normal bridge's identity retains the source filename extension.
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(media.Images[0].Path)), ".")
	if mime != "image/png" && mime != "image/jpeg" {
		return bad("task_source_media")
	}
	identity := []any{"normal-ask-v1", e.Scope.Body, e.Scope.Decision, e.Scope.Todo, e.Title, e.Origin, [][]string{{media.Images[0].SHA256, ext}}}
	if e.Request != "normal-ask-"+canonicalHash(identity) {
		return bad("task_source_question")
	}
	current, err := r.Task(ctx, e.Scope.Todo)
	if err != nil || !current.eligible(question, e.Scope.Owner) || canonicalHash(current) != canonicalHash(state) || !r.Dst(e.Scope) || ctx.Err() != nil {
		return bad("task_source_current")
	}
	receipt = taskQuestionReceipt{Evidence: path, SHA: bytesHash(b), State: canonicalHash(state)}
	return e, receipt, nil
}

func taskQuestionDecisionImage(b []byte) string {
	var v struct {
		Scope struct {
			Image string `json:"image_sha256"`
		}
	}
	_ = json.Unmarshal(b, &v)
	return v.Scope.Image
}

func taskQuestionDecision(b []byte, e taskQuestionEvidence) bool {
	var v struct {
		Todo                           uint64 `json:"todo_id"`
		Decision, Owner, Guild, Parent string
		Resources                      []struct{ Kind, ID string }
		Scope                          struct {
			Image       string `json:"image_sha256"`
			Description string
		}
	}
	if historyStrictJSON(b, &v) != nil || canonicalHash(json.RawMessage(b)) != e.Scope.Decision || v.Todo != e.Scope.Todo || v.Decision != "direction" || v.Owner != e.Scope.Owner || v.Guild != e.Scope.Guild || v.Parent != e.Scope.Forum || len(v.Resources) != 2 || !shaPattern.MatchString(v.Scope.Image) || strings.TrimSpace(v.Scope.Description) == "" {
		return false
	}
	repos, images := 0, 0
	for _, resource := range v.Resources {
		switch resource.Kind {
		case "repository":
			if resource.ID != e.Repository.FullName {
				return false
			}
			repos++
		case "image":
			if !regexp.MustCompile(`^exec-[a-f0-9-]{36}\.png$`).MatchString(resource.ID) {
				return false
			}
			images++
		default:
			return false
		}
	}
	return repos == 1 && images == 1
}

func taskQuestionName(request string) string { return bytesHash([]byte(request)) + ".json" }

func (r taskQuestionRegistry) register(ctx context.Context, path string) error {
	e, receipt, err := r.check(ctx, path)
	if err != nil {
		return err
	}
	home := filepath.FromSlash(r.Keys.Home)
	if os.MkdirAll(home, 0700) != nil {
		return errConversationMedia
	}
	physical, _, err := conversationMediaPhysical(home)
	if err != nil || !conversationMediaSamePath(physical, home) {
		return errConversationMedia
	}
	key, err := r.Keys.key(true)
	if err != nil {
		return errConversationMedia
	}
	defer eraseGeneratedKey(key)
	payload, _ := json.Marshal(receipt)
	b, _ := json.Marshal(generatedQuestionEnvelope{Payload: payload, MAC: taskQuestionMAC(key, payload)})
	name := filepath.Join(physical, taskQuestionName(e.Request))
	err = generatedWriteNew(name, b)
	if os.IsExist(err) {
		old, readErr := r.Read(name, 128<<10)
		if readErr == nil && bytes.Equal(old, b) {
			return nil
		}
	}
	return err
}

func (r taskQuestionRegistry) load(ctx context.Context, input ConversationAskInput, state taskQuestionState) (taskQuestionEvidence, error) {
	zero := taskQuestionEvidence{}
	if !conversationMediaNormal.MatchString(input.RequestID) {
		return zero, errConversationMedia
	}
	home := filepath.FromSlash(r.Keys.Home)
	physical, _, err := conversationMediaPhysical(home)
	if err != nil || !conversationMediaSamePath(physical, home) {
		return zero, errConversationMedia
	}
	b, err := r.Read(filepath.Join(physical, taskQuestionName(input.RequestID)), 128<<10)
	var envelope generatedQuestionEnvelope
	if err != nil || historyStrictJSON(b, &envelope) != nil {
		return zero, errConversationMedia
	}
	key, err := r.Keys.key(false)
	if err != nil {
		return zero, errConversationMedia
	}
	defer eraseGeneratedKey(key)
	mac, err := hex.DecodeString(envelope.MAC)
	expected, _ := hex.DecodeString(taskQuestionMAC(key, envelope.Payload))
	if err != nil || !hmac.Equal(mac, expected) {
		return zero, errConversationMedia
	}
	var saved taskQuestionReceipt
	if historyStrictJSON(envelope.Payload, &saved) != nil {
		return zero, errConversationMedia
	}
	e, current, err := r.checkForQuestion(ctx, saved.Evidence, input.heldQuestion)
	if err != nil || current != saved || current.State != canonicalHash(state) || e.Request != input.RequestID || e.Scope.Todo != input.Todo || e.Origin != input.OriginThread || e.Title != input.Title || e.Wait != input.Wait || input.ActorTodo != 0 || input.ActorJob != "" {
		return zero, errConversationMedia
	}
	body, err := r.Read(input.BodyFile, 128<<10)
	if err != nil || bytesHash(body) != e.Scope.Body {
		return zero, errConversationMedia
	}
	decision, err := r.Read(input.DecisionFile, 128<<10)
	if err != nil || !taskQuestionDecision(decision, e) {
		return zero, errConversationMedia
	}
	return e, nil
}

var errTaskQuestion = errors.New("conversation task source unverified")
