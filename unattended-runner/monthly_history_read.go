package main

import (
	"context"
	"io"
	"strconv"
	"time"
)

const monthlyView = uint64(1 << 10)
const monthlyHistory = uint64(1 << 16)
const monthlyAdmin = uint64(1 << 3)
const monthlyManageThreads = uint64(1 << 34)
const monthlyConnect = uint64(1 << 20)

type monthlyOverwrite struct {
	ID    string `json:"id"`
	Type  *int   `json:"type"`
	Allow string `json:"allow"`
	Deny  string `json:"deny"`
}

// A 200 [] message list does not prove READ_MESSAGE_HISTORY. Check effective
// parent permissions using the configured bot; never retain role names.
func monthlyPermissions(ctx context.Context, r *Runner, c monthlyChannel) (uint64, error) {
	if c.Overwrites == nil {
		return 0, monthlyInvalid("permission_overwrites")
	}
	var roles []struct {
		ID          string `json:"id"`
		Permissions string `json:"permissions"`
	}
	guild := r.State.Destination.Guild
	if err := monthlyGet(ctx, r, "/guilds/"+guild+"/roles", "roles", &roles); err != nil {
		return 0, err
	}
	var member struct {
		User struct {
			ID  string `json:"id"`
			Bot bool   `json:"bot"`
		} `json:"user"`
		Roles []string `json:"roles"`
	}
	if err := monthlyGet(ctx, r, "/guilds/"+guild+"/members/"+r.State.Destination.Bot, "bot_member", &member); err != nil {
		return 0, err
	}
	if roles == nil || member.Roles == nil || member.User.ID != r.State.Destination.Bot || !member.User.Bot {
		return 0, monthlyInvalid("permissions")
	}
	roleBits := map[string]uint64{}
	for _, role := range roles {
		bits, e := strconv.ParseUint(role.Permissions, 10, 64)
		if e != nil || !monthlyID(role.ID) {
			return 0, monthlyInvalid("role_permissions")
		}
		if _, ok := roleBits[role.ID]; ok {
			return 0, monthlyInvalid("duplicate_role")
		}
		roleBits[role.ID] = bits
	}
	permissions, ok := roleBits[guild]
	if !ok {
		return 0, monthlyInvalid("everyone_role")
	}
	selected := map[string]bool{guild: true}
	for _, id := range member.Roles {
		bits, ok := roleBits[id]
		if !ok || selected[id] {
			return 0, monthlyInvalid("member_roles")
		}
		permissions |= bits
		selected[id] = true
	}
	if permissions&monthlyAdmin != 0 {
		return ^uint64(0), nil
	}
	var everyoneAllow, everyoneDeny, rolesAllow, rolesDeny, memberAllow, memberDeny uint64
	seen := map[string]bool{}
	for _, o := range *c.Overwrites {
		allow, e1 := strconv.ParseUint(o.Allow, 10, 64)
		deny, e2 := strconv.ParseUint(o.Deny, 10, 64)
		if !monthlyID(o.ID) || o.Type == nil || (*o.Type != 0 && *o.Type != 1) || e1 != nil || e2 != nil {
			return 0, monthlyInvalid("permission_overwrites")
		}
		key := strconv.Itoa(*o.Type) + ":" + o.ID
		if seen[key] {
			return 0, monthlyInvalid("duplicate_overwrite")
		}
		seen[key] = true
		if *o.Type == 0 && o.ID == guild {
			everyoneAllow, everyoneDeny = allow, deny
		} else if *o.Type == 0 && selected[o.ID] {
			rolesAllow |= allow
			rolesDeny |= deny
		} else if *o.Type == 1 && o.ID == r.State.Destination.Bot {
			memberAllow, memberDeny = allow, deny
		}
	}
	permissions = (permissions &^ everyoneDeny) | everyoneAllow
	permissions = (permissions &^ rolesDeny) | rolesAllow
	permissions = (permissions &^ memberDeny) | memberAllow
	if permissions&(monthlyView|monthlyHistory) != (monthlyView | monthlyHistory) {
		return 0, monthlyInvalid("read_message_history_permission")
	}
	return permissions, nil
}

