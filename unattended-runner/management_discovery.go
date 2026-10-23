package main

import (
	"context"
	"sort"
	"strings"
	"time"
)

// Reuse the qualified monthly metadata reader but retain ordinary discovery
// progress in the existing state. The monthly audit receipts/cursors are untouched.
func (r *Runner) discoverManagement(ctx context.Context, active []Thread) {
	scope := monthlyAuditScope{Guild: r.State.Destination.Guild, Owner: r.State.Destination.Owner, Since: r.State.Since, Until: r.Now().UTC().Format(time.RFC3339Nano)}
	s := monthlyInventoryScope{monthlyAuditScope: scope, Mode: "channels"}
	page, err := readMonthlyInventory(ctx, r, s, "", "", false)
	if err != nil {
		r.DiscoveryErrors = append(r.DiscoveryErrors, "management_channels_unavailable")
		return
	}
	parents := map[string]int{}
	for _, c := range page.Channels {
		parents[c.ID] = c.Type
		if c.Type == 0 || c.Type == 5 || c.Type == 2 || c.Type == 13 {
			r.State.Threads[c.ID] = Thread{ID: c.ID, Guild: scope.Guild, Type: c.Type, Managed: true}
		}
	}
	for _, t := range active {
		if _, ok := parents[t.Parent]; !ok || t.Guild != "" && t.Guild != scope.Guild || !monthlyID(t.ID) || (t.Type != 10 && t.Type != 11 && t.Type != 12) {
			continue
		}
		if _, exists := r.State.Threads[t.ID]; !exists {
			t.Guild = scope.Guild
			t.Managed = true
			r.State.Threads[t.ID] = t
		}
	}
	r.State.Discovery["management:channels"] = DiscoveryPage{Returned: page.Scanned, Complete: true, Pages: 1, Checked: page.Acquired}
	// One archive endpoint per tick, rotating even after a denied/invalid page.
	// Private variants apply only to GUILD_TEXT, exactly as the monthly reader.
	keys := make([]string, 0)
	for id, typ := range parents {
		if typ == 0 || typ == 5 || typ == 15 || typ == 16 {
			if id != requestForum && id != r.State.Destination.Forum {
				keys = append(keys, id+"/public-archived")
			}
			if typ == 0 {
				keys = append(keys, id+"/private-archived", id+"/joined-private-archived")
			}
		}
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		_ = r.save()
		return
	}
	start := sort.Search(len(keys), func(i int) bool { return keys[i] > r.State.ManagedDiscoveryCursor })
	key := keys[start%len(keys)]
	r.State.ManagedDiscoveryCursor = key
	if r.save() != nil {
		r.DiscoveryErrors = append(r.DiscoveryErrors, "management_archive_cursor_deferred")
		return
	}
	parts := strings.SplitN(key, "/", 2)
	prior := r.State.Discovery["management:"+key]
	s.Mode, s.Channel = parts[1], parts[0]
	page, err = readMonthlyInventory(ctx, r, s, "", prior.Next, prior.BoundaryUnresolved)
	if err != nil {
		r.DiscoveryErrors = append(r.DiscoveryErrors, "management_archive_unavailable")
		return
	}
	for _, c := range page.Channels {
		if _, exists := r.State.Threads[c.ID]; !exists {
			r.State.Threads[c.ID] = Thread{ID: c.ID, Guild: scope.Guild, Parent: c.Parent, Type: c.Type, Managed: true}
		}
	}
	r.State.Discovery["management:"+key] = DiscoveryPage{Before: prior.Next, Next: page.Next, Returned: page.Scanned, HasMore: !page.Complete, Complete: page.Complete && !page.TimestampBoundary, Pages: prior.Pages + 1, Checked: page.Acquired, BoundaryUnresolved: page.TimestampBoundary}
	if r.save() != nil {
		r.State.Discovery["management:"+key] = prior
		r.DiscoveryErrors = append(r.DiscoveryErrors, "management_archive_save_deferred")
	}
}

func (r *Runner) replayManagement(ctx context.Context, health *Health) {
	ids := make([]string, 0, len(r.State.Threads))
	for id := range r.State.Threads {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 || ctx.Err() != nil {
		return
	}
	start := sort.Search(len(ids), func(i int) bool { return ids[i] > r.State.ReplayCursor })
	id := ids[start%len(ids)]
	r.State.ReplayCursor = id
	if r.save() != nil {
		health.Errors = append(health.Errors, "management_replay_cursor_deferred")
		return
	}
	n, err := r.scanPage(ctx, r.State.Threads[id], true)
	if err != nil {
		health.Errors = append(health.Errors, "management_replay_unavailable")
		return
	}
	health.Captured += n
}
