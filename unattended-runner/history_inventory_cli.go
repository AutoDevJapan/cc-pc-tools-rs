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
	"strings"
	"time"
)

// Metadata discovery is explicitly requested, separate from message reading.
// Thread names, messages, the legacy ledger and resident state are never read.
type historyInventoryScope struct {
	Domain string `json:"domain"`
	Todo   uint64 `json:"todo_id"`
	Guild  string `json:"guild_id"`
	Owner  string `json:"owner_id"`
	Since  string `json:"since"`
	Until  string `json:"until"`
	Forum  string `json:"forum"`
	Mode   string `json:"mode"`
}

type historyInventoryPage struct {
	Domain      string   `json:"domain"`
	Source      string   `json:"source"`
	ScopeSHA    string   `json:"scope_sha256"`
	Todo        uint64   `json:"todo_id"`
	Guild       string   `json:"guild_id"`
	Owner       string   `json:"owner_id"`
	Bot         string   `json:"bot_id"`
	Forum       string   `json:"forum_id"`
	Mode        string   `json:"mode"`
	Before      string   `json:"before,omitempty"`
	LastArchive string   `json:"last_scanned_archive_timestamp,omitempty"`
	Acquired    string   `json:"acquired_at"`
	Scanned     int      `json:"scanned_threads"`
	Threads     []Thread `json:"threads"`
	Complete    bool     `json:"complete"`
	Next        string   `json:"next,omitempty"`
}

func historyInventoryCLI(args []string, out io.Writer) int {
	return historyInventoryCLIWith(args, out, loadHistoryRead, historyReadGuard)
}

func historyInventoryCLIWith(args []string, out io.Writer, load func(context.Context, historyReadScope) (*Runner, error), guard func(context.Context, string) bool) int {
	fail := func() int { _, _ = io.WriteString(out, "history inventory unavailable\n"); return 1 }
	var scopePath, output, cursorPath string
	flags := flag.NewFlagSet("history-inventory", flag.ContinueOnError)
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
	var scope historyInventoryScope
	if historyStrictJSON(raw, &scope) != nil || scope.Domain != "discord-history-inventory-scope-v1" || (scope.Forum != "task" && scope.Forum != "requests") || (scope.Mode != "active" && scope.Mode != "archived") {
		return fail()
	}
	readScope := historyReadScope{Domain: "discord-history-read-scope-v1", Todo: scope.Todo, Guild: scope.Guild, Owner: scope.Owner, Since: scope.Since, Until: scope.Until, Threads: []string{"1"}}
	if !historyScopeValid(readScope, time.Now()) {
		return fail()
	}
	hash, before := bytesHash(raw), ""
	var previousPage *historyInventoryPage
	if cursorPath != "" {
		b, err := historyPrivateInput(ctx, cursorPath, guard)
		var prior historyInventoryPage
		if err != nil || historyStrictJSON(b, &prior) != nil || prior.Domain != "discord-history-inventory-page-v1" || prior.Source != "discord-api-v10" || prior.ScopeSHA != hash || prior.Todo != scope.Todo || prior.Guild != scope.Guild || prior.Owner != scope.Owner || prior.Mode != "archived" || prior.Mode != scope.Mode || prior.Complete || prior.Scanned < 1 || prior.Scanned > 100 || prior.Next == "" || prior.Next != prior.LastArchive {
			return fail()
		}
		next, err := time.Parse(time.RFC3339Nano, prior.Next)
		if err != nil || next.After(time.Now()) {
			return fail()
		}
		if prior.Before != "" {
			old, e := time.Parse(time.RFC3339Nano, prior.Before)
			if e != nil || !next.Before(old) {
				return fail()
			}
		}
		before = prior.Next
		previousPage = &prior
	}
	r, err := load(ctx, readScope)
	if err != nil || r == nil || r.State == nil || r.State.Destination.Guild != scope.Guild || r.State.Destination.Owner != scope.Owner {
		return fail()
	}
	expectedForum := r.State.Destination.Forum
	if scope.Forum == "requests" {
		expectedForum = requestForum
	}
	if previousPage != nil && (previousPage.Forum != expectedForum || previousPage.Bot != r.State.Destination.Bot) {
		return fail()
	}
	page, err := readHistoryInventory(ctx, r, scope, hash, before)
	if err != nil || ctx.Err() != nil {
		return fail()
	}
	b, err := json.MarshalIndent(page, "", "  ")
	if err != nil || len(b) > 4<<20 || writeHistoryReceipt(output, b) != nil {
		return fail()
	}
	if json.NewEncoder(out).Encode(map[string]any{"domain": page.Domain, "source": page.Source, "todo_id": page.Todo, "scanned_threads": page.Scanned, "selected_threads": len(page.Threads), "complete": page.Complete, "receipt_sha256": bytesHash(b), "coverage": "metadata_only", "delivery": "inspection_only", "application": "not_inferred"}) != nil {
		return 1
	}
	return 0
}

