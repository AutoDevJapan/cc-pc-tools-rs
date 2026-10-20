package main

// Qualification is scoped to one question and independently recorded native
// generation evidence. It never changes the general source classification.
import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const generatedQuestionDomain = "owner-generated-question-image-v1"
const generatedNativeScanLimit int64 = 256 << 20
const generatedNativeLineLimit = 36 << 20

type generatedQuestionScope struct {
	Todo     uint64 `json:"todo_id"`
	Body     string `json:"question_sha256"`
	Decision string `json:"decision_sha256"`
	Owner    string `json:"owner_id"`
	Guild    string `json:"guild_id"`
	Forum    string `json:"forum_id"`
}

type generatedImageQualification struct {
	Domain    string                 `json:"domain"`
	Qualified bool                   `json:"qualified"`
	Scope     generatedQuestionScope `json:"scope"`
	SHA256    string                 `json:"sha256"`
	Size      int64                  `json:"size"`
	MIME      string                 `json:"mime"`
	Extension string                 `json:"extension"`
}

type generatedImageBound struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	Record    string `json:"record_path"`
	RecordSHA string `json:"record_sha256"`
	PromptSHA string `json:"prompt_sha256"`
	Option    string `json:"option"`
	Size      int64  `json:"size"`
	Revision  int    `json:"revision"`
	Order     int    `json:"order"`
}
type generatedEventBound struct {
	Line int    `json:"line"`
	SHA  string `json:"sha256"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	Call string `json:"call_id"`
}
type generatedQuestionSpec struct {
	Scope                             generatedQuestionScope
	Session, Proof, ProofSHA, Rollout string
	Images                            []generatedImageBound
	Events                            []generatedEventBound
	SessionSHA                        string
	Assignment                        generatedEventBound
	Cell                              string
	Created, Expires                  time.Time
	Load                              func(context.Context, string, generatedQuestionScope) (generatedQuestionSpec, error)
	Read                              func(string, int64) ([]byte, error)
	Guard                             func(context.Context, string) (bool, error)
	Records                           func(string, []generatedEventBound) ([]json.RawMessage, error)
	Task                              func() bool
	SourceClear                       func(string) bool
	Now                               func() time.Time
}

func generatedQuestionProduction() generatedQuestionSpec {
	r := generatedQuestionRegistryProduction()
	return generatedQuestionSpec{Load: r.load}
}

// Preserve any available original exclusion in memory; never export its rows.
// Missing legacy classification is never represented as recovered. Independent
// native-generation and exact question evidence must still qualify below.
func generatedQuestionSourceClear(path string) bool {
	b, err := os.ReadFile("D:/CC/Tools/client-projects.txt")
	if os.IsNotExist(err) {
		return true
	}
	if err != nil {
		return false
	}
	key := strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.Contains(key, strings.ToLower(strings.ReplaceAll(line, `\`, "/"))) {
			return false
		}
	}
	return true
}

func generatedQuestionRead(path string, limit int64) ([]byte, error) {
	b, _, err := conversationMediaRead(filepath.FromSlash(path), limit)
	return b, err
}

// Only the pinned selected original records are consumed. Appending a new turn
// does not invalidate the earlier records; no other messages leave this reader.
func generatedQuestionRecords(path string, expected []generatedEventBound) ([]json.RawMessage, error) {
	physical, before, err := conversationMediaPhysical(filepath.FromSlash(path))
	if err != nil || !before.Mode().IsRegular() {
		return nil, errConversationMedia
	}
	f, err := os.Open(physical)
	if err != nil {
		return nil, errConversationMedia
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return nil, errConversationMedia
	}
	scanner := bufio.NewScanner(io.LimitReader(f, generatedNativeScanLimit))
	scanner.Buffer(make([]byte, 65536), generatedNativeLineLimit)
	var result []json.RawMessage
	line, next := 0, 0
	for scanner.Scan() {
		line++
		if next >= len(expected) {
			break
		}
		if line != expected[next].Line {
			continue
		}
		b := append([]byte(nil), scanner.Bytes()...)
		if bytesHash(b) != expected[next].SHA {
			return nil, errConversationMedia
		}
		result = append(result, b)
		next++
		if next == len(expected) {
			break
		}
	}
	_, named, pathErr := conversationMediaPhysical(physical)
	if scanner.Err() != nil || next != len(expected) || pathErr != nil || !os.SameFile(opened, named) {
		return nil, errConversationMedia
	}
	return result, nil
}

func generatedQuestionTask(todo uint64) bool {
	var db struct {
		Items []struct {
			ID              uint64 `json:"id"`
			Project, Status string
			Stopped         bool `json:"stopped"`
			OwnerStopped    bool `json:"owner_stopped"`
		}
	}
	if readJSON("D:/CC/Tools/todo/todo.json", maxTodo, &db) != nil {
		return false
	}
	found := 0
	for _, row := range db.Items {
		if row.ID == todo {
			if row.Project == "" || row.Stopped || row.OwnerStopped {
				return false
			}
			switch row.Status {
			case "todo", "doing", "block", "wait":
			default:
				return false
			}
			found++
		}
	}
	return found == 1
}

func qualifyGeneratedQuestionImage(ctx context.Context, path string, scope generatedQuestionScope, s generatedQuestionSpec) (*generatedImageQualification, error) {
	if s.Load != nil {
		loaded, err := s.Load(ctx, path, scope)
		if err != nil {
			return nil, errConversationMedia
		}
		s = loaded
	}
	if scope != s.Scope || s.Read == nil || s.Guard == nil || s.Records == nil || s.Task == nil || s.SourceClear == nil || s.Now == nil || ctx.Err() != nil {
		return nil, errConversationMedia
	}
	now := s.Now()
	if s.Created.IsZero() || s.Expires.IsZero() || now.Before(s.Created) || !now.Before(s.Expires) || s.Expires.After(s.Created.Add(7*24*time.Hour)) || !s.Task() {
		return nil, errConversationMedia
	}
	physical, _, err := conversationMediaPhysical(path)
	if err != nil {
		return nil, errConversationMedia
	}
	var image *generatedImageBound
	for i := range s.Images {
		if conversationMediaSamePath(physical, filepath.FromSlash(s.Images[i].Path)) {
			image = &s.Images[i]
		}
	}
	if image == nil {
		return nil, errConversationMedia
	}
	if validateGeneratedAssignment(s) != nil {
		return nil, errConversationMedia
	}
	if !s.SourceClear(physical) {
		return nil, errConversationMedia
	}
	for _, evidence := range []struct{ path, sha string }{{s.Proof, s.ProofSHA}, {image.Record, image.RecordSHA}} {
		allowed, guardErr := s.Guard(ctx, evidence.path)
		if guardErr != nil || !allowed {
			return nil, errConversationMedia
		}
		b, readErr := s.Read(evidence.path, 128<<10)
		if readErr != nil || bytesHash(b) != evidence.sha {
			return nil, errConversationMedia
		}
		if evidence.path == image.Record {
			var g struct {
				Todo                                                                                 uint64
				Option                                                                               string
				Revision                                                                             int
				Mode, Tool, Orchestrator, ExecCellID, OriginalPath, SessionArtifactDirectoryFromTool string
				InvocationOrder                                                                      int
				InputImages                                                                          []any
				Request                                                                              map[string]json.RawMessage
				Response                                                                             struct {
					Hint string `json:"output_hint"`
				}
			}
			if json.Unmarshal(b, &g) != nil || g.Todo != scope.Todo || g.Option != image.Option || g.Revision != image.Revision || g.Mode != "new_generation" || g.Tool != "image_gen__imagegen" || g.Orchestrator != "functions.exec" || g.ExecCellID != s.Cell || g.InvocationOrder != image.Order || g.InputImages == nil || len(g.InputImages) != 0 || g.SessionArtifactDirectoryFromTool != s.Session || !conversationMediaSamePath(filepath.FromSlash(g.OriginalPath), physical) || len(g.Request) < 1 || len(g.Request) > 2 {
				return nil, errConversationMedia
			}
			var prompt string
			var transparent bool
			if json.Unmarshal(g.Request["prompt"], &prompt) != nil || strings.TrimSpace(prompt) == "" || bytesHash([]byte(prompt)) != image.PromptSHA || !strings.Contains(g.Response.Hint, filepath.Base(physical)) {
				return nil, errConversationMedia
			}
			for key, raw := range g.Request {
				if key == "transparent_background" {
					if json.Unmarshal(raw, &transparent) != nil {
						return nil, errConversationMedia
					}
				} else if key != "prompt" {
					return nil, errConversationMedia
				}
			}
		}
	}
	records, err := s.Records(s.Rollout, s.Events)
	if err != nil || len(records) != len(s.Events) {
		return nil, errConversationMedia
	}
	if validateGeneratedQuestionNative(records, s) != nil {
		return nil, errConversationMedia
	}
	for i, raw := range records {
		var record struct {
			Type    string
			Payload struct {
				Type, Name string
				Call       string `json:"call_id"`
				Input      string
				Output     json.RawMessage
			}
		}
		if bytesHash(raw) != s.Events[i].SHA || json.Unmarshal(raw, &record) != nil || record.Type != "response_item" || record.Payload.Type != s.Events[i].Kind || record.Payload.Name != s.Events[i].Name || record.Payload.Call != s.Events[i].Call {
			return nil, errConversationMedia
		}
		if i == 0 && (!strings.Contains(record.Payload.Input, "tools.image_gen__imagegen") || strings.Contains(record.Payload.Input, "referenced_image_paths") || strings.Contains(record.Payload.Input, "num_last_images_to_include")) {
			return nil, errConversationMedia
		}
		if i == 3 {
			for _, bound := range s.Images {
				if !strings.Contains(string(record.Payload.Output), filepath.Base(filepath.FromSlash(bound.Path))) {
					return nil, errConversationMedia
				}
			}
		}
	}
	b, err := s.Read(physical, conversationMediaLimit)
	if err != nil || int64(len(b)) != image.Size || bytesHash(b) != image.SHA256 || ctx.Err() != nil || !s.Task() || !s.SourceClear(physical) {
		return nil, errConversationMedia
	}
	mime, ext, err := conversationMediaMagic(b)
	if err != nil || mime != "image/png" {
		return nil, errConversationMedia
	}
	return &generatedImageQualification{generatedQuestionDomain, true, scope, image.SHA256, image.Size, mime, ext}, nil
}
