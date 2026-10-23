package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Fixed original authority, separate from the two-forum legacy commands.
const monthlyGuild = "1552944707464597524"
const monthlyOwner = "730822776147148835"
const monthlySince = "2026-09-22T00:00:00+09:00"
const monthlyUntil = "2026-10-22T16:08:09+09:00"

type monthlyAuditScope struct {
	Domain string `json:"domain"`
	Todo   uint64 `json:"todo_id"`
	Guild  string `json:"guild_id"`
	Owner  string `json:"owner_id"`
	Since  string `json:"since"`
	Until  string `json:"until"`
}

func (s monthlyAuditScope) valid(domain string) bool {
	return s.validAt(domain, time.Now())
}

func (s monthlyAuditScope) validAt(domain string, now time.Time) bool {
	if s.Domain != domain || s.Guild != monthlyGuild || s.Owner != monthlyOwner {
		return false
	}
	if s.Todo == 3327 {
		// Existing receipts/cursors retain the original, exact audit authority.
		return s.Since == monthlySince && s.Until == monthlyUntil && !mustHistoryTime(s.Until).After(now)
	}
	if s.Todo != 3387 {
		return false
	}
	since, sinceErr := time.Parse(time.RFC3339Nano, s.Since)
	until, untilErr := time.Parse(time.RFC3339Nano, s.Until)
	lower := time.Date(2026, 9, 23, 0, 0, 0, 0, time.FixedZone("JST", 9*60*60))
	return sinceErr == nil && untilErr == nil && !since.Before(lower) && since.Before(until) && until.Sub(since) <= 31*24*time.Hour && !until.After(now)
}
func (s monthlyAuditScope) loaderScope() historyReadScope {
	return historyReadScope{Domain: "discord-history-read-scope-v1", Todo: s.Todo, Guild: s.Guild, Owner: s.Owner, Since: s.Since, Until: s.Until, Threads: []string{"1"}}
}
func monthlyID(s string) bool { n, ok := number(s); return ok && strconv.FormatUint(n, 10) == s }

type monthlyFailure struct {
	Stage  string
	Status int
	Code   string
}

func (e monthlyFailure) Error() string { return e.Code }
func monthlyInvalid(stage string) error {
	return monthlyFailure{Stage: stage, Code: "invalid_or_unavailable"}
}
func monthlyFail(out io.Writer, err error) int {
	e := monthlyFailure{Stage: "input", Code: "invalid_or_unavailable"}
	var exact monthlyFailure
	if errors.As(err, &exact) {
		e = exact
	}
	_ = json.NewEncoder(out).Encode(map[string]any{"domain": "discord-monthly-history-error-v1", "stage": e.Stage, "status": e.Status, "code": e.Code, "receipt_published": false, "cursor_advanced": false})
	return 1
}
func monthlyGet(ctx context.Context, r *Runner, path, stage string, value any) error {
	if err := r.get(ctx, path, value); err != nil {
		status := 0
		if strings.HasPrefix(err.Error(), "discord status ") {
			status, _ = strconv.Atoi(strings.TrimPrefix(err.Error(), "discord status "))
		}
		return monthlyFailure{Stage: stage, Status: status, Code: "source_unavailable"}
	}
	return nil
}
func monthlyIdentity(ctx context.Context, r *Runner, s monthlyAuditScope) error {
	if r == nil || r.State == nil || r.State.Destination.Guild != s.Guild || r.State.Destination.Owner != s.Owner || !monthlyID(r.State.Destination.Bot) {
		return monthlyInvalid("destination")
	}
	var me struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	}
	if err := monthlyGet(ctx, r, "/users/@me", "bot_identity", &me); err != nil {
		return err
	}
	if me.ID != r.State.Destination.Bot || !me.Bot {
		return monthlyInvalid("bot_identity")
	}
	return nil
}

type monthlyInput struct {
	Raw    []byte
	Cursor []byte
	Output string
}

