package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// New receipts only: deploying this must not reply to the historical backlog.
const ownerAcknowledgementSince = "2026-10-31T15:35:25Z"
const ownerAcknowledgementText = "受け取りました。"
const ownerAcknowledgementDomain = "discord-owner-acknowledgements-v1"
const ownerAcknowledgementLimit = 4 << 20
const ownerAcknowledgementBudget = 3 * time.Second
const ownerAcknowledgementPerTick = 2
const ownerAcknowledgementRetained = 1024

var errOwnerAcknowledgement = errors.New("owner acknowledgement deferred")
var errOwnerAcknowledgementHistory = errors.New("owner acknowledgement history coverage unavailable")

type acknowledgementPostFailure struct {
	kind      string
	retryAt   time.Time
	retryable bool
	terminal  bool
}

func (e acknowledgementPostFailure) Error() string { return "owner acknowledgement " + e.kind }

func acknowledgementStatus(err error) int {
	if err == nil {
		return 0
	}
	var code int
	if _, e := fmt.Sscanf(err.Error(), "discord status %d", &code); e == nil && err.Error() == fmt.Sprintf("discord status %d", code) {
		return code
	}
	return 0
}

type ownerAcknowledgement struct {
	Guild     string `json:"guild_id"`
	Thread    string `json:"thread_id"`
	Message   string `json:"message_id"`
	Author    string `json:"author_id"`
	Created   string `json:"created_at"`
	Edited    string `json:"edited_at"`
	BodySHA   string `json:"body_sha256"`
	Queued    string `json:"queued_at"`
	Checked   string `json:"checked_at,omitempty"`
	Attempted string `json:"post_attempted_at,omitempty"`
	Reply     string `json:"acknowledgement_message_id,omitempty"`
	Verified  string `json:"verified_at,omitempty"`
	Reused    bool   `json:"existing_acknowledgement,omitempty"`
	Stale     bool   `json:"source_version_changed,omitempty"`
	NextTry   string `json:"next_try_at,omitempty"`
	Terminal  string `json:"terminal_reason,omitempty"`
	Reported  bool   `json:"terminal_health_recorded,omitempty"`
}

func (a ownerAcknowledgement) version() string {
	return canonicalHash([]string{a.Guild, a.Thread, a.Message, a.Author, a.Created, a.Edited, a.BodySHA})
}

type ownerAcknowledgements struct {
	Domain         string                          `json:"domain"`
	Destination    Destination                     `json:"destination"`
	Receipts       map[string]ownerAcknowledgement `json:"receipts"`
	RecoveryCursor string                          `json:"capture_recovery_cursor,omitempty"`
}

func acknowledgementTime(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, true
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	return t, err == nil
}

func acknowledgementSameTime(a, b string) bool {
	x, ok := acknowledgementTime(a)
	y, other := acknowledgementTime(b)
	return ok && other && x.Equal(y)
}

func (r *Runner) acknowledgementCache() (*ownerAcknowledgements, error) {
	if !r.ownerAcknowledgementLoaded {
		s, err := r.loadOwnerAcknowledgements()
		r.ownerAcknowledgementLoaded = true
		r.ownerAcknowledgementLoadError = err
		if err == nil {
			r.ownerAcknowledgementCache = &s
		}
	}
	return r.ownerAcknowledgementCache, r.ownerAcknowledgementLoadError
}

func (r *Runner) acknowledgementArchive(key string) string {
	return filepath.Join(r.Config.Home, "owner-acknowledgements", key+".json")
}

func (r *Runner) archivedAcknowledgement(key string) (bool, error) {
	var saved struct {
		Domain      string               `json:"domain"`
		Destination Destination          `json:"destination"`
		Receipt     ownerAcknowledgement `json:"receipt"`
	}
	err := readJSON(r.acknowledgementArchive(key), 16<<10, &saved)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil || saved.Domain != ownerAcknowledgementDomain || saved.Destination != r.State.Destination || saved.Receipt.version() != key || saved.Receipt.Verified == "" && saved.Receipt.Terminal == "" && !saved.Receipt.Stale {
		return false, errOwnerAcknowledgement
	}
	return true, nil
}

