package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"sort"
	"time"
)

type intakeBindInput struct {
	Domain  string `json:"domain"`
	Message string `json:"message_id"`
	Thread  string `json:"thread_id"`
	Author  string `json:"author_id"`
	IntakeBinding
}

func intakeCLI(args []string, out io.Writer) int {
	return intakeCLIWith(args, out, func(path string, readOnly bool) (*Runner, func(), error) {
		backend, unlock, err := loadConversationRunner(path, conversationLoadOptions{ReadOnly: readOnly, Credentials: !readOnly})
		if err != nil {
			return nil, nil, err
		}
		r, ok := backend.(*Runner)
		if !ok {
			unlock()
			return nil, nil, errors.New("intake unavailable")
		}
		return r, unlock, nil
	}, historyReadGuard)
}

func intakeCLIWith(args []string, out io.Writer, load func(string, bool) (*Runner, func(), error), guard func(context.Context, string) bool) int {
	fail := func() int { _, _ = io.WriteString(out, "intake unavailable\n"); return 1 }
	if len(args) == 0 {
		return fail()
	}
	command := args[0]
	if command != "status" && command != "bind" {
		return fail()
	}
	config, input, after := conversationDefaultConfig, "", ""
	f := flag.NewFlagSet("intake", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	conversationStringFlag(f, "config", &config)
	if command == "bind" {
		conversationStringFlag(f, "input", &input)
	} else {
		conversationStringFlag(f, "after", &after)
	}
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || command == "bind" && input == "" {
		return fail()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var plan intakeBindInput
	if command == "bind" {
		b, err := historyPrivateInput(ctx, input, guard)
		if err != nil || historyStrictJSON(b, &plan) != nil || plan.Domain != "discord-intake-binding-v1" || !monthlyID(plan.Message) || !monthlyID(plan.Thread) || !monthlyID(plan.Author) || plan.Todo == 0 || !shaPattern.MatchString(plan.SourceSHA) {
			return fail()
		}
	}
	r, unlock, err := load(config, command == "status")
	if errors.Is(err, errConversationBusy) {
		_, _ = io.WriteString(out, "busy\n")
		return 2
	}
	if err != nil {
		return fail()
	}
	defer unlock()
	// loadConversationRunner does not perform discovery. Load exact question
	// receipts under the same lock before permitting a general-purpose binding.
	if command == "bind" {
		if _, err := r.knownBotThreads(); err != nil {
			return fail()
		}
	}
	if command == "status" {
		ids := make([]string, 0, len(r.State.Requests))
		for id, q := range r.State.Requests {
			if q != nil && q.Conversation == "" && id > after {
				ids = append(ids, id)
			}
		}
		sort.Strings(ids)
		items := make([]map[string]any, 0, min(len(ids), 50))
		for _, id := range ids[:min(len(ids), 50)] {
			q := r.State.Requests[id]
			owner, next := q.PendingOwner, q.NextAction
			if q.Todo == 0 && owner == "" {
				owner, next = "OMB", "read_source_and_bind_original_task"
			}
			items = append(items, map[string]any{"message_id": q.Message, "thread_id": q.Thread, "author_id": q.Author, "created_at": q.Created, "edited_at": q.Edited, "source_sha256": ordinaryVersion(q), "todo_id": q.Todo, "phase": q.Phase, "pending_owner": owner, "next_action": next, "delivery_version": q.DeliveryVersion, "message_type_known": q.MessageType != nil, "attachments_known": q.Attachments != nil})
		}
		next := ""
		if len(ids) > 50 {
			next = ids[49]
		}
		if json.NewEncoder(out).Encode(map[string]any{"domain": "discord-intake-status-v1", "items": items, "next_after": next, "metadata_only": true, "discovery": r.State.Discovery}) != nil {
			return 1
		}
		return 0
	}
	q := r.State.Requests[plan.Message]
	if q == nil || q.Thread != plan.Thread || q.Author != plan.Author || q.Author != r.State.Destination.Owner {
		return fail()
	}
	if err = r.bindOrdinary(ctx, plan.Message, plan.IntakeBinding); err != nil {
		return fail()
	}
	if json.NewEncoder(out).Encode(map[string]any{"domain": "discord-intake-binding-result-v1", "message_id": q.Message, "source_sha256": ordinaryVersion(q), "todo_id": q.Todo, "phase": q.Phase, "delivery": "canonical_note_verified", "application": "not_inferred"}) != nil {
		return 1
	}
	return 0
}
