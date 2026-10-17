package main

import (
	"context"
	"time"
)

// Reporting an already registered task reads its exact owner source. It does
// not interpret an old answer, migrate intake receipts, or authorize execution.
func (r *Runner) verifyReportRequest(ctx context.Context, request *Request) bool {
	return r.verifyRequestReceiptReadOnly(ctx, request, false)
}

// Restoring a receiver link retains the original bot-context condition. A
// metadata report alone must not make an unknown old question actionable.
func (r *Runner) verifyLinkedRequest(ctx context.Context, request *Request) bool {
	return r.verifyRequestReceiptReadOnly(ctx, request, true)
}

func (r *Runner) verifyRequestReceiptReadOnly(ctx context.Context, request *Request, requireBotContext bool) bool {
	cutoff, err := time.Parse(time.RFC3339Nano, r.State.Since)
	stamp, stampErr := time.Parse(time.RFC3339Nano, request.Created)
	if err != nil || stampErr != nil || stamp.Before(cutoff) || stamp.After(r.Now().Add(time.Minute)) || request.Source != "discord-api-v10" || request.Author != r.State.Destination.Owner || request.Key != requestKey(request.Thread, request.Message) || request.RecordVersion > 2 {
		return false
	}
	if _, ok := number(request.Thread); !ok {
		return false
	}
	if _, ok := number(request.Message); !ok {
		return false
	}
	var thread Thread
	if r.get(ctx, "/channels/"+request.Thread, &thread) != nil || thread.ID != request.Thread || thread.Type != 11 || thread.Guild != r.State.Destination.Guild {
		return false
	}
	knownBot := r.ProgressLinks[thread.ID].Todo != 0 || r.KnownQuestionThreads[thread.ID]
	botRequest := thread.Parent == requestForum && thread.Owner == r.State.Destination.Bot && (!requireBotContext || knownBot)
	if !((thread.Parent == requestForum && thread.Owner == r.State.Destination.Owner) || botRequest || (thread.Parent == r.State.Destination.Forum && thread.Owner == r.State.Destination.Bot)) {
		return false
	}
	if requireBotContext && thread.Owner == r.State.Destination.Bot && request.Origin == nil && !knownBot && !(request.Phase == "status_query" && isPureStatusQuestion(request.Text, r.State.Destination.Bot)) {
		return false
	}
	var live Message
	if r.get(ctx, "/channels/"+request.Thread+"/messages/"+request.Message, &live) != nil || live.ID != request.Message || live.Channel != request.Thread || live.Author.ID != request.Author || live.Author.Bot || live.Timestamp != request.Created || safeText(live.Content, r.Token) != request.Text || live.Edited != request.Edited {
		return false
	}
	if request.Guild != "" && request.Guild != r.State.Destination.Guild {
		return false
	}
	if (request.ReplyTo != "" && request.ReplyTo != live.Reference.Message) || (request.ReplyChannel != "" && request.ReplyChannel != live.Reference.Channel) || (request.ReplyGuild != "" && request.ReplyGuild != live.Reference.Guild) {
		return false
	}
	if request.RecordVersion == 2 && (request.Guild != r.State.Destination.Guild || live.Reference.Message != request.ReplyTo || live.Reference.Channel != request.ReplyChannel || live.Reference.Guild != request.ReplyGuild) {
		return false
	}
	return true
}
