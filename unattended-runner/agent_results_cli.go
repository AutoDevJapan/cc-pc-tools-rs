package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"strings"
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
	backend, unlock, err := loadConversationRunner(*config, conversationLoadOptions{Credentials: command == "drain"})
	if err != nil {
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
	} else if p.Attempts >= agentResultAttempts {
		delivery = "retry_exhausted"
	}
	return map[string]any{"domain": p.Domain, "record_id": p.ID, "todo_id": p.Event.Todo, "parent_received_at": p.ParentReceived, "work_completed": p.Event.State == "completed", "delivery": delivery, "attempts": p.Attempts, "message_id": p.Message, "url": p.URL, "verified_at": p.Verified}
}

// Keep the command independent of environment-specific output paths; the saved
// private source reference is deliberately absent from public JSON.
var _ = os.ErrNotExist
