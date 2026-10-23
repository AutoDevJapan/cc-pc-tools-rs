package main

import (
	"context"
	"errors"
	"time"
)

type HistoryWatchInput struct {
	Domain     string `json:"domain"`
	Message    string `json:"message_id"`
	Receipt    string `json:"receipt_file"`
	ReceiptSHA string `json:"receipt_sha256"`
}

type HistoryWatch struct {
	Scope      monthlyAuditScope `json:"scope"`
	Receipt    string            `json:"receipt_file"`
	ReceiptSHA string            `json:"receipt_sha256"`
	ScopeSHA   string            `json:"scope_sha256"`
}

func (r *Runner) watchHistory(ctx context.Context, in HistoryWatchInput, guard func(context.Context, string) bool) error {
	if in.Domain != "discord-intake-history-watch-v1" || !monthlyID(in.Message) || !shaPattern.MatchString(in.ReceiptSHA) {
		return errors.New("historical watch input invalid")
	}
	b, err := historyPrivateInput(ctx, in.Receipt, guard)
	if err != nil || bytesHash(b) != in.ReceiptSHA {
		return errors.New("historical receipt unavailable or changed")
	}
	if old := r.State.Requests[in.Message]; old != nil {
		if old.HistoryWatch != nil && old.HistoryWatch.Receipt == in.Receipt && old.HistoryWatch.ReceiptSHA == in.ReceiptSHA {
			return nil
		}
		return errors.New("existing intake route retained")
	}
	var page monthlyReadPage
	if historyStrictJSON(b, &page) != nil || page.Domain != "discord-monthly-history-read-page-v1" || page.Source != "discord-api-v10" || page.Bot != r.State.Destination.Bot || page.Guild != r.State.Destination.Guild || page.Owner != r.State.Destination.Owner || !monthlyID(page.Channel) || !shaPattern.MatchString(page.ScopeSHA) || page.Permission != "effective_parent_permissions_verified" || page.Scanned < 0 || page.Scanned > 100 || len(page.Messages) > page.Scanned {
		return errors.New("historical receipt scope differs")
	}
	scope := page.monthlyAuditScope
	scope.Domain = "discord-monthly-history-read-scope-v1"
	if !scope.validAt(scope.Domain, r.Now()) {
		return errors.New("historical audit authority differs")
	}
	acquired, e := time.Parse(time.RFC3339Nano, page.Acquired)
	if e != nil || acquired.After(r.Now()) {
		return errors.New("historical receipt clock differs")
	}
	var source *historyReadMessage
	for i := range page.Messages {
		m := &page.Messages[i]
		if m.ID == in.Message {
			if source != nil {
				return errors.New("duplicate historical source")
			}
			source = m
		}
	}
	if source == nil || source.Channel != page.Channel || source.Author.ID != page.Owner || source.Author.Bot || safeText(source.Content, r.Token) != source.Content {
		return errors.New("historical owner source differs")
	}
	q := &Request{Thread: source.Channel, Guild: page.Guild, RecordVersion: 2, Message: source.ID, Author: source.Author.ID, Created: source.Timestamp, Edited: source.Edited, ReplyTo: source.Reference.Message, ReplyChannel: source.Reference.Channel, ReplyGuild: source.Reference.Guild, Acquired: r.Now().UTC().Format(time.RFC3339Nano), Source: "discord-api-v10", Text: source.Content, Key: requestKey(source.Channel, source.ID), MessageType: source.Type, Attachments: source.Attachments, Phase: "history_watch", PendingOwner: "OMB", NextAction: "watch_source_version_without_reinterpreting_original_answer", HistoryWatch: &HistoryWatch{Scope: scope, Receipt: in.Receipt, ReceiptSHA: in.ReceiptSHA, ScopeSHA: page.ScopeSHA}}
	if !r.validHistoryWatch(q) {
		return errors.New("historical source outside accepted month")
	}
	created, _ := time.Parse(time.RFC3339Nano, q.Created)
	if acquired.Before(created) {
		return errors.New("historical acquisition predates source")
	}
	if q.Edited != "" {
		edited, e := time.Parse(time.RFC3339Nano, q.Edited)
		if e != nil || edited.Before(created) || edited.After(r.Now()) {
			return errors.New("historical edit clock differs")
		}
	}
	var bot struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	}
	if err := r.get(ctx, "/users/@me", &bot); err != nil || !bot.Bot || bot.ID != r.State.Destination.Bot {
		return errors.New("historical current bot differs")
	}
	// The audit receipt alone never authorizes a current body read. Reuse the
	// live guild permissions/private-membership check before accessing a message.
	if _, err := monthlyReadable(ctx, r, q.Thread); err != nil {
		return err
	}
	var live historyReadMessage
	if err := r.get(ctx, "/channels/"+q.Thread+"/messages/"+q.Message, &live); err != nil {
		return err
	}
	if live.ID != q.Message || live.Channel != q.Thread || live.Author.ID != q.Author || live.Author.Bot || live.Timestamp != q.Created || live.Edited != q.Edited || live.Content != q.Text || live.Reference.Message != q.ReplyTo || live.Reference.Channel != q.ReplyChannel || live.Reference.Guild != q.ReplyGuild {
		return errors.New("historical live source has changed; reconcile its current version")
	}
	// Missing audit metadata remains distinguishable in the frozen receipt;
	// the watch baseline records what was actually observed in this live read.
	if q.MessageType != nil && canonicalHash(q.MessageType) != canonicalHash(live.Type) || q.Attachments != nil && canonicalHash(q.Attachments) != canonicalHash(live.Attachments) {
		return errors.New("historical attachment or type differs")
	}
	q.MessageType, q.Attachments = live.Type, live.Attachments
	r.State.Requests[q.Message] = q
	if err := r.save(); err != nil {
		delete(r.State.Requests, q.Message)
		return err
	}
	return nil
}

func (r *Runner) validHistoryWatch(q *Request) bool {
	w := q.HistoryWatch
	if w == nil || w.Scope.Guild != r.State.Destination.Guild || w.Scope.Owner != r.State.Destination.Owner || !shaPattern.MatchString(w.ReceiptSHA) || !shaPattern.MatchString(w.ScopeSHA) || w.Receipt == "" || !w.Scope.validAt("discord-monthly-history-read-scope-v1", r.Now()) {
		return false
	}
	stamp, err := time.Parse(time.RFC3339Nano, q.Created)
	lower := mustHistoryTime("2026-09-23T00:00:00+09:00")
	return err == nil && !stamp.Before(lower) && !stamp.Before(mustHistoryTime(w.Scope.Since)) && stamp.Before(mustHistoryTime(w.Scope.Until))
}
