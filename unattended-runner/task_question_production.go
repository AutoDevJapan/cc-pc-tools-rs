package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
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

// Check the store envelope and project keys without recursively interpreting
// unrelated Tasks' spec/note values. Only the selected row receives the normal
// Task parser and eligibility checks below.
func taskQuestionStoreFields(raw []byte, fold bool) (map[string]json.RawMessage, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') {
		return nil, errTaskQuestion
	}
	fields := map[string]json.RawMessage{}
	seen := map[string]bool{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok {
			return nil, errTaskQuestion
		}
		normal := key
		if fold {
			normal = historyFoldJSONKey(key)
		}
		if seen[normal] {
			return nil, errTaskQuestion
		}
		seen[normal] = true
		if fold && strings.EqualFold(key, "projects") {
			key = "projects"
		} else if fold && strings.EqualFold(key, "items") {
			key = "items"
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, errTaskQuestion
		}
		fields[key] = value
	}
	end, err := d.Token()
	if err != nil || end != json.Delim('}') {
		return nil, errTaskQuestion
	}
	var tail json.RawMessage
	if d.Decode(&tail) != io.EOF {
		return nil, errTaskQuestion
	}
	return fields, nil
}

// Decode only a consumed metadata string. encoding/json's replacement of
// invalid UTF-8 or isolated UTF-16 surrogates must not change its identity.
func taskQuestionJSONString(raw []byte) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' || !utf8.Valid(raw) {
		return "", errTaskQuestion
	}
	unit := func(at int) (uint16, bool) {
		if at+4 > len(raw) { return 0, false }
		var n uint16
		for _, c := range raw[at:at+4] {
			var v byte
			switch {
			case c >= '0' && c <= '9': v = c-'0'
			case c >= 'a' && c <= 'f': v = c-'a'+10
			case c >= 'A' && c <= 'F': v = c-'A'+10
			default: return 0, false
			}
			n = n<<4 | uint16(v)
		}
		return n, true
	}
	for i := 1; i < len(raw)-1; {
		if raw[i] != '\\' {
			if raw[i] < 0x20 || raw[i] == '"' { return "", errTaskQuestion }
			i++
			continue
		}
		if i+1 >= len(raw)-1 { return "", errTaskQuestion }
		switch raw[i+1] {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
			i += 2
		case 'u':
			n, ok := unit(i+2)
			if !ok { return "", errTaskQuestion }
			if n >= 0xd800 && n <= 0xdbff {
				if i+12 > len(raw)-1 || raw[i+6] != '\\' || raw[i+7] != 'u' { return "", errTaskQuestion }
				low, valid := unit(i+8)
				if !valid || low < 0xdc00 || low > 0xdfff { return "", errTaskQuestion }
				i += 12
			} else {
				if n >= 0xdc00 && n <= 0xdfff { return "", errTaskQuestion }
				i += 6
			}
		default:
			return "", errTaskQuestion
		}
	}
	var value string
	if json.Unmarshal(raw, &value) != nil || !utf8.ValidString(value) { return "", errTaskQuestion }
	return value, nil
}

// Inspect one member at this object level. Unknown values remain RawMessage:
// this never interprets another Task's spec/note object or its duplicate keys.
func taskQuestionObjectMember(raw []byte, wanted string, fold bool) (json.RawMessage, bool, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	start, err := d.Token()
	if err != nil || start != json.Delim('{') { return nil, false, errTaskQuestion }
	var selected json.RawMessage
	found := false
	for d.More() {
		from := d.InputOffset()
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok { return nil, false, errTaskQuestion }
		keyEnd := d.InputOffset()
		matches := key == wanted || fold && strings.EqualFold(key, wanted)
		if matches {
			lexeme := raw[from:keyEnd]
			quote := bytes.IndexByte(lexeme, '"')
			if quote < 0 { return nil, false, errTaskQuestion }
			strict, err := taskQuestionJSONString(lexeme[quote:])
			if err != nil || strict != key || found { return nil, false, errTaskQuestion }
		}
		var value json.RawMessage
		if d.Decode(&value) != nil { return nil, false, errTaskQuestion }
		if matches { selected, found = value, true }
	}
	end, err := d.Token()
	var tail json.RawMessage
	if err != nil || end != json.Delim('}') || d.Decode(&tail) != io.EOF { return nil, false, errTaskQuestion }
	return selected, found, nil
}

func taskQuestionRowID(raw []byte) (uint64, error) {
	value, found, err := taskQuestionObjectMember(raw, "id", true)
	value = bytes.TrimSpace(value)
	if err != nil || !found || len(value) == 0 || value[0] < '1' || value[0] > '9' { return 0, errTaskQuestion }
	for _, c := range value { if c < '0' || c > '9' { return 0, errTaskQuestion } }
	id, err := strconv.ParseUint(string(value), 10, 64)
	if err != nil || id == 0 { return 0, errTaskQuestion }
	return id, nil
}

func taskQuestionOptionalString(raw []byte, member string) (*string, error) {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) { return nil, nil }
	value, found, err := taskQuestionObjectMember(raw, member, true)
	if err != nil { return nil, errTaskQuestion }
	if !found || bytes.Equal(bytes.TrimSpace(value), []byte("null")) { return nil, nil }
	text, err := taskQuestionJSONString(value)
	if err != nil { return nil, errTaskQuestion }
	return &text, nil
}

func taskQuestionReadState(path string, id uint64) (taskQuestionState, error) {
	var db struct {
		Projects map[string]string
		Items []json.RawMessage
	}
	var raw json.RawMessage
	if readJSON(path, maxTodo, &raw) != nil {
		return taskQuestionState{}, errTaskQuestion
	}
	defer clear(raw)
	fields, err := taskQuestionStoreFields(raw, true)
	if err != nil {
		return taskQuestionState{}, errTaskQuestion
	}
	if _, err := taskQuestionStoreFields(fields["projects"], false); err != nil {
		return taskQuestionState{}, errTaskQuestion
	}
	if json.Unmarshal(fields["projects"], &db.Projects) != nil || json.Unmarshal(fields["items"], &db.Items) != nil {
		return taskQuestionState{}, errTaskQuestion
	}
	var found json.RawMessage
	for _, raw := range db.Items {
		rowID, err := taskQuestionRowID(raw)
		if err != nil {
			return taskQuestionState{}, errTaskQuestion
		}
		if rowID == id {
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
	projectRaw, present, err := taskQuestionObjectMember(found, "project", true)
	if err != nil || !present { return state, errTaskQuestion }
	project, err := taskQuestionJSONString(projectRaw)
	if err != nil || project != state.Project { return state, errTaskQuestion }
	rootRaw, present, err := taskQuestionObjectMember(fields["projects"], project, false)
	if err != nil || !present { return state, errTaskQuestion }
	state.Root, err = taskQuestionJSONString(rootRaw)
	if err != nil { return state, errTaskQuestion }
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
