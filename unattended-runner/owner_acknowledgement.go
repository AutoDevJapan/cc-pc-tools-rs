package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

var errOwnerAcknowledgement = errors.New("owner acknowledgement deferred")

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
}

func (a ownerAcknowledgement) version() string {
	return canonicalHash([]string{a.Guild, a.Thread, a.Message, a.Author, a.Created, a.Edited, a.BodySHA})
}

type ownerAcknowledgements struct {
	Domain      string                          `json:"domain"`
	Destination Destination                     `json:"destination"`
	Receipts    map[string]ownerAcknowledgement `json:"receipts"`
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
	b, err := json.Marshal(s)
	if err != nil || len(b) > ownerAcknowledgementLimit {
		return errOwnerAcknowledgement
	}
	return publish(r.ownerAcknowledgementPath(), s)
}

// Called only by the existing periodic receiver after a verified owner capture.
// The journal contains IDs/hashes, never the instruction or a second task state.
func (r *Runner) queueOwnerAcknowledgement(m Message) error {
	if !r.OwnerAcknowledgementsActive {
		return nil
	}
	if m.Author.ID != r.State.Destination.Owner || m.Author.Bot || !monthlyID(m.Channel) || !monthlyID(m.ID) {
		return nil
	}
	created, e1 := time.Parse(time.RFC3339Nano, m.Timestamp)
	cutoff, _ := time.Parse(time.RFC3339Nano, ownerAcknowledgementSince)
	versionAt := created
	if m.Edited != "" {
		edited, err := time.Parse(time.RFC3339Nano, m.Edited)
		if err != nil || edited.Before(created) {
			return errOwnerAcknowledgement
		}
		versionAt = edited
	}
	if e1 != nil || versionAt.After(r.Now()) {
		return errOwnerAcknowledgement
	}
	if versionAt.Before(cutoff) {
		return nil
	}
	s, err := r.loadOwnerAcknowledgements()
	if err != nil {
		return err
	}
	a := ownerAcknowledgement{Guild: r.State.Destination.Guild, Thread: m.Channel, Message: m.ID, Author: m.Author.ID, Created: m.Timestamp, Edited: m.Edited, BodySHA: bytesHash([]byte(m.Content)), Queued: r.Now().UTC().Format(time.RFC3339Nano)}
	key := a.version()
	if _, exists := s.Receipts[key]; exists {
		return nil
	}
	s.Receipts[key] = a
	return r.saveOwnerAcknowledgements(s)
}

func acknowledgementSourceMatches(a ownerAcknowledgement, live Message) bool {
	return live.ID == a.Message && live.Channel == a.Thread && live.Author.ID == a.Author && !live.Author.Bot && live.Timestamp == a.Created && live.Edited == a.Edited && bytesHash([]byte(live.Content)) == a.BodySHA
}

func acknowledgementBody(text string) bool {
	return text == ownerAcknowledgementText || strings.Contains(text, "受け取りました") || strings.Contains(text, "受け付けました") || strings.Contains(text, "Discordの指示を共有ToDo")
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
		if len(messages) < 100 || minimum <= source {
			return "", nil
		}
		before = strconv.FormatUint(minimum, 10)
	}
	return "", errOwnerAcknowledgement
}

func (r *Runner) postOwnerAcknowledgement(ctx context.Context, a ownerAcknowledgement) (string, error) {
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
	response, err := r.Client.Do(req)
	if err != nil {
		return "", errOwnerAcknowledgement
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
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
	s, err := r.loadOwnerAcknowledgements()
	if err != nil {
		if health != nil {
			health.Errors = append(health.Errors, "owner_acknowledgement_journal_deferred")
		}
		return
	}
	keys := make([]string, 0)
	for key, a := range s.Receipts {
		if a.Verified == "" && !a.Stale {
			keys = append(keys, key)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return s.Receipts[keys[i]].Checked < s.Receipts[keys[j]].Checked })
	for _, key := range keys {
		if r.OwnerAcknowledgementAttempts >= ownerAcknowledgementPerTick || work.Err() != nil {
			break
		}
		r.OwnerAcknowledgementAttempts++
		a := s.Receipts[key]
		a.Checked = r.Now().UTC().Format(time.RFC3339Nano)
		s.Receipts[key] = a
		if r.saveOwnerAcknowledgements(s) != nil {
			break
		}
		if err := r.acknowledgeOwnerVersion(work, &s, key); err != nil {
			if health != nil {
				health.Errors = append(health.Errors, "owner_acknowledgement_deferred")
			}
		} else if health != nil && s.Receipts[key].Verified != "" {
			health.OwnerAcknowledgements++
		}
	}
}

func (r *Runner) acknowledgeOwnerVersion(ctx context.Context, s *ownerAcknowledgements, key string) error {
	a := s.Receipts[key]
	var channel Thread
	if r.get(ctx, "/channels/"+a.Thread, &channel) != nil || channel.ID != a.Thread || channel.Guild != a.Guild {
		return errOwnerAcknowledgement
	}
	var source Message
	if r.get(ctx, "/channels/"+a.Thread+"/messages/"+a.Message, &source) != nil {
		return errOwnerAcknowledgement
	}
	if !acknowledgementSourceMatches(a, source) {
		a.Stale = true
		s.Receipts[key] = a
		return r.saveOwnerAcknowledgements(*s)
	}
	if a.Reply != "" {
		if r.verifyOwnerAcknowledgement(ctx, a, a.Reply) != nil {
			return errOwnerAcknowledgement
		}
	} else {
		existing, err := r.existingOwnerAcknowledgement(ctx, a)
		if err != nil {
			return err
		}
		if existing != "" {
			a.Reply, a.Reused = existing, true
		} else {
			// An uncertain POST is only reconciled by GET on later ticks. Never
			// issue another POST just because a response or local save was lost.
			if a.Attempted != "" {
				return errOwnerAcknowledgement
			}
			a.Attempted = r.Now().UTC().Format(time.RFC3339Nano)
			s.Receipts[key] = a
			if r.saveOwnerAcknowledgements(*s) != nil {
				return errOwnerAcknowledgement
			}
			id, err := r.postOwnerAcknowledgement(ctx, a)
			if err != nil {
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