func (r *Runner) ownerAcknowledgementPath() string {
	return filepath.Join(r.Config.Home, "owner-acknowledgements.json")
}

func (r *Runner) loadOwnerAcknowledgements() (ownerAcknowledgements, error) {
	s := ownerAcknowledgements{Domain: ownerAcknowledgementDomain, Destination: r.State.Destination, Receipts: map[string]ownerAcknowledgement{}}
	err := readJSON(r.ownerAcknowledgementPath(), ownerAcknowledgementLimit, &s)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil || s.Domain != ownerAcknowledgementDomain || s.Destination != r.State.Destination || s.Receipts == nil {
		return s, errOwnerAcknowledgement
	}
	for key, a := range s.Receipts {
		if key != a.version() || !shaPattern.MatchString(a.BodySHA) || a.Author != s.Destination.Owner || a.Guild != s.Destination.Guild || !monthlyID(a.Thread) || !monthlyID(a.Message) {
			return s, errOwnerAcknowledgement
		}
	}
	return s, nil
}

func (r *Runner) saveOwnerAcknowledgements(s ownerAcknowledgements) error {
	if len(s.Receipts) > ownerAcknowledgementRetained {
		keys := make([]string, 0)
		for key, a := range s.Receipts {
			if a.Verified != "" || a.Stale || a.Terminal != "" && a.Reported {
				keys = append(keys, key)
			}
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := s.Receipts[keys[i]], s.Receipts[keys[j]]
			if a.Queued == b.Queued {
				return keys[i] < keys[j]
			}
			return a.Queued < b.Queued
		})
		for _, key := range keys {
			if len(s.Receipts) <= ownerAcknowledgementRetained {
				break
			}
			saved := struct {
				Domain      string               `json:"domain"`
				Destination Destination          `json:"destination"`
				Receipt     ownerAcknowledgement `json:"receipt"`
			}{ownerAcknowledgementDomain, s.Destination, s.Receipts[key]}
			b, err := json.MarshalIndent(saved, "", "  ")
			if err != nil || len(b) > 16<<10 || writeConversationOnce(r.acknowledgementArchive(key), b) != nil {
				return errOwnerAcknowledgement
			}
			delete(s.Receipts, key)
		}
	}
	// publish uses exactly this encoding; never validate a smaller compact form.
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil || len(b) > ownerAcknowledgementLimit {
		return errOwnerAcknowledgement
	}
	return publish(r.ownerAcknowledgementPath(), s)
}

// Called only by the existing periodic receiver after a verified owner capture.
// The journal contains IDs/hashes, never the instruction or a second task state.
func (r *Runner) queueOwnerAcknowledgement(m Message) error {
	return r.queueOwnerAcknowledgements([]Message{m})
}

