//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

const unslopHistoryLauncher = "D:/CC/Tools/codex-progress-tui/codex-launch.exe"

type unslopHistoryFrame struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}
type unslopHistoryRequest struct {
	Method string
	Params any
}

// Concrete finite values handed to the normal protocol controller. There is
// no caller function/Reader/Writer to execute while a child is alive.
type unslopHistoryStep struct {
	Input      []byte
	ResponseID int64
}
type unslopHistoryProtocol struct{ Steps []unslopHistoryStep }

func buildUnslopHistoryProtocol(requests []unslopHistoryRequest) (unslopHistoryProtocol, error) {
	raw, e := unslopHistoryInput(requests)
	if e != nil {
		return unslopHistoryProtocol{}, e
	}
	defer clear(raw)
	lines := bytes.Split(bytes.TrimSuffix(raw, []byte{'\n'}), []byte{'\n'})
	if len(lines) != len(requests)+2 {
		return unslopHistoryProtocol{}, errUnslopOrigin
	}
	plan := unslopHistoryProtocol{Steps: []unslopHistoryStep{{Input: append(append([]byte(nil), lines[0]...), '\n'), ResponseID: 1}}}
	for i := range requests {
		input := append(append([]byte(nil), lines[i+2]...), '\n')
		if i == 0 {
			input = append(append(append([]byte(nil), lines[1]...), '\n'), input...)
		}
		plan.Steps = append(plan.Steps, unslopHistoryStep{Input: input, ResponseID: int64(i + 2)})
	}
	return plan, nil
}

func acceptUnslopHistoryProtocolFrame(raw []byte, expected int64) (json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > 2<<20 {
		return nil, errUnslopOrigin
	}
	var frame unslopHistoryFrame
	if json.Unmarshal(raw, &frame) != nil || frame.ID == nil || *frame.ID != expected || frame.Method != "" || len(frame.Error) > 0 && string(frame.Error) != "null" || len(frame.Result) == 0 || string(frame.Result) == "null" {
		return nil, errUnslopOrigin
	}
	return frame.Result, nil
}

