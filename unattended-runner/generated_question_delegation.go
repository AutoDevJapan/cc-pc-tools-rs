package main

// A delegation selector is not a receipt or grant. Its parent/child edge and
// root job/task association must be read independently on every consumption.
import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

type generatedQuestionDelegation struct {
	Job            string              `json:"job_id"`
	Parent         string              `json:"parent_thread_id"`
	ParentTurn     string              `json:"parent_turn_id"`
	SpawnItem      string              `json:"spawn_item_id"`
	GenerationTurn string              `json:"generation_turn_id"`
	GenerationItem string              `json:"generation_item_id"`
	Context        generatedEventBound `json:"generation_context"`
}

type generatedQuestionLineage struct {
	Call func(context.Context, string, map[string]any) (json.RawMessage, error)
	Job  func(context.Context, string, uint64) (generatedQuestionOriginJob, error)
}

type generatedQuestionOriginJob struct {
	ID           string `json:"id"`
	Todo         uint64 `json:"todo_id"`
	Thread       string `json:"thread_id"`
	Cwd          string `json:"cwd"`
	Status       string `json:"status"`
	Stopped      bool   `json:"stopped"`
	OwnerStopped bool   `json:"owner_stopped"`
	Cancelled    bool   `json:"cancel_requested"`
	CancelReason string `json:"cancel_reason"`
	ResourceSHA  string `json:"-"`
}

// Kept separate from prefix-based legacy assignments: natural-language task
// mentions never supply authority, and callers cannot assert their own parent.
func (l generatedQuestionLineage) verify(ctx context.Context, s generatedQuestionSpec, text string) error {
	d := s.Delegation
	bad := func() error { return generatedFailure("native_delegation", "native_delegation_unverified") }
	if d == nil || l.Call == nil || l.Job == nil || ctx.Err() != nil || !validJob(d.Job) || !generatedUUID.MatchString(d.Parent) || d.Parent == s.Session || !generatedUUID.MatchString(d.ParentTurn) || !generatedUUID.MatchString(d.GenerationTurn) || strings.TrimSpace(d.SpawnItem) == "" || len(d.SpawnItem) > 256 || strings.TrimSpace(d.GenerationItem) == "" || len(d.GenerationItem) > 256 || d.Context.Line <= s.Assignment.Line || d.Context.Line >= s.Events[0].Line || !shaPattern.MatchString(d.Context.SHA) || len(s.Images) != 1 || text == "" {
		return bad()
	}
	job, err := l.Job(ctx, d.Job, s.Scope.Todo)
	if err != nil || job.ID != d.Job || job.Todo != s.Scope.Todo || job.Thread != d.Parent || !filepath.IsAbs(filepath.FromSlash(job.Cwd)) || job.Stopped || job.OwnerStopped || job.Cancelled || job.CancelReason != "" || (job.Status != "running" && job.Status != "succeeded") {
		return bad()
	}
	decode := func(method string, params map[string]any, out any) bool {
		raw, err := l.Call(ctx, method, params)
		var envelope map[string]json.RawMessage
		return err == nil && len(raw) <= 4<<20 && historyStrictJSON(raw, &envelope) == nil && json.Unmarshal(raw, out) == nil
	}
	type threadResponse struct {
		Thread struct {
			ID, Cwd   string
			CreatedAt int64
			Parent    string `json:"parentThreadId"`
			Source    json.RawMessage
		}
	}
	var child, parent threadResponse
	if !decode("thread/read", map[string]any{"threadId": s.Session, "includeTurns": false}, &child) || child.Thread.ID != s.Session || child.Thread.CreatedAt <= 0 || child.Thread.CreatedAt > s.Created.Unix() || child.Thread.Parent != d.Parent || !conversationMediaSamePath(filepath.FromSlash(child.Thread.Cwd), filepath.FromSlash(job.Cwd)) {
		return bad()
	}
	var source struct {
		SubAgent struct {
			Spawn struct {
				Parent string `json:"parent_thread_id"`
				Depth  int
			} `json:"thread_spawn"`
		} `json:"subAgent"`
	}
	if json.Unmarshal(child.Thread.Source, &source) != nil || source.SubAgent.Spawn.Parent != d.Parent || source.SubAgent.Spawn.Depth <= 0 {
		return bad()
	}
	if !decode("thread/read", map[string]any{"threadId": d.Parent, "includeTurns": false}, &parent) || parent.Thread.ID != d.Parent || !conversationMediaSamePath(filepath.FromSlash(parent.Thread.Cwd), filepath.FromSlash(job.Cwd)) {
		return bad()
	}
	var spawn struct {
		Data []struct {
			Turn string `json:"turnId"`
			Item struct {
				ID, Type, Tool, Status, Prompt string
				Sender                         string   `json:"senderThreadId"`
				Receivers                      []string `json:"receiverThreadIds"`
			}
		}
	}
	if !decode("thread/items/read", map[string]any{"threadId": d.Parent, "turnId": d.ParentTurn, "itemIds": []string{d.SpawnItem}}, &spawn) || len(spawn.Data) != 1 {
		return bad()
	}
	item := spawn.Data[0].Item
	if spawn.Data[0].Turn != d.ParentTurn || item.ID != d.SpawnItem || item.Type != "collabAgentToolCall" || item.Tool != "spawnAgent" || item.Status != "completed" || item.Sender != d.Parent || len(item.Receivers) != 1 || item.Receivers[0] != s.Session || strings.TrimSpace(item.Prompt) != text {
		return bad()
	}
	// The selected native generation context must belong to this turn and cwd.
	// It cannot be a later context spliced between another turn and its call.
	if generatedDelegationContext(s, d, job.Cwd) != nil {
		return bad()
	}
	var turn struct {
		Data []struct {
			Turn string `json:"turnId"`
			Item struct {
				ID, Type, Status, SavedPath string
				InputImages                 []json.RawMessage
			}
		}
	}
	if !decode("thread/items/read", map[string]any{"threadId": s.Session, "turnId": d.GenerationTurn, "itemIds": []string{d.GenerationItem}}, &turn) || len(turn.Data) != 1 || turn.Data[0].Turn != d.GenerationTurn {
		return bad()
	}
	generated := turn.Data[0].Item
	if generated.ID != d.GenerationItem || generated.Type != "imageGeneration" || generated.Status != "completed" || len(generated.InputImages) != 0 || !conversationMediaSamePath(filepath.FromSlash(generated.SavedPath), filepath.FromSlash(s.Images[0].Path)) {
		return bad()
	}
	current, err := l.Job(ctx, d.Job, s.Scope.Todo)
	if err != nil || !reflect.DeepEqual(job, current) || ctx.Err() != nil {
		return bad()
	}
	var currentChild threadResponse
	if !decode("thread/read", map[string]any{"threadId": s.Session, "includeTurns": false}, &currentChild) || !reflect.DeepEqual(child, currentChild) {
		return bad()
	}
	return nil
}

