package main

// Native partial records preserve authenticated work evidence. They never
// satisfy application, close, task completion or permission to resume work.
import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"
)

const nativePartialKind = "native_partial_implementation"
const nativePartialDomain = "discord-native-partial-implementation-v1"
const nativePartialReceiptDomain = "todo-native-partial-implementation-record-v1"

var errNativePartial = errors.New("native partial evidence unverified")

type nativePartialError struct{ stage string }

func (e *nativePartialError) Error() string  { return errNativePartial.Error() }
func (e *nativePartialError) Unwrap() error  { return errNativePartial }
func nativePartialFailed(stage string) error { return &nativePartialError{stage: stage} }

type nativePartialSelector struct {
	Turn      string `json:"turn_id"`
	Item      string `json:"item_id"`
	PromptSHA string `json:"prompt_sha256"`
}
type nativePartialCommand struct {
	Turn       string `json:"turn_id"`
	Item       string `json:"item_id"`
	CommandSHA string `json:"command_sha256"`
	OutputSHA  string `json:"output_sha256"`
}
type nativePartialArtifact struct {
	Path string `json:"path"`
	SHA  string `json:"sha256"`
}
type nativePartialInput struct {
	Domain     string                             `json:"domain"`
	Kind       string                             `json:"kind"`
	Request    string                             `json:"request_id"`
	Message    string                             `json:"message_id"`
	SourceSHA  string                             `json:"source_receipt_sha256"`
	ReplySHA   string                             `json:"reply_sha256"`
	Origin     string                             `json:"origin_todo_id"`
	Project    string                             `json:"project"`
	Revision   string                             `json:"revision"`
	Parent     string                             `json:"parent_thread_id"`
	Child      string                             `json:"child_thread_id"`
	Assignment nativePartialSelector              `json:"assignment"`
	Commands   []nativePartialCommand             `json:"verification_commands"`
	Artifact   nativePartialArtifact              `json:"artifact"`
	Evidence   conversationImplementationEvidence `json:"evidence"`
}
type nativePartialVerification struct {
	Domain     string `json:"domain"`
	Verified   bool   `json:"verified"`
	InputSHA   string `json:"input_sha256"`
	OriginSHA  string `json:"origin_state_sha256"`
	RootSHA    string `json:"project_path_sha256"`
	SourceSHA  string `json:"source_receipt_sha256"`
	ReplySHA   string `json:"reply_sha256"`
	NativeSHA  string `json:"native_evidence_sha256"`
	Acceptance string `json:"acceptance"`
	Applied    bool   `json:"application_inferred"`
	Closed     bool   `json:"question_closed_get_verified"`
}
type nativePartialResult struct {
	Domain     string `json:"domain"`
	Kind       string `json:"kind"`
	Recorded   bool   `json:"recorded"`
	Changed    bool   `json:"changed"`
	InputSHA   string `json:"input_sha256"`
	ReceiptSHA string `json:"receipt_sha256"`
	Acceptance string `json:"acceptance"`
	Applied    bool   `json:"application_inferred"`
	Closed     bool   `json:"question_closed_get_verified"`
}
type nativePartialReceipt struct {
	Domain   string             `json:"domain"`
	Kind     string             `json:"kind"`
	Input    nativePartialInput `json:"input"`
	InputSHA string             `json:"input_sha256"`
}

func nativePartialResultDecode(b []byte) (nativePartialResult, error) {
	var result nativePartialResult
	var fields map[string]json.RawMessage
	keys := []string{"domain", "kind", "recorded", "changed", "input_sha256", "receipt_sha256", "acceptance", "application_inferred", "question_closed_get_verified"}
	if historyStrictJSON(b, &fields) != nil || len(fields) != len(keys) {
		return result, errNativePartial
	}
	for _, key := range keys {
		if _, ok := fields[key]; !ok {
			return result, errNativePartial
		}
	}
	for _, key := range []string{"recorded", "changed", "application_inferred", "question_closed_get_verified"} {
		if string(fields[key]) != "true" && string(fields[key]) != "false" {
			return result, errNativePartial
		}
	}
	if historyStrictJSON(b, &result) != nil {
		return result, errNativePartial
	}
	return result, nil
}

