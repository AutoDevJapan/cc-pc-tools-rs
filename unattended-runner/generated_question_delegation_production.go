package main

// This reader invokes only documented local metadata APIs. It never resumes a
// thread, sends input, or turns a caller-authored projection into provenance.
import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type generatedQuestionRPC struct {
	cmd      *exec.Cmd
	in       io.WriteCloser
	out      *bufio.Scanner
	sequence int64
	cancel   context.CancelFunc
}

func generatedQuestionStartRPC(ctx context.Context) (*generatedQuestionRPC, error) {
	name := "codex"
	if runtime.GOOS == "windows" {
		name = "codex.exe"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, errConversationMedia
	}
	allowed, err := conversationMediaLiveGuard(ctx, path)
	if err != nil || !allowed {
		return nil, errConversationMedia
	}
	child, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(child, path, "app-server", "--listen", "stdio://")
	hide(cmd)
	cmd.WaitDelay = time.Second
	cmd.Stderr = io.Discard
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(key) {
		case "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "OPENROUTER_API_KEY", "GEMINI_API_KEY", "GOOGLE_API_KEY":
			continue
		}
		cmd.Env = append(cmd.Env, entry)
	}
	return generatedQuestionStartRPCProcess(child, cancel, cmd)
}

func generatedQuestionStartRPCProcess(ctx context.Context, cancel context.CancelFunc, cmd *exec.Cmd) (*generatedQuestionRPC, error) {
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, errConversationMedia
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		in.Close()
		cancel()
		return nil, errConversationMedia
	}
	if cmd.Start() != nil {
		in.Close()
		cancel()
		return nil, errConversationMedia
	}
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 4096), 4<<20)
	r := &generatedQuestionRPC{cmd: cmd, in: in, out: scanner, cancel: cancel}
	_, err = r.call(ctx, "initialize", map[string]any{"clientInfo": map[string]any{"name": "generated-question-lineage-reader", "version": "1"}, "capabilities": map[string]any{"experimentalApi": true}})
	if err == nil {
		err = json.NewEncoder(in).Encode(map[string]any{"method": "initialized", "params": map[string]any{}})
	}
	if err != nil {
		r.close()
		return nil, errConversationMedia
	}
	return r, nil
}

func generatedQuestionReadMethod(method string) bool {
	return method == "initialize" || method == "thread/read" || method == "thread/items/read"
}

func (r *generatedQuestionRPC) call(ctx context.Context, method string, params map[string]any) (json.RawMessage, error) {
	if ctx.Err() != nil || !generatedQuestionReadMethod(method) {
		return nil, errConversationMedia
	}
	r.sequence++
	if json.NewEncoder(r.in).Encode(map[string]any{"id": r.sequence, "method": method, "params": params}) != nil {
		return nil, errConversationMedia
	}
	for i := 0; i < 32 && r.out.Scan(); i++ {
		var original map[string]json.RawMessage
		raw := r.out.Bytes()
		if historyStrictJSON(raw, &original) != nil {
			return nil, errConversationMedia
		}
		var response struct {
			ID     int64
			Result json.RawMessage
			Error  json.RawMessage
			Method string
		}
		if json.Unmarshal(raw, &response) != nil {
			return nil, errConversationMedia
		}
		// Never service a server request/approval on this read-only connection.
		if response.Method != "" && len(original["id"]) != 0 {
			return nil, errConversationMedia
		}
		if response.ID != r.sequence {
			continue
		}
		if len(response.Error) != 0 && string(response.Error) != "null" {
			return nil, errConversationMedia
		}
		if len(response.Result) == 0 || string(response.Result) == "null" {
			return nil, errConversationMedia
		}
		return append(json.RawMessage(nil), response.Result...), nil
	}
	return nil, errConversationMedia
}

func (r *generatedQuestionRPC) close() error {
	defer r.cancel()
	_ = r.in.Close()
	done := make(chan error, 1)
	go func() { done <- r.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(time.Second):
		r.cancel()
		return <-done
	}
}

func generatedQuestionDelegationProduction(ctx context.Context, s generatedQuestionSpec, text string) error {
	ctx, stop := context.WithTimeout(ctx, 15*time.Second)
	defer stop()
	if s.Delegation == nil {
		return errConversationMedia
	}
	// Check the canonical resource edge before starting an API reader. Missing
	// native job identity is a dependency, never a fabricated ordinary job.
	if _, err := generatedQuestionReadOriginJob(ctx, s.Delegation.Job, s.Scope.Todo); err != nil {
		return generatedFailure("native_delegation", "native_resource_edge_unavailable")
	}
	rpc, err := generatedQuestionStartRPC(ctx)
	if err != nil {
		return generatedFailure("native_delegation", "native_api_unavailable")
	}
	result := (generatedQuestionLineage{Call: rpc.call, Job: generatedQuestionReadOriginJob}).verify(ctx, s, text)
	closeErr := rpc.close()
	if result != nil {
		return result
	}
	if closeErr != nil {
		return generatedFailure("native_delegation", "native_reader_close_failed")
	}
	return nil
}

func generatedQuestionReadOriginJob(ctx context.Context, id string, todo uint64) (generatedQuestionOriginJob, error) {
	return generatedQuestionOriginJobAt(ctx, id, todo, "D:/CC/Tools/codex-jobs/jobs", generatedQuestionRead, conversationMediaLiveGuard, func(ctx context.Context, todo uint64) (*TodoRow, error) {
		r := &Runner{TodoPath: "D:/CC/Tools/todo/todo.json", Run: runCommand}
		return r.conversationTask(ctx, todo)
	})
}

func generatedQuestionOriginJobAt(ctx context.Context, id string, todo uint64, jobs string, read func(string, int64) ([]byte, error), guard func(context.Context, string) (bool, error), task func(context.Context, uint64) (*TodoRow, error)) (generatedQuestionOriginJob, error) {
	var zero generatedQuestionOriginJob
	if !validJob(id) || todo == 0 || ctx.Err() != nil {
		return zero, errConversationMedia
	}
	row, err := task(ctx, todo)
	if err != nil || row == nil || row.ID != todo || conversationActorHeld(row.conversationState) || (row.Status != "doing" && row.Status != "todo") || row.Worker.Job != id || row.Scheduler.Job != id {
		return zero, errConversationMedia
	}
	var original any
	if historyStrictJSON(row.conversationState, &original) != nil {
		return zero, errConversationMedia
	}
	found := 0
	for _, raw := range row.Jobs {
		var job string
		if json.Unmarshal(raw, &job) == nil && job == id {
			found++
		}
	}
	if found != 1 {
		return zero, errConversationMedia
	}
	path := filepath.Join(jobs, id, "job.json")
	allowed, err := guard(ctx, path)
	if err != nil || !allowed {
		return zero, errConversationMedia
	}
	raw, err := read(path, 1<<20)
	var exact map[string]json.RawMessage
	var job generatedQuestionOriginJob
	if err != nil || historyStrictJSON(raw, &exact) != nil || json.Unmarshal(raw, &job) != nil || job.ID != id || job.Todo != todo || !generatedUUID.MatchString(job.Thread) || !conversationMediaSamePath(filepath.FromSlash(job.Cwd), filepath.FromSlash(row.conversationRoot)) || !conversationMediaSamePath(filepath.FromSlash(job.Cwd), filepath.FromSlash(row.Scheduler.Worktree)) || job.Stopped || job.OwnerStopped || job.Cancelled || job.CancelReason != "" || (job.Status != "running" && job.Status != "succeeded") {
		return zero, errConversationMedia
	}
	job.ResourceSHA = bytesHash(row.conversationState)
	return job, nil
}
