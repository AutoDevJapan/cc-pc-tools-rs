package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// record never needs Discord credentials or a running parent process. The
// existing periodic task consumes the parent's saved receipt afterwards.
func agentResultCLI(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("agent-result", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	config := fs.String("config", conversationDefaultConfig, "")
	if fs.Parse(args) != nil || fs.NArg() == 0 {
		_, _ = io.WriteString(out, "invalid agent result arguments\n")
		return 2
	}
	command := fs.Arg(0)
	op := flag.NewFlagSet(command, flag.ContinueOnError)
	op.SetOutput(io.Discard)
	input := op.String("input", "", "")
	id := op.String("id", "", "")
	if op.Parse(fs.Args()[1:]) != nil || op.NArg() != 0 {
		_, _ = io.WriteString(out, "invalid agent result arguments\n")
		return 2
	}
	if (command == "record" || command == "watch") && (*input == "" || *id != "") || (command == "status" || command == "resume") && (*id == "" || *input != "") || command == "drain" && (*id != "" || *input != "") || !strings.Contains("|record|watch|status|resume|drain|", "|"+command+"|") {
		_, _ = io.WriteString(out, "invalid agent result arguments\n")
		return 2
	}
	var backend conversationCLIBackend
	var unlock func()
	var err error
	if command == "record" {
		backend, unlock, err = loadAgentResultReceiver(*config)
	} else {
		backend, unlock, err = loadConversationRunner(*config, conversationLoadOptions{Credentials: command == "drain"})
	}
	if err != nil {
		if errors.Is(err, errAgentReceiverBusy) {
			_, _ = io.WriteString(out, "busy_retry_same_input\n")
			return 75
		}
		_, _ = io.WriteString(out, "agent result storage unavailable\n")
		return 1
	}
	defer unlock()
	r, ok := backend.(*Runner)
	if !ok {
		return 1
	}
	var result any
	switch command {
	case "record", "watch":
		b, readErr := boundedRegular(*input)
		if readErr != nil || len(b) > 16<<10 {
			err = errors.New("agent result input unavailable")
			break
		}
		var e AgentResultEvent
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&e) != nil || decoder.Decode(new(any)) != io.EOF {
			err = errors.New("agent result input invalid")
			break
		}
		if command == "watch" {
			err = r.watchAgentResult(e)
			result = map[string]any{"watch_id": agentWatchID(e), "registered": err == nil, "delivery": "not_sent"}
		} else {
			var p AgentResultRecord
			p, err = r.recordAgentResult(e)
			result = agentResultPublic(p)
		}
	case "status":
		if len(*id) != 64 || strings.Trim(*id, "0123456789abcdef") != "" {
			err = errors.New("agent result identity invalid")
			break
		}
		var p AgentResultRecord
		err = readJSON(r.agentResultPath(*id), 1<<20, &p)
		if os.IsNotExist(err) {
			err = readJSON(filepath.Join(r.Config.Home, "agent-results", "verified", *id+".json"), 1<<20, &p)
		}
		if err == nil && (p.ID != *id || p.Domain != agentResultDomain || agentResultID(p.Event) != *id || validateAgentEvent(p.Event) != nil) {
			err = errors.New("agent result receipt invalid")
		}
		result = agentResultPublic(p)
	case "resume":
		err = r.resumeAgentResult(*id)
		result = map[string]any{"record_id": *id, "resumed": err == nil, "delivery": "not_sent"}
	case "drain":
		ctx, cancel := context.WithTimeout(context.Background(), agentResultBudget)
		h := Health{}
		r.agentResultPhase(ctx, &h)
		cancel()
		result = map[string]any{"errors": h.Errors, "delivery": "inspect_receipt"}
	}
	if err != nil {
		if errors.Is(err, errAgentReceiverBusy) {
			_, _ = io.WriteString(out, "busy_retry_same_input\n")
			return 75
		}
		_, _ = io.WriteString(out, "agent result operation deferred\n")
		return 1
	}
	b, _ := json.Marshal(result)
	_, _ = out.Write(append(b, '\n'))
	return 0
}
func agentResultPublic(p AgentResultRecord) map[string]any {
	delivery := "pending"
	if p.Verified != "" {
		delivery = "verified"
	} else if p.LastError == "owner_question_scope_invalid" {
		delivery = "invalid_input"
	} else if p.Attempts >= agentResultAttempts {
		delivery = "retry_exhausted"
	}
	errorCode := ""
	if p.LastError == "delivery_unverified" || p.LastError == "owner_question_scope_invalid" || p.LastError == "owner_action_notification_missing" {
		errorCode = p.LastError
	}
	return map[string]any{"domain": p.Domain, "record_id": p.ID, "todo_id": p.Event.Todo, "parent_received_at": p.ParentReceived, "work_completed": p.Event.State == "completed", "notify_owner": p.NotifyOwner, "delivery": delivery, "error_code": errorCode, "attempts": p.Attempts, "message_id": p.Message, "url": p.URL, "verified_at": p.Verified}
}

// Keep the command independent of environment-specific output paths; the saved
// private source reference is deliberately absent from public JSON.
// Receipt ingestion is independent of the long-running periodic lock and of
// Discord credentials/state. Different processes still serialize inbox writes.
var errAgentReceiverBusy = errors.New("agent result receiver temporarily busy")

func loadAgentResultReceiver(configPath string) (conversationCLIBackend, func(), error) {
	var cfg Config
	if readJSON(configPath, 1<<20, &cfg) != nil {
		return nil, nil, errors.New("configuration unavailable")
	}
	since, err := time.Parse(time.RFC3339Nano, cfg.Since)
	if err != nil || since.Before(time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)) || since.After(time.Now()) || !filepath.IsAbs(cfg.Home) || cfg.Project != "cc-pc-tools-rs" {
		return nil, nil, errors.New("configuration scope invalid")
	}
	return &Runner{Config: cfg, Now: time.Now}, func() {}, nil
}
func (r *Runner) agentReceiveLock(ctx context.Context) (func(), error) {
	home := filepath.Join(r.Config.Home, "agent-results")
	if err := os.MkdirAll(home, 0700); err != nil {
		return nil, err
	}
	until := time.Now().Add(2 * time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		unlock, lockErr := acquire(filepath.Join(home, "receive.lock"))
		if lockErr == nil {
			return unlock, nil
		}
		if !agentLockContention(lockErr) {
			return nil, lockErr
		}
		if !time.Now().Before(until) {
			return nil, errAgentReceiverBusy
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
func agentLockContention(err error) bool {
	// ERROR_LOCK_VIOLATION=33; access denied (5) and sharing violations (32)
	// are not byte-range lock contention. Microsoft system-error-codes--0-499-.
	if runtime.GOOS == "windows" {
		return errors.Is(err, syscall.Errno(33))
	}
	return errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN)
}