func readHistoryInventory(ctx context.Context, r *Runner, scope historyInventoryScope, hash, before string) (historyInventoryPage, error) {
	page := historyInventoryPage{Domain: "discord-history-inventory-page-v1", Source: "discord-api-v10", ScopeSHA: hash, Todo: scope.Todo, Guild: scope.Guild, Owner: scope.Owner, Bot: r.State.Destination.Bot, Forum: r.State.Destination.Forum, Mode: scope.Mode, Before: before, Threads: []Thread{}}
	bad := errors.New("history inventory source unavailable")
	if scope.Forum == "requests" {
		page.Forum = requestForum
	}
	var me struct {
		ID  string `json:"id"`
		Bot bool   `json:"bot"`
	}
	if r.get(ctx, "/users/@me", &me) != nil || me.ID != page.Bot || !me.Bot {
		return page, bad
	}
	var forum struct {
		ID    string `json:"id"`
		Guild string `json:"guild_id"`
		Type  int    `json:"type"`
	}
	if r.get(ctx, "/channels/"+page.Forum, &forum) != nil || forum.ID != page.Forum || forum.Guild != scope.Guild || forum.Type != 15 {
		return page, bad
	}
	path := "/guilds/" + scope.Guild + "/threads/active"
	if scope.Mode == "archived" {
		path = "/channels/" + page.Forum + "/threads/archived/public?limit=100"
		if before != "" {
			path += "&before=" + url.QueryEscape(before)
		}
	}
	var listing struct {
		Threads []Thread `json:"threads"`
		HasMore *bool    `json:"has_more"`
	}
	if r.get(ctx, path, &listing) != nil || listing.Threads == nil || len(listing.Threads) > 10000 || (scope.Mode == "archived" && (listing.HasMore == nil || len(listing.Threads) > 100)) {
		return page, bad
	}
	seen := map[string]bool{}
	var previous time.Time
	if before != "" {
		previous = mustHistoryTime(before)
	}
	for _, thread := range listing.Threads {
		if _, ok := number(thread.ID); !ok || seen[thread.ID] || (thread.Guild != "" && thread.Guild != scope.Guild) {
			return page, bad
		}
		seen[thread.ID] = true
		if scope.Mode == "archived" {
			stamp, err := time.Parse(time.RFC3339Nano, thread.Metadata.ArchiveTimestamp)
			if err != nil || stamp.After(r.Now().Add(time.Minute)) || (!previous.IsZero() && stamp.After(previous)) || (before != "" && !stamp.Before(mustHistoryTime(before))) || thread.Parent != page.Forum {
				return page, bad
			}
			previous = stamp
			page.LastArchive = thread.Metadata.ArchiveTimestamp
		}
		if thread.Parent == page.Forum && thread.Type == 11 && (thread.Owner == scope.Owner || thread.Owner == page.Bot) {
			thread.Guild = scope.Guild
			page.Threads = append(page.Threads, thread)
		}
	}
	page.Scanned = len(listing.Threads)
	page.Complete = scope.Mode == "active" || !*listing.HasMore
	if !page.Complete {
		if page.LastArchive == "" {
			return page, bad
		}
		page.Next = page.LastArchive
	}
	page.Acquired = r.Now().UTC().Format(time.RFC3339Nano)
	return page, nil
}
