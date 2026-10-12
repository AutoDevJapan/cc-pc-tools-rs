// record-recovery stages original Discord evidence. It never restores live state,
// sends messages, starts workers, or interprets historical answers as new consent.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"
)

type obj = map[string]any

func str(v any) string { s, _ := v.(string); return s }
func object(v any) obj { m, _ := v.(map[string]any); return m }
func array(v any) []any {
	if a, ok := v.([]any); ok {
		return a
	}
	if a, ok := v.([]obj); ok {
		out := make([]any, len(a))
		for i, x := range a {
			out[i] = x
		}
		return out
	}
	return nil
}
func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

var numeric = regexp.MustCompile(`^[0-9]{17,20}$`)
var links = regexp.MustCompile(`https://discord\.com/channels/([0-9]{17,20})/([0-9]{17,20})(?:/([0-9]{17,20}))?`)

func readJSON(path string) (obj, []byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, nil, errors.New("source_unavailable")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 40<<20+1))
	if e != nil || len(b) > 40<<20 {
		return nil, nil, errors.New("source_read_failed")
	}
	var v obj
	if json.Unmarshal(bytes.TrimPrefix(b, []byte{239, 187, 191}), &v) != nil || v == nil {
		return nil, nil, errors.New("source_invalid_json")
	}
	return v, b, nil
}

func boundedText(path string) ([]byte, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
		return nil, errors.New("spec_unavailable_or_oversize")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("spec_read_failed")
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 1<<20+1))
	if e != nil || len(b) > 1<<20 {
		return nil, errors.New("spec_read_failed")
	}
	return b, nil
}

