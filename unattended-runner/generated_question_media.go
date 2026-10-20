package main

// Qualification for the two exact new 3084 design outputs. This never changes
// scrub391 classification, restores a registry, or grants a directory access.
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
const generatedQuestionSession = "01a1589a-24ef-7742-9d87-efee972aa027"
const generatedQuestionProofRoot = "D:/CC/Work/unattended-operations-20261013-wt/todo-3084-console-redesign/.ci-work/console-redesign-3084"
const generatedQuestionArtifactRoot = "C:/Users/makhmeto/.codex/generated_images/" + generatedQuestionSession
const generatedQuestionRollout = "C:/Users/makhmeto/.codex/sessions/2026/10/20/rollout-2026-10-20T18-17-39-" + generatedQuestionSession + ".jsonl"

type generatedQuestionScope struct {
	Todo     uint64 `json:"todo_id"`
	Body     string `json:"question_sha256"`
	Decision string `json:"decision_sha256"`
	Owner    string `json:"owner_id"`
	Guild    string `json:"guild_id"`
	Forum    string `json:"forum_id"`
}

var generatedQuestion3084 = generatedQuestionScope{3084,
	"c2d113c65e2e2dae5be18da2c55de68f777f9287f450eab014ba985ed509e425",
	"b3e571917027bbaec5862f1797fd5b10d82c22346c713700e1ba350699b3b092",
	"730822776147148835", "1552944707464597524", "1554276307997692004"}

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
	Path, SHA256, Record, RecordSHA, PromptSHA, Option string
	Size                                               int64
	Revision, Order                                    int
}
type generatedEventBound struct {
	Line                  int
	SHA, Kind, Name, Call string
}
type generatedQuestionSpec struct {
	Scope                             generatedQuestionScope
	Session, Proof, ProofSHA, Rollout string
	Images                            []generatedImageBound
	Events                            []generatedEventBound
	Read                              func(string, int64) ([]byte, error)
	Guard                             func(context.Context, string) (bool, error)
	Records                           func(string, []generatedEventBound) ([]json.RawMessage, error)
	Task                              func() bool
	SourceClear                       func(string) bool
	Now                               func() time.Time
}

func generatedQuestionProduction() generatedQuestionSpec {
	return generatedQuestionSpec{Scope: generatedQuestion3084, Session: generatedQuestionSession,
		Proof: generatedQuestionProofRoot + "/tool-event-proof.private.json", ProofSHA: "9baffc15bd6bafa7ef88103468a9773b470d3baf7e2ff0ddde0cd6e6cd11cb30", Rollout: generatedQuestionRollout,
		Images: []generatedImageBound{
			{Path: generatedQuestionArtifactRoot + "/exec-80fffc6b-f839-4be7-8a0c-eb2b46fa56d3.png", SHA256: "90427e42bcb95129b5b618824c7a224dd1773187bcad32b3d4d94a160b34fa9a", Size: 1534375, Record: generatedQuestionProofRoot + "/generation-a-v3.private.json", RecordSHA: "f56cf86ca2c936a92fb2be684f6c2c18fa506ebf5249fe50dcfbe927a906356e", PromptSHA: "d2ce929a571cf8c7f7df2bf34299b94150baaf26910fc6ac37900992b781bfe8", Option: "a", Revision: 3, Order: 1},
			{Path: generatedQuestionArtifactRoot + "/exec-673f1fed-5c39-4c28-9bfe-dc897cdaf4ba.png", SHA256: "69ab52afa23e1701e2e477e36ef5fb9d0017ff5d63c76e631352ab288beea195", Size: 1611163, Record: generatedQuestionProofRoot + "/generation-b-v2.private.json", RecordSHA: "23b03f4a5dd171184301ef73870b74723d35cb35bb19e4524cdcf7a93fa7d77a", PromptSHA: "d88a2c5332f7d7513eb4f9ef7d55226d1a7290755ef5a6f41b0d6491705f2f99", Option: "b", Revision: 2, Order: 2},
		}, Events: []generatedEventBound{
			{237, "7e7d57a366922fa5240fc4ca971b40555355ee47a82797a1becb0d5b002035ee", "custom_tool_call", "exec", "call_35bd13ad20dc4c6dbf6a53f3789c1635"},
			{239, "b20aeb7b7d75512f8c55dc741d0316916b0a94246dc2f1173b9c738a0898a363", "custom_tool_call_output", "", "call_35bd13ad20dc4c6dbf6a53f3789c1635"},
			{259, "37d79800ccd3d8d0b4d07ef502e017dd5565e1229fc58d7408e3cae72e6c02c0", "function_call", "wait", "call_64edcf6e28004a99b9ca7b202d34763d"},
			{261, "d731d4256abc58bb9be1d69496e07f54f8970ef654fb95051d38a131e0cf5e8a", "function_call_output", "", "call_64edcf6e28004a99b9ca7b202d34763d"},
		}, Read: generatedQuestionRead, Guard: conversationMediaLiveGuard, Records: generatedQuestionRecords, Task: generatedQuestionTask, SourceClear: generatedQuestionSourceClear, Now: time.Now}
}

// Preserve any available original exclusion in memory; never export its rows.
// A missing registry is tolerated only after the separate exact artifact and
// question anchors below qualify, and is never represented as recovered.
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
	scanner := bufio.NewScanner(io.LimitReader(f, 64<<20))
	scanner.Buffer(make([]byte, 65536), 24<<20)
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

func generatedQuestionTask() bool {
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
		if row.ID == 3084 {
			if row.Project != "cc-pc-tools-rs" || row.Stopped || row.OwnerStopped {
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
	if scope != s.Scope || s.Read == nil || s.Guard == nil || s.Records == nil || s.Task == nil || s.SourceClear == nil || s.Now == nil || ctx.Err() != nil {
		return nil, errConversationMedia
	}
	now := s.Now()
	if now.Before(time.Date(2026, 10, 20, 9, 39, 19, 0, time.UTC)) || !now.Before(time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)) || !s.Task() {
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
			if json.Unmarshal(b, &g) != nil || g.Todo != scope.Todo || g.Option != image.Option || g.Revision != image.Revision || g.Mode != "new_generation" || g.Tool != "image_gen__imagegen" || g.Orchestrator != "functions.exec" || g.ExecCellID != "25" || g.InvocationOrder != image.Order || len(g.InputImages) != 0 || g.SessionArtifactDirectoryFromTool != s.Session || !conversationMediaSamePath(filepath.FromSlash(g.OriginalPath), physical) || len(g.Request) != 2 {
				return nil, errConversationMedia
			}
			var prompt string
			var transparent bool
			if json.Unmarshal(g.Request["prompt"], &prompt) != nil || bytesHash([]byte(prompt)) != image.PromptSHA || json.Unmarshal(g.Request["transparent_background"], &transparent) != nil || transparent || !strings.Contains(g.Response.Hint, filepath.Base(physical)) {
				return nil, errConversationMedia
			}
		}
	}
	records, err := s.Records(s.Rollout, s.Events)
	if err != nil || len(records) != len(s.Events) {
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
