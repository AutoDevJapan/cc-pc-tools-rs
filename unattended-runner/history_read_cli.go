package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type historyReadMessage struct {
	Message
	Type        *int                     `json:"type,omitempty"`
	Attachments *[]historyReadAttachment `json:"attachments"`
}

// A nil observation is unknown; a non-nil empty slice is the API's empty array.
// Deliberately do not decode filenames, descriptions or signed media URLs.
type historyReadAttachment struct {
	ID   string `json:"id"`
	Size *int64 `json:"size"`
}

type historyReadScope struct {
	Domain  string   `json:"domain"`
	Todo    uint64   `json:"todo_id"`
	Guild   string   `json:"guild_id"`
	Owner   string   `json:"owner_id"`
	Since   string   `json:"since"`
	Until   string   `json:"until"`
	Threads []string `json:"thread_ids"`
}

type historyReadCursor struct {
	ScopeSHA    string `json:"scope_sha256"`
	ThreadIndex int    `json:"thread_index"`
	Before      string `json:"before"`
}

type historyReadPage struct {
	Domain   string            `json:"domain"`
	Source   string            `json:"source"`
	Todo     uint64            `json:"todo_id"`
	Guild    string            `json:"guild_id"`
	Owner    string            `json:"owner_id"`
	Bot      string            `json:"bot_id"`
	ScopeSHA string            `json:"scope_sha256"`
	Since    string            `json:"since"`
	Until    string            `json:"until"`
	Acquired string            `json:"acquired_at"`
	Cursor   historyReadCursor `json:"cursor"`
	Thread   string            `json:"thread_id"`
	// Absent structural fields identify an original two-forum v1 receipt.
	ChannelType *int                 `json:"channel_type,omitempty"`
	Parent      string               `json:"parent_id,omitempty"`
	Starter     *historyReadMessage  `json:"starter"`
	Scanned     int                  `json:"scanned_messages"`
	LastID      string               `json:"last_scanned_id,omitempty"`
	LastTime    string               `json:"last_scanned_timestamp,omitempty"`
	Messages    []historyReadMessage `json:"messages"`
	Complete    bool                 `json:"complete"`
	Next        *historyReadCursor   `json:"next"`
}

// This command uses the ordinary credential loader, but never opens live intake,
// old question state, name registries, locks, or a browser's application state.
func loadHistoryRead(ctx context.Context, scope historyReadScope) (*Runner, error) {
	var cfg Config
	if readJSON(conversationDefaultConfig, 1<<20, &cfg) != nil || cfg.Project != "cc-pc-tools-rs" {
		return nil, errors.New("configuration unavailable")
	}
	var destination Destination
	if readJSON(cfg.NotifyConfig, 1<<20, &destination) != nil {
		return nil, errors.New("destination unavailable")
	}
	for _, id := range []string{destination.Guild, destination.Owner, destination.Bot, destination.Forum} {
		if _, ok := number(id); !ok {
			return nil, errors.New("destination invalid")
		}
	}
	if destination.Guild != scope.Guild || destination.Owner != scope.Owner {
		return nil, errors.New("destination differs")
	}
	token := discordToken()
	if token == "" {
		return nil, errors.New("credential unavailable")
	}
	return &Runner{State: &State{Destination: destination}, Client: &http.Client{Timeout: 6 * time.Second}, Token: token, Base: "https://discord.com/api/v10", Now: time.Now}, nil
}

func historyReadGuard(ctx context.Context, path string) bool {
	raw, err := runCommand(ctx, "D:/CC/Tools/client-records/scrub391.exe", "--guard-path", path)
	var result struct {
		OK        bool  `json:"ok"`
		Protected *bool `json:"client_resource"`
		Unknown   *bool `json:"classification_unverified"`
	}
	return err == nil && json.Unmarshal([]byte(raw), &result) == nil && result.OK && result.Protected != nil && !*result.Protected && result.Unknown != nil && !*result.Unknown
}

func historyReadCLI(args []string, out io.Writer) int {
	return historyReadCLIWith(args, out, loadHistoryRead, historyReadGuard)
}

func historyPlainParent(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	for parent := filepath.Dir(filepath.Clean(path)); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		// Go 1.23+ reports Windows mount points as irregular, not symlinks.
		if err != nil || !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return false
		}
		if filepath.Dir(parent) == parent {
			return true
		}
	}
}

func historyPrivateInput(ctx context.Context, path string, guard func(context.Context, string) bool) ([]byte, error) {
	if !historyPlainParent(path) || !guard(ctx, path) {
		return nil, errors.New("input not qualified")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4<<20 {
		return nil, errors.New("input unavailable")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 4<<20+1))
	if err != nil || len(raw) > 4<<20 {
		return nil, errors.New("input too large")
	}
	return raw, nil
}