func monthlyInputs(ctx context.Context, args []string, guard func(context.Context, string) bool) (monthlyInput, error) {
	var s, o, c string
	f := flag.NewFlagSet("monthly-history", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	conversationStringFlag(f, "scope", &s)
	conversationStringFlag(f, "output", &o)
	conversationStringFlag(f, "cursor", &c)
	if f.Parse(args) != nil || f.NArg() != 0 || s == "" || o == "" || !strings.HasSuffix(o, ".private.json") || !historyPlainParent(o) || !guard(ctx, filepath.Dir(o)) {
		return monthlyInput{}, monthlyInvalid("input")
	}
	if _, err := os.Lstat(o); !os.IsNotExist(err) {
		return monthlyInput{}, monthlyInvalid("output")
	}
	b, err := historyPrivateInput(ctx, s, guard)
	if err != nil {
		return monthlyInput{}, monthlyInvalid("scope")
	}
	i := monthlyInput{Raw: b, Output: o}
	if c != "" {
		i.Cursor, err = historyPrivateInput(ctx, c, guard)
		if err != nil {
			return monthlyInput{}, monthlyInvalid("cursor")
		}
	}
	return i, nil
}
func monthlyPublish(out io.Writer, path string, page any, summary map[string]any) int {
	b, err := json.MarshalIndent(page, "", "  ")
	if err != nil || len(b) > 4<<20 || writeHistoryReceipt(path, b) != nil {
		return monthlyFail(out, monthlyInvalid("publication"))
	}
	summary["receipt_sha256"] = bytesHash(b)
	summary["delivery"] = "inspection_only"
	summary["application"] = "not_inferred"
	if json.NewEncoder(out).Encode(summary) != nil {
		return 1
	}
	return 0
}

// Names and topics have no decoding field and can never enter a receipt.
type monthlyChannel struct {
	ID       string `json:"id"`
	Type     *int   `json:"type"`
	Guild    string `json:"guild_id"`
	Parent   string `json:"parent_id"`
	Last     string `json:"last_message_id"`
	Metadata *struct {
		ArchiveTimestamp string `json:"archive_timestamp"`
	} `json:"thread_metadata"`
	Overwrites *[]monthlyOverwrite `json:"permission_overwrites"`
}
type monthlyChannelMetadata struct {
	ID      string `json:"id"`
	Type    int    `json:"type"`
	Parent  string `json:"parent_id"`
	Archive string `json:"archive_timestamp,omitempty"`
	Last    string `json:"last_message_id,omitempty"`
}

func (c monthlyChannel) valid(guild string, requireGuild bool) bool {
	return monthlyID(c.ID) && c.Type != nil && *c.Type >= 0 && *c.Type <= 255 && (c.Guild == guild || (!requireGuild && c.Guild == "")) && (c.Parent == "" || monthlyID(c.Parent))
}
func (c monthlyChannel) structural() monthlyChannelMetadata {
	s := monthlyChannelMetadata{ID: c.ID, Type: *c.Type, Parent: c.Parent, Last: c.Last}
	if c.Metadata != nil {
		s.Archive = c.Metadata.ArchiveTimestamp
	}
	return s
}

type monthlyInventoryScope struct {
	monthlyAuditScope
	Mode    string `json:"mode"`
	Channel string `json:"channel_id,omitempty"`
}
type monthlyInventoryPage struct {
	monthlyAuditScope
	Source            string                   `json:"source"`
	ScopeSHA          string                   `json:"scope_sha256"`
	Bot               string                   `json:"bot_id"`
	Mode              string                   `json:"mode"`
	Channel           string                   `json:"channel_id,omitempty"`
	Endpoint          string                   `json:"endpoint"`
	Before            string                   `json:"before,omitempty"`
	Channels          []monthlyChannelMetadata `json:"channels"`
	Scanned           int                      `json:"scanned_channels"`
	Complete          bool                     `json:"complete"`
	Next              string                   `json:"next,omitempty"`
	TimestampBoundary bool                     `json:"timestamp_boundary_unresolved"`
	Acquired          string                   `json:"acquired_at"`
	Coverage          string                   `json:"coverage"`
}

func monthlyArchiveMode(mode string) bool {
	return mode == "public-archived" || mode == "private-archived" || mode == "joined-private-archived"
}
func monthlyInventoryEndpoint(s monthlyInventoryScope) string {
	switch s.Mode {
	case "channels":
		return "/guilds/" + s.Guild + "/channels"
	case "active":
		return "/guilds/" + s.Guild + "/threads/active"
	case "public-archived":
		return "/channels/" + s.Channel + "/threads/archived/public"
	case "private-archived":
		return "/channels/" + s.Channel + "/threads/archived/private"
	case "joined-private-archived":
		return "/channels/" + s.Channel + "/users/@me/threads/archived/private"
	}
	return ""
}
func monthlyInventoryCursor(prior monthlyInventoryPage, s monthlyInventoryScope, hash string) bool {
	want := s.monthlyAuditScope
	want.Domain = "discord-monthly-history-inventory-page-v1"
	if prior.monthlyAuditScope != want || prior.Source != "discord-api-v10" || prior.ScopeSHA != hash || prior.Mode != s.Mode || prior.Channel != s.Channel || prior.Endpoint != monthlyInventoryEndpoint(s) || prior.Complete || prior.Scanned < 1 || prior.Scanned > 100 || prior.Scanned != len(prior.Channels) || prior.Next == "" || !monthlyID(prior.Bot) || !monthlyArchiveMode(s.Mode) {
		return false
	}
	last := prior.Channels[len(prior.Channels)-1]
	if s.Mode == "joined-private-archived" {
		n, ok := number(prior.Next)
		old, valid := number(prior.Before)
		return ok && last.ID == prior.Next && (prior.Before == "" || (valid && n < old)) && !prior.TimestampBoundary
	}
	n, e := time.Parse(time.RFC3339Nano, prior.Next)
	old, e2 := time.Parse(time.RFC3339Nano, prior.Before)
	return e == nil && last.Archive == prior.Next && !n.After(time.Now()) && prior.TimestampBoundary && (prior.Before == "" || (e2 == nil && n.Before(old)))
}
func monthlyInventoryCLIWith(args []string, out io.Writer, load func(context.Context, historyReadScope) (*Runner, error), guard func(context.Context, string) bool) int {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	i, err := monthlyInputs(ctx, args, guard)
	if err != nil {
		return monthlyFail(out, err)
	}
	var s monthlyInventoryScope
	if historyStrictJSON(i.Raw, &s) != nil || !s.monthlyAuditScope.valid("discord-monthly-history-inventory-scope-v1") || monthlyInventoryEndpoint(s) == "" || (monthlyArchiveMode(s.Mode) && !monthlyID(s.Channel)) || (!monthlyArchiveMode(s.Mode) && s.Channel != "") {
		return monthlyFail(out, monthlyInvalid("scope"))
	}
	hash, before, ties := bytesHash(i.Raw), "", false
	var prior monthlyInventoryPage
	if i.Cursor != nil {
		if historyStrictJSON(i.Cursor, &prior) != nil || !monthlyInventoryCursor(prior, s, hash) {
			return monthlyFail(out, monthlyInvalid("cursor"))
		}
		before, ties = prior.Next, prior.TimestampBoundary
	}
	r, err := load(ctx, s.loaderScope())
	if err != nil {
		return monthlyFail(out, monthlyInvalid("configuration"))
	}
	if err = monthlyIdentity(ctx, r, s.monthlyAuditScope); err != nil {
		return monthlyFail(out, err)
	}
	if i.Cursor != nil && prior.Bot != r.State.Destination.Bot {
		return monthlyFail(out, monthlyInvalid("cursor_identity"))
	}
	p, err := readMonthlyInventory(ctx, r, s, hash, before, ties)
	if err != nil {
		return monthlyFail(out, err)
	}
	if ctx.Err() != nil {
		return monthlyFail(out, monthlyInvalid("deadline"))
	}
	return monthlyPublish(out, i.Output, p, map[string]any{"domain": p.Domain, "mode": p.Mode, "scanned_channels": p.Scanned, "complete": p.Complete, "timestamp_boundary_unresolved": p.TimestampBoundary, "coverage": p.Coverage})
}
func readMonthlyInventory(ctx context.Context, r *Runner, s monthlyInventoryScope, hash, before string, ties bool) (monthlyInventoryPage, error) {
	p := monthlyInventoryPage{monthlyAuditScope: s.monthlyAuditScope, Source: "discord-api-v10", ScopeSHA: hash, Bot: r.State.Destination.Bot, Mode: s.Mode, Channel: s.Channel, Endpoint: monthlyInventoryEndpoint(s), Before: before, Channels: []monthlyChannelMetadata{}, TimestampBoundary: ties, Coverage: "returned_metadata_only"}
	p.Domain = "discord-monthly-history-inventory-page-v1"
	if monthlyArchiveMode(s.Mode) {
		var parent monthlyChannel
		if err := monthlyGet(ctx, r, "/channels/"+s.Channel, "archive_parent", &parent); err != nil {
			return p, err
		}
		if !parent.valid(s.Guild, true) || parent.ID != s.Channel || (*parent.Type != 0 && *parent.Type != 5 && *parent.Type != 15 && *parent.Type != 16) || (s.Mode != "public-archived" && *parent.Type != 0) {
			return p, monthlyInvalid("archive_parent")
		}
	}
	path := p.Endpoint
	if monthlyArchiveMode(s.Mode) {
		path += "?limit=100"
		if before != "" {
			path += "&before=" + url.QueryEscape(before)
		}
	}
	var channels []monthlyChannel
	complete := true
	if s.Mode == "channels" {
		if err := monthlyGet(ctx, r, path, "channels", &channels); err != nil {
			return p, err
		}
	} else {
		var listing struct {
			Threads []monthlyChannel `json:"threads"`
			HasMore *bool            `json:"has_more"`
		}
		if err := monthlyGet(ctx, r, path, "thread_listing", &listing); err != nil {
			return p, err
		}
		channels = listing.Threads
		if monthlyArchiveMode(s.Mode) {
			if listing.HasMore == nil || len(channels) > 100 {
				return p, monthlyInvalid("archive_page")
			}
			complete = !*listing.HasMore
		}
	}
	if channels == nil || len(channels) > 10000 || (!complete && len(channels) == 0) {
		return p, monthlyInvalid("listing")
	}
	seen := map[string]bool{}
	previousID := uint64(^uint64(0))
	var previousTime time.Time
	if before != "" {
		if s.Mode == "joined-private-archived" {
			previousID, _ = number(before)
		} else {
			previousTime = mustHistoryTime(before)
		}
	}
	for _, c := range channels {
		if !c.valid(s.Guild, s.Mode == "channels") || seen[c.ID] {
			return p, monthlyInvalid("channel_structure")
		}
		seen[c.ID] = true
		if s.Mode == "channels" {
			if *c.Type == 1 || *c.Type == 3 || *c.Type == 10 || *c.Type == 11 || *c.Type == 12 {
				return p, monthlyInvalid("guild_channel_type")
			}
		}
		if s.Mode == "active" || monthlyArchiveMode(s.Mode) {
			if (*c.Type != 10 && *c.Type != 11 && *c.Type != 12) || !monthlyID(c.Parent) {
				return p, monthlyInvalid("thread_structure")
			}
		}
		if monthlyArchiveMode(s.Mode) {
			if c.Parent != s.Channel || (s.Mode == "public-archived" && *c.Type != 10 && *c.Type != 11) || (s.Mode != "public-archived" && *c.Type != 12) {
				return p, monthlyInvalid("archive_structure")
			}
			if s.Mode == "joined-private-archived" {
				n, _ := number(c.ID)
				if n >= previousID {
					return p, monthlyInvalid("archive_order")
				}
				previousID = n
			} else {
				if c.Metadata == nil {
					return p, monthlyInvalid("archive_timestamp")
				}
				stamp, e := time.Parse(time.RFC3339Nano, c.Metadata.ArchiveTimestamp)
				if e != nil || stamp.After(r.Now()) || (!previousTime.IsZero() && stamp.After(previousTime)) || (before != "" && !stamp.Before(mustHistoryTime(before))) {
					return p, monthlyInvalid("archive_order")
				}
				previousTime = stamp
			}
		}
		p.Channels = append(p.Channels, c.structural())
	}
	p.Scanned = len(channels)
	p.Complete = complete
	p.Acquired = r.Now().UTC().Format(time.RFC3339Nano)
	if !complete {
		last := p.Channels[len(p.Channels)-1]
		p.Next = last.Archive
		if s.Mode == "joined-private-archived" {
			p.Next = last.ID
		} else {
			p.TimestampBoundary = true
		}
	}
	return p, nil
}
func monthlyInventoryCLI(args []string, out io.Writer) int {
	return monthlyInventoryCLIWith(args, out, loadHistoryRead, historyReadGuard)
}
func monthlyReadCLI(args []string, out io.Writer) int {
	return monthlyReadCLIWith(args, out, loadHistoryRead, historyReadGuard)
}