func monthlyReadable(ctx context.Context, r *Runner, id string) (monthlyChannel, error) {
	var c monthlyChannel
	if err := monthlyGet(ctx, r, "/channels/"+id, "selected_channel", &c); err != nil {
		return c, err
	}
	if !c.valid(r.State.Destination.Guild, true) || c.ID != id {
		return c, monthlyInvalid("selected_channel")
	}
	parent := c
	thread := *c.Type == 10 || *c.Type == 11 || *c.Type == 12
	if !thread && *c.Type != 0 && *c.Type != 5 && *c.Type != 2 && *c.Type != 13 {
		return c, monthlyInvalid("selected_channel_type")
	}
	if thread {
		if !monthlyID(c.Parent) {
			return c, monthlyInvalid("thread_parent")
		}
		// Decode into a fresh value: c.Type is a pointer and a shallow copy
		// would let the parent response overwrite the selected thread type.
		parent = monthlyChannel{}
		if err := monthlyGet(ctx, r, "/channels/"+c.Parent, "thread_parent", &parent); err != nil {
			return c, err
		}
		if !parent.valid(r.State.Destination.Guild, true) || parent.ID != c.Parent || !monthlyThreadParentType(*c.Type, *parent.Type) {
			return c, monthlyInvalid("thread_parent")
		}
		c.ParentType = parent.Type
	}
	permissions, err := monthlyPermissions(ctx, r, parent)
	if err != nil {
		return c, err
	}
	// Reading voice-channel text also needs CONNECT; no voice connection or
	// membership mutation is made by this reader.
	if (*c.Type == 2 || *c.Type == 13) && permissions&monthlyConnect == 0 {
		return c, monthlyInvalid("connect_permission")
	}
	if *c.Type == 12 && permissions&monthlyManageThreads == 0 {
		var membership struct {
			ID   string `json:"id"`
			User string `json:"user_id"`
		}
		if err := monthlyGet(ctx, r, "/channels/"+c.ID+"/thread-members/"+r.State.Destination.Bot, "private_membership", &membership); err != nil {
			return c, err
		}
		if membership.ID != c.ID || membership.User != r.State.Destination.Bot {
			return c, monthlyInvalid("private_membership")
		}
	}
	return c, nil
}

func monthlyThreadParentType(thread, parent int) bool {
	switch thread {
	case 10:
		return parent == 5
	case 11:
		return parent == 0 || parent == 15 || parent == 16
	case 12:
		return parent == 0
	}
	return false
}

type monthlyReadScope struct {
	monthlyAuditScope
	Channels []string `json:"channel_ids"`
}
type monthlyReadCursor struct {
	ScopeSHA string `json:"scope_sha256"`
	Index    int    `json:"channel_index"`
	Before   string `json:"before"`
}
type monthlyReadPage struct {
	monthlyAuditScope
	Source     string               `json:"source"`
	ScopeSHA   string               `json:"scope_sha256"`
	Bot        string               `json:"bot_id"`
	Channel    string               `json:"channel_id"`
	Type       int                  `json:"channel_type"`
	Cursor     monthlyReadCursor    `json:"cursor"`
	Scanned    int                  `json:"scanned_messages"`
	LastID     string               `json:"last_scanned_id,omitempty"`
	LastTime   string               `json:"last_scanned_timestamp,omitempty"`
	Messages   []historyReadMessage `json:"messages"`
	Complete   bool                 `json:"complete"`
	Next       *monthlyReadCursor   `json:"next"`
	Acquired   string               `json:"acquired_at"`
	Permission string               `json:"permission_evidence"`
}

func monthlyReadCursorValid(p monthlyReadPage, s monthlyReadScope, hash string) bool {
	want := s.monthlyAuditScope
	want.Domain = "discord-monthly-history-read-page-v1"
	if p.monthlyAuditScope != want || p.Source != "discord-api-v10" || p.ScopeSHA != hash || p.Cursor.ScopeSHA != hash || p.Complete || p.Next == nil || p.Cursor.Index < 0 || p.Cursor.Index >= len(s.Channels) || p.Channel != s.Channels[p.Cursor.Index] || p.Scanned < 0 || p.Scanned > 100 || len(p.Messages) > p.Scanned || !monthlyID(p.Bot) || p.Permission != "effective_parent_permissions_verified" {
		return false
	}
	before, ok := number(p.Cursor.Before)
	high, _ := number(historyHighBefore(mustHistoryTime(s.Until)))
	channel, _ := number(p.Channel)
	if !ok || before > high || before < channel {
		return false
	}
	next := p.Cursor
	if p.Scanned == 0 {
		if p.LastID != "" || p.LastTime != "" {
			return false
		}
		next.Index++
		next.Before = historyHighBefore(mustHistoryTime(s.Until))
	} else {
		last, ok := number(p.LastID)
		stamp, err := time.Parse(time.RFC3339Nano, p.LastTime)
		if !ok || last >= before || last < channel || err != nil || stamp.After(mustHistoryTime(s.Until).Add(time.Millisecond)) {
			return false
		}
		if stamp.Before(mustHistoryTime(s.Since)) {
			next.Index++
			next.Before = historyHighBefore(mustHistoryTime(s.Until))
		} else {
			next.Before = p.LastID
		}
	}
	return next.Index < len(s.Channels) && *p.Next == next
}

