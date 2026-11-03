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
	diagnostic, stage := false, "arguments"
	fail := func() int {
		if diagnostic {
			_ = json.NewEncoder(out).Encode(map[string]any{"domain": "discord-intake-diagnostic-v1", "stage": stage, "application": "not_inferred", "state_change": "not_inferred"})
		} else {
			_, _ = io.WriteString(out, "intake unavailable\n")
		}
		return 1
	}
	if len(args) == 0 {
		return fail()
	}
	command := args[0]
	if command != "status" && command != "bind" && command != "watch" {
		return fail()
	}
	config, input, after := conversationDefaultConfig, "", ""
	f := flag.NewFlagSet("intake", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.BoolVar(&diagnostic, "diagnostic", false, "return a fixed failure stage without raw input or errors")
	conversationStringFlag(f, "config", &config)
	if command != "status" {
		conversationStringFlag(f, "input", &input)
	} else {
		conversationStringFlag(f, "after", &after)
	}
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || command != "status" && input == "" {
		return fail()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var plan intakeBindInput
	var watch HistoryWatchInput
	if command == "bind" {
		stage = "binding_input"
		b, err := historyPrivateInput(ctx, input, guard)
		if err != nil || historyStrictJSON(b, &plan) != nil || plan.Domain != "discord-intake-binding-v1" || !monthlyID(plan.Message) || !monthlyID(plan.Thread) || !monthlyID(plan.Author) || plan.Todo == 0 || !shaPattern.MatchString(plan.SourceSHA) {
			return fail()
		}
	}
	if command == "watch" {
		stage = "history_watch_input"
		b, err := historyPrivateInput(ctx, input, guard)
		if err != nil || historyStrictJSON(b, &watch) != nil || watch.Domain != "discord-intake-history-watch-v1" || !monthlyID(watch.Message) || !shaPattern.MatchString(watch.ReceiptSHA) {
			return fail()
		}
	}
	stage = "saved_state_loader"
	r, unlock, err := load(config, command == "status")
	if errors.Is(err, errConversationBusy) {
		if diagnostic {
			stage = "busy"
			fail()
		} else {
			_, _ = io.WriteString(out, "busy\n")
		}
		return 2
	}
	if err != nil {
		return fail()
	}
	defer unlock()
	if command == "watch" {
		stage = "history_watch"
		if err := r.watchHistory(ctx, watch, guard); err != nil {
			return fail()
		}
		q := r.State.Requests[watch.Message]
		if json.NewEncoder(out).Encode(map[string]any{"domain": "discord-intake-history-watch-result-v1", "message_id": q.Message, "source_sha256": ordinaryVersion(q), "phase": q.Phase, "application": "not_inferred"}) != nil {
			return 1
		}
		return 0
	}
	// loadConversationRunner does not perform discovery. Load exact question
	// receipts under the same lock before permitting a general-purpose binding.
	if command == "bind" {
		stage = "receipt_registry"
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
			if q.Phase == "origin_pending" {
				if owner == "" {
					owner = "OMB"
				}
				if next == "" {
					next = "review_original_context_and_existing_task_linkage"
				}
			}
			if q.Todo == 0 && owner == "" && q.Phase == "task_binding_pending" {
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
		stage = "source_identity"
		return fail()
	}
	if err = r.bindOrdinary(ctx, plan.Message, plan.IntakeBinding); err != nil {
		stage = intakeBindingFailureStage(r, plan, err)
		return fail()
	}
	if json.NewEncoder(out).Encode(map[string]any{"domain": "discord-intake-binding-result-v1", "message_id": q.Message, "source_sha256": ordinaryVersion(q), "todo_id": q.Todo, "phase": q.Phase, "delivery": "canonical_note_verified", "application": "not_inferred"}) != nil {
		return 1
	}
	return 0
}

// Inspect only the saved routing predicates and exact fixed error vocabulary.
// Do not print errors, body, evidence, paths, supplied IDs, or configuration.
// A failed delivery may already have saved a binding; diagnostics deliberately
// do not infer whether any durable state changed and never bypass a gate.
func intakeBindingFailureStage(r *Runner, plan intakeBindInput, err error) string {
	q := r.State.Requests[plan.Message]
	if q == nil {
		return "saved_request"
	}
	switch err.Error() {
	case "ordinary binding input invalid":
		if r.ordinaryProductScope(q) {
			return "product_context"
		}
		if r.isConfiguredRequestReport(plan.Todo) {
			return "configured_request_context"
		}
		return "binding_contract"
	case "ordinary binding requires exact question delivery instead":
		if r.KnownQuestionThreads[q.Thread] || ordinaryShortAnswer(q.Text) {
			return "original_question_context"
		}
		return "dedicated_context"
	case "ordinary source belongs to an exact conversation":
		return "conversation_context"
	case "ordinary binding source or task changed":
		return "binding_version"
	case "ordinary binding task unavailable":
		return "target_task_scope"
	case "owner receipt association differs", "owner thread invalid", "owner message invalid", "owner thread scope differs", "owner message changed; receipt retained", "owner message guild changed; receipt retained", "owner message reference changed; receipt retained":
		return "source_verification"
	default:
		return "binding_operation"
	}
}