func generatedDelegationContext(s generatedQuestionSpec, d *generatedQuestionDelegation, cwd string) error {
	records, err := s.Records(s.Rollout, []generatedEventBound{d.Context})
	if err != nil || len(records) != 1 {
		return errConversationMedia
	}
	var record struct {
		Type      string
		Timestamp time.Time
		Payload   struct {
			Turn string `json:"turn_id"`
			Cwd  string
		}
	}
	var raw map[string]json.RawMessage
	if historyStrictJSON(records[0], &raw) != nil || json.Unmarshal(records[0], &record) != nil || record.Type != "turn_context" || record.Payload.Turn != d.GenerationTurn || record.Timestamp.IsZero() || record.Timestamp.After(s.Created) || !conversationMediaSamePath(filepath.FromSlash(record.Payload.Cwd), filepath.FromSlash(cwd)) {
		return errConversationMedia
	}
	physical, before, err := conversationMediaPhysical(filepath.FromSlash(s.Rollout))
	if err != nil {
		return errConversationMedia
	}
	f, err := os.Open(physical)
	if err != nil {
		return errConversationMedia
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) {
		return errConversationMedia
	}
	scanner := bufio.NewScanner(io.LimitReader(f, generatedNativeScanLimit))
	scanner.Buffer(make([]byte, 65536), generatedNativeLineLimit)
	line, seen := 0, false
	for scanner.Scan() {
		line++
		if line < d.Context.Line {
			continue
		}
		if line > s.Events[len(s.Events)-1].Line {
			break
		}
		var header struct {
			Type    string
			Payload struct {
				Turn string `json:"turn_id"`
				Cwd  string
			}
		}
		if json.Unmarshal(scanner.Bytes(), &header) != nil {
			return errConversationMedia
		}
		if line == d.Context.Line {
			if bytesHash(scanner.Bytes()) != d.Context.SHA {
				return errConversationMedia
			}
			seen = true
		}
		if line > d.Context.Line && header.Type == "turn_context" && (header.Payload.Turn != d.GenerationTurn || !conversationMediaSamePath(filepath.FromSlash(header.Payload.Cwd), filepath.FromSlash(cwd))) {
			return errConversationMedia
		}
		if line == s.Events[len(s.Events)-1].Line {
			break
		}
	}
	_, after, pathErr := conversationMediaPhysical(physical)
	if scanner.Err() != nil || !seen || line < s.Events[len(s.Events)-1].Line || pathErr != nil || !os.SameFile(opened, after) {
		return errConversationMedia
	}
	return nil
}
