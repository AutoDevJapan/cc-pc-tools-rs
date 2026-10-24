package main

import (
	"context"
	"errors"
	"net/url"
	"time"
)

// One archived page per forum per tick, with a durable continuation. Complete
// means the end was reached in this pass, never that a bounded page was a scan.
type DiscoveryPage struct {
	Before             string `json:"before,omitempty"`
	Next               string `json:"next,omitempty"`
	Returned           int    `json:"returned"`
	HasMore            bool   `json:"has_more"`
	Complete           bool   `json:"complete"`
	Pages              uint64 `json:"pages"`
	Checked            string `json:"checked_at"`
	BoundaryUnresolved bool   `json:"timestamp_boundary_unresolved,omitempty"`
	FailureStage       string `json:"failure_stage,omitempty"`
	HTTPStatus         int    `json:"http_status,omitempty"`
	Coverage           string `json:"coverage,omitempty"`
	LowerBound         string `json:"lower_bound,omitempty"`
}

func (r *Runner) eligibleThread(t Thread, known map[string]bool) bool {
	if _, ok := number(t.ID); !ok {
		return false
	}
	if t.Type != 11 || (t.Guild != "" && t.Guild != r.State.Destination.Guild) {
		return false
	}
	if t.Parent == requestForum {
		// The real audit found no post-cutover missing reply in legacy bot
		// request threads. Admit bot threads only with an exact new receipt or
		// verified origin; do not turn legacy threads into new approvals.
		return t.Owner == r.State.Destination.Owner || (t.Owner == r.State.Destination.Bot && (known[t.ID] || r.ProgressLinks[t.ID].Todo != 0))
	}
	return t.Parent == r.State.Destination.Forum && t.Owner == r.State.Destination.Bot
}

func (r *Runner) discoverPages(ctx context.Context, known map[string]bool) {
	if r.State.Discovery == nil {
		r.State.Discovery = make(map[string]DiscoveryPage)
	}
	r.DiscoveryErrors = nil
	type listing struct {
		Threads []Thread `json:"threads"`
		HasMore bool     `json:"has_more"`
	}
	var active listing
	if err := r.get(ctx, "/guilds/"+r.State.Destination.Guild+"/threads/active", &active); err != nil {
		r.DiscoveryErrors = append(r.DiscoveryErrors, "active_thread_discovery_deferred")
	} else {
		for _, t := range active.Threads {
			if r.eligibleThread(t, known) {
				t.Guild = r.State.Destination.Guild
				r.State.Threads[t.ID] = t
			}
		}
		r.State.Discovery["active"] = DiscoveryPage{Returned: len(active.Threads), Complete: true, Pages: 1, Checked: r.Now().UTC().Format(time.RFC3339Nano)}
	}
	forums := []string{requestForum}
	if r.State.Destination.Forum != requestForum {
		forums = append(forums, r.State.Destination.Forum)
	}
	for _, forum := range forums {
		prior := r.State.Discovery[forum]
		path := "/channels/" + forum + "/threads/archived/public?limit=100"
		if prior.Next != "" {
			path += "&before=" + url.QueryEscape(prior.Next)
		}
		var page listing
		if err := r.get(ctx, path, &page); err != nil {
			r.DiscoveryErrors = append(r.DiscoveryErrors, "archived_thread_discovery_deferred")
			continue
		}
		next := ""
		var err error
		if page.HasMore {
			if len(page.Threads) == 0 {
				err = errors.New("empty archive continuation")
			} else {
				next = page.Threads[len(page.Threads)-1].Metadata.ArchiveTimestamp
				stamp, e := time.Parse(time.RFC3339Nano, next)
				if e != nil {
					err = e
				} else if prior.Next != "" {
					old, e := time.Parse(time.RFC3339Nano, prior.Next)
					if e != nil || !stamp.Before(old) {
						err = errors.New("archive continuation did not advance")
					}
				}
			}
		}
		for _, t := range page.Threads {
			if t.Parent != forum || t.Guild != "" && t.Guild != r.State.Destination.Guild {
				err = errors.New("archive source differs")
			}
			if _, ok := number(t.ID); !ok {
				err = errors.New("archive identity invalid")
			}
		}
		if err != nil || len(page.Threads) > 100 {
			r.DiscoveryErrors = append(r.DiscoveryErrors, "archive_page_invalid")
			continue
		}
		for _, t := range page.Threads {
			if r.eligibleThread(t, known) {
				t.Guild = r.State.Destination.Guild
				r.State.Threads[t.ID] = t
			}
		}
		r.State.Discovery[forum] = DiscoveryPage{Before: prior.Next, Next: next, Returned: len(page.Threads), HasMore: page.HasMore, Complete: !page.HasMore, Pages: prior.Pages + 1, Checked: r.Now().UTC().Format(time.RFC3339Nano)}
		// Do not move a continuation past threads that were not durably saved.
		if r.save() != nil {
			r.State.Discovery[forum] = prior
			r.DiscoveryErrors = append(r.DiscoveryErrors, "archive_cursor_deferred")
			return
		}
	}
	if r.Config.ManagementIntake {
		r.discoverManagement(ctx, active.Threads)
	}
}