func (r *Runner) queueOwnerAcknowledgements(messages []Message) error {
	if !r.OwnerAcknowledgementsActive {
		return nil
	}
	s, err := r.acknowledgementCache()
	if err != nil {
		return err
	}
	changed := false
	var firstErr error
	for _, m := range messages {
		if m.Author.ID != r.State.Destination.Owner || m.Author.Bot || !monthlyID(m.Channel) || !monthlyID(m.ID) {
			continue
		}
		created, e1 := time.Parse(time.RFC3339Nano, m.Timestamp)
		cutoff, _ := time.Parse(time.RFC3339Nano, ownerAcknowledgementSince)
		versionAt := created
		if m.Edited != "" {
			edited, err := time.Parse(time.RFC3339Nano, m.Edited)
			if err != nil || edited.Before(created) {
				firstErr = errOwnerAcknowledgement
				continue
			}
			versionAt = edited
		}
		if e1 != nil || versionAt.After(r.Now().Add(time.Minute)) {
			firstErr = errOwnerAcknowledgement
			continue
		}
		if versionAt.Before(cutoff) {
			continue
		}
		edited := ""
		if m.Edited != "" {
			t, _ := acknowledgementTime(m.Edited)
			edited = t.UTC().Format(time.RFC3339Nano)
		}
		a := ownerAcknowledgement{Guild: r.State.Destination.Guild, Thread: m.Channel, Message: m.ID, Author: m.Author.ID, Created: created.UTC().Format(time.RFC3339Nano), Edited: edited, BodySHA: bytesHash([]byte(m.Content)), Queued: r.Now().UTC().Format(time.RFC3339Nano)}
		key := a.version()
		if _, exists := s.Receipts[key]; exists {
			continue
		}
		archived, err := r.archivedAcknowledgement(key)
		if err != nil {
			firstErr = err
			continue
		}
		if archived {
			continue
		}
		s.Receipts[key] = a
		changed = true
	}
	if !changed {
		return firstErr
	}
	if err := r.saveOwnerAcknowledgements(*s); err != nil {
		return err
	}
	return firstErr
}

func acknowledgementSourceMatches(a ownerAcknowledgement, live Message) bool {
	return live.ID == a.Message && live.Channel == a.Thread && live.Author.ID == a.Author && !live.Author.Bot && acknowledgementSameTime(live.Timestamp, a.Created) && acknowledgementSameTime(live.Edited, a.Edited) && bytesHash([]byte(live.Content)) == a.BodySHA
}

func acknowledgementBody(text string) bool {
	return text == ownerAcknowledgementText || strings.Contains(text, "受け取りました") || strings.Contains(text, "受け付けました") || strings.Contains(text, "Discordの指示を共有ToDo") || strings.Contains(text, "追加の指示を、共有ToDo")
}

// Inspect complete coverage back to the source, not a nonce's short cache.
// A missing/partial history can never prove that another POST is needed.
func (r *Runner) existingOwnerAcknowledgement(ctx context.Context, a ownerAcknowledgement) (string, error) {
	source, _ := number(a.Message)
	versionAt, _ := time.Parse(time.RFC3339Nano, a.Created)
	if a.Edited != "" {
		versionAt, _ = time.Parse(time.RFC3339Nano, a.Edited)
	}
	before := ""
	for page := 0; page < 8; page++ {
		path := "/channels/" + a.Thread + "/messages?limit=100"
		if before != "" {
			path += "&before=" + before
		}
		var messages []Message
		if err := r.get(ctx, path, &messages); err != nil || len(messages) > 100 {
			return "", errOwnerAcknowledgement
		}
		minimum := ^uint64(0)
		found := ""
		for _, m := range messages {
			id, ok := number(m.ID)
			prior, _ := number(before)
			if !ok || m.Channel != a.Thread || before != "" && id >= prior {
				return "", errOwnerAcknowledgement
			}
			minimum = min(minimum, id)
			stamp, err := time.Parse(time.RFC3339Nano, m.Timestamp)
			linked := m.Reference.Message == a.Message && (m.Reference.Channel == "" || m.Reference.Channel == a.Thread)
			linked = linked || strings.Contains(m.Content, fmt.Sprintf("https://discord.com/channels/%s/%s/%s", a.Guild, a.Thread, a.Message))
			if id > source && linked && m.Author.ID == r.State.Destination.Bot && m.Author.Bot && acknowledgementBody(m.Content) && err == nil && !stamp.Before(versionAt) && !stamp.After(r.Now().Add(time.Minute)) {
				if found == "" {
					found = m.ID
				}
			}
		}
		if found != "" {
			return found, nil
		}
		if minimum <= source {
			return "", nil
		}
		if len(messages) < 100 {
			return "", errOwnerAcknowledgementHistory
		}
		before = strconv.FormatUint(minimum, 10)
	}
	return "", errOwnerAcknowledgementHistory
}