func monthlyReadCLIWith(args []string, out io.Writer, load func(context.Context, historyReadScope) (*Runner, error), guard func(context.Context, string) bool) int {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	i, err := monthlyInputs(ctx, args, guard)
	if err != nil {
		return monthlyFail(out, err)
	}
	var s monthlyReadScope
	if historyStrictJSON(i.Raw, &s) != nil || !s.monthlyAuditScope.valid("discord-monthly-history-read-scope-v1") || len(s.Channels) < 1 || len(s.Channels) > 10 {
		return monthlyFail(out, monthlyInvalid("scope"))
	}
	high, _ := number(historyHighBefore(mustHistoryTime(s.Until)))
	seen := map[string]bool{}
	for _, id := range s.Channels {
		n, _ := number(id)
		if !monthlyID(id) || n >= high || seen[id] {
			return monthlyFail(out, monthlyInvalid("channel_ids"))
		}
		seen[id] = true
	}
	hash := bytesHash(i.Raw)
	cursor := monthlyReadCursor{ScopeSHA: hash, Before: historyHighBefore(mustHistoryTime(s.Until))}
	var prior monthlyReadPage
	if i.Cursor != nil {
		if historyStrictJSON(i.Cursor, &prior) != nil || !monthlyReadCursorValid(prior, s, hash) {
			return monthlyFail(out, monthlyInvalid("cursor"))
		}
		cursor = *prior.Next
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
	p, err := readMonthlyPage(ctx, r, s, cursor)
	if err != nil {
		return monthlyFail(out, err)
	}
	if ctx.Err() != nil {
		return monthlyFail(out, monthlyInvalid("deadline"))
	}
	count, other, unknown := 0, 0, 0
	for _, m := range p.Messages {
		if m.Type == nil {
			unknown++
		} else if *m.Type == 0 || *m.Type == 19 {
			count++
		} else {
			other++
		}
	}
	return monthlyPublish(out, i.Output, p, map[string]any{"domain": p.Domain, "channel_id": p.Channel, "scanned_messages": p.Scanned, "owner_messages": count, "owner_other_types": other, "owner_unknown_types": unknown, "complete": p.Complete, "next": p.Next})
}

func readMonthlyPage(ctx context.Context, r *Runner, s monthlyReadScope, cursor monthlyReadCursor) (monthlyReadPage, error) {
	p := monthlyReadPage{monthlyAuditScope: s.monthlyAuditScope, Source: "discord-api-v10", ScopeSHA: cursor.ScopeSHA, Bot: r.State.Destination.Bot, Channel: s.Channels[cursor.Index], Cursor: cursor, Messages: []historyReadMessage{}}
	p.Domain = "discord-monthly-history-read-page-v1"
	c, err := monthlyReadable(ctx, r, p.Channel)
	if err != nil {
		return p, err
	}
	p.Type = *c.Type
	p.Permission = "effective_parent_permissions_verified"
	var messages []historyReadMessage
	if err := monthlyGet(ctx, r, "/channels/"+p.Channel+"/messages?limit=100&before="+cursor.Before, "channel_messages", &messages); err != nil {
		return p, err
	}
	if messages == nil || len(messages) > 100 {
		return p, monthlyInvalid("messages")
	}
	previous, _ := number(cursor.Before)
	channel, _ := number(p.Channel)
	since, until := mustHistoryTime(s.Since), mustHistoryTime(s.Until)
	previousTime := until.Add(time.Millisecond)
	below := false
	for _, m := range messages {
		id, ok := number(m.ID)
		if !ok || id >= previous || id < channel || !historyMessageMetadataValid(m.Message, p.Channel, r) {
			return p, monthlyInvalid("message_structure")
		}
		previous = id
		stamp := mustHistoryTime(m.Timestamp)
		if stamp.After(previousTime) {
			return p, monthlyInvalid("message_order")
		}
		previousTime = stamp
		if stamp.Before(since) {
			below = true
			continue
		}
		if !stamp.Before(until) {
			continue
		}
		if m.Author.ID == s.Owner {
			if safeText(m.Content, r.Token) != m.Content || !historyAttachmentsValid(m) {
				return p, monthlyInvalid("selected_owner_message")
			}
			p.Messages = append(p.Messages, m)
		}
	}
	p.Scanned = len(messages)
	if len(messages) > 0 {
		last := messages[len(messages)-1]
		p.LastID, p.LastTime = last.ID, last.Timestamp
	}
	next := cursor
	if len(messages) == 0 || below {
		next.Index++
		next.Before = historyHighBefore(until)
	} else {
		next.Before = p.LastID
	}
	p.Complete = next.Index == len(s.Channels)
	if !p.Complete {
		p.Next = &next
	}
	p.Acquired = r.Now().UTC().Format(time.RFC3339Nano)
	return p, nil
}
