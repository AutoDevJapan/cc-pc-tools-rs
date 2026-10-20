package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

//go:embed decision-worker.md
var decisionWorker string

const todoExe = `D:\CC\Tools\todo\todo.exe`

type decisionReceipt struct {
	Status         string `json:"status"`
	TodoID         int64  `json:"todo_id,omitempty"`
	Generation     string `json:"generation,omitempty"`
	WorkerLaunched bool   `json:"worker_launched"`
}

func queueDecision(r report) *decisionReceipt {
	return queueDecisionWith(r, runtimeRoot, func(ctx context.Context, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, todoExe, args...)
		cmd.WaitDelay = 2 * time.Second
		hide(cmd)
		cmd.Stderr = io.Discard
		return cmd.Output()
	})
}

func queueDecisionWith(r report, home string, call func(context.Context, ...string) ([]byte, error)) *decisionReceipt {
	failure := &decisionReceipt{Status: "queue_unavailable"}
	if !safePath(home) {
		return failure
	}
	// Only newly generated anonymous data and the compiled finite instruction
	// are written. Neither checkpoint paths nor the protection registry is read.
	write := func(pattern string, data []byte) (string, error) {
		f, err := os.CreateTemp(home, pattern)
		if err != nil {
			return "", err
		}
		name := f.Name()
		_, err = f.Write(data)
		if err == nil {
			err = f.Sync()
		}
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(name)
			return "", err
		}
		return name, nil
	}
	r.Decision = nil
	data, err := json.Marshal(r)
	if err != nil {
		return failure
	}
	reportPath, err := write("decision-measurement-*.json", data)
	if err != nil {
		return failure
	}
	defer os.Remove(reportPath)
	specPath, err := write("decision-instruction-*.md", []byte(decisionWorker))
	if err != nil {
		return failure
	}
	defer os.Remove(specPath)
	if !within(home, reportPath, false) || !within(home, specPath, false) {
		return failure
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	output, err := call(ctx, "capacity-cycle", "--report-file", filepath.Clean(reportPath), "--spec", filepath.Clean(specPath), "--project", "cc-pc-tools-rs")
	if err != nil || len(output) > 4096 {
		return failure
	}
	var got struct {
		TodoID         int64  `json:"todo_id"`
		Generation     string `json:"generation"`
		Action         string `json:"action"`
		WorkerLaunched *bool  `json:"worker_launched"`
	}
	if json.Unmarshal(output, &got) != nil || got.TodoID <= 0 || len(got.Generation) != 64 || got.WorkerLaunched == nil || *got.WorkerLaunched {
		return failure
	}
	switch got.Action {
	case "queued", "reused_completed", "retained_unfinished_or_same_generation":
	default:
		return failure
	}
	return &decisionReceipt{Status: got.Action, TodoID: got.TodoID, Generation: got.Generation}
}