func (r *Runner) postOwnerAcknowledgement(ctx context.Context, a ownerAcknowledgement) (string, error) {
	if ctx.Err() != nil {
		return "", acknowledgementPostFailure{kind: "not_sent", retryable: true, retryAt: r.Now().Add(time.Minute)}
	}
	payload := map[string]any{
		"content": ownerAcknowledgementText, "flags": uint64(4096 | 4), "tts": false,
		"allowed_mentions":  map[string]any{"parse": []string{}, "users": []string{}, "roles": []string{}, "replied_user": false},
		"message_reference": map[string]any{"message_id": a.Message, "channel_id": a.Thread, "guild_id": a.Guild, "fail_if_not_exists": true},
		"nonce":             a.version()[:24], "enforce_nonce": true,
	}
	b, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Base+"/channels/"+a.Thread+"/messages", bytes.NewReader(b))
	if err != nil {
		return "", errOwnerAcknowledgement
	}
	req.Header.Set("Authorization", "Bot "+r.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "DiscordBot (https://github.com/AutoDevJapan/cc-pc-tools-rs, 0.1.0)")
	// A redirect must not replay an authenticated POST or change its destination.
	client := *r.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		var u *url.Error
		var dns *net.DNSError
		var dial *net.OpError
		if errors.As(err, &u) && (errors.As(u.Err, &dns) || errors.As(u.Err, &dial) && dial.Op == "dial") {
			return "", acknowledgementPostFailure{kind: "not_sent", retryable: true, retryAt: r.Now().Add(time.Minute)}
		}
		return "", errOwnerAcknowledgement
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		if response.StatusCode == http.StatusTooManyRequests {
			wait := time.Minute
			var rate struct {
				RetryAfter float64 `json:"retry_after"`
			}
			data, _ := io.ReadAll(io.LimitReader(response.Body, 4097))
			if len(data) <= 4096 && json.Unmarshal(data, &rate) == nil && rate.RetryAfter > 0 && rate.RetryAfter <= 86400 {
				wait = time.Duration(rate.RetryAfter * float64(time.Second))
			}
			if seconds, e := strconv.ParseFloat(response.Header.Get("Retry-After"), 64); e == nil && seconds > 0 && seconds <= 86400 {
				wait = max(wait, time.Duration(seconds*float64(time.Second)))
			}
			return "", acknowledgementPostFailure{kind: "rate_limited", retryable: true, retryAt: r.Now().Add(wait)}
		}
		if response.StatusCode >= 300 && response.StatusCode < 500 {
			return "", acknowledgementPostFailure{kind: fmt.Sprintf("post_rejected_%d", response.StatusCode), terminal: true}
		}
		return "", errOwnerAcknowledgement
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	var message Message
	if err != nil || len(data) > 1<<20 || json.Unmarshal(data, &message) != nil || !monthlyID(message.ID) {
		return "", errOwnerAcknowledgement
	}
	return message.ID, nil
}

func (r *Runner) verifyOwnerAcknowledgement(ctx context.Context, a ownerAcknowledgement, id string) error {
	var m Message
	if r.get(ctx, "/channels/"+a.Thread+"/messages/"+id, &m) != nil || m.ID != id || m.Channel != a.Thread || m.Author.ID != r.State.Destination.Bot || !m.Author.Bot || m.Content != ownerAcknowledgementText || m.Reference.Message != a.Message || m.Flags&4096 == 0 || m.MentionEveryone || len(m.Mentions) != 0 {
		return errOwnerAcknowledgement
	}
	at, ok := acknowledgementTime(m.Timestamp)
	created, _ := acknowledgementTime(a.Created)
	if a.Edited != "" {
		created, _ = acknowledgementTime(a.Edited)
	}
	if !ok || at.Before(created) || at.After(r.Now().Add(time.Minute)) {
		return errOwnerAcknowledgement
	}
	return nil
}