func historyStrictJSON(raw []byte, value any) error {
	keys := json.NewDecoder(bytes.NewReader(raw))
	if historyJSONValue(keys, 0) != nil {
		return errors.New("ambiguous JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func historyJSONValue(d *json.Decoder, depth int) error {
	if depth > 64 {
		return errors.New("JSON depth")
	}
	token, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			s, ok := key.(string)
			folded := historyFoldJSONKey(s)
			if !ok || seen[folded] {
				return errors.New("duplicate key")
			}
			seen[folded] = true
			if err = historyJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err = historyJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("unexpected delimiter")
	}
	_, err = d.Token()
	return err
}

// encoding/json matches struct fields with Unicode simple case folding.
func historyFoldJSONKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		for {
			next := unicode.SimpleFold(r)
			if next <= r {
				b.WriteRune(next)
				break
			}
			r = next
		}
	}
	return b.String()
}

func historyScopeValid(scope historyReadScope, now time.Time) bool {
	if scope.Domain != "discord-history-read-scope-v1" || scope.Todo == 0 || len(scope.Threads) == 0 || len(scope.Threads) > 10 {
		return false
	}
	for _, id := range []string{scope.Guild, scope.Owner} {
		if _, ok := number(id); !ok {
			return false
		}
	}
	since, e1 := time.Parse(time.RFC3339Nano, scope.Since)
	until, e2 := time.Parse(time.RFC3339Nano, scope.Until)
	if e1 != nil || e2 != nil || since.Before(time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC)) || !since.Before(until) || until.After(now) || until.Sub(since) > 31*24*time.Hour {
		return false
	}
	high, _ := number(historyHighBefore(until))
	seen := map[string]bool{}
	for _, id := range scope.Threads {
		if n, ok := number(id); !ok || seen[id] || n > high {
			return false
		}
		seen[id] = true
	}
	return true
}

