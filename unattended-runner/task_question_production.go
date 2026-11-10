package main

import (
	"context"
	"encoding/json"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func taskQuestionProduction() taskQuestionRegistry {
	keys := generatedQuestionRegistryProduction()
	keys.Home = "D:/CC/Tools/discord-ask/unattended/task-question-metadata"
	return taskQuestionRegistry{
		Keys: keys, Sessions: keys.Sessions, Read: generatedQuestionRead,
		Guard: conversationMediaLiveGuard, Clear: generatedQuestionSourceClear,
		Records: generatedQuestionRecords, Media: generatedQuestionRegistryProduction().check,
		Now: time.Now, Dst: keys.Destination, Jobs: "D:/CC/Tools/codex-jobs/jobs",
		Task: func(_ context.Context, id uint64) (taskQuestionState, error) {
			return taskQuestionReadState("D:/CC/Tools/todo/todo.json", id)
		},
		Repo: func(ctx context.Context, repo taskQuestionRepository) bool {
			if repo.ID == 0 || repo.OwnerID == 0 || !regexp.MustCompile(`^(AutoDevJapan|BM1314|mashfromband)/[A-Za-z0-9_.-]+$`).MatchString(repo.FullName) {
				return false
			}
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			raw, err := runCommand(ctx, "gh", "api", "repos/"+repo.FullName)
			var api struct {
				ID    uint64
				Name  string `json:"full_name"`
				Owner struct {
					ID    uint64
					Login string
				}
			}
			var original any
			if err != nil || len(raw) > 128<<10 || historyStrictJSON([]byte(raw), &original) != nil || json.Unmarshal([]byte(raw), &api) != nil {
				return false
			}
			return api.ID == repo.ID && api.Name == repo.FullName && api.Owner.ID == repo.OwnerID && api.Owner.Login == strings.Split(repo.FullName, "/")[0]
		},
		Root: func(ctx context.Context, path string) bool {
			raw, err := runCommand(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", path)
			return err == nil && taskQuestionUnattributed(raw)
		},
	}
}

// This narrow metadata purpose applies only to the known attribution gap. An
// explicit client/card exclusion or a failed/malformed guard never reaches it.
func taskQuestionUnattributed(raw string) bool {
	var p struct {
		Client, Unverified, Access *bool
		Policy, Basis, Status      string
		Categories                 []string
	}
	var v map[string]json.RawMessage
	if len(raw) > 64<<10 || historyStrictJSON([]byte(raw), &v) != nil {
		return false
	}
	_ = json.Unmarshal(v["client_resource"], &p.Client)
	_ = json.Unmarshal(v["classification_unverified"], &p.Unverified)
	_ = json.Unmarshal(v["access_allowed"], &p.Access)
	_ = json.Unmarshal(v["classification_policy"], &p.Policy)
	_ = json.Unmarshal(v["classification_basis"], &p.Basis)
	_ = json.Unmarshal(v["protection_status"], &p.Status)
	_ = json.Unmarshal(v["protection_categories"], &p.Categories)
	return p.Client != nil && *p.Client && p.Unverified != nil && !*p.Unverified && p.Access != nil && !*p.Access && p.Policy == "owner-client-rule-20261027" && p.Basis == "owner_rule_unattributed_location" && p.Status == "protected" && len(p.Categories) == 1 && p.Categories[0] == "client_project"
}

func taskQuestionReadState(path string, id uint64) (taskQuestionState, error) {
	var db struct {
		Projects map[string]string
		Items    []json.RawMessage
	}
	if readJSON(path, maxTodo, &db) != nil {
		return taskQuestionState{}, errTaskQuestion
	}
	var found json.RawMessage
	for _, raw := range db.Items {
		var header struct{ ID uint64 }
		if json.Unmarshal(raw, &header) != nil {
			return taskQuestionState{}, errTaskQuestion
		}
		if header.ID == id {
			if found != nil {
				return taskQuestionState{}, errTaskQuestion
			}
			found = raw
		}
	}
	var state taskQuestionState
	var flags struct {
		Status       string
		Stopped      bool            `json:"stopped"`
		OwnerStopped bool            `json:"owner_stopped"`
		Cancelled    bool            `json:"cancel_requested"`
		Stop         string          `json:"stop_kind"`
		Postponed    bool            `json:"owner_postponement_recorded"`
		Decision     json.RawMessage `json:"decision_wait"`
		Wait         json.RawMessage `json:"owner_question_wait"`
		Scheduler    struct {
			Stop json.RawMessage `json:"owner_stop_job"`
		}
	}
	var original any
	if historyStrictJSON(found, &original) != nil || json.Unmarshal(found, &state) != nil || json.Unmarshal(found, &flags) != nil || state.ID != id || state.Project == "" || state.Project == "owner" || state.Title == "" || state.Created <= 0 {
		return state, errTaskQuestion
	}
	nonnull := func(b json.RawMessage) bool { return len(b) != 0 && string(b) != "null" }
	if (flags.Status != "doing" && flags.Status != "todo" && flags.Status != "block") || flags.Stopped || flags.OwnerStopped || flags.Cancelled || flags.Postponed || flags.Stop != "" || nonnull(flags.Decision) || nonnull(flags.Scheduler.Stop) || conversationActorHeld(found) {
		return state, errTaskQuestion
	}
	state.Root = db.Projects[state.Project]
	state.current = found
	if !filepath.IsAbs(filepath.FromSlash(state.Root)) {
		return state, errTaskQuestion
	}
	return state, nil
}

func (r taskQuestionRegistry) images(ctx context.Context, input ConversationAskInput, e taskQuestionEvidence, home string) bool {
	media, err := r.Media(ctx, e.Media)
	if err != nil || len(input.Attachments) != 1 || len(media.Images) != 1 {
		return false
	}
	original := media.Images[0]
	originalPath, err := conversationMediaPath(original.Path)
	if err != nil {
		return false
	}
	path, err := conversationMediaPath(input.Attachments[0])
	if err != nil {
		return false
	}
	if !conversationMediaSamePath(path, originalPath) {
		// Only the existing normal bridge's receipt can retain an original path.
		expected := filepath.Join(home, "conversation-bridge", input.RequestID)
		if !conversationMediaSamePath(filepath.Dir(path), expected) || !conversationMediaAttachment.MatchString(filepath.Base(path)) {
			return false
		}
		var source conversationMediaSource
		b, err := r.Read(strings.TrimSuffix(path, filepath.Ext(path))+".source.json", 64<<10)
		if err != nil || historyStrictJSON(b, &source) != nil || source.Domain != conversationMediaSourceDomain || source.SHA256 != original.SHA256 || source.Size != original.Size {
			return false
		}
		sourcePath, err := conversationMediaPath(source.SourcePath)
		if err != nil || !conversationMediaSamePath(sourcePath, originalPath) {
			return false
		}
	}
	proof, err := verifyConversationMedia(ctx, path, "conversation-v1-"+bytesHash([]byte(input.RequestID))[:32], conversationMediaDependencies{
		Home: home, Guard: r.Guard, Question: &e.Scope,
		Qualify: func(ctx context.Context, path string, scope generatedQuestionScope) (*generatedImageQualification, error) {
			return qualifyGeneratedQuestionImage(ctx, path, scope, generatedQuestionProduction())
		},
	})
	return err == nil && proof.Verified && proof.SHA256 == original.SHA256 && proof.Size == original.Size
}

func (r *Runner) verifyTaskQuestion(ctx context.Context, input ConversationAskInput, original taskQuestionState) error {
	if input.heldQuestion == nil {
		index, err := r.loadConversations()
		if err != nil {
			return errTaskQuestion
		}
		if q, exists := index.Questions[input.RequestID]; exists {
			if q.Phase == "waiting" || q.Phase == "answered" || q.Phase == "posted" {
				retained, _, err := r.taskQuestionRetainedInput(ctx, q)
				if err != nil {
					return errTaskQuestion
				}
				input.heldQuestion = retained.heldQuestion
			}
		}
	}
	registry := r.taskQuestionRegistry()
	e, err := registry.load(ctx, input, original)
	if err != nil || e.Scope.Owner != r.State.Destination.Owner || e.Scope.Guild != r.State.Destination.Guild || e.Scope.Forum != r.State.Destination.Forum {
		return errTaskQuestion
	}
	if input.heldQuestion == nil {
		if !registry.images(ctx, input, e, filepath.Dir(r.Config.Home)) || !registry.Now().Before(e.Expires) || registry.stageAdmission(e) != nil {
			return errTaskQuestion
		}
	}
	return nil
}

func (r *Runner) recheckTaskQuestion(ctx context.Context, input ConversationAskInput) error {
	state, err := taskQuestionReadState(r.TodoPath, input.Todo)
	if err != nil {
		return errTaskQuestion
	}
	return r.verifyTaskQuestion(ctx, input, state)
}