// Only the existing tick calls this, under its existing receiver lock. No worker
// is created and no ToDo, question, delivery or application state is changed.
func (r *Runner) processOwnerAcknowledgements(ctx context.Context, health *Health) {
	if !r.OwnerAcknowledgementsActive || r.OwnerAcknowledgementAttempts >= ownerAcknowledgementPerTick || ctx.Err() != nil {
		return
	}
	work, cancel := context.WithTimeout(ctx, ownerAcknowledgementBudget)
	defer cancel()
	s, err := r.acknowledgementCache()
	if err != nil {
		if health != nil {
			health.Errors = append(health.Errors, "owner_acknowledgement_journal_deferred")
		}
		return
	}
	keys := make([]string, 0)
	for key, a := range s.Receipts {
		if a.Terminal != "" && !a.Reported {
			if health != nil {
				health.Errors = append(health.Errors, "owner_acknowledgement_terminal_"+a.Terminal)
			}
			a.Reported = true
			s.Receipts[key] = a
			if r.saveOwnerAcknowledgements(*s) != nil {
				return
			}
		}
		next, _ := acknowledgementTime(a.NextTry)
		if a.Verified == "" && !a.Stale && a.Terminal == "" && !next.After(r.Now()) {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := s.Receipts[keys[i]], s.Receipts[keys[j]]
		if (a.Attempted == "") != (b.Attempted == "") {
			return a.Attempted == ""
		}
		if a.Checked == b.Checked {
			return keys[i] < keys[j]
		}
		return a.Checked < b.Checked
	})
	for _, key := range keys {
		if r.OwnerAcknowledgementAttempts >= ownerAcknowledgementPerTick || work.Err() != nil {
			break
		}
		r.OwnerAcknowledgementAttempts++
		a := s.Receipts[key]
		a.Checked = r.Now().UTC().Format(time.RFC3339Nano)
		s.Receipts[key] = a
		if r.saveOwnerAcknowledgements(*s) != nil {
			break
		}
		if err := r.acknowledgeOwnerVersion(work, s, key); err != nil {
			if health != nil {
				health.Errors = append(health.Errors, "owner_acknowledgement_deferred")
			}
		} else if health != nil && s.Receipts[key].Verified != "" {
			health.OwnerAcknowledgements++
		}
		if health != nil && s.Receipts[key].Stale && !a.Stale {
			health.OwnerAcknowledgementsSuperseded++
		}
		if a = s.Receipts[key]; a.Terminal != "" && !a.Reported {
			if health != nil {
				health.Errors = append(health.Errors, "owner_acknowledgement_terminal_"+a.Terminal)
			}
			a.Reported = true
			s.Receipts[key] = a
			if r.saveOwnerAcknowledgements(*s) != nil {
				return
			}
		}
	}
}