func historyReadCLIWith(args []string, out io.Writer, load func(context.Context, historyReadScope) (*Runner, error), guard func(context.Context, string) bool) int {
	fail := func() int { _, _ = io.WriteString(out, "history read unavailable\n"); return 1 }
	var scopePath, output, cursorPath string
	flags := flag.NewFlagSet("history-read", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	conversationStringFlag(flags, "scope", &scopePath)
	conversationStringFlag(flags, "output", &output)
	conversationStringFlag(flags, "cursor", &cursorPath)
	if flags.Parse(args) != nil || flags.NArg() != 0 || scopePath == "" || output == "" {
		return fail()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if !historyPlainParent(output) || !strings.HasSuffix(output, ".private.json") || !guard(ctx, filepath.Dir(output)) {
		return fail()
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		return fail()
	}
	raw, err := historyPrivateInput(ctx, scopePath, guard)
	if err != nil {
		return fail()
	}
	var scope historyReadScope
	if historyStrictJSON(raw, &scope) != nil || !historyScopeValid(scope, time.Now()) {
		return fail()
	}
	hash := bytesHash(raw)
	cursor := historyReadCursor{ScopeSHA: hash, Before: historyHighBefore(mustHistoryTime(scope.Until))}
	var prior historyReadPage
	if cursorPath != "" {
		priorRaw, err := historyPrivateInput(ctx, cursorPath, guard)
		if err != nil {
			return fail()
		}
		if historyStrictJSON(priorRaw, &prior) != nil || !historyContinuationValid(prior, scope, hash) {
			return fail()
		}
		cursor = *prior.Next
	}
	before, valid := number(cursor.Before)
	high, _ := number(historyHighBefore(mustHistoryTime(scope.Until)))
	if cursor.ScopeSHA != hash || cursor.ThreadIndex < 0 || cursor.ThreadIndex >= len(scope.Threads) || !valid || before > high {
		return fail()
	}
	runner, err := load(ctx, scope)
	if err != nil || runner == nil || runner.State == nil || runner.State.Destination.Guild != scope.Guild || runner.State.Destination.Owner != scope.Owner {
		return fail()
	}
	if cursorPath != "" && prior.Bot != runner.State.Destination.Bot {
		return fail()
	}
	var previous *historyReadPage
	if cursorPath != "" {
		previous = &prior
	}
	page, err := readHistoryPage(ctx, runner, scope, cursor, previous)
	if err != nil || ctx.Err() != nil {
		return fail()
	}
	b, err := json.MarshalIndent(page, "", "  ")
	if err != nil || len(b) > 4<<20 {
		return fail()
	}
	if err = writeHistoryReceipt(output, b); err != nil {
		return fail()
	}
	ownerRecords, ownerCount, ownerOther, ownerUnknown := 0, 0, 0, 0
	for _, m := range page.Messages {
		if m.Author.ID == scope.Owner {
			ownerRecords++
			if m.Type == nil {
				ownerUnknown++
			} else if *m.Type == 0 || *m.Type == 19 {
				ownerCount++
			} else {
				ownerOther++
			}
		}
	}
	result := map[string]any{"domain": page.Domain, "source": page.Source, "todo_id": page.Todo, "thread_id": page.Thread, "scanned_messages": page.Scanned, "owner_records": ownerRecords, "owner_messages": ownerCount, "owner_other_types": ownerOther, "owner_unknown_types": ownerUnknown, "complete": page.Complete, "next": page.Next, "receipt_sha256": bytesHash(b), "delivery": "inspection_only", "application": "not_inferred"}
	returnCode := 0
	if json.NewEncoder(out).Encode(result) != nil {
		returnCode = 1
	}
	return returnCode
}

// Check the continuation against the actual last scanned ID, including pages
// containing only third-party messages. Those bodies are deliberately absent.
func historyContinuationValid(prior historyReadPage, scope historyReadScope, hash string) bool {
	if prior.Domain != "discord-history-read-page-v1" || prior.Source != "discord-api-v10" || prior.ScopeSHA != hash || prior.Next == nil || prior.Complete || prior.Todo != scope.Todo || prior.Guild != scope.Guild || prior.Owner != scope.Owner || prior.Since != scope.Since || prior.Until != scope.Until || prior.Cursor.ScopeSHA != hash || prior.Cursor.ThreadIndex < 0 || prior.Cursor.ThreadIndex >= len(scope.Threads) || prior.Thread != scope.Threads[prior.Cursor.ThreadIndex] || prior.Scanned < 0 || prior.Scanned > 100 || len(prior.Messages) > prior.Scanned {
		return false
	}
	if !historyReceiptChannelValid(prior) {
		return false
	}
	before, ok := number(prior.Cursor.Before)
	high, _ := number(historyHighBefore(mustHistoryTime(scope.Until)))
	thread, _ := number(prior.Thread)
	if !ok || before > high || before < thread {
		return false
	}
	next := prior.Cursor
	if prior.Scanned == 0 {
		if prior.LastID != "" || prior.LastTime != "" {
			return false
		}
		next.ThreadIndex++
		next.Before = historyHighBefore(mustHistoryTime(scope.Until))
	} else {
		last, ok := number(prior.LastID)
		stamp, err := time.Parse(time.RFC3339Nano, prior.LastTime)
		if !ok || last >= before || last < thread || err != nil || stamp.After(mustHistoryTime(scope.Until).Add(time.Millisecond)) {
			return false
		}
		if stamp.Before(mustHistoryTime(scope.Since)) {
			next.ThreadIndex++
			next.Before = historyHighBefore(mustHistoryTime(scope.Until))
		} else {
			next.Before = prior.LastID
		}
	}
	return next.ThreadIndex < len(scope.Threads) && *prior.Next == next
}

func historyReceiptChannelValid(p historyReadPage) bool {
	// Old receipts retain their original starter shape and cursor semantics.
	if p.ChannelType == nil {
		return p.Parent == "" && p.Starter != nil && p.Starter.ID == p.Thread
	}
	switch *p.ChannelType {
	case 0, 2, 5, 13:
		return p.Starter == nil && (p.Parent == "" || monthlyID(p.Parent))
	case 10, 11, 12:
		return monthlyID(p.Parent) && (p.Starter == nil || (*p.ChannelType == 11 && p.Starter.ID == p.Thread))
	}
	return false
}

func mustHistoryTime(value string) time.Time { t, _ := time.Parse(time.RFC3339Nano, value); return t }

func historyHighBefore(until time.Time) string {
	return snowflake(until.Truncate(time.Millisecond).Add(time.Millisecond))
}

func readHistoryPage(ctx context.Context, r *Runner, scope historyReadScope, cursor historyReadCursor, prior *historyReadPage) (historyReadPage, error) {
	page := historyReadPage{Domain: "discord-history-read-page-v1", Source: "discord-api-v10", Todo: scope.Todo, Guild: scope.Guild, Owner: scope.Owner, Bot: r.State.Destination.Bot, ScopeSHA: cursor.ScopeSHA, Since: scope.Since, Until: scope.Until, Cursor: cursor, Thread: scope.Threads[cursor.ThreadIndex], Messages: []historyReadMessage{}}
	bad := errors.New("history source differs or unavailable")
	var bot struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	}
	if r.get(ctx, "/users/@me", &bot) != nil || !bot.Bot || bot.ID != page.Bot {
		return page, bad
	}
	channel, err := monthlyReadable(ctx, r, page.Thread)
	if err != nil {
		return page, bad
	}
	page.ChannelType, page.Parent = channel.Type, channel.Parent
	thread := *channel.Type == 10 || *channel.Type == 11 || *channel.Type == 12
	forumPost := *channel.Type == 11 && channel.ParentType != nil && (*channel.ParentType == 15 || *channel.ParentType == 16)
	legacy := thread && (channel.Parent == r.State.Destination.Forum || channel.Parent == requestForum)
	if legacy && (!forumPost || (channel.Owner != page.Bot && channel.Owner != scope.Owner)) {
		return page, bad
	}
	if prior != nil && prior.Thread == page.Thread {
		if prior.ChannelType == nil {
			// Genuine old v1 pages could only originate in the two legacy forums.
			if !legacy {
				return page, bad
			}
		} else if *prior.ChannelType != *page.ChannelType || prior.Parent != page.Parent {
			return page, bad
		}
	}
	// Forum/media posts have a same-ID starter. Private threads and ordinary
	// channels need no such message; their access was proved before any body GET.
	// For other public threads, the page preserves actual reply references.
	if forumPost {
		var starter historyReadMessage
		if r.get(ctx, "/channels/"+page.Thread+"/messages/"+page.Thread, &starter) != nil || !historyMessageMetadataValid(starter.Message, page.Thread, r) || starter.ID != page.Thread {
			return page, bad
		}
		selected := starter.Author.ID == scope.Owner || starter.Author.ID == page.Bot
		if legacy && !selected {
			return page, bad
		}
		if selected {
			if !historyMessageValid(starter.Message, page.Thread, r) || !historyAttachmentsValid(starter) {
				return page, bad
			}
			page.Starter = &starter
		}
	}
	var messages []historyReadMessage
	if r.get(ctx, "/channels/"+page.Thread+"/messages?limit=100&before="+cursor.Before, &messages) != nil || messages == nil || len(messages) > 100 {
		return page, bad
	}
	previous, _ := number(cursor.Before)
	threadID, _ := number(page.Thread)
	since, until := mustHistoryTime(scope.Since), mustHistoryTime(scope.Until)
	below := false
	previousTime := until.Add(time.Millisecond)
	for _, message := range messages {
		id, ok := number(message.ID)
		if !ok || id >= previous || id < threadID || !historyMessageMetadataValid(message.Message, page.Thread, r) {
			return page, bad
		}
		previous = id
		stamp := mustHistoryTime(message.Timestamp)
		if stamp.After(previousTime) {
			return page, bad
		}
		previousTime = stamp
		if stamp.Before(since) {
			below = true
			continue
		}
		if !stamp.Before(until) {
			continue
		}
		if message.Author.ID == scope.Owner || message.Author.ID == page.Bot {
			if safeText(message.Content, r.Token) != message.Content || !historyAttachmentsValid(message) {
				return page, bad
			}
			page.Messages = append(page.Messages, message)
		}
	}
	page.Scanned = len(messages)
	if len(messages) > 0 {
		page.LastID = messages[len(messages)-1].ID
		page.LastTime = messages[len(messages)-1].Timestamp
	}
	next := cursor
	if len(messages) == 0 || below {
		next.ThreadIndex++
		next.Before = historyHighBefore(until)
	} else {
		next.Before = messages[len(messages)-1].ID
	}
	page.Complete = next.ThreadIndex == len(scope.Threads)
	if !page.Complete {
		page.Next = &next
	}
	page.Acquired = r.Now().UTC().Format(time.RFC3339Nano)
	return page, nil
}

func historyAttachmentsValid(message historyReadMessage) bool {
	if message.Attachments == nil {
		return true
	}
	for _, attachment := range *message.Attachments {
		if _, ok := number(attachment.ID); !ok || attachment.Size == nil || *attachment.Size < 0 {
			return false
		}
	}
	return true
}

func historyMessageValid(message Message, thread string, r *Runner) bool {
	return historyMessageMetadataValid(message, thread, r) && safeText(message.Content, r.Token) == message.Content
}

func historyMessageMetadataValid(message Message, thread string, r *Runner) bool {
	if _, ok := number(message.ID); !ok {
		return false
	}
	if _, ok := number(message.Author.ID); !ok {
		return false
	}
	if message.Channel != thread || (message.Author.ID == r.State.Destination.Owner && message.Author.Bot) || (message.Author.ID == r.State.Destination.Bot && !message.Author.Bot) {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, message.Timestamp)
	if err != nil || t.After(r.Now()) {
		return false
	}
	if message.Edited != "" {
		edited, err := time.Parse(time.RFC3339Nano, message.Edited)
		if err != nil || edited.Before(t) || edited.After(r.Now()) {
			return false
		}
	}
	return true
}

func writeHistoryReceipt(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".history-*.tmp")
	if err != nil {
		return err
	}
	defer f.Close()
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// Link the flushed complete bytes without replacing a concurrent receipt.
	return os.Link(f.Name(), path)
}
