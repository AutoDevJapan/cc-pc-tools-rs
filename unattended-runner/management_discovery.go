package main

import (
	"context"
	"errors"
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
			r.State.Threads[c.ID] = Thread{ID: c.ID, Guild: scope.Guild, Type: c.Type, Last: c.Last, Managed: true}
		}
	}
	for _, t := range active {
		if _, ok := parents[t.Parent]; !ok || t.Guild != "" && t.Guild != scope.Guild || !monthlyID(t.ID) || (t.Type != 10 && t.Type != 11 && t.Type != 12) {
			continue
		}
		if old, exists := r.State.Threads[t.ID]; !exists || old.Managed {
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
	// Unlike the immutable monthly audit, this recurring pass starts afresh
	// after its terminal page. Do not carry an old boundary flag into a new pass.
	ties := prior.Next != "" && prior.BoundaryUnresolved
	page, err = readMonthlyInventory(ctx, r, s, "", prior.Next, ties)
	if err != nil {
		failure := monthlyFailure{Stage: "archive_source"}
		_ = errors.As(err, &failure)
		prior.FailureStage, prior.HTTPStatus, prior.Checked, prior.Complete = failure.Stage, failure.Status, r.Now().UTC().Format(time.RFC3339Nano), false
		r.State.Discovery["management:"+key] = prior
		_ = r.save()
		r.DiscoveryErrors = append(r.DiscoveryErrors, "management_archive_unavailable")
		return
	}
	reachedLower := false
	cutoff, _ := time.Parse(time.RFC3339Nano, r.State.Since)
	for _, c := range page.Channels {
		stamp, stampErr := time.Parse(time.RFC3339Nano, c.Archive)
		if stampErr == nil && stamp.Before(cutoff) {
			// Joined-private pages are ordered by ID, not archival time.
			if s.Mode != "joined-private-archived" {
				reachedLower = true
			}
			continue
		}
		if old, exists := r.State.Threads[c.ID]; !exists || old.Managed {
			r.State.Threads[c.ID] = Thread{ID: c.ID, Guild: scope.Guild, Parent: c.Parent, Type: c.Type, Last: c.Last, Managed: true}
		}
	}
	r.State.Discovery["management:"+key] = DiscoveryPage{Before: prior.Next, Next: page.Next, Returned: page.Scanned, HasMore: !page.Complete, Complete: page.Complete && !page.TimestampBoundary, Pages: prior.Pages + 1, Checked: page.Acquired, BoundaryUnresolved: page.TimestampBoundary}
	if reachedLower {
		r.State.Discovery["management:"+key] = DiscoveryPage{Before: prior.Next, Returned: page.Scanned, Complete: !page.TimestampBoundary, Pages: prior.Pages + 1, Checked: page.Acquired, BoundaryUnresolved: page.TimestampBoundary, Coverage: "archive_lower_bound_reached", LowerBound: r.State.Since}
	}
	if r.save() != nil {
		r.State.Discovery["management:"+key] = prior
		r.DiscoveryErrors = append(r.DiscoveryErrors, "management_archive_save_deferred")
	}
}

func (r *Runner) replayManagement(ctx context.Context, health *Health) {
	ids := make([]string, 0, len(r.State.Threads))
	for id, thread := range r.State.Threads {
		if !thread.Managed || r.Config.ManagementIntake {
			ids = append(ids, id)
		}
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

// Preserve the legacy request forum's full turn. Additional guild channels have
// an independent cursor and bounded turn, so they cannot occupy its eight slots.
func (r *Runner) scanIntakeThreads(ctx context.Context, health *Health, managed bool, budget int) {
	ids := make([]string, 0)
	for id, thread := range r.State.Threads {
		if thread.Managed == managed {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	cursor := &r.State.Cursor
	if managed {
		cursor = &r.State.ManagedScanCursor
	}
	start := sort.Search(len(ids), func(i int) bool { return ids[i] > *cursor })
	cutoff, _ := time.Parse(time.RFC3339Nano, r.State.Since)
	seen := 0
	for i := 0; i < len(ids) && seen < budget; i++ {
		if ctx.Err() != nil {
			return
		}
		id := ids[(start+i)%len(ids)]
		thread := r.State.Threads[id]
		after, _ := number(snowflake(cutoff))
		scan := r.State.Scans[id]
		if scan != nil {
			after, _ = number(scan.After)
		}
		last, known := number(thread.Last)
		if known && last <= after && (scan == nil || scan.Before == "") {
			continue
		}
		seen++
		*cursor = id
		if err := r.save(); err != nil {
			health.Errors = append(health.Errors, "scan_cursor_deferred")
			return
		}
		n, err := r.scan(ctx, thread)
		if err != nil {
			health.Errors = append(health.Errors, "discord_history_deferred")
			continue
		}
		health.Captured += n
		health.Scanned++
	}
}