func (r *Runner) acknowledgeOwnerVersion(ctx context.Context, s *ownerAcknowledgements, key string) error {
	a := s.Receipts[key]
	var channel Thread
	if err := r.get(ctx, "/channels/"+a.Thread, &channel); err != nil {
		if code := acknowledgementStatus(err); code == 403 || code == 404 {
			a.Terminal = fmt.Sprintf("source_channel_%d", code)
			s.Receipts[key] = a
			return r.saveOwnerAcknowledgements(*s)
		}
		return errOwnerAcknowledgement
	}
	if channel.ID != a.Thread || channel.Guild != a.Guild || channel.Type != 0 && channel.Type != 5 && channel.Type != 10 && channel.Type != 11 && channel.Type != 12 {
		a.Terminal = "channel_scope_changed"
		s.Receipts[key] = a
		return r.saveOwnerAcknowledgements(*s)
	}
	if channel.Type >= 10 {
		known, ok := r.State.Threads[a.Thread]
		configured := channel.Parent == r.State.Destination.Forum && channel.Owner == r.State.Destination.Bot || channel.Parent == requestForum && (channel.Owner == r.State.Destination.Owner || channel.Owner == r.State.Destination.Bot)
		observed := ok && known.Guild == channel.Guild && known.Parent == channel.Parent && known.Owner == channel.Owner && known.Type == channel.Type && monthlyID(channel.Parent) && monthlyID(channel.Owner)
		if !configured && !observed || channel.Metadata.Locked {
			a.Terminal = "thread_scope_or_lock_changed"
			s.Receipts[key] = a
			return r.saveOwnerAcknowledgements(*s)
		}
		// Discord can reopen an unlocked archived thread by the requested post;
		// this receiver never PATCHes archive/lock/ownership settings.
	}
	var source historyReadMessage
	if err := r.get(ctx, "/channels/"+a.Thread+"/messages/"+a.Message, &source); err != nil {
		if code := acknowledgementStatus(err); code == 403 || code == 404 {
			a.Terminal = fmt.Sprintf("source_message_%d", code)
			s.Receipts[key] = a
			return r.saveOwnerAcknowledgements(*s)
		}
		return errOwnerAcknowledgement
	}
	if source.Type == nil || *source.Type != 0 && *source.Type != 19 {
		a.Terminal = "source_message_type"
		s.Receipts[key] = a
		return r.saveOwnerAcknowledgements(*s)
	}
	if !acknowledgementSourceMatches(a, source.Message) {
		a.Stale = true
		s.Receipts[key] = a
		if err := r.saveOwnerAcknowledgements(*s); err != nil {
			return err
		}
		return r.queueOwnerAcknowledgement(source.Message)
	}
	if a.Reply != "" {
		if r.verifyOwnerAcknowledgement(ctx, a, a.Reply) != nil {
			return errOwnerAcknowledgement
		}
	} else {
		existing, err := r.existingOwnerAcknowledgement(ctx, a)
		if err != nil {
			if errors.Is(err, errOwnerAcknowledgementHistory) {
				a.Terminal = "history_coverage_unconfirmed"
				s.Receipts[key] = a
				return r.saveOwnerAcknowledgements(*s)
			}
			return err
		}
		if existing != "" {
			a.Reply, a.Reused = existing, true
		} else {
			// An uncertain POST is only reconciled by GET on later ticks. Never
			// issue another POST just because a response or local save was lost.
			if a.Attempted != "" {
				at, _ := acknowledgementTime(a.Attempted)
				if !at.IsZero() && r.Now().Sub(at) >= 24*time.Hour {
					a.Terminal = "delivery_unconfirmed"
				} else {
					a.NextTry = r.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339Nano)
				}
				s.Receipts[key] = a
				if r.saveOwnerAcknowledgements(*s) != nil {
					return errOwnerAcknowledgement
				}
				return errOwnerAcknowledgement
			}
			if deadline, ok := ctx.Deadline(); ctx.Err() != nil || ok && time.Until(deadline) < 1500*time.Millisecond {
				return errOwnerAcknowledgement
			}
			if !r.ownerAcknowledgementBotVerified {
				var me struct {
					ID  string `json:"id"`
					Bot bool   `json:"bot"`
				}
				if r.get(ctx, "/users/@me", &me) != nil || me.ID != r.State.Destination.Bot || !me.Bot {
					return errOwnerAcknowledgement
				}
				r.ownerAcknowledgementBotVerified = true
			}
			if deadline, ok := ctx.Deadline(); ctx.Err() != nil || ok && time.Until(deadline) < 1500*time.Millisecond {
				return errOwnerAcknowledgement
			}
			a.Attempted = r.Now().UTC().Format(time.RFC3339Nano)
			s.Receipts[key] = a
			if r.saveOwnerAcknowledgements(*s) != nil {
				return errOwnerAcknowledgement
			}
			id, err := r.postOwnerAcknowledgement(ctx, a)
			if err != nil {
				var failure acknowledgementPostFailure
				if errors.As(err, &failure) {
					if failure.retryable {
						a.Attempted = ""
						a.NextTry = failure.retryAt.UTC().Format(time.RFC3339Nano)
					}
					if failure.terminal {
						a.Terminal = failure.kind
					}
				} else {
					a.NextTry = r.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano)
				}
				s.Receipts[key] = a
				if r.saveOwnerAcknowledgements(*s) != nil {
					return errOwnerAcknowledgement
				}
				return err
			}
			a.Reply = id
			s.Receipts[key] = a
			if r.saveOwnerAcknowledgements(*s) != nil {
				return errOwnerAcknowledgement
			}
			if r.verifyOwnerAcknowledgement(ctx, a, id) != nil {
				return errOwnerAcknowledgement
			}
		}
	}
	a.Verified = r.Now().UTC().Format(time.RFC3339Nano)
	s.Receipts[key] = a
	return r.saveOwnerAcknowledgements(*s)
}

