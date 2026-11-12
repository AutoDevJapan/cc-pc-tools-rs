package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Only the original, signed native task question qualifies the selected row.
// This historical reader permits preserving stops; it cannot release them.
func (r *Runner) nativePartialState(ctx context.Context, v nativePartialInput) (json.RawMessage, string, error) {
	var db struct {
		Projects map[string]string
		Items    []json.RawMessage
	}
	if ctx.Err() != nil || readJSON(r.TodoPath, maxTodo, &db) != nil {
		return nil, "", errNativePartial
	}
	id, _ := number(v.Origin)
	var selected json.RawMessage
	for _, raw := range db.Items {
		var h struct {
			ID      uint64
			Project string
		}
		if json.Unmarshal(raw, &h) != nil {
			return nil, "", errNativePartial
		}
		if h.ID != id {
			continue
		}
		if selected != nil || h.Project != v.Project {
			return nil, "", errNativePartial
		}
		selected = raw
	}
	root := db.Projects[v.Project]
	var original any
	if selected == nil || historyStrictJSON(selected, &original) != nil || !filepath.IsAbs(filepath.FromSlash(root)) {
		return nil, "", errNativePartial
	}
	return selected, root, nil
}

func (r *Runner) nativePartialAuthority(ctx context.Context, v nativePartialInput, state json.RawMessage, root string) (ConversationQuestion, *conversationProof, conversationReply, error) {
	bad := func() (ConversationQuestion, *conversationProof, conversationReply, error) {
		return ConversationQuestion{}, nil, conversationReply{}, errNativePartial
	}
	index, err := r.loadConversations()
	if err != nil || index == nil {
		return bad()
	}
	q, ok := index.Questions[v.Request]
	if !ok || fmt.Sprint(q.Todo) != v.Origin || q.Project != v.Project || q.Actor != nil || q.Phase == "reserved" || q.Phase == "withdrawn" || q.Phase == "closed" {
		return bad()
	}
	_, proof, err := r.taskQuestionRetainedInput(ctx, q)
	if err != nil {
		return bad()
	}
	registry := r.taskQuestionRegistry()
	var registration taskQuestionReceipt
	var admission taskQuestionAdmission
	if registry.readSigned(taskQuestionName(v.Request), &registration) != nil || registry.readSigned(taskQuestionAdmissionName(v.Request, true), &admission) != nil || admission.Domain != "owner-task-question-admission-v1" || admission.Registration != registration || admission.Question != taskQuestionAdmittedIdentity(q) || admission.Validated.IsZero() || admission.Validated.Before(admission.Evidence.Created) || !admission.Validated.Before(admission.Evidence.Expires) || admission.Validated.After(r.Now()) {
		return bad()
	}
	e := admission.Evidence
	if e.Domain != taskQuestionDomain || e.Request != v.Request || e.Scope.Todo != q.Todo || e.Session != v.Parent || e.Origin != q.OriginThread || e.Title != q.Title || !e.Wait || e.Scope.Body != q.QuestionSHA || !registry.Dst(e.Scope) || e.Scope.Owner != r.State.Destination.Owner || !conversationMediaSamePath(filepath.FromSlash(root), filepath.FromSlash(e.Root)) || !regexp.MustCompile(`^(AutoDevJapan|BM1314|mashfromband)/[A-Za-z0-9_.-]+$`).MatchString(e.Repository.FullName) || e.Repository.ID == 0 || e.Repository.OwnerID == 0 {
		return bad()
	}
	// Re-read exactly the registered source, never an arbitrary caller path.
	b, err := registry.guarded(ctx, registration.Evidence, 128<<10)
	if err != nil || bytesHash(b) != registration.SHA {
		return bad()
	}
	var stored taskQuestionEvidence
	if historyStrictJSON(b, &stored) != nil || canonicalHash(stored) != canonicalHash(e) {
		return bad()
	}
	var current taskQuestionState
	if json.Unmarshal(state, &current) != nil {
		return bad()
	}
	current.Root, current.current = root, state
	// The exact original owner/generation/root/spec tuple must still match.
	// Status/stop flags are deliberately absent from this authority projection.
	if canonicalHash(current) != registration.State || current.Worker.Job != v.Parent || current.Scheduler.Job != v.Parent || len(current.Jobs) != 1 || current.Jobs[0] != v.Parent || taskQuestionNative(e, current, registry.Records) != nil || !registry.Repo(ctx, e.Repository) {
		return bad()
	}
	guard, guardErr := r.Run(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", root)
	if guardErr != nil || (!nativePartialClearJSON(guard) && !taskQuestionUnattributed(guard)) {
		return bad()
	}
	var reply *conversationReply
	for i := range proof.Replies {
		if proof.Replies[i].Message == v.Message {
			if reply != nil {
				return bad()
			}
			reply = &proof.Replies[i]
		}
	}
	if reply == nil || canonicalHash(*reply) != v.ReplySHA {
		return bad()
	}
	// repairLiveMetadata only performs a GET; it never changes runner files.
	stamp, ready, err := r.conversationReplySource(ctx, q, proof, *reply)
	if err != nil || !ready || stamp.SourceSHA != v.SourceSHA || stamp.ReplySHA != v.ReplySHA {
		return bad()
	}
	acquired, err := time.Parse(time.RFC3339Nano, r.conversationRequest(v.Message).Acquired)
	at, timeErr := time.Parse(time.RFC3339Nano, v.Evidence.AppliedAt)
	if err != nil || timeErr != nil || at.Before(acquired) || at.After(r.Now()) {
		return bad()
	}
	return q, proof, *reply, nil
}

func nativePartialClearJSON(raw string) bool {
	var v struct {
		Client     *bool `json:"client_resource"`
		Unverified *bool `json:"classification_unverified"`
		Access     *bool `json:"access_allowed"`
	}
	var original any
	return len(raw) <= 64<<10 && historyStrictJSON([]byte(raw), &original) == nil && json.Unmarshal([]byte(raw), &v) == nil && v.Client != nil && !*v.Client && v.Unverified != nil && !*v.Unverified && v.Access != nil && *v.Access
}

func (r *Runner) nativePartialGuard(ctx context.Context, path string) bool {
	if r.NativePartialGuard != nil {
		return r.NativePartialGuard(ctx, path)
	}
	raw, err := r.Run(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", path)
	return err == nil && nativePartialClearJSON(raw)
}

func (r *Runner) nativePartialVerify(ctx context.Context, v nativePartialInput, input, state json.RawMessage, root string) (nativePartialVerification, error) {
	zero := nativePartialVerification{}
	q, _, reply, err := r.nativePartialAuthority(ctx, v, state, root)
	if err != nil {
		return zero, nativePartialFailed("authority")
	}
	if !generatedWithin(root, v.Artifact.Path) || !r.nativePartialGuard(ctx, v.Artifact.Path) {
		return zero, nativePartialFailed("artifact_guard")
	}
	artifact, err := generatedQuestionRead(v.Artifact.Path, 1<<20)
	var artifactJSON any
	if err != nil || bytesHash(artifact) != v.Artifact.SHA || historyStrictJSON(artifact, &artifactJSON) != nil {
		return zero, nativePartialFailed("artifact_bytes")
	}
	// This is evidence retention, not an assertion that an arbitrary command
	// completed the product. Pin its scope to the original reply and revision.
	if !nativePartialArtifactIdentity(artifact, v) {
		return zero, nativePartialFailed("artifact_identity")
	}
	call := r.NativePartialRPC
	var rpc *generatedQuestionRPC
	if call == nil {
		rpc, err = generatedQuestionStartRPC(ctx)
		if err != nil {
			return zero, nativePartialFailed("api_start")
		}
		defer func() {
			if rpc != nil {
				_ = rpc.close()
			}
		}()
		call = rpc.call
	}
	nativeSHA, err := nativePartialLineage(ctx, v, q, reply, root, r.Now(), call)
	if err != nil {
		return zero, nativePartialFailed("lineage")
	}
	if rpc != nil {
		closeErr := rpc.close()
		rpc = nil
		if closeErr != nil {
			return zero, errNativePartial
		}
	}
	current, currentRoot, err := r.nativePartialState(ctx, v)
	if err != nil || canonicalHash(current) != canonicalHash(state) || currentRoot != root {
		return zero, errNativePartial
	}
	// A reply can be edited while the native API is being read. Recheck its
	// original source and signed admission after that work, before returning.
	if _, _, _, err = r.nativePartialAuthority(ctx, v, current, currentRoot); err != nil {
		return zero, err
	}
	if !r.nativePartialGuard(ctx, v.Artifact.Path) {
		return zero, errNativePartial
	}
	again, err := generatedQuestionRead(v.Artifact.Path, 1<<20)
	if err != nil || bytesHash(again) != v.Artifact.SHA {
		return zero, errNativePartial
	}
	return nativePartialVerification{Domain: "discord-native-partial-verification-v1", Verified: true, InputSHA: bytesHash(input), OriginSHA: bytesHash(state), RootSHA: bytesHash([]byte(root)), SourceSHA: v.SourceSHA, ReplySHA: v.ReplySHA, NativeSHA: nativeSHA, Acceptance: "pending"}, nil
}

func nativePartialToken(body, id string) bool {
	if id == "" {
		return false
	}
	for _, at := range regexp.MustCompile(regexp.QuoteMeta(id)).FindAllStringIndex(body, -1) {
		digit := func(b byte) bool { return b >= '0' && b <= '9' }
		if (at[0] == 0 || !digit(body[at[0]-1])) && (at[1] == len(body) || !digit(body[at[1]])) {
			return true
		}
	}
	return false
}

func nativePartialRequestToken(body, id string) bool {
	for _, at := range regexp.MustCompile(regexp.QuoteMeta(id)).FindAllStringIndex(body, -1) {
		word := func(b byte) bool {
			return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '_' || b == '-'
		}
		if (at[0] == 0 || !word(body[at[0]-1])) && (at[1] == len(body) || !word(body[at[1]])) {
			return true
		}
	}
	return false
}

// Read identity from explicit JSON fields, not incidental prose. These are
// the two existing artifact conventions; conflicting aliases are rejected.
func nativePartialArtifactIdentity(b []byte, v nativePartialInput) bool {
	var artifact map[string]json.RawMessage
	if historyStrictJSON(b, &artifact) != nil {
		return false
	}
	match := func(keys []string, want string) bool {
		found := false
		for _, key := range keys {
			raw, ok := artifact[key]
			if !ok {
				continue
			}
			var text string
			if json.Unmarshal(raw, &text) != nil || text != want {
				return false
			}
			found = true
		}
		return found
	}
	return match([]string{"revision", "main"}, v.Revision) && match([]string{"owner_reply", "ownerReply"}, v.Message)
}

func nativePartialLineage(ctx context.Context, v nativePartialInput, q ConversationQuestion, reply conversationReply, root string, now time.Time, call func(context.Context, string, map[string]any) (json.RawMessage, error)) (string, error) {
	read := func(method string, args map[string]any, out any) bool {
		b, err := call(ctx, method, args)
		var raw any
		return err == nil && len(b) <= 4<<20 && historyStrictJSON(b, &raw) == nil && json.Unmarshal(b, out) == nil
	}
	type thread struct {
		Thread struct {
			ID, Cwd string
			Parent  string `json:"parentThreadId"`
			Source  json.RawMessage
		}
	}
	var parent, child thread
	if !read("thread/read", map[string]any{"threadId": v.Parent, "includeTurns": false}, &parent) || !read("thread/read", map[string]any{"threadId": v.Child, "includeTurns": false}, &child) || parent.Thread.ID != v.Parent || child.Thread.ID != v.Child || child.Thread.Parent != v.Parent || !conversationMediaSamePath(filepath.FromSlash(parent.Thread.Cwd), filepath.FromSlash(root)) || !conversationMediaSamePath(filepath.FromSlash(child.Thread.Cwd), filepath.FromSlash(root)) {
		return "", errNativePartial
	}
	var source struct {
		Sub struct {
			Spawn struct {
				Parent string `json:"parent_thread_id"`
				Depth  int
			} `json:"thread_spawn"`
		} `json:"subAgent"`
	}
	if json.Unmarshal(child.Thread.Source, &source) != nil || source.Sub.Spawn.Parent != v.Parent || source.Sub.Spawn.Depth < 1 {
		return "", errNativePartial
	}
	type item struct {
		ID, Type, Tool, Status, Prompt, Command, Cwd string
		Sender                                       string   `json:"senderThreadId"`
		Receivers                                    []string `json:"receiverThreadIds"`
		Output                                       string   `json:"aggregatedOutput"`
		Exit                                         *int     `json:"exitCode"`
		Source                                       string
		Started                                      *int64                           `json:"startedAtMs"`
		Completed                                    *int64                           `json:"completedAtMs"`
		Actions                                      []struct{ Type, Command string } `json:"commandActions"`
	}
	get := func(thread, turn, id string) (item, bool) {
		var response struct {
			Data []struct {
				Turn      string `json:"turnId"`
				Item      item
				Started   *int64 `json:"startedAtMs"`
				Completed *int64 `json:"completedAtMs"`
			}
		}
		if !read("thread/items/read", map[string]any{"threadId": thread, "turnId": turn, "itemIds": []string{id}}, &response) || len(response.Data) != 1 || response.Data[0].Turn != turn || response.Data[0].Item.ID != id {
			return item{}, false
		}
		entry := response.Data[0]
		selected := entry.Item
		for _, pair := range [][2]*int64{{entry.Started, selected.Started}, {entry.Completed, selected.Completed}} {
			if pair[0] != nil && pair[1] != nil && *pair[0] != *pair[1] {
				return item{}, false
			}
		}
		if entry.Started != nil {
			selected.Started = entry.Started
		}
		if entry.Completed != nil {
			selected.Completed = entry.Completed
		}
		return selected, true
	}
	a, ok := get(v.Parent, v.Assignment.Turn, v.Assignment.Item)
	if !ok || a.Type != "collabAgentToolCall" || a.Tool != "sendInput" || a.Status != "completed" || a.Sender != v.Parent || len(a.Receivers) != 1 || a.Receivers[0] != v.Child || bytesHash([]byte(a.Prompt)) != v.Assignment.PromptSHA {
		return "", errNativePartial
	}
	// An actual parent assignment is required in addition to the child edge.
	// Exact IDs and source/version tie this work to this answer, not a title.
	requestBound := nativePartialRequestToken(a.Prompt, v.Request)
	if !nativePartialToken(a.Prompt, v.Message) || (!requestBound && (!nativePartialToken(a.Prompt, v.Origin) || !nativePartialToken(a.Prompt, q.Message))) {
		return "", errNativePartial
	}
	bodyBound := reply.Body != "" && strings.Contains(a.Prompt, reply.Body)
	if !strings.Contains(a.Prompt, v.SourceSHA) || (!bodyBound && !strings.Contains(a.Prompt, v.ReplySHA)) {
		return "", errNativePartial
	}
	witness := []any{parent, child, a}
	artifactBound := false
	appliedAt, _ := time.Parse(time.RFC3339Nano, v.Evidence.AppliedAt)
	repliedAt, err := time.Parse(time.RFC3339Nano, reply.Created)
	if err != nil {
		return "", errNativePartial
	}
	for _, selector := range v.Commands {
		c, ok := get(v.Child, selector.Turn, selector.Item)
		if !ok || c.Type != "commandExecution" || c.Status != "completed" || c.Source != "unifiedExecStartup" || c.Exit == nil || *c.Exit != 0 || c.Started == nil || c.Completed == nil || *c.Started <= 0 || *c.Completed < *c.Started || *c.Completed > now.UnixMilli() || *c.Completed > appliedAt.UnixMilli() || !conversationMediaSamePath(filepath.FromSlash(c.Cwd), filepath.FromSlash(root)) || bytesHash([]byte(c.Command)) != selector.CommandSHA || bytesHash([]byte(c.Output)) != selector.OutputSHA {
			return "", errNativePartial
		}
		if *c.Started < repliedAt.UnixMilli() || (a.Started != nil && *c.Started < *a.Started) {
			return "", errNativePartial
		}
		if len(c.Actions) == 1 && c.Actions[0].Type == "unknown" && nativePartialHashCommand(c.Command, c.Actions[0].Command, c.Output, v.Artifact) {
			artifactBound = true
		}
		witness = append(witness, c)
	}
	if !artifactBound {
		return "", errNativePartial
	}
	return canonicalHash(witness), nil
}

// The native command parser must report one exact, read-only literal hash
// command. Echoes, variables, comments, extra statements and same basenames
// at another path cannot certify the artifact.
func nativePartialHashCommand(rawCommand, command, output string, artifact nativePartialArtifact) bool {
	path := filepath.ToSlash(filepath.Clean(artifact.Path))
	if strings.ContainsAny(path, "'\r\n\x00") {
		return false
	}
	expected := "Get-FileHash -LiteralPath '" + path + "' -Algorithm SHA256 | Select-Object Path,Hash | ConvertTo-Json -Compress"
	if command != expected {
		return false
	}
	prefix, script, ok := strings.Cut(rawCommand, " -Command \"")
	if !ok || script != expected+"\"" || len(prefix) < 3 || prefix[0] != '"' || prefix[len(prefix)-1] != '"' {
		return false
	}
	executable := strings.ReplaceAll(prefix[1:len(prefix)-1], "\\\\", "\\")
	executable = strings.ReplaceAll(executable, "\\", "/")
	if !regexp.MustCompile(`(?i)^(?:[A-Z]:/Windows/System32/WindowsPowerShell/v1\.0/powershell\.exe|[A-Z]:/Program Files/WindowsApps/OpenAI\.CodexPrimaryRuntime\.[A-Za-z0-9_.-]+/dependencies/native/powershell/pwsh\.exe)$`).MatchString(executable) {
		return false
	}
	var result map[string]json.RawMessage
	if len(output) > 16<<10 || historyStrictJSON([]byte(output), &result) != nil || len(result) != 2 {
		return false
	}
	var actualPath, hash string
	if json.Unmarshal(result["Path"], &actualPath) != nil || json.Unmarshal(result["Hash"], &hash) != nil {
		return false
	}
	return conversationMediaSamePath(filepath.FromSlash(actualPath), filepath.FromSlash(artifact.Path)) && strings.EqualFold(hash, artifact.SHA)
}