func nativePartialDecode(b []byte) (nativePartialInput, error) {
	var v nativePartialInput
	var original any
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if len(b) == 0 || len(b) > 1<<20 || historyStrictJSON(b, &original) != nil || d.Decode(&v) != nil || d.Decode(new(any)) != io.EOF {
		return v, errNativePartial
	}
	_, originOK := number(v.Origin)
	_, messageOK := number(v.Message)
	if !originOK || !messageOK || v.Domain != nativePartialDomain || v.Kind != nativePartialKind || !conversationMediaNormal.MatchString(v.Request) || !shaPattern.MatchString(v.SourceSHA) || !shaPattern.MatchString(v.ReplySHA) || !regexpRevision(v.Revision) || v.Revision != strings.ToLower(v.Revision) || v.Project == "" || v.Project == "owner" || !generatedUUID.MatchString(v.Parent) || !generatedUUID.MatchString(v.Child) || v.Parent == v.Child || !generatedUUID.MatchString(v.Assignment.Turn) || !nativePartialItem(v.Assignment.Item) || !shaPattern.MatchString(v.Assignment.PromptSHA) || !filepath.IsAbs(filepath.FromSlash(v.Artifact.Path)) || !shaPattern.MatchString(v.Artifact.SHA) || len(v.Commands) < 1 || len(v.Commands) > 16 {
		return v, errNativePartial
	}
	seen := map[string]bool{}
	for _, c := range v.Commands {
		key := c.Turn + "/" + c.Item
		if !generatedUUID.MatchString(c.Turn) || !nativePartialItem(c.Item) || !shaPattern.MatchString(c.CommandSHA) || !shaPattern.MatchString(c.OutputSHA) || seen[key] {
			return v, errNativePartial
		}
		seen[key] = true
	}
	for _, s := range []string{v.Evidence.Completion, v.Evidence.Execution, v.Evidence.Origin, v.Evidence.Identity, v.Evidence.AppliedAt} {
		if strings.TrimSpace(s) == "" || len(s) > 16<<10 || strings.ContainsRune(s, '\x00') {
			return v, errNativePartial
		}
	}
	if at, err := time.Parse(time.RFC3339Nano, v.Evidence.AppliedAt); err != nil || at.Unix() <= 0 {
		return v, errNativePartial
	}
	return v, nil
}

func nativePartialItem(s string) bool {
	if len(s) == 0 || len(s) > 192 {
		return false
	}
	for _, b := range []byte(s) {
		if !(b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_' || b == '-') {
			return false
		}
	}
	return true
}

func nativePartialFile(r *Runner, v nativePartialInput, b []byte) string {
	return filepath.Join(r.Config.Home, "conversations", v.Request, "replies", v.Message+"-"+bytesHash(b)+".native-partial.json")
}

// A partial record must block every old completion fallback for this version.
func nativePartialPending(row *TodoRow, q ConversationQuestion, message, source string) (bool, error) {
	found := false
	for _, raw := range row.NativePartialRecords {
		var receipt nativePartialReceipt
		if json.Unmarshal(raw, &receipt) != nil {
			return false, errNativePartial
		}
		if receipt.Input.Request != q.RequestID || receipt.Input.Message != message || receipt.Input.SourceSHA != source {
			continue
		}
		if found || receipt.Domain != nativePartialReceiptDomain || receipt.Kind != nativePartialKind || !shaPattern.MatchString(receipt.InputSHA) || receipt.Input.Origin != fmt.Sprint(q.Todo) || receipt.Input.Project != q.Project {
			return false, errNativePartial
		}
		found = true
	}
	return found, nil
}

// No runner state write is made here. The canonical writer re-verifies the
// current API/source/native evidence under todo.lock, then returns a receipt.
func (r *Runner) nativePartialRecord(ctx context.Context, path string) (map[string]any, error) {
	if !r.nativePartialGuard(ctx, path) {
		return nil, nativePartialFailed("input_guard")
	}
	b, err := generatedQuestionRead(path, 1<<20)
	if err != nil {
		return nil, errNativePartial
	}
	v, err := nativePartialDecode(b)
	if err != nil {
		return nil, err
	}
	state, root, err := r.nativePartialState(ctx, v)
	if err != nil {
		return nil, err
	}
	if _, err = r.nativePartialVerify(ctx, v, b, state, root); err != nil {
		return nil, err
	}
	file := nativePartialFile(r, v, b)
	if writeConversationOnce(file, b) != nil {
		return nil, errNativePartial
	}
	var result nativePartialResult
	for _, verify := range []bool{false, true} {
		state, root, err = r.nativePartialState(ctx, v)
		if err != nil {
			return nil, err
		}
		stateFile := file + "-" + bytesHash(state) + ".state.json"
		scopeFile := file + "-" + bytesHash([]byte(root)) + ".scope.json"
		scope, _ := json.Marshal(conversationTodoScope{Domain: "discord-conversation-todo-scope-v1", Todo: v.Origin, Project: v.Project, RootSHA: bytesHash([]byte(root))})
		if writeConversationOnce(stateFile, state) != nil || writeConversationOnce(scopeFile, scope) != nil {
			return nil, errNativePartial
		}
		args := []string{"native-implementation-record", "--input-file", file, "--origin-state-file", stateFile, "--origin-scope-file", scopeFile}
		if verify {
			args = append(args, "--verify-only")
		}
		raw, runErr := r.Run(ctx, conversationTodo, args...)
		current, decodeErr := nativePartialResultDecode([]byte(raw))
		if runErr != nil || decodeErr != nil || current.Domain != nativePartialReceiptDomain || current.Kind != nativePartialKind || !current.Recorded || current.InputSHA != bytesHash(b) || !shaPattern.MatchString(current.ReceiptSHA) || current.Acceptance != "pending" || current.Applied || current.Closed || (verify && (current.Changed || current.ReceiptSHA != result.ReceiptSHA)) {
			return nil, errNativePartial
		}
		if !verify {
			result = current
		}
	}
	return map[string]any{"domain": result.Domain, "kind": result.Kind, "recorded": true, "changed": result.Changed, "input_sha256": result.InputSHA, "receipt_sha256": result.ReceiptSHA, "acceptance": "pending", "application_inferred": false, "question_closed_get_verified": false, "origin_resumed": false}, nil
}