// A durable capture survives a failed acknowledgement enqueue even when the
// channel watermark no longer changes. Revisit a bounded rotating set through
// the receiver's normal exact-message GET, never restart the original task.
func (r *Runner) recoverOwnerAcknowledgementQueue(ctx context.Context, health *Health) {
	if !r.OwnerAcknowledgementsActive || ctx.Err() != nil {
		return
	}
	s, err := r.acknowledgementCache()
	if err != nil {
		health.Errors = append(health.Errors, "owner_acknowledgement_journal_deferred")
		return
	}
	cutoff, _ := acknowledgementTime(ownerAcknowledgementSince)
	requests := make(map[string]*Request)
	for id, q := range r.State.Requests {
		requests[id] = q
	}
	for id, q := range r.State.ConversationCaptures {
		requests[id] = q
	}
	ids := make([]string, 0)
	for id, q := range requests {
		if q == nil || q.Author != r.State.Destination.Owner || !monthlyID(id) || !monthlyID(q.Thread) || q.Source != "discord-api-v10" {
			continue
		}
		at, ok := acknowledgementTime(q.Created)
		if q.Edited != "" {
			at, ok = acknowledgementTime(q.Edited)
		}
		if !ok || at.Before(cutoff) {
			continue
		}
		known := false
		for _, a := range s.Receipts {
			if a.Message == id && a.Thread == q.Thread && a.Author == q.Author && acknowledgementSameTime(a.Created, q.Created) && acknowledgementSameTime(a.Edited, q.Edited) && a.BodySHA == bytesHash([]byte(q.Text)) {
				known = true
				break
			}
		}
		if !known {
			created, _ := acknowledgementTime(q.Created)
			edited, _ := acknowledgementTime(q.Edited)
			editText := ""
			if q.Edited != "" {
				editText = edited.UTC().Format(time.RFC3339Nano)
			}
			a := ownerAcknowledgement{Guild: r.State.Destination.Guild, Thread: q.Thread, Message: id, Author: q.Author, Created: created.UTC().Format(time.RFC3339Nano), Edited: editText, BodySHA: bytesHash([]byte(q.Text))}
			if archived, e := r.archivedAcknowledgement(a.version()); e == nil && archived {
				continue
			}
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	start := sort.Search(len(ids), func(i int) bool { return ids[i] > s.RecoveryCursor })
	work, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	for i := 0; i < min(8, len(ids)); i++ {
		if work.Err() != nil {
			break
		}
		id := ids[(start+i)%len(ids)]
		q := requests[id]
		s.RecoveryCursor = id
		if r.saveOwnerAcknowledgements(*s) != nil {
			health.Errors = append(health.Errors, "owner_acknowledgement_queue_deferred")
			return
		}
		var live Message
		if r.get(work, "/channels/"+q.Thread+"/messages/"+id, &live) != nil || live.ID != q.Message || live.Channel != q.Thread || live.Author.ID != q.Author || live.Author.Bot {
			health.Errors = append(health.Errors, "owner_acknowledgement_capture_recheck_deferred")
			continue
		}
		if r.queueOwnerAcknowledgement(live) != nil {
			health.Errors = append(health.Errors, "owner_acknowledgement_queue_deferred")
		}
	}
}
