package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func generatedOriginFixture(t *testing.T) (*TodoRow, generatedQuestionOriginJob) {
	t.Helper()
	root := t.TempDir()
	row := &TodoRow{ID: 3084, Status: "doing", Jobs: []json.RawMessage{json.RawMessage(`"original-job"`)}, conversationRoot: root, conversationState: json.RawMessage(`{"id":3084,"status":"doing"}`)}
	row.Worker.Job = "original-job"
	row.Scheduler.Job = "original-job"
	row.Scheduler.Worktree = root
	job := generatedQuestionOriginJob{ID: "original-job", Todo: 3084, Thread: "11234567-0123-0123-0123-012345678901", Cwd: root, Status: "running"}
	return row, job
}

func TestGeneratedQuestionCanonicalResourceEdgeIsIndependent(t *testing.T) {
	for _, kind := range []string{"valid", "missing-task", "other-task", "missing-worker", "missing-scheduler", "missing-history", "duplicate-history", "task-stop", "bad-task-json", "job-id", "job-task", "job-session", "job-cwd", "job-stopped", "job-cancelled", "job-status", "guard-denied", "duplicate-job-json"} {
		t.Run(kind, func(t *testing.T) {
			row, job := generatedOriginFixture(t)
			taskErr := error(nil)
			allowed := true
			switch kind {
			case "missing-task":
				taskErr = errors.New("unavailable")
			case "other-task":
				row.ID++
			case "missing-worker":
				row.Worker.Job = ""
			case "missing-scheduler":
				row.Scheduler.Job = ""
			case "missing-history":
				row.Jobs = nil
			case "duplicate-history":
				row.Jobs = append(row.Jobs, row.Jobs[0])
			case "task-stop":
				row.conversationState = json.RawMessage(`{"stopped":true}`)
			case "bad-task-json":
				row.conversationState = json.RawMessage(`{"id":3084,"id":3084}`)
			case "job-id":
				job.ID = "another-job"
			case "job-task":
				job.Todo++
			case "job-session":
				job.Thread = "unknown"
			case "job-cwd":
				job.Cwd = filepath.Dir(job.Cwd)
			case "job-stopped":
				job.Stopped = true
			case "job-cancelled":
				job.CancelReason = "manual"
			case "job-status":
				job.Status = "cancelled"
			case "guard-denied":
				allowed = false
			}
			raw, err := json.Marshal(job)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "duplicate-job-json" {
				raw = append([]byte(`{"id":"forged",`), raw[1:]...)
			}
			reads := 0
			read := func(path string, limit int64) ([]byte, error) {
				reads++
				if filepath.Base(path) != "job.json" || filepath.Base(filepath.Dir(path)) != "original-job" || limit != 1<<20 {
					t.Fatal("read escaped origin metadata")
				}
				return raw, nil
			}
			got, err := generatedQuestionOriginJobAt(context.Background(), "original-job", 3084, t.TempDir(), read, func(context.Context, string) (bool, error) { return allowed, nil }, func(context.Context, uint64) (*TodoRow, error) { return row, taskErr })
			if kind == "valid" {
				if err != nil || got.ID != "original-job" || reads != 1 {
					t.Fatal("canonical control failed", got, err, reads)
				}
			} else if err == nil {
				t.Fatal("ineligible resource edge accepted", kind)
			}
			if kind == "guard-denied" && reads != 0 {
				t.Fatal("job guard did not precede metadata read")
			}
		})
	}
}

func TestGeneratedQuestionReadonlyApiAllowlist(t *testing.T) {
	for _, method := range []string{"initialize", "thread/read", "thread/items/read"} {
		if !generatedQuestionReadMethod(method) {
			t.Fatal("normal reader missing", method)
		}
	}
	for _, method := range []string{"thread/resume", "thread/start", "turn/start", "thread/inject_items", "thread/shellCommand", "thread/items/list", "config/value/write", "account/login/start"} {
		if generatedQuestionReadMethod(method) {
			t.Fatal("writer or unbounded route admitted", method)
		}
	}
}

func TestGeneratedQuestionRPCFixtureProcess(t *testing.T) {
	mode := os.Getenv("GENERATED_QUESTION_RPC_FIXTURE")
	if mode == "" {
		return
	}
	if mode == "normal" {
		defer os.Exit(0)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     int64
			Method string
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		if request.Method == "initialized" {
			continue
		}
		if request.Method != "initialize" && mode == "stalled" {
			time.Sleep(time.Hour)
		}
		if mode == "server-request" && request.Method != "initialize" {
			json.NewEncoder(os.Stdout).Encode(map[string]any{"id": 99, "method": "item/tool/requestUserInput", "params": map[string]any{}})
			continue
		}
		json.NewEncoder(os.Stdout).Encode(map[string]any{"id": request.ID, "result": map[string]any{"thread": map[string]any{"id": "fixture-thread"}}})
	}
	os.Exit(0)
}

func TestGeneratedQuestionRPCProcessBudgetAndClose(t *testing.T) {
	for _, mode := range []string{"normal", "stalled", "server-request"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGeneratedQuestionRPCFixtureProcess$")
			cmd.Env = append(os.Environ(), "GENERATED_QUESTION_RPC_FIXTURE="+mode)
			hide(cmd)
			cmd.WaitDelay = time.Second
			rpc, err := generatedQuestionStartRPCProcess(ctx, cancel, cmd)
			if err != nil {
				cancel()
				t.Fatal("normal initialize failed", err)
			}
			if mode == "stalled" {
				time.AfterFunc(50*time.Millisecond, cancel)
			}
			_, err = rpc.call(ctx, "thread/read", map[string]any{"threadId": "fixture-thread", "includeTurns": false})
			closeErr := rpc.close()
			if mode == "normal" && (err != nil || closeErr != nil) {
				t.Fatal("normal reader failed", err, closeErr)
			}
			if mode != "normal" && err == nil {
				t.Fatal("unanswered request or stalled reader accepted")
			}
			if cmd.ProcessState == nil {
				t.Fatal("reader process not reaped")
			}
			if mode == "normal" && !cmd.ProcessState.Success() {
				t.Fatal("normal child did not exit successfully")
			}
		})
	}
}