func unslopHistoryInput(requests []unslopHistoryRequest) ([]byte, error) {
	if len(requests) == 0 || len(requests) > 16 {
		return nil, errUnslopOrigin
	}
	var input bytes.Buffer
	encode := json.NewEncoder(&input)
	if encode.Encode(map[string]any{"id": int64(1), "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "software_history_reader", "version": "1.0"}, "capabilities": map[string]bool{"experimentalApi": true}}}) != nil {
		return nil, errUnslopOrigin
	}
	_ = encode.Encode(map[string]any{"method": "initialized", "params": map[string]any{}})
	for i, r := range requests {
		switch r.Method {
		case "thread/read", "thread/turns/list", "thread/items/list", "thread/items/read":
		default:
			return nil, errUnslopOrigin
		}
		if encode.Encode(map[string]any{"id": int64(i + 2), "method": r.Method, "params": r.Params}) != nil {
			return nil, errUnslopOrigin
		}
	}
	if input.Len() > 1<<20 {
		return nil, errUnslopOrigin
	}
	return input.Bytes(), nil
}

func parseUnslopHistoryBatch(raw []byte, count int) ([]json.RawMessage, error) {
	if len(raw) == 0 || len(raw) > 8<<20 || count < 1 || count > 16 {
		return nil, errors.New("history_response_size_invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	answers := map[int64]json.RawMessage{}
	accepted := false
	defer func() {
		if !accepted {
			for _, answer := range answers {
				clear(answer)
			}
		}
	}()
	notifications := 0
	for {
		var f unslopHistoryFrame
		e := decoder.Decode(&f)
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, errors.New("history_response_json_invalid")
		}
		if f.ID == nil {
			notifications++
			if f.Method == "" || notifications > 32 {
				return nil, errUnslopOrigin
			}
			continue
		}
		id := *f.ID
		if len(f.Error) > 0 && string(f.Error) != "null" {
			var failure struct {
				Code int `json:"code"`
			}
			if json.Unmarshal(f.Error, &failure) == nil {
				return nil, fmt.Errorf("history_rpc_error_%d", failure.Code)
			}
			return nil, errors.New("history_rpc_error")
		}
		if id < 1 || id > int64(count+1) || answers[id] != nil || len(f.Result) == 0 || string(f.Result) == "null" {
			return nil, errUnslopOrigin
		}
		answers[id] = f.Result
	}
	if len(answers) != count+1 {
		return nil, errors.New("history_response_count_incomplete")
	}
	result := make([]json.RawMessage, count)
	for i := range result {
		result[i] = answers[int64(i+2)]
	}
	clear(answers[1])
	accepted = true
	return result, nil
}

func runUnslopHistoryBatch(ctx context.Context, requests []unslopHistoryRequest) ([]json.RawMessage, error) {
	input, e := unslopHistoryInput(requests)
	if e != nil {
		return nil, e
	}
	defer clear(input)
	raw, ok := askGitCommand(ctx, installedGuardPath, "--guard-path", unslopHistoryLauncher)
	var q struct {
		Allowed bool `json:"access_allowed"`
		Client  bool `json:"client_resource"`
	}
	e = json.Unmarshal(raw, &q)
	clear(raw)
	if !ok || e != nil || !q.Allowed || q.Client {
		return nil, errors.New("history_launcher_unqualified")
	}
	pin, e := openUpdateObject(unslopHistoryLauncher)
	if e != nil {
		return nil, e
	}
	defer pin.Close()
	token, e := syscall.OpenCurrentProcessToken()
	if e != nil {
		return nil, e
	}
	profile, e := token.GetUserProfileDirectory()
	token.Close()
	if e != nil {
		return nil, e
	}
	settings := map[string]string{"USERPROFILE": profile, "HOME": profile, "CODEX_HOME": filepath.Join(profile, ".codex"), "LOCALAPPDATA": filepath.Join(profile, "AppData/Local"), "APPDATA": filepath.Join(profile, "AppData/Roaming")}
	for _, name := range []string{"SYSTEMROOT", "WINDIR", "TEMP", "TMP", "PATH"} {
		if value, ok := os.LookupEnv(name); ok {
			settings[name] = value
		}
	}
	childCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	childCtx = context.WithValue(childCtx, askChildEnvironment{}, settings)
	childCtx = context.WithValue(childCtx, askChildExactEnvironment{}, true)
	childCtx = context.WithValue(childCtx, askChildCaptureKey{}, askChildCapture{Stdin: input})
	child := askOwnedChildAt(childCtx, filepath.Dir(unslopHistoryLauncher), unslopHistoryLauncher, "app-server", "--listen", "stdio://")
	defer clear(child.Raw)
	if child.Capture == nil {
		return nil, errors.New("history_capture_unavailable")
	}
	defer clear(child.Capture.Stdout)
	if !child.Capture.Complete || child.Capture.Overflow || !child.Reaped || !child.WaitVerified || child.Exit != 0 || child.TimedOut {
		return nil, errors.New("history_process_collection_unavailable")
	}
	answers, e := parseUnslopHistoryBatch(child.Capture.Stdout, len(requests))
	if e != nil {
		return nil, e
	}
	return answers, nil
}

// The original controller supplies these selectors. Reading metadata never
// grants permission to the protected packet or invents its original tuple.
func normalUnslopHistoryInspection(ctx context.Context, threads [2]string) unslopOriginInspection {
	result := unslopOriginInspection{Reason: "normal_history_binding_unavailable"}
	if threads[0] == "" || threads[1] == "" || threads[0] == threads[1] {
		return result
	}
	requests := make([]unslopHistoryRequest, 0, 4)
	for _, thread := range threads {
		requests = append(requests, unslopHistoryRequest{"thread/read", map[string]any{"threadId": thread, "includeTurns": false}}, unslopHistoryRequest{"thread/turns/list", map[string]any{"threadId": thread, "itemsView": "notLoaded", "limit": 20, "sortDirection": "desc"}})
	}
	replies, e := runUnslopHistoryBatch(ctx, requests)
	if e != nil {
		result.Reason = "normal_history_provider_unavailable"
		return result
	}
	defer func() {
		for _, raw := range replies {
			clear(raw)
		}
	}()
	for i, thread := range threads {
		var metadata struct {
			Thread struct {
				ID    string
				Turns []json.RawMessage
			}
		}
		if json.Unmarshal(replies[i*2], &metadata) != nil || metadata.Thread.ID != thread || len(metadata.Thread.Turns) != 0 {
			result.Reason = "normal_history_thread_mismatch"
			return result
		}
		result.ThreadsMatched++
		var turns struct {
			Data []struct {
				ID, Status, ItemsView string
				Items                 []json.RawMessage
			}
			NextCursor *string
		}
		if json.Unmarshal(replies[i*2+1], &turns) != nil || len(turns.Data) > 20 {
			result.Reason = "normal_history_turn_mismatch"
			return result
		}
		if turns.NextCursor != nil && *turns.NextCursor != "" {
			result.Reason = "normal_history_pagination_incomplete"
			return result
		}
		for _, turn := range turns.Data {
			if turn.ID == "" || turn.ItemsView != "notLoaded" || len(turn.Items) != 0 {
				result.Reason = "normal_history_turn_mismatch"
				return result
			}
			if turn.Status == "completed" {
				result.CompletedTurns++
			}
		}
	}
	result.Reason = "original_producer_binding_unavailable"
	return result
}