func privateDir(path string) error {
	abs, e := filepath.Abs(path)
	if e != nil {
		return errors.New("invalid_evidence_path")
	}
	for parent := filepath.Dir(abs); ; parent = filepath.Dir(parent) {
		_, err := os.Lstat(filepath.Join(parent, ".git"))
		if err == nil {
			return errors.New("evidence_inside_repository_refused")
		}
		if !os.IsNotExist(err) {
			return errors.New("evidence_parent_check_failed")
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	if e := os.Mkdir(path, 0700); e != nil {
		return errors.New("new_evidence_directory_required")
	}
	if runtime.GOOS == "windows" {
		u, e := user.Current()
		if e != nil {
			return errors.New("evidence_acl_failed")
		}
		cmd := exec.Command("icacls.exe", path, "/inheritance:r", "/grant:r", u.Username+":(OI)(CI)F", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F")
		if cmd.Run() != nil {
			return errors.New("evidence_acl_failed")
		}
	}
	return nil
}
func writeNew(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return errors.New("serialization_failed")
	}
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return errors.New("evidence_write_failed")
	}
	defer f.Close()
	if _, e = f.Write(b); e != nil {
		return errors.New("evidence_write_failed")
	}
	if f.Sync() != nil {
		return errors.New("evidence_sync_failed")
	}
	return nil
}

// Only these supplied operational records are inspected; no recursive PC scan.
func seeds(home, todo, output string) (obj, error) {
	if e := privateDir(output); e != nil {
		return nil, e
	}
	ids := map[string]map[string]bool{}
	sources := []any{}
	add := func(id, origin string) {
		if numeric.MatchString(id) {
			if ids[id] == nil {
				ids[id] = map[string]bool{}
			}
			ids[id][origin] = true
		}
	}
	for _, name := range []string{"state.json", "log_threads.json"} {
		v, b, e := readJSON(filepath.Join(home, name))
		if e != nil {
			sources = append(sources, obj{"kind": name, "status": e.Error()})
			continue
		}
		sources = append(sources, obj{"kind": name, "status": "read", "sha256": digest(b), "bytes": len(b)})
		if name == "log_threads.json" {
			for key, id := range v {
				add(str(id), name)
				add(key, name)
			}
		} else {
			for _, q := range array(v["questions"]) {
				add(str(object(q)["thread_id"]), name)
			}
			for id := range object(v["requests"]) {
				add(id, name)
			}
		}
	}
	db, b, e := readJSON(todo)
	meta := obj{"id": 2467, "found": false}
	if e != nil {
		sources = append(sources, obj{"kind": "todo2467", "status": e.Error()})
	} else {
		sources = append(sources, obj{"kind": "todo2467", "status": "read", "store_sha256": digest(b)})
		for _, x := range array(db["items"]) {
			v := object(x)
			if v["id"] != float64(2467) {
				continue
			}
			meta["found"] = true
			meta["status"] = v["status"]
			meta["progress"] = v["progress"]
			meta["job_count"] = len(array(v["jobs"]))
			meta["spec_history_count"] = len(array(v["spec_history"]))
			meta["note_history_count"] = len(array(v["note_history"]))
			// Paths/text remain private. Link IDs only are exposed to the next bounded read.
			texts := []string{str(v["note"])}
			for _, n := range array(v["note_history"]) {
				texts = append(texts, str(object(n)["note"]))
			}
			for _, text := range texts {
				for _, m := range links.FindAllStringSubmatch(text, -1) {
					if m[1] == "1552944707464597524" {
						add(m[2], "todo2467_note")
					}
				}
			}
			paths := []string{str(v["spec"])}
			for _, h := range array(v["spec_history"]) {
				paths = append(paths, str(object(h)["spec"]))
			}
			specs := []any{}
			for i, p := range paths {
				r := obj{"index": i, "status": "unavailable"}
				if p != "" {
					data, err := boundedText(p)
					if err == nil {
						r["status"] = "read"
						r["bytes"] = len(data)
						r["sha256"] = digest(data)
						for _, m := range links.FindAllStringSubmatch(string(data), -1) {
							if m[1] == "1552944707464597524" {
								add(m[2], "todo2467_spec")
							}
						}
					}
				}
				specs = append(specs, r)
			}
			meta["specs"] = specs
		}
	}
	rows := []any{}
	keys := []string{}
	for id := range ids {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		origins := []string{}
		for origin := range ids[id] {
			origins = append(origins, origin)
		}
		sort.Strings(origins)
		rows = append(rows, obj{"thread_id": id, "sources": origins})
	}
	manifest := obj{"version": 1, "kind": "original-record-seeds", "todo_id": 2467, "sources": sources, "todo_metadata": meta, "threads": rows, "created_at": time.Now().UTC().Format(time.RFC3339), "authority": "historical identifiers only; no executable instructions"}
	if e := writeNew(filepath.Join(output, "seeds.json"), manifest); e != nil {
		return nil, e
	}
	return obj{"ok": true, "seed_threads": len(rows), "sources": sources, "todo_metadata": meta, "threads": rows}, nil
}

type api struct {
	client      *http.Client
	base, token string
}

func (a *api) get(ctx context.Context, path string) (json.RawMessage, error) {
	for attempt := 0; attempt < 5; attempt++ {
		req, e := http.NewRequestWithContext(ctx, "GET", a.base+path, nil)
		if e != nil {
			return nil, errors.New("request_failed")
		}
		req.Header.Set("Authorization", "Bot "+a.token)
		req.Header.Set("User-Agent", "DiscordBot (https://github.com/AutoDevJapan/cc-pc-tools-rs, 0.1.0)")
		r, e := a.client.Do(req)
		if e != nil {
			return nil, errors.New("network_failed")
		}
		b, e := io.ReadAll(io.LimitReader(r.Body, 8<<20+1))
		r.Body.Close()
		if e != nil || len(b) > 8<<20 {
			return nil, errors.New("response_read_failed")
		}
		if r.StatusCode == 429 {
			var limit obj
			json.Unmarshal(b, &limit)
			wait, _ := limit["retry_after"].(float64)
			if wait <= 0 || wait > 30 {
				return nil, errors.New("rate_limit_wait_exceeded")
			}
			select {
			case <-time.After(time.Duration(wait * float64(time.Second))):
			case <-ctx.Done():
				return nil, errors.New("deadline")
			}
			continue
		}
		if r.StatusCode != 200 {
			return nil, fmt.Errorf("http_status_%d", r.StatusCode)
		}
		if !json.Valid(b) {
			return nil, errors.New("invalid_api_json")
		}
		return b, nil
	}
	return nil, errors.New("rate_limit_attempts_exceeded")
}
func (a *api) getObj(ctx context.Context, path string) (obj, error) {
	b, e := a.get(ctx, path)
	if e != nil {
		return nil, e
	}
	var v obj
	if json.Unmarshal(b, &v) != nil || v == nil {
		return nil, errors.New("invalid_api_object")
	}
	return v, nil
}
func newAPI(token string) *api {
	return &api{client: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect_refused") }}, base: "https://discord.com/api/v10", token: token}
}

func history(ctx context.Context, a *api, tid string, maxPages int) ([]obj, bool, error) {
	rows := []obj{}
	seen := map[string]bool{}
	before := ""
	for p := 0; p < maxPages; p++ {
		q := url.Values{"limit": {"100"}}
		if before != "" {
			q.Set("before", before)
		}
		b, e := a.get(ctx, "/channels/"+tid+"/messages?"+q.Encode())
		if e != nil {
			return rows, false, e
		}
		var page []obj
		if json.Unmarshal(b, &page) != nil || page == nil {
			return rows, false, errors.New("invalid_message_page")
		}
		oldest := before
		for _, m := range page {
			id := str(m["id"])
			if !numeric.MatchString(id) || str(m["channel_id"]) != tid || seen[id] {
				return rows, false, errors.New("invalid_history_identity_or_cursor")
			}
			seen[id] = true
			if oldest == "" || less(id, oldest) {
				oldest = id
			}
			rows = append(rows, m)
		}
		if len(page) < 100 {
			sort.Slice(rows, func(i, j int) bool { return less(str(rows[i]["id"]), str(rows[j]["id"])) })
			return rows, true, nil
		}
		if oldest == before {
			return rows, false, errors.New("history_cursor_not_advanced")
		}
		before = oldest
	}
	return rows, false, nil
}
func less(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}
func question(body string) bool {
	if strings.Contains(body, "<!-- cc-decision ") {
		return true
	}
	first := strings.TrimSpace(strings.SplitN(body, "\n", 2)[0])
	if strings.HasPrefix(first, "状態:") {
		return first == "状態: 要判断"
	}
	if strings.Contains(body, "（1時間たったので再送です）") || strings.Contains(body, "未回答の質問をまとめて再通知します") {
		return false
	}
	return (strings.Contains(body, "a/b") || regexp.MustCompile(`(?m)^\s*(?:[-*]\s*)?[ab][.．、:：）)\s]`).MatchString(body)) && (strings.Contains(body, "教えてください") || strings.Contains(body, "判断") || strings.Contains(body, "選んで") || strings.Contains(body, "決めてください"))
}

// Preserve original texts privately; stdout/report contain counts, IDs and hashes only.
func stageHistory(tid, bot, owner string, rows []obj, complete bool) (obj, obj) {
	rows = append([]obj(nil), rows...)
	sort.Slice(rows, func(i, j int) bool { return less(str(rows[i]["id"]), str(rows[j]["id"])) })
	records := []any{}
	questions := []obj{}
	replies := []obj{}
	attachments := []obj{}
	latest := ""
	byID := map[string]obj{}
	for _, m := range rows {
		byID[str(m["id"])] = m
	}
	for _, m := range rows {
		id := str(m["id"])
		author := object(m["author"])
		aid := str(author["id"])
		if aid != bot && aid != owner {
			continue
		}
		if aid == owner && author["bot"] == true {
			continue
		}
		if aid == bot && author["bot"] != true {
			continue
		}
		r := obj{"id": id, "thread_id": tid, "author_id": aid, "timestamp": m["timestamp"], "edited_timestamp": m["edited_timestamp"], "content": str(m["content"]), "content_sha256": digest([]byte(str(m["content"]))), "message_reference": m["message_reference"], "embeds": m["embeds"], "attachments": m["attachments"], "components": m["components"]}
		records = append(records, r)
		if aid == bot && question(str(m["content"])) {
			latest = id
			questions = append(questions, obj{"question_message": id, "body_sha256": r["content_sha256"], "detection": "candidate_only", "decision_scope_verified": false})
		}
		if aid == owner {
			ref := object(m["message_reference"])
			refid := str(ref["message_id"])
			original := byID[refid]
			verified := refid != "" && str(ref["channel_id"]) == tid && less(refid, id) && str(object(original["author"])["id"]) == bot && object(original["author"])["bot"] == true && question(str(original["content"]))
			q := latest
			association := "sequence_candidate"
			if verified {
				q = refid
				association = "explicit_same_thread_reference"
			} else if refid != "" {
				q = ""
				association = "explicit_reference_unverified"
			}
			if q == "" && refid == "" {
				association = "missing_question"
			}
			replies = append(replies, obj{"message_id": id, "author_id": aid, "reply_sha256": r["content_sha256"], "question_message": q, "original_reference_message_id": refid, "association": association, "decision_scope_verified": false, "delivery_verified": false, "applied": false})
		}
		for _, x := range array(m["attachments"]) {
			v := object(x)
			name := strings.ToLower(str(v["filename"]))
			if name == "client-projects.txt" || name == "client-projects.txt.bak" {
				attachments = append(attachments, obj{"message_id": id, "author_id": aid, "attachment_id": v["id"], "url": v["url"], "filename": v["filename"], "bytes": v["size"], "original_provenance_verified": false})
			}
		}
	}
	missing := 0
	explicit := 0
	unverified := 0
	for _, r := range replies {
		if r["association"] == "missing_question" {
			missing++
		}
		if r["association"] == "explicit_same_thread_reference" {
			explicit++
		}
		if r["association"] == "explicit_reference_unverified" {
			unverified++
		}
	}
	stage := obj{"kind": "original-history-staging", "thread_id": tid, "bot_id": bot, "owner_id": owner, "history_complete": complete, "messages": records, "questions": questions, "owner_replies": replies, "original_setting_attachment_candidates": attachments, "ready_for_live_import": false, "source": "authenticated Discord API GET"}
	meta := obj{"thread_id": tid, "history_complete": complete, "messages_read": len(rows), "messages_staged": len(records), "question_candidates": len(questions), "owner_replies_identity_verified": len(replies), "reply_explicit_generation_reference": explicit, "reply_explicit_reference_unverified": unverified, "reply_missing_question": missing, "setting_attachment_candidates": len(attachments), "verified_decision_scopes": 0, "live_imports": 0}
	return stage, meta
}

func collect(ctx context.Context, a *api, cfg, seedsPath, output string, ids []string, maxPages int) (obj, error) {
	if len(ids) == 0 || len(ids) > 10 || maxPages < 1 || maxPages > 100 {
		return nil, errors.New("bounded_explicit_threads_required")
	}
	config, _, e := readJSON(cfg)
	if e != nil {
		return nil, e
	}
	guild := str(config["guild_id"])
	owner := str(config["owner_id"])
	if owner == "" {
		owner = str(config["owner_user_id"])
	}
	task := str(config["task_channel_id"])
	if guild != "1552944707464597524" || owner != "730822776147148835" || task != "1554276307997692004" {
		return nil, errors.New("configured_identity_mismatch")
	}
	seed, seedBytes, e := readJSON(seedsPath)
	if e != nil {
		return nil, e
	}
	if seed["kind"] != "original-record-seeds" {
		return nil, errors.New("invalid_seeds")
	}
	known := map[string]bool{}
	for _, x := range array(seed["threads"]) {
		known[str(object(x)["thread_id"])] = true
	}
	unique := map[string]bool{}
	for _, id := range ids {
		if !numeric.MatchString(id) || !known[id] || unique[id] {
			return nil, errors.New("untrusted_or_duplicate_thread")
		}
		unique[id] = true
	}
	me, e := a.getObj(ctx, "/users/@me")
	if e != nil {
		return nil, e
	}
	bot := str(me["id"])
	if me["bot"] != true || bot != "1554269962229649478" || str(config["application_id"]) != bot {
		return nil, errors.New("bot_identity_unavailable")
	}
	if e = privateDir(output); e != nil {
		return nil, e
	}
	report := obj{"version": 1, "kind": "original-record-report", "created_at": time.Now().UTC().Format(time.RFC3339), "guild_id": guild, "bot_id": bot, "owner_id": owner, "seed_sha256": digest(seedBytes), "threads": []any{}, "live_state_changed": false, "messages_sent": 0, "workers_started": 0, "guard_changed": false}
	resultRows := []any{}
	for _, tid := range ids {
		channel, err := a.getObj(ctx, "/channels/"+tid)
		if err != nil {
			resultRows = append(resultRows, obj{"thread_id": tid, "status": err.Error()})
			continue
		}
		typ, _ := channel["type"].(float64)
		parent := str(channel["parent_id"])
		// Request forum is a fixed operational forum from cc-ask/core, never an arbitrary guild channel.
		if str(channel["id"]) != tid || str(channel["guild_id"]) != guild || (typ != 10 && typ != 11 && typ != 12) || (parent != task && parent != requestForum) || (parent == task && str(channel["owner_id"]) != bot) {
			resultRows = append(resultRows, obj{"thread_id": tid, "status": "outside_trusted_forums"})
			continue
		}
		rows, complete, err := history(ctx, a, tid, maxPages)
		stage, meta := stageHistory(tid, bot, owner, rows, complete)
		meta["status"] = "staged"
		if !complete {
			meta["status"] = "history_incomplete_max_pages"
		}
		if err != nil {
			meta["status"] = err.Error()
		}
		meta["thread_last_message_id"] = channel["last_message_id"]
		stage["guild_id"] = guild
		stage["parent_id"] = parent
		stage["collected_at"] = time.Now().UTC().Format(time.RFC3339)
		if e = writeNew(filepath.Join(output, tid+".private.json"), stage); e != nil {
			return nil, e
		}
		data, _ := json.MarshalIndent(stage, "", "  ")
		meta["stage_sha256"] = digest(data)
		resultRows = append(resultRows, meta)
	}
	report["threads"] = resultRows
	allComplete := true
	for _, x := range resultRows {
		r := object(x)
		if r["status"] != "staged" || r["history_complete"] != true {
			allComplete = false
		}
	}
	report["requested_histories_complete"] = allComplete
	if e = writeNew(filepath.Join(output, "report.json"), report); e != nil {
		return nil, e
	}
	return report, nil
}

// Fixed in the existing cc-ask source; populated after source verification.
const requestForum = "1554771264298819675"

// Reindex collected evidence offline, preserving the acquisition receipt/hash.
// Also collect same-guild references as bounded future GET candidates, not authority.
func reindex(source, output string) (obj, error) {
	if source == "" {
		return nil, errors.New("explicit_acquisition_source_required")
	}
	report, reportBytes, e := readJSON(filepath.Join(source, "report.json"))
	if e != nil {
		return nil, e
	}
	if report["kind"] != "original-record-report" || report["guild_id"] != "1552944707464597524" || report["bot_id"] != "1554269962229649478" || report["owner_id"] != "730822776147148835" {
		return nil, errors.New("invalid_acquisition_report")
	}
	created := false
	rows := []any{}
	references := map[string][]any{}
	for _, x := range array(report["threads"]) {
		acquired := object(x)
		tid := str(acquired["thread_id"])
		if !numeric.MatchString(tid) {
			return nil, errors.New("invalid_acquisition_thread")
		}
		if str(acquired["stage_sha256"]) == "" {
			continue
		}
		old, b, err := readJSON(filepath.Join(source, tid+".private.json"))
		if err != nil {
			return nil, err
		}
		if digest(b) != str(acquired["stage_sha256"]) || old["thread_id"] != tid || old["source"] != "authenticated Discord API GET" {
			return nil, errors.New("acquisition_hash_or_identity_mismatch")
		}
		if old["bot_id"] != report["bot_id"] || old["owner_id"] != report["owner_id"] || old["guild_id"] != report["guild_id"] {
			return nil, errors.New("acquisition_identity_mismatch")
		}
		messages := []obj{}
		seen := map[string]bool{}
		for _, entry := range array(old["messages"]) {
			r := object(entry)
			id := str(r["id"])
			aid := str(r["author_id"])
			if !numeric.MatchString(id) || seen[id] || (aid != str(old["bot_id"]) && aid != str(old["owner_id"])) || digest([]byte(str(r["content"]))) != str(r["content_sha256"]) {
				return nil, errors.New("invalid_acquired_message")
			}
			seen[id] = true
			m := obj{"id": id, "channel_id": tid, "content": r["content"], "author": obj{"id": aid, "bot": aid == str(old["bot_id"])}, "timestamp": r["timestamp"], "edited_timestamp": r["edited_timestamp"], "message_reference": r["message_reference"], "embeds": r["embeds"], "attachments": r["attachments"], "components": r["components"]}
			messages = append(messages, m)
			for _, link := range links.FindAllStringSubmatch(str(r["content"]), -1) {
				if link[1] == "1552944707464597524" {
					references[link[2]] = append(references[link[2]], obj{"origin_thread_id": tid, "origin_message_id": id, "source_message_sha256": r["content_sha256"]})
				}
			}
		}
		complete, _ := old["history_complete"].(bool)
		stage, meta := stageHistory(tid, str(old["bot_id"]), str(old["owner_id"]), messages, complete)
		stage["guild_id"] = old["guild_id"]
		stage["parent_id"] = old["parent_id"]
		stage["collected_at"] = old["collected_at"]
		stage["acquisition_stage_sha256"] = acquired["stage_sha256"]
		stage["acquisition_report_sha256"] = digest(reportBytes)
		if !created {
			if e = privateDir(output); e != nil {
				return nil, e
			}
			created = true
		}
		if e = writeNew(filepath.Join(output, tid+".private.json"), stage); e != nil {
			return nil, e
		}
		data, _ := json.MarshalIndent(stage, "", "  ")
		meta["stage_sha256"] = digest(data)
		meta["messages_read"] = acquired["messages_read"]
		meta["status"] = acquired["status"]
		rows = append(rows, meta)
	}
	refrows := []any{}
	keys := []string{}
	for id := range references {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		refrows = append(refrows, obj{"thread_id": id, "sources": []string{"authenticated_original_history_reference"}, "reference_evidence": references[id]})
	}
	seeds := obj{"kind": "original-record-seeds", "version": 1, "threads": refrows, "source_acquisition_report_sha256": digest(reportBytes), "authority": "historical references only; no executable instructions"}
	if !created {
		if e = privateDir(output); e != nil {
			return nil, e
		}
	}
	if e = writeNew(filepath.Join(output, "reference-seeds.json"), seeds); e != nil {
		return nil, e
	}
	result := obj{"version": 1, "kind": "offline-original-record-index", "threads": rows, "source_acquisition_report_sha256": digest(reportBytes), "reference_threads": refrows, "live_state_changed": false, "guard_changed": false, "workers_started": 0, "messages_sent": 0}
	if e = writeNew(filepath.Join(output, "index-report.json"), result); e != nil {
		return nil, e
	}
	return result, nil
}

func backups(root, output string) (obj, error) {
	if e := privateDir(output); e != nil {
		return nil, e
	}
	rows := []any{}
	count := 0
	for _, suffix := range []string{"", ".bak", ".backup", ".old", ".orig"} {
		path := filepath.Join(root, "client-projects.txt"+suffix)
		r := obj{"candidate_index": len(rows), "status": "missing"}
		info, statErr := os.Lstat(path)
		if statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
			r["status"] = "not_regular_or_linked"
			rows = append(rows, r)
			continue
		}
		f, e := os.Open(path)
		if e == nil {
			info, err := f.Stat()
			if err == nil && info.Mode().IsRegular() && info.Size() <= 1<<20 {
				b, err := io.ReadAll(f)
				if err == nil {
					r["status"] = "candidate"
					r["bytes"] = len(b)
					r["sha256"] = digest(b)
					r["provenance_verified"] = false
					if suffix == "" {
						r["status"] = "current_live_file"
					} else {
						count++
					}
				}
			} else {
				r["status"] = "not_regular_or_oversize"
			}
			f.Close()
		} else if !os.IsNotExist(e) {
			r["status"] = "read_failed"
		}
		rows = append(rows, r)
	}
	// Exact independently supplied recovery archive; inspect names only, no contents.
	archive := "D:/CC/Work/cc-tools-recovery-20261012.zip"
	z, e := zip.OpenReader(archive)
	zr := obj{"status": "unavailable"}
	if e == nil {
		defer z.Close()
		matched := 0
		for _, f := range z.File {
			if strings.EqualFold(filepath.Base(strings.ReplaceAll(f.Name, "\\", "/")), "client-projects.txt") {
				matched++
			}
		}
		zr = obj{"status": "toc_read", "entries": len(z.File), "exact_setting_candidates": matched}
		count += matched
	}
	report := obj{"kind": "explicit-setting-backup-probe", "paths_checked": len(rows), "rows": rows, "independent_recovery_archive": zr, "candidates": count, "original_restored": false}
	if e := writeNew(filepath.Join(output, "backup-report.json"), report); e != nil {
		return nil, e
	}
	return report, nil
}

// A shadow device is supplied by the administrator's successful VSS inventory.
// Probe only the two historical exact locations; never traverse their links.
func snapshotProbe(root, output string) (obj, error) {
	if !regexp.MustCompile(`^\\\\\?\\GLOBALROOT\\Device\\HarddiskVolumeShadowCopy[0-9]+\\?$`).MatchString(root) {
		return nil, errors.New("invalid_shadow_device")
	}
	root = strings.TrimRight(root, `\`) + `\`
	if e := privateDir(output); e != nil {
		return nil, e
	}
	rootInfo, rootErr := os.Stat(root)
	if rootErr != nil || !rootInfo.IsDir() {
		status := "not_directory"
		if os.IsNotExist(rootErr) {
			status = "not_found"
		} else if os.IsPermission(rootErr) {
			status = "access_denied"
		} else if rootErr != nil {
			status = "read_failed"
		}
		report := obj{"kind": "exact-shadow-setting-probe", "status": "shadow_root_unavailable", "root_status": status, "root_access_verified": false, "original_restored": false, "shadow_device_sha256": digest([]byte(root))}
		if e := writeNew(filepath.Join(output, "shadow-report.json"), report); e != nil {
			return nil, e
		}
		return report, nil
	}
	rows := []any{}
	for index, relative := range []string{`CC\Tools\client-projects.txt`, `Users\makhmeto\Desktop\dev\ADJ\cc-pc-tools-rs\client-projects.txt`} {
		r := obj{"candidate_index": index, "status": "missing", "original_provenance_verified": false}
		path := strings.TrimRight(root, `\`)
		valid := true
		for component, part := range strings.Split(relative, `\`) {
			path += `\` + part
			info, e := os.Lstat(path)
			if e != nil {
				r["missing_or_failed_component_index"] = component
				if !os.IsNotExist(e) {
					r["status"] = "probe_failed"
				}
				valid = false
				break
			}
			if info.Mode()&os.ModeSymlink != 0 || info.Mode()&os.ModeIrregular != 0 {
				r["status"] = "linked_or_irregular_ancestor"
				valid = false
				break
			}
		}
		if valid {
			f, e := os.Open(path)
			if e != nil {
				r["status"] = "read_failed"
			} else {
				info, e := f.Stat()
				if e != nil || !info.Mode().IsRegular() || info.Size() > 1<<20 {
					r["status"] = "not_regular_or_oversize"
				} else {
					b, e := io.ReadAll(io.LimitReader(f, 1<<20+1))
					if e != nil || len(b) > 1<<20 {
						r["status"] = "read_failed"
					} else {
						n := 0
						for _, line := range strings.Split(strings.TrimPrefix(string(b), "\ufeff"), "\n") {
							line = strings.TrimSpace(line)
							if line != "" && !strings.HasPrefix(line, "#") {
								n++
							}
						}
						r["status"] = "candidate_read"
						r["bytes"] = len(b)
						r["sha256"] = digest(b)
						r["noncomment_lines"] = n
						r["modified_at"] = info.ModTime().UTC().Format(time.RFC3339)
						r["empty_or_tiny"] = len(b) <= 32
					}
				}
				f.Close()
			}
		}
		rows = append(rows, r)
	}
	report := obj{"kind": "exact-shadow-setting-probe", "root_access_verified": true, "rows": rows, "original_restored": false, "shadow_device_sha256": digest([]byte(root))}
	if e := writeNew(filepath.Join(output, "shadow-report.json"), report); e != nil {
		return nil, e
	}
	return report, nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println(`{"ok":false,"code":"command_required"}`)
		os.Exit(2)
	}
	f := flag.NewFlagSet("recovery", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	home := f.String("home", "D:/CC/Tools/discord-ask", "")
	todo := f.String("todo", "D:/CC/Tools/todo/todo.json", "")
	out := f.String("output", "", "")
	cfg := f.String("config", filepath.Join(os.Getenv("USERPROFILE"), ".godd", "discord-notify.json"), "")
	seed := f.String("seeds", "", "")
	threads := f.String("threads", "", "")
	pages := f.Int("max-pages", 20, "")
	shadow := f.String("shadow-root", "", "")
	source := f.String("source", "", "")
	if f.Parse(os.Args[2:]) != nil || *out == "" {
		fmt.Println(`{"ok":false,"code":"invalid_arguments"}`)
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var v obj
	var e error
	switch os.Args[1] {
	case "seeds":
		v, e = seeds(*home, *todo, *out)
	case "backups":
		v, e = backups("D:/CC/Tools", *out)
	case "snapshot":
		v, e = snapshotProbe(*shadow, *out)
	case "reindex":
		v, e = reindex(*source, *out)
	case "collect":
		token := os.Getenv("GODD_DISCORD_BOT_TOKEN")
		if token == "" {
			e = errors.New("bot_token_unavailable")
		} else {
			v, e = collect(ctx, newAPI(token), *cfg, *seed, *out, strings.Split(*threads, ","), *pages)
		}
	default:
		e = errors.New("invalid_command")
	}
	if e != nil {
		v = obj{"ok": false, "code": e.Error()}
	}
	b, _ := json.Marshal(v)
	fmt.Println(string(b))
	if e != nil {
		os.Exit(1)
	}
	if os.Args[1] == "collect" && v["requested_histories_complete"] != true {
		os.Exit(3)
	}
}
